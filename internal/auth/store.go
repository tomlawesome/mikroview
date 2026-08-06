// Package auth implements mikroview's local username/password
// authentication: user accounts (this file), Argon2id password hashing
// (password.go), in-memory sessions (session.go), and login-attempt
// rate limiting (ratelimit.go). It also owns OIDC/SSO identity storage
// and just-in-time provisioning (FindOrCreateOIDCUser below) -- the
// OIDC protocol itself (discovery, the auth-code+PKCE exchange, ID
// token verification) lives in the separate, provider-agnostic
// internal/oidc package, which this package doesn't import; a User
// provisioned via OIDC is just a User, indistinguishable to Session/
// SessionStore from one created by Register/CreateUser.
//
// Mikroview stays fully open (today's behavior) until the first local
// or OIDC-provisioned user is created -- see Store.Count(), consulted
// by internal/api's auth middleware.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User is one local account. PasswordHash is never exposed outside this
// package/its JSON persistence -- internal/api must never serialize a
// User directly into an HTTP response.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	LastLogin    time.Time `json:"lastLogin,omitzero"`
	// PasswordChangedAt lets a session be invalidated by a password
	// reset that happens in a *different process* -- the CLI recovery
	// tool (`-reset-password`) has no access to the running server's
	// in-memory SessionStore (see auth.SessionStore.RevokeAllForUser,
	// which only helps a same-process caller, e.g. a future in-app
	// change-password flow). Comparing a session's IssuedAt against this
	// field works across that boundary since both are read from/written
	// to the same persisted store.
	PasswordChangedAt time.Time `json:"passwordChangedAt,omitzero"`
	// OIDCIssuer/OIDCSubject identify this account's linked SSO identity,
	// if any -- both empty for a purely local-password account. Together
	// they're the immutable identity key FindOrCreateOIDCUser matches
	// on, deliberately never email or username (see that method's doc
	// comment for why). A local-password account can also carry these
	// (see LinkOIDCIdentity) once linked to an SSO identity, in which
	// case both login paths reach the same account.
	OIDCIssuer  string `json:"oidcIssuer,omitempty"`
	OIDCSubject string `json:"oidcSubject,omitempty"`
}

// oidcKey is (issuer, subject) as a map key -- a struct rather than a
// delimited string concatenation, so there's no theoretical risk of one
// issuer/subject pair's serialized form colliding with a different
// pair's.
type oidcKey struct {
	issuer  string
	subject string
}

var (
	// ErrNotPersisted is returned by Register/CreateUser when no
	// storePath is configured -- refusing rather than silently creating
	// a user that would vanish on restart (either locking everyone out,
	// or worse, silently reverting to the open, unauthenticated state).
	ErrNotPersisted = errors.New("auth: storePath is not configured, refusing to create a user that would not survive a restart")
	// ErrUsernameTaken is returned by Register/CreateUser for a
	// already-registered username (case-insensitive).
	ErrUsernameTaken = errors.New("auth: username already exists")
	// ErrRegistrationClosed is returned by Register once at least one
	// user already exists -- self-registration is a one-time, first-
	// account-only path (see Store.Count()); every user after that is
	// created by an existing admin via CreateUser.
	ErrRegistrationClosed = errors.New("auth: registration is closed, an account already exists")
	// ErrInvalidCredentials is returned by Authenticate for either an
	// unknown username or a wrong password -- deliberately not
	// distinguished, so a caller can't be tricked into leaking which
	// usernames exist via the error alone (VerifyPassword's constant-
	// time dummy-hash comparison handles the timing side of the same
	// concern).
	ErrInvalidCredentials = errors.New("auth: invalid username or password")
	// ErrUserNotFound is returned by SetPassword/Get for an unknown user
	// ID/username -- used by the CLI recovery tool, where "no such user"
	// is a legitimate, expected outcome worth distinguishing (unlike
	// Authenticate, this isn't a login attempt an attacker controls).
	ErrUserNotFound = errors.New("auth: no such user")
	// ErrAuthDisabled is returned by Register once this deployment has
	// explicitly opted out of authentication (see Disable) -- refusing
	// registration here, not just at the API-routing layer, is what
	// makes EnableSetup's CLI-only re-arming actually load-bearing:
	// without this check, a client could bypass the UI entirely and
	// POST straight to /api/auth/register (still reachable while
	// disabled -- see internal/api's requireAuth) to unilaterally
	// re-impose auth for everyone, exactly what EnableSetup's doc
	// comment says this design prevents.
	ErrAuthDisabled = errors.New("auth: this deployment has disabled authentication -- run -enable-auth-setup to allow creating an account again")
	// ErrOIDCIdentityTaken is returned by LinkOIDCIdentity when the
	// (issuer, subject) pair is already linked to a *different* user --
	// an OIDC identity can back at most one local account.
	ErrOIDCIdentityTaken = errors.New("auth: this SSO identity is already linked to a different account")
	// ErrPasswordTooShort is returned by createLocked/SetPassword for a
	// password under minPasswordLength -- LoginLimiter meaningfully
	// slows brute-forcing a weak password but doesn't prevent it, so
	// this is the actual floor. Deliberately not checked inside
	// HashPassword itself: that function also hashes two non-user-
	// chosen values (dummyHash's fixed timing-comparison string, and
	// FindOrCreateOIDCUser's random unmatchable-hash input for SSO-only
	// accounts), neither of which should be subject to a password
	// policy at all.
	ErrPasswordTooShort = fmt.Errorf("auth: password must be at least %d characters", minPasswordLength)
)

