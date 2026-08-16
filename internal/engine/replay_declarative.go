// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// var _ Replayable = (*DeclarativeDefinition)(nil) pins the compile-time
// half of "declarative definitions ARE replayable" (issue #403's own
// phrase): DeclarativeDefinition is this issue's proof-of-contract
// implementation, and the only concrete Replayable this codebase ships
// yet -- the programmatic kind's own Replay/NonReplayable implementations
// are #405/#406's job, deliberately out of scope here (see
// NonReplayable's own doc comment, replayability.go).
var _ Replayable = (*DeclarativeDefinition)(nil)

// replayParamSchema is the closed set of params DeclarativeDefinition.Replay
// accepts as a candidate override: window and threshold, exactly the
// two knobs docs/decisions/evaluation-engine.md section 4's own
// auto-tune example tunes ("at X this would have fired 6 times, not
// 41"). Named to match shipped_params.go's own "window"/"threshold"
// convention (see e.g. PortScanParamSchema) so a future caller building
// a candidate Params value from a definition's own ParamSchema does not
// have to learn a second name for the same knob. Every other field a
// DeclarativeDefinition carries -- conditions, key mode, counting mode,
// its distinct field -- is structural: changed by editing the
// definition itself (#404's store), never swept by a replay candidate.
//
// Neither entry is Required: a caller may override just one, or
// neither (an empty/nil candidate simply replays this definition's own
// current window/threshold unchanged), matching ValidateParams' own
// "a missing, non-required param is simply absent" contract.
var replayParamSchema = []ParamSchema{
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second)},
	{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1)},
}

// Replay satisfies Replayable: re-runs this definition's conditions,
// window and threshold over corpus with candidate optionally overriding
// window and/or threshold, exactly as Evaluate (declarative.go) does
// against live traffic -- same condition matching (d.compiled.match),
// same keying (d.keyFor), same evidence accumulation (recordEvidence),
// same rendering (RenderEmission) -- but against fresh, call-local
// per-key state, never d.state: a replay call touches nothing a
// concurrent live Evaluate call (or another concurrent Replay call) is
// using, and is never itself affected by one. See
// TestDeclarativeReplayDoesNotTouchLiveState.
//
// DeclarativeDefinition has no baseline/history-floor concept at all
// (see baseline.go's own doc comment: only a definition that consults a
// Baseline can ever produce a provisional emission) -- every
// ReplaySample this method produces therefore has Provisional=false
// unconditionally, the same default Evaluate's own
// RenderEmission(..., false) call uses live. Receipt.AnyProvisional is
// consequently always false for this kind; a future baseline-backed
// programmatic definition's own Replay implementation (#405/#406) is
// where that field starts doing real work.
//
// No separate cap bounds how many distinct per-key states this call
// tracks, unlike live evaluation's Keyed[V] (maxTrackedKeys, keyed.go):
// live traffic has no ceiling at all, which is exactly why Keyed[V]
// needs one, where a replay corpus is already bounded by
// maxCorpusEvents (corpus.go) -- at most one new key per corpus event,
// so distinct-key cardinality here can never exceed a bound this call
// already enforces for an unrelated reason.
func (d *DeclarativeDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	window := d.window
	threshold := d.threshold

	if len(candidate) > 0 {
		validated, err := ValidateParams(replayParamSchema, candidate)
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
	}
	// window/threshold are already validated positive by
	// replayParamSchema's own Min bounds when candidate supplied them;
	// re-checked here to cover the "candidate left them unset, and this
	// definition's own stored value is somehow non-positive" case too
	// (unreachable in practice -- NewDeclarativeDefinition already
	// requires both positive -- but Replay does not get to assume its
	// caller only ever hands it a definition built through that
	// constructor).
	if window <= 0 {
		return Result{}, fmt.Errorf("engine: definition %q: replay window must be positive, got %s", d.def.ID, window)
	}
	if threshold <= 0 {
		return Result{}, fmt.Errorf("engine: definition %q: replay threshold must be positive, got %d", d.def.ID, threshold)
	}

	states := make(map[string]*declState)
	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		// Scope gates replay identically to live Evaluate (declarative.go)
		// -- added by #405 alongside Evaluate's own scope enforcement (see
		// scope_match.go); Replay predates that change and this definition's
		// receipt would otherwise overclaim by counting events live
		// evaluation would never have seen at all.
		if !scopeMatches(d.def.Scope, e) {
			return
		}
		if !d.compiled.match(e, d.members) {
			return
		}

		key := d.keyFor(e)
		st, ok := states[key]
		if !ok {
			st = d.newStateForWindow(window, e)
			states[key] = st
		}
		d.recordEvidence(st, e)

		now := e.ReceivedAt
		var count int
		switch d.countingMode {
		case CountingTotal:
			st.count.Add(now, true)
			count = st.count.Count(now, window)
		case CountingDistinct:
			v, ok := distinctFieldValue(d.distinctField, e)
			if !ok {
				return
			}
			st.distinct.Add(now, v)
			count = st.distinct.Count(now, window, nil)
		}

		if count < threshold {
			return
		}

		em, err := RenderEmission(st.evidence, count, st.detailTemplate, false)
		if err != nil {
			// Same "log and skip this one occurrence, keep replaying"
			// policy Evaluate itself uses (declarative.go): a render
			// failure here means the detail template references
			// evidence this definition's own conditions can never
			// populate -- a construction-time bug in the definition,
			// not something specific to this one corpus event, so
			// aborting the whole replay over it would only hide the
			// same failure Evaluate already logs live.
			logger.Error(fmt.Sprintf("declarative definition %q: replay RenderEmission failed: %v", d.def.ID, err))
			return
		}

		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: st.target,
				Detail: em.Detail,
				Ports:  em.Ports,
				Hosts:  em.Hosts,
				Labels: em.Labels,
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
