package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type destSample struct {
	at    time.Time
	dstIP string
}

type destWindow struct {
	samples      []destSample
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
		w = &destWindow{}
		d.destWindows[e.SrcIP] = w
	}
	w.lastActivity = now
	w.samples = append(w.samples, destSample{at: now, dstIP: e.DstIP})

	window := d.cfg.OutboundAnomalyWindow
	if d.cfg.InternalReconWindow > window {
		window = d.cfg.InternalReconWindow
	}
	cutoff := now.Add(-window)
	i := 0
	for i < len(w.samples) && w.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		w.samples = w.samples[i:]
	}

	outboundCutoff := now.Add(-d.cfg.OutboundAnomalyWindow)
	reconCutoff := now.Add(-d.cfg.InternalReconWindow)
	external := make(map[string]struct{})
	internal := make(map[string]struct{})
	for _, s := range w.samples {
		if s.dstIP == "" {
			continue
		}
		if isPublic(s.dstIP) {
			if oaActive && !s.at.Before(outboundCutoff) {
				external[s.dstIP] = struct{}{}
			}
		} else if irActive && !s.at.Before(reconCutoff) {
			internal[s.dstIP] = struct{}{}
		}
	}

	if oaActive && len(external) >= d.cfg.OutboundAnomalyThreshold {
		d.fs.AddWithConfidence(flags.TypeOutboundAnomaly, e.SrcIP,
			fmt.Sprintf("%d distinct external destinations in %s", len(external), d.cfg.OutboundAnomalyWindow),
			overshootConfidence(len(external), d.cfg.OutboundAnomalyThreshold), now)
	}
	if irActive && len(internal) >= d.cfg.InternalReconThreshold {
		d.fs.AddWithConfidence(flags.TypeInternalRecon, e.SrcIP,
			fmt.Sprintf("%d distinct internal destinations in %s", len(internal), d.cfg.InternalReconWindow),
			overshootConfidence(len(internal), d.cfg.InternalReconThreshold), now)
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
