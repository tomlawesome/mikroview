// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/tomlawesome/mikroview/internal/decommission"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file is issue #460's evaluation half: a retired network segment,
// still on the map, still being watched, wearing the chassis's envelope.
//
// # Why this is programmatic rather than declarative
//
// #460's own body proposed "a custom declarative expectation scoped to
// the retired CIDR", and that was the right instinct on the engine that
// existed when it was written. Building it showed two structural reasons
// the declarative shape cannot carry it, both worth recording so nobody
// re-proposes it:
//
//  1. **"To or from" is not expressible.** A declarative Condition set is
//     AND across fields (conditions.go), so source-in-CIDR and
//     destination-in-CIDR cannot be OR'd in one definition -- and
//     Definition.Scope's Hosts axis is matched against the source address
//     alone (scope_match.go). #460's rule is "ANY traffic to or from it",
//     and a forgotten rule still steering packets *at* a dead range is
//     exactly as much a straggler as a device still speaking *from* one.
//     Splitting it into two definitions would give one segment two
//     watches, two ghosts and two clocks -- the opposite of the ratified
//     "one object, one state machine".
//  2. **The clean window is an absence, not a threshold.** A decommission
//     retires because nothing arrived for long enough. Kind's own doc
//     comment names absence-of-events as the case the programmatic kind
//     is permanent for: "there is no event for a condition to match
//     against".
//
// So this is one programmatic expectation definition holding every
// decommission watch, exactly as InvertedExpectations is one programmatic
// definition holding every inverted watchlist entry -- and for the same
// structural reason that type's doc comment gives: provenance=custom
// implies kind=declarative, so an operator-created watch whose logic is
// Go cannot be its own stored envelope. The per-watch data is data.
//
// Each watch still routes under its own envelope (see emit), so a
// straggler lands in internal/matchlog under that watch's own id and the
// watchlist lists it like any other watch.

// DecommissionWatchesID is the fixed id this set registers under. Like
// WatchLivenessTickerID, it names nothing an operator looks up in the
// definitions API: the operator-facing objects are the watches inside it.
const DecommissionWatchesID = "decommission-watches"

// DecommissionStore is what this definition needs from
// internal/decommission -- narrowed to the two calls the evaluation
// goroutine makes, so the engine depends on the behaviour rather than on
// the store's full surface, and a test can supply either.
type DecommissionStore interface {
	// Active returns every watch that has not retired, including
	// force-removed ones: a detached watch is off the map but still
	// watching (owner ruling, 2026-08-17), and dropping it here is
	// exactly the "nothing is silently dropped" property that ruling
	// turns on.
	Active() []decommission.Watch
	// RecordTraffic takes one observation against every active watch the
	// event touched and returns those watches as they now stand.
	RecordTraffic(srcIP, dstIP string, at time.Time) []decommission.Watch
	// Sweep records retirement for every watch whose clean window has
	// elapsed.
	Sweep(now time.Time) []decommission.Watch
}

// DecommissionWatches is the one programmatic expectation definition
// holding every decommission watch.
//
// Rebuilt wholesale on every Registry.Sync, which is safe because it
// holds no evaluation state of its own: the clock, the count and the
// retirement all live on the watch record in the decommission store,
// which is where they have to live anyway to survive a restart. The
// prefix index below is a cache of that store, not state.
type DecommissionWatches struct {
	store DecommissionStore
	// prefixes is the parsed range of every active watch, snapshotted at
	// construction so the evaluation goroutine can reject the
	// overwhelming majority of events -- everything not inside a retired
	// range -- without touching the store's lock at all. A hit falls
	// through to RecordTraffic, which takes it.
	//
	// A stale snapshot costs at most one Sync's worth of lag on a watch
	// just created, and cannot produce a false violation: RecordTraffic
	// re-checks containment against the live record before recording
	// anything.
	prefixes []netip.Prefix
	watches  []decommission.Watch

	// sweepInterval is how often the clean window is checked. Declared
	// rather than derived: see TickInterval.
	sweepInterval time.Duration

	// OnRoutedEmission is the seam main.go wires onto a real
	// matchlog.Store (see MatchlogSink). nil is a valid no-op.
	OnRoutedEmission func(RoutedEmission)
}

