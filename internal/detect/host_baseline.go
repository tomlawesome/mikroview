package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// hostActivityMinSamples is a hard floor: a host with fewer prior
// observations than this can never raise a flag, no matter how extreme
// the first few readings look. A baseline built from 1-2 samples isn't a
// baseline -- there's nothing to have deviated from yet.
const hostActivityMinSamples = 5

// checkHostActivityBaseline is the per-host counterpart to
// GlobalSpikeDetector (network-wide) and observeRuleRate (per rule): an
// EMA baseline of this specific source's own event rate, so a host
// that's always busy is judged against its own normal rather than one
// fixed threshold applied to every host equally. Unlike those two, it
// also tracks a rolling variance so it can express *how* unusual a
// reading is (a z-score) rather than just "over the line" -- which is
// what makes a meaningful confidence score possible: a small deviation
// backed by a long history and a huge deviation backed by three samples
// should not read as equally trustworthy.
//
// currentRate is spikeCount from the caller's already-computed window
// (see observeScanAndSpike) -- events in ActivitySpikeWindow, reused
// rather than recomputed. srcCountry is the triggering event's already-
// GeoIP-resolved SrcCountry, threaded through so the raised flag can
// carry it without a second lookup. iface is the triggering event's
// InInterface, threaded through so the flag's confidence can be scored
// against Config.VPNInterfaces (issue #105, see vpn.go) -- an empty
// string (or one that matches no configured VPN pattern) leaves scoring
// completely unchanged.
func (d *Detector) checkHostActivityBaseline(w *sourceWindow, srcIP, srcCountry, iface string, currentRate int, now time.Time) {
	rate := float64(currentRate)

	if !w.primed {
		w.baseline = rate
		w.variance = 0
		w.primed = true
		w.sampleCount = 1
		return
	}

	prevBaseline := w.baseline
	z := emaZScore(rate, prevBaseline, w.variance)

	if w.sampleCount >= hostActivityMinSamples &&
		z >= emaMinZ &&
		rate >= float64(d.cfg.ActivitySpikeThreshold) &&
		prevBaseline > 0 &&
		rate >= prevBaseline*d.cfg.HostActivityMultiplier {

		confidence := d.vpnBoostConfidence(emaConfidence(z, w.sampleCount, d.cfg.HostActivityWarmupSamples), iface)

		detail := fmt.Sprintf(
			"%d events in %s vs a baseline of %.1f for this host (based on %d samples, %.1fσ above normal)",
			currentRate, d.cfg.ActivitySpikeWindow, prevBaseline, w.sampleCount, z,
		)
		if isVPNInterface(d.cfg.VPNInterfaces, iface) {
			detail += fmt.Sprintf(" -- arrived via VPN interface %q, scored more confidently as an already-authenticated remote peer", iface)
		}
		isNew := d.fs.AddWithDetail(flags.TypeActivitySpike, srcIP, detail, confidence, flags.Evidence{}, srcCountry, now)
		d.maybeCheckReputation(flags.TypeActivitySpike, srcIP, srcIP, isNew)
	}

	// EMA update, applied after the check above so the flag (if any)
	// compares against the baseline as it stood *before* this reading,
	// not after.
	w.baseline, w.variance = emaUpdate(rate, w.baseline, w.variance)
	if w.sampleCount < d.cfg.HostActivityWarmupSamples {
		w.sampleCount++
	}
}
