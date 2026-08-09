// SPDX-License-Identifier: AGPL-3.0-only

// Package watchlist is the operator-owned entry set #243 grows Control
// Ports into: a persisted, admin-manageable list of (source,
// destination, port set) tuples to record attempts against, replacing
// the single flat criticalPorts port list every operator previously
// shared regardless of what they actually wanted watched.
//
// This slice ships non-inverted matching only -- "record attempts
// against these ports," a direct generalisation of what
// ControlPorts.svelte already does client-side, now evaluated
// server-side against every ingested event and persisted via
// internal/matchlog instead of only ever existing in a 5,000-event
// client buffer. The invert toggle ("this device should only ever reach
// X") and the observe/promote workflow are #243's slice 3, not this one
// -- Entry deliberately has no Invert field yet, so a partially-working
// invert state can't exist to confuse an operator; adding it lands
// alongside its own matching logic, not ahead of it.
//
// There is also, as yet, no way for an operator to create an Entry: this
// slice proves the persistence and matching machinery end to end via
// Store's Go API and tests, but the HTTP API and UI that would let an
// operator actually add one are #243's slice 4. Until then this package
// is fully wired into the live ingest path (see main.go) and fully
// inert in practice, the same as internal/matchlog was after slice 1 --
// an empty Store matches nothing, so there is nothing to observe.
package watchlist

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
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/store"
)

var persistLog = logging.New("watchlist")

// Entry is one watchlist entry. Source and DestIP are both optional --
// zero-value means unscoped ("any source"/"any destination"), which is
// what makes this a strict superset of today's Control Ports capability
// (port-only scoping) rather than a stricter replacement for it: an
// operator who wants exactly today's behaviour for a given port set
// leaves both unset.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Source is optional identity scoping (#243 section 1's
	// MAC-preferred, IP-fallback rule -- see matchlog.Identity). Empty
	// matches any source.
	Source matchlog.Identity `json:"source,omitempty"`
	// DestIP is optional destination scoping. Empty matches any
	// destination.
	DestIP string `json:"destIp,omitempty"`
	// Ports is the set of destination ports this entry watches.
	// Required -- an entry with no ports would never match anything,
	// which is indistinguishable from a mistake, so Upsert refuses it
	// (see ErrNoPorts).
	Ports     []int     `json:"ports"`
	CreatedAt time.Time `json:"createdAt"`
}

// ErrInvalidEntry is returned by Upsert for an entry with no ID.
var ErrInvalidEntry = errors.New("watchlist: an entry must have an id")

// ErrNoPorts is returned by Upsert for an entry with an empty Ports --
// see Entry.Ports.
var ErrNoPorts = errors.New("watchlist: an entry must watch at least one port")

// ErrInvalidText is returned by Upsert for a Name, DestIP or Source
// field containing control or format characters, or one that is too
// long -- the same contract internal/entities.Upsert enforces on its own
// free-text fields, for the same reason: these values render directly in
// the UI and land in a persisted file an admin can read back.
var ErrInvalidText = errors.New("watchlist: name, destIp and source fields must not contain control characters, and must be 256 characters or fewer")

// maxTextLen and validText mirror internal/entities.validateEntityText
// exactly (control/format characters, malformed UTF-8, length) -- the
// same reasoning applies unchanged: these values render directly in the
// UI (#243 slice 4) and land in a persisted file an admin can read back.
const maxTextLen = 256

func validText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	if utf8.RuneCountInString(s) > maxTextLen {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// storeFile is the on-disk shape, mirroring internal/entities.storeFile.
type storeFile struct {
	Entries []*Entry `json:"entries"`
}

// maxEntries bounds the store generously -- entries are operator-created
// one at a time through a UI (#243 slice 4), not a high-rate hot path, so
// this is a safety net mirroring internal/flags.Store's maxFlags, not a
// limit expected to be hit in normal use.
var maxEntries = 10000

// Store holds every watchlist entry. The zero value is not usable;
// construct with Open or OpenWithBackend.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	version int64
	entries map[string]*Entry
}

// Open loads path if it exists (a missing file is the expected first-run
// case) and returns a Store that persists to it from then on. An empty
// path keeps everything in-memory only, the same optional-persistence
// contract every other small store in this codebase follows.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, entries: make(map[string]*Entry)}

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
	for _, e := range file.Entries {
		s.entries[e.ID] = e
	}
	return s, nil
}

