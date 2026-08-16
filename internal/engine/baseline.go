// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"math"
	"sync"
	"time"
)

// emaAlpha weights how much each new reading moves an EMA baseline --
// lifted from internal/detect.emaAlpha (see internal/detect/global_spike.go)
// per docs/decisions/evaluation-engine.md section 1's "baseline
// management" contract: a slow-moving average (2% weight per sample) so
// one genuine spike doesn't immediately drag the baseline up and mask
// itself, while still adapting over many samples to a real, sustained
// change in normal traffic levels. internal/detect keeps its own copy
// until #405 ports onto this one.
const emaAlpha = 0.02

// emaMinZ is the deviation floor a z-score must clear before Fire ever
// reports true, lifted from internal/detect.emaMinZ (see
// internal/detect/ema_confidence.go) for the same reason as emaAlpha
// above.
const emaMinZ = 2.0

// emaZScore reports how many standard deviations above baseline rate
// is, given the EMA's running variance -- unchanged from
// internal/detect's own copy.
func emaZScore(rate, baseline, variance float64) float64 {
	stddev := math.Sqrt(variance)
	switch {
	case stddev > 0:
		return (rate - baseline) / stddev
	case rate > baseline:
		// Perfectly steady baseline so far (variance still 0) and this
		// reading is above it -- genuinely unusual, but with no
		// variance estimate yet to size it against. A finite cap, not
		// +Inf; a caller's own sample-count/warmup gating (here,
		// BaselineFloor) is what actually keeps a tiny sample count
		// from reading as fully confident on this alone.
		return 6.0
	default:
		return 0
	}
}

// emaUpdate advances an EMA baseline+variance by one reading -- the
// standard exponentially-weighted mean/variance update, unchanged from
// internal/detect's own copy. Callers apply this after computing
// z-score for the current reading, so a firing decision compares
// against the baseline as it stood before this reading, not after.
func emaUpdate(rate, baseline, variance float64) (newBaseline, newVariance float64) {
	diff := rate - baseline
	incr := emaAlpha * diff
	newBaseline = baseline + incr
	newVariance = (1 - emaAlpha) * (variance + diff*incr)
	return newBaseline, newVariance
}

// BaselineFloor declares the minimum history a definition requires
// before a Baseline may be trusted -- the chassis contract
// docs/decisions/evaluation-engine.md section 1 calls "a history floor
// on every baseline," #368's fix made structural instead of a
// per-detector patch. A definition expresses its floor as a duration, a
// sample count, or both; a zero field means that dimension imposes no
// floor. All three shapes internal/detect's existing baselines use are
// expressible:
//
//   - sample-count only: BaselineFloor{MinSamples: 5} --
//     internal/detect.hostActivityMinSamples (host_baseline.go).
//   - duration only: BaselineFloor{MinDuration: 45 * time.Minute} --
//     internal/detect.Config.LowSlowScanMinObservation (low_slow_scan.go).
//   - both: BaselineFloor{MinDuration: 14 * 24 * time.Hour, MinSamples: 14} --
//     internal/detect.Config.OffHoursMinSampleDays (off_hours.go), whose
//     "distinct prior days of history" floor is inherently both a
//     sample count (>=14 distinct days observed) and, because each
//     sample is a calendar day, a wall-clock duration that has to have
//     elapsed for that many samples to even be possible.
//
// See Baseline.Reading and Snapshot.Fire for where a floor actually
// gates evaluation.
type BaselineFloor struct {
	MinDuration time.Duration
	MinSamples  int
}

// cleared reports whether observedFor/samples satisfy every non-zero
// dimension of f.
func (f BaselineFloor) cleared(observedFor time.Duration, samples int) bool {
	if f.MinDuration > 0 && observedFor < f.MinDuration {
		return false
	}
	if f.MinSamples > 0 && samples < f.MinSamples {
		return false
	}
	return true
}

// UpdateCadence declares how often a Baseline's EMA is meant to be
// advanced -- per event, or once per completed window. This exists
// because of a hard-won lesson from #397's characterization of the
// baselines being ported (see the #420 issue body): naively folding in
// one reading per raw event means a single alpha-weighted update per
// event, which puts a hard ceiling on how fast the baseline can ever
// move (roughly 1/emaAlpha events) -- for some definitions that makes a
// real spike condition structurally unfireable, not just slow to
// detect. This package does not fix #420 here (that is #405's ported
// definitions' job, once they exist), but Baseline must not bake in
// "always update per event" as the only option, or #420's fix would be
// impossible to express later. A definition declares which cadence it
// uses; Baseline.Reading itself does not change behavior based on the
// value -- it is the caller's job to call Reading once per event
// (UpdatePerEvent) or once per completed window with a
// window-aggregated reading (UpdatePerWindow), and Cadence exists so
// that choice is declared and introspectable rather than an implicit
// property of how often some definition happens to call Reading.
type UpdateCadence int

