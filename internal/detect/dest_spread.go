package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type destWindow struct {
	// external/internal replace a single []destSample slice with two
	// rings (see window.go), each sized to its own direction's window
	// (OutboundAnomalyWindow/InternalReconWindow). Unlike a
	// SettingsStore scope, isPublic's public/private classification is
	// static for a given IP -- it never changes after the fact -- so
	// routing an event to the matching ring at insert time is a pure
	// optimization over classifying at query time, not a departure from
	// the filter-at-query-time rule (which is specifically about
	// filters that can change live).
	external *distinctRing[string]
	internal *distinctRing[string]

	lastActivity time.Time
}

// observeDestSpread tracks, per LAN source, the distinct destination IPs
// it has contacted recently -- shared state for two directionally
// opposite detectors, split by classifying each destination as the
// samples are read rather than tracked separately, the same way
// observeScanAndSpike computes both port-scan and activity-spike from
// one shared window:
//
//   - distinct *external* destinations -> TypeOutboundAnomaly. One of the
//     strongest signals of a compromised/malware-infected LAN device
//     (C2 beaconing, botnet participation) -- nothing else in this
//     codebase is positioned to notice "this device just started
//     talking to 30 IPs it's never touched before."
//   - distinct *internal* destinations -> TypeInternalRecon. A network
//     sweep: the classic lateral-movement signature of an attacker who
//     already has a foothold on the LAN, scanning for what else is
//     reachable.
//
// Only called for events whose source is itself private (see Observe) --
// an external source's destination spread isn't meaningful here, since
// it's just internet background noise scanning many networks, not one.
func (d *Detector) observeDestSpread(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	// Independently toggleable even though they share destWindow/
	// w.samples below -- both consulted once up front so a detector
	// that's off contributes no work beyond this pair of settings
	// lookups, and short-circuits entirely if neither wants this event.
	oa := d.settings.Get(DetectorOutboundAnomaly)
	ir := d.settings.Get(DetectorInternalRecon)
	oaActive := oa.Enabled && scopeMatchesHost(oa.Scope, e.SrcIP)
	irActive := ir.Enabled && scopeMatchesHost(ir.Scope, e.SrcIP)
	if !oaActive && !irActive {
		return
	}

	w, ok := d.destWindows[e.SrcIP]
	if !ok {
		if len(d.destWindows) >= maxTrackedSources {
			d.evictOldestDestWindow()
		}
		w = &destWindow{
			external: newDistinctRing[string](d.cfg.OutboundAnomalyWindow),
			internal: newDistinctRing[string](d.cfg.InternalReconWindow),
		}
		d.destWindows[e.SrcIP] = w
	}
	w.lastActivity = now

	// Recorded unconditionally (regardless of oaActive/irActive, like
	// the old shared w.samples slice) -- only the query below is gated
	// per-detector.
	if e.DstIP != "" {
		if isPublic(e.DstIP) {
			w.external.Add(now, e.DstIP)
		} else {
			w.internal.Add(now, e.DstIP)
		}
	}

	if oaActive {
		external := w.external.Count(now, d.cfg.OutboundAnomalyWindow, nil)
		if external >= d.cfg.OutboundAnomalyThreshold {
			hosts := w.external.Values(now, d.cfg.OutboundAnomalyWindow, nil)
			isNew := d.fs.AddWithDetail(flags.TypeOutboundAnomaly, e.SrcIP,
				fmt.Sprintf("%d distinct external destinations in %s", external, d.cfg.OutboundAnomalyWindow),
				overshootConfidence(external, d.cfg.OutboundAnomalyThreshold),
				flags.Evidence{Hosts: sortedHostsCapped(hosts)}, "", now)
			d.maybeCheckGroupReputation(flags.TypeOutboundAnomaly, e.SrcIP, hosts, isNew)
		}
	}
	if irActive {
		internal := w.internal.Count(now, d.cfg.InternalReconWindow, nil)
		if internal >= d.cfg.InternalReconThreshold {
			hosts := w.internal.Values(now, d.cfg.InternalReconWindow, nil)
			d.fs.AddWithDetail(flags.TypeInternalRecon, e.SrcIP,
				fmt.Sprintf("%d distinct internal destinations in %s", internal, d.cfg.InternalReconWindow),
				overshootConfidence(internal, d.cfg.InternalReconThreshold),
				flags.Evidence{Hosts: sortedHostsCapped(hosts)}, "", now)
		}
	}
}

func (d *Detector) evictOldestDestWindow() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, w := range d.destWindows {
		if first || w.lastActivity.Before(oldest) {
			oldestKey, oldest, first = k, w.lastActivity, false
		}
	}
	if oldestKey != "" {
		delete(d.destWindows, oldestKey)
	}
}
