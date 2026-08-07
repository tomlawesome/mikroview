package detect

import (
	"math"
	"path"
)

// isVPNInterface reports whether iface matches any entry in patterns --
// see Config.VPNInterfaces for what this feeds into and why it exists
// at all. Each entry is matched with path.Match's glob syntax (*, ?,
// [...]), covering both an exact interface name ("wireguard1") and a
// prefix-style pattern ("wireguard*") through one mechanism rather than
// two separate matching modes -- a trailing "*" already expresses
// "prefix," and a plain name with no glob metacharacters already
// expresses "exact," through the same function. A malformed pattern
// (path.ErrBadPattern) is treated as "doesn't match" rather than
// erroring or panicking -- the same "a bad config value degrades to
// no-match, never a crash" stance settings.go's hostEntryMatches
// already takes -- and, like that function, patterns are matched fresh
// on every call rather than compiled/cached: VPNInterfaces is expected
// to be a handful of entries, not a hot path worth optimizing.
func isVPNInterface(patterns []string, iface string) bool {
	if iface == "" {
		return false
	}
	for _, p := range patterns {
		if ok, err := path.Match(p, iface); err == nil && ok {
			return true
		}
	}
	return false
}

// vpnBoostConfidence scales confidence up when iface matches
// d.cfg.VPNInterfaces, clamped to 100; returns confidence unchanged
// otherwise.
//
// Design decision (issue #105 deliberately leaves the exact mechanism
// open): the boost is applied as a flat multiplier on the
// *already-computed* confidence score -- whichever detector-specific
// formula produced it (emaConfidence's z-score-and-history judgment in
// host_baseline.go, or overshootConfidence's threshold-overshoot
// measure in dest_spread.go) -- rather than by lowering the firing
// threshold or z-score bar itself. Two reasons:
//
//  1. Consistency across the two call sites this issue scopes.
//     host_baseline.go and dest_spread.go use two structurally
//     different confidence formulas (one z-score/history-based, one a
//     plain overshoot ratio) with no shared "effective threshold" or
//     "effective z" concept that would mean the same thing lowered in
//     both places. A post-hoc multiplier on the 0-100 output both
//     formulas already produce is the one thing they share, so it's
//     the one lever that can be applied identically at both sites.
//  2. It never changes *whether* a detector fires, only how urgently an
//     already-firing flag reads. Lowering the threshold/z bar instead
//     would mean a VPN peer could trip a flag that a LAN host with the
//     exact same behavior would not -- a materially different (and
//     harder to reason about or tune) semantic than "the same anomaly,
//     scored as more significant." Every detector's own firing
//     condition (threshold crossing, multiplier-vs-baseline, z >=
//     emaMinZ) is left completely untouched by this function.
//
// The result is never allowed to fall below the input: a
// VPNConfidenceMultiplier of <= 0 is treated as 1 (no boost), and one
// in (0, 1) -- which would otherwise *lower* confidence -- is clamped
// back up to the unboosted value instead. A VPN-tagged event's
// confidence is monotonically >= the same event's confidence would be
// without VPNInterfaces configured at all; a misconfigured multiplier
// should never make a VPN-sourced anomaly read as less alarming than an
// identical LAN-sourced one.
func (d *Detector) vpnBoostConfidence(confidence int, iface string) int {
	if !isVPNInterface(d.cfg.VPNInterfaces, iface) {
		return confidence
	}
	mult := d.cfg.VPNConfidenceMultiplier
	if mult <= 0 {
		mult = 1
	}
	boosted := int(math.Round(float64(confidence) * mult))
	if boosted > 100 {
		boosted = 100
	}
	if boosted < confidence {
		return confidence
	}
	return boosted
}