// minPasswordLength is enforced at every path that sets a user-chosen
// password (createLocked, SetPassword) -- self-registration, admin-
// created accounts, and the CLI reset-password tool all funnel through
// one of those two, so there's exactly one place this needs to live.
const minPasswordLength = 8

// storeFile is the on-disk shape -- an object wrapping the user list
// plus the Disabled marker (see Store.Disable), rather than a bare
// array. storeFile.UnmarshalJSON below stays compatible with a
// pre-Disabled-state file (a bare `[]User` array, written by every
// mikroview version before this one) so an existing deployment's
// accounts still load correctly, treated as Disabled: false.
type storeFile struct {
	Disabled bool    `json:"disabled"`
	Users    []*User `json:"users"`
}

func (f *storeFile) UnmarshalJSON(data []byte) error {
	type shape storeFile // avoids infinite recursion into this method
	var s shape
	if err := json.Unmarshal(data, &s); err == nil {
		*f = storeFile(s)
		return nil
	}
	// A top-level JSON array can't unmarshal into a struct -- that's
	// exactly the pre-Disabled-state legacy shape, so this is where a
	// genuinely malformed file also gets one more (correct) chance to
	// report its real error, not this fallback's.
	var legacy []*User
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	f.Users = legacy
	f.Disabled = false
	return nil
}

// Store persists user accounts to a JSON file. Unlike internal/flags'
// Store, persistence is not optional: an empty path leaves Store usable
// (so mikroview still boots fine with auth unconfigured) but Register/
// CreateUser refuse to add a user in that state -- see ErrNotPersisted.
type Store struct {
	mu        sync.RWMutex
	path      string
	byID      map[string]*User
	byName    map[string]string  // lowercased username -> ID
	oidcIndex map[oidcKey]string // (issuer, subject) -> ID, see ByOIDCIdentity
	// disabled records a deliberate, permanent opt-out of authentication
	// for this deployment -- see Disabled/Disable/EnableSetup. Distinct
	// from len(byID)==0, which just means "no account yet, decision
	// still pending" (see internal/api's requireAuth for how the two
	// states are gated differently).
	disabled bool
	// mtime tracks the store file's modification time as of the last
	// load, so a running server can pick up a change made by a separate
	// process -- namely the CLI recovery tools (`-reset-password`,
	// `-enable-auth-setup`), which each open their own independent
	// Store and write to the same file. Without this, a password reset
	// (or re-arming the setup flow) would silently have no effect on an
	// already-running server until it restarts, defeating the point of
	// a recovery tool that shouldn't require one.
	mtime time.Time
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, byID: make(map[string]*User), byName: make(map[string]string), oidcIndex: make(map[oidcKey]string)}
	if path == "" {
		return s, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return s, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return s, err
	}
	for _, u := range file.Users {
		// A JSON array containing `null` unmarshals successfully into a
		// nil *User -- valid JSON, so the err check above doesn't catch
		// it. Skipping it here is what actually delivers this store's
		// "a corrupted file shouldn't block startup" intent (see
		// SECURITY.md); relying on the unmarshal error alone doesn't
		// cover every way a file can be malformed.
		if u == nil {
			continue
		}
		s.byID[u.ID] = u
		s.byName[strings.ToLower(u.Username)] = u.ID
		if u.OIDCIssuer != "" {
			s.oidcIndex[oidcKey{issuer: u.OIDCIssuer, subject: u.OIDCSubject}] = u.ID
		}
	}
	s.disabled = file.Disabled
	s.mtime = info.ModTime()
	return s, nil
}

