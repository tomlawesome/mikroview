// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strings"
	"time"
)

// replaySampleBound caps how many of a Receipt's emissions carry full
// evidence (target, rendered detail, ports/hosts/labels) rather than
// just contributing to the count. Issue #403's own phrasing is the
// requirement this exists to satisfy: "here are the 35 dropped" must be
// answerable, not just a number.
//
// 50 is chosen, not merely convenient: it is large enough to show a
// genuinely varied spread of instances (different targets, different
// times within the window) for an operator or auto-tune judging whether
// a candidate threshold is too loose or too tight -- a handful of
// samples from only the busiest one or two targets would misrepresent a
// definition that fired against many different sources. It is small
// enough that a Receipt over a corpus a candidate would have fired
// thousands of times against stays a bounded, cheap-to-serialize
// response, rather than growing linearly with EmissionCount -- the same
// reasoning EvidenceSet's own maxEvidencePorts/Hosts/Labels bounds state
// (evidence.go) for the same class of problem one level down (per-key
// evidence rather than per-receipt samples).
const replaySampleBound = 50

// Window is the covered window every Receipt is mandatorily built
// around -- issue #403's own requirement, quoted directly: "the covered
// window ... mandatory and non-omittable at the TYPE level ... Design
// so a receipt cannot be constructed without it." Enforced by making
// every field unexported and providing exactly one constructor
// (NewWindow) that validates start/end and derives Duration/EventCount
// from them, so there is no assignment a caller can perform to produce
// a "half-built" Window carrying a plausible-looking Start with a zero
// End, or a Duration that disagrees with Start/End.
//
// The Go zero value (Window{}) is still reachable (`var w Window`
// bypasses NewWindow entirely -- Go has no way to forbid that), which is
// exactly why "unexported fields plus a required constructor" alone is
// not the whole guarantee: Window.valid and every rendering entry point
// built on this type (Receipt.String) additionally refuse to treat a
// zero Window as data. See TestWindowZeroValueNotRenderable and
// TestReceiptZeroValueNotRenderable.
type Window struct {
	start      time.Time
	end        time.Time
	duration   time.Duration
	eventCount int
}

// NewWindow constructs a Window covering [start, end], having actually
// observed eventCount events in it. Both instants are required (a zero
// time.Time for either is rejected) and end may not precede start --
// the only way to obtain a valid Window is through here, and this is
// the one place that validates it.
func NewWindow(start, end time.Time, eventCount int) (Window, error) {
	if start.IsZero() || end.IsZero() {
		return Window{}, fmt.Errorf("engine: replay window requires non-zero start and end instants")
	}
	if end.Before(start) {
		return Window{}, fmt.Errorf("engine: replay window end %s is before start %s", end, start)
	}
	if eventCount < 0 {
		return Window{}, fmt.Errorf("engine: replay window event count must be non-negative, got %d", eventCount)
	}
	return Window{start: start, end: end, duration: end.Sub(start), eventCount: eventCount}, nil
}

// Start is the window's covered start instant.
func (w Window) Start() time.Time { return w.start }

// End is the window's covered end instant.
func (w Window) End() time.Time { return w.end }

// Duration is End minus Start, computed once by NewWindow rather than
// derived fresh on every call, so it can never disagree with Start/End
// even under a hypothetical future refactor of this type.
func (w Window) Duration() time.Duration { return w.duration }

// EventCount is how many events were actually observed within the
// window -- not a capacity or a configured retention figure, the count
// of events a replay (or whatever else constructs a Window) actually
// walked.
func (w Window) EventCount() int { return w.eventCount }

// valid reports whether w was built by NewWindow (or is otherwise
// well-formed) rather than being the unconstructed zero value -- see
// this type's own doc comment.
func (w Window) valid() bool {
	return !w.start.IsZero() && !w.end.IsZero() && !w.end.Before(w.start)
}

// String renders w for logs and receipt summaries. Deliberately panics
// on the zero value rather than printing "0001-01-01T00:00:00Z ...
// 0s ... 0 events" as though that were a legitimate (if unusually
// short) window: a Window this codebase constructs is only ever built
// by NewWindow, so reaching this panic means something bypassed the
// constructor entirely (`var w Window`), which is a construction bug in
// the caller, not a state worth silently rendering. See
// TestWindowZeroValueNotRenderable.
func (w Window) String() string {
	if !w.valid() {
		panic("engine: Window zero value is not renderable -- construct with NewWindow, never the zero value")
	}
	return fmt.Sprintf("%s to %s (%s, %d event(s))",
		w.start.Format(time.RFC3339), w.end.Format(time.RFC3339), w.duration, w.eventCount)
}

