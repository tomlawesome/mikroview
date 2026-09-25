// SPDX-License-Identifier: AGPL-3.0-only

// Package coverage stores coverage-gap declarations (issue #630/#392):
// an admin's deliberate record that a specific boundary-direction pair
// -- e.g. "ether1|bridge1", a firewall boundary crossed in a given
// direction -- is intentionally not expected to log anything, so its
// silence should read as "declared quiet" rather than as an unexplained
// gap in detection coverage. This is documentation of an operator
// decision, not a detector or a store the engine evaluates: nothing here
// changes what fires, only what a human reviewing coverage gaps sees
// next to one.
//
// Persistence follows the same convention as internal/entities.Store
// and internal/flags.Store: mutex-protected, optional JSON persistence
// through internal/persist (an empty StorePath keeps everything
// in-memory only, same as every other optional store in this codebase --
// see SECURITY.md). Declarations are rare, admin-only, interactive
// writes -- not a high-rate hot path -- so, like internal/entities, this
// persists synchronously on every mutation via persist.SaveWithRetry
// rather than flags.Store's rate-limited write-behind, which exists
// specifically to keep a detection hot path off disk I/O.
package coverage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("coverage")

// maxKeyLength/maxReasonLength bound a declaration's Key and Reason --
// generous next to any real boundary-direction pair or human-written
// justification, tight enough that the admin UI and audit trail stay
// renderable. Same style of ceiling as internal/entities'
// maxEntityTextLength.
const (
	maxKeyLength    = 120
	maxReasonLength = 400
)

// ErrInvalidDeclaration is returned by Put when Key or Reason fails
// validation: empty, too long, not valid UTF-8, or containing a control
// or Unicode format character (the same no-control-characters rule
// internal/entities.validateEntityText and auth.ValidateUsername apply,
// since Key and Reason both flow into the admin UI and the audit trail).
var ErrInvalidDeclaration = errors.New("coverage: key and reason are required, must be valid text, and must respect the length limit")

// Declaration is one persisted, human-authored record that a boundary-
// direction pair is intentionally not expected to log anything. Key is
// the boundary-direction pair itself (e.g. "ether1|bridge1") -- an
// opaque, caller-defined string, not parsed or validated for shape by
// this package, since what counts as a boundary and a direction is a
// decision made by whatever calls Put, not by this store.
type Declaration struct {
	Key        string    `json:"key"`
	Reason     string    `json:"reason"`
	DeclaredBy string    `json:"declaredBy"`
	DeclaredAt time.Time `json:"declaredAt"`
}

// storeFile is the on-disk shape: an object wrapping the declaration
// list, mirroring internal/entities' storeFile.
type storeFile struct {
	Declarations []*Declaration `json:"declarations"`
}

// Store holds every known coverage-gap declaration, keyed by Key. The
// zero value is not usable; construct with Open.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
	byKey   map[string]*Declaration
}

