// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("rule_spike", buildRuleSpikeDefinition)
}

// ruleSpikeDefinition is rule_spike ported onto the chassis (issue #405):
// a firewall rule's own hit rate against a slow EMA baseline of itself,
// so a normally-quiet rule suddenly lighting up is visible even when it
// is nowhere near large enough to move the network-wide total.
// Programmatic rather than declarative because a statistical baseline is
// not a threshold-over-window predicate -- see Kind's own doc comment.
//
// # This is where #368 closes
//
// internal/detect's observeRuleRate had a rate floor (RuleSpikeMinRate)
// but no history floor, and it was the only one of the four EMA
// detectors without one. The consequence #368 documents: with
// DefaultConfig, perfectly steady traffic under a single rule label
// raised a spike at event 12 -- 2.2 seconds in -- claiming "0.2 hits/s
// vs a baseline of 0.0 ... 4.0σ above normal" when traffic had never
// changed. It recurred on every restart, because ruleWindows was
// in-memory only, and again the first time an operator followed
// docs/routeros-setup.md and gave a rule a new log-prefix.
//
// The mechanism was the window's own fill, not the traffic:
// currentRate = hits.Count(now, 60s)/60 is measured over a 60-second
// window that has only existed for two seconds, so it climbs
// monotonically as the ring fills regardless of what the network is
// doing. The baseline was primed from the first such reading (0.0167/s)
// and advanced at emaAlpha=0.02, so it stayed near zero while the rate
// climbed -- and `currentRate >= prevBaseline*5` was satisfied by the
// fill itself.
//
// #368's judge was explicit that gating only the *firing* check would
// have moved the false positive to the window edge rather than removing
// it, because the baseline would still have been primed from that same
// deflated first sample. Both halves are fixed here, and neither is
// rule_spike's own code:
//
//   - Baseline.Reading (baseline.go) refuses to prime at all until a
//     full window has been observed, so the first baseline value is a
//     fully-windowed rate rather than a still-filling ring's.
//   - BaselineFloor gates Snapshot.Ready, and this definition's firing
//     condition consults it, so no emission is possible below the floor.
//
// That is the point of closing #368 here rather than in #403: the
// chassis contract existing is not the same as the affected code obeying
// it. It obeys it now because there is no other way for a definition to
// get a baseline -- see baselineSet.
//
// Restart behaviour is the other half of #368's scenario, and it is now
// answered by persistence rather than by warm-up silence: baselineSet
// resumes a key from the engine-state store (#399/#400) when there is
// state to resume from, so a rule whose baseline was already warm before
// a restart does not spend another whole window blind. With no
// persistence configured it falls back to the warm-up silence the floor
// guarantees, which is still correct, just colder. Both are pinned --
// see TestShippedRuleSpikeNoFalseSpikeOnRestart.
type ruleSpikeDefinition struct {
	programmaticBase

	window        time.Duration
	multiplier    float64
	minRate       float64
	warmupSamples int

	hits      *Keyed[*CountRing]
	baselines *baselineSet
}

func buildRuleSpikeDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	multiplier, err := paramFloat(params, "multiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minRate, err := paramFloat(params, "minRate")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	warmup, err := paramIntOptional(params, "warmupSamples")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	floor, err := baselineFloorFromParams(params, window)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}

	return &ruleSpikeDefinition{
		programmaticBase: programmaticBase{def: def},
		window:           window,
		multiplier:       multiplier,
		minRate:          minRate,
		warmupSamples:    warmup,
		hits:             NewKeyed[*CountRing](),
		baselines:        newBaselineSet(def.ID, window, floor, cadence, deps.State),
	}, nil
}

