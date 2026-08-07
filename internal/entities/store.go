// SPDX-License-Identifier: AGPL-3.0-only

// Package entities is the shared primitive behind two roadmap items
// (issue #107): a persisted, admin-manageable record per (entity type,
// key) with an optional friendly label and open-ended tags. A UI-managed
// mail-sender allowlist and UI-managed IP/port/rule aliasing both need
// "a persisted set of tagged/labeled things," not two bespoke stores, so
// this package deliberately stays generic rather than shaped around
// either sibling feature.
//
// Persistence follows the exact convention already established by
// internal/flags.Store and internal/auth.Store: mutex-protected,
// optional atomic-write JSON persistence (an empty StorePath keeps
// everything in-memory only, same as every other optional store in this
// codebase -- see SECURITY.md).
package entities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("entities")

// Known Type values. Type is deliberately a plain string, not a closed
// Go enum with validation against it (contrast flags.Type) -- sibling
// issues extend the set (issue #109 adds TypePort) and this store's job
// is to hold whatever type/key pair a caller gives it, not to gatekeep
// which types exist. TypeHost/TypeRule/TypePort exist only so callers
// that already know their type at compile time don't need a raw string
// literal.
const (
	TypeHost = "host"
	TypeRule = "rule"
	// TypePort keys an entity by a port number formatted as a decimal
	// string (e.g. "8291" for RouterOS's Winbox port), not an int --
	// same "Key is always a string" contract every other Type follows,
	// so Store never needs a type-specific comparison/parse path (see
	// internal/naming.Resolver.Port, which does the int-to-string
	// conversion at the one call site that actually has an int).
	TypePort = "port"
)

