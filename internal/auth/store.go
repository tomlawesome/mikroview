// Package auth implements mikroview's local username/password
// authentication: user accounts (this file), Argon2id password hashing
// (password.go), in-memory sessions (session.go), and login-attempt
// rate limiting (ratelimit.go).
//
// Mikroview stays fully open (today's behavior) until the first local
// user is created -- see Store.Count(), consulted by internal/api's
// auth middleware. Nothing here ever assumes OIDC/SSO exists; that's a
// separate, later addition on top of this.
package auth

import (
	"encoding/json"
	"errors"
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
)

// Store persists user accounts to a JSON file. Unlike internal/flags'
// Store, persistence is not optional: an empty path leaves Store usable
// (so mikroview still boots fine with auth unconfigured) but Register/
// CreateUser refuse to add a user in that state -- see ErrNotPersisted.
type Store struct {
	mu     sync.RWMutex
	path   string
	byID   map[string]*User
	byName map[string]string // lowercased username -> ID
	// mtime tracks the store file's modification time as of the last
	// load, so a running server can pick up a change made by a separate
	// process -- namely the CLI recovery tool (`-reset-password`),
	// which opens its own independent Store and writes to the same
	// file. Without this, a password reset would silently have no
	// effect on an already-running server until it restarts, defeating
	// the point of a recovery tool that shouldn't require one.
	mtime time.Time
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, byID: make(map[string]*User), byName: make(map[string]string)}
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

	var list []*User
	if err := json.Unmarshal(data, &list); err != nil {
		return s, err
	}
	for _, u := range list {
		s.byID[u.ID] = u
		s.byName[strings.ToLower(u.Username)] = u.ID
	}
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
	var list []*User
	if err := json.Unmarshal(data, &list); err != nil {
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
	byID := make(map[string]*User, len(list))
	byName := make(map[string]string, len(list))
	for _, u := range list {
		byID[u.ID] = u
		byName[strings.ToLower(u.Username)] = u.ID
	}
	s.byID = byID
	s.byName = byName
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

// Register creates the very first account, always as RoleAdmin,
// regardless of what a future caller might pass -- there is no role
// parameter because there's no meaningful choice: the first person to
// register is the super-admin by definition (see the local-auth design).
// Fails with ErrRegistrationClosed once any account exists.
func (s *Store) Register(username, password string, now time.Time) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	if s.Count() > 0 {
		return nil, ErrRegistrationClosed
	}
	return s.createLocked(username, password, RoleAdmin, now)
}

// CreateUser adds an additional account with the given role -- for use
// by an already-authenticated admin (internal/api enforces the caller's
// role; Store itself has no notion of "who is calling"), or by the CLI
// recovery tooling.
func (s *Store) CreateUser(username, password string, role Role, now time.Time) (*User, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}
	return s.createLocked(username, password, role, now)
}

func (s *Store) createLocked(username, password string, role Role, now time.Time) (*User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

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

	data, err := json.MarshalIndent(list, "", "  ")
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
