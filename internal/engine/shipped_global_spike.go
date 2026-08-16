// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("global_spike", buildGlobalSpikeDefinition)
}

// globalSpikeCheckInterval is how often this definition's baseline takes
// a reading -- lifted from main.go's own globalSpikeCheckInterval, which
// is where it lived while internal/detect owned this detector.
//
// It is a property of the definition, not of whatever drives it: the EMA
// advances exactly one sample per reading, so this interval decides how
// much wall-clock history a given sample count represents. See Ticked's
// own doc comment for why the chassis makes a definition declare its
// cadence rather than inheriting the driver's.
const globalSpikeCheckInterval = 10 * time.Second

// globalSpikeEPSWindow is the span store.Store.EventsPerSecond averages
// over (see eventsPerSecondLocked). Restated here because Replay has to
// reconstruct the same quantity from a corpus, and reconstructing it
// against a different window would produce a number that is not the one
// this definition actually judges.
const globalSpikeEPSWindow = 10 * time.Second

// globalSpikeDefinition is global_spike ported onto the chassis (issue
// #405): the network-wide events-per-second figure against a slow EMA
// baseline of itself, so a sudden jump in overall traffic is visible
// without anyone having to pick an absolute number that would be wrong
// for every other deployment.
//
// Ticked rather than per-event, and that is not an implementation detail:
// "spike relative to normal" is a property of a rate over time, not of
// any single event, so there is nothing an Evaluate call could
// meaningfully do with one. internal/detect made the same call, driving
// this from a main.go ticker rather than from Observe.
//
// One baseline, not a Keyed set: the target is the literal "global".
// That is also why the disable behaviour differs from rule_spike's, and
// the difference is deliberate on both sides. Disabling this definition
// discards the baseline entirely, so re-enabling re-primes from the next
// reading rather than comparing live traffic against a figure that went
// stale while nobody was watching. rule_spike deliberately does NOT do
// that (#267 finding 17, measured and rejected) because its rate comes
// from a ring that only fills while it is enabled, so re-priming would
// read the ordinary refill as a spike. This one is handed an accurate
// current EPS on every reading, so re-priming gives it a correct
// baseline immediately -- the same reasoning internal/detect's own
// GlobalSpikeDetector.Check recorded.
type globalSpikeDefinition struct {
	programmaticBase

	multiplier    float64
	minEPS        float64
	warmupSamples int
	floor         BaselineFloor
	cadence       UpdateCadence

	rate  EventRateSource
	state *StateStore

	// mu guards baseline/lastPersisted. Tick runs on the caller's ticker
	// goroutine rather than the evaluation goroutine (see Engine.Tick),
	// and Snapshot-style reads may come from anywhere, so this one
	// definition does own a lock -- the exception Ticked's doc comment
	// says an implementation is responsible for.
	mu            sync.Mutex
	baseline      *Baseline
	lastPersisted time.Time
}

func buildGlobalSpikeDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	multiplier, err := paramFloat(params, "multiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minEPS, err := paramFloat(params, "minEPS")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	warmup, err := paramIntOptional(params, "warmupSamples")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	// primeWindow zero: internal/detect primed this baseline from the
	// very first reading, and the reading is an accurate instantaneous
	// rate rather than a still-filling window's, so #368's priming gate
	// does not apply. baselineFloorFromParams' window argument is
	// therefore zero too -- an operator-set baselineFloorDuration can
	// still add one.
	floor, err := baselineFloorFromParams(params, 0)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}

	d := &globalSpikeDefinition{
		programmaticBase: programmaticBase{def: def},
		multiplier:       multiplier,
		minEPS:           minEPS,
		warmupSamples:    warmup,
		floor:            floor,
		cadence:          cadence,
		rate:             deps.Rate,
		state:            deps.State,
	}
	return d, nil
}

// Evaluate satisfies Evaluated and does nothing. A network-wide rate is
// not a property of any one event -- see this type's own doc comment.
// The chassis requires Evaluate on every definition rather than making
// it optional, so that "this definition sees every event" is never
// something a reader has to go and check.
func (d *globalSpikeDefinition) Evaluate(store.Event) {}

// TickInterval satisfies Ticked.
func (d *globalSpikeDefinition) TickInterval() time.Duration { return globalSpikeCheckInterval }

// Tick satisfies Ticked: one EPS reading folded into the baseline, and a
// flag if the current rate clears both the absolute floor and the
// multiple of its own baseline.
func (d *globalSpikeDefinition) Tick(now time.Time) {
	if d.rate == nil {
		return
	}
	d.mu.Lock()
	if !d.def.Enabled {
		// Discard rather than freeze -- see this type's own doc comment
		// for why this definition re-primes on re-enable and rule_spike
		// deliberately does not.
		d.baseline = nil
		d.mu.Unlock()
		return
	}
	if d.baseline == nil {
		d.baseline = d.newBaseline()
	}
	current := d.rate.EventsPerSecond()
	before := d.baseline.Reading(now, current)
	d.maybePersistLocked(now)
	d.mu.Unlock()

	if !before.Ready {
		return
	}
	if current < d.minEPS || before.Value <= 0 || current < before.Value*d.multiplier {
		return
	}
	samples := before.Samples
	if d.warmupSamples > 0 && samples > d.warmupSamples {
		samples = d.warmupSamples
	}
	confidence := emaConfidence(before.ZScore, samples, d.warmupSamples)
	d.emit(Emission{
		Target: "global",
		Detail: fmt.Sprintf("%.1f events/s vs a baseline of %.1f (based on %d samples, %.1fσ above normal)",
			current, before.Value, samples, before.ZScore),
		Confidence: &confidence,
		EventTime:  now,
	})
}

