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
	// ResetCodeHash is the Argon2id hash of the one-time code an admin
	// issued for this account (#1251) -- the same hash function and
	// parameters a password gets, because for as long as it is live this
	// *is* the account's password. The code itself is never stored,
	// logged or audited anywhere: the single response to the admin's
	// reset request is the only place it exists in clear, which is why a
	// second click has to issue a new one rather than re-show the old.
	//
	// Empty whenever no reset is outstanding, and cleared again the
	// moment the code is spent (single use) or a new password is set.
	ResetCodeHash string `json:"resetCodeHash,omitempty"`
	// ResetCodeExpiresAt ends an unspent code, 24 hours after it was
	// issued (ResetCodeTTL). Checked against, never the only check --
	// see User.resetCodeLive.
	ResetCodeExpiresAt time.Time `json:"resetCodeExpiresAt,omitzero"`
	// MustChangePassword is set by an admin reset and cleared by
	// SetPassword. While it is true the account's session may reach
	// nothing but the change-password route (see internal/api's
	// requireAuth): the person signed in with a code somebody else
	// chose, so they are not yet holding a credential only they know.
	//
	// Deliberately recorded on the account rather than on the session:
	// the flag has to survive the login that redeems the code, outlive
	// a restart (sessions do not), and be cleared in exactly one place.
	MustChangePassword bool `json:"mustChangePassword,omitempty"`
	// TOTPSecret is the shared secret behind the authenticator-app second
	// factor (#1249), stored in the clear -- unlike a password or a
	// recovery code, it has to be reversible: verifying a 30-second code
	// means recomputing HMAC-SHA1 over it, not comparing a hash. It gets
	// no separate at-rest mechanism of its own (no second file, no
	// encryption layer): this store already persists the whole document
	// as one JSON file behind normal file permissions, and TOTPSecret is
	// just one more field in it, same as OIDCIssuer or Username.
	//
	// RFC 6238 code verification (reading this field, generating it,
	// confirming it) lives in totp.go, not here -- this package only
	// stores the secret and the replay counter below.
	//
	// A non-empty secret alone is not an active factor: see
	// TOTPConfirmedAt and HasActiveTOTP.
	TOTPSecret string `json:"totpSecret,omitempty"`
	// TOTPConfirmedAt is when the account owner proved they could produce
	// a valid code from TOTPSecret, which is what activates the factor.
	// Zero means a secret exists but was never confirmed -- e.g. a QR
	// code was shown mid-setup and the flow was abandoned -- and
	// HasActiveTOTP is false until this is set, so an abandoned setup
	// never gates a login the way a real second factor does.
	TOTPConfirmedAt time.Time `json:"totpConfirmedAt,omitzero"`
	// TOTPLastCounter is the RFC 6238 30-second time-step counter of the
	// most recently accepted code, so that code (or an earlier one still
	// inside the verification window) cannot be replayed. Unsigned, and
	// the same width VerifyTOTP takes and returns, so the value moves
	// between the verifier and this field without a conversion at each
	// call site -- a signed round trip is where a replay guard quietly
	// stops guarding. Advanced only through RecordTOTPCounter, which is
	// the same replay-guard role a reset code's single-use hash fills
	// for that credential.
	TOTPLastCounter uint64 `json:"totpLastCounter,omitzero"`
	// RecoveryCodes are the ten single-use fallback codes for signing in
	// without the authenticator app -- hashed with HashPassword, the same
	// Argon2id treatment a password gets, never stored in clear. See
	// GenerateRecoveryCodes and BurnRecoveryCode in recoverycodes.go.
	//
	// Shared between the authenticator app and passkeys below (#1250):
	// one set of ten covers whichever factors are active. See
	// HasSecondFactor and ClearTOTP's doc comment for the clearing rule,
	// and DeletePasskey/ClearPasskeys in passkeys.go for the same rule
	// from the passkey side.
	RecoveryCodes []RecoveryCode `json:"recoveryCodes,omitempty"`
	// Passkeys are this account's registered WebAuthn credentials
	// (#1250) -- zero or more, unlike TOTPSecret's single shared secret,
	// because an account may reasonably hold more than one authenticator
	// (a phone and a security key, say). See passkeys.go for the type
	// and every method that touches this field; this package does not
	// perform the WebAuthn ceremony itself, only stores what it produced
	// -- internal/api/webauthn.go (a parallel #1250 slice) owns that.
	Passkeys []Passkey `json:"passkeys,omitempty"`
}

