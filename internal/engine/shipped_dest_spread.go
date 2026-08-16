// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("outbound_anomaly", buildOutboundAnomalyDefinition)
}

// destDirection selects which half of a LAN source's destination spread a
// destSpreadDefinition judges.
type destDirection int

const (
	// destExternal counts distinct *external* destinations one LAN source
	// contacted -- outbound_anomaly. One of the strongest signals of a
	// compromised/malware-infected device (C2 beaconing, botnet
	// participation): nothing else in this codebase is positioned to
	// notice "this device just started talking to 30 IPs it has never
	// touched before."
	destExternal destDirection = iota
	// destInternal counts distinct *internal* destinations -- a network
	// sweep, the classic lateral-movement signature of an attacker who
	// already has a foothold on the LAN.
	destInternal
)

// destSpreadDefinition is internal/detect's observeDestSpread ported onto
// the chassis (issue #405), one instance per direction.
//
// # Why two definitions where internal/detect had one function
//
// internal/detect kept a single destWindow per source holding both
// direction's rings, because the two detectors were "independently
// toggleable but share window state" -- an optimization, not a semantic:
// each detector only ever queried its own direction's ring, and each had
// its own threshold, its own window, its own scope and its own flag type.
// On the chassis the sharing has nowhere to live (a definition is the
// unit of registration, enablement, scope and params), and it buys
// nothing: one DistinctRing per direction per source is exactly what the
// shared struct held anyway.
//
// What the split does NOT change, and this is the part worth stating
// because getting it wrong would be invisible: internal/detect ran its
// threshold query on *every* qualifying event, whichever direction that
// event's destination happened to be. So an internal-destination event
// from a source already over the outbound threshold re-fired the outbound
// flag. That is reproduced literally here -- the event's direction
// decides only whether it is *recorded*, never whether the threshold is
// *checked*.
type destSpreadDefinition struct {
	programmaticBase

	direction destDirection
	window    time.Duration
	threshold int

	vpnInterfaces []string
	vpnMultiplier float64

	dests *Keyed[*DistinctRing[string]]
}

func buildOutboundAnomalyDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	return buildDestSpreadDefinition(def, deps, destExternal)
}

func buildDestSpreadDefinition(def Definition, _ ShippedDeps, direction destDirection) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
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

	return &destSpreadDefinition{
		programmaticBase: programmaticBase{def: def},
		direction:        direction,
		window:           window,
		threshold:        threshold,
		vpnInterfaces:    ifaces,
		vpnMultiplier:    vpnMult,
		dests:            NewKeyed[*DistinctRing[string]](),
	}, nil
}

// noun is the word this direction's Detail sentence uses -- the one
// string that differs between the two definitions' otherwise identical
// emissions, kept as a method so the sentence itself is written once.
func (d *destSpreadDefinition) noun() string {
	if d.direction == destInternal {
		return "internal"
	}
	return "external"
}

// tracks reports whether dstIP belongs to this definition's direction.
//
// Classifying at insert time rather than at query time is safe here, and
// is what internal/detect did for the same reason: unlike a scope, the
// public/private classification of a given address is static -- it never
// changes after the fact -- so routing an event to the matching ring on
// arrival is a pure optimization, not a departure from the
// filter-at-query-time rule (which is specifically about filters an
// operator can change while a window is still open).
func (d *destSpreadDefinition) tracks(dstIP string) bool {
	if d.direction == destInternal {
		return !isPublicIPAddress(dstIP)
	}
	return isPublicIPAddress(dstIP)
}

