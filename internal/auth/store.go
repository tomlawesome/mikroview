// SPDX-License-Identifier: AGPL-3.0-only

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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("auth")

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
	// RoleViewer is the third, lowest tier (#653): read access to
	// everything an operator sees, but nothing that changes the
	// instance. Stacks below RoleUser, which stacks below RoleAdmin --
	// see Role.AtLeast.
	RoleViewer Role = "viewer"
)

// rank orders roles from lowest privilege to highest, backing
// Role.AtLeast. An unrecognized Role -- including the zero value -- ranks
// below RoleViewer, so it is refused everything AtLeast ever gates for a
// legitimate min. Every path inside this package produces one of the
// three named constants, so the only way one reaches a live User.Role is
// a document written outside this package -- a hand-edited accounts
// file. That account then fails closed, denied by every role gate, which
// is the right direction for a role nobody legitimately assigned.
func (r Role) rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleUser:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// AtLeast reports whether r's tier is at or above min, per mikroview's
// stacked access tiers: admin ⊇ user ⊇ viewer. internal/api's gates are
// all built on this rather than comparing Role values directly, so
// there is exactly one place "who outranks whom" is decided.
func (r Role) AtLeast(min Role) bool {
	return r.rank() >= min.rank()
}

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
	// tool (`-recover-admin-account`) has no access to the running server's
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
	// HasLocalPassword distinguishes a real, user-chosen password from
	// the random unmatchable hash FindOrCreateOIDCUser issues to an
	// SSO-provisioned account. The hashes themselves cannot be told
	// apart -- both are valid Argon2id strings -- so this has to be
	// recorded rather than inferred from the credential.
	//
	// It matters because letting an admin set a *real* password on an
	// SSO-only account would quietly reopen the local-attack surface
	// that provisioning it via OIDC deliberately closed.
	HasLocalPassword bool `json:"hasLocalPassword"`
	// RoleChangedAt records the last admin transfer touching this
	// account, on both sides of it. For the audit trail and the UI only:
	// authorization always reads Role, never this.
	RoleChangedAt time.Time `json:"roleChangedAt,omitzero"`
}

// LocalPassword reports whether this account has a real, user-chosen
// password that may be reset.
func (u *User) LocalPassword() bool { return u.HasLocalPassword }

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
	// ErrSingleAdmin is returned by CreateUser for a RoleAdmin request.
	// mikroview holds exactly one admin; handover is TransferAdmin, not
	// creating a second one.
	ErrSingleAdmin = errors.New("auth: mikroview has a single admin account -- transfer the role instead of creating another admin")
	// ErrInvalidRole is returned by CreateUser for any role other than
	// RoleUser or RoleViewer. RoleAdmin is refused separately, as
	// ErrSingleAdmin above -- that failure means something different to a
	// caller (a deployment invariant) than an unrecognized value does.
	ErrInvalidRole = errors.New(`auth: role must be "user" or "viewer"`)
	// ErrCannotDeleteAdmin is returned by DeleteUser for the admin
	// account. Transfer the role first if the intent is to remove the
	// person currently holding it.
	ErrCannotDeleteAdmin = errors.New("auth: the admin account cannot be deleted -- transfer the admin role first")
	// ErrTransferToSelf is returned by TransferAdmin when the target is
	// already the admin.
	ErrTransferToSelf = errors.New("auth: that account is already the admin")
	// ErrNoAdmin is returned by TransferAdmin when no account holds the
	// role -- nothing to transfer.
	ErrNoAdmin = errors.New("auth: this deployment has no admin account")
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
// created accounts, and the CLI admin-recovery tool all funnel through
// one of those two, so there's exactly one place this needs to live.
const minPasswordLength = 8

// storeFile is the on-disk shape: an object wrapping the user list.
type storeFile struct {
	Users []*User `json:"users"`
}

