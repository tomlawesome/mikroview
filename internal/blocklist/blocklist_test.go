// SPDX-License-Identifier: AGPL-3.0-only

package blocklist

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

func TestParseSpamhausSkipsCommentsAndBlankLines(t *testing.T) {
	body := []byte(`; Last updated 2026-01-01
; https://www.spamhaus.org/drop/drop.txt

1.10.16.0/20 ; SBL2472
1.19.0.0/16 ; SBL401503
not-a-cidr ; should be skipped
203.0.113.0/24
`)
	prefixes, _, err := parseSpamhaus(body)
	if err != nil {
		t.Fatalf("parseSpamhaus: %v", err)
	}
	want := []string{"1.10.16.0/20", "1.19.0.0/16", "203.0.113.0/24"}
	if len(prefixes) != len(want) {
		t.Fatalf("got %d prefixes, want %d: %v", len(prefixes), len(want), prefixes)
	}
	for i, w := range want {
		if prefixes[i].String() != w {
			t.Errorf("prefixes[%d] = %s, want %s", i, prefixes[i], w)
		}
	}
}

func TestParseEmergingThreatsCompromised(t *testing.T) {
	body := []byte(`# Emerging Threats compromised IPs
# generated 2026-01-01

198.51.100.7
198.51.100.8
not-an-ip
203.0.113.9
`)
	prefixes, _, err := parseEmergingThreatsCompromised(body)
	if err != nil {
		t.Fatalf("parseEmergingThreatsCompromised: %v", err)
	}
	want := []string{"198.51.100.7/32", "198.51.100.8/32", "203.0.113.9/32"}
	if len(prefixes) != len(want) {
		t.Fatalf("got %d prefixes, want %d: %v", len(prefixes), len(want), prefixes)
	}
	for i, w := range want {
		if prefixes[i].String() != w {
			t.Errorf("prefixes[%d] = %s, want %s", i, prefixes[i], w)
		}
	}
}

// mustPrefixes parses a list of CIDR literals, failing the test on any
// parse error -- test-only convenience so table-driven cases below stay
// readable.
func mustPrefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatalf("ParsePrefix(%q): %v", c, err)
		}
		out[i] = p
	}
	return out
}

