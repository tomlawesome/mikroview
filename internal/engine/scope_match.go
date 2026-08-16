// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"net"

	"github.com/tomlawesome/mikroview/internal/store"
)

// This file is #405's answer to the gap declarative.go's own doc comment
// left open: DeclarativeDefinition.Evaluate previously consulted only its
// compiled Condition set, never Definition.Scope -- issue #402 deliberately
// left Scope application to this issue (docs/decisions/evaluation-engine.md
// section 2, "Scope (hosts AND netclass, the existing #44 model)" is part
// of the envelope every definition carries, but nothing evaluated it yet).
// scopeMatches below is that missing enforcement, ported from
// internal/detect.scopeMatchesHost/scopeMatchesPort/scopeMatchesRule
// (internal/detect/settings.go) unchanged in meaning.
//
// One deliberate simplification from internal/detect's per-detector axis
// wiring: detect.go consulted each Scope axis only where that specific
// detector's own signature made it meaningful (e.g. critical_port's Ports
// axis narrows the *effective* critical-port subset at query time, while
// port_scan's Hosts axis gates whether a source is tracked at all -- see
// detect.Scope's own doc comment for the full per-detector table). Here,
// every axis is checked uniformly for every declarative definition: Hosts+
// Classification against the source address, Ports against the destination
// port, Rules against the rule label, all AND'd. This is behaviourally
// identical to the old per-axis wiring for every detector this issue ports
// to declarative, because a shipped definition's Scope only ever has
// entries on the axes that detector's own settings.go comment already
// declared meaningful -- an axis a definition doesn't use is simply always
// empty (matches everything, see matchesList's own doc comment), so
// checking it costs nothing and changes nothing. The one behaviour this
// does NOT reproduce is detect's few axis/detector pairs that gated
// "counts toward the threshold" rather than "is this event even tracked"
// (critical_port/repeated_drops' Ports axis, see settings.go's Scope doc
// comment) -- gating the whole event uniformly here means a live scope
// narrowing takes effect on already-buffered ring state differently than
// detect's query-time-only filtering did. None of this package's pinned
// characterization tests (internal/detect/characterization_test.go's
// TestCharacterizationScope_* series) exercise a live mid-window scope
// change, so this simplification does not change any pinned outcome; see
// this issue's own report for the full reasoning.
func scopeMatches(sc Scope, e store.Event) bool {
	return matchesHostList(sc.Hosts, sc.HostsMode, e.SrcIP) &&
		scopeClassificationMatches(sc.Classification, e.SrcIP) &&
		matchesList(sc.Ports, sc.PortsMode, e.DstPort) &&
		matchesList(sc.Rules, sc.RulesMode, e.RuleLabel)
}

// scopeMatchesSource is internal/detect.scopeMatchesHost unchanged: the
// hosts-and-classification half of a Scope, applied to a source address
// alone.
//
// It exists because scopeMatches above deliberately checks every axis
// against every event, and a handful of ported programmatic definitions
// cannot use that: internal/detect applied their Ports axis at *query*
// time -- narrowing which distinct destination ports count toward a
// breadth threshold -- rather than as a gate on whether the event was
// tracked at all (see detect.Scope's own per-detector field-usage
// table). low_slow_scan is the case in this port: gating the whole event
// on the Ports axis would also change its host-breadth count and its
// drop ratio, which are computed from the same tracked events, so the
// query-time application is reproduced literally rather than folded into
// the uniform gate.
func scopeMatchesSource(sc Scope, ip string) bool {
	return matchesHostList(sc.Hosts, sc.HostsMode, ip) && scopeClassificationMatches(sc.Classification, ip)
}

// matchesHostList is internal/detect.matchesHostList unchanged: reports
// whether ip is admitted by list under mode, an entry being either a bare
// IP or a CIDR (see hostEntryMatchesScope).
func matchesHostList(list []string, mode ListMode, ip string) bool {
	if len(list) == 0 {
		return true
	}
	hit := false
	for _, entry := range list {
		if hostEntryMatchesScope(entry, ip) {
			hit = true
			break
		}
	}
	if mode == ListModeDeny {
		return !hit
	}
	return hit
}

// hostEntryMatchesScope is internal/detect.hostEntryMatches unchanged --
// named distinctly from this package's own hostEntryValid (definition.go,
// a structural param-validation check, not a match test) to keep the two
// purposes from being confused at a glance.
func hostEntryMatchesScope(entry, ip string) bool {
	if _, ipNet, err := net.ParseCIDR(entry); err == nil {
		parsed := net.ParseIP(ip)
		return parsed != nil && ipNet.Contains(parsed)
	}
	return entry == ip
}

// matchesList is internal/detect.matchesList unchanged: an empty list
// always matches, an allow-list (or unset mode) admits only members, a
// deny-list admits everything except members. Generic over comparable so
// both the int (port) and string (rule label) scope axes share one
// implementation.
func matchesList[T comparable](list []T, mode ListMode, v T) bool {
	if len(list) == 0 {
		return true
	}
	hit := false
	for _, entry := range list {
		if entry == v {
			hit = true
			break
		}
	}
	if mode == ListModeDeny {
		return !hit
	}
	return hit
}

// scopeClassificationMatches is internal/detect.classificationMatches
// unchanged -- named distinctly from this package's own
// classificationMatches (conditions.go, which takes the condition
// language's "internal"/"external"/"any" string operand, not a
// store.Scope value) to keep the two purposes from being confused at a
// glance.
func scopeClassificationMatches(scope store.Scope, ip string) bool {
	if scope == store.ScopeAny {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	public := isPublicIP(parsed)
	if scope == store.ScopeInternal {
		return !public
	}
	return public // store.ScopeExternal
}

// thresholdOvershootCeiling/overshootConfidence are
// internal/detect.thresholdOvershootCeiling/overshootConfidence
// (internal/detect/confidence.go) unchanged: how many multiples of a
// definition's own threshold correspond to 100% confidence for a plain
// threshold-crossing declarative definition -- confidence here measures
// "how far over the line" an observed count is, not history or
// statistical deviation (see baseline.go's Snapshot/Fire for that, used
// only by a programmatic definition built on Baseline). Every declarative
// definition's firing shape (per docs/decisions/evaluation-engine.md
// section 2: "conditions + window + threshold + emission") is exactly the
// shape this formula was written for, so DeclarativeDefinition.Evaluate
// (declarative.go) applies it uniformly rather than each shipped
// declarative definition re-deriving its own copy.
const thresholdOvershootCeiling = 3.0

func overshootConfidence(count, threshold int) int {
	if threshold <= 0 {
		return 100
	}
	ratio := float64(count-threshold) / float64(threshold) / (thresholdOvershootCeiling - 1)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return int(ratio*100 + 0.5)
}