// Store persists user accounts through a persist.Backend -- a JSON file
// by default, or Postgres when configured (issue #131). Unlike
// internal/flags' Store, persistence is not optional: a nil backend
// leaves Store usable (so mikroview still boots fine with auth
// unconfigured) but Register/CreateUser refuse to add a user in that
// state -- see ErrNotPersisted.
type Store struct {
	mu        sync.RWMutex
	backend   persist.Backend
	byID      map[string]*User
	byName    map[string]string  // lowercased username -> ID
	oidcIndex map[oidcKey]string // (issuer, subject) -> ID, see ByOIDCIdentity
	// version is the backend's token for the document as of the last
	// load, so a running server can pick up a change made by a separate
	// process -- namely the CLI recovery tools (`-recover-admin-account`,
	// `-enable-auth-setup`), which each open their own independent
	// Store against the same backend. Without this, a password reset
	// (or re-arming the setup flow) would silently have no effect on an
	// already-running server until it restarts, defeating the point of
	// a recovery tool that shouldn't require one.
	//
	// It is also what makes a write conditional: see persistLocked.
	version int64

	// reloadInFlight is non-nil while one caller is checking the backend
	// for staleness, and is closed when that check finishes. It is
	// deliberately not guarded by mu: a caller waiting on it must not
	// hold a lock the reload itself needs to take. See reloadIfStale.
	reloadMu       sync.Mutex
	reloadInFlight chan struct{}
}

// reloadTimeout bounds one staleness check against the backend.
//
// Five seconds matches the persistTimeout the ingest-path stores use
// (internal/rules, internal/flags, internal/device, internal/matchlog):
// long enough that an ordinary slow query is not mistaken for an outage,
// short enough that a stalled backend does not hold a request goroutine
// and a pool connection indefinitely. Exceeding it is not fatal -- the
// store keeps serving what it already has in memory.
//
// A var, not a const, only so the stall tests can shorten it -- same
// reason internal/store's maxScannedPerQuery is one. Nothing outside
// tests assigns to it.
var reloadTimeout = 5 * time.Second

// Open returns a Store persisting to a JSON file at path. An empty path
// gives a usable but unpersisted store -- see Store's doc comment.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- the entry point
// main.go uses when Postgres is configured (issue #131). A nil backend
// is the unpersisted case.
//
// A backend that exists but cannot be read, or holds a document that
// cannot be parsed, is a hard error (issue #378): OpenWithBackend
// returns (nil, err) rather than a store whose live backend would
// overwrite that document on the first write. That distinction is
// load-bearing: treating an unreadable accounts store as an absent one
// turns a corrupted file into a fresh install, silently reopening
// registration to whoever loads the page next. main.go refuses to
// start on it. See persist.Open.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{
		backend:   b,
		byID:      make(map[string]*User),
		byName:    make(map[string]string),
		oidcIndex: make(map[oidcKey]string),
	}

	version, existed, err := persist.Open(context.Background(), b, "the accounts store", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		// version isn't in scope yet here -- persist.Open hasn't
		// returned it to this statement's left-hand side. applyLoaded
		// is called with a placeholder and corrected below once
		// persist.Open's real version is available.
		s.applyLoaded(file, 0)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if existed {
		s.version = version
	}
	return s, nil
}

// applyLoaded replaces the in-memory index from a decoded document.
// Shared by OpenWithBackend and reloadIfStale so the two can't diverge
// on what loading means -- the migration below in particular must run on
// both paths.
func (s *Store) applyLoaded(file storeFile, version int64) {
	s.byID = make(map[string]*User, len(file.Users))
	s.byName = make(map[string]string, len(file.Users))
	s.oidcIndex = make(map[oidcKey]string, len(file.Users))
	for _, u := range file.Users {
		// A JSON array containing `null` unmarshals successfully into a
		// nil *User -- valid JSON, so the error check above doesn't
		// catch it.
		if u == nil {
			continue
		}
		s.byID[u.ID] = u
		s.byName[strings.ToLower(u.Username)] = u.ID
		if u.OIDCIssuer != "" || u.OIDCSubject != "" {
			s.oidcIndex[oidcKey{issuer: u.OIDCIssuer, subject: u.OIDCSubject}] = u.ID
		}
	}
	s.version = version
}