// decommissionSweepInterval is how often a watch's clean window is
// checked for expiry.
//
// A minute, against a window measured in hours. The cadence only decides
// how promptly an already-true retirement is noticed -- Watch.StateAt
// answers correctly whether or not a sweep has run, so nothing is
// *waiting* on this -- which is device_silence's half of the cadence
// distinction Ticked.TickInterval draws, not global_spike's: there is no
// baseline here whose span would be retuned by changing it.
const decommissionSweepInterval = time.Minute

// NewDecommissionWatches builds the set from every active watch in st.
// A nil store is valid and yields a set that evaluates nothing -- the
// same nil-tolerant contract every other dependency here states, so a
// deployment with no persistence still registers the whole engine.
func NewDecommissionWatches(st DecommissionStore) *DecommissionWatches {
	x := &DecommissionWatches{store: st, sweepInterval: decommissionSweepInterval}
	if st == nil {
		return x
	}
	x.watches = st.Active()
	for _, w := range x.watches {
		p, err := netip.ParsePrefix(w.CIDR)
		if err != nil {
			// The store validates on the way in (Watch.Validate), so
			// this is a document written by a future binary or corrupted
			// by hand. Skipped rather than fatal: one unparseable watch
			// must not stop every other one being evaluated, which is
			// Registry.Sync's own collect-don't-stop stance.
			logger.Error(fmt.Sprintf("decommission watch %q: unparseable range %q, not evaluated", w.ID, w.CIDR))
			continue
		}
		x.prefixes = append(x.prefixes, p.Masked())
	}
	return x
}

// ID satisfies Evaluated.
func (x *DecommissionWatches) ID() string { return DecommissionWatchesID }

// Kind satisfies Evaluated -- the state machine is Go.
func (x *DecommissionWatches) Kind() string { return string(KindProgrammatic) }

// Len reports how many watches this set holds, for a caller or a test
// that wants to assert the set was actually built -- the same visibility
// reason InvertedExpectations.Len exists.
func (x *DecommissionWatches) Len() int { return len(x.watches) }

// SetSink wires this definition's emission sink.
func (x *DecommissionWatches) SetSink(sink func(RoutedEmission)) { x.OnRoutedEmission = sink }

// TickInterval and Tick satisfy Ticked: the clean window's expiry is the
// retirement, and nothing arrives to notice it.
func (x *DecommissionWatches) TickInterval() time.Duration { return x.sweepInterval }

// Tick records retirement for every watch whose clean window has just
// elapsed.
//
// Called from Engine.Tick, which is not the evaluation goroutine -- the
// store is safe for concurrent use, which is the whole of the contract
// this owes.
func (x *DecommissionWatches) Tick(now time.Time) {
	if x.store == nil {
		return
	}
	for _, w := range x.store.Sweep(now) {
		logger.Info(fmt.Sprintf("decommission watch %q retired: %s was quiet for %s", w.ID, w.CIDR, w.CleanWindow))
	}
}

// Evaluate satisfies Evaluated: any traffic to or from a retired range
// is a violation.
//
// No threshold and no window. One straggler is the whole finding -- the
// range was declared dead, so a single packet contradicts the
// declaration -- which is also why the Detail below describes this one
// event rather than a count over a period.
func (x *DecommissionWatches) Evaluate(e store.Event) {
	if len(x.prefixes) == 0 {
		return
	}
	if !x.touches(e.SrcIP) && !x.touches(e.DstIP) {
		return
	}
	// Stamped on ReceivedAt, not the router's self-reported Time: the
	// clean window is mikroview's claim about what it saw and when it saw
	// it, and internal/store documents device clocks as not monotonic
	// with arrival order. A skewed router must not be able to shorten or
	// extend a decommission.
	for _, w := range x.store.RecordTraffic(e.SrcIP, e.DstIP, e.ReceivedAt) {
		x.emit(w, e)
	}
}