const (
	// UpdatePerEvent folds in one reading for every event a definition
	// observes -- internal/detect's existing baselines all use this
	// shape today.
	UpdatePerEvent UpdateCadence = iota
	// UpdatePerWindow folds in one reading per completed window -- the
	// option #420's fix needs to exist, see this type's own doc comment.
	UpdatePerWindow
)

// Snapshot is a Baseline's state as of one Reading or Snapshot call --
// value-only (no pointers, no shared backing storage), so returning it
// by value already satisfies the copy-on-read contract this package
// states once (docs/decisions/evaluation-engine.md section 1); there is
// nothing further to deep-copy.
type Snapshot struct {
	// Value/Variance are the EMA baseline as it stood before the
	// reading that produced this Snapshot (Reading) or as of the moment
	// requested (the standalone Snapshot method) -- zero and unprimed
	// until Primed is true.
	Value    float64
	Variance float64
	// ZScore is the just-supplied reading's deviation from Value, given
	// Variance -- 0 for a Snapshot not produced by Reading, or for one
	// produced before priming.
	ZScore float64
	// Samples is how many readings have been folded into Value/Variance
	// since priming (0 before priming).
	Samples int
	// FirstSeen is when this Baseline first received a reading -- the
	// zero time if it never has.
	FirstSeen time.Time
	// Primed reports whether Value/Variance hold a real EMA seed yet.
	// Priming itself is gated on a fully-observed window (#368, see
	// Baseline.Reading's own doc comment) -- Primed can be false for a
	// long time even under steady traffic, if traffic is sparse enough
	// that a full window hasn't elapsed since FirstSeen.
	Primed bool
	// Ready reports whether Primed is true AND the definition's
	// declared BaselineFloor has cleared -- the one field a firing
	// decision built on this Baseline is required to consult. See Fire.
	Ready bool
}

// Fire is the chassis's structural embodiment of "refuses to evaluate
// the firing condition below the floor": it reports true only when
// s.Ready (both primed and past the floor) and the just-computed z-score
// clears minZ. A definition layers its own additional conditions
// (a multiplier against s.Value, an absolute count floor, ...) on top of
// this -- Fire owns only the baseline-history part of the decision, not
// a definition's whole firing predicate.
func (s Snapshot) Fire(minZ float64) bool {
	return s.Ready && s.ZScore >= minZ
}

// Baseline is the chassis's EMA baseline primitive: one instance tracks
// one key's history (a definition holds many, one per key, typically
// inside a Keyed[*Baseline] -- see Keyed's own doc comment), gated by a
// BaselineFloor on both firing (Snapshot.Ready) and priming (see
// Reading) -- docs/decisions/evaluation-engine.md section 1's history-
// floor contract, #368's fix made structural.
//
// Safe for concurrent use: Reading is meant to run only on the
// evaluation goroutine (the same single-writer convention every other
// per-key primitive in this package uses), but Snapshot -- this type's
// read API -- may be called from any goroutine concurrently with
// Reading. See TestBaselineSnapshotRaceSafeAgainstConcurrentReadings.
type Baseline struct {
	mu sync.Mutex

	window  time.Duration
	floor   BaselineFloor
	cadence UpdateCadence

	firstSeen time.Time
	primed    bool
	value     float64
	variance  float64
	samples   int
}

// NewBaseline constructs a Baseline for one key. window is the span a
// caller's reading is computed over (e.g. an ActivitySpikeWindow-style
// rolling count) -- purely to gate priming (see Reading), independent of
// floor's own firing gate, which may use an entirely different
// duration/count. cadence is declared, not enforced -- see
// UpdateCadence's doc comment.
func NewBaseline(window time.Duration, floor BaselineFloor, cadence UpdateCadence) *Baseline {
	return &Baseline{window: window, floor: floor, cadence: cadence}
}

