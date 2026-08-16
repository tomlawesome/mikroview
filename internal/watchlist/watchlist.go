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

	// SourceList scopes a non-inverted entry to whatever addresses are
	// in a router's own address list *at the moment an event arrives*,
	// rather than to one fixed address (#274 item 2).
	//
	// This is the piece that could not be built before. Source above is
	// a stored identity, decided when the entry is created; an address
	// list is edited on the router, often by the router itself
	// (RouterOS adds dynamic entries from its own rules), so an entry
	// scoped to one is only meaningful if membership is resolved live.
	// Expanding the list into fixed entries at creation time was the
	// alternative and is exactly wrong: it would be stale the first time
	// the list changed, silently.
	//
	// Empty means unused, and Source applies instead. The two are not
	// combined: an entry is scoped by an identity or by a list, and
	// silently intersecting them would make "no matches" ambiguous in a
	// way this package works hard to avoid.
	SourceList AddressListRef `json:"sourceList,omitzero"`

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
	mu sync.RWMutex
	// wb is nil when persistence isn't configured -- see
	// persist.WriteBehind for what it now owns: write-behind, the
	// backend deadline, the after-write-stamped rate limit/back-off, and
	// version bookkeeping (issue #400). Every method on it is a safe
	// no-op on a nil receiver. Before #400 this store persisted every
	// mutation synchronously, unconditionally, under context.Background()
	// with no deadline -- #380's first item.
	wb      *persist.WriteBehind
	entries map[string]*Entry
}

// watchlistPersistMinInterval rate-limits the write-behind writer's
// actual backend attempts. Most callers here are operator-interactive
// (Upsert/Delete/Reset, through the admin-only Watchlist page), but
// RecordObservation runs from Evaluator on every Observed match --
// see its own doc comment for why it already avoids persisting a bare
// Count/LastSeen bump. A short interval, same reasoning
// detect.settingsPersistMinInterval gives: this store never had any
// debounce at all before #400 (every mutation persisted synchronously),
// so a short interval coalesces a burst of admin edits or observations
// without meaningfully delaying anything a human is watching. A var so
// tests that need every call to persist immediately can shrink it, same
// convention as flags.persistMinInterval.
var watchlistPersistMinInterval = 200 * time.Millisecond

// Open loads path if it exists (a missing file is the expected first-run
// case) and returns a Store that persists to it from then on. An empty
// path keeps everything in-memory only, the same optional-persistence
// contract every other small store in this codebase follows. A document
// that exists but cannot be read or parsed is a hard error (issue #378):
// the caller gets (nil, err) rather than a store whose live backend
// would overwrite that document on the first write. See persist.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{entries: make(map[string]*Entry)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the watchlist", persist.WriteBehindOptions{
		MinInterval: watchlistPersistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, e := range file.Entries {
			// A JSON array containing `null` is syntactically valid and
			// unmarshals into a nil *Entry -- skipped here so a
			// malformed entry can't crash startup by indexing through a
			// nil pointer (same guard every sibling store applies).
			if e == nil {
				continue
			}
			s.entries[e.ID] = e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// Flush forces this store's write-behind writer to persist whatever is
// currently dirty now, without waiting out its usual debounce interval,
// and blocks until that attempt finishes or ctx expires -- see
// flags.Store.Flush's own doc comment for when this is the right call
// (a test, or a `-backup` CLI invocation racing a still-running
// process). A store with no backend configured (wb == nil) is a safe
// no-op.
func (s *Store) Flush(ctx context.Context) error {
	return s.wb.Flush(ctx)
}

// Close stops this store's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A store with no backend configured (wb == nil)
// is a safe no-op. Not safe to call any mutating method after Close.
func (s *Store) Close(ctx context.Context) error {
	return s.wb.Close(ctx)
}

// List returns every entry, sorted by ID for a stable, deterministic
// order -- callers that want a different order (creation time, name)
// sort the result themselves rather than this store guessing which
// order a caller wants.
func (s *Store) List() []Entry {
	out := s.entriesSnapshot()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// entriesSnapshot is List without the sort, for evaluateRecovered's
// per-event Match loop -- Match doesn't care about order, so the
// sort.Slice call List() does purely for API/UI stability was dead
// weight on that path, measured at up to 4.3ms/event at 5,000 entries.
// Still copies every entry rather than returning s.entries directly:
// the caller (evaluateRecovered) iterates the result after this
// returns, without holding s.mu, because it calls RecordObservation
// mid-loop, which takes its own Lock -- holding RLock across that call
// would deadlock.
func (s *Store) entriesSnapshot() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, *e)
	}
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

