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
	"unicode"
	"unicode/utf8"

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

// TokenKind separates the three credentials this store holds. They are
// not interchangeable in either direction, and that is enforced
// structurally rather than by convention: Authenticate requires its
// caller to name the kind it expects, so "I forgot to check the kind" is
// not an available mistake.
//
// The asymmetry is the reason. A read-only API token reads everything
// mikroview knows -- events, flags, stats, devices. An ingest token only
// writes observations about one router and can read nothing at all. A
// droplist-pull token (#1224) can read exactly one thing -- the
// generated .rsc drop-list feed -- and nothing else. If any could be
// presented where another was expected, the ingest token issued to a
// script on a router (where #186 established any `read` user can print
// it) would become a read-everything credential, and the droplist-pull
// key -- meant to sit in the same kind of scheduled router fetch, so the
// same exposure applies to it -- would become one too.
type TokenKind string

const (
	// TokenKindAPI is the read-only service-to-service token from #101.
	TokenKindAPI TokenKind = "api"
	// TokenKindIngest is a RouterOS push-ingest token (#186), scoped to
	// one device and accepted only by the ingest endpoint.
	TokenKindIngest TokenKind = "ingest"
	// TokenKindDroplistPull is the pull-only credential RouterOS's own
	// scheduled `/tool fetch` presents at GET /api/droplist.rsc (#1224).
	// It carries no device: unlike an ingest token it is not scoped to
	// one router -- every router that fetches the drop list reads the
	// same list -- so the "kind != TokenKindIngest && device != """ rule
	// in Create already refuses one that tries to carry one.
	TokenKindDroplistPull TokenKind = "droplist-pull"
)

