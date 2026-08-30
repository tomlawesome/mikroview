// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("low_slow_scan", buildLowSlowScanDefinition)
}

// lowSlowScanWarmupSamples is smaller than activity_spike's own
// warmupSamples (20) -- each sample here already represents a whole
// window-sized snapshot of a source's behaviour (hours), not one event,
// so far fewer are needed before a baseline is trustworthy. Lifted
// unchanged from internal/detect.lowSlowScanWarmupSamples, and kept a
// constant rather than becoming a param for the same reason it was one
// there: it is a property of what a sample *is* for this definition, not
// a threshold an operator tunes.
const lowSlowScanWarmupSamples = 10

// lowSlowScanDefinition is low_slow_scan ported onto the chassis (issue
// #405): a port scan deliberately paced to stay under port_scan's
// short-burst threshold -- one new port/host every few minutes rather
// than fifteen in a minute (#20).
//
// # Four independent signals, and why the weakest one wins
//
// This is the most intricate definition in the shipped catalogue, and
// the intricacy is the point. Judged over hours, a single "distinct
// ports per hour" count is hopeless: container orchestration, health
// checks and an ordinary browser all accumulate distinct destinations
// slowly. So four axes must clear independently --
//
//   - destination breadth: distinct ports AND distinct hosts, each past
//     its own threshold (a vertical scan of one host is port_scan's
//     territory; a horizontal probe of one port is not a scan);
//   - drop ratio: most of what this source tried was refused, which
//     paced scan traffic is and ordinary low-rate access to real
//     services is not;
//   - this source's own EMA baseline of its destination breadth, so a
//     host that legitimately talks to many things is judged against its
//     own normal;
//   - an observation floor: the source must have been watched for at
//     least minObservation before it is eligible at all.
//
// -- and then confidence is the *minimum* of the four axes' own scores,
// not the maximum or a blend. Several independent signals must each be
// convincing, not just the strongest one; a flag whose drop ratio only
// barely cleared reads as barely-confident even if its port breadth is
// enormous. That rule is reproduced here exactly, including which four
// numbers it minimises over.
//
// # Where each floor lives
//
// minObservation is this definition's BaselineFloor duration (see
// LowSlowScanParamSchema's own note, and BaselineFloor's doc comment):
// internal/detect expressed it as `now.Sub(w.firstSeen) >= MinObservation`
// alongside a separate `w.primed` check, which is exactly
// Snapshot.Ready's conjunction of primed-and-floor-cleared. primeWindow
// is zero, not the definition's window: internal/detect primed this
// baseline on the very first reading, and deferring that to a full
// window would move the earliest possible flag from 45 minutes to three
// hours -- a behaviour change #405 is not licensed to make (see
// baselineSet.primeWindow).
type lowSlowScanDefinition struct {
	programmaticBase

	window         time.Duration
	portThreshold  int
	hostThreshold  int
	minObservation time.Duration
	dropRatio      float64
	multiplier     float64

	tracks    *Keyed[*lowSlowTrack]
	baselines *baselineSet
}

// lowSlowTrack is one source's three purpose-sized rings, all sized to
// this definition's window: distinct destination ports, distinct
// destination hosts, and a drop/reject-vs-total tally. Three rings
// rather than one sample slice, unchanged from internal/detect's
// lowSlowWindow.
type lowSlowTrack struct {
	ports *DistinctRing[int]
	hosts *DistinctRing[string]
	drops *CountRing
}

func buildLowSlowScanDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	portThreshold, err := paramInt(params, "portThreshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	hostThreshold, err := paramInt(params, "hostThreshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minObservation, err := paramDuration(params, "minObservation")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	dropRatio, err := paramFloat(params, "dropRatio")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	multiplier, err := paramFloat(params, "baselineMultiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}

	return &lowSlowScanDefinition{
		programmaticBase: programmaticBase{def: def},
		window:           window,
		portThreshold:    portThreshold,
		hostThreshold:    hostThreshold,
		minObservation:   minObservation,
		dropRatio:        dropRatio,
		multiplier:       multiplier,
		tracks:           NewKeyed[*lowSlowTrack](),
		baselines: newBaselineSet(def.ID, 0,
			BaselineFloor{MinDuration: minObservation}, cadence, deps.State),
	}, nil
}

// Evaluate satisfies Evaluated.
//
// The scope gate is deliberately not programmaticBase.active: the Ports
// axis is applied at query time here, not as a gate on which events are
// tracked -- see scopeMatchesSource's own doc comment, and portFilter
// below.
func (d *lowSlowScanDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || !d.def.Enabled || !scopeMatchesSource(d.def.Scope, e.SrcIP) {
		return
	}
	// internal/detect's isTrackableConnState: RouterOS commonly logs both
	// directions of an established connection on one stateful accept
	// rule, and a busy server's ordinary return traffic would trivially
	// accumulate destination breadth meant to catch new connection
	// attempts (#35). An empty ConnState -- a setup that does not log
	// connection state -- stays trackable.
	if e.ConnState != "" && e.ConnState != "new" {
		return
	}

	now := e.ReceivedAt
	tr := d.tracks.GetOrCreate(e.SrcIP, now, func() *lowSlowTrack {
		return &lowSlowTrack{
			ports: NewDistinctRing[int](d.window),
			hosts: NewDistinctRing[string](d.window),
			drops: NewCountRing(d.window),
		}
	})

	// Recorded unconditionally; port != 0 / scope / dstIP != "" are
	// query-time filters below rather than insert-time ones, so a live
	// scope change takes effect on the very next query instead of once
	// samples recorded under the old scope age out.
	tr.ports.Add(now, e.DstPort)
	tr.hosts.Add(now, e.DstIP)
	tr.drops.Add(now, e.Action == store.ActionDrop || e.Action == store.ActionReject)

	portFilter := func(p int) bool {
		return p != 0 && matchesList(d.def.Scope.Ports, d.def.Scope.PortsMode, p)
	}
	hostFilter := func(h string) bool { return h != "" }
	portCount := tr.ports.Count(now, d.window, portFilter)
	hostCount := tr.hosts.Count(now, d.window, hostFilter)
	dropCount, total := tr.drops.Ratio(now, d.window)
	breadth := float64(portCount + hostCount)

	// The reading is folded in here and the pre-reading snapshot judged
	// below, so a flag compares against the baseline as it stood *before*
	// this reading -- internal/detect's own ordering, which it achieved
	// by doing the whole check before its emaUpdate call.
	before := d.baselines.reading(e.SrcIP, now, breadth)

	// Ready is primed AND past the observation floor -- internal/detect's
	// `w.primed` and its `observedLongEnough` axis, which are the same
	// conjunction once minObservation is declared as this definition's
	// BaselineFloor duration.
	if !before.Ready {
		return
	}
	if portCount < d.portThreshold || hostCount < d.hostThreshold {
		return
	}
	observedDropRatio := 0.0
	if total > 0 {
		observedDropRatio = float64(dropCount) / float64(total)
	}
	if total == 0 || observedDropRatio < d.dropRatio {
		return
	}
	if before.ZScore < emaMinZ || before.Value <= 0 || breadth < before.Value*d.multiplier {
		return
	}

	baselineConfidence := emaConfidence(before.ZScore, before.Samples, lowSlowScanWarmupSamples)
	dropConfidence := int(math.Round(math.Min(1, math.Max(0,
		(observedDropRatio-d.dropRatio)/(1-d.dropRatio))) * 100))
	portConfidence := overshootConfidence(portCount, d.portThreshold)
	hostConfidence := overshootConfidence(hostCount, d.hostThreshold)

	// The weakest-clearing axis bounds overall confidence -- see this
	// type's own doc comment. Reproduced as a literal minimum over the
	// same four values, in the same order, as internal/detect.
	confidence := portConfidence
	for _, c := range []int{hostConfidence, dropConfidence, baselineConfidence} {
		if c < confidence {
			confidence = c
		}
	}

	// Values only called now that Count has already shown the thresholds
	// crossed -- see DistinctRing.Values' own doc comment for why that
	// ordering matters (Count is allocation-free, Values is not).
	distinctPorts := tr.ports.Values(now, d.window, portFilter)
	distinctHosts := tr.hosts.Values(now, d.window, hostFilter)

	d.emit(Emission{
		Target: e.SrcIP,
		Detail: fmt.Sprintf(
			"%d distinct ports, %d distinct hosts over %s (%.0f%% drop/reject, %.1fσ above this source's normal breadth)",
			portCount, hostCount, d.window, observedDropRatio*100, before.ZScore),
		Confidence: &confidence,
		Ports:      sortedPortsCapped(distinctPorts),
		Hosts:      sortedHostsCapped(distinctHosts),
		Country:    e.SrcCountry,
		SourceIP:   e.SrcIP,
		EventTime:  now,
	})
}

// Learning satisfies LearningReporter: one baseline per source, so Ready
// answers how many sources have cleared minObservation -- see
// baselineSet.learning and learningStateFrom for the shared read/reduce
// this and every other baseline-backed shipped definition rely on.
func (d *lowSlowScanDefinition) Learning(now time.Time) (LearningState, bool) {
	return learningStateFrom(d.baselines.floor, d.baselines.learning(now)), true
}

// Replay satisfies Replayable: the same four-axis walk against fresh,
// call-local state, touching none of this definition's live rings or
// baselines.
//
// Candidate params override the four that decide firing -- the two
// breadth thresholds, the drop ratio and the baseline multiplier. The
// window and the observation floor are deliberately not sweepable here:
// both change what the corpus would have to *contain* rather than how a
// contained pattern is judged, and a corpus long enough to answer a
// three-hour window honestly is exactly what the Decline below exists to
// report the absence of.
func (d *lowSlowScanDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	portThreshold, hostThreshold := d.portThreshold, d.hostThreshold
	dropRatio, multiplier := d.dropRatio, d.multiplier
	if len(candidate) > 0 {
		validated, err := ValidateParams(lowSlowScanReplaySchema, candidate)
		if err != nil {
			return Result{}, fmt.Errorf("engine: definition %q: replay candidate params: %w", d.def.ID, err)
		}
		if raw, ok := validated["portThreshold"]; ok {
			portThreshold = raw.(int)
		}
		if raw, ok := validated["hostThreshold"]; ok {
			hostThreshold = raw.(int)
		}
		if raw, ok := validated["dropRatio"]; ok {
			dropRatio = toFloat(raw)
		}
		if raw, ok := validated["baselineMultiplier"]; ok {
			multiplier = toFloat(raw)
		}
	}

	tracks := map[string]*lowSlowTrack{}
	baselines := map[string]*Baseline{}
	floor := BaselineFloor{MinDuration: d.minObservation}
	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		if e.SrcIP == "" || !d.def.Enabled || !scopeMatchesSource(d.def.Scope, e.SrcIP) {
			return
		}
		if e.ConnState != "" && e.ConnState != "new" {
			return
		}
		now := e.ReceivedAt
		tr, ok := tracks[e.SrcIP]
		if !ok {
			tr = &lowSlowTrack{
				ports: NewDistinctRing[int](d.window),
				hosts: NewDistinctRing[string](d.window),
				drops: NewCountRing(d.window),
			}
			tracks[e.SrcIP] = tr
		}
		tr.ports.Add(now, e.DstPort)
		tr.hosts.Add(now, e.DstIP)
		tr.drops.Add(now, e.Action == store.ActionDrop || e.Action == store.ActionReject)

		portFilter := func(p int) bool {
			return p != 0 && matchesList(d.def.Scope.Ports, d.def.Scope.PortsMode, p)
		}
		hostFilter := func(h string) bool { return h != "" }
		portCount := tr.ports.Count(now, d.window, portFilter)
		hostCount := tr.hosts.Count(now, d.window, hostFilter)
		dropCount, total := tr.drops.Ratio(now, d.window)
		breadth := float64(portCount + hostCount)

		bl, ok := baselines[e.SrcIP]
		if !ok {
			bl = NewBaseline(0, floor, UpdatePerEvent)
			baselines[e.SrcIP] = bl
		}
		before := bl.Reading(now, breadth)
		if !before.Ready || portCount < portThreshold || hostCount < hostThreshold {
			return
		}
		observedDropRatio := 0.0
		if total > 0 {
			observedDropRatio = float64(dropCount) / float64(total)
		}
		if total == 0 || observedDropRatio < dropRatio {
			return
		}
		if before.ZScore < emaMinZ || before.Value <= 0 || breadth < before.Value*multiplier {
			return
		}
		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: e.SrcIP,
				Detail: fmt.Sprintf(
					"%d distinct ports, %d distinct hosts over %s (%.0f%% drop/reject, %.1fσ above this source's normal breadth)",
					portCount, hostCount, d.window, observedDropRatio*100, before.ZScore),
			})
		}
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	// The observation floor, not the window, is what a corpus has to
	// cover before this definition could have fired at all: nothing under
	// minObservation is eligible however broad it looks. A corpus shorter
	// than that would report a confident zero for a definition that was
	// structurally barred from firing, which is the specific dishonesty
	// Decline exists to avoid.
	required := d.minObservation
	if d.window > required {
		required = d.window
	}
	if corpusWindow.Count == 0 || span < required {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)), shorter than this definition's %s window and %s observation floor -- declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, d.window, d.minObservation),
			CorpusSpan:       span,
			DefinitionWindow: required,
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

// lowSlowScanReplaySchema is the closed set of candidate overrides -- see
// Replay's own doc comment for why window/minObservation are not in it.
var lowSlowScanReplaySchema = []ParamSchema{
	{Name: "portThreshold", Type: ParamTypeInt, Min: floatBound(1)},
	{Name: "hostThreshold", Type: ParamTypeInt, Min: floatBound(1)},
	{Name: "dropRatio", Type: ParamTypeFloat, Min: floatBound(0), Max: floatBound(1)},
	{Name: "baselineMultiplier", Type: ParamTypeFloat, Min: floatBound(0)},
}