// RestoreBaseline is NewBaseline plus seeding the EMA state directly
// from a previously-persisted BaselineState (see StateStore) instead of
// starting cold -- what lets a definition resume warm across a restart
// rather than being blind for its whole warm-up again (#399's decision,
// docs/decisions/evaluation-engine.md section 1). window/floor/cadence
// are declared exactly as NewBaseline: what a definition needs varies by
// version, not by what happened to be persisted, so a restored Baseline
// is still gated by the *current* floor, not whatever floor produced s.
func RestoreBaseline(window time.Duration, floor BaselineFloor, cadence UpdateCadence, s BaselineState) *Baseline {
	return &Baseline{
		window:    window,
		floor:     floor,
		cadence:   cadence,
		firstSeen: s.FirstSeen,
		primed:    s.Primed,
		value:     s.Value,
		variance:  s.Variance,
		samples:   s.Samples,
	}
}

// Cadence reports the cadence this Baseline was declared with.
func (b *Baseline) Cadence() UpdateCadence {
	return b.cadence
}

// State returns the persistable snapshot of this Baseline's EMA state --
// exactly what StateStore.Set needs and RestoreBaseline needs back,
// deliberately narrower than Snapshot (no ZScore, no Ready: those are
// meaningless without an in-flight reading and a floor to gate against,
// neither of which a persisted document carries). Safe to call
// concurrently with Reading, same contract as Snapshot.
func (b *Baseline) State() BaselineState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BaselineState{
		Value:     b.value,
		Variance:  b.variance,
		Samples:   b.samples,
		FirstSeen: b.firstSeen,
		Primed:    b.primed,
	}
}

// Reading folds one reading into this Baseline's tracked history and
// returns the Snapshot as it stood *before* this reading -- so a firing
// decision compares against the baseline as it was, not as it becomes,
// matching internal/detect.checkHostActivityBaseline's own ordering.
// How often a caller invokes Reading is what actually implements its
// declared Cadence (see UpdateCadence) -- Reading itself behaves
// identically either way.
//
// Priming (the very first EMA seed, value=reading/variance=0) is gated
// on a fully-observed window, not merely on this being the first
// reading: #368's finding is that a bare "first reading primes"
// approach -- even combined with a floor that blocks *firing* until
// enough samples exist -- still seeds the baseline from an artificially
// low value (a still-filling window reads as less busy than it will be
// once full), so ordinary traffic reads as a spike for the rest of that
// same window purely from the window-fill artifact, not from anything
// unusual happening. Gating firing alone just moves the false emission
// to the moment the floor clears, at the window edge -- it does not fix
// the wrong prime. So: a reading taken before now-firstSeen >= window
// is discarded entirely for priming purposes here, not merely withheld
// from firing -- it does not prime, and it is not counted as a sample.
// See TestBaselineDoesNotFalselyFireInsideFirstWindowFromColdStart,
// reproducing #368 directly against this type.
func (b *Baseline) Reading(now time.Time, value float64) Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.firstSeen.IsZero() {
		b.firstSeen = now
	}
	observedFor := now.Sub(b.firstSeen)

	before := Snapshot{
		Value:     b.value,
		Variance:  b.variance,
		Samples:   b.samples,
		FirstSeen: b.firstSeen,
		Primed:    b.primed,
	}
	if b.primed {
		before.ZScore = emaZScore(value, b.value, b.variance)
	}
	before.Ready = before.Primed && b.floor.cleared(observedFor, before.Samples)

	if !b.primed {
		if observedFor < b.window {
			return before // still inside the first window -- do not prime, see doc comment
		}
		b.value = value
		b.variance = 0
		b.primed = true
		b.samples = 1
		return before
	}

	b.value, b.variance = emaUpdate(value, b.value, b.variance)
	b.samples++
	return before
}

// Snapshot reports this Baseline's current state as of now, without
// folding in a reading -- the read API any goroutine other than the
// evaluation goroutine uses (an admin endpoint, a replay pass), safe to
// call concurrently with Reading.
func (b *Baseline) Snapshot(now time.Time) Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	var observedFor time.Duration
	if !b.firstSeen.IsZero() {
		observedFor = now.Sub(b.firstSeen)
	}
	return Snapshot{
		Value:     b.value,
		Variance:  b.variance,
		Samples:   b.samples,
		FirstSeen: b.firstSeen,
		Primed:    b.primed,
		Ready:     b.primed && b.floor.cleared(observedFor, b.samples),
	}
}