// reloadIfStale re-reads the store file if its modification time has
// moved on since the last load -- see Store.mtime's doc comment. Called
// at the top of every read path (Authenticate, Get, ByUsername, List);
// write paths (Register/CreateUser/SetPassword) don't need it since they
// persist their own change immediately after.
func (s *Store) reloadIfStale() {
	if s.path == "" {
		return
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return // missing/unreadable -- keep serving whatever's already in memory
	}

	s.mu.RLock()
	stale := info.ModTime().After(s.mtime)
	s.mu.RUnlock()
	if !stale {
		return
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-check under the write lock -- another goroutine may have
	// already reloaded (or this store's own persistLocked may have run)
	// while this call was reading the file without holding the lock.
	if !info.ModTime().After(s.mtime) {
		return
	}
	byID := make(map[string]*User, len(file.Users))
	byName := make(map[string]string, len(file.Users))
	oidcIndex := make(map[oidcKey]string, len(file.Users))
	for _, u := range file.Users {
		if u == nil { // see Open's identical guard for why this is needed
			continue
		}
		byID[u.ID] = u
		byName[strings.ToLower(u.Username)] = u.ID
		if u.OIDCIssuer != "" {
			oidcIndex[oidcKey{issuer: u.OIDCIssuer, subject: u.OIDCSubject}] = u.ID
		}
	}
	s.byID = byID
	s.byName = byName
	s.oidcIndex = oidcIndex
	s.disabled = file.Disabled
	s.mtime = info.ModTime()
}

// Persisted reports whether this Store can actually survive a restart.
func (s *Store) Persisted() bool {
	return s.path != ""
}

// Count returns the number of user accounts. 0 is what gates both
// whether auth is active at all, and whether Register is still open.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}

// Disabled reports whether this deployment has explicitly opted out of
// authentication (see Disable) -- distinct from Count()==0, which just
// means "no account yet, decision still pending." Reloads first (see
// reloadIfStale) so a live server picks up a change made by the CLI
// recovery tool (`-enable-auth-setup`) without needing a restart, same
// as Authenticate/Get/List already do for password resets.
func (s *Store) Disabled() bool {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disabled
}

// Disable permanently opts this deployment out of authentication --
// only callable while Count()==0: disabling auth out from under
// existing accounts isn't this method's job, and is never exposed
// anywhere (internal/api's handleAuthSkip is the only caller, itself
// only reachable during the pre-decision bootstrap window). Persists
// immediately.
func (s *Store) Disable() error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	// Same cross-process reload Register does, for the same reason --
	// the len(s.byID) test below is the correctness boundary (it runs
	// under the write lock), this just makes sure an account created by
	// another process is visible before we get there.
	s.reloadIfStale()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byID) > 0 {
		return ErrRegistrationClosed
	}
	s.disabled = true
	s.persistLocked()
	return nil
}

// EnableSetup clears a prior Disable, re-arming the web setup form so
// /api/auth/register (and the choice screen in front of it) becomes
// reachable again. Deliberately not exposed via any API endpoint a
// browser could reach -- only internal/main.go's `-enable-auth-setup`
// CLI mode calls this, so a UI visitor can never re-impose auth for
// everyone else without host/container access (the same trust anchor
// `-reset-password`/`-list-users` already rely on).
func (s *Store) EnableSetup() error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = false
	s.persistLocked()
	return nil
}

