// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type destWindow struct {
	// internal is the internal-destination half of what used to be two
	// rings here (see window.go), sized to InternalReconWindow. The
	// external half moved to internal/engine with outbound_anomaly (issue
	// #405, see shipped_dest_spread.go). Unlike a SettingsStore scope,
	// isPublic's public/private classification is static for a given IP --
	// it never changes after the fact -- so routing an event to the
	// matching ring at insert time is a pure optimization over
	// classifying at query time, not a departure from the
	// filter-at-query-time rule (which is specifically about filters that
	// can change live).
	internal *distinctRing[string]

	lastActivity time.Time
}

// observeDestSpread tracks, per LAN source, the distinct *internal*
// destination IPs it has contacted recently -> TypeInternalRecon. A
// network sweep: the classic lateral-movement signature of an attacker
// who already has a foothold on the LAN, scanning for what else is
// reachable.
//
// Its directionally opposite twin, outbound_anomaly, moved to
// internal/engine as a shipped programmatic definition (issue #405, see
// shipped_dest_spread.go) and took the external ring with it. The two
// shared one destWindow here purely as an optimization -- each only ever
// queried its own direction's ring -- so the split cost nothing.
//
// Only called for events whose source is itself private (see Observe) --
// an external source's destination spread isn't meaningful here, since
// it's just internet background noise scanning many networks, not one.
func (d *Detector) observeDestSpread(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	ir := d.settings.Get(DetectorInternalRecon)
	if !ir.Enabled || !scopeMatchesHost(ir.Scope, e.SrcIP) {
		return
	}

	w, ok := d.destWindows[e.SrcIP]
	if !ok {
		if len(d.destWindows) >= maxTrackedSources {
			evictOldestByActivity(d.destWindows)
		}
		w = &destWindow{internal: newDistinctRing[string](d.cfg.InternalReconWindow)}
		d.destWindows[e.SrcIP] = w
	}
	w.lastActivity = now

	// Recorded unconditionally -- only the query below is gated.
	if e.DstIP != "" && !isPublic(e.DstIP) {
		w.internal.Add(now, e.DstIP)
	}

	// See vpn.go's vpnBoostConfidence for the mechanism and reasoning
	// (issue #105): an anomaly whose triggering event arrived via a
	// VPN-tagged interface (Config.VPNInterfaces) is scored more
	// confidently than the identical anomaly from an ordinary LAN
	// interface. e.InInterface empty, or matching no configured pattern
	// (the default, empty VPNInterfaces), leaves scoring completely
	// unchanged.
	vpnDetailSuffix := ""
	if isVPNInterface(d.cfg.VPNInterfaces, e.InInterface) {
		vpnDetailSuffix = fmt.Sprintf(" -- arrived via VPN interface %q, scored more confidently as an already-authenticated remote peer", e.InInterface)
	}

	internal := w.internal.Count(now, d.cfg.InternalReconWindow, nil)
	if internal >= d.cfg.InternalReconThreshold {
		hosts := w.internal.Values(now, d.cfg.InternalReconWindow, nil)
		confidence := d.vpnBoostConfidence(overshootConfidence(internal, d.cfg.InternalReconThreshold), e.InInterface)
		d.fs.AddWithDetail(flags.TypeInternalRecon, e.SrcIP,
			fmt.Sprintf("%d distinct internal destinations in %s", internal, d.cfg.InternalReconWindow)+vpnDetailSuffix,
			confidence,
			flags.Evidence{Hosts: sortedHostsCapped(hosts)}, "", now)
	}
}

func (w *destWindow) lastActivityTime() time.Time { return w.lastActivity }
