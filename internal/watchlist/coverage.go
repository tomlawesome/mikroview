// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"github.com/tomlawesome/mikroview/internal/ingest"
)

// Answering "can anything ever feed this entry?" (#274 item 1).
//
// The problem this solves: an entry showing no matches is ambiguous
// between "nothing happened" and "nothing here is even watching". The
// second is a configuration mistake the operator cannot see, and it is
// the exact silent failure the watchlist exists to avoid -- so a
// watchlist that cannot tell them apart is quietly not doing its job.
//
// The rule this is built on: **only ever claim a definite answer.**
// #274 rejected an earlier sketch for guessing from whether a rule
// happened to carry a log-prefix, because a false "this can never fire"
// hides a working entry and a false "this looks fine" is worse than
// saying nothing at all. So everything here defaults to CoverageUnknown,
// and a negative answer requires every relevant rule to have been read
// and understood.

// CoverageState is what can be said about whether any pushed firewall
// rule could produce an event matching an entry.
type CoverageState string

const (
	// CoverageUnknown: no answer. No router has pushed its filter table,
	// or the rules that were pushed use a condition this cannot read
	// (an address-list name, a syntax RouterOS grew later). The UI says
	// nothing at all in this state -- it is the default, and by far the
	// most common one for a deployment that has not set up the optional
	// push at all.
	CoverageUnknown CoverageState = "unknown"
	// CoverageOK: at least one logging rule could produce a matching
	// event.
	CoverageOK CoverageState = "covered"
	// CoverageNoLogging: routers pushed their tables, and not one rule
	// in them has logging switched on. Nothing anywhere can feed the
	// watchlist -- or the live view. The most useful thing this can
	// report, and the cheapest to be sure of.
	CoverageNoLogging CoverageState = "no-logging"
	// CoverageOutOfScope: rules do log, but none of them can match this
	// entry's scope -- every logging rule was read, and each provably
	// excludes it on port or address.
	CoverageOutOfScope CoverageState = "out-of-scope"
)

// Coverage decides what can be said about entry against the filter
// tables devices have pushed. rulesByDevice is every device's table;
// an empty map means nothing has been pushed and the answer is
// CoverageUnknown.
//
// Entries are not scoped to a device, so this asks whether *any* device
// could feed the entry. One router logging the right traffic is enough,
// even if five others do not.
func Coverage(entry Entry, rulesByDevice map[string][]ingest.FilterRule) CoverageState {
	if len(rulesByDevice) == 0 {
		return CoverageUnknown
	}

	sawRule := false
	sawLoggingRule := false
	sawUnreadable := false

	for _, rules := range rulesByDevice {
		for _, rule := range rules {
			sawRule = true
			// A rule that does not log feeds nothing, whatever else it
			// matches. This is the check the whole feature turns on, and
			// the field it needs was only added in #274's first slice --
			// before that there was no honest way to ask.
			if !rule.Log {
				continue
			}
			sawLoggingRule = true

			switch ruleCovers(entry, rule) {
			case ingest.Covers:
				return CoverageOK
			case ingest.Unknown:
				sawUnreadable = true
			}
		}
	}

	switch {
	case !sawRule:
		// Tables pushed, but empty. A router with no filter rules at all
		// is a real state and says nothing about intent.
		return CoverageUnknown
	case !sawLoggingRule:
		return CoverageNoLogging
	case sawUnreadable:
		// Something could not be read, and it might have been the rule
		// that covers. Saying nothing beats a confident wrong answer.
		return CoverageUnknown
	default:
		return CoverageOutOfScope
	}
}

// ruleCovers reports whether one logging rule could produce an event
// this entry would match.
//
// Deliberately asks only what the pushed schema can answer. A rule
// carries no MAC condition, so an entry scoped by MAC cannot be
// narrowed by address at all -- any logging rule might carry that
// device's traffic, and the honest answer is Covers rather than a guess
// about which subnet the device is on.
func ruleCovers(entry Entry, rule ingest.FilterRule) ingest.Coverage {
	// Inverted entries watch every port their device touches, so only
	// the source scoping applies.
	if entry.Invert {
		return coversSource(entry, rule)
	}

	// A non-inverted entry always has ports (Upsert refuses one
	// without). It is covered if the rule admits any of them.
	portAnswer := ingest.Excludes
	for _, port := range entry.Ports {
		switch ingest.CoversPort(string(rule.DstPort), port) {
		case ingest.Covers:
			portAnswer = ingest.Covers
		case ingest.Unknown:
			return ingest.Unknown
		}
	}
	if portAnswer != ingest.Covers {
		return ingest.Excludes
	}

	if entry.DestIP != "" {
		switch ingest.CoversAddress(rule.DstAddress, entry.DestIP) {
		case ingest.Excludes:
			return ingest.Excludes
		case ingest.Unknown:
			return ingest.Unknown
		}
	}

	return coversSource(entry, rule)
}

func coversSource(entry Entry, rule ingest.FilterRule) ingest.Coverage {
	// A MAC-scoped entry cannot be narrowed: rules carry no MAC
	// condition. Any logging rule could be the one carrying that
	// device's traffic.
	if entry.Source.IP == "" {
		return ingest.Covers
	}
	// A rule scoping by src-address-list names a list this has no
	// contents for, so whether the entry's address is in it is
	// unanswerable -- and a rule that scopes by list is exactly the kind
	// that might well cover the entry.
	if rule.SrcAddressList != "" {
		return ingest.Unknown
	}
	return ingest.CoversAddress(rule.SrcAddress, entry.Source.IP)
}
