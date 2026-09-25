// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func header(day int) string {
	return fmt.Sprintf("# 2026/09/%02d 03:00:00 by RouterOS 7.24.1", day)
}

func shown(d []DiffLine) []string {
	out := make([]string, len(d))
	for i, l := range d {
		out[i] = l.Op + l.Text
	}
	return out
}

func wantLines(t *testing.T, got []DiffLine, want []string) {
	t.Helper()
	g := shown(got)
	if len(g) != len(want) {
		t.Fatalf("diff = %q, want %q", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("diff = %q, want %q", g, want)
		}
	}
}

// TestDiffIgnoresOnlyTheDateHeader is the nightly case: the same config
// exported twice differs only in the header line RouterOS stamps on it,
// and a comparison that reported that would report one every single
// night.
func TestDiffIgnoresOnlyTheDateHeader(t *testing.T) {
	body := "\n/ip firewall filter\nadd action=accept chain=input\n"
	got := Diff(header(1)+body, header(2)+body)
	if len(got) != 0 {
		t.Errorf("diff = %q, want nothing but the date header ignored", shown(got))
	}
}

// TestDiffSeesAnAddedAndARemovedRule is the shape the UI's two-colour
// list is drawn from: what the older export had and the newer does not,
// and the other way round. Shared lines are absent entirely.
func TestDiffSeesAnAddedAndARemovedRule(t *testing.T) {
	from := strings.Join([]string{
		header(1),
		"/ip firewall filter",
		"add action=accept chain=input comment=established",
		"add action=drop chain=forward comment=old",
	}, "\n")
	to := strings.Join([]string{
		header(2),
		"/ip firewall filter",
		"add action=accept chain=input comment=established",
		"add action=drop chain=forward comment=new",
		"add action=accept chain=forward comment=extra",
	}, "\n")

	wantLines(t, Diff(from, to), []string{
		"-add action=drop chain=forward comment=old",
		"+add action=drop chain=forward comment=new",
		"+add action=accept chain=forward comment=extra",
	})
}

// TestDiffReportsLineNumbersInTheirOwnSide: a removal's number is where
// it was in the older export, an addition's where it is in the newer.
func TestDiffReportsLineNumbersInTheirOwnSide(t *testing.T) {
	from := header(1) + "\nkeep\ngone\nkeep-too\n"
	to := header(2) + "\nkeep\nkeep-too\narrived\n"

	got := Diff(from, to)
	if len(got) != 2 {
		t.Fatalf("diff = %q, want two lines", shown(got))
	}
	if got[0].Op != DiffRemoved || got[0].Text != "gone" || got[0].Line != 3 {
		t.Errorf("removal = %+v, want - \"gone\" at line 3", got[0])
	}
	if got[1].Op != DiffAdded || got[1].Text != "arrived" || got[1].Line != 4 {
		t.Errorf("addition = %+v, want + \"arrived\" at line 4", got[1])
	}
}

// TestDiffShowsTheRedactionMarkerChanging: the marker Redact prepends is
// not ignored. A router that started leaking a secret past
// hide-sensitive is exactly the kind of change a comparison is for.
func TestDiffShowsTheRedactionMarkerChanging(t *testing.T) {
	from := header(1) + "\n/ip firewall filter\nadd action=accept chain=input\n"
	to := "# mikroview: 1 secret values removed at ingest (lines 4)\n" +
		header(2) + "\n/ip firewall filter\nadd action=accept chain=input\n"

	wantLines(t, Diff(from, to), []string{
		"+# mikroview: 1 secret values removed at ingest (lines 4)",
	})
}

// TestDiffOfIdenticalTextIsEmpty, including the trailing-newline case:
// a file ending properly is not a line either side "has".
func TestDiffOfIdenticalTextIsEmpty(t *testing.T) {
	in := loadFixture(t)
	if got := Diff(in, in); len(got) != 0 {
		t.Errorf("diff of a file with itself = %q, want nothing", shown(got))
	}
	if got := Diff(in, strings.TrimRight(in, "\n")); len(got) != 0 {
		t.Errorf("diff across a trailing newline = %q, want nothing", shown(got))
	}
}