// Register creates the very first account, always as RoleAdmin,
// regardless of what a future caller might pass -- there is no role
// parameter because there's no meaningful choice: the first person to
// register is the super-admin by definition (see the local-auth design).
// Fails with ErrRegistrationClosed once any account exists.
//
// The "is registration still open" test is passed down as a guard and
// evaluated inside createLocked's critical section rather than checked
// here, because checking it here would be a TOCTOU: Count()/Disabled()
// each take and release the lock on their own, so two concurrent
// Register calls could both observe an empty store and both go on to
// insert. That window is not theoretical or narrow -- HashPassword
// below runs before the lock is taken and deliberately costs ~100ms
// (Argon2id), holding it open for the entire hash. Left unguarded, N
// concurrent registrations during the first-run window all succeed and
// every one of them gets RoleAdmin.
func (s *Store) Register(username, password string, now time.Time) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	// Reload first so a decision made by another process (the CLI
	// recovery tool) is visible before we take the lock -- the guard
	// below re-reads the same fields under it, so this is an
	// optimization for the cross-process case, not the correctness
	// boundary.
	s.reloadIfStale()
	return s.createLocked(username, password, RoleAdmin, now, registrationOpenGuard)
}

// registrationOpenGuard is Register's under-the-lock precondition: no
// account may exist yet, and the deployment must not have already
// opted out of auth entirely. Both are re-read from the live store
// with the write lock held (see createLocked), which is what makes
// "exactly one account can ever be self-registered" actually hold
// under concurrency.
//
// Checking disabled here (not just in Register) also closes a second,
// worse race: Disable() and Register() could previously both succeed,
// leaving a store with a real admin account AND disabled == true.
// internal/api's requireAuth checks Disabled() first, so that state
// means the deployment serves everyone with no login at all while an
// admin account quietly exists -- the operator sees their own
// registration succeed and has no reason to suspect auth is off.
func registrationOpenGuard(s *Store) error {
	if s.disabled {
		return ErrAuthDisabled
	}
	if len(s.byID) > 0 {
		return ErrRegistrationClosed
	}
	return nil
}

// CreateUser adds an additional account with the given role -- for use
// by an already-authenticated admin (internal/api enforces the caller's
// role; Store itself has no notion of "who is calling"), or by the CLI
// recovery tooling. No guard: unlike Register, this is deliberately
// callable at any time, and its "who may call this" question is
// answered a layer up.
func (s *Store) CreateUser(username, password string, role Role, now time.Time) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	return s.createLocked(username, password, role, now, nil)
}