// ReplaySample is one emission a replay judged the candidate params
// would have produced, carried together with the evidence it was judged
// from -- issue #403's own requirement: "here are the 35 dropped" must
// be answerable, not just a number. Mirrors Emission's own fields
// (emission.go) since a replayed emission and a live one carry the same
// shape; At is when, in corpus time, this particular emission would
// have fired, which a live Emission has no need to restate about
// itself.
type ReplaySample struct {
	At     time.Time
	Target string
	Detail string
	Ports  []int
	Hosts  []string
	Labels []string
	// Provisional marks a sample that would have fired while the
	// definition's own baseline (if any) had not yet cleared its
	// history floor -- see Emission.Provisional's own doc comment. A
	// definition with no baseline concept (every DeclarativeDefinition
	// today -- see declarative.go) leaves this false unconditionally,
	// the same default RenderEmission uses live.
	Provisional bool
}

// appendReplaySample adds s to a replay's running sample, keeping at most
// replaySampleBound entries. Once full, a new sample bumps out the oldest
// one rather than being refused -- issue #860: an operator pressing Try is
// judging something that just happened, so the receipt should illustrate
// with the emissions they are most likely to recognise, not whichever fifty
// happened to fire first over however much corpus is held. Every Replay
// call feeds this in chronological (oldest-to-newest) walk order, so the
// returned slice comes out chronological too -- oldest of the kept entries
// first -- without needing a re-sort.
func appendReplaySample(sample []ReplaySample, s ReplaySample) []ReplaySample {
	sample = append(sample, s)
	if len(sample) > replaySampleBound {
		sample = sample[1:]
	}
	return sample
}

// Receipt is what a Replayable definition's Replay call returns when
// the corpus was long enough to answer honestly (see Decline for the
// alternative). Every field issue #403 asks a receipt to carry is here:
// the emission count the candidate params would have produced, a
// bounded sample of those emissions with their evidence, the covered
// window (mandatory -- see Window), and whether the corpus read was
// truncated mid-replay.
//
// Unexported fields, constructed only through NewReceipt, which
// requires a valid Window and rejects a sample that disagrees with its
// own stated bound or its own stated count -- "a receipt cannot be
// constructed without its window" applies to this type exactly the way
// it applies to Window itself, for the same reason.
type Receipt struct {
	window Window

	emissionCount   int
	sample          []ReplaySample
	sampleTruncated bool

	corpusTruncated bool
	anyProvisional  bool
}

// NewReceipt constructs a Receipt. window must be valid (see
// Window/NewWindow). sample must not exceed replaySampleBound, and must
// not exceed emissionCount -- a sample can never claim to hold more
// instances than the receipt says fired in total. corpusTruncated
// carries CorpusWindow.Truncated through from whatever Corpus.Replay
// call produced window, unchanged.
func NewReceipt(window Window, emissionCount int, sample []ReplaySample, corpusTruncated bool) (Receipt, error) {
	if !window.valid() {
		return Receipt{}, fmt.Errorf("engine: replay receipt requires a valid window (see NewWindow) -- got the zero value")
	}
	if emissionCount < 0 {
		return Receipt{}, fmt.Errorf("engine: replay receipt emission count must be non-negative, got %d", emissionCount)
	}
	if len(sample) > replaySampleBound {
		return Receipt{}, fmt.Errorf("engine: replay receipt sample of %d exceeds replaySampleBound (%d) -- bound the sample before constructing a Receipt", len(sample), replaySampleBound)
	}
	if len(sample) > emissionCount {
		return Receipt{}, fmt.Errorf("engine: replay receipt sample (%d) exceeds its own emission count (%d)", len(sample), emissionCount)
	}

	anyProvisional := false
	for _, s := range sample {
		if s.Provisional {
			anyProvisional = true
			break
		}
	}

	return Receipt{
		window:          window,
		emissionCount:   emissionCount,
		sample:          append([]ReplaySample(nil), sample...),
		sampleTruncated: emissionCount > len(sample),
		corpusTruncated: corpusTruncated,
		anyProvisional:  anyProvisional,
	}, nil
}

// Window is the covered window this receipt states -- see Window's own
// doc comment for why this is never omittable.
func (r Receipt) Window() Window { return r.window }

