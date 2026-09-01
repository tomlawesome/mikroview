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
// definitions.go: the definitions routes, which an expectation
// definition is one kind of) and the admin-only Watchlist page in the UI
// (frontend/src/components/Watchlist.svelte) -- #243's slice 4.
//
// What this package is NOT, since issue #406: an evaluator. It used to
// carry its own event queue, its own worker goroutine, its own
// backpressure policy and its own panic boundary -- a second copy of
// machinery internal/detect had built independently, right down to two
// constants whose comments said they mirrored their counterparts'
// reasoning exactly. All of it is gone.
//
// What this package is NOT, since issue #407: a store. The entry set
// lived on here after #406 as a second persisted document holding the
// same entries the definitions document already held, converted on every
// registration -- the two-sources-of-truth shape docs/decisions/
// evaluation-engine.md's Migration section exists to remove. Entries are
// expectation definitions in engine.DefinitionsStore now (see
// engine.EntryFromDefinition and the expectation methods beside it), and
// what remains here is the entry *shape* (Entry and its validation) plus
// the matching rules those definitions call (Match, invert.go). With no
// entries configured the whole thing is still provably inert: an empty
// definition set matches nothing.
package watchlist

import (
	"errors"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

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
	// engine.DefinitionsStore.RecordObservation), for the operator to
	// review and promote. A new inverted entry starts Observing; the
	// definitions API's observing action is the mechanism to leave that
	// state, on whatever cadence an operator (or slice 4's UI) decides --
	// this package makes no judgement about when that should happen (#243
	// open question 3).
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
	// before deciding). Capped at engine.maxObservedPerEntry; see
	// engine.DefinitionsStore.RecordObservation for what happens once
	// full. Unused for a non-inverted entry.
	Observed []ObservedDest `json:"observed,omitempty"`

	// Window is when this entry is expected to see traffic: a daily
	// clock range, days of the week, and the IANA zone those clock times
	// are read in (#680). Zero means no window -- the entry is watched at
	// every hour, which is what a row renders as "always". See Window,
	// and window.go's file comment for why the zone exists at all when
	// every other timestamp in this codebase is UTC.
	Window Window `json:"window,omitzero"`
	// Nights is the last MaxNights occurrences of Window and what
	// happened in each: kept, empty, or not observed. Recorded, not
	// derived -- matchlog keeps 48 hours by default, so deriving seven
	// nights from it would report a healthy watch as five empty nights
	// and look like it had worked. Bounded, so it rides inside the
	// existing definitions blob without growing. See Night.
	Nights []Night `json:"nights,omitempty"`
	// Ring is the recorded break in this entry's run of kept nights,
	// written at the moment it breaks. The coverage-derived break (no
	// rule is logging this pathway) is a different kind of broken and
	// stays computed on read from router state -- see Ring.
	Ring Ring `json:"ring,omitzero"`
	// SilentOccurrences is the sticky liveness mark MarkSilent writes and
	// FillNights consults, via Observation.Silent (issue #730): the Open
	// instant of every occurrence of Window found, at some tick, to have
	// the device behind this entry's pathway gone stale. Persisted
	// alongside Nights/Ring rather than held only in memory, for the same
	// reason those are: it must survive a restart, and it must still be
	// there whenever FillNights eventually gets around to closing the
	// occurrence it names.
	SilentOccurrences []time.Time `json:"silentOccurrences,omitempty"`

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

// ErrInvalidEntry is returned by ValidateEntry for an entry with no ID.
var ErrInvalidEntry = errors.New("watchlist: an entry must have an id")

// ErrNoPorts is returned by ValidateEntry for a non-inverted entry with
// an empty Ports -- see Entry.Ports.
var ErrNoPorts = errors.New("watchlist: a non-inverted entry must watch at least one port")

// ErrInvertedRequiresSource is returned by ValidateEntry for an inverted
// entry with no Source -- see Entry.Invert. An inverted entry with no device
// to scope it would mean "nothing in particular should reach anything in
// particular," which isn't a coherent policy to enforce.
var ErrInvertedRequiresSource = errors.New("watchlist: an inverted entry must scope a source device")

// ErrInvalidText is returned by ValidateEntry for a Name, DestIP or Source
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

// ValidateEntry rejects an entry with no ID, invalid text, or a scoping
// requirement its mode does not satisfy (non-inverted: at least one
// port; inverted: a source device) -- the write-boundary contract
// watchlist.Store.Upsert enforced before issue #407 moved the entry set
// into engine.DefinitionsStore. The rules did not move with the storage:
// this is the same function body, exported so the one store that now
// holds entries calls it rather than re-deriving it.
func ValidateEntry(e Entry) error {
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
	if !validText(e.Window.Zone) {
		return ErrInvalidText
	}
	if err := e.Window.Validate(); err != nil {
		return err
	}
	return nil
}

// isTrackableConnState is the "this is an attempt, not an established
// conversation's return traffic" filter: RouterOS commonly logs both
// directions of an established connection on one stateful accept rule,
// and without this a busy accepted service's own return traffic would
// swamp a watchlist entry. internal/engine expresses the same rule as a
// declarative condition on its expectation definitions (connectionState
// in {"", "new"} -- see BuildExpectationDefinition); this copy is what
// the inverted state machine below still applies directly.
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
	// the other reasons covered below. The caller takes no action.
	NoMatch Outcome = iota
	// Violation: record this to internal/matchlog -- a non-inverted
	// entry's watched port was reached, or an inverted entry's device
	// reached somewhere neither permitted nor still being observed.
	Violation
	// Observed: an inverted entry, still Observing, saw its device reach
	// a destination that is neither permitted nor dismissed yet. The
	// the caller records this as a candidate (Store.RecordObservation)
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
