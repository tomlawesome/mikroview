// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"io"
	"log/slog"
	"net/netip"
	"testing"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// build makes a classifier with its sets populated directly, bypassing
// the network -- the fetch path is tested separately.
func build(t *testing.T, bySrc map[Source][]classifiedPrefix, order ...Source) *Classifier {
	t.Helper()
	c := New(nil, testLog())
	for _, s := range order {
		c.sources[s] = registryBySource[s]
	}
	c.order = order
	c.sets, c.details, c.detailStrings = buildSets(bySrc)
	return c
}

func cp(t *testing.T, cidr, detail string) classifiedPrefix {
	t.Helper()
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		t.Fatalf("bad test prefix %q: %v", cidr, err)
	}
	return classifiedPrefix{prefix: p.Masked(), detail: detail}
}

// TestClassOrderDecidesOverlaps is the #1313 "done when" test: netipx has
// no trie, so where two sources both claim an address, ClassOrder (see
// sources.go) decides, not prefix width. A Tor /32 sitting inside a
// cloud /16 must read as Tor -- Tor is checked first regardless of its
// narrower prefix -- and a cloud /24 with no competing claim must still
// read as cloud, so the order is deciding overlaps, not breaking
// matching altogether.
//
// This test is only worth having if it actually pins the order: reverse
// ClassOrder and this must fail. Verified by hand while writing it
// (temporarily swapping SourceTor and SourceGCP in the slice made the
// nested-address assertion below fail with CategoryDatacenter instead of
// CategoryTor), then restored.
func TestClassOrderDecidesOverlaps(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceTor: {cp(t, "34.80.0.1/32", "")},
		SourceGCP: {cp(t, "34.80.0.0/16", "asia-east1")},
	}, SourceTor, SourceGCP)

	// The Tor /32 is nested inside the GCP /16. ClassOrder checks Tor
	// first, so the narrower-by-width cloud entry never gets a say.
	got := c.Lookup("34.80.0.1")
	if !got.Matched || got.Category != CategoryTor || got.Source != SourceTor {
		t.Fatalf("Lookup(nested address) = %+v, want CategoryTor (Tor is checked before cloud in ClassOrder, regardless of prefix width)", got)
	}

	// An address in the GCP range only -- no Tor claim on it -- still
	// classifies as cloud.
	got = c.Lookup("34.80.1.1")
	if !got.Matched || got.Category != CategoryDatacenter || got.Source != SourceGCP {
		t.Fatalf("Lookup(cloud-only address) = %+v, want CategoryDatacenter/SourceGCP", got)
	}
	if got.Detail != "asia-east1" {
		t.Errorf("Detail = %q, want asia-east1", got.Detail)
	}
}

// TestClassOrderVPNWinsOverPrivateRelayOnExactCollision documents a real
// consequence of the #1313 ruling's literal order (Tor, VPN, Private
// Relay, datacenter, cloud): VPN is checked before Private Relay, so
// where X4BNet's VPN feed has copied Apple's ranges verbatim (see
// SourceApplePrivateRelay's registry comment), the exact same prefix now
// classifies as VPN. Before #1313, buildTable's exact-prefix precedence
// (removed) put SourceApplePrivateRelay first in the registry
// specifically to make this resolve to CategoryPrivacyRelay instead --
// this test's name and result are the opposite of what it asserted
// before that change. Recorded here rather than left to be discovered
// as a silent regression.
func TestClassOrderVPNWinsOverPrivateRelayOnExactCollision(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceApplePrivateRelay: {cp(t, "172.224.226.0/27", "London")},
		SourceX4BVPN:            {cp(t, "172.224.226.0/27", "")},
	}, SourceApplePrivateRelay, SourceX4BVPN)

	got := c.Lookup("172.224.226.5")
	if !got.Matched || got.Category != CategoryVPN || got.Source != SourceX4BVPN {
		t.Fatalf("Lookup = %+v, want CategoryVPN/SourceX4BVPN -- ClassOrder checks VPN before Private Relay", got)
	}
}

// TestMissIsNotClean guards the "absence of evidence" contract: a lookup
// that finds nothing returns an unmatched Class, never something a caller
// could read as a positive "clean".
func TestMissIsNotClean(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceTor: {cp(t, "1.2.3.0/24", "")},
	}, SourceTor)

	got := c.Lookup("9.9.9.9")
	if got.Matched {
		t.Errorf("an unlisted IP matched: %+v", got)
	}
	if got.String() != "" {
		t.Errorf("an unmatched Class rendered as %q, want empty", got.String())
	}
}

