// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"net/netip"
	"strings"
)

// minIPv4Bits / minIPv6Bits are the shortest prefixes we will ever load,
// enforced at parse time and unconditionally.
//
// This is the AHBL rule. When AHBL's DNSBL shut down in 2015 its operator
// wildcarded every zone, so every consumer suddenly matched the whole
// internet -- abandonment producing a 0.0.0.0/0 outcome. A network-class
// feed is fetched from the public internet on a timer, so the same shape
// is reachable here by a compromised or careless publisher. Refusing any
// prefix shorter than a /8 (v4) or /32 (v6) makes "this feed now claims
// half the address space" un-loadable rather than merely improbable.
const (
	minIPv4Bits = 8
	minIPv6Bits = 32
)

// acceptablePrefix reports whether p is a prefix we are willing to load:
// masked, not shorter than the floor for its family, and not inside a
// reserved/special-use range. A rejected prefix is dropped silently at
// parse time -- one bad line in a third-party feed is not a reason to
// discard the whole feed.
func acceptablePrefix(p netip.Prefix) bool {
	if !p.IsValid() {
		return false
	}
	p = p.Masked()
	if p.Addr().Is4() {
		if p.Bits() < minIPv4Bits {
			return false
		}
	} else if p.Bits() < minIPv6Bits {
		return false
	}
	return !overlapsReserved(p)
}

// reservedV4 lists ranges no legitimate attribution feed should ever
// carry. Go's own net/netip predicates miss most of these -- verified:
// none of IsPrivate/IsLoopback/IsLinkLocalUnicast/IsUnspecified/
// IsMulticast returns true for 100.64.0.0/10 (CGNAT), 192.0.0.0/24,
// 198.18.0.0/15, or all but the exact 0.0.0.0 -- so the check has to be
// an explicit list rather than a call to the standard library.
var reservedV4 = mustPrefixes(
	"0.0.0.0/8",       // "this network"
	"10.0.0.0/8",      // RFC1918
	"100.64.0.0/10",   // CGNAT (RFC6598) -- the one Go misses that matters most
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local
	"172.16.0.0/12",   // RFC1918
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1
	"192.168.0.0/16",  // RFC1918
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved / 255.255.255.255
)

var reservedV6 = mustPrefixes(
	"::1/128",       // loopback
	"::/128",        // unspecified
	"fc00::/7",      // unique local
	"fe80::/10",     // link-local
	"ff00::/8",      // multicast
	"2001:db8::/32", // documentation
)

// overlapsReserved reports whether p is contained in, or contains, any
// reserved range for its family. A feed entry that straddles a reserved
// range is as suspect as one wholly inside it.
func overlapsReserved(p netip.Prefix) bool {
	list := reservedV4
	if p.Addr().Is6() {
		list = reservedV6
	}
	for _, r := range list {
		if r.Overlaps(p) {
			return true
		}
	}
	return false
}

// parsePlainCIDRs reads a feed that is one CIDR (or bare IP) per line,
// with '#' and ';' comments -- the shape of the X4BNet and Tor lists.
//
// netip.ParsePrefix is strict: it rejects the CVE-2021-29923 leading-zero
// ambiguity (0177.0.0.1) for free, and it rejects a trailing '\r', so a
// CRLF file would fail every line without the TrimSpace here.
func parsePlainCIDRs(body []byte) []netip.Prefix {
	var out []netip.Prefix
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		p, err := netip.ParsePrefix(line)
		if err != nil {
			// A bare address is fine too -- promote it to a host prefix.
			if a, aerr := netip.ParseAddr(line); aerr == nil {
				bits := 32
				if a.Is6() {
					bits = 128
				}
				p = netip.PrefixFrom(a, bits)
			} else {
				continue
			}
		}
		if acceptablePrefix(p) {
			out = append(out, p.Masked())
		}
	}
	return out
}

func mustPrefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			panic("netclass: bad built-in reserved prefix " + c)
		}
		out = append(out, p.Masked())
	}
	return out
}