// LocalPassword reports whether this account has a real, user-chosen
// password that may be reset.
func (u *User) LocalPassword() bool { return u.HasLocalPassword }

// HasActiveTOTP reports whether u's authenticator-app factor is
// confirmed and therefore active. A secret alone is not enough: a
// generated-but-never-confirmed secret (TOTPSecret set, TOTPConfirmedAt
// zero) is mid-setup, not something that should ever gate a sign-in --
// see TOTPConfirmedAt's doc comment.
func (u *User) HasActiveTOTP() bool {
	return u.TOTPSecret != "" && !u.TOTPConfirmedAt.IsZero()
}

// HasSecondFactor reports whether u has any active second factor at
// all -- authenticator app or at least one passkey. This is #1250's
// widening of the single-factor question #1249 only had to ask:
// everywhere that used to gate on HasActiveTOTP alone now gates on this
// instead (handleAuthLogin's login-requires-a-second-step check chief
// among them, internal/api, wave 2), and everywhere that decides
// whether recovery codes may still be cleared (ClearTOTP below,
// DeletePasskey and ClearPasskeys in passkeys.go) asks this rather than
// re-deriving "any factor left" from TOTP and Passkeys separately.
//
// Every passkey counts here regardless of whether it's stale (the
// design's "when publicUrl changes" section): staleness only affects
// whether a passkey can complete a *login*, not whether the account is
// considered to have a second factor at all. An account with only stale
// passkeys still shows 2FA as on and its recovery codes still apply --
// it just can't redeem them by presenting that passkey anymore.
func (u *User) HasSecondFactor() bool {
	return u.HasActiveTOTP() || len(u.Passkeys) > 0
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
	// ErrSingleAdmin is returned by CreateUser for a RoleAdmin request.
	// mikroview holds exactly one admin; handover is TransferAdmin, not
	// creating a second one.
	ErrSingleAdmin = errors.New("auth: MikroView has a single admin account -- transfer the role instead of creating another admin")
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
	// ErrOIDCAlreadyLinked is returned by LinkOIDCIdentity when the
	// account is already connected to a different (issuer, subject).
	// Reachable only since #1252: before it, a linked account had no
	// local password, and the link route refuses those, so nothing could
	// ask for a second link. Re-pointing would leave the first identity
	// signing in as this account too, which is not a thing any caller
	// asked for -- unlinking is a separate operation nothing implements
	// yet.
	ErrOIDCAlreadyLinked = errors.New("auth: account is already connected to an SSO identity")
	// ErrOIDCIdentityTaken is returned by LinkOIDCIdentity when the
	// (issuer, subject) pair is already linked to a *different* user --
	// an OIDC identity can back at most one local account.
	ErrOIDCIdentityTaken = errors.New("auth: this SSO identity is already linked to a different account")
	// ErrTOTPAlreadyActive is returned by SetPendingTOTPSecret when the
	// account already holds a confirmed factor. Enrolling again would
	// silently replace a factor its owner is still using -- and if they
	// abandoned the new enrolment halfway, HasActiveTOTP would keep
	// answering true against a secret their authenticator app no longer
	// holds, locking them out of their own account. Turning a factor off
	// is its own deliberate step (the password-gated route, or
	// ClearTOTP), never a side effect of starting a new one.
	ErrTOTPAlreadyActive = errors.New("auth: this account already has an authenticator app -- remove it before enrolling another")
	// ErrNoPendingTOTP is returned by ConfirmTOTP when there is no
	// unconfirmed secret to confirm: either enrolment never started, or
	// it already finished. Confirming is what activates a factor, so
	// there is nothing safe to do with a code that arrives without one.
	ErrNoPendingTOTP = errors.New("auth: no authenticator-app enrolment is waiting to be confirmed")
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
	oidcKeyDeleted := oidcKey{issuer: u.OIDCIssuer, subject: u.OIDCSubject}
	if u.OIDCIssuer != "" {
		delete(s.oidcIndex, oidcKeyDeleted)
	}
	if err := s.tryPersistLocked(); err != nil {
		// A deletion that only exists in memory must not be reported as
		// done: the caller would revoke the account's sessions and
		// tokens and tell its operator the account is gone, and a
		// restart before the next good write would bring it straight
		// back -- with none of those revocations remembered.
		s.byID[id] = u
		s.byName[strings.ToLower(u.Username)] = u.ID
		if u.OIDCIssuer != "" {
			s.oidcIndex[oidcKeyDeleted] = u.ID
		}
		return nil, fmt.Errorf("saving accounts: %w", err)
	}

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

	prevCurrentRole, prevCurrentRoleChangedAt := current.Role, current.RoleChangedAt
	prevTargetRole, prevTargetRoleChangedAt := target.Role, target.RoleChangedAt

	current.Role = RoleUser
	current.RoleChangedAt = now
	target.Role = RoleAdmin
	target.RoleChangedAt = now
	if err := s.tryPersistLocked(); err != nil {
		// Put both roles back rather than leave this call's caller
		// believing the transfer happened: an admin transfer that only
		// exists in memory is a deployment that silently regains its old
		// admin -- or loses the only one -- on the next restart.
		current.Role, current.RoleChangedAt = prevCurrentRole, prevCurrentRoleChangedAt
		target.Role, target.RoleChangedAt = prevTargetRole, prevTargetRoleChangedAt
		return nil, nil, fmt.Errorf("saving accounts: %w", err)
	}

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

// HasLocalAdmin reports whether the deployment still has a way in that
// does not depend on an identity provider: an admin account that can
// sign in with a local password.
//
// "SSO is additive; keep a local admin" (#1252, from #1245 decision 2)
// is this predicate. mikroview holds exactly one admin at a time (see
// CreateUser), so "at least one admin has a local password" and "the
// admin has a local password" are the same question -- but the name
// says the rule rather than the current cardinality, so a future second
// admin would only change this method's body.
//
// Since #1252's ruling, linking is not what makes this false -- the
// admin keeps its password (see LinkOIDCIdentity). What is left is an
// admin that never had one: a deployment bootstrapped through SSO,
// where FindOrCreateOIDCUser made the first identity to sign in the
// admin. main.go says so at every start while it holds.
//
// It reuses User.LocalPassword() rather than re-deriving "has a
// password" from the stored hash: an unmatchable hash is deliberately
// indistinguishable from a real one (see FindOrCreateOIDCUser), so
// HasLocalPassword is the only honest source.
func (s *Store) HasLocalAdmin() bool {
	admin := s.Admin()
	return admin != nil && admin.LocalPassword()
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
	// falls back instead of refusing, and why the email rule below is
	// local-creation-only.)
	if err := ValidateLocalUsername(username); err != nil {
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
	if err := s.tryPersistLocked(); err != nil {
		// An account that only exists in memory must not be reported as
		// created: Register/CreateUser's callers hand the operator a
		// session or a success response for it, and a restart before
		// the next good write would erase the account under them.
		delete(s.byID, u.ID)
		delete(s.byName, key)
		return nil, fmt.Errorf("saving accounts: %w", err)
	}

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
			// LastLogin only -- a missed update here costs nothing
			// worth failing an otherwise-successful SSO login over, so
			// this keeps the log-and-carry-on write (same reasoning as
			// Authenticate's ordinary-login path below).
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
	if err := s.tryPersistLocked(); err != nil {
		// A JIT-provisioned account that only exists in memory must not
		// be reported as created: the caller is about to sign this
		// person in as though the account durably exists, and a restart
		// before the next good write would erase it while sessions
		// referencing its ID are still live.
		delete(s.byID, u.ID)
		delete(s.byName, strings.ToLower(u.Username))
		delete(s.oidcIndex, key)
		return nil, false, fmt.Errorf("saving accounts: %w", err)
	}

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
// converting it to SSO-only in the same operation -- unless the account
// is the admin, which keeps its password and its second factor.
//
// **For every role but admin, linking is destructive and one-way.** The
// account's local password is replaced with a fresh unmatchable hash
// and HasLocalPassword is set to false, exactly as if the account had
// been OIDC-provisioned from the start. There is deliberately no state
// where a local password and a linked identity both work: keeping the
// old password alive would preserve the weaker local-password attack
// surface on an account that has supposedly moved past it, which
// defeats the point of linking.
//
// The same call also clears every local second factor -- TOTPSecret,
// TOTPConfirmedAt, TOTPLastCounter, RecoveryCodes and Passkeys, the same
// fields ClearAllSecondFactors (passkeys.go) zeroes for the CLI's
// "I've lost everything" path, inlined here rather than called out to
// because this write already holds s.mu and ClearAllSecondFactors takes
// its own lock. This closes the gap #1249 shipped and #1253 (note
// 22375) caught: leaving those fields untouched left a non-admin
// holding a factor their SSO sign-in never asks for and which
// DELETE /api/auth/totp -- password-gated -- could no longer reach,
// since the password was already gone. Unconditional, not the
// factor-remaining check ClearTOTP/ClearPasskeys make for a caller
// removing one factor at a time: linking removes both local credentials
// at once, so there is nothing left standing for either to guard.
//
// **The admin keeps its local password and its local second factor,
// permanently** (owner, 2026-09-18, #1252: "the admin must always be
// able to sign in, even with the identity provider down"). mikroview
// holds exactly one admin and never authenticates to the provider on
// its own behalf, so a provider that cannot answer means nobody gets in
// at all -- the one account that can end that outage is worth the
// attack surface the paragraph above refuses everybody else. For the
// admin, SSO is an additional way in rather than a replacement, and
// stripping its factor here would leave it unable to satisfy the
// forced-enrolment door (requireAuth, internal/api) the moment its
// linked session ends and it has to sign in locally again.
//
// Both halves live here, inside the store, rather than in the API
// handler that calls it. A convention at the call site is one forgetful
// future caller away from a dual-mode ordinary account existing, or a
// disarmed admin; an invariant here cannot be bypassed by adding a
// second caller.
//
// A role change afterwards does not re-run this: an admin demoted to
// user keeps the password and factor it had, and -transfer-admin's own
// rules (main.go) decide what the new admin holds. Linking is the event
// this method describes, not a standing property of the role.
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
	// the same reasoning createLocked documents. Which means it is
	// generated for an admin's link too and then not used; the role is
	// not knowable until the lock is held, and one wasted hash on a rare
	// operation is cheaper than holding the lock across one.
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
	// Already connected to something else. Idempotent for the same
	// identity (above and below), refused for a different one: the old
	// (issuer, subject) would stay in the index and go on signing in as
	// this account, so "re-link" would quietly mean "two ways in".
	if u.OIDCSubject != "" && (u.OIDCIssuer != issuer || u.OIDCSubject != subject) {
		return ErrOIDCAlreadyLinked
	}

	prevIssuer, prevSubject := u.OIDCIssuer, u.OIDCSubject
	prevHash, prevHasLocalPassword := u.PasswordHash, u.HasLocalPassword
	prevPasswordChangedAt := u.PasswordChangedAt
	prevTOTPSecret := u.TOTPSecret
	prevTOTPConfirmedAt := u.TOTPConfirmedAt
	prevTOTPLastCounter := u.TOTPLastCounter
	prevRecoveryCodes := u.RecoveryCodes
	prevPasskeys := u.Passkeys
	_, hadIndexEntry := s.oidcIndex[key]

	u.OIDCIssuer = issuer
	u.OIDCSubject = subject
	if u.Role != RoleAdmin {
		u.PasswordHash = unmatchable
		u.HasLocalPassword = false
		// See the doc comment above: every non-admin loses both local
		// credentials on linking, not just the password. Left alone,
		// these fields are exactly the gap note 22375 on #1253 recorded.
		u.TOTPSecret = ""
		u.TOTPConfirmedAt = time.Time{}
		u.TOTPLastCounter = 0
		u.RecoveryCodes = nil
		u.Passkeys = nil
	}
	// Invalidates every session issued before this point, including in
	// another process -- the account's credentials just changed
	// fundamentally, so anything holding a session from before that
	// should have to come back through the IdP. True for the admin too,
	// whose password survives: a second way into the account was just
	// attached, and a session issued before that should be re-made
	// through one of them. The caller that started the link is handed a
	// fresh session (see completeOIDCLink), so it is other sessions that
	// this ends.
	u.PasswordChangedAt = now
	s.oidcIndex[key] = userID
	if err := s.tryPersistLocked(); err != nil {
		// A link that only exists in memory must not be reported as
		// done: for everyone but the admin this also destroyed the
		// local password above, so the caller would tell its operator
		// SSO is now the only way in when a restart could revert to a
		// password nobody remembers is still live -- or, worse, leave
		// the account's SSO index entry pointing nowhere durable.
		u.OIDCIssuer, u.OIDCSubject = prevIssuer, prevSubject
		u.PasswordHash, u.HasLocalPassword = prevHash, prevHasLocalPassword
		u.PasswordChangedAt = prevPasswordChangedAt
		u.TOTPSecret = prevTOTPSecret
		u.TOTPConfirmedAt = prevTOTPConfirmedAt
		u.TOTPLastCounter = prevTOTPLastCounter
		u.RecoveryCodes = prevRecoveryCodes
		u.Passkeys = prevPasskeys
		if hadIndexEntry {
			s.oidcIndex[key] = userID
		} else {
			delete(s.oidcIndex, key)
		}
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// Authenticate verifies username/password and, on success, records
// LastLogin and returns a copy of the user. Always runs a password
// comparison (against dummyHash if the username doesn't exist) so a
// failed login takes the same time either way. The CPU-heavy Argon2id
// comparison deliberately happens with the lock released -- only the
// map/field reads and writes around it are synchronized.
// While an admin-issued reset code is live (#1251) the code is what
// this verifies, in place of the password -- that is what lets somebody
// locked out type it into the password box and get in. Only one
// Argon2id comparison ever runs, whichever credential is in play, so a
// pending reset is not something an attacker can spot from how long a
// failed attempt took. Nothing is lost by not also trying the password:
// issuing a code replaces the stored password hash with an unmatchable
// one, so the old password is already dead.
//
// A code is spent on the login that uses it (single use, per the
// owner's ruling on #1245 question 21, restated 2026-09-18 after the
// v0.6.0 pre-release audit had briefly amended it). The audit's
// reasoning was that losing the session before completing the forced
// change locks the account out; the owner's answer is that it does not
// -- the admin issues another code, and that round trip is the price.
// A code left live until the password is actually set is replayable
// for its full 24 hours by anyone who saw it, and whoever finishes
// first takes the account.
//
// MustChangePassword is *not* cleared here -- only setting a new
// password does that -- so the session this login goes on to create is
// still the restricted one.
func (s *Store) Authenticate(username, password string, now time.Time) (*User, error) {
	s.reloadIfStale()

	s.mu.RLock()
	id, known := s.byName[strings.ToLower(username)]
	hash := dummyHash
	viaResetCode := false
	if known {
		u := s.byID[id]
		if u.resetCodeLive(now) {
			hash, viaResetCode = u.ResetCodeHash, true
		} else {
			hash = u.PasswordHash
		}
	}
	s.mu.RUnlock()

	secret := password
	if viaResetCode {
		secret = NormaliseResetCode(password)
	}
	valid := VerifyPassword(secret, hash)
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
	if viaResetCode {
		// Re-checked under the write lock rather than trusted from the
		// read above: a second reset in the window between them issues a
		// new code and must kill this one, and a spend that landed first
		// must not be honoured twice.
		if !u.resetCodeLive(now) {
			return nil, ErrInvalidCredentials
		}
		// Spending the code is the write that matters here: a spend
		// that only lands in memory is undone by a restart, and the
		// code is live again for whoever saw it. Refuse the login
		// rather than honour a spend nothing recorded (R6). A missed
		// LastLogin on an ordinary password login costs nothing, so
		// that path keeps the log-and-carry-on write below.
		prevHash, prevExpires, prevLogin := u.ResetCodeHash, u.ResetCodeExpiresAt, u.LastLogin
		u.ResetCodeHash = ""
		u.ResetCodeExpiresAt = time.Time{}
		u.LastLogin = now
		if err := s.tryPersistLocked(); err != nil {
			u.ResetCodeHash, u.ResetCodeExpiresAt, u.LastLogin = prevHash, prevExpires, prevLogin
			return nil, fmt.Errorf("saving the spent reset code: %w", err)
		}
		cp := *u
		return &cp, nil
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
	prevHash := u.PasswordHash
	prevPasswordChangedAt := u.PasswordChangedAt
	prevHasLocalPassword := u.HasLocalPassword
	prevResetHash := u.ResetCodeHash
	prevResetExpiresAt := u.ResetCodeExpiresAt
	prevMustChange := u.MustChangePassword

	u.PasswordHash = hash
	u.PasswordChangedAt = now
	// An account that has a password has a local password, by
	// definition. Stated explicitly rather than left to be derived from
	// OIDCIssuer, so a linked account (OIDC *and* a local password)
	// isn't misread as SSO-only by the recovery tooling.
	u.HasLocalPassword = true
	// Setting a password ends any outstanding admin reset (#1251): the
	// account now has a credential only its owner knows, so the code
	// stops working and the forced-change gate lifts. Done here, inside
	// the store, so every path that sets a password clears it -- the CLI
	// recovery tool as much as the change-password route -- rather than
	// each caller having to remember.
	u.ResetCodeHash = ""
	u.ResetCodeExpiresAt = time.Time{}
	u.MustChangePassword = false
	if err := s.tryPersistLocked(); err != nil {
		// A password change that only exists in memory must not be
		// reported as done: the caller (the change-password route, or
		// the recovery CLI) would tell its operator the old credential
		// is dead, and a restart before the next good write would prove
		// that wrong.
		u.PasswordHash = prevHash
		u.PasswordChangedAt = prevPasswordChangedAt
		u.HasLocalPassword = prevHasLocalPassword
		u.ResetCodeHash = prevResetHash
		u.ResetCodeExpiresAt = prevResetExpiresAt
		u.MustChangePassword = prevMustChange
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// HasActiveTOTP reports whether userID holds a confirmed authenticator-
// app second factor -- both login (does this sign-in need a code?) and
// the admin UI (does this account have 2FA on?) need the answer, and
// "has a secret" is not the same question as "has an active factor" --
// see User.HasActiveTOTP. An unknown user answers false rather than
// erroring: this is a yes/no gate, not a lookup, and the false answer is
// the same one a real account with no factor would give.
func (s *Store) HasActiveTOTP(userID string) bool {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[userID]
	return ok && u.HasActiveTOTP()
}

// SetPendingTOTPSecret stores a freshly generated, not-yet-confirmed
// secret for userID, replacing any earlier enrolment that was started
// and abandoned. The factor is not active afterwards: ConfirmTOTP is
// what activates it, so an enrolment interrupted at the QR code leaves
// the account signing in exactly as it did before.
//
// Refuses with ErrTOTPAlreadyActive when a confirmed factor is already
// in place -- see that error for why replacing one silently is a
// lockout waiting to happen.
//
// The replay counter is reset alongside the secret. A counter is only
// meaningful against the secret it was accepted for: carried over to a
// new secret it would refuse that secret's early codes for as long as
// the old factor had been in use, which reads to the person enrolling
// as an authenticator app that simply does not work.
func (s *Store) SetPendingTOTPSecret(userID, encodedSecret string) error {
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
	if u.HasActiveTOTP() {
		return ErrTOTPAlreadyActive
	}

	prevSecret := u.TOTPSecret
	prevCounter := u.TOTPLastCounter

	u.TOTPSecret = encodedSecret
	u.TOTPLastCounter = 0
	if err := s.tryPersistLocked(); err != nil {
		// An enrolment that only exists in memory must not be reported
		// as started: the caller is about to show a QR code the user
		// scans into their phone, and a restart before the next good
		// write would leave the store with no secret to confirm that
		// app's codes against.
		u.TOTPSecret = prevSecret
		u.TOTPLastCounter = prevCounter
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// ConfirmTOTP activates the pending secret for userID, recording when
// its owner proved they could produce a code from it and the counter of
// the code that proved it. Passing the matching counter rather than
// starting the guard at zero closes the obvious replay: the code just
// used to enrol must not also work as the first sign-in.
//
// Returns ErrNoPendingTOTP when there is nothing unconfirmed to
// activate, which covers both "enrolment never started" and "already
// confirmed" -- neither is a state where accepting a code should change
// anything.
//
// Verifying the code is the caller's job (VerifyTOTP in totp.go); this
// only records the outcome. The recovery codes that accompany a
// confirmed factor are minted separately, by GenerateRecoveryCodes.
func (s *Store) ConfirmTOTP(userID string, confirmedAt time.Time, matchedCounter uint64) error {
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
	if u.TOTPSecret == "" || !u.TOTPConfirmedAt.IsZero() {
		return ErrNoPendingTOTP
	}

	prevConfirmedAt := u.TOTPConfirmedAt
	prevCounter := u.TOTPLastCounter

	u.TOTPConfirmedAt = confirmedAt
	u.TOTPLastCounter = matchedCounter
	if err := s.tryPersistLocked(); err != nil {
		// A confirmation that only exists in memory must not be
		// reported as done: the caller is about to tell its user the
		// factor is on and hand them recovery codes, and a restart
		// would drop the account back to password-only underneath that
		// claim.
		u.TOTPConfirmedAt = prevConfirmedAt
		u.TOTPLastCounter = prevCounter
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// RecordTOTPCounter advances userID's replay guard to the counter of a
// code just accepted at sign-in. The caller reads User.TOTPLastCounter,
// passes it to VerifyTOTP, and hands the matched counter back here;
// without this call the guard never moves and every code stays usable
// for its whole window.
//
// The counter only ever moves forward. A value at or below the stored
// one is a no-op rather than an error: it means some other request for
// the same account already recorded this code or a later one, and
// writing it back would undo their guard -- the one outcome this method
// exists to prevent. There is no failure for the caller to handle,
// because nothing has gone wrong.
func (s *Store) RecordTOTPCounter(userID string, matchedCounter uint64) error {
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
	if matchedCounter <= u.TOTPLastCounter {
		return nil
	}

	prevCounter := u.TOTPLastCounter
	u.TOTPLastCounter = matchedCounter
	if err := s.tryPersistLocked(); err != nil {
		// A guard that only advanced in memory must not be reported as
		// advanced: the caller has already let this sign-in through, and
		// a restart before the next good write would make the same code
		// live again for whoever else presented it.
		u.TOTPLastCounter = prevCounter
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// VerifyAndRecordTOTP checks code against userID's active TOTP secret
// and, only when it matches, advances the replay counter -- both under
// the same lock acquisition. Login (handleAuthLoginFactor, auth.go) is
// the caller this exists for: checking with VerifyTOTP and recording
// with RecordTOTPCounter as two separate calls left a window where two
// concurrent submissions of the same code both verified against the
// same not-yet-advanced counter and both won a session (reproduced 8 of
// 15 runs). Doing both under one lock closes it -- whichever request
// gets the lock second sees the first request's already-advanced
// counter, so VerifyTOTP itself (its own doc comment: "any candidate
// counter <= lastUsedCounter is skipped even when its code is correct")
// refuses the replay.
//
// ok reports whether code matched; err is only ever a persistence
// failure on a match, reported the same degraded-but-not-locked-out way
// RecordTOTPCounter's own doc comment describes -- the code that just
// matched earned the login regardless of whether the counter's advance
// made it to disk.
func (s *Store) VerifyAndRecordTOTP(userID, code string, now time.Time) (ok bool, err error) {
	if !s.Persisted() {
		return false, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, found := s.byID[userID]
	if !found {
		return false, ErrUserNotFound
	}

	matched, matchedOK := VerifyTOTP(u.TOTPSecret, code, now, u.TOTPLastCounter)
	if !matchedOK {
		return false, nil
	}

	prevCounter := u.TOTPLastCounter
	u.TOTPLastCounter = matched
	if err := s.tryPersistLocked(); err != nil {
		u.TOTPLastCounter = prevCounter
		return true, fmt.Errorf("saving accounts: %w", err)
	}
	return true, nil
}

// ClearTOTP removes userID's authenticator-app factor entirely: the
// secret, its confirmation and the replay counter, always. The
// account's recovery codes went the same way unconditionally before
// #1250; now they're cleared only if this was the account's last second
// factor. Recovery codes are shared between the authenticator app and
// passkeys (#1250): stripping them here while a passkey remains would
// silently orphan that passkey's fallback, for a call this was never
// asked to touch. See HasSecondFactor's doc comment for the shared
// test, and DeletePasskey/ClearPasskeys in passkeys.go for the same
// rule applied from the passkey side.
func (s *Store) ClearTOTP(userID string) error {
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

	prevSecret := u.TOTPSecret
	prevConfirmedAt := u.TOTPConfirmedAt
	prevCounter := u.TOTPLastCounter
	prevCodes := u.RecoveryCodes

	u.TOTPSecret = ""
	u.TOTPConfirmedAt = time.Time{}
	u.TOTPLastCounter = 0
	if len(u.Passkeys) == 0 {
		u.RecoveryCodes = nil
	}
	if err := s.tryPersistLocked(); err != nil {
		// A clear that only exists in memory must not be reported as
		// done: the caller tells its operator the authenticator app is
		// off, and a restart before the next good write would silently
		// bring back the old secret, counter and (if it was cleared)
		// recovery codes underneath that claim.
		u.TOTPSecret = prevSecret
		u.TOTPConfirmedAt = prevConfirmedAt
		u.TOTPLastCounter = prevCounter
		u.RecoveryCodes = prevCodes
		return fmt.Errorf("saving accounts: %w", err)
	}
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
		// The reset-code hash is a credential verifier too (#1251), and
		// this list is the one that leaves the package on its way to an
		// admin-facing API. Blanked for the same reason the password
		// hash is, so neither can be serialized by accident.
		cp.ResetCodeHash = ""
		// TOTPSecret is worse than a verifier hash if it leaked -- it's
		// the actual shared secret, good for minting valid codes
		// indefinitely, not just checking one. RecoveryCodes are hashes
		// only, same category as ResetCodeHash above. Neither belongs in
		// an admin-facing account list.
		cp.TOTPSecret = ""
		cp.RecoveryCodes = nil
		// Passkeys carries each credential's PublicKey -- not a secret
		// the way a private key would be, but still credential material
		// an admin-facing account list has no business serializing, same
		// stance as the three fields above. Blanked wholesale rather
		// than per-field, same as TOTPSecret: a caller that needs a
		// count (the users list's passkeyCount, internal/api wave 2)
		// must call PasskeyCount(userID) (passkeys.go) instead of
		// reading len(this copy's Passkeys), which always reads zero
		// now. #1249 shipped exactly this mistake once already, reading
		// HasActiveTOTP off a List() copy whose TOTPSecret was blanked
		// the same way -- see PasskeyCount's doc comment.
		cp.Passkeys = nil
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// tryPersistLocked is persistLocked's error-returning half, for the
// callers (IssueResetCode, TransferAdmin, SetPassword, DeleteUser,
// createLocked -- behind Register and CreateUser --,
// FindOrCreateOIDCUser's new-account branch, LinkOIDCIdentity) that
// change a credential, a role, or which accounts exist, and so must not
// let the caller believe a write happened when it didn't -- see each
// one's own restore-on-error comment. Every other caller keeps using
// persistLocked below, which keeps today's swallow-and-log behaviour.
func (s *Store) tryPersistLocked() error {
	if s.backend == nil {
		return nil
	}
	list := make([]*User, 0, len(s.byID))
	for _, u := range s.byID {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Username < list[j].Username })

	data, err := json.MarshalIndent(storeFile{Users: list}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding accounts for persistence failed: %w", err)
	}

	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing accounts to %s failed: %w", s.backend.Describe(), err)
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
	return nil
}

// persistLocked is the swallow-and-log default every ordinary write
// uses: the in-memory state (which every read goes through) stays
// correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than failing the caller outright.
// Kept by FindOrCreateOIDCUser's existing-login branch and
// Authenticate's ordinary-login branch, both of which only touch
// LastLogin -- a bookkeeping timestamp not worth failing an otherwise
// successful login over.
func (s *Store) persistLocked() {
	if err := s.tryPersistLocked(); err != nil {
		persistLog.Error(fmt.Sprintf("%v -- this change exists only in memory and will be lost on restart", err))
	}
}