// EmissionCount is how many times the candidate params would have
// produced an emission over Window.
func (r Receipt) EmissionCount() int { return r.emissionCount }

// Sample is the bounded (at most replaySampleBound), copy-on-read set
// of emissions EmissionCount counts, each with its own evidence -- see
// SampleTruncated for whether this is all of them.
func (r Receipt) Sample() []ReplaySample {
	return append([]ReplaySample(nil), r.sample...)
}

// SampleTruncated reports whether EmissionCount exceeds len(Sample) --
// i.e. whether replaySampleBound actually bound anything for this
// receipt, as opposed to every emission having a sample entry.
func (r Receipt) SampleTruncated() bool { return r.sampleTruncated }

// CorpusTruncated reports whether the corpus read stopped before
// visiting every currently-available event (CorpusWindow.Truncated) --
// distinct from SampleTruncated, which bounds the receipt's own
// emission sample, not the corpus it was computed from.
func (r Receipt) CorpusTruncated() bool { return r.corpusTruncated }

// AnyProvisional reports whether any sampled emission would have fired
// during the definition's history-floor warm-up (see
// ReplaySample.Provisional) -- issue #403's own requirement: a receipt
// states "whether any emission would have been provisional under the
// definition's history floor." Computed once at construction from the
// sample actually kept; a definition whose true EmissionCount exceeds
// the sample bound could in principle have had a provisional emission
// outside the sample that this therefore cannot see -- true today for
// none of this repository's definitions (DeclarativeDefinition has no
// baseline/history-floor concept at all, see declarative.go), and
// exactly the kind of edge a programmatic, baseline-backed definition's
// own #405/#406 Replay implementation will need to reason about when it
// exists.
func (r Receipt) AnyProvisional() bool { return r.anyProvisional }

// String renders r for logs and summaries. Panics on the zero value for
// exactly the reason Window.String does -- see that method's own doc
// comment; a Receipt this codebase constructs is only ever built by
// NewReceipt, so reaching this panic means the zero value (`var r
// Receipt`) was used directly, a construction bug in the caller. See
// TestReceiptZeroValueNotRenderable.
func (r Receipt) String() string {
	if !r.window.valid() {
		panic("engine: Receipt zero value is not renderable -- construct with NewReceipt, never the zero value")
	}
	var flags []string
	if r.sampleTruncated {
		flags = append(flags, "sample truncated")
	}
	if r.corpusTruncated {
		flags = append(flags, "corpus truncated")
	}
	if r.anyProvisional {
		flags = append(flags, "includes provisional emissions")
	}
	suffix := ""
	if len(flags) > 0 {
		suffix = " [" + strings.Join(flags, ", ") + "]"
	}
	return fmt.Sprintf("replay: %d emission(s) over %s%s", r.emissionCount, r.window, suffix)
}

// Decline is a typed, non-error refusal to answer a replay question --
// issue #403's own requirement: "A receipt over a corpus shorter than
// the definition's own window DECLINES (a typed 'corpus shorter than
// definition window' outcome) rather than reporting a misleading zero."
//
// A definition needing a five-minute window judged against ninety
// seconds of available corpus has not been shown to fire zero times --
// it has not been evaluated at all, since it never even reached one
// full window's worth of history. Reporting EmissionCount: 0 for that
// case would be indistinguishable, in the response shape, from "ninety
// minutes of data and it genuinely never fired" -- which is exactly the
// overclaim docs/decisions/evaluation-engine.md's ratified open
// question 2 answer rules out: "so a short corpus can never overclaim."
// Decline is what makes the two cases structurally distinct instead of
// relying on a caller to notice a suspiciously short Window on an
// otherwise-normal-looking Receipt.
type Decline struct {
	// Reason is a one-sentence, operator-presentable explanation.
	Reason string
	// CorpusSpan is how much time the corpus actually covered.
	CorpusSpan time.Duration
	// DefinitionWindow is the (possibly candidate-overridden) window the
	// definition needed at least that much corpus for.
	DefinitionWindow time.Duration
}

// Result is what a Replayable definition's Replay call returns: exactly
// one of Receipt or Decline is set, chosen structurally so a caller must
// handle the decline case explicitly rather than treating a short
// corpus as an ordinary Receipt with a suspiciously small EmissionCount.
type Result struct {
	Receipt *Receipt
	Decline *Decline
}