// createLocked inserts a new account. guard, when non-nil, is evaluated
// with the write lock already held and aborts the insert if it returns
// an error -- that's the hook callers use to make a precondition
// ("registration is still open") atomic with the insert itself rather
// than checking it beforehand and racing.
//
// HashPassword deliberately runs before the lock is acquired: Argon2id
// is ~100ms by design, and holding the store's write lock for that long
// would serialize every reader behind each in-flight registration --
// an easy self-inflicted DoS. The cost of hashing before the guard runs
// is one wasted hash on the losing side of a race, which is the right
// trade.
func (s *Store) createLocked(username, password string, role Role, now time.Time, guard func(*Store) error) (*User, error) {
	if len(password) < minPasswordLength {
		return nil, ErrPasswordTooShort
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if guard != nil {
		if err := guard(s); err != nil {
			return nil, err
		}
	}

	key := strings.ToLower(username)
	if _, exists := s.byName[key]; exists {
		return nil, ErrUsernameTaken
	}

	u := &User{
		ID:           newID(),
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
	}
	s.byID[u.ID] = u
	s.byName[key] = u.ID
	s.persistLocked()

	cp := *u
	return &cp, nil
}

// ByOIDCIdentity looks up the user linked to the given (issuer,
// subject) pair, if any.
func (s *Store) ByOIDCIdentity(issuer, subject string) (*User, bool) {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[s.oidcIndex[oidcKey{issuer: issuer, subject: subject}]]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

// FindOrCreateOIDCUser looks up the user for (issuer, subject), or
// just-in-time provisions one if this identity has never signed in
// before -- the reported bool is true exactly when a new account was
// created. Unlike Register, this is never gated by Count() > 0:
// Register's one-time-only rule exists to close the *self-service local
// registration form* after the first account, but has no bearing on
// admin-driven creation (CreateUser) or, here, an identity provider
// vouching for someone -- every never-before-seen (issuer, subject)
// pair is provisioned regardless of how many accounts already exist,
// which is what "no pre-registration required" (issue #43) means.
//
// usernameHint (typically the ID token's preferred_username or email
// claim) is used as the new account's Username only if it's non-empty
// and not already taken by a *different* user -- it is a display
// convenience only, never part of the identity key, and this method
// never attaches a login to an existing account merely because it
// shares that hint: an IdP-side email/username reassignment must never
// silently inherit a pre-existing mikroview account. On any collision
// (or an empty hint) a deterministic synthetic username is used
// instead, derived from (issuer, subject) so a retried provisioning
// attempt (e.g. a network blip between JIT-create and the caller
// creating a session) lands on the same account rather than racing
// itself.
//
// The very first user -- local or OIDC, whichever happens first --
// becomes RoleAdmin, the same rule Register already applies; every
// later account (from either path) is RoleUser, decided under this
// method's own write lock rather than a separate Count() pre-check, so
// this doesn't add a second copy of the (pre-existing, unrelated to
// this issue) narrow TOCTOU window Register's own pre-lock Count()
// check already has.
func (s *Store) FindOrCreateOIDCUser(issuer, subject, usernameHint string, now time.Time) (user *User, created bool, err error) {
	if !s.Persisted() {
		return nil, false, ErrNotPersisted
	}
	if s.Disabled() {
		return nil, false, ErrAuthDisabled
	}

	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	key := oidcKey{issuer: issuer, subject: subject}
	if id, ok := s.oidcIndex[key]; ok {
		if u, ok := s.byID[id]; ok {
			u.LastLogin = now
			s.persistLocked()
			cp := *u
			return &cp, false, nil
		}
	}

	// A real, freshly generated, unmatchable Argon2id hash -- not "" --
	// so a local-login attempt against this username takes the same
	// time as a genuine wrong-password attempt. VerifyPassword's
	// malformed-hash guard returns false before ever running Argon2id
	// for an empty/malformed hash, which would otherwise let an
	// attacker distinguish "this username is SSO-only" from the
	// response time alone.
	unmatchable, err := HashPassword(newID())
	if err != nil {
		return nil, false, err
	}

	role := RoleUser
	if len(s.byID) == 0 {
		role = RoleAdmin
	}

	u := &User{
		ID:           newID(),
		Username:     s.uniqueUsernameLocked(usernameHint, issuer, subject),
		PasswordHash: unmatchable,
		Role:         role,
		CreatedAt:    now,
		LastLogin:    now,
		OIDCIssuer:   issuer,
		OIDCSubject:  subject,
	}
	s.byID[u.ID] = u
	s.byName[strings.ToLower(u.Username)] = u.ID
	s.oidcIndex[key] = u.ID
	s.persistLocked()

	cp := *u
	return &cp, true, nil
}

// uniqueUsernameLocked picks hint if it's non-empty and not already
// taken, otherwise a deterministic synthetic username derived from
// (issuer, subject) -- see FindOrCreateOIDCUser's doc comment. Callers
// must hold s.mu.
func (s *Store) uniqueUsernameLocked(hint, issuer, subject string) string {
	hint = strings.TrimSpace(hint)
	if hint != "" {
		if _, taken := s.byName[strings.ToLower(hint)]; !taken {
			return hint
		}
	}
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	full := hex.EncodeToString(sum[:])
	// Grows the slice of the hash used until a free username is found --
	// deterministic and idempotent for the same (issuer, subject) across
	// retries, since it always starts from the same hash. A collision at
	// the shortest length is exceptionally unlikely on its own; growing
	// further makes it vanishingly so without ever depending on
	// randomness for reproducibility.
	for n := 8; n <= len(full); n += 8 {
		candidate := "oidc-" + full[:n]
		if _, taken := s.byName[strings.ToLower(candidate)]; !taken {
			return candidate
		}
	}
	return "oidc-" + newID() // practically unreachable
}

// LinkOIDCIdentity attaches (issuer, subject) to an existing user by
// ID -- the low-level primitive a future "connect SSO to my account"
// endpoint would use (an authenticated local user proving they also
// control an OIDC identity), not itself exposed via any API in issue
// #43. Idempotent for the same user; fails with ErrOIDCIdentityTaken if
// that identity is already linked to a *different* user.
func (s *Store) LinkOIDCIdentity(userID, issuer, subject string, now time.Time) error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}

	key := oidcKey{issuer: issuer, subject: subject}
	if existingID, ok := s.oidcIndex[key]; ok && existingID != userID {
		return ErrOIDCIdentityTaken
	}

	u.OIDCIssuer = issuer
	u.OIDCSubject = subject
	s.oidcIndex[key] = userID
	s.persistLocked()
	return nil
}