// Valid reports whether k is a kind this build knows about. Anything
// else is treated as unusable rather than as a variant to be tolerated
// -- see OpenTokenStoreWithBackend.
func (k TokenKind) Valid() bool {
	return k == TokenKindAPI || k == TokenKindIngest || k == TokenKindDroplistPull
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
	// ErrTokenDeviceInvalid is returned by Create for a device id that
	// cannot be one: too long, or carrying control/formatting
	// characters. The id is a display value (it appears in the token
	// list, the audit trail and log lines) as well as a scope key, and
	// an unbounded or control-bearing one is a typo that becomes a
	// permanently, invisibly dead token at best.
	ErrTokenDeviceInvalid = errors.New("auth: device id must be at most 64 characters of printable text")
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
//
// A document that exists but cannot be read or parsed is a hard error
// (issue #378): the caller gets (nil, err) rather than a store whose
// live backend would overwrite that document on the first write. See
// persist.Open.
func OpenTokenStoreWithBackend(b persist.Backend) (*TokenStore, error) {
	s := &TokenStore{backend: b, byID: make(map[string]*Token), byHash: make(map[string]string)}

	version, existed, err := persist.Open(context.Background(), b, "the API tokens store", func(data []byte) error {
		var list []*Token
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		for _, t := range list {
			if t == nil { // see Store.Open's identical guard for why this is needed
				continue
			}
			s.byID[t.ID] = t
			// A token whose kind this build does not recognise is kept
			// in byID but deliberately left out of byHash, so it is
			// listable and revocable but cannot authenticate anything.
			// Failing closed is the only safe direction: the
			// alternative is guessing which kind a stored token meant,
			// and guessing "read-only API" for a value that reads
			// everything mikroview knows is not a guess worth making.
			// An operator sees it in the token list and reissues it.
			if !t.Kind.Valid() {
				persistLog.Warn(fmt.Sprintf("token %q has unknown kind %q -- it will not authenticate; revoke and reissue it", t.Name, t.Kind))
				continue
			}
			s.byHash[t.HashedValue] = t.ID
		}
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

// MaxDeviceIDLen bounds a token's device scope. Nothing legitimate comes
// close: a configured id is operator-chosen and a discovered one is an
// IP literal (at most 45 characters for IPv6 with a zone).
//
// Exported (#1304, Q9) so internal/api/setupcommands.go's own device-name
// limit can be this value directly, rather than a second constant that
// was only ever kept in step with this one by a comment saying so.
const MaxDeviceIDLen = 64

// validDeviceID rejects a device scope that could not have come from a
// real device. Control and Unicode formatting characters are refused for
// the same reason logging.Printable exists -- this string reaches an
// operator's terminal and browser -- and the length cap keeps a 60KB
// "device" out of the token store.
func validDeviceID(device string) bool {
	if device == "" {
		return true // the required/not-allowed rules above already ruled on this
	}
	if len(device) > MaxDeviceIDLen {
		return false
	}
	for _, r := range device {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return false
		}
	}
	return utf8.ValidString(device)
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
	if !validDeviceID(device) {
		return "", nil, ErrTokenDeviceInvalid
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
	if err := s.tryPersistLocked(); err != nil {
		// A token that only exists in memory must not be handed to the
		// caller: the raw value is shown exactly once, here, so a
		// restart before the next good write would leave the caller
		// holding a value that authenticates against nothing.
		delete(s.byID, t.ID)
		delete(s.byHash, hash)
		return "", nil, fmt.Errorf("saving API tokens: %w", err)
	}

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
	// LastUsedAt is a "roughly when was this last used" field for the
	// tokens UI, not an audit record -- the audit log is where actions
	// are accounted for. Writing the whole token document on every
	// successful authentication made it behave like one anyway:
	// measured at 0.40 ms per authentication with 50 tokens, under the
	// write lock, on a path a read-only API token hits on every poll.
	// Every one of those writes also went through the file backend's
	// write-temp-then-rename, so it compounded with the shared-temp-name
	// corruption fixed in #287.
	//
	// Persisting only once the recorded value is more than
	// lastUsedGranularity stale keeps the display honest to the minute
	// while collapsing a poll loop's writes to one an hour. The
	// in-memory value is always exact; only the durable copy is
	// coarsened. See #285.
	//
	// Kept on the swallow-and-log persistLocked rather than converted
	// for R6: LastUsedAt is a display convenience, not a credential or
	// grant, so a failed write here costs nothing worth refusing an
	// otherwise-valid authentication over.
	if now.Sub(t.LastUsedAt) >= lastUsedGranularity {
		t.LastUsedAt = now
		s.persistLocked()
	} else {
		t.LastUsedAt = now
	}
	cp := *t
	return &cp, true
}

// lastUsedGranularity is how stale a token's persisted LastUsedAt may
// become before a write is worth it. An hour is far finer than the
// question this field answers ("is this token still in use, or can I
// revoke it?") and coarse enough that a polling client writes once
// rather than continuously.
const lastUsedGranularity = time.Hour

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
	if err := s.tryPersistLocked(); err != nil {
		// A revoke that only exists in memory must not be reported as
		// done: the caller tells its operator the token is dead, and a
		// restart before the next good write would let the raw value
		// authenticate again with nobody the wiser.
		s.byID[id] = t
		s.byHash[t.HashedValue] = id
		return fmt.Errorf("saving API tokens: %w", err)
	}
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
//
// On a persistence failure the deletions are rolled back and the
// returned count is 0: a revoke that only exists in memory must not be
// reported as done, or the caller (handleAuthDeleteUser) would tell its
// operator every one of the deleted account's tokens is dead when a
// restart could bring them all back.
func (s *TokenStore) RevokeAllCreatedBy(userID string) (int, error) {
	if userID == "" {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := make([]*Token, 0)
	for id, t := range s.byID {
		if t.CreatedBy != userID {
			continue
		}
		delete(s.byID, id)
		delete(s.byHash, t.HashedValue)
		removed = append(removed, t)
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := s.tryPersistLocked(); err != nil {
		for _, t := range removed {
			s.byID[t.ID] = t
			s.byHash[t.HashedValue] = t.ID
		}
		return 0, fmt.Errorf("saving API tokens: %w", err)
	}
	return len(removed), nil
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

// ByKind returns every token of kind kind, oldest first -- the same
// copy-and-zero-hash contract List uses, so a caller holding the result
// can never use it to authenticate. Introduced for #1224's droplist-pull
// key: at most one is ever meant to exist, and the admin handlers use
// this to find it (to report its status, or to revoke it once a
// replacement is minted) without listing and filtering every token themselves.
func (s *TokenStore) ByKind(kind TokenKind) []*Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Token
	for _, t := range s.byID {
		if t.Kind != kind {
			continue
		}
		cp := *t
		cp.HashedValue = ""
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// tryPersistLocked is persistLocked's error-returning half, for the
// callers (Create, Revoke, RevokeAllCreatedBy) that issue or revoke a
// token and so must not let the caller believe a write happened when it
// didn't -- see each one's own restore-on-error comment. Authenticate's
// LastUsedAt update keeps using persistLocked below, which keeps
// today's swallow-and-log behaviour: that field is a display
// convenience, not worth failing an otherwise-valid authentication over.
func (s *TokenStore) tryPersistLocked() error {
	if s.backend == nil {
		return nil
	}
	list := make([]*Token, 0, len(s.byID))
	for _, t := range s.byID {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding API tokens for persistence failed: %w", err)
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing API tokens to %s failed: %w", s.backend.Describe(), err)
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("API tokens was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
	return nil
}

// persistLocked is the swallow-and-log default -- see tryPersistLocked's
// doc comment for which callers keep it and why.
func (s *TokenStore) persistLocked() {
	if err := s.tryPersistLocked(); err != nil {
		persistLog.Error(fmt.Sprintf("%v -- this change exists only in memory and will be lost on restart", err))
	}
}
