// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import "testing"

func TestRowFor(t *testing.T) {
	for _, tc := range []struct {
		name        string
		version     string
		wantOK      bool
		wantDialect string
		wantNote    string
	}{
		{"the floor", "7.18", true, "a", ""},
		{"mid-range", "7.20.1", true, "a", ""},
		{"the find-bug release", "7.24", true, "a",
			"7.24.0 has a `find` argument-lookup bug, fixed in 7.24.1: on this release, tag rules one at a time rather than with the bulk commands."},
		{"newest reviewed", "7.24.1", true, "a", ""},
		{"a channel suffix real routers send", "7.23.3 (stable)", true, "a", ""},
		{"below the floor", "7.12", false, "", ""},
		{"ahead of every row", "7.25", false, "", ""},
		{"unparseable", "not a version", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, ok := RowFor(tc.version)
			if ok != tc.wantOK {
				t.Fatalf("RowFor(%q) ok = %v, want %v", tc.version, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if row.Dialect != tc.wantDialect {
				t.Errorf("RowFor(%q).Dialect = %q, want %q", tc.version, row.Dialect, tc.wantDialect)
			}
			if row.Note != tc.wantNote {
				t.Errorf("RowFor(%q).Note = %q, want %q", tc.version, row.Note, tc.wantNote)
			}
		})
	}
}

// The contract's example table (#436) states these bounds explicitly;
// this pins Rows to them so an edit that reorders or renumbers a row
// can't drift from what the API promises without a test noticing.
func TestRowsMatchTheContract(t *testing.T) {
	want := []Row{
		{From: "7.18", To: "7.23.3", Dialect: "a", VerifiedBy: "exercised on CHR 7.23.3", Note: ""},
		{From: "7.24", To: "7.24", Dialect: "a", VerifiedBy: "release notes read 2026-08-29",
			Note: "7.24.0 has a `find` argument-lookup bug, fixed in 7.24.1: on this release, tag rules one at a time rather than with the bulk commands."},
		{From: "7.24.1", To: "7.24.1", Dialect: "a", VerifiedBy: "release notes read 2026-08-29", Note: ""},
	}
	if len(Rows) != len(want) {
		t.Fatalf("Rows has %d entries, want %d", len(Rows), len(want))
	}
	for i, row := range Rows {
		if row != want[i] {
			t.Errorf("Rows[%d] = %+v, want %+v", i, row, want[i])
		}
	}
}

func TestNewestVersion(t *testing.T) {
	if got := NewestVersion(); got != "7.24.1" {
		t.Errorf("NewestVersion() = %q, want %q", got, "7.24.1")
	}
}