// reloadIfStale re-reads the document if the backend has moved on since
// this Store last loaded it.
//
// This is what lets a running server pick up a change made by a separate
// process -- the CLI recovery commands each open their own Store against
// the same backend. Without it, a password reset would silently have no
// effect on a live server until restart, defeating the point of a
// recovery tool that shouldn't require one.
//
// Every failure here is deliberately silent and non-fatal: it keeps
// serving whatever is already in memory. A transient backend problem
// must not take authentication down on a server that is running fine.
//
// Three things bound what a sick backend can cost, because this runs on
// every authenticated request (requireAuth -> sessionUser -> Get) and on
// every login and registration attempt, including unauthenticated ones:
//
//   - The read has a deadline. It used to pass context.Background(), and
//     a Postgres server that stops answering while its TCP connection
//     stays ESTABLISHED (a blackhole, a long lock wait, an overloaded
//     server) blocks pgx forever. http.Server.WriteTimeout does not
//     rescue this -- it tears down the client connection and leaves the
//     handler goroutine blocked, measured at +125 retained goroutines
//     for 25 concurrent requests. Every request therefore leaked a
//     goroutine and held a pool connection until the database returned,
//     including the login request an operator needs to diagnose it.
//
//   - Only one check is ever in flight. Concurrent callers join the
//     running one instead of opening their own, so a stall costs one
//     pooled connection rather than one per request -- the pool cannot
//     be exhausted by request volume.
//
//   - The staleness question is asked with Version when the backend can
//     answer it cheaply, so a healthy Postgres deployment no longer
//     ships the whole accounts document per request. Backends without
//     that capability (the file backend, whose version is a hash of its
//     own bytes) fall back to Load exactly as before.
//
// What deliberately did *not* change: staleness is still checked on
// every call rather than on a timer. A time-based cache would be a
// smaller change and would break the guarantee this exists for -- that
// a CLI recovery command's password reset takes effect on the running
// server immediately, not up to a cache interval later.
func (s *Store) reloadIfStale() {
	if s.backend == nil {
		return
	}

	// Join a check already in flight rather than starting a second one.
	s.reloadMu.Lock()
	if inFlight := s.reloadInFlight; inFlight != nil {
		s.reloadMu.Unlock()
		<-inFlight
		return
	}
	done := make(chan struct{})
	s.reloadInFlight = done
	s.reloadMu.Unlock()
	defer func() {
		s.reloadMu.Lock()
		s.reloadInFlight = nil
		s.reloadMu.Unlock()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
	defer cancel()

	if vr, ok := s.backend.(persist.VersionReader); ok {
		version, exists, err := vr.Version(ctx)
		if err != nil || !exists {
			return
		}
		s.mu.RLock()
		stale := version != s.version
		s.mu.RUnlock()
		if !stale {
			return
		}
	}

	snap, err := s.backend.Load(ctx)
	if err != nil || !snap.Exists {
		return
	}

	s.mu.RLock()
	stale := snap.Version != s.version
	s.mu.RUnlock()
	if !stale {
		return
	}

	var file storeFile
	if err := json.Unmarshal(snap.Payload, &file); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-checked under the write lock: another goroutine may have
	// reloaded (or this store's own persistLocked may have run) while
	// this call was reading without holding it.
	if snap.Version == s.version {
		return
	}
	s.applyLoaded(file, snap.Version)
}

func (s *Store) Persisted() bool {
	return s.backend != nil
}

// Count returns the number of user accounts. 0 is what gates both
// whether auth is active at all, and whether Register is still open.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
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

	// Cheap rejection BEFORE hashing. HashPassword is Argon2id at 64
	// MiB, and /api/auth/register is unauthenticated and rate-limit-free
	// in every auth state -- so without this, a ~60-byte POST that is
	// going to be refused anyway still costs 64 MiB and ~66ms, and a
	// handful of concurrent ones OOM-kill the container. Measured at
	// ~1 GiB peak heap for 16 concurrent requests, all of which
	// correctly returned "registration closed".
	//
	// This is a fast path, NOT the correctness boundary: it reads the
	// guard's fields without holding the write lock, so it can race.
	// registrationOpenGuard re-checks under the lock inside
	// createLocked, and that remains what actually guarantees exactly
	// one account can be self-registered.
	if err := func() error {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return registrationOpenGuard(s)
	}(); err != nil {
		return nil, err
	}

	return s.createLocked(username, password, RoleAdmin, now, registrationOpenGuard)
}