// persistLocked encodes the current state and hands it to the
// write-behind writer (see persist.WriteBehind), which coalesces it
// with whatever else is pending and persists it off this goroutine,
// under its own deadline and rate limit -- closing #380's first item
// for this store (every persist call used to run under
// context.Background() with no deadline, synchronously, on whichever
// goroutine called Upsert/Delete/Reset/RecordObservation/Promote/
// SetObserving). Marshal failures are swallowed rather than surfaced to
// the caller: the in-memory state (which every read goes through) stays
// correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than breaking live use. Must be
// called with s.mu already held -- see flags.Store.persistLocked's own
// doc comment for the "lock covers the encode, not the backend call"
// contract this mirrors.
func (s *Store) persistLocked() {
	if s.wb == nil {
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
	s.wb.MarkDirty(data)
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
// compares against: SrcMAC when the parser found one, SrcIP otherwise.
//
// Which chains carry src-mac is a property of the firmware, not
// something to rely on: on a real RouterOS 7.23.3 both forward and input
// carry it (#273), while output -- traffic the router originates, so
// there is no incoming frame to read a source MAC from -- does not. The
// IP fallback is what makes that not matter here.
func eventIdentity(e store.Event) matchlog.Identity {
	return matchlog.Identity{MAC: e.SrcMAC, IP: e.SrcIP}
}

// AddressListRef names one router's address list.
//
// Device as well as List because an address list belongs to a router:
// two routers can both have a "mgmt" list meaning entirely different
// things, and a watchlist entry that silently matched either would be
// answering a question nobody asked.
type AddressListRef struct {
	Device string `json:"device,omitempty"`
	List   string `json:"list,omitempty"`
}

func (r AddressListRef) Empty() bool { return r.Device == "" || r.List == "" }

// AddressListMembership answers whether an address is in a router's
// address list right now.
//
// A local interface rather than an import of internal/routerstate, the
// same dependency-direction reasoning internal/syslog uses for its
// certificate source and internal/oidc for its config. It also keeps
// matching testable without standing up a router-state store.
type AddressListMembership interface {
	InAddressList(device, list, ip string) bool
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
	return MatchWithLists(entry, e, nil)
}

// MatchWithLists is Match with a way to resolve address-list membership
// (#274 item 2). members may be nil, in which case an entry scoped to a
// list matches nothing -- which is the safe direction: without a way to
// answer "is this address in that list", the honest answer is not to
// record a match against an entry whose scope cannot be evaluated.
func MatchWithLists(entry Entry, e store.Event, members AddressListMembership) (matchlog.Tuple, Outcome) {
	if entry.Invert {
		return matchInverted(entry, e)
	}
	return matchNonInverted(entry, e, members)
}

// matchNonInverted implements "record attempts against these ports":
// Ports must contain e.DstPort; ConnState must be trackable (see
// isTrackableConnState); Source, if the entry scopes it, must match the
// event's own resolved identity; DestIP, if the entry scopes it, must
// equal e.DstIP. Only ever returns NoMatch or Violation -- there is no
// observe state for a non-inverted entry.
func matchNonInverted(entry Entry, e store.Event, members AddressListMembership) (matchlog.Tuple, Outcome) {
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
	if !entry.SourceList.Empty() {
		// Scoped to a list: membership is resolved now, against what the
		// router has pushed, rather than against anything stored on the
		// entry. e.SrcIP rather than the resolved identity, because an
		// address list holds addresses -- a MAC-identified event is not
		// a member of anything.
		if members == nil || e.SrcIP == "" {
			return matchlog.Tuple{}, NoMatch
		}
		if !members.InAddressList(entry.SourceList.Device, entry.SourceList.List, e.SrcIP) {
			return matchlog.Tuple{}, NoMatch
		}
	} else if !entry.Source.Empty() && !entry.Source.MatchesSource(id) {
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