// touches reports whether ip falls in any watched range.
func (x *DecommissionWatches) touches(ip string) bool {
	if ip == "" {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, p := range x.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// decommissionDefinitionFor is the envelope one watch routes under.
//
// Built here rather than stored, because these definitions are not in the
// definitions store: a watch is created and deleted by an operator, and a
// stored envelope would have to be either shipped (never deletable) or
// custom (never programmatic). The same escape InvertedExpectations
// takes, for the same invariant.
func decommissionDefinitionFor(w decommission.Watch) Definition {
	return Definition{
		ID:      w.ID,
		Name:    decommissionWatchName(w),
		Intent:  IntentExpectation,
		Kind:    KindProgrammatic,
		Enabled: true,
	}
}

// decommissionWatchName is what the watchlist calls this watch. The range
// is in the name because that is the thing being watched, and the zone's
// old name is beside it because that is what the operator remembers it
// as.
func decommissionWatchName(w decommission.Watch) string {
	if w.Name == "" {
		return fmt.Sprintf("retired %s", w.CIDR)
	}
	return fmt.Sprintf("retired %s (%s)", w.CIDR, w.Name)
}

// emit renders and routes one straggler.
//
// The Detail is #460's own sentence, and it degrades honestly: where the
// router's last push named the address it says what it was called and
// until when, and where no push ever covered it, it says the address and
// stops. Watch.Describe owns that rule so the API, the match log and any
// future surface cannot word it three different ways.
func (x *DecommissionWatches) emit(w decommission.Watch, e store.Event) {
	// The straggler is whichever end of this event is inside the dead
	// range. Source first: a device still speaking from the old range is
	// the case the operator can act on, and where both ends are inside it
	// (traffic within the retired segment) the source is still the
	// honest subject.
	straggler := e.SrcIP
	if !w.Contains(straggler) {
		straggler = e.DstIP
	}

	em, err := RenderEmission(nil, 1, w.Describe(straggler), false)
	if err != nil {
		logger.Error(fmt.Sprintf("decommission watch %q: RenderEmission failed: %v", w.ID, err))
		return
	}
	def := decommissionDefinitionFor(w)
	em.DefinitionID = def.ID
	em.Target = straggler
	em.SourceIP = e.SrcIP
	em.Country = e.SrcCountry
	em.EventTime = e.ReceivedAt
	ev := e
	em.TriggeringEvent = &ev

	routed, err := Route(def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("decommission watch %q: Route failed: %v", w.ID, err))
		return
	}
	if x.OnRoutedEmission != nil {
		x.OnRoutedEmission(routed)
	}
}

// ReplayDecommission is the receipt behind the offer: "in the last N
// hours this would have caught 3".
//
// A standalone function rather than a Replayable method, because the
// thing being replayed does not exist yet. The whole persuasive force of
// #403's receipts here is that the number arrives *before* the operator
// answers -- a replay of an already-created watch would be a report, not
// an argument.
//
// Honestly replayable, unlike an inverted expectation: this definition's
// judgement is a property of single events ("did anything touch this
// range"), not of an observation period measured in days, so a corpus
// measured in minutes-to-hours answers exactly the question asked -- over
// exactly the span it covers, which is why the caller shows the span
// beside the count and never the count alone.
func ReplayDecommission(cidr string, corpus Corpus) (Result, error) {
	normalised, err := decommission.NormaliseCIDR(cidr)
	if err != nil {
		return Result{}, err
	}
	prefix, err := netip.ParsePrefix(normalised)
	if err != nil {
		return Result{}, err
	}
	prefix = prefix.Masked()

	inRange := func(ip string) bool {
		if ip == "" {
			return false
		}
		addr, parseErr := netip.ParseAddr(ip)
		return parseErr == nil && prefix.Contains(addr)
	}

	var samples []ReplaySample
	count := 0
	window := corpus.Replay(func(e store.Event) {
		src, dst := inRange(e.SrcIP), inRange(e.DstIP)
		if !src && !dst {
			return
		}
		count++
		if len(samples) < replaySampleBound {
			target := e.SrcIP
			if !src {
				target = e.DstIP
			}
			samples = append(samples, ReplaySample{
				At:     e.ReceivedAt,
				Target: target,
				Detail: fmt.Sprintf("traffic from %s", target),
				Ports:  nonZeroPort(e.DstPort),
			})
		}
	})

	if window.Count == 0 {
		// No corpus is not a zero receipt. "Nothing would have been
		// caught" and "there was nothing to catch it in" are different
		// answers, and reporting the first for the second is the exact
		// dishonesty the Decline shape exists to prevent.
		return Result{Decline: &Decline{
			Reason:     "there are no retained events to replay this against yet",
			CorpusSpan: 0,
		}}, nil
	}

	span, err := NewWindow(window.Start, window.End, window.Count)
	if err != nil {
		return Result{}, err
	}
	receipt, err := NewReceipt(span, count, samples, window.Truncated)
	if err != nil {
		return Result{}, err
	}
	return Result{Receipt: &receipt}, nil
}

func nonZeroPort(p int) []int {
	if p == 0 {
		return nil
	}
	return []int{p}
}