// Evaluate satisfies Evaluated. Keyed on the rule label, which is also
// the emitted flag's Target -- internal/detect's own convention (see
// flags.Flag.Target's doc comment).
//
// Deliberately no "mark the baseline stale" reset when this definition is
// disabled, unlike what #267 finding 17 proposed for consistency with
// global_spike. Measured, it makes this detector worse, and the reason is
// the same one #368 is about: this definition derives its rate from a
// time-windowed ring that only fills while it is enabled, so re-priming
// on the first event after re-enabling primes against a nearly empty
// ring, and the ordinary refill back to normal traffic then reads as a
// spike. See TestShippedRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives.
func (d *ruleSpikeDefinition) Evaluate(e store.Event) {
	if e.RuleLabel == "" || !d.active(e) {
		return
	}
	// Scope's rule axis is the one this definition keys on, so it is
	// checked here as well as by active's general scopeMatches -- which
	// covers it, but only because Scope.Rules happens to be one of the
	// axes scope_match.go applies uniformly. Stated rather than assumed
	// because internal/detect's own gate was explicitly
	// scopeMatchesRule(rs.Scope, e.RuleLabel) and nothing else.
	now := e.ReceivedAt
	ring := d.hits.GetOrCreate(e.RuleLabel, now, func() *CountRing { return NewCountRing(d.window) })
	ring.Add(now, true)
	rate := float64(ring.Count(now, d.window)) / d.window.Seconds()

	before := d.baselines.reading(e.RuleLabel, now, rate)
	if !before.Ready {
		// Below the history floor, or not primed yet -- #368. No
		// emission at all, provisional or otherwise: a baseline of "we
		// have not been watching long enough to have one" cannot support
		// the claim this definition's Detail makes ("vs a baseline of X
		// for this rule"), so there is nothing honest to say yet.
		return
	}
	if rate < d.minRate || before.Value <= 0 || rate < before.Value*d.multiplier {
		return
	}

	// samples is capped at warmupSamples for display, reproducing
	// internal/detect's own capped counter (ruleWindow.sampleCount,
	// "capped at RuleSpikeWarmupSamples, same warmup-then-cap pattern").
	// Baseline.Samples itself is uncapped, which is right -- a cap on
	// the stored count would be a cap on history -- but the sentence an
	// operator reads and the confidence score both used the capped
	// number, and emaConfidence's own min(1, samples/warmup) makes the
	// two identical anyway.
	samples := before.Samples
	if d.warmupSamples > 0 && samples > d.warmupSamples {
		samples = d.warmupSamples
	}
	confidence := emaConfidence(before.ZScore, samples, d.warmupSamples)
	d.emit(Emission{
		Target: e.RuleLabel,
		Detail: fmt.Sprintf("%.1f hits/s vs a baseline of %.1f for this rule (based on %d samples, %.1fσ above normal)",
			rate, before.Value, samples, before.ZScore),
		Confidence: &confidence,
		EventTime:  now,
	})
}

// Replay satisfies Replayable: re-runs this definition's own logic over
// the corpus against fresh, call-local state, never touching the live
// rings or baselines. Candidate params override window/multiplier/minRate.
//
// A baseline-backed definition replays cold by construction -- it cannot
// borrow the live baseline without making the receipt a statement about
// state the corpus did not produce -- so the first window of any corpus
// is spent priming and produces nothing. That is exactly the same floor
// live evaluation obeys, and it is why the Decline below is not merely
// "the corpus is short": a corpus shorter than two windows cannot
// contain a single judgement this definition would have made.
func (d *ruleSpikeDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	window, multiplier, minRate := d.window, d.multiplier, d.minRate
	if len(candidate) > 0 {
		validated, err := ValidateParams(ruleSpikeReplaySchema, candidate)
		if err != nil {
			return Result{}, fmt.Errorf("engine: definition %q: replay candidate params: %w", d.def.ID, err)
		}
		if raw, ok := validated["window"]; ok {
			parsed, err := time.ParseDuration(raw.(string))
			if err != nil {
				return Result{}, fmt.Errorf("engine: definition %q: replay candidate window: %w", d.def.ID, err)
			}
			window = parsed
		}
		if raw, ok := validated["multiplier"]; ok {
			multiplier = toFloat(raw)
		}
		if raw, ok := validated["minRate"]; ok {
			minRate = toFloat(raw)
		}
	}
	if window <= 0 {
		return Result{}, fmt.Errorf("engine: definition %q: replay window must be positive, got %s", d.def.ID, window)
	}

	rings := map[string]*CountRing{}
	baselines := map[string]*Baseline{}
	floor := BaselineFloor{MinDuration: window}
	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		if e.RuleLabel == "" || !d.active(e) {
			return
		}
		now := e.ReceivedAt
		ring, ok := rings[e.RuleLabel]
		if !ok {
			ring = NewCountRing(window)
			rings[e.RuleLabel] = ring
		}
		ring.Add(now, true)
		rate := float64(ring.Count(now, window)) / window.Seconds()

		bl, ok := baselines[e.RuleLabel]
		if !ok {
			bl = NewBaseline(window, floor, UpdatePerEvent)
			baselines[e.RuleLabel] = bl
		}
		before := bl.Reading(now, rate)
		if !before.Ready || rate < minRate || before.Value <= 0 || rate < before.Value*multiplier {
			return
		}
		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: e.RuleLabel,
				Detail: fmt.Sprintf("%.1f hits/s vs a baseline of %.1f for this rule (%.1fσ above normal)", rate, before.Value, before.ZScore),
			})
		}
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	// Two windows, not one: the first is spent priming a baseline that
	// cannot be borrowed from live state, so a corpus shorter than that
	// has no judgement in it to count.
	if corpusWindow.Count == 0 || span < 2*window {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)); this definition needs a full %s to prime a baseline before it can judge anything, so at least %s of corpus -- declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, window, 2*window),
			CorpusSpan:       span,
			DefinitionWindow: 2 * window,
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

// ruleSpikeReplaySchema is the closed set of params Replay accepts as a
// candidate override -- the three knobs that decide when this definition
// fires. warmupSamples is deliberately absent: it scores confidence, it
// does not decide firing (see internal/detect.Config's own doc comment
// on the field), so sweeping it would change no count.
var ruleSpikeReplaySchema = []ParamSchema{
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second)},
	{Name: "multiplier", Type: ParamTypeFloat, Min: floatBound(0)},
	{Name: "minRate", Type: ParamTypeFloat, Min: floatBound(0)},
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return math.NaN()
	}
}
