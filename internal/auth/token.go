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

// Token is a long-lived bearer credential for service-to-service access
// (issue #101) -- e.g. a companion project like Birdcage pulling
// event/flag data with no browser to hold a session cookie. Unlike
// Session, a Token is persisted: it has to survive a mikroview restart
// without the caller re-provisioning it.
//
// The raw token value is never stored -- only HashedValue, its SHA-256
// digest -- and is shown to the creator exactly once, at creation time
// (see TokenStore.Create). SHA-256, not Argon2id: Argon2id's cost is
// there to slow down guessing a low-entropy, human-chosen password: a
// token's value is a 128-bit crypto/rand string (see newID), already
// far outside brute-forceable range, so a slow KDF buys nothing here
// and would only add needless CPU cost to every authenticated request
// (same reasoning GitHub/GitLab personal access tokens use).
//
// There is no expiry field: like sessions and accounts, a token stays
// valid until explicitly revoked (see TokenStore.Revoke) -- no silent-
// expiry surprises for whatever integration is holding it.
type Token struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	HashedValue string    `json:"hashedValue"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUsedAt  time.Time `json:"lastUsedAt,omitzero"`
}

var (
	// ErrTokenNotPersisted is returned by Create when no storePath is
	// configured -- refusing rather than silently issuing a token that
	// would vanish (and become unrevocable, since it was never recorded)
	// on the next restart.
	ErrTokenNotPersisted = errors.New("auth: storePath is not configured, refusing to create a token that would not survive a restart")
	// ErrTokenNotFound is returned by Revoke for an unknown token ID.
	ErrTokenNotFound = errors.New("auth: no such token")
)

// TokenStore persists API tokens to a JSON file -- the same JSON-file +
// atomic-write + mutex convention as Store (internal/auth/store.go).
// Unlike Store, there is no separate "disabled"/undecided bootstrap
// state to track: a token can only ever be created by an already-
// authenticated admin (see internal/api's handleTokensCreate), so
// there's no zero-tokens state that needs special handling the way
// Store.Count()==0 does.
type TokenStore struct {
	mu   sync.RWMutex
	path string
	byID map[string]*Token
	// byHash maps a token's SHA-256 hash straight to its ID, so
	// Authenticate is an O(1) map lookup rather than scanning every
	// token -- possible only because, unlike Argon2id password hashes,
	// SHA-256 is unsalted and deterministic: the same raw value always
	// hashes to the same key.
	byHash map[string]string
}

// OpenTokenStore loads (or, on first run, prepares to create) the token
// store at path. path=="" returns a usable, empty, unpersisted store --
// the same "stays usable, just refuses to persist" contract auth.Open
// has, so a deployment with no tokens configured never fails to start
// over this.
func OpenTokenStore(path string) (*TokenStore, error) {
	s := &TokenStore{path: path, byID: make(map[string]*Token), byHash: make(map[string]string)}
	if path == "" {
		return s, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return s, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}

	var list []*Token
	if err := json.Unmarshal(data, &list); err != nil {
		return s, err
	}
	for _, t := range list {
		if t == nil { // see Store.Open's identical guard for why this is needed
			continue
		}
		s.byID[t.ID] = t
		s.byHash[t.HashedValue] = t.ID
	}
	return s, nil
}

// Persisted reports whether this store can actually survive a restart.
func (s *TokenStore) Persisted() bool {
	return s.path != ""
}

// hashTokenValue is the one place a raw token value is ever hashed --
// used identically by Create (to compute what gets stored) and
// Authenticate (to compute what gets looked up), so the two can never
// drift apart.
func hashTokenValue(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create generates a new token named name and persists its metadata +
// hash. The returned raw string is the only time the actual bearer
// value ever exists outside the caller's memory -- it is not
// recoverable afterward, only re-issuable as a brand new token.
func (s *TokenStore) Create(name string, now time.Time) (raw string, tok *Token, err error) {
	if !s.Persisted() {
		return "", nil, ErrTokenNotPersisted
	}

	// newID's generator -- same 128-bit crypto/rand source Session
	// already uses for its own unguessable IDs (see id.go).
	raw = newID()
	hash := hashTokenValue(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	t := &Token{
		ID:          newID(),
		Name:        strings.TrimSpace(name),
		HashedValue: hash,
		CreatedAt:   now,
	}
	s.byID[t.ID] = t
	s.byHash[hash] = t.ID
	s.persistLocked()

	cp := *t
	return raw, &cp, nil
}

// Authenticate validates a raw bearer token value, recording LastUsedAt
// on success. Returns (nil, false) for an unknown, malformed, or
// revoked token -- deliberately no distinction between those, same as
// Store.Authenticate's treatment of unknown-username vs. wrong-password.
func (s *TokenStore) Authenticate(raw string, now time.Time) (*Token, bool) {
	if raw == "" {
		return nil, false
	}
	hash := hashTokenValue(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byHash[hash]
	if !ok {
		return nil, false
	}
	t, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	t.LastUsedAt = now
	s.persistLocked()
	cp := *t
	return &cp, true
}

// Revoke permanently deletes a token by ID -- there is no "disable and
// keep around" state, matching how a revoked session is deleted
// outright rather than flagged (see SessionStore.Revoke).
func (s *TokenStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok {
		return ErrTokenNotFound
	}
	delete(s.byID, id)
	delete(s.byHash, t.HashedValue)
	s.persistLocked()
	return nil
}

// List returns every token's metadata, oldest first -- HashedValue is
// always zeroed out (never the raw value either, since this store never
// retains it past Create's return) so a list response can never leak
// anything an attacker could use to authenticate.
func (s *TokenStore) List() []Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Token, 0, len(s.byID))
	for _, t := range s.byID {
		cp := *t
		cp.HashedValue = ""
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *TokenStore) persistLocked() {
	if s.path == "" {
		return
	}
	list := make([]*Token, 0, len(s.byID))
	for _, t := range s.byID {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding %s for persistence failed: %v -- this change exists only in memory and will be lost on restart", s.path, err))
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		persistLog.Error(fmt.Sprintf("writing %s failed: %v -- this change exists only in memory and will be lost on restart", tmp, err))
		return
	}
	// Same filesystem, so the rename itself is atomic -- but it can
	// still fail (read-only remount, permissions change), and a
	// silent failure here means the caller believes a write landed
	// when it did not.
	if err := os.Rename(tmp, s.path); err != nil {
		persistLog.Error(fmt.Sprintf("replacing %s failed: %v -- this change exists only in memory and will be lost on restart", s.path, err))
		return
	}
}