func (d *globalSpikeDefinition) newBaseline() *Baseline {
	if d.state != nil {
		if persisted, ok := d.state.Get(d.def.ID, "global"); ok {
			return RestoreBaseline(0, d.floor, d.cadence, persisted)
		}
	}
	return NewBaseline(0, d.floor, d.cadence)
}

func (d *globalSpikeDefinition) maybePersistLocked(now time.Time) {
	if d.state == nil {
		return
	}
	if !d.lastPersisted.IsZero() && now.Sub(d.lastPersisted) < baselinePersistInterval {
		return
	}
	d.lastPersisted = now
	d.state.Set(d.def.ID, "global", d.baseline.State())
}

// Replay satisfies Replayable, and does so by reconstruction rather than
// by re-running the live reading -- which is worth being precise about,
// because the two are not the same thing.
//
// Live, this definition's reading is store.Store.EventsPerSecond(): the
// events counted in the last ten seconds of *wall-clock*, divided by ten.
// That number cannot be re-observed after the fact; it is a property of
// when this process happened to look, not of the corpus. What a corpus
// does carry is every event's own ReceivedAt, from which the same
// quantity -- events per second over a ten-second span -- is
// reconstructable exactly, at whatever instants the tick cadence would
// have sampled it.
//
// So the receipt answers "how often would this have fired over these
// events", which is the question auto-tune asks, and it answers it from
// the events rather than from a counter that no longer exists. It is not
// a claim that the live figures were identical: a burst that arrived in
// one clump but is timestamped across a minute reads differently to each,
// and the corpus's timestamps are the more truthful of the two.
func (d *globalSpikeDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	multiplier, minEPS := d.multiplier, d.minEPS
	if len(candidate) > 0 {
		validated, err := ValidateParams(globalSpikeReplaySchema, candidate)
		if err != nil {
			return Result{}, fmt.Errorf("engine: definition %q: replay candidate params: %w", d.def.ID, err)
		}
		if raw, ok := validated["multiplier"]; ok {
			multiplier = toFloat(raw)
		}
		if raw, ok := validated["minEPS"]; ok {
			minEPS = toFloat(raw)
		}
	}

	// One CountRing over the EPS window reproduces
	// eventsPerSecondLocked's "events in the last N seconds" exactly,
	// sampled at tick instants rather than continuously.
	events := NewCountRing(globalSpikeEPSWindow)
	baseline := NewBaseline(0, d.floor, d.cadence)
	var (
		emissionCount int
		sample        []ReplaySample
		nextTick      time.Time
	)

	fire := func(now time.Time) {
		current := float64(events.Count(now, globalSpikeEPSWindow)) / globalSpikeEPSWindow.Seconds()
		before := baseline.Reading(now, current)
		if !before.Ready || current < minEPS || before.Value <= 0 || current < before.Value*multiplier {
			return
		}
		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: "global",
				Detail: fmt.Sprintf("%.1f events/s vs a baseline of %.1f (%.1fσ above normal)", current, before.Value, before.ZScore),
			})
		}
	}

	corpusWindow := corpus.Replay(func(e store.Event) {
		now := e.ReceivedAt
		if nextTick.IsZero() {
			nextTick = now.Add(globalSpikeCheckInterval)
		}
		// Catch up any tick instants this event has moved past, so a
		// quiet stretch produces the same low readings it would have
		// live rather than being skipped over.
		for !now.Before(nextTick) {
			fire(nextTick)
			nextTick = nextTick.Add(globalSpikeCheckInterval)
		}
		events.Add(now, true)
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	if corpusWindow.Count == 0 || span < globalSpikeCheckInterval {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)), shorter than this definition's %s sampling interval -- there is not one full reading in it, so declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, globalSpikeCheckInterval),
			CorpusSpan:       span,
			DefinitionWindow: globalSpikeCheckInterval,
		}}, nil
	}

	w, err := NewWindow(corpusWindow.Start, corpusWindow.End, corpusWindow.Count)
	if err != nil {
		return Result{}, fmt.Errorf("engine: definition %q: replay: %w", d.def.ID, err)
	}
	receipt, err := NewReceipt(w, emissionCount, sample, corpusWindow.Truncated)
	if err != nil {
		return Result{}, fmt.Errorf("engine: definition %q: replay: %w", d.def.ID, err)
	}
	return Result{Receipt: &receipt}, nil
}

// globalSpikeReplaySchema is the closed set of candidate overrides --
// the two knobs that decide firing. warmupSamples scores confidence
// only, so sweeping it would change no count.
var globalSpikeReplaySchema = []ParamSchema{
	{Name: "multiplier", Type: ParamTypeFloat, Min: floatBound(0)},
	{Name: "minEPS", Type: ParamTypeFloat, Min: floatBound(0)},
}
