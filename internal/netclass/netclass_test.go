// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"io"
	"log/slog"
	"net/netip"
	"testing"
)

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// build makes a classifier with a table populated directly, bypassing the
// network -- the fetch path is tested separately.
func build(t *testing.T, bySrc map[Source][]classifiedPrefix, order ...Source) *Classifier {
	t.Helper()
	c := New(nil, testLog())
	for _, s := range order {
		c.sources[s] = registryBySource[s]
	}
	c.order = order
	table, entries := buildTable(order, bySrc)
	c.table = table
	c.entries = entries
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

// TestLongestPrefixWins is the whole reason for bart: where feeds overlap,
// the more specific range is the more useful attribution.
func TestLongestPrefixWins(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceX4BDC: {cp(t, "52.0.0.0/8", "")},
		SourceAWS:   {cp(t, "52.94.0.0/16", "eu-west-1")},
	}, SourceX4BDC, SourceAWS)

	got := c.Lookup("52.94.1.1")
	if !got.Matched || got.Source != SourceAWS {
		t.Fatalf("Lookup = %+v, want the more specific AWS match", got)
	}
	if got.Detail != "eu-west-1" {
		t.Errorf("Detail = %q, want eu-west-1", got.Detail)
	}

	// An address inside only the broad range still attributes to it.
	if got := c.Lookup("52.1.1.1"); got.Source != SourceX4BDC {
		t.Errorf("Lookup of the broad-only address = %+v, want X4B datacenter", got)
	}
}

// TestApplePrivateRelayWinsOverX4BVPNOnExactCollision is the reproducer
// for the reason SourceApplePrivateRelay is listed before SourceX4BVPN
// in feedRegistry (see that entry's own comment): X4BNet's VPN feed
// pulls Apple's ranges in verbatim, so the exact same prefix can arrive
// from both sources. On that exact-prefix tie, priority order decides --
// this proves it resolves to the authoritative CategoryPrivacyRelay, not
// CategoryVPN, which is the whole point: an iPhone's ordinary Private
// Relay traffic must never read as "known VPN exit".
func TestApplePrivateRelayWinsOverX4BVPNOnExactCollision(t *testing.T) {
	c := build(t, map[Source][]classifiedPrefix{
		SourceApplePrivateRelay: {cp(t, "172.224.226.0/27", "London")},
		SourceX4BVPN:            {cp(t, "172.224.226.0/27", "")},
	}, SourceApplePrivateRelay, SourceX4BVPN)

	got := c.Lookup("172.224.226.5")
	if !got.Matched || got.Category != CategoryPrivacyRelay {
		t.Fatalf("Lookup = %+v, want CategoryPrivacyRelay (Apple's own feed must win the exact-prefix tie over X4B's copy)", got)
	}
	if got.Source != SourceApplePrivateRelay {
		t.Errorf("Source = %q, want %q", got.Source, SourceApplePrivateRelay)
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

// TestLabelsAreInterned proves the entries slice holds one entry per
// distinct (source, detail), not one per prefix -- the memory property
// the #114 research asked for.
func TestLabelsAreInterned(t *testing.T) {
	var many []classifiedPrefix
	for i := 0; i < 200; i++ {
		many = append(many, cp(t, netip.PrefixFrom(netip.AddrFrom4([4]byte{52, byte(i), 0, 0}), 16).String(), "eu-west-1"))
	}
	c := build(t, map[Source][]classifiedPrefix{SourceAWS: many}, SourceAWS)
	if len(c.entries) != 1 {
		t.Errorf("entries = %d for 200 prefixes sharing one label, want 1", len(c.entries))
	}
}

func TestCoverageOf(t *testing.T) {
	ps := []classifiedPrefix{cp(t, "1.0.0.0/24", ""), cp(t, "2.0.0.0/16", "")}
	if got := coverageOf(ps); got != 256+65536 {
		t.Errorf("coverageOf = %d, want %d", got, 256+65536)
	}
}
