// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"strings"
	"testing"
)

func upperPrefix(action string) string {
	if action == "" {
		return ""
	}
	return strings.ToUpper(action[:1]) + "|" + action + "|"
}

// assertEqualLines fails the test if got and want differ anywhere,
// naming the segment and the first differing line.
func assertEqualLines(t *testing.T, segment string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d lines, want %d", segment, len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: line %d differs:\n got: %q\nwant: %q", segment, i, got[i], want[i])
		}
	}
}

// TestRenderTouchesOnlySelectedRules is Render's central promise: every
// line outside a selected rule's own Line..LineEnd range is copied
// through byte-for-byte, and an unselected rule (idx 3, "guest network
// to wan only", already logging) is untouched even though it sits
// between two rules that are selected. Compared by segment (not by raw
// line index) because Render collapses each edited rule's continuation
// lines to one line, shifting every line number after it -- exactly
// the shift a naive same-index comparison would miss.
func TestRenderTouchesOnlySelectedRules(t *testing.T) {
	text := loadFixture(t)
	before, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}

	r1 := before.FilterRules[1] // log=no, empty log-prefix
	r4 := before.FilterRules[4] // no log attributes at all

	// Select idx 1 and idx 4, leaving idx 3 (already log=yes) alone in
	// between them.
	annotated, changed := before.Render([]int{1, 4}, upperPrefix)
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}

	beforeLines := strings.Split(text, "\n")
	afterLines := strings.Split(annotated, "\n")

	prefixBefore := beforeLines[:r1.Line-1]
	gapBefore := beforeLines[r1.LineEnd : r4.Line-1]
	suffixBefore := beforeLines[r4.LineEnd:]

	wantLen := len(prefixBefore) + 1 + len(gapBefore) + 1 + len(suffixBefore)
	if len(afterLines) != wantLen {
		t.Fatalf("annotated text has %d lines, want %d (two rules collapsed from 2 physical lines to 1 each)", len(afterLines), wantLen)
	}

	prefixAfter := afterLines[:len(prefixBefore)]
	gapAfter := afterLines[len(prefixBefore)+1 : len(prefixBefore)+1+len(gapBefore)]
	suffixAfter := afterLines[len(prefixBefore)+1+len(gapBefore)+1:]

	assertEqualLines(t, "prefix (before rule 1)", prefixAfter, prefixBefore)
	assertEqualLines(t, "gap (between rule 1 and rule 4, includes rule 3)", gapAfter, gapBefore)
	assertEqualLines(t, "suffix (after rule 4)", suffixAfter, suffixBefore)

	after, err := Parse(annotated)
	if err != nil {
		t.Fatalf("re-parsing the annotated text failed: %v", err)
	}
	if len(after.FilterRules) != len(before.FilterRules) {
		t.Fatalf("rule count changed: %d -> %d", len(before.FilterRules), len(after.FilterRules))
	}

	r1After := after.FilterRules[1]
	if !r1After.Log || r1After.LogPrefix != "A|accept|" {
		t.Errorf("rule 1 = log=%v prefix=%q, want log=true prefix=A|accept| (derived, was empty)", r1After.Log, r1After.LogPrefix)
	}
	r4After := after.FilterRules[4]
	if !r4After.Log || r4After.LogPrefix != "D|drop|" {
		t.Errorf("rule 4 = log=%v prefix=%q, want log=true prefix=D|drop| (derived, was absent)", r4After.Log, r4After.LogPrefix)
	}

	// Untouched rules keep exactly their original logging state.
	r3 := after.FilterRules[3]
	if !r3.Log || r3.LogPrefix != "A|guest|" {
		t.Errorf("rule 3 (not selected) = log=%v prefix=%q, want its original log=true prefix=A|guest| unchanged", r3.Log, r3.LogPrefix)
	}
}