// List returns every entry, sorted by ID for a stable, deterministic
// order -- callers that want a different order (creation time, name)
// sort the result themselves rather than this store guessing which
// order a caller wants.
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the entry with the given ID, or false if none exists.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// Upsert creates or replaces the entry at e.ID, setting CreatedAt only
// on first creation -- an update must not reset how long an entry has
// existed. Rejects an entry with no ID, no ports, or invalid text before
// it ever reaches disk, the same "refuse malformed data at the write
// boundary" contract internal/entities.Upsert follows.
func (s *Store) Upsert(e Entry) error {
	if e.ID == "" {
		return ErrInvalidEntry
	}
	if len(e.Ports) == 0 {
		return ErrNoPorts
	}
	for _, text := range []string{e.Name, e.DestIP, e.Source.MAC, e.Source.IP} {
		if !validText(text) {
			return ErrInvalidText
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.entries[e.ID]
	if !exists && len(s.entries) >= maxEntries {
		return fmt.Errorf("watchlist: at the %d-entry limit", maxEntries)
	}
	if exists {
		e.CreatedAt = existing.CreatedAt
	} else if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	cp := e
	s.entries[e.ID] = &cp
	s.persistLocked()
	return nil
}

// Delete removes the entry with the given ID. Deleting an ID that
// doesn't exist is a no-op, not an error -- the caller's intent (this ID
// should not be in the store) is already satisfied.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return
	}
	delete(s.entries, id)
	s.persistLocked()
}

func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	entries := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	data, err := json.MarshalIndent(storeFile{Entries: entries}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the watchlist for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing the watchlist to %s failed: %v -- this change exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("the watchlist was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}

// isTrackableConnState mirrors internal/detect's own private copy of the
// same check (detect.go's isTrackableConnState) and
// ControlPorts.svelte's client-side equivalent: without it, a busy
// accepted service's own return traffic would swamp a watchlist entry
// the same way it would the fast port-scan detector. Not shared as a
// single exported helper across packages -- same "each package keeps its
// own small private copy" precedent internal/detect's own doc comment on
// isPublic already sets for this codebase.
func isTrackableConnState(e store.Event) bool {
	return e.ConnState == "" || e.ConnState == "new"
}

// eventIdentity resolves an event's source identity the same
// MAC-preferred, IP-fallback way matchlog.Identity.MatchesSource
// compares against: SrcMAC when the parser found one (only the forward
// chain reliably carries it), SrcIP otherwise.
func eventIdentity(e store.Event) matchlog.Identity {
	return matchlog.Identity{MAC: e.SrcMAC, IP: e.SrcIP}
}

// Match reports whether e matches entry (non-inverted: "record attempts
// against these ports"), and if so the matchlog.Tuple to record it
// under. Ports must contain e.DstPort; ConnState must be trackable (see
// isTrackableConnState); Source, if the entry scopes it, must match the
// event's own resolved identity; DestIP, if the entry scopes it, must
// equal e.DstIP.
//
// The returned Tuple always carries the event's own real, specific
// identity -- never the entry's (possibly empty/unscoped) Source -- so
// an unscoped entry watching many devices still produces one matchlog
// record per device, not one shared record every device's traffic
// collapses into.
func Match(entry Entry, e store.Event) (matchlog.Tuple, bool) {
	if e.DstPort == 0 || !containsPort(entry.Ports, e.DstPort) {
		return matchlog.Tuple{}, false
	}
	if !isTrackableConnState(e) {
		return matchlog.Tuple{}, false
	}
	id := eventIdentity(e)
	if id.Empty() {
		// Nothing to record a match under -- see matchlog.ErrEmptyIdentity.
		// A chain with neither src-mac nor a usable source IP cannot be
		// attributed to a device at all.
		return matchlog.Tuple{}, false
	}
	if !entry.Source.Empty() && !entry.Source.MatchesSource(id) {
		return matchlog.Tuple{}, false
	}
	if entry.DestIP != "" && entry.DestIP != e.DstIP {
		return matchlog.Tuple{}, false
	}
	return matchlog.Tuple{Source: id, DestIP: e.DstIP, Port: e.DstPort}, true
}

func containsPort(ports []int, port int) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}
