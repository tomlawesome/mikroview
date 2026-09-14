// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"errors"
	"fmt"
	"net/netip"
)

// ErrInvalidCIDR is returned by Validate when the input parses as
// neither a CIDR nor a bare IP address.
var ErrInvalidCIDR = errors.New("droplist: not a valid CIDR or IP address")

// ErrNotIPv4 is returned by Validate for anything that is not a plain
// IPv4 address -- including an IPv4-mapped IPv6 address ("::ffff:a.b.c.d"),
// which parses as IPv6 and must be rejected the same way a bare IPv6
// prefix is (#461's settled scope: IPv6 address-lists are out for v1,
// see this package's own doc comment).
var ErrNotIPv4 = errors.New("droplist: only IPv4 addresses are accepted")

// ErrTooBroad is returned by Validate for a prefix wider than /24 -- an
// entry this broad risks blocking far more than the operator intended,
// and a mistaken /8 would be effectively unrecoverable without noticing
// the outage first.
var ErrTooBroad = errors.New("droplist: entries may not be broader than /24")

// ErrNotPublic is returned by Validate for a candidate that overlaps
// private, loopback, link-local, multicast, unspecified or other
// reserved/documentation address space -- there is no legitimate reason
// to block traffic to or from any of it, and RouterOS enforcement built
// on top of this store (a later stage) would otherwise risk cutting off
// the router's own reachable ranges.
var ErrNotPublic = errors.New("droplist: not a public address range")

// ErrRouterOwn is returned by Validate when the candidate overlaps a
// range the pushed router state (internal/routerstate, via OwnRanges)
// shows as one of the router's own configured addresses -- blocking a
// router's own range is never intentional, and stage 2's RouterOS push
// would otherwise risk the router blocking itself.
var ErrRouterOwn = errors.New("droplist: the pushed router state shows this range as the router's own")

// nonPublicRanges is the IPv4 address space Validate refuses regardless
// of what internal/routerstate has ever pushed -- reserved,
// documentation, benchmarking and other special-use ranges that Go's
// netip.Addr.IsPrivate/IsLoopback/etc (checked separately, see
// isPublic) do not flag. Values and citations from IANA's IPv4
// Special-Purpose Address Registry:
//   - 0.0.0.0/8            -- "this network" (RFC 791)
//   - 100.64.0.0/10        -- shared address space / CGNAT (RFC 6598)
//   - 192.0.0.0/24         -- IETF protocol assignments (RFC 6890)
//   - 192.0.2.0/24         -- TEST-NET-1 documentation (RFC 5737)
//   - 198.18.0.0/15        -- benchmarking (RFC 2544)
//   - 198.51.100.0/24      -- TEST-NET-2 documentation (RFC 5737)
//   - 203.0.113.0/24       -- TEST-NET-3 documentation (RFC 5737)
//   - 240.0.0.0/4          -- reserved for future use (RFC 1112), which
//     also covers 255.255.255.255/32, the limited broadcast address.
//
// 169.254.0.0/16 (link-local) is deliberately absent: Addr.
// IsLinkLocalUnicast already flags it, so listing it again here would
// only be redundant.
var nonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// isPublic reports whether every address p covers is legitimate public
// Internet space. Checked against p.Addr() -- p is already Masked() by
// the time this is called, so that address is p's network address --
// rather than against each individual address in the range, which is
// safe only because Validate's own /24 floor means every one of these
// reference ranges is at least as broad as the narrowest thing p could
// be: a flagged range that is a strict superset of the narrowest
// possible p can only ever fully contain p or miss it entirely, never
// straddle it, so testing the one address decides it for the whole
// prefix. The single exception, 0.0.0.0/32 (IsUnspecified), is the
// network address of any 0.0.0.0/n itself, so it is still caught by
// checking the same address.
func isPublic(p netip.Prefix) bool {
	addr := p.Addr()
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, r := range nonPublicRanges {
		if p.Overlaps(r) {
			return false
		}
	}
	return true
}

// parseCIDR accepts either a CIDR ("203.0.113.0/24") or a bare address
// ("203.0.113.7", treated as /32) -- ParsePrefix alone rejects the
// latter, and a bare address is the common case for "block this one
// host".
func parseCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err == nil {
		return p, nil
	}
	addr, addrErr := netip.ParseAddr(s)
	if addrErr != nil {
		return netip.Prefix{}, fmt.Errorf("%w: %q", ErrInvalidCIDR, s)
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// Validate is the server-side gate on what may ever become a droplist
// Entry (issue #1223, design ratified on #461) -- pure and independent
// of any Store, so every rule below has its own direct test. own is the
// router's own pushed address ranges (see OwnRanges); pass nil when
// there is none to check against.
//
// In order: the input must parse as a CIDR or bare address (a bare
// address is /32); it must be plain IPv4, not IPv6 and not an
// IPv4-mapped IPv6 address; its prefix must be no broader than /24; it
// must not overlap private, loopback, link-local, multicast,
// unspecified or other reserved/documentation space; and it must not
// overlap any range own says the router already considers its own. The
// canonical Masked() prefix is returned on success, so two spellings of
// the same range (e.g. "203.0.113.7/24" and "203.0.113.0/24") always
// store and compare identically.
func Validate(cidr string, own []netip.Prefix) (netip.Prefix, error) {
	p, err := parseCIDR(cidr)
	if err != nil {
		return netip.Prefix{}, err
	}

	addr := p.Addr()
	if !addr.Is4() || addr.Is4In6() {
		return netip.Prefix{}, fmt.Errorf("%w: %q", ErrNotIPv4, cidr)
	}

	if p.Bits() < 24 {
		return netip.Prefix{}, fmt.Errorf("%w: %q", ErrTooBroad, cidr)
	}

	p = p.Masked()

	if !isPublic(p) {
		return netip.Prefix{}, fmt.Errorf("%w: %q", ErrNotPublic, cidr)
	}

	for _, o := range own {
		if p.Overlaps(o) {
			return netip.Prefix{}, fmt.Errorf("%w: %q overlaps %s", ErrRouterOwn, cidr, o)
		}
	}

	return p, nil
}