// TestIPv4MappedIPv6IsUnmapped is the silent-miss bug: without Unmap, a
// v4-mapped v6 form of a listed address finds nothing.
func TestIPv4MappedIPv6IsUnmapped(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceTor: {cp(t, "1.2.3.0/24", "")},
	}, SourceTor)

	if got := c.Lookup("::ffff:1.2.3.4"); !got.Matched {
		t.Error("the IPv4-mapped IPv6 form of a listed address did not match")
	}
}

func TestParseRejectsTooBroadAndReserved(t *testing.T) {
	body := []byte(`
# comment
1.2.3.0/24
7.0.0.0/7          # shorter than /8 -- the AHBL rule
10.0.0.0/8         # RFC1918
100.64.0.0/10      # CGNAT, which Go's predicates miss
0.0.0.0/0          # the whole internet
8.8.8.0/24
`)
	got := parsePlainCIDRs(body)
	want := map[string]bool{"1.2.3.0/24": true, "8.8.8.0/24": true}
	if len(got) != len(want) {
		t.Fatalf("parsed %d prefixes, want %d: %v", len(got), len(want), got)
	}
	for _, p := range got {
		if !want[p.String()] {
			t.Errorf("unexpectedly accepted %s", p)
		}
	}
}

func TestSanitiseDetailRejectsUntrustedGarbage(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"eu-west-1", "eu-west-1"},
		{"us_central.1", "us_central.1"},
		{"<script>alert(1)</script>", ""}, // angle brackets rejected
		{"name\x00withnull", ""},          // control char rejected
		{string(make([]byte, 100)), ""},   // over-length rejected
		{"europe west", "europe west"},    // space allowed
	}
	for _, tc := range cases {
		if got := sanitiseDetail(tc.in); got != tc.want {
			t.Errorf("sanitiseDetail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestLabelsAreInterned proves detailStrings holds one string per
// distinct detail, not one per prefix -- the memory property the #114
// research asked for, now backing the sorted detail index instead of
// bart's per-prefix trie value.
func TestLabelsAreInterned(t *testing.T) {
	var many []classifiedPrefix
	for i := 0; i < 200; i++ {
		many = append(many, cp(t, netip.PrefixFrom(netip.AddrFrom4([4]byte{52, byte(i), 0, 0}), 16).String(), "eu-west-1"))
	}
	c := build(t, map[Source][]classifiedPrefix{SourceAWS: many}, SourceAWS)
	if len(c.detailStrings) != 1 {
		t.Errorf("detailStrings = %d for 200 prefixes sharing one label, want 1", len(c.detailStrings))
	}
}

func TestCoverageOf(t *testing.T) {
	ps := []classifiedPrefix{cp(t, "1.0.0.0/24", ""), cp(t, "2.0.0.0/16", "")}
	if got := coverageOf(ps); got != 256+65536 {
		t.Errorf("coverageOf = %d, want %d", got, 256+65536)
	}
}

// Four prefixes wider than /64 sum to 1<<64 and wrapped the total to
// near zero before #324 -- which decided whether the poisoning guard
// believed a feed, not just what the log said.
func TestCoverageOfSaturatesRatherThanWrapping(t *testing.T) {
	wide := []classifiedPrefix{
		cp(t, "2600:1900::/32", ""),
		cp(t, "2a00:1450::/32", ""),
		cp(t, "2606:4700::/32", ""),
		cp(t, "2620:11a::/32", ""),
		cp(t, "2803:f800::/32", ""),
	}
	if got := coverageOf(wide); got != maxCoverage {
		t.Errorf("coverageOf(5 wide v6 prefixes) = %d, want the saturated %d", got, maxCoverage)
	}
	// Doubling is what the guard does; it must stay in range.
	if maxCoverage*2 < maxCoverage {
		t.Error("maxCoverage*2 overflows -- the guard's own comparison would wrap")
	}
}

func TestDescribeCoverageIsReadable(t *testing.T) {
	cases := map[uint64]string{
		0:           "0 addresses",
		256:         "256 addresses",
		3136082:     "3,136,082 addresses",
		maxCoverage: "more address space than is worth counting (a prefix wider than /64)",
	}
	for v, want := range cases {
		if got := describeCoverage(v); got != want {
			t.Errorf("describeCoverage(%d) = %q, want %q", v, got, want)
		}
	}
}
