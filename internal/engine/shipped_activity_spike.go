// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("activity_spike", buildActivitySpikeDefinition)
}

// hostActivityMinSamples is a hard floor: a source with fewer prior
// observations than this can never raise a flag, however extreme its
// first few readings look. A baseline built from one or two samples is
// not a baseline -- there is nothing to have deviated from yet. Lifted
// unchanged from internal/detect.hostActivityMinSamples.
const hostActivityMinSamples = 5

// activitySpikeDefinition is activity_spike ported onto the chassis
// (issue #405): one source's own event rate against an EMA baseline of
// itself, so a host that is always busy is judged against its own normal
// rather than one fixed threshold applied to every host equally.
//
// # Ported as-is, including a known defect
//
// #420 reports that this detector is structurally unfireable through the
// ordinary event path at shipped defaults: the baseline folds in the
// current reading on every event, and the window's live count can only
// change by +1 per call, so the baseline can never lag the observed rate
// by more than 1/emaAlpha = 50 events. Firing needs the rate to clear
// both the absolute threshold (200) and 3x the baseline, and by the time
// any single-source ramp reaches 200 the baseline has caught up to
// within ~50 of it, so the 3x condition cannot be satisfied.
//
// That is NOT fixed here, deliberately. #420 records that the remedy is a
// design decision -- freeze the baseline fold-in while a candidate spike
// is running, fold in on a slower cadence than per-event, or retune the
// multiplier/alpha relationship -- and #405's own scope rule is that no
// behaviour change rides along with the port. So this definition
// reproduces the arithmetic exactly, and #420 stays open with the
// decision still to make.
//
// Two things the port does leave in place for whoever takes #420 on. The
// cadence is already expressible: UpdateCadence exists precisely so
// "fold in per window rather than per event" is a declaration this
// definition can make rather than a rewrite (see UpdateCadence's own doc
// comment, which cites #420 by name). And replay-with-receipts can
// demonstrate the detector firing on recorded data before the claim
// returns to the UI, which is #420's own stated bar for closing.
type activitySpikeDefinition struct {
	programmaticBase

	window        time.Duration
	threshold     int
	multiplier    float64
	warmupSamples int
	vpnInterfaces []string
	vpnMultiplier float64

	counts    *Keyed[*CountRing]
	baselines *baselineSet
}

func buildActivitySpikeDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	multiplier, err := paramFloat(params, "baselineMultiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	warmup, err := paramIntOptional(params, "warmupSamples")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	ifaces, err := paramStringList(params, "vpnInterfaces")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	vpnMult, err := paramFloatOptional(params, "vpnConfidenceMultiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	// primeWindow zero: internal/detect primed this baseline on the very
	// first reading, and the firing floor is a sample count
	// (hostActivityMinSamples), not a duration. Deferring priming to a
	// full window would change when this can fire, which #405 is not
	// licensed to do -- see baselineSet.primeWindow. The operator-set
	// baselineFloorDuration (#399, seeded at zero) can still add a
	// duration dimension on top.
	floor, err := baselineFloorFromParams(params, 0)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	floor.MinSamples = hostActivityMinSamples

	return &activitySpikeDefinition{
		programmaticBase: programmaticBase{def: def},
		window:           window,
		threshold:        threshold,
		multiplier:       multiplier,
		warmupSamples:    warmup,
		vpnInterfaces:    ifaces,
		vpnMultiplier:    vpnMult,
		counts:           NewKeyed[*CountRing](),
		baselines:        newBaselineSet(def.ID, 0, floor, cadence, deps.State),
	}, nil
}

// Evaluate satisfies Evaluated: one per-source rolling event count,
// judged against that source's own EMA baseline.
//
// The connection-state filter is internal/detect's isTrackableConnState,
// and it is not incidental: RouterOS commonly logs both directions of an
// established connection on one stateful accept rule, so without it a
// busy server's ordinary return traffic trivially crosses a threshold
// meant to catch new activity (mikroview issue #35). An empty
// ConnState -- a setup that does not log connection state at all -- is
// treated as trackable rather than discarded, so those deployments keep
// today's behaviour.
func (d *activitySpikeDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || !d.active(e) {
		return
	}
	if e.ConnState != "" && e.ConnState != "new" {
		return
	}

	now := e.ReceivedAt
	ring := d.counts.GetOrCreate(e.SrcIP, now, func() *CountRing { return NewCountRing(d.window) })
	ring.Add(now, true)
	d.checkBaseline(e.SrcIP, e.SrcCountry, e.InInterface, ring.Count(now, d.window), now)
}