// Authenticate verifies username/password and, on success, records
// LastLogin and returns a copy of the user. Always runs a password
// comparison (against dummyHash if the username doesn't exist) so a
// failed login takes the same time either way. The CPU-heavy Argon2id
// comparison deliberately happens with the lock released -- only the
// map/field reads and writes around it are synchronized.
func (s *Store) Authenticate(username, password string, now time.Time) (*User, error) {
	s.reloadIfStale()

	s.mu.RLock()
	id, known := s.byName[strings.ToLower(username)]
	hash := dummyHash
	if known {
		hash = s.byID[id].PasswordHash
	}
	s.mu.RUnlock()

	valid := VerifyPassword(password, hash)
	if !known || !valid {
		return nil, ErrInvalidCredentials
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-fetch by id rather than trusting a pointer captured above -- a
	// concurrent reloadIfStale (triggered by another in-flight request)
	// could have swapped s.byID/s.byName for entirely new maps in the
	// window since the RUnlock, which would otherwise leave this write
	// landing on an orphaned copy nothing else references.
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrInvalidCredentials
	}
	u.LastLogin = now
	s.persistLocked()
	cp := *u
	return &cp, nil
}

// Get returns a copy of the user with the given ID -- used to resolve a
// session's UserID back to a user on every authenticated request.
func (s *Store) Get(id string) (*User, bool) {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

// ByUsername looks up a user by username (case-insensitive) -- used by
// the CLI recovery tooling to confirm an account exists before
// prompting for a new password.
func (s *Store) ByUsername(username string) (*User, bool) {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[s.byName[strings.ToLower(username)]]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

// SetPassword replaces username's password hash -- the CLI recovery
// path (`mikroview -reset-password`), which needs no current password
// since container/host access is the trust anchor for that tool. Also
// records PasswordChangedAt, which is what actually invalidates any
// session issued before this reset (see User.PasswordChangedAt) -- the
// CLI tool runs in a different process from the live server, so it has
// no way to reach into that server's in-memory SessionStore directly.
func (s *Store) SetPassword(username, newPassword string, now time.Time) error {
	if len(newPassword) < minPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[s.byName[strings.ToLower(username)]]
	if !ok {
		return ErrUserNotFound
	}
	u.PasswordHash = hash
	u.PasswordChangedAt = now
	s.persistLocked()
	return nil
}

// List returns every user (without password hashes), sorted by
// username -- used by the CLI recovery tool (`-list-users`) and the
// admin-facing user list.
func (s *Store) List() []User {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.byID))
	for _, u := range s.byID {
		cp := *u
		cp.PasswordHash = ""
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

func (s *Store) persistLocked() {
	if s.path == "" {
		return
	}
	list := make([]*User, 0, len(s.byID))
	for _, u := range s.byID {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })

	data, err := json.MarshalIndent(storeFile{Disabled: s.disabled, Users: list}, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, s.path) // same filesystem, so this is atomic

	// Keep mtime in sync with this store's own write, so reloadIfStale
	// doesn't immediately re-read the file it just wrote on the next
	// call -- harmless if it did (idempotent), but wasted work.
	if info, err := os.Stat(s.path); err == nil {
		s.mtime = info.ModTime()
	}
}
