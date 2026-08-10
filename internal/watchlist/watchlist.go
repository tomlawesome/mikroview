// SPDX-License-Identifier: AGPL-3.0-only

// Package watchlist is the operator-owned entry set #243 grew Control
// Ports into: a persisted, admin-manageable list of (source,
// destination, port set) tuples, replacing the single flat
// criticalPorts port list every operator previously shared regardless
// of what they actually wanted watched. Two matching modes:
//
//   - Non-inverted: "record attempts against these ports" -- a direct
//     generalisation of what the old Control Ports tab did client-side,
//     now evaluated server-side against every ingested event and
//     persisted via internal/matchlog instead of only ever existing in
//     a 5,000-event client buffer.
//   - Inverted: "this device should only ever reach X" -- egress-policy
//     monitoring. A new inverted entry starts in an observe state,
//     recording every distinct destination the device touches without
//     firing anything; the operator promotes what should be permitted,
//     and everything else becomes a fireable violation from then on.
//     Structural noise (broadcast/multicast/link-local) is exempt by
//     default. See invert.go.
//
// An operator manages entries through the HTTP API (internal/api/
// watchlist.go: entry CRUD, promote, observing toggle, and the match
// query) and the admin-only Watchlist page in the UI
// (frontend/src/components/Watchlist.svelte) -- #243's slice 4. This
// package itself remains fully wired into the live ingest path (see
// main.go) and, with no entries configured, provably inert -- an empty
// Store matches nothing.
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

// Entry is one watchlist entry. Source and DestIP are both optional for
// a non-inverted entry -- zero-value means unscoped ("any source"/"any
// destination"), which is what makes non-inverted matching a strict
// superset of today's Control Ports capability (port-only scoping)
// rather than a stricter replacement for it. An inverted entry is about
// one specific device's expected behaviour, so Source is required for
// it (see ErrInvertedRequiresSource) and Ports is unused.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Source is optional identity scoping for a non-inverted entry
	// (#243 section 1's MAC-preferred, IP-fallback rule -- see
	// matchlog.Identity), empty meaning "any source". Required,
	// non-empty, for an inverted entry -- see Invert.
	Source matchlog.Identity `json:"source,omitempty"`
	// DestIP is optional destination scoping for a non-inverted entry.
	// Empty matches any destination. Unused when Invert is true -- an
	// inverted entry's destinations are its Permitted set instead.
	DestIP string `json:"destIp,omitempty"`
	// Ports is the set of destination ports a non-inverted entry
	// watches. Required for a non-inverted entry -- an entry with no
	// ports would never match anything, which is indistinguishable from
	// a mistake, so Upsert refuses it (see ErrNoPorts). Unused when
	// Invert is true: an inverted entry watches every port its device
	// touches, since the question is "did it reach somewhere
	// unexpected," not "did it use a particular port."
	Ports []int `json:"ports,omitempty"`

	// Invert switches this entry from "record attempts against these
	// ports" to "this device should only ever reach the destinations in
	// Permitted" -- see invert.go for the matching rule, and this
	// package's doc comment for the design.
	Invert bool `json:"invert,omitempty"`
	// Observing is only meaningful when Invert is true. While true,
	// nothing this entry sees fires as a violation -- distinct
	// destinations are recorded into Observed instead (see
	// Store.RecordObservation), for the operator to review and promote.
	// A new inverted entry starts Observing; Store.SetObserving is the
	// mechanism to leave that state, on whatever cadence an operator (or
	// slice 4's UI) decides -- this package makes no judgement about
	// when that should happen (#243 open question 3).
	Observing bool `json:"observing,omitempty"`
	// IncludeStructuralNoise opts an inverted entry INTO evaluating
	// non-unicast destinations (broadcast/multicast/link-local), which
	// are exempt by default -- see invert.go's isStructurallyExempt.
	// Unused for a non-inverted entry.
	IncludeStructuralNoise bool `json:"includeStructuralNoise,omitempty"`
	// Permitted is an inverted entry's promoted allow-list: a
	// destination/port pair in here never fires, no matter how it got
	// there (explicitly permitted by the operator, or promoted out of
	// Observed). Unused for a non-inverted entry.
	Permitted []PermittedDest `json:"permitted,omitempty"`
	// Observed is an inverted entry's candidate list while Observing --
	// every distinct destination/port the device has touched that isn't
	// already Permitted, with first/last-seen and a count (the same
	// evidence shape matchlog.Record uses, so "how often" is visible
	// before deciding). Capped at maxObservedPerEntry; see
	// Store.RecordObservation for what happens once full. Unused for a
	// non-inverted entry.
	Observed []ObservedDest `json:"observed,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

// PermittedDest is one destination/port pair an inverted entry's device
// is allowed to reach.
type PermittedDest struct {
	DestIP string `json:"destIp"`
	Port   int    `json:"port"`
}

// ObservedDest is one destination/port pair seen while an inverted entry
// was Observing, not yet promoted or dismissed.
type ObservedDest struct {
	DestIP    string    `json:"destIp"`
	Port      int       `json:"port"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Count     uint64    `json:"count"`
}