func TestSearchRangesBoundaries(t *testing.T) {
	ranges := buildRanges(mustPrefixes(t, "10.0.0.0/24", "10.0.2.0/23", "192.168.100.0/30"))
	// 10.0.0.0/24 -> 10.0.0.0-10.0.0.255
	// 10.0.2.0/23 -> 10.0.2.0-10.0.3.255
	// 192.168.100.0/30 -> 192.168.100.0-192.168.100.3

	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.0", true},    // exact start of first range
		{"10.0.0.255", true},  // exact end of first range
		{"10.0.0.128", true},  // interior of first range
		{"10.0.1.0", false},   // gap between the two 10.0.x ranges
		{"10.0.1.255", false}, // still in the gap, right before the second range
		{"10.0.2.0", true},    // exact start of second range
		{"10.0.3.255", true},  // exact end of second range
		{"10.0.4.0", false},   // just past the second range
		{"9.255.255.255", false},
		{"192.168.100.0", true},
		{"192.168.100.3", true},
		{"192.168.100.4", false},
		{"192.168.99.255", false},
		{"255.255.255.255", false},
		{"0.0.0.0", false},
	}
	for _, c := range cases {
		addr := netip.MustParseAddr(c.ip)
		_, got := searchRanges(ranges, addr)
		if got != c.want {
			t.Errorf("searchRanges(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestBuildRangesMergesOverlappingAndNestedPrefixes(t *testing.T) {
	// 10.0.0.0/16 fully contains 10.0.5.0/24 -- without merging, a naive
	// "rightmost lo <= target" binary search would miss an address
	// inside the wide range but after the narrow one's own end, since
	// the narrow range's lo sorts after the wide range's lo. This test
	// guards against exactly that regression.
	//
	// Nesting (one prefix fully containing another) is the only way two
	// real CIDR prefixes can ever overlap without being identical --
	// two CIDR blocks are always either disjoint, equal, or one strictly
	// contains the other, never "partially overlapping" the way two
	// arbitrary intervals can. So this nested case is the only overlap
	// shape buildRanges' merge logic ever actually needs to handle for
	// real feed data.
	ranges := buildRanges(mustPrefixes(t, "10.0.0.0/16", "10.0.5.0/24"))
	if len(ranges) != 1 {
		t.Fatalf("expected the nested prefixes to merge into 1 range, got %d: %+v", len(ranges), ranges)
	}
	// An address inside 10.0.0.0/16 but after 10.0.5.0/24's own end
	// (10.0.5.255) must still match, via the merged wide range.
	addr := netip.MustParseAddr("10.0.9.1")
	if _, ok := searchRanges(ranges, addr); !ok {
		t.Errorf("expected %s to match the merged range, got no match", addr)
	}
}

func TestRangeEntryLabelFallsBackWhenMerged(t *testing.T) {
	ranges := buildRanges(mustPrefixes(t, "10.0.0.0/16", "10.0.5.0/24"))
	if got := ranges[0].label(); got != "10.0.0.0-10.0.255.255" {
		t.Errorf("label() = %q, want the merged lo-hi range", got)
	}

	single := buildRanges(mustPrefixes(t, "203.0.113.0/24"))
	if got := single[0].label(); got != "203.0.113.0/24" {
		t.Errorf("label() for an unmerged single prefix = %q, want the original CIDR", got)
	}
}

func TestRefreshKeepsLastGoodListOnFailure(t *testing.T) {
	var mu sync.Mutex
	fail := false
	body := []byte("203.0.113.0/24\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	b := &Blocklist{
		feeds:  map[Source]*feedState{SourceSpamhausDROP: {def: feedDef{Source: SourceSpamhausDROP, Label: "test-feed", URL: srv.URL, Parse: parseSpamhaus}}},
		client: srv.Client(),
		log:    logging.New("blocklist-test"),
	}

	b.Refresh(context.Background())
	if _, ok := b.Match("203.0.113.5"); !ok {
		t.Fatal("expected a match after the first successful refresh")
	}

	mu.Lock()
	fail = true
	mu.Unlock()
	b.Refresh(context.Background())

	// The feed now fails on every fetch, but the previously-loaded data
	// must still be served.
	if _, ok := b.Match("203.0.113.5"); !ok {
		t.Fatal("expected the last successfully-fetched list to keep serving matches after a failed refresh")
	}

	b.mu.RLock()
	lastErr := b.feeds[SourceSpamhausDROP].lastErr
	b.mu.RUnlock()
	if lastErr == nil {
		t.Error("expected lastErr to be recorded after a failed refresh")
	}
}

func TestRefreshRecoversAfterFailure(t *testing.T) {
	var mu sync.Mutex
	fail := false
	body := []byte("203.0.113.0/24\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	b := &Blocklist{
		feeds:  map[Source]*feedState{SourceSpamhausDROP: {def: feedDef{Source: SourceSpamhausDROP, Label: "test-feed", URL: srv.URL, Parse: parseSpamhaus}}},
		client: srv.Client(),
		log:    logging.New("blocklist-test"),
	}

	mu.Lock()
	fail = true
	mu.Unlock()
	b.Refresh(context.Background())
	if _, ok := b.Match("203.0.113.5"); ok {
		t.Fatal("expected no match before any successful refresh has happened")
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	b.Refresh(context.Background())
	if _, ok := b.Match("203.0.113.5"); !ok {
		t.Fatal("expected a match once the feed recovers")
	}
}

func TestRefreshTruncatesAtCombinedEntryCap(t *testing.T) {
	// Build a feed body with more /32s than the combined cap, to verify
	// Refresh truncates rather than loading everything unbounded.
	var sb strings.Builder
	n := maxTotalEntries + 500
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "10.%d.%d.%d\n", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sb.String()))
	}))
	defer srv.Close()

	b := &Blocklist{
		feeds: map[Source]*feedState{
			SourceEmergingThreatsCompromised: {def: feedDef{Source: SourceEmergingThreatsCompromised, Label: "big-feed", URL: srv.URL, Parse: parseEmergingThreatsCompromised}},
		},
		client: srv.Client(),
		log:    logging.New("blocklist-test"),
	}
	b.Refresh(context.Background())

	b.mu.RLock()
	rawCount := b.feeds[SourceEmergingThreatsCompromised].rawCount
	b.mu.RUnlock()
	if rawCount != maxTotalEntries {
		t.Errorf("rawCount = %d, want exactly the cap (%d)", rawCount, maxTotalEntries)
	}
}

func TestKnownSourcesMatchesRegistryOrder(t *testing.T) {
	want := []Source{SourceSpamhausDROP, SourceEmergingThreatsCompromised}
	got := KnownSources()
	if len(got) != len(want) {
		t.Fatalf("KnownSources() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KnownSources()[%d] = %s, want %s (registry order matters -- see orderedEnabledSources' doc comment)", i, got[i], want[i])
		}
	}
}

func TestNewSkipsUnknownSources(t *testing.T) {
	b := New([]string{"spamhaus_drop", "not_a_real_feed"}, logging.New("blocklist-test"))
	if !b.HasFeeds() {
		t.Fatal("expected the recognized source to still be enabled")
	}
	if len(b.feeds) != 1 {
		t.Fatalf("expected exactly 1 recognized feed, got %d", len(b.feeds))
	}
}

func TestNewWithNoSourcesHasNoFeeds(t *testing.T) {
	b := New(nil, logging.New("blocklist-test"))
	if b.HasFeeds() {
		t.Fatal("expected HasFeeds() to be false with no configured sources")
	}
	if _, ok := b.Match("203.0.113.5"); ok {
		t.Fatal("expected no match with no feeds configured")
	}
}