// registrationOpenGuard is Register's under-the-lock precondition: no
// account may exist yet, and the deployment must not have already
// opted out of auth entirely. Both are re-read from the live store
// with the write lock held (see createLocked), which is what makes
// "exactly one account can ever be self-registered" actually hold
// under concurrency.
func registrationOpenGuard(s *Store) error {
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
//
// role must be RoleUser or RoleViewer (#653). Anything else is refused:
// RoleAdmin specifically as ErrSingleAdmin (see below), any other value
// as ErrInvalidRole -- neither is silently coerced to a lesser role,
// since that would create an account under the name the caller chose
// with a privilege they did not ask for.
func (s *Store) CreateUser(username, password string, role Role, now time.Time) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	// mikroview holds exactly one admin at a time. Refused in the store
	// rather than only at the API layer so the CLI and any future caller
	// inherit the invariant instead of each remembering it.
	if role == RoleAdmin {
		return nil, ErrSingleAdmin
	}
	if role != RoleUser && role != RoleViewer {
		return nil, ErrInvalidRole
	}
	return s.createLocked(username, password, role, now, nil)
}

// DeleteUser removes an account by ID and returns it, so the caller can
// clean up what belonged to it (sessions, API tokens).
//
// It refuses to delete the admin. mikroview holds exactly one admin, and
// a deployment with none has no way to add accounts, manage tokens, or
// reach any admin-gated screen -- recoverable only from the CLI, which
// is a worse position than whatever prompted the deletion. Enforced here
// rather than only at the API layer so every caller inherits it: with a
// single admin, "don't delete the admin" and "don't delete yourself" are
// the same rule, and this is the one place that stays true if that ever
// changes.
func (s *Store) DeleteUser(id string) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	if u.Role == RoleAdmin {
		return nil, ErrCannotDeleteAdmin
	}

	delete(s.byID, id)
	delete(s.byName, strings.ToLower(u.Username))
	if u.OIDCIssuer != "" {
		delete(s.oidcIndex, oidcKey{issuer: u.OIDCIssuer, subject: u.OIDCSubject})
	}
	s.persistLocked()

	cp := *u
	cp.PasswordHash = ""
	return &cp, nil
}

// TransferAdmin moves the admin role to toUsername, atomically.
//
// This is the only way to change who administers mikroview. There is
// deliberately no separate promote or demote: either alone would leave
// the deployment with two admins or none, and the rest of the system
// assumes neither can happen.
//
// It is reachable only from the recovery-key-gated CLI, never from the
// API. If an authenticated admin could transfer the role, then anyone
// who reached that session -- a compromised IdP account, a stolen
// cookie -- could grant themselves durable ownership and demote the real
// admin out of their own deployment. Requiring host access plus a
// recovery key means an identity-provider compromise buys the ability to
// log in, and nothing more.
//
// The whole operation runs under one write lock with the invariant
// re-checked inside it; doing it as two calls, or checking the current
// admin beforehand, is the check-then-act race behind the Appsmith
// duplicate-admin and open-webui zero-admin bugs.
func (s *Store) TransferAdmin(toUsername string, now time.Time) (from, to *User, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var current *User
	for _, u := range s.byID {
		if u.Role == RoleAdmin {
			current = u
			break
		}
	}
	if current == nil {
		return nil, nil, ErrNoAdmin
	}

	targetID, ok := s.byName[strings.ToLower(toUsername)]
	if !ok {
		return nil, nil, ErrUserNotFound
	}
	target := s.byID[targetID]
	if target.ID == current.ID {
		return nil, nil, ErrTransferToSelf
	}

	current.Role = RoleUser
	current.RoleChangedAt = now
	target.Role = RoleAdmin
	target.RoleChangedAt = now
	s.persistLocked()

	fromCopy, toCopy := *current, *target
	return &fromCopy, &toCopy, nil
}