// ErrInvalidEntry is returned by Upsert for an entry with no ID.
var ErrInvalidEntry = errors.New("watchlist: an entry must have an id")

// ErrNoPorts is returned by Upsert for a non-inverted entry with an
// empty Ports -- see Entry.Ports.
var ErrNoPorts = errors.New("watchlist: a non-inverted entry must watch at least one port")

// ErrInvertedRequiresSource is returned by Upsert for an inverted entry
// with no Source -- see Entry.Invert. An inverted entry with no device
// to scope it would mean "nothing in particular should reach anything in
// particular," which isn't a coherent policy to enforce.
var ErrInvertedRequiresSource = errors.New("watchlist: an inverted entry must scope a source device")

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
// existed. Rejects an entry with no ID, invalid text, or a scoping
// requirement its mode doesn't satisfy (non-inverted: at least one port;
// inverted: a source device) before it ever reaches disk, the same
// "refuse malformed data at the write boundary" contract
// internal/entities.Upsert follows.
func (s *Store) Upsert(e Entry) error {
	if e.ID == "" {
		return ErrInvalidEntry
	}
	if e.Invert {
		if e.Source.Empty() {
			return ErrInvertedRequiresSource
		}
	} else if len(e.Ports) == 0 {
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

// Reset wipes every entry -- the watchlist half of #243 slice 5's "nuke"
// action: a deliberate, confirm-gated, fully destructive reset back to a
// fresh look at the router. The suggestion candidate tracking
// (internal/suggest) is a separate store; the caller (internal/api) is
// responsible for wiping both together, since nuking one without the
// other would leave every candidate pointing at an EntryID that no
// longer exists.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*Entry)
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
// same check (detect.go's isTrackableConnState): without it, a busy
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

// Outcome is what evaluating one event against one entry decided.
type Outcome int

const (
	// NoMatch: this entry has nothing to say about this event -- wrong
	// port, wrong source, a permitted inverted destination, or any of
	// the other reasons covered below. The Evaluator takes no action.
	NoMatch Outcome = iota
	// Violation: record this to internal/matchlog -- a non-inverted
	// entry's watched port was reached, or an inverted entry's device
	// reached somewhere neither permitted nor still being observed.
	Violation
	// Observed: an inverted entry, still Observing, saw its device reach
	// a destination that is neither permitted nor dismissed yet. The
	// Evaluator records this as a candidate (Store.RecordObservation)
	// rather than a violation -- nothing fires while observing.
	Observed
)

// Match decides what entry has to say about e, and the matchlog.Tuple to
// record it under if the outcome is Violation or Observed. Dispatches on
// entry.Invert -- see matchNonInverted (this file) and matchInverted
// (invert.go) for the two rules.
//
// The returned Tuple always carries the event's own real, specific
// identity -- never the entry's (possibly empty/unscoped for a
// non-inverted entry) Source -- so an unscoped entry watching many
// devices still produces one matchlog record per device, not one shared
// record every device's traffic collapses into.
func Match(entry Entry, e store.Event) (matchlog.Tuple, Outcome) {
	if entry.Invert {
		return matchInverted(entry, e)
	}
	return matchNonInverted(entry, e)
}

// matchNonInverted implements "record attempts against these ports":
// Ports must contain e.DstPort; ConnState must be trackable (see
// isTrackableConnState); Source, if the entry scopes it, must match the
// event's own resolved identity; DestIP, if the entry scopes it, must
// equal e.DstIP. Only ever returns NoMatch or Violation -- there is no
// observe state for a non-inverted entry.
func matchNonInverted(entry Entry, e store.Event) (matchlog.Tuple, Outcome) {
	if e.DstPort == 0 || !containsPort(entry.Ports, e.DstPort) {
		return matchlog.Tuple{}, NoMatch
	}
	if !isTrackableConnState(e) {
		return matchlog.Tuple{}, NoMatch
	}
	id := eventIdentity(e)
	if id.Empty() {
		// Nothing to record a match under -- see matchlog.ErrEmptyIdentity.
		// A chain with neither src-mac nor a usable source IP cannot be
		// attributed to a device at all.
		return matchlog.Tuple{}, NoMatch
	}
	if !entry.Source.Empty() && !entry.Source.MatchesSource(id) {
		return matchlog.Tuple{}, NoMatch
	}
	if entry.DestIP != "" && entry.DestIP != e.DstIP {
		return matchlog.Tuple{}, NoMatch
	}
	return matchlog.Tuple{Source: id, DestIP: e.DstIP, Port: e.DstPort}, Violation
}

func containsPort(ports []int, port int) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}