// Evaluate satisfies Evaluated.
//
// The scope gate is scopeMatchesSource rather than programmaticBase.active
// because internal/detect gated these two on scopeMatchesHost only -- the
// Hosts and Classification axes -- and explicitly documented Ports and
// Rules as ignored for them (see detect.Scope's per-detector field-usage
// table): the destination port is not what either definition is keyed or
// thresholded on, so admitting events by it would silently narrow a
// destination-breadth count to one port's worth of breadth.
func (d *destSpreadDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || e.DstIP == "" || !d.def.Enabled {
		return
	}
	// Only a LAN source's destination spread is meaningful: an external
	// source's is just internet background noise scanning many networks,
	// not one. internal/detect applied this in Observe, before
	// observeDestSpread was called at all.
	if isPublicIPAddress(e.SrcIP) {
		return
	}
	if !scopeMatchesSource(d.def.Scope, e.SrcIP) {
		return
	}
	// internal/detect's isTrackableConnState -- the mikroview#35/#36
	// database-server false positive: a busy server's established-
	// connection return traffic to many distinct clients must not read as
	// a network sweep.
	if e.ConnState != "" && e.ConnState != "new" {
		return
	}

	now := e.ReceivedAt
	ring := d.dests.GetOrCreate(e.SrcIP, now, func() *DistinctRing[string] {
		return NewDistinctRing[string](d.window)
	})
	if d.tracks(e.DstIP) {
		ring.Add(now, e.DstIP)
	}

	// Checked on every qualifying event, not only on one this direction
	// recorded -- see this type's own doc comment.
	count := ring.Count(now, d.window, nil)
	if count < d.threshold {
		return
	}

	hosts := ring.Values(now, d.window, nil)
	confidence := vpnBoostConfidence(
		overshootConfidence(count, d.threshold),
		d.vpnInterfaces, d.vpnMultiplier, e.InInterface)

	d.emit(Emission{
		Target: e.SrcIP,
		Detail: fmt.Sprintf("%d distinct %s destinations in %s", count, d.noun(), d.window) +
			vpnDetailSuffix(d.vpnInterfaces, e.InInterface),
		Confidence: &confidence,
		Hosts:      sortedHostsCapped(hosts),
		// No Country: internal/detect passed "" for both directions. The
		// emission is about a LAN source, whose country badge would be
		// meaningless, and the destinations it names are many.
		SourceIP:  e.SrcIP,
		EventTime: now,
	})
}

// Replay satisfies Replayable: the same per-source distinct-destination
// walk against fresh, call-local state. Candidate params override the
// threshold, the one knob that decides firing (the window decides what
// the corpus would have to contain, which the Decline below reports on
// instead).
func (d *destSpreadDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	threshold := d.threshold
	if len(candidate) > 0 {
		validated, err := ValidateParams(destSpreadReplaySchema, candidate)
		if err != nil {
			return Result{}, fmt.Errorf("engine: definition %q: replay candidate params: %w", d.def.ID, err)
		}
		if raw, ok := validated["threshold"]; ok {
			threshold = raw.(int)
		}
	}

	rings := map[string]*DistinctRing[string]{}
	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		if e.SrcIP == "" || e.DstIP == "" || !d.def.Enabled {
			return
		}
		if isPublicIPAddress(e.SrcIP) || !scopeMatchesSource(d.def.Scope, e.SrcIP) {
			return
		}
		if e.ConnState != "" && e.ConnState != "new" {
			return
		}
		now := e.ReceivedAt
		ring, ok := rings[e.SrcIP]
		if !ok {
			ring = NewDistinctRing[string](d.window)
			rings[e.SrcIP] = ring
		}
		if d.tracks(e.DstIP) {
			ring.Add(now, e.DstIP)
		}
		count := ring.Count(now, d.window, nil)
		if count < threshold {
			return
		}
		emissionCount++
		if len(sample) < replaySampleBound {
			sample = append(sample, ReplaySample{
				At:     now,
				Target: e.SrcIP,
				Detail: fmt.Sprintf("%d distinct %s destinations in %s", count, d.noun(), d.window),
			})
		}
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	if corpusWindow.Count == 0 || span < d.window {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)), shorter than this definition's %s window -- declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, d.window),
			CorpusSpan:       span,
			DefinitionWindow: d.window,
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

// destSpreadReplaySchema is the closed set of candidate overrides -- the
// one knob that decides firing. vpnConfidenceMultiplier scores confidence
// only, so sweeping it would change no count.
var destSpreadReplaySchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1)},
}