// TestRenderPreservesExistingLogPrefix covers the "only when the rule's
// own log-prefix is empty" rule: rule 5 already carries a non-empty,
// custom prefix, and selecting it must not overwrite it, only ensure
// log=yes (already true here).
func TestRenderPreservesExistingLogPrefix(t *testing.T) {
	before, err := Parse(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	annotated, changed := before.Render([]int{5}, upperPrefix)
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	after, err := Parse(annotated)
	if err != nil {
		t.Fatal(err)
	}
	r5 := after.FilterRules[5]
	if !r5.Log || r5.LogPrefix != "R|custom|" {
		t.Errorf("rule 5 = log=%v prefix=%q, want its original custom prefix preserved", r5.Log, r5.LogPrefix)
	}
	// The rest of that rule's own attributes (in particular the quoted,
	// escaped comment) survive the round trip through Render.
	if r5.Comment != `reject intra-lan pair (a "noisy" host)` {
		t.Errorf("rule 5 comment = %q, want the original escaped comment preserved", r5.Comment)
	}
}

// TestRenderFingerprintsMatchExceptLogging is the mechanical check
// POST /api/tune-logging/render itself performs: after Render, every
// rule's Fingerprint (everything except log/log-prefix) is unchanged
// from before, whether or not that rule was selected.
func TestRenderFingerprintsMatchExceptLogging(t *testing.T) {
	before, err := Parse(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	annotated, _ := before.Render([]int{0, 1, 2, 3, 4, 5, 6}, upperPrefix)
	after, err := Parse(annotated)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.FilterRules) != len(before.FilterRules) {
		t.Fatalf("rule count changed: %d -> %d", len(before.FilterRules), len(after.FilterRules))
	}
	for i := range before.FilterRules {
		if before.FilterRules[i].Fingerprint() != after.FilterRules[i].Fingerprint() {
			t.Errorf("rule %d: fingerprint changed across Render, want only logging to differ:\nbefore: %+v\nafter:  %+v",
				i, before.FilterRules[i].Fingerprint(), after.FilterRules[i].Fingerprint())
		}
	}
}

// TestFingerprintCatchesANonLoggingChange is the negative case for the
// check above: two rules that differ in a non-logging attribute must
// not fingerprint equal, or the mechanical enforcement in
// internal/api/tunelogging.go would wave through a bug that corrupted
// something other than logging.
func TestFingerprintCatchesANonLoggingChange(t *testing.T) {
	a := Rule{Chain: "forward", Action: "accept", Comment: "x", InInterface: "ether1"}
	b := a
	b.InInterface = "ether2"
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("fingerprints matched despite a different InInterface")
	}

	c := a
	c.Log = true
	c.LogPrefix = "A|accept|"
	if a.Fingerprint() != c.Fingerprint() {
		t.Error("fingerprints differed on Log/LogPrefix alone, want them ignored")
	}
}

// TestRenderSkipsUnknownIDs covers a selected id with no matching rule
// -- Render must not panic or corrupt the text, just skip it.
func TestRenderSkipsUnknownIDs(t *testing.T) {
	text := loadFixture(t)
	before, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	annotated, changed := before.Render([]int{999, 1}, upperPrefix)
	if changed != 1 {
		t.Errorf("changed = %d, want 1 (999 has no matching rule)", changed)
	}
	if _, err := Parse(annotated); err != nil {
		t.Errorf("annotated text with a skipped unknown id failed to re-parse: %v", err)
	}
}

// TestRenderWithNoSelectionIsByteIdentical covers the degenerate case:
// selecting nothing must reproduce the input exactly, the same
// guarantee TestParseRoundTripsByteIdentical pins for Parse/Text alone.
func TestRenderWithNoSelectionIsByteIdentical(t *testing.T) {
	text := loadFixture(t)
	before, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	annotated, changed := before.Render(nil, upperPrefix)
	if changed != 0 {
		t.Errorf("changed = %d, want 0", changed)
	}
	if annotated != text {
		t.Error("Render with no selection did not reproduce the input byte-identically")
	}
}