// Admin returns the single admin account, or nil if there isn't one yet.
func (s *Store) Admin() *User {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.byID {
		if u.Role == RoleAdmin {
			cp := *u
			return &cp
		}
	}
	return nil
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
	// Validated here rather than in Register/CreateUser separately: this
	// is the single funnel every locally-created account passes through,
	// so nothing can be added later that skips it. (OIDC provisioning
	// does not come through here -- see sanitiseUsernameHint for why it
	// falls back instead of refusing.)
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
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
		// A real password the user chose, so it may later be reset.
		HasLocalPassword: true,
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

	unmatchable, err := unmatchablePasswordHash()
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
		// Explicitly false: the hash above is random and unmatchable, so
		// there is no password here to reset. Recorded rather than
		// inferred, because the hash itself is indistinguishable from a
		// real one.
		HasLocalPassword: false,
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
	// The hint is whatever the identity provider put in
	// preferred_username or email -- text mikroview does not control.
	// An unusable one is dropped, not rejected, so the person still gets
	// a stable account under the generated name below.
	hint = sanitiseUsernameHint(hint)
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

// unmatchablePasswordHash produces a real, freshly generated Argon2id
// hash of a random value -- the credential given to an account that has
// no local password.
//
// Not "": a local-login attempt against such an account has to take the
// same time as a genuine wrong-password attempt. VerifyPassword's
// malformed-hash guard returns false *before* running Argon2id for an
// empty or malformed hash, so storing "" would let an attacker tell
// "this username is SSO-only" from response time alone -- and knowing
// which accounts can't be attacked locally tells them which ones can.
//
// Shared by FindOrCreateOIDCUser (provisioned SSO-only from the start)
// and LinkOIDCIdentity (converted to SSO-only), so the two can't drift.
func unmatchablePasswordHash() (string, error) {
	return HashPassword(newID())
}

// LinkOIDCIdentity attaches (issuer, subject) to an existing account,
// converting it to SSO-only in the same operation.
//
// **Linking is destructive and one-way.** The account's local password
// is replaced with a fresh unmatchable hash and HasLocalPassword is set
// to false, exactly as if the account had been OIDC-provisioned from
// the start. There is deliberately no state where a local password and
// a linked identity both work: keeping the old password alive would
// preserve the weaker local-password attack surface on an account
// that has supposedly moved past it, which defeats the point of
// linking.
//
// That conversion lives here, inside the store, rather than in the API
// handler that calls it. A convention at the call site is one forgetful
// future caller away from a dual-mode account existing; an invariant
// here cannot be bypassed by adding a second caller.
//
// Idempotent for the same user. Fails with ErrOIDCIdentityTaken if that
// identity is already linked to a *different* account -- which is what
// stops someone attaching their own IdP identity to a colleague's
// account, and, on the admin account, stops it being quietly taken over.
func (s *Store) LinkOIDCIdentity(userID, issuer, subject string, now time.Time) error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	// Generated before the lock: HashPassword is ~100ms by design, and
	// holding the write lock across it would serialize every reader --
	// the same reasoning createLocked documents.
	unmatchable, err := unmatchablePasswordHash()
	if err != nil {
		return err
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
	u.PasswordHash = unmatchable
	u.HasLocalPassword = false
	// Invalidates every session issued before this point, including in
	// another process -- the account's credentials just changed
	// fundamentally, so anything holding a session from before that
	// should have to come back through the IdP.
	u.PasswordChangedAt = now
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
// path (`mikroview -recover-admin-account`), which needs no current password
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
	// An account that has a password has a local password, by
	// definition. Stated explicitly rather than left to be derived from
	// OIDCIssuer, so a linked account (OIDC *and* a local password)
	// isn't misread as SSO-only by the recovery tooling.
	u.HasLocalPassword = true
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
	if s.backend == nil {
		return
	}
	list := make([]*User, 0, len(s.byID))
	for _, u := range s.byID {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })

	data, err := json.MarshalIndent(storeFile{Users: list}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding accounts for persistence failed: %v -- "+
			"this change exists only in memory and will be lost on restart", err))
		return
	}

	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing accounts to %s failed: %v -- "+
			"this change exists only in memory and will be lost on restart",
			s.backend.Describe(), err))
		return
	}
	if conflicted {
		// Another process wrote while this change was pending -- almost
		// always a CLI recovery command against a live server. This
		// change went on top; a concurrent change to a *different*
		// account may have been lost. Said out loud rather than implied,
		// because a whole-document store cannot merge them.
		persistLog.Warn(fmt.Sprintf("accounts store was modified by another process while this change "+
			"was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}
