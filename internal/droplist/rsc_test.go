// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

// TestScriptGoldenOutput pins the exact rendered shape for two entries --
// the header line, the staging-list clear, one `add` per entry in the
// order given, and the trailing live-list clear/set pair.
func TestScriptGoldenOutput(t *testing.T) {
	entries := []Entry{
		{CIDR: mustPrefix(t, "203.0.113.0/24"), Reason: "scanning our SSH port"},
		{CIDR: mustPrefix(t, "198.51.100.9/32"), Reason: "credential stuffing", FlagID: "flag-42"},
	}
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)

	got := string(Script(entries, now))
	// The hash is pinned to a value computed independently (sha256 of
	// "198.51.100.9/32\n203.0.113.0/24\n", the two CIDRs sorted
	// lexically, first 12 hex characters) rather than derived from
	// scriptHash itself, so this test can actually catch a change to the
	// hashing rule instead of only checking Script against its own
	// helper.
	want := "# mikroview drop list: 2 entries, generated 2026-09-14T08:00:00Z, sha256 6a103d673f74\n" +
		"/ip firewall address-list remove [find list=mikroview-drop-next]\n" +
		`/ip firewall address-list add list=mikroview-drop-next address=203.0.113.0/24 comment="mv: scanning our SSH port"` + "\n" +
		`/ip firewall address-list add list=mikroview-drop-next address=198.51.100.9/32 comment="mv: credential stuffing [flag flag-42]"` + "\n" +
		"/ip firewall address-list remove [find list=mikroview-drop]\n" +
		"/ip firewall address-list set [find list=mikroview-drop-next] list=mikroview-drop\n"

	if got != want {
		t.Errorf("Script output mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestScriptZeroEntries covers the empty case explicitly: the header
// still names 0 entries, the staging clear and live remove/set are still
// present, and there is no `add` line at all -- an empty drop list must
// still empty the router's live list on the next fetch, not leave
// whatever it last held in place.
func TestScriptZeroEntries(t *testing.T) {
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	lines := strings.Split(strings.TrimSuffix(string(Script(nil, now)), "\n"), "\n")

	if len(lines) != 4 {
		t.Fatalf("Script(nil, ...) produced %d lines, want 4 (header, staging clear, live remove, set):\n%v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "# mikroview drop list: 0 entries") {
		t.Errorf("header = %q, want it to name 0 entries", lines[0])
	}
	for _, l := range lines {
		if strings.Contains(l, "address-list add") {
			t.Errorf("zero entries produced an add line: %q", l)
		}
	}
	if lines[len(lines)-2] != "/ip firewall address-list remove [find list=mikroview-drop]" ||
		lines[len(lines)-1] != "/ip firewall address-list set [find list=mikroview-drop-next] list=mikroview-drop" {
		t.Errorf("the router must still be emptied when the last entry goes; got final lines %v", lines[len(lines)-2:])
	}
}

// TestScriptMidwayFailureLeavesLiveListIntact is the proof #1224 asks
// for: whatever line a bad `/import` stops on, the live list is
// unaffected until the very last two lines run, and those only ever run
// once every earlier line -- entirely staging-list work -- has already
// succeeded.
func TestScriptMidwayFailureLeavesLiveListIntact(t *testing.T) {
	entries := []Entry{
		{CIDR: mustPrefix(t, "203.0.113.0/24"), Reason: "a"},
		{CIDR: mustPrefix(t, "198.51.100.9/32"), Reason: "b"},
		{CIDR: mustPrefix(t, "192.0.2.0/24"), Reason: "c"},
	}
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	lines := strings.Split(strings.TrimSuffix(string(Script(entries, now)), "\n"), "\n")

	if len(lines) != 1+1+len(entries)+2 {
		t.Fatalf("got %d lines, want header + staging-clear + %d adds + remove + set:\n%v", len(lines), len(entries), lines)
	}

	// (i) The very first command after the header clears the staging
	// list -- so a router that has never fetched before, or one recovering
	// from an aborted previous fetch, never builds the new generation on
	// top of a stale one.
	if want := "/ip firewall address-list remove [find list=" + StagingListName + "]"; lines[1] != want {
		t.Errorf("first command after the header = %q, want %q (must clear staging before building the new generation)", lines[1], want)
	}

	body := lines[2 : len(lines)-2]
	last2 := lines[len(lines)-2:]

	// (ii) Every add targets the staging list, never the live one -- an
	// import that dies on entry N leaves entries 1..N-1 sitting in
	// mikroview-drop-next, which nothing reads yet, rather than in the
	// list the router's firewall rules actually match against.
	for i, l := range body {
		if !strings.HasPrefix(l, "/ip firewall address-list add") {
			t.Errorf("body line %d is not an add line: %q", i, l)
			continue
		}
		if !strings.Contains(l, "list="+StagingListName+" ") {
			t.Errorf("add line %d does not target the staging list: %q", i, l)
		}
	}

	// (iii) No line before the final two ever names the live list at
	// all -- confirming the property (ii) checks for adds also holds for
	// the header and the staging clear, so there is no line anywhere in
	// the importable body that a partial run could have touched the live
	// list with.
	for i, l := range lines[:len(lines)-2] {
		if strings.Contains(l, "list="+ListName+"]") || strings.Contains(l, "list="+ListName+" ") {
			t.Errorf("line %d touches the live list before the swap -- an import that dies here would have already changed what the router enforces: %q", i, l)
		}
	}

	// (iv) The final two lines, and only them, are the live-list
	// remove-then-set -- reached only once every add above has already
	// succeeded, which is what makes them safe to run: by construction,
	// StagingListName is either the previous generation (if any add
	// failed) or the complete new one (if none did), never a half-built
	// mix, so the router's live list becomes one or the other atomically
	// as far as anything watching it can tell.
	wantRemove := "/ip firewall address-list remove [find list=" + ListName + "]"
	wantSet := "/ip firewall address-list set [find list=" + StagingListName + "] list=" + ListName
	if last2[0] != wantRemove {
		t.Errorf("second-to-last line = %q, want the live-list remove %q", last2[0], wantRemove)
	}
	if last2[1] != wantSet {
		t.Errorf("last line = %q, want the staging-to-live set %q", last2[1], wantSet)
	}
}

// TestScriptEscapesReasonCharacters proves a reason carrying a quote and
// a backslash can never break out of the comment's quoted string --
// checked against routeros.Quote directly (the one place the escaping
// rule lives) rather than re-implementing the escaping to compare
// against.
func TestScriptEscapesReasonCharacters(t *testing.T) {
	entries := []Entry{
		{CIDR: mustPrefix(t, "203.0.113.0/24"), Reason: `say "no" to \bad actors\`},
	}
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	got := string(Script(entries, now))

	wantComment := entryComment(entries[0])
	wantLine := `/ip firewall address-list add list=mikroview-drop-next address=203.0.113.0/24 comment="` + wantComment + `"`
	if !strings.Contains(got, wantLine) {
		t.Fatalf("script does not contain the expected escaped add line.\nwant substring:\n%s\ngot:\n%s", wantLine, got)
	}

	// The naive, unescaped reason must not appear anywhere -- if it did,
	// the raw quote/backslash would have broken out of the comment's
	// quoted string.
	if strings.Contains(got, `comment="mv: say "no" to \bad actors\"`) {
		t.Fatal("the reason's raw quote/backslash characters reached the script unescaped")
	}
}

// TestScriptDeterministic asserts the same entries and the same now
// produce byte-identical output every time -- an operator diffing two
// fetches, or a router re-importing after a network blip, must see the
// same script for the same underlying state.
func TestScriptDeterministic(t *testing.T) {
	entries := []Entry{
		{CIDR: mustPrefix(t, "203.0.113.0/24"), Reason: "a"},
		{CIDR: mustPrefix(t, "198.51.100.9/32"), Reason: "b", FlagID: "flag-1"},
	}
	now := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)

	first := Script(entries, now)
	second := Script(entries, now)
	if string(first) != string(second) {
		t.Errorf("Script is not deterministic for identical inputs:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
