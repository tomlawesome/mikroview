// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"sort"
	"time"
)

// MaxNights is how many nights an entry remembers: the seven the drawer
// summarises. The cap is what lets this history ride inside the existing
// definitions blob without a schema change and without unbounded growth --
// the oldest night is dropped when an eighth arrives.
const MaxNights = 7

// NightState is what one occurrence of an entry's window turned out to be.
//
// Three states, not two, and the third is the whole point. "kept" and
// "empty" are both claims about the network. If the app was down for a
// night, or no firewall rule was logging the pathway, mikroview has no
// claim to make -- and reporting that as "empty" would be presenting an
// absence of our own as a fact about the network, which is the single
// failure this project refuses to ship. That night is "not observed".
type NightState string

const (
	// NightKept: at least one match attributed to this entry landed
	// inside the window. Presence, not similarity -- see RecordMatch.
	NightKept NightState = "kept"
	// NightEmpty: the window opened and closed with mikroview watching
	// throughout, and nothing matched. This is a statement about the
	// network, and it is only ever recorded when it is one.
	NightEmpty NightState = "empty"
	// NightUnobserved: mikroview cannot say what happened. The app was
	// not running for the whole window, or nothing was logging the
	// pathway it watches. Never rendered as "empty".
	NightUnobserved NightState = "not observed"
)

// Night is one occurrence of an entry's window, and what happened in it.
//
// Opened is the UTC instant the window opened, and is this record's
// identity: a night is written once, keyed by it, which is what makes the
// lazy fill idempotent.
//
// Count and First are recorded even though nothing reads them for the
// kept/empty decision today. They are cheap here and impossible to
// reconstruct later -- matchlog keeps 48 hours, so by the time a size
// judgement or a "widen the window to what actually happens" offer wants
// them, the events they came from are long gone.
type Night struct {
	Opened time.Time  `json:"opened"`
	State  NightState `json:"state"`
	// First is the first match inside the window, zero unless State is
	// NightKept.
	First time.Time `json:"first,omitzero"`
	Count uint64    `json:"count,omitempty"`
}

// Observation is what the caller knows about its own ability to have seen
// a night, which is what separates "empty" from "not observed".
//
// Since is the instant this process began watching, so a window that
// opened before it cannot be claimed as observed end to end: the previous
// process either recorded that night already (the record is persisted, and
// a night is written once) or was not running for it. A zero Since means
// the caller cannot say, which records "not observed" -- the honest
// direction when the answer is unknown.
//
// Covered is whether any firewall rule visible to mikroview logs the
// pathway this entry watches. A watch nothing logs cannot be judged on
// nightly presence at all, so its nights are recorded "not observed",
// never "empty".
//
// Silent is the sticky record MarkSilent accumulates (issue #730): the
// Open instant of every occurrence found to have gone through a tick
// where the device behind this entry's pathway was stale, whether or not
// it has recovered since. Consulted here rather than by asking "is the
// device stale right now" at fill time, because fill can run long after
// the occurrence closed -- by which point a router that was quiet for
// four hours and came back before the window shut would look perfectly
// live again. The sticky mark is what remembers the outage was real.
type Observation struct {
	Since   time.Time
	Covered bool
	Silent  []time.Time
}

// stateFor decides what an occurrence nothing matched in gets recorded as.
func (obs Observation) stateFor(o Occurrence) NightState {
	if !obs.Covered || obs.Since.IsZero() || o.Open.Before(obs.Since) {
		return NightUnobserved
	}
	if wasMarkedSilent(obs.Silent, o.Open) {
		return NightUnobserved
	}
	return NightEmpty
}

// wasMarkedSilent reports whether open appears in marks -- see
// Observation.Silent and MarkSilent.
func wasMarkedSilent(marks []time.Time, open time.Time) bool {
	for _, m := range marks {
		if m.Equal(open) {
			return true
		}
	}
	return false
}

