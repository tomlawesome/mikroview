// SPDX-License-Identifier: AGPL-3.0-only

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

	"github.com/tomlawesome/mikroview/internal/persist"
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is what this token may be used for, and it is not advisory:
	// Authenticate takes the kind its caller expects and will not match
	// a token of any other, so there is no code path where a token of
	// one kind satisfies a check written for another. See TokenKind.
	Kind TokenKind `json:"kind"`
	// Device scopes an ingest token to exactly one router (issue #186).
	// Required for TokenKindIngest, and rejected for any other kind.
	//
	// The point is blast radius. A router-side script has to hold this
	// value in a place that #186 established any RouterOS user with
	// `read` can print, so the realistic question is not whether one
	// leaks but what a leaked one reaches. Scoped, it can only ever
	// speak for the router it was issued for; unscoped, one compromised
	// router could report state for every other device in the
	// deployment.
	//
	// Uniqueness per device is deliberately *not* enforced. Rotation
	// needs a window where the replacement exists before the old one is
	// revoked, and forbidding that would push operators towards
	// revoke-then-reissue -- a gap where the router is silently not
	// reporting.
	Device      string    `json:"device,omitempty"`
	HashedValue string    `json:"hashedValue"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUsedAt  time.Time `json:"lastUsedAt,omitzero"`
	// CreatedBy is the account ID that issued this token, so deleting
	// that account can revoke it (see RevokeAllCreatedBy).
	//
	// Reachable via admin transfer: an admin creates tokens, hands admin
	// to someone else, and is later deleted as an ordinary user. Without
	// this, they keep working read-only API access after their account
	// is gone -- they still hold the raw value, which is all a token
	// needs.
	//
	// Empty on tokens written before this field existed. Those cannot be
	// attributed to anyone and so are never auto-revoked; they have to
	// be reviewed by hand in the token list.
	CreatedBy string `json:"createdBy,omitempty"`
	// CreatedByUsername is a display snapshot, taken at creation. Kept
	// alongside the ID because the point at which it is most useful --
	// after that account has been deleted -- is exactly when the ID can
	// no longer be resolved to a name. Never used for authorization.
	CreatedByUsername string `json:"createdByUsername,omitempty"`
}

// TokenKind separates the two credentials this store holds. They are not
// interchangeable in either direction, and that is enforced structurally
// rather than by convention: Authenticate requires its caller to name
// the kind it expects, so "I forgot to check the kind" is not an
// available mistake.
//
// The asymmetry is the reason. A read-only API token reads everything
// mikroview knows -- events, flags, stats, devices. An ingest token only
// writes observations about one router and can read nothing at all. If
// either could be presented where the other was expected, the ingest
// token issued to a script on a router (where #186 established any
// `read` user can print it) would become a read-everything credential.
type TokenKind string

const (
	// TokenKindAPI is the read-only service-to-service token from #101.
	TokenKindAPI TokenKind = "api"
	// TokenKindIngest is a RouterOS push-ingest token (#186), scoped to
	// one device and accepted only by the ingest endpoint.
	TokenKindIngest TokenKind = "ingest"
)

// Valid reports whether k is a kind this build knows about. Anything
// else is treated as unusable rather than as a variant to be tolerated
// -- see OpenTokenStoreWithBackend.
func (k TokenKind) Valid() bool {
	return k == TokenKindAPI || k == TokenKindIngest
}

var (
	// ErrTokenNotPersisted is returned by Create when no storePath is
	// configured -- refusing rather than silently issuing a token that
	// would vanish (and become unrevocable, since it was never recorded)
	// on the next restart.
	ErrTokenNotPersisted = errors.New("auth: storePath is not configured, refusing to create a token that would not survive a restart")
	// ErrTokenNotFound is returned by Revoke for an unknown token ID.
	ErrTokenNotFound = errors.New("auth: no such token")
	// ErrTokenKindInvalid is returned by Create for a kind this build
	// does not know. Defaulting an unrecognised kind to the read-only
	// one would be the wrong direction to guess in.
	ErrTokenKindInvalid = errors.New("auth: unknown token kind")
	// ErrTokenDeviceRequired is returned by Create for an ingest token
	// with no device: an unscoped ingest token is the thing the scope
	// exists to prevent, so there is no "leave it blank for all
	// devices" reading of an empty value.
	ErrTokenDeviceRequired = errors.New("auth: an ingest token must name the device it is issued for")
	// ErrTokenDeviceNotAllowed is returned by Create when a non-ingest
	// token carries a device. Accepting and ignoring it would leave the
	// caller believing in a scope that nothing enforces.
	ErrTokenDeviceNotAllowed = errors.New("auth: only an ingest token may name a device")
)

// TokenStore persists API tokens to a JSON file -- the same JSON-file +
// atomic-write + mutex convention as Store (internal/auth/store.go).
// Unlike Store, there is no separate "disabled"/undecided bootstrap
// state to track: a token can only ever be created by an already-
// authenticated admin (see internal/api's handleTokensCreate), so
// there's no zero-tokens state that needs special handling the way
// Store.Count()==0 does.
type TokenStore struct {
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
	byID    map[string]*Token
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
	if path == "" {
		return OpenTokenStoreWithBackend(nil)
	}
	return OpenTokenStoreWithBackend(persist.NewFileBackend(path))
}

// OpenTokenStoreWithBackend is OpenTokenStore against any persist.Backend
// -- a JSON file by default, or Postgres when configured (issue #131).
func OpenTokenStoreWithBackend(b persist.Backend) (*TokenStore, error) {
	s := &TokenStore{backend: b, byID: make(map[string]*Token), byHash: make(map[string]string)}

	data, version, err := persist.LoadDocument(context.Background(), b)
	if err != nil {
		return s, err
	}
	if data == nil {
		return s, nil
	}
	s.version = version

	var list []*Token
	if err := json.Unmarshal(data, &list); err != nil {
		return s, err
	}
	for _, t := range list {
		if t == nil { // see Store.Open's identical guard for why this is needed
			continue
		}
		s.byID[t.ID] = t
		// A token whose kind this build does not recognise is kept in
		// byID but deliberately left out of byHash, so it is listable
		// and revocable but cannot authenticate anything. Failing closed
		// is the only safe direction: the alternative is guessing which
		// kind a stored token meant, and guessing "read-only API" for a
		// value that reads everything mikroview knows is not a guess
		// worth making. An operator sees it in the token list and
		// reissues it.
		if !t.Kind.Valid() {
			persistLog.Warn(fmt.Sprintf("token %q has unknown kind %q -- it will not authenticate; revoke and reissue it", t.Name, t.Kind))
			continue
		}
		s.byHash[t.HashedValue] = t.ID
	}
	return s, nil
}

// Persisted reports whether this store can actually survive a restart.
func (s *TokenStore) Persisted() bool {
	return s.backend != nil
}

// hashTokenValue is the one place a raw token value is ever hashed --
// used identically by Create (to compute what gets stored) and
// Authenticate (to compute what gets looked up), so the two can never
// drift apart.
func hashTokenValue(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create generates a new token named name, of kind kind, and persists
// its metadata + hash. The returned raw string is the only time the
// actual bearer value ever exists outside the caller's memory -- it is
// not recoverable afterward, only re-issuable as a brand new token.
// creator identifies the account issuing the token, so it can be
// revoked if that account is later deleted.
//
// device scopes an ingest token to one router and must be empty for any
// other kind -- see Token.Device.
func (s *TokenStore) Create(name string, kind TokenKind, device string, creator *User, now time.Time) (raw string, tok *Token, err error) {
	if !s.Persisted() {
		return "", nil, ErrTokenNotPersisted
	}
	if !kind.Valid() {
		return "", nil, ErrTokenKindInvalid
	}
	device = strings.TrimSpace(device)
	if kind == TokenKindIngest && device == "" {
		return "", nil, ErrTokenDeviceRequired
	}
	if kind != TokenKindIngest && device != "" {
		return "", nil, ErrTokenDeviceNotAllowed
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
		Kind:        kind,
		Device:      device,
		HashedValue: hash,
		CreatedAt:   now,
	}
	if creator != nil {
		t.CreatedBy = creator.ID
		t.CreatedByUsername = creator.Username
	}
	s.byID[t.ID] = t
	s.byHash[hash] = t.ID
	s.persistLocked()

	cp := *t
	return raw, &cp, nil
}

// Authenticate validates a raw bearer token value *of kind want*,
// recording LastUsedAt on success. Returns (nil, false) for an unknown,
// malformed, or revoked token -- deliberately no distinction between
// those, same as Store.Authenticate's treatment of unknown-username vs.
// wrong-password -- and equally for a real, valid token of the wrong
// kind.
//
// want is a parameter rather than something the caller inspects
// afterwards on purpose. A returned *Token with a Kind field invites
// exactly one bug: a caller that authenticates, forgets to check, and
// thereby accepts an ingest token wherever it meant to accept a
// read-only one. Requiring the kind up front means that mistake cannot
// be made silently -- there is no way to call this without saying what
// you expect. LastUsedAt is left untouched on a kind mismatch, so a
// token presented at the wrong door does not look like it was used.
func (s *TokenStore) Authenticate(raw string, want TokenKind, now time.Time) (*Token, bool) {
	if raw == "" || !want.Valid() {
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
	if t.Kind != want {
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

// RevokeAllCreatedBy deletes every token issued by userID, returning how
// many went. Called when that account is deleted: the person still holds
// the raw values, so the account going away has to take its tokens with
// it.
//
// An empty userID matches nothing, deliberately -- pre-attribution
// tokens carry an empty CreatedBy, and treating that as a match would
// let deleting any one account wipe every unattributed token in the
// deployment.
func (s *TokenStore) RevokeAllCreatedBy(userID string) int {
	if userID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	revoked := 0
	for id, t := range s.byID {
		if t.CreatedBy != userID {
			continue
		}
		delete(s.byID, id)
		delete(s.byHash, t.HashedValue)
		revoked++
	}
	if revoked > 0 {
		s.persistLocked()
	}
	return revoked
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
	if s.backend == nil {
		return
	}
	list := make([]*Token, 0, len(s.byID))
	for _, t := range s.byID {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding API tokens for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing API tokens to %s failed: %v -- this change exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("API tokens was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}