// TestDiffFallsBackWholesaleOnTooManyEdits: past maxDiffEdits the
// answer is the whole of one side removed and the whole of the other
// added, rather than an unreadable list nobody could act on.
func TestDiffFallsBackWholesaleOnTooManyEdits(t *testing.T) {
	var from, to strings.Builder
	n := maxDiffEdits + 200
	for i := 0; i < n; i++ {
		fmt.Fprintf(&from, "old line %d\n", i)
		fmt.Fprintf(&to, "new line %d\n", i)
	}
	got := Diff(from.String(), to.String())
	if len(got) != 2*n {
		t.Fatalf("diff has %d lines, want %d (every line of each side)", len(got), 2*n)
	}
	if got[0].Op != DiffRemoved || got[n].Op != DiffAdded {
		t.Errorf("fallback = %s then %s, want every removal then every addition", got[0].Op, got[n].Op)
	}
}

// TestDiffDoesNotBuildTheMyersTraceItWillThrowAway (#1269): the same
// too-many-edits pair as above still falls back to wholesale (that
// answer is not changing), but getting there should not cost building
// the Myers backtracking trace first. Before the fix, myers snapshotted
// every one of the 2001 rounds up to maxDiffEdits regardless -- roughly
// d² ints, about 39 MiB for this input -- and only then discovered no
// solution existed within budget and discarded all of it. Deciding the
// distance first, with no snapshot, keeps this call's own allocation to
// a small multiple of n+m rather than a small multiple of maxDiffEdits².
func TestDiffDoesNotBuildTheMyersTraceItWillThrowAway(t *testing.T) {
	var from, to strings.Builder
	n := maxDiffEdits + 200
	for i := 0; i < n; i++ {
		fmt.Fprintf(&from, "old line %d\n", i)
		fmt.Fprintf(&to, "new line %d\n", i)
	}
	fromStr, toStr := from.String(), to.String()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := Diff(fromStr, toStr)
	runtime.ReadMemStats(&after)

	if len(got) != 2*n {
		t.Fatalf("diff has %d lines, want %d (every line of each side)", len(got), 2*n)
	}

	const budget = 8 * 1024 * 1024 // generous: the trace this replaces alone ran to roughly 39 MiB
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
		t.Errorf("Diff allocated %d bytes falling back to wholesale, want under %d -- looks like the "+
			"abandoned Myers trace is being built again", allocated, budget)
	}
}

// TestDiffOverLineCapNeverBuildsFullSides (#1280): two exports past
// maxDiffLines on their own -- the shape of a pair crafted as millions
// of short lines near the 16 MiB vault cap -- used to have both sides
// built in full as []diffSide, one entry per line, before Diff ever
// looked at maxDiffLines. That is the actual cost the compare screen
// paid: hundreds of MB per side for a pair that was always going to
// answer wholesale. The lines here never coincide between from and to
// ("old N" vs "new N"), so the common-prefix/suffix trim the old code
// ran first would not have removed anything either -- the early exit's
// answer is provably the same one Diff returned before this fix for
// this shape of input, just without paying to build either side.
func TestDiffOverLineCapNeverBuildsFullSides(t *testing.T) {
	var from, to strings.Builder
	n := maxDiffLines + 200
	for i := 0; i < n; i++ {
		fmt.Fprintf(&from, "old line %d\n", i)
		fmt.Fprintf(&to, "new line %d\n", i)
	}
	fromStr, toStr := from.String(), to.String()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := Diff(fromStr, toStr)
	runtime.ReadMemStats(&after)

	if len(got) != 2*n {
		t.Fatalf("diff has %d lines, want %d (every line of each side)", len(got), 2*n)
	}
	if got[0].Op != DiffRemoved || got[n].Op != DiffAdded {
		t.Errorf("fallback = %s then %s, want every removal then every addition", got[0].Op, got[n].Op)
	}

	// Measured at this size: the code before this fix allocated about
	// 8.07 MB building both full sides before ever checking the cap;
	// walking the text directly instead brings that to about 4.03 MB
	// (the wholesale output itself, which still has to exist). Budget
	// partway between the two, so a regression back to building a full
	// side first is caught well before it could reach the old figure.
	const budget = 6 * 1024 * 1024
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > budget {
		t.Errorf("Diff allocated %d bytes past the line cap, want under %d -- looks like a full side "+
			"is being built before the cap is checked", allocated, budget)
	}
}