// MarkSilent records, sticky, that the device behind w's pathway was
// found stale at the tick occurring at "at" -- the caller (internal/
// engine's watch-liveness ticker) calls this on every tick while w may be
// open, reusing internal/engine/shipped_device_silence.go's own
// definition of stale rather than a fresh one (issue #730).
//
// This must be sticky rather than a check performed once, at window
// close, because the whole point is catching an outage that has already
// healed by the time anything gets around to filling the night: a router
// quiet for four hours that comes back before the window shuts must still
// close as NightUnobserved, and the only way to know that later is to
// have written it down while it was still true.
//
// A no-op outside every occurrence of w (undefined window, or "at"
// between windows) and idempotent within one (marking the same occurrence
// twice changes nothing). Bounded at MaxNights entries, the same cap
// Night history itself carries, so a fill cadence slower than the tick
// cadence cannot grow this without bound; the oldest mark is dropped, on
// the same "matchlog does not go back far enough to reconstruct this
// anyway" reasoning Night's own doc comment gives.
func MarkSilent(marks []time.Time, w Window, at time.Time) []time.Time {
	o, ok := w.OccurrenceAt(at)
	if !ok {
		return marks
	}
	for _, m := range marks {
		if m.Equal(o.Open) {
			return marks
		}
	}
	marks = append(marks, o.Open)
	sort.SliceStable(marks, func(i, j int) bool { return marks[i].Before(marks[j]) })
	if len(marks) > MaxNights {
		marks = append([]time.Time(nil), marks[len(marks)-MaxNights:]...)
	}
	return marks
}

// lastOpened is the newest night already recorded, or the zero time.
func lastOpened(nights []Night) time.Time {
	var last time.Time
	for _, n := range nights {
		if n.Opened.After(last) {
			last = n.Opened
		}
	}
	return last
}

// capNights sorts oldest-first and drops the oldest beyond MaxNights.
func capNights(nights []Night) []Night {
	sort.SliceStable(nights, func(i, j int) bool { return nights[i].Opened.Before(nights[j].Opened) })
	if len(nights) > MaxNights {
		nights = append(nights[:0:0], nights[len(nights)-MaxNights:]...)
	}
	return nights
}

// FillNights writes down every occurrence of w that has closed since the
// last recorded night and is not recorded yet, and returns the capped
// history.
//
// Lazy and idempotent, deliberately not driven by a timer. A timer that
// fires at each window close misses every close the app was not running
// for, and a restart at 03:00 would silently lose the night before it.
// Filling on evaluation instead means the record catches up the moment the
// app is doing anything at all, and calling it twice writes nothing the
// second time.
//
// Nights filled here are never "kept": a kept night was written when the
// match arrived (RecordMatch), while the window was open. What this adds
// is the nights nothing matched in -- as "empty" when mikroview was
// watching throughout, and as "not observed" when it was not. See
// Observation.
func FillNights(nights []Night, w Window, now time.Time, obs Observation) []Night {
	if !w.Defined() {
		return nights
	}
	for _, o := range w.ClosedSince(lastOpened(nights), now, MaxNights) {
		nights = append(nights, Night{Opened: o.Open, State: obs.stateFor(o)})
	}
	return capNights(nights)
}

// RecordMatch marks the night containing at as kept, and counts the match.
//
// Kept means presence: any match attributed to the entry whose time falls
// inside the window. Not similarity. "Was it the usual size" needs a
// baseline this app does not have, and a night flipping from kept to empty
// because traffic was lighter than usual would be a judgement the operator
// could neither see nor check.
//
// A match outside every occurrence of the window changes nothing: it says
// nothing about any night. So does a match against an entry with no
// window -- there are no nights to keep.
//
// Reports whether this was more than a repeat: a night newly written
// down, or one that had been empty or unobserved turning kept. A caller
// on the evaluation path persists on that and lets a bare count bump ride
// in memory, the same trade RecordObservation already makes -- an unclean
// shutdown can lose a night's latest count, never the fact it was kept.
func RecordMatch(nights []Night, w Window, at time.Time) ([]Night, bool) {
	o, ok := w.OccurrenceAt(at)
	if !ok {
		return nights, false
	}
	for i := range nights {
		if !nights[i].Opened.Equal(o.Open) {
			continue
		}
		n := &nights[i]
		notable := n.State != NightKept
		n.State = NightKept
		n.Count++
		if n.First.IsZero() || at.Before(n.First) {
			n.First = at.UTC()
			notable = true
		}
		return nights, notable
	}
	nights = append(nights, Night{Opened: o.Open, State: NightKept, First: at.UTC(), Count: 1})
	return capNights(nights), true
}