// Open loads path if it exists (a missing file is the expected
// first-run case, not an error) and returns a Store that persists to it
// from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Store is returned. A
// document that exists but cannot be read or parsed is a hard error,
// same "don't let a live backend silently overwrite an unparseable
// document" contract as internal/entities.Open and internal/flags.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, byKey: make(map[string]*Declaration)}

	version, existed, err := persist.Open(context.Background(), b, "the coverage declarations store", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, d := range file.Declarations {
			// A JSON array containing `null` is syntactically valid and
			// unmarshals into a nil *Declaration -- skipped here so a
			// malformed entry can't crash startup by indexing through a
			// nil pointer, same defensive handling as
			// internal/entities.OpenWithBackend.
			if d == nil || d.Key == "" {
				continue
			}
			s.byKey[d.Key] = d
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

// Put creates or replaces the declaration at key, with declaredAt set
// server-side to now -- a caller never gets to backdate a declaration.
// Returns ErrInvalidDeclaration if key or reason fails validateText.
func (s *Store) Put(key, reason, declaredBy string) (Declaration, error) {
	if err := validateText(key, maxKeyLength); err != nil {
		return Declaration{}, err
	}
	if err := validateText(reason, maxReasonLength); err != nil {
		return Declaration{}, err
	}
	if key == "" || reason == "" {
		return Declaration{}, ErrInvalidDeclaration
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous, existed := s.byKey[key]
	d := Declaration{Key: key, Reason: reason, DeclaredBy: declaredBy, DeclaredAt: time.Now()}
	s.byKey[key] = &d
	if err := s.tryPersistLocked(); err != nil {
		// A coverage-gap declaration that cannot be saved must not read
		// back as declared (R6): put the previous one back (or drop the
		// key entirely for a brand new declaration) rather than leave
		// this write only in memory for a restart to discard silently.
		if existed {
			s.byKey[key] = previous
		} else {
			delete(s.byKey, key)
		}
		return Declaration{}, fmt.Errorf("saving coverage declarations: %w", err)
	}
	return d, nil
}

// Delete removes the declaration at key. Reports whether one was
// actually found and removed -- deleting an unknown key is a no-op, not
// an error, same "caller might be looking at a stale list" reasoning
// internal/entities.Store.Delete and internal/flags.Store.Clear already
// document.
//
// Returns an error (rather than only the found/removed bool) when the
// key existed but the removal could not be saved -- see the
// restore-on-error comment inside for why (R6).
func (s *Store) Delete(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.byKey[key]
	if !ok {
		return false, nil
	}
	delete(s.byKey, key)
	if err := s.tryPersistLocked(); err != nil {
		// An undeclare that cannot be saved must not read as undeclared
		// (R6): put the declaration back rather than report success and
		// have it reappear, un-deleted, after the next restart.
		s.byKey[key] = existing
		return false, fmt.Errorf("saving coverage declarations: %w", err)
	}
	return true, nil
}

// List returns every known declaration, sorted by Key for a stable,
// deterministic order across calls.
func (s *Store) List() []Declaration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Declaration {
	out := make([]Declaration, 0, len(s.byKey))
	for _, d := range s.byKey {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// tryPersistLocked is persistLocked's error-returning half, for Put and
// Delete -- every mutator on this store, both of which change an
// operator's on-record coverage-gap declaration, and so must not let a
// caller believe a write happened when it didn't (v0.6.0 audit finding
// R6) -- see each one's own restore-on-error comment. No debounce
// interval, same reasoning as internal/entities.Store.tryPersistLocked:
// declarations are rare, admin-only, interactive writes, not a
// high-rate hot path, so there is nothing to rate-limit.
func (s *Store) tryPersistLocked() error {
	if s.backend == nil {
		return nil
	}
	list := s.listLocked()
	ptrs := make([]*Declaration, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	data, err := json.MarshalIndent(storeFile{Declarations: ptrs}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding coverage declarations for persistence failed: %w", err)
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing coverage declarations to %s failed: %w", s.backend.Describe(), err)
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("coverage declarations store was modified by another process while this change was pending (%s); this change was applied on top",
			s.backend.Describe()))
	}
	s.version = version
	return nil
}

// validateText rejects an empty string, text over maxLen runes, invalid
// UTF-8, or control/Unicode-format characters (the bidi overrides that
// let a string render in an order other than the one it is stored in) --
// same reasoning internal/entities.validateEntityText and
// auth.ValidateUsername apply, since Key and Reason both flow into the
// admin UI and the audit trail.
func validateText(s string, maxLen int) error {
	if s == "" {
		return ErrInvalidDeclaration
	}
	if !utf8.ValidString(s) {
		return ErrInvalidDeclaration
	}
	if utf8.RuneCountInString(s) > maxLen {
		return ErrInvalidDeclaration
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrInvalidDeclaration
		}
	}
	return nil
}
