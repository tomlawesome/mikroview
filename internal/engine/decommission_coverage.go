// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"net/netip"
	"strings"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// This file answers, for a retiring segment, the only question that
// entitles it to report itself quiet: **could a straggler have been seen
// at all?**
//
// It matters more here than anywhere else coverage is asked. Everywhere
// else an uncovered watch means a finding might be missed. Here an
// uncovered watch would eventually announce a *successful decommission* --
// the range went quiet, the ghost left the map, the job is done -- purely
// because nothing was ever logging it. That is the exact shape of
// "absence of detection presented as absence of threat", so a
// decommission watch with no coverage is painted broken (see
// decommission.StateBroken) and never retires on its own.
//
// The rule mirrors coverageForEntry's three-way answer, and inherits its
// #367 caveat about the evidence base being complete -- which is applied
// by the caller (internal/api's definitionsCoverage), not restated here.

// DecommissionCoverage reports whether any pushed filter rule logs
// traffic that could touch cidr.
//
// Entries are not scoped to a device and neither is this: one router
// logging the right traffic is enough, exactly as Definition.Coverage
// decides, and for the same reasons its doc comment sets out (including
// the cost of that choice, which is unchanged here).
func DecommissionCoverage(cidr string, rulesByDevice map[string][]ingest.FilterRule) CoverageState {
	if len(rulesByDevice) == 0 {
		return CoverageUnknown
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return CoverageUnknown
	}
	prefix = prefix.Masked()

	sawRule := false
	sawUnreadable := false
	for _, rules := range rulesByDevice {
		for _, rule := range rules {
			if rule.Disabled {
				continue
			}
			sawRule = true
			if !rule.Log {
				// A rule that does not log cannot feed anything,
				// whatever it matches. Not evidence either way.
				continue
			}
			switch coversPrefix(rule, prefix) {
			case ingest.Covers:
				return CoverageOK
			case ingest.Unknown:
				sawUnreadable = true
			}
		}
	}
	switch {
	case sawUnreadable:
		// Something in a rule could not be read, and it might have been
		// the rule that covers. A definite negative requires every
		// relevant rule to have been read and understood -- coverage.go's
		// own stated rule, applied here.
		return CoverageUnknown
	case sawRule:
		return CoverageNoLogging
	default:
		return CoverageUnknown
	}
}

// coversPrefix reports whether one rule's source or destination address
// spec could carry traffic to or from prefix.
//
// Either end counts, because the watch itself watches either end -- "any
// traffic to or from it". A rule logging only traffic *into* the dead
// range still proves a straggler would be seen.
func coversPrefix(rule ingest.FilterRule, prefix netip.Prefix) ingest.Coverage {
	src := specCoversPrefix(rule.SrcAddress, prefix)
	if src == ingest.Covers {
		return ingest.Covers
	}
	dst := specCoversPrefix(rule.DstAddress, prefix)
	if dst == ingest.Covers {
		return ingest.Covers
	}
	if src == ingest.Unknown || dst == ingest.Unknown {
		return ingest.Unknown
	}
	return ingest.Excludes
}

// specCoversPrefix is ingest.CoversAddress's question asked of a whole
// range rather than one address.
//
// It is not CoversAddress with a representative address substituted, and
// the difference is the point: a rule scoped to 192.0.2.200/32 covers
// part of 192.0.2.0/24, so asking about the network address alone would
// answer "excludes" for a rule that genuinely would log a straggler --
// and the consequence of that mistake is a watch painted broken, or
// worse, a decommission that never retires because coverage looked
// absent. Overlap, not containment, is the right test in both
// directions.
//
// A negated element ("!10.0.0.0/8") is answered Unknown rather than
// inverted. CoversAddress can flip a negation safely because it is
// asking about one address; over a range, "everything outside 10/8"
// overlaps almost every prefix and excludes a few, and the arithmetic to
// say which is a subtlety this does not need. Unknown is the honest
// answer and it errs towards refusing to claim quiet.
func specCoversPrefix(spec string, prefix netip.Prefix) ingest.Coverage {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		// No address condition at all: the rule matches every address,
		// so it certainly covers this range.
		return ingest.Covers
	}
	result := ingest.Excludes
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "!") {
			return ingest.Unknown
		}
		switch elementCoversPrefix(part, prefix) {
		case ingest.Unknown:
			// One unreadable element makes the whole answer unsafe: it
			// might have been the element that covers -- CoversAddress's
			// own reasoning, unchanged.
			return ingest.Unknown
		case ingest.Covers:
			result = ingest.Covers
		}
	}
	return result
}

// elementCoversPrefix answers for one element of an address spec: a bare
// address, a CIDR, or a dash range.
func elementCoversPrefix(spec string, prefix netip.Prefix) ingest.Coverage {
	if p, err := netip.ParsePrefix(spec); err == nil {
		p = p.Masked()
		if p.Overlaps(prefix) {
			return ingest.Covers
		}
		return ingest.Excludes
	}
	if addr, err := netip.ParseAddr(spec); err == nil {
		if prefix.Contains(addr.Unmap()) {
			return ingest.Covers
		}
		return ingest.Excludes
	}
	if lo, hi, ok := parseAddressRange(spec); ok {
		// A dash range overlaps the prefix when the prefix's own first
		// and last addresses are not both on the same side of it.
		first, last := prefixBounds(prefix)
		if lo.Compare(last) <= 0 && hi.Compare(first) >= 0 {
			return ingest.Covers
		}
		return ingest.Excludes
	}
	return ingest.Unknown
}

// parseAddressRange reads RouterOS's "a.b.c.d-e.f.g.h" form.
func parseAddressRange(spec string) (lo, hi netip.Addr, ok bool) {
	left, right, found := strings.Cut(spec, "-")
	if !found {
		return netip.Addr{}, netip.Addr{}, false
	}
	lo, err := netip.ParseAddr(strings.TrimSpace(left))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	hi, err = netip.ParseAddr(strings.TrimSpace(right))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	return lo.Unmap(), hi.Unmap(), true
}

// prefixBounds returns a prefix's first and last address.
func prefixBounds(p netip.Prefix) (first, last netip.Addr) {
	first = p.Masked().Addr()
	bits := first.As16()
	// Set every host bit, working from the low end of the address.
	hostBits := first.BitLen() - p.Bits()
	offset := 16 - first.BitLen()/8
	for i := len(bits) - 1; i >= offset && hostBits > 0; i-- {
		take := min(hostBits, 8)
		bits[i] |= byte(0xff >> (8 - take))
		hostBits -= take
	}
	last = netip.AddrFrom16(bits)
	if first.Is4() {
		last = last.Unmap()
	}
	return first, last
}
