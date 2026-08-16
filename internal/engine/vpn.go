// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"math"
	"path"
)

// This file is internal/detect/vpn.go, moved unchanged in meaning by
// issue #405's port (#105 is the issue that introduced it). Two shipped
// programmatic definitions consult it -- activity_spike and dest_spread's
// two halves -- so it lives beside them rather than being duplicated into
// each.

// isVPNInterface reports whether iface matches any entry in patterns.
// Each entry is matched with path.Match's glob syntax (*, ?, [...]),
// covering both an exact interface name ("wireguard1") and a
// prefix-style pattern ("wireguard*") through one mechanism rather than
// two matching modes: a trailing "*" already expresses "prefix", and a
// plain name with no glob metacharacters already expresses "exact".
//
// A malformed pattern (path.ErrBadPattern) is treated as "doesn't match"
// rather than erroring or panicking -- the same "a bad config value
// degrades to no-match, never a crash" stance every list-matching path in
// this codebase takes. Patterns are matched fresh on every call rather
// than compiled and cached: a definition's vpnInterfaces param is
// expected to be a handful of entries, not a hot path worth optimizing.
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

// vpnBoostConfidence scales an already-computed confidence score up when
// iface matches one of patterns, clamped to 100; returns it unchanged
// otherwise.
//
// The design decision #105 deliberately left open, recorded in
// internal/detect and carried here: the boost is a flat multiplier on the
// score whichever formula produced it, not a lowered firing threshold or
// z-score bar. Two reasons. The definitions that use it score confidence
// in structurally different ways (a z-score-and-history judgement for
// activity_spike, a plain overshoot ratio for dest_spread's halves) with
// no shared "effective threshold" concept a lowered bar could mean the
// same thing in. And a post-hoc multiplier cannot change *whether* an
// anomaly is reported, only how confidently -- which keeps a VPN tag from
// silently manufacturing detections.
//
// A non-positive multiplier is treated as 1 (no boost), never as
// "suppress or invert": a misconfigured value must never make a
// VPN-sourced anomaly read as less alarming than an identical LAN one.
func vpnBoostConfidence(confidence int, patterns []string, multiplier float64, iface string) int {
	if !isVPNInterface(patterns, iface) {
		return confidence
	}
	if multiplier <= 0 {
		multiplier = 1
	}
	boosted := int(math.Round(float64(confidence) * multiplier))
	if boosted > 100 {
		boosted = 100
	}
	if boosted < confidence {
		return confidence
	}
	return boosted
}

// vpnDetailSuffix is the sentence fragment appended to a Detail string
// when the triggering event arrived via a VPN-tagged interface -- an
// operator reading a boosted confidence score is entitled to know why it
// was boosted. Empty when the interface is not VPN-tagged.
func vpnDetailSuffix(patterns []string, iface string) string {
	if !isVPNInterface(patterns, iface) {
		return ""
	}
	return " -- arrived via VPN interface \"" + iface + "\", scored more confidently as an already-authenticated remote peer"
}