// checkBaseline is the baseline half of Evaluate, split out at exactly
// the seam internal/detect had (Observe computed the windowed count,
// checkHostActivityBaseline judged it) -- which is not a stylistic echo:
// #397's characterization of this detector had to drive that function
// directly, because #420 means no input through the ordinary event path
// can make it fire at shipped defaults. Keeping the seam is what lets
// those pins move across unchanged instead of being rewritten into
// something that tests a different thing.
func (d *activitySpikeDefinition) checkBaseline(srcIP, country, iface string, count int, now time.Time) {
	before := d.baselines.reading(srcIP, now, float64(count))
	if !before.Ready {
		return
	}
	rate := float64(count)
	if before.ZScore < emaMinZ || rate < float64(d.threshold) || before.Value <= 0 || rate < before.Value*d.multiplier {
		return
	}

	samples := before.Samples
	if d.warmupSamples > 0 && samples > d.warmupSamples {
		samples = d.warmupSamples
	}
	confidence := vpnBoostConfidence(
		emaConfidence(before.ZScore, samples, d.warmupSamples),
		d.vpnInterfaces, d.vpnMultiplier, iface)

	detail := fmt.Sprintf(
		"%d events in %s vs a baseline of %.1f for this host (based on %d samples, %.1fσ above normal)",
		count, d.window, before.Value, samples, before.ZScore,
	) + vpnDetailSuffix(d.vpnInterfaces, iface)

	d.emit(Emission{
		Target:     srcIP,
		Detail:     detail,
		Confidence: &confidence,
		Country:    country,
		SourceIP:   srcIP,
		EventTime:  now,
	})
}

// Replay satisfies Replayable: the same per-source count-and-baseline
// walk against fresh, call-local state. Candidate params override
// window/threshold/baselineMultiplier -- the three that decide firing.
//
// Worth stating plainly given #420: a replay of this definition over a
// corpus is expected to report zero at shipped defaults, and that zero is
// a true statement about the definition rather than a failure of the
// replay. It is also exactly the instrument #420 asks for -- "replay
// with receipts should be able to demonstrate the detector actually
// firing on recorded data before the claim returns to the UI" -- so
// whoever takes #420 on can sweep the multiplier here and see the
// crossover point move.
func (d *activitySpikeDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	window, threshold, multiplier := d.window, d.threshold, d.multiplier
	if len(candidate) > 0 {
		validated, err := ValidateParams(activitySpikeReplaySchema, candidate)
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
		if raw, ok := validated["threshold"]; ok {
			threshold = raw.(int)
		}
		if raw, ok := validated["baselineMultiplier"]; ok {
			multiplier = toFloat(raw)
		}
	}
	if window <= 0 {
		return Result{}, fmt.Errorf("engine: definition %q: replay window must be positive, got %s", d.def.ID, window)
	}

	rings := map[string]*CountRing{}
	baselines := map[string]*Baseline{}
	floor := BaselineFloor{MinSamples: hostActivityMinSamples}
	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		if e.SrcIP == "" || !d.active(e) {
			return
		}
		if e.ConnState != "" && e.ConnState != "new" {
			return
		}
		now := e.ReceivedAt
		ring, ok := rings[e.SrcIP]
		if !ok {
			ring = NewCountRing(window)
			rings[e.SrcIP] = ring
		}
		ring.Add(now, true)
		count := ring.Count(now, window)

		bl, ok := baselines[e.SrcIP]
		if !ok {
			bl = NewBaseline(0, floor, UpdatePerEvent)
			baselines[e.SrcIP] = bl
		}
		before := bl.Reading(now, float64(count))
		rate := float64(count)
		if !before.Ready || before.ZScore < emaMinZ || rate < float64(threshold) ||
			before.Value <= 0 || rate < before.Value*multiplier {
			return
		}
		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: e.SrcIP,
				Detail: fmt.Sprintf("%d events in %s vs a baseline of %.1f for this host (%.1fσ above normal)", count, window, before.Value, before.ZScore),
			})
		}
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	if corpusWindow.Count == 0 || span < window {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)), shorter than this definition's %s window -- declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, window),
			CorpusSpan:       span,
			DefinitionWindow: window,
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

var activitySpikeReplaySchema = []ParamSchema{
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second)},
	{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1)},
	{Name: "baselineMultiplier", Type: ParamTypeFloat, Min: floatBound(0)},
}