// RingReason is why a watch's run of kept nights is broken.
type RingReason string

const (
	// RingNoMatchInWindow: the window opened and closed with nothing in
	// it, while mikroview was watching. The only reason recorded on the
	// entry -- see Ring.
	RingNoMatchInWindow RingReason = "no-match-in-window"
	// RingNoLogging: no firewall rule mikroview can see logs this
	// pathway. Computed on read from router state, never recorded here.
	RingNoLogging RingReason = "no-logging"
	// RingPaused: the watch is turned off. Live state on the definition,
	// never recorded here.
	RingPaused RingReason = "paused"
)

// Ring is the recorded break in a watch's run of kept nights.
//
// Recorded at the moment it breaks rather than computed on read, because
// *why* it broke -- which window closed empty, and how long the run before
// it was -- is knowable then and expensive later: computing it on read
// means rebuilding seven nights of history on every list request.
//
// Only RingNoMatchInWindow is ever recorded. The other two reasons are
// live state, not history: coverage comes from what the routers are
// currently logging and paused from the definition's own enabled flag, so
// recording either would be a second, staler source of truth for a fact
// the caller already has. They exist as constants because the UI ranks all
// three -- paused > no logging visible > ring broken > watching -- and
// that ranking should name the same things this package does.
type Ring struct {
	Broken bool `json:"broken,omitempty"`
	// Since is the close of the first empty window in the current run.
	Since  time.Time  `json:"since,omitzero"`
	Reason RingReason `json:"reason,omitempty"`
}

// UpdateRing recomputes the recorded ring from the nights just written.
//
// The ring is broken when the most recent night mikroview could actually
// judge was empty, and Since is the close of the earliest empty night in
// that unbroken run of empties. A "not observed" night neither breaks the
// ring nor heals it -- it is not evidence either way, so it is stepped
// over rather than counted, which is the same refusal NightUnobserved
// exists for.
func UpdateRing(nights []Night, w Window) Ring {
	var since time.Time
	for i := len(nights) - 1; i >= 0; i-- {
		if nights[i].State == NightKept {
			break // the run of empties ends here
		}
		if nights[i].State == NightEmpty {
			since = nights[i].Opened
		}
		// NightUnobserved: no evidence either way, so step over it.
	}
	if since.IsZero() {
		return Ring{}
	}
	closes := since
	if o, ok := w.occurrenceFor(since); ok {
		closes = o.Close
	}
	return Ring{Broken: true, Since: closes, Reason: RingNoMatchInWindow}
}

// occurrenceFor rebuilds the occurrence that opened at exactly open, so a
// recorded night can name the instant it closed without storing it twice.
func (w Window) occurrenceFor(open time.Time) (Occurrence, bool) {
	if !w.Defined() {
		return Occurrence{}, false
	}
	loc, err := w.Location()
	if err != nil {
		return Occurrence{}, false
	}
	for back := 0; back <= 1; back++ {
		o, ok := w.occurrenceOpeningOn(localDate(open, loc, -back))
		if ok && o.Open.Equal(open) {
			return o, true
		}
	}
	return Occurrence{}, false
}

// NightSummary counts a history by state, for the drawer's sentence.
type NightSummary struct {
	Kept       int `json:"kept"`
	Empty      int `json:"empty"`
	Unobserved int `json:"unobserved"`
}

// SummariseNights counts nights by state.
func SummariseNights(nights []Night) NightSummary {
	var s NightSummary
	for _, n := range nights {
		switch n.State {
		case NightKept:
			s.Kept++
		case NightEmpty:
			s.Empty++
		default:
			s.Unobserved++
		}
	}
	return s
}