// Entity is one persisted record: a friendly label and/or free-form tags
// attached to (Type, Key). Key is whatever the raw value is for that
// Type -- a host IP, a rule label -- there is at most one Entity per
// (Type, Key) pair; a second Upsert with the same pair replaces the
// first rather than creating a duplicate.
type Entity struct {
	Type  string   `json:"type"`
	Key   string   `json:"key"`
	Label string   `json:"label,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

// ErrInvalidEntity is returned by Upsert when Type or Key is empty --
// both are required to form the identity an entity is stored/looked up
// by; Label and Tags are the only genuinely optional fields.
var ErrInvalidEntity = errors.New("entities: type and key are both required")

// ErrInvalidEntityText is returned by Upsert for a key, label or tag
// containing control or format characters, or one that is too long.
var ErrInvalidEntityText = errors.New("entities: key, label and tags must not contain control characters, and must be 256 characters or fewer")

func id(entityType, key string) string {
	return entityType + ":" + key
}

// storeFile is the on-disk shape -- an object wrapping the entity list
// plus a Seeded marker, not a bare array, mirroring auth.Store's own
// storeFile{Disabled, Users} (see internal/auth/store.go). The marker is
// what makes Seed's "should this run" decision independent of whether
// the store happens to be empty *right now* -- without it, an admin
// deleting every entity via the UI (this same package's own Delete,
// exposed through the admin-only DELETE /api/entities endpoint) would
// look, on the next restart, identical to "migration never ran,"
// silently resurrecting the config.yaml aliases they just deliberately
// removed. storeFile.UnmarshalJSON stays compatible with a bare
// `[]*Entity` array -- this package's original on-disk shape, before
// this fix -- for the same reason auth.Store's own legacy-array
// fallback exists: nothing written by an earlier build should fail to
// load. A file recovered via that legacy path decodes with Seeded false
// (the pre-fix shape never recorded it), which is the correct, safe
// interpretation -- it predates this marker entirely, so Seed must be
// free to run once more against it.
type storeFile struct {
	Seeded   bool      `json:"seeded"`
	Entities []*Entity `json:"entities"`
}

func (f *storeFile) UnmarshalJSON(data []byte) error {
	type shape storeFile // avoids infinite recursion into this method
	var s shape
	if err := json.Unmarshal(data, &s); err == nil {
		*f = storeFile(s)
		return nil
	}
	// A top-level JSON array can't unmarshal into a struct -- that's
	// exactly the legacy pre-fix shape, so this is where a genuinely
	// malformed file also gets one more (correct) chance to report its
	// real error, not this fallback's.
	var legacy []*Entity
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	f.Entities = legacy
	f.Seeded = false
	return nil
}

// Store holds every known entity, keyed by (Type, Key). The zero value
// is not usable; construct with Open.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
	byID    map[string]*Entity
	// seeded records whether Seed has already run against this store,
	// persisted so the decision survives a restart -- see storeFile's
	// doc comment and Seed's for why this can't just be inferred from
	// len(byID).
	seeded bool
}

// Open loads path if it exists (a missing file is the expected
// first-run case, not an error) and returns a Store that persists to it
// from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Store is returned. A
// malformed file is treated as empty rather than failing -- a corrupted
// entities file should never block mikroview from starting. Either way
// the returned Store is always safe to use unconditionally; a non-nil
// error is only ever informational, for the caller to log.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured (issue #131).
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, byID: make(map[string]*Entity)}

	data, version, err := persist.LoadDocument(context.Background(), b)
	if err != nil {
		return s, err
	}
	if data == nil {
		return s, nil
	}
	s.version = version

	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return s, err
	}
	for _, e := range file.Entities {
		// A JSON array containing `null` is syntactically valid and
		// unmarshals into a nil *Entity -- skipped here for the same
		// reason flags.Open/auth.Store.Open skip it: a malformed file
		// must never crash startup by indexing through a nil pointer.
		if e == nil {
			continue
		}
		s.byID[id(e.Type, e.Key)] = e
	}
	s.seeded = file.Seeded
	return s, nil
}

// Upsert creates or replaces the entity identified by (e.Type, e.Key).
// Returns ErrInvalidEntity if either is empty -- neither Upsert nor the
// admin-only API layer in front of it ever silently stores a partial
// identity.
func (s *Store) Upsert(e Entity) (Entity, error) {
	if e.Type == "" || e.Key == "" {
		return Entity{}, ErrInvalidEntity
	}
	// Key can arrive from a discovered rule label, which originates in
	// syslog and so is not mikroview's own text; Label is typed by an
	// admin. Both end up in the audit trail and, via the naming
	// resolver, in exported CSV columns, so both are held to the same
	// no-control-characters rule as a username. Setting an entity is
	// admin-only, which lowers the severity but not the reasoning.
	if err := validateEntityText(e.Key); err != nil {
		return Entity{}, err
	}
	if err := validateEntityText(e.Label); err != nil {
		return Entity{}, err
	}
	for _, tag := range e.Tags {
		if err := validateEntityText(tag); err != nil {
			return Entity{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cp := e
	s.byID[id(e.Type, e.Key)] = &cp
	s.persistLocked()

	out := cp
	return out, nil
}

// Delete removes the entity identified by (entityType, key). Reports
// whether an entity was actually found and removed -- deleting an
// unknown pair is a no-op, not an error, same "caller might be looking
// at a stale list" reasoning flags.Store.Clear already documents.
func (s *Store) Delete(entityType, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := id(entityType, key)
	if _, ok := s.byID[k]; !ok {
		return false
	}
	delete(s.byID, k)
	s.persistLocked()
	return true
}

// List returns every known entity, sorted by (Type, Key) for a stable,
// deterministic order across calls.
func (s *Store) List() []Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Entity {
	out := make([]Entity, 0, len(s.byID))
	for _, e := range s.byID {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Count returns the number of known entities -- a general-purpose
// utility for a caller that wants to know the store's size (e.g. a
// future admin UI affordance). Not used by Seed, which is gated by the
// persisted Seeded marker instead -- see Seed's own doc comment for why
// "currently empty" isn't a safe proxy for "never seeded."
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}

// Label returns the label set for (entityType, key), or "" if no
// matching entity exists or it has no label set. A direct O(1) map
// lookup rather than routing through List() (which copies and sorts
// every entity on every call) -- this is meant to be cheap enough to
// call on internal/naming's per-event hot path (see naming.Resolver),
// where an admin-managed entity label takes precedence over
// config.yaml's static ruleNames/hostNames maps.
func (s *Store) Label(entityType, key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[id(entityType, key)]
	if !ok {
		return ""
	}
	return e.Label
}

// HasTag reports whether the entity at (entityType, key) has tag among
// its Tags -- a small membership helper so a caller that only needs a
// yes/no answer (e.g. internal/detect's mail-sender allowlist, issue
// #108) doesn't have to reimplement a linear List() scan at each call
// site. A missing entity, or one with no matching tag, both report
// false. Same direct O(1)-lookup-then-linear-tag-scan shape as Label
// above -- Tags lists are expected to be small (a handful at most).
func (s *Store) HasTag(entityType, key, tag string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[id(entityType, key)]
	if !ok {
		return false
	}
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Seed imports config.yaml's legacy RuleNames/HostNames maps
// (internal/config.Config.RuleNames/HostNames) as Entity records, but
// only the first time it's ever called against a given persisted store
// -- the one-time upgrade path documented in issue #107: an existing
// deployment's YAML-only aliases become UI-editable Entity records on
// first boot after upgrading, without losing them.
//
// Gated on the persisted Seeded marker (see storeFile), deliberately
// *not* on "is the store currently empty" -- an admin can delete every
// entity one at a time via the UI (Delete, behind DELETE
// /api/entities), which leaves the store genuinely empty on purpose.
// Inferring "never seeded" from "empty" would make that indistinguishable
// from a fresh store on the next restart (main.go calls Seed
// unconditionally on every boot) and silently resurrect exactly the
// aliases the admin just removed. Once seeded, it stays seeded --
// regardless of whether anything was actually imported (e.g. both maps
// were empty) or how many entities exist afterward -- so this is safe
// and cheap to call unconditionally on every startup. Returns the
// number of entities imported (0 once already seeded).
func (s *Store) Seed(ruleNames, hostNames map[string]string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.seeded {
		return 0
	}
	s.seeded = true

	imported := 0
	for key, label := range ruleNames {
		if key == "" {
			continue
		}
		s.byID[id(TypeRule, key)] = &Entity{Type: TypeRule, Key: key, Label: label}
		imported++
	}
	for key, label := range hostNames {
		if key == "" {
			continue
		}
		s.byID[id(TypeHost, key)] = &Entity{Type: TypeHost, Key: key, Label: label}
		imported++
	}

	// Always persist, even when nothing was imported -- what's being
	// recorded is "the migration decision point has already passed,"
	// not "something was imported," so a later restart must never
	// re-evaluate it either way (e.g. an operator adding ruleNames to
	// config.yaml well after the first-ever boot must not have them
	// suddenly appear -- Seed's contract is first-boot-only, not
	// "import whatever's configured until the store has something").
	s.persistLocked()
	return imported
}

// persistLocked writes the current state to disk if persistence is
// configured. Unlike flags.Store's persistLocked, there's no debounce
// interval here -- entity mutations are rare, admin-only, interactive
// actions (add/edit/remove one record at a time), not a high-rate
// detection hot path, so there's nothing to rate-limit. Write failures
// are swallowed rather than surfaced to Upsert/Delete/Seed's callers:
// the in-memory state (which every read goes through) stays correct
// either way, so a transient disk issue degrades to "won't survive a
// restart right now" rather than breaking live use.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	list := s.listLocked()
	ptrs := make([]*Entity, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	data, err := json.MarshalIndent(storeFile{Seeded: s.seeded, Entities: ptrs}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding entities for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing entities to %s failed: %v -- this change exists only in memory and will be lost on restart",
			s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("entity store was modified by another process while this change was pending (%s); this change was applied on top",
			s.backend.Describe()))
	}
	s.version = version
}

// maxEntityTextLength bounds a key, label or tag. Generous next to any
// real rule label or hostname, tight enough that the audit trail and
// the UI table stay renderable.
const maxEntityTextLength = 256

// validateEntityText rejects text that stops being text downstream.
//
// Control characters (ANSI escapes, newlines) and Unicode format
// characters (the bidi overrides that let a string render in an order
// other than the one it is stored in) are refused for the same reasons
// they are refused in a username -- see auth.ValidateUsername, which
// carries the full reasoning and the CVE references.
//
// An empty string is allowed: Label and Tags are optional, and Upsert
// checks Key separately.
func validateEntityText(s string) error {
	if !utf8.ValidString(s) {
		return ErrInvalidEntityText
	}
	if utf8.RuneCountInString(s) > maxEntityTextLength {
		return ErrInvalidEntityText
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrInvalidEntityText
		}
	}
	return nil
}