func TestMatchReturnsSourceAndLabel(t *testing.T) {
	b := &Blocklist{
		feeds: map[Source]*feedState{
			SourceSpamhausDROP: {
				def:    feedDef{Source: SourceSpamhausDROP, Label: "Spamhaus DROP"},
				ranges: buildRanges(mustPrefixes(t, "203.0.113.0/24")),
			},
		},
	}
	match, ok := b.Match("203.0.113.9")
	if !ok {
		t.Fatal("expected a match")
	}
	if match.Source != SourceSpamhausDROP || match.Label != "Spamhaus DROP" || match.Range != "203.0.113.0/24" {
		t.Errorf("unexpected match: %+v", match)
	}
}

// TestSearchRangesIsFastAtCap is a coarse performance sanity check, not
// a strict benchmark: a linear scan over maxTotalEntries entries would
// still "pass" a generous wall-clock budget on modern hardware, so this
// doesn't prove O(log n) on its own -- but it does catch a gross
// regression (e.g. accidentally reintroducing a linear scan across many
// thousands of Match calls) within CI's own time budget, and documents
// the expectation that a single lookup against a full-size table is
// microsecond-scale, not millisecond-scale.
func TestSearchRangesIsFastAtCap(t *testing.T) {
	prefixes := make([]netip.Prefix, 0, maxTotalEntries)
	for i := 0; i < maxTotalEntries; i++ {
		addr := netip.AddrFrom4([4]byte{10, byte(i >> 8), byte(i), 0})
		prefixes = append(prefixes, netip.PrefixFrom(addr, 24))
	}
	ranges := buildRanges(prefixes)

	// A target address picked from the middle of the generated range
	// (i = maxTotalEntries/2), rather than an arbitrary literal --
	// using one outside the actual generated address space would make
	// every lookup a guaranteed (fast) miss instead of exercising a
	// real match.
	mid := maxTotalEntries / 2
	target := netip.AddrFrom4([4]byte{10, byte(mid >> 8), byte(mid), 128})
	start := time.Now()
	const iterations = 100_000
	hits := 0
	for i := 0; i < iterations; i++ {
		if _, ok := searchRanges(ranges, target); ok {
			hits++
		}
	}
	elapsed := time.Since(start)
	if hits != iterations {
		t.Fatalf("expected every lookup to match, got %d/%d", hits, iterations)
	}
	if elapsed > 2*time.Second {
		t.Errorf("%d lookups against a %d-entry table took %s, expected well under 2s for an O(log n) search", iterations, len(ranges), elapsed)
	}
}

// Spamhaus's DROP terms require that "credit must be given to Spamhaus
// Project, and the date and © text should remain with the file and
// data". That text lives in the feed's leading ";" comment block, which
// the parser used to discard outright -- so this pins that it survives
// parsing, because losing it is a licence-compliance failure and not
// merely a cosmetic one.
func TestSpamhausAttributionNoticeSurvivesParsing(t *testing.T) {
	body := []byte("; Spamhaus DROP List 2026/08/10 - (c) 2026 The Spamhaus Project SLU\n" +
		"; https://www.spamhaus.org/drop/drop.txt\n" +
		"; Last-Modified: Mon, 10 Aug 2026 12:30:17 GMT\n" +
		"\n" +
		"1.10.16.0/20 ; SBL256894\n")

	prefixes, notice, err := parseSpamhaus(body)
	if err != nil {
		t.Fatalf("parseSpamhaus: %v", err)
	}
	if len(prefixes) != 1 {
		t.Fatalf("got %d prefixes, want 1", len(prefixes))
	}
	if !strings.Contains(notice, "(c) 2026 The Spamhaus Project SLU") {
		t.Errorf("copyright line missing from the retained notice:\n%s", notice)
	}
	if !strings.Contains(notice, "2026/08/10") {
		t.Errorf("list date missing from the retained notice:\n%s", notice)
	}
}

// A feed whose body is nothing but comments parses "successfully" with
// zero entries. That is exactly what the retired Spamhaus EDROP endpoint
// returns, and treating it as a healthy refresh is what let it sit
// enabled-by-default for two years contributing nothing.
func TestFeedOfOnlyCommentsYieldsNoPrefixes(t *testing.T) {
	body := []byte("; This list has been merged into https://www.spamhaus.org/drop/drop.txt\n; EOF\n")
	prefixes, notice, err := parseSpamhaus(body)
	if err != nil {
		t.Fatalf("parseSpamhaus: %v", err)
	}
	if len(prefixes) != 0 {
		t.Fatalf("got %d prefixes, want 0", len(prefixes))
	}
	if notice == "" {
		t.Error("expected the comment block to still be retained as a notice")
	}
}

// EDROP must not come back as a selectable source: Spamhaus merged it
// into DROP, so offering it would advertise protection that fetches
// nothing.
func TestRetiredEdropIsNotOnTheMenu(t *testing.T) {
	for _, s := range KnownSources() {
		if strings.Contains(string(s), "edrop") {
			t.Errorf("KnownSources() still offers %q -- EDROP was merged into DROP on 2024-04-10 and serves no ranges", s)
		}
	}
	for _, s := range DefaultSources {
		if strings.Contains(s, "edrop") {
			t.Errorf("DefaultSources still enables %q by default", s)
		}
	}
}
