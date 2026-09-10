// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `// SPDX-License-Identifier: AGPL-3.0-only

package routeros

type Row struct {
	From       string
	To         string
	Dialect    string
	VerifiedBy string
	Note       string
}

var Rows = []Row{
	{From: "7.18", To: "7.23.3", Dialect: "a", VerifiedBy: "exercised on CHR 7.23.3", Note: ""},
	{
		From: "7.24", To: "7.24", Dialect: "a", VerifiedBy: "release notes read 2026-08-29",
		Note: "a note that spans nothing in particular",
	},
	{From: "7.24.1", To: "7.24.1", Dialect: "a", VerifiedBy: "release notes read 2026-08-29", Note: ""},
}

func NewestVersion() string { return "" }
`

// TestAppendRowInsertsLastRow covers the actual insertion: the new row
// lands after the existing three, inside the same slice literal, not
// swallowing or duplicating anything already there.
func TestAppendRowInsertsLastRow(t *testing.T) {
	out, err := appendRow([]byte(fixture), "7.25.0", "a", "exercised on CHR 7.25.0, 2026-09-10")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	if strings.Count(got, "VerifiedBy:") != 4 {
		t.Fatalf("want 4 rows after insertion, got:\n%s", got)
	}
	if !strings.Contains(got, `{From: "7.25.0", To: "7.25.0", Dialect: "a", VerifiedBy: "exercised on CHR 7.25.0, 2026-09-10", Note: ""},`) {
		t.Errorf("new row missing or malformed:\n%s", got)
	}
	// The new row must be the last one inside the slice, after
	// 7.24.1's row and before the closing brace -- not spliced in
	// the middle of the existing rows.
	last := strings.LastIndex(got, `VerifiedBy: "release notes read 2026-08-29"`)
	newRow := strings.Index(got, `7.25.0`)
	if newRow < last {
		t.Errorf("new row is not last: found at %d, want after %d", newRow, last)
	}
	// Nothing outside the Rows slice (NewestVersion, the closing
	// brace of the struct type, etc.) should have moved or been
	// touched.
	if !strings.Contains(got, "func NewestVersion() string") {
		t.Errorf("code after the Rows slice was lost:\n%s", got)
	}
}

// TestAppendRowThenFormatSourceIsValidAndIdempotent guards the two
// things this tool must never produce on a real file: output that does
// not parse, or output gofmt would still want to rewrite (the same
// gofmt -l check lint:go runs in CI).
func TestAppendRowThenFormatSourceIsValidAndIdempotent(t *testing.T) {
	out, err := appendRow([]byte(fixture), "7.25.0", "a", "exercised on CHR 7.25.0, 2026-09-10")
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("format.Source: %v\noutput was:\n%s", err, out)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "dialects.go", formatted, parser.AllErrors); err != nil {
		t.Fatalf("formatted output does not parse: %v", err)
	}
	twice, err := format.Source(formatted)
	if err != nil {
		t.Fatalf("format.Source (second pass): %v", err)
	}
	if string(twice) != string(formatted) {
		t.Errorf("formatting is not idempotent -- gofmt -l would still flag this file")
	}
}

// TestAppendRowNoClosingBrace covers the one way this can honestly fail:
// asked to edit a file whose Rows slice it cannot find, it must say so
// rather than silently doing nothing or corrupting the file.
func TestAppendRowNoClosingBrace(t *testing.T) {
	if _, err := appendRow([]byte("package routeros\n"), "7.25.0", "a", "exercised on CHR 7.25.0, 2026-09-10"); err == nil {
		t.Error("want an error when Rows' closing brace cannot be found, got nil")
	}
}

// TestMainAgainstRealDialectsFile exercises the actual code path main()
// uses -- read, edit, format, write -- against a copy of the real
// internal/routeros/dialects.go, so a change to that file's shape (not
// just this test's fixture) is what would catch a real drift.
func TestMainAgainstRealDialectsFile(t *testing.T) {
	real, err := os.ReadFile(filepath.Join("..", "..", "internal", "routeros", "dialects.go"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "dialects.go")
	if err := os.WriteFile(tmp, real, 0o644); err != nil {
		t.Fatal(err)
	}

	edited, err := appendRow(real, "9.9.9", "a", "exercised on CHR 9.9.9, 2026-09-10")
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(edited)
	if err != nil {
		t.Fatalf("format.Source on the real file: %v", err)
	}
	if !strings.Contains(string(formatted), `VerifiedBy: "exercised on CHR 9.9.9, 2026-09-10"`) {
		t.Errorf("new row missing from the real file's edited output")
	}
	if strings.Count(string(formatted), "VerifiedBy:") != strings.Count(string(real), "VerifiedBy:")+1 {
		t.Errorf("row count did not grow by exactly one")
	}
}
