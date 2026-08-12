// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
	"net/netip"
	"strconv"
	"strings"
)

// Interpreting a rule's own match conditions -- what addresses and ports
// a RouterOS filter rule covers -- so a caller can ask whether a rule
// could ever produce a particular event (#274 item 1).
//
// Every function here answers a three-way question, not a boolean:
// yes, no, or "cannot tell". That shape is the whole point. #274 rejects
// a coverage check that guesses, because a false "this can never fire"
// hides a working entry and a false "this is fine" is worse than saying
// nothing at all. So anything these cannot parse -- a syntax RouterOS
// grew since, an address list referenced by name, an IPv6 form not
// handled here -- reports Unknown, and the caller stays quiet.
//
// The shapes are the ones a real RouterOS 7.23.3 was observed to emit;
// see FilterRule's doc comment and TestDecodeRealFilterRulePush.

// Coverage is a three-way answer about whether a rule's match condition
// admits some value.
type Coverage int

const (
	// Unknown: the condition could not be interpreted, so nothing may be
	// concluded from it in either direction.
	Unknown Coverage = iota
	// Covers: the condition admits the value (including the common case
	// of an unset condition, which matches anything).
	Covers
	// Excludes: the condition provably does not admit the value.
	Excludes
)

// CoversAddress reports whether a rule's dst-address/src-address
// condition admits ip.
//
// An empty spec is Covers, not Excludes: RouterOS omits the property
// when a rule does not scope by address, and a rule with no address
// condition matches every address. Getting that backwards would report
// "nothing watches this" for the most common rule there is.
//
// Handles the four shapes observed off a real router -- bare address,
// CIDR, range, and a leading "!" negating any of them. A comma-separated
// list is also accepted, since RouterOS permits one and it costs nothing
// to split; every element is evaluated and the answer is Covers if any
// element covers.
func CoversAddress(spec, ip string) Coverage {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Covers
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return Unknown
	}
	addr = addr.Unmap()

	result := Excludes
	for _, part := range strings.Split(spec, ",") {
		switch coversOneAddress(part, addr) {
		case Unknown:
			// One unparseable element makes the whole answer unsafe:
			// it might have been the element that covers.
			return Unknown
		case Covers:
			result = Covers
		}
	}
	return result
}

func coversOneAddress(spec string, addr netip.Addr) Coverage {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Unknown
	}

	negated := false
	if rest, found := strings.CutPrefix(spec, "!"); found {
		negated = true
		spec = strings.TrimSpace(rest)
	}

	inner := plainAddressCovers(spec, addr)
	if inner == Unknown || !negated {
		return inner
	}
	// "!10.0.0.0/8" means everything outside it, so the answer flips.
	// Worth the explicit handling rather than treating "!" as noise: a
	// negated condition is the exact case where a naive containment test
	// answers backwards, and backwards is the failure #274 cares about.
	if inner == Covers {
		return Excludes
	}
	return Covers
}

func plainAddressCovers(spec string, addr netip.Addr) Coverage {
	// A range: "10.0.0.1-10.0.0.5". Checked before CIDR because neither
	// form contains the other's separator, so order is only about
	// reading clearly.
	if lo, hi, found := strings.Cut(spec, "-"); found {
		loAddr, errLo := netip.ParseAddr(strings.TrimSpace(lo))
		hiAddr, errHi := netip.ParseAddr(strings.TrimSpace(hi))
		if errLo != nil || errHi != nil {
			return Unknown
		}
		loAddr, hiAddr = loAddr.Unmap(), hiAddr.Unmap()
		if loAddr.Is4() != addr.Is4() {
			// Different families never overlap, which is a real answer
			// rather than an unparseable one.
			return Excludes
		}
		if addr.Compare(loAddr) >= 0 && addr.Compare(hiAddr) <= 0 {
			return Covers
		}
		return Excludes
	}

	if strings.Contains(spec, "/") {
		prefix, err := netip.ParsePrefix(spec)
		if err != nil {
			return Unknown
		}
		if prefix.Addr().Unmap().Is4() != addr.Is4() {
			return Excludes
		}
		if prefix.Contains(addr) {
			return Covers
		}
		return Excludes
	}

	one, err := netip.ParseAddr(spec)
	if err != nil {
		return Unknown
	}
	if one.Unmap() == addr {
		return Covers
	}
	return Excludes
}

// CoversPort reports whether a rule's dst-port condition admits port.
//
// Empty is Covers, for the same reason as CoversAddress: RouterOS omits
// the property on a rule that does not scope by port, and such a rule
// matches every port.
//
// Accepts the shapes RouterOSPortSpec normalises to a string: a single
// port ("3389"), a comma-separated list ("22,23"), and a range
// ("1000-2000"), in any combination.
func CoversPort(spec string, port int) Coverage {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Covers
	}
	if port <= 0 {
		return Unknown
	}

	result := Excludes
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return Unknown
		}
		if lo, hi, found := strings.Cut(part, "-"); found {
			loN, errLo := strconv.Atoi(strings.TrimSpace(lo))
			hiN, errHi := strconv.Atoi(strings.TrimSpace(hi))
			if errLo != nil || errHi != nil {
				return Unknown
			}
			if port >= loN && port <= hiN {
				result = Covers
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return Unknown
		}
		if n == port {
			result = Covers
		}
	}
	return result
}
