// SPDX-License-Identifier: AGPL-3.0-only

// Command routerosappendrow appends one row to
// internal/routeros/dialects.go's Rows table -- the mechanical half of
// #894's weekly CHR exercise. Given -version (what booted) and -dialect
// (what dialect it was exercised against), it inserts
// {From: version, To: version, Dialect: dialect, VerifiedBy: "exercised
// on CHR <version>, <date>", Note: ""} as the table's last row and
// gofmts the file.
//
// It deliberately never touches ReviewedVersion
// (internal/routeros/versions.go): that constant is the "a human read
// the release notes" claim, which this command never makes -- it only
// records that the starting commands still parsed against a booted CHR.
// Leaving ReviewedVersion where it was means
// TestReviewedVersionMatchesNewest goes red on the commit this produces,
// on purpose: whoever reviews the resulting pull request reads the
// release notes and bumps ReviewedVersion themselves, in the same
// change, which is the review step #894's issue body and
// scripts/routeros-freshness.sh both insist stays human.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// currentDialect is the table's newest row's dialect -- "the dialect
// table's current dialect" #894 asks the exercise to run, and the one a
// freshly-exercised version should be recorded against.
func currentDialect() string {
	if len(routeros.Rows) == 0 {
		return ""
	}
	return routeros.Rows[len(routeros.Rows)-1].Dialect
}

// appendRow inserts one new row line just before the closing brace of
// `var Rows = []Row{`, by text -- not go/ast -- because the target is
// one struct-literal slice whose closing brace is unambiguous: it is the
// only line inside the block that is exactly "}" with no leading
// whitespace and no trailing comma (a nested multi-line entry's own
// closing brace is indented and comma-terminated). format.Source
// reformats the result afterward, so exact spacing here does not matter.
func appendRow(src []byte, version, dialect, verifiedBy string) ([]byte, error) {
	lines := strings.Split(string(src), "\n")
	inRows := false
	inserted := false
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if !inRows && strings.TrimSpace(line) == "var Rows = []Row{" {
			inRows = true
		}
		if inRows && !inserted && strings.TrimSpace(line) == "}" {
			out = append(out, fmt.Sprintf(
				"\t{From: %q, To: %q, Dialect: %q, VerifiedBy: %q, Note: \"\"},",
				version, version, dialect, verifiedBy))
			inserted = true
		}
		out = append(out, line)
	}
	if !inserted {
		return nil, fmt.Errorf("routerosappendrow: could not find Rows' closing brace to insert before")
	}
	return []byte(strings.Join(out, "\n")), nil
}

func main() {
	file := flag.String("file", "internal/routeros/dialects.go", "path to the dialects.go file to edit")
	version := flag.String("version", "", "the RouterOS version that was exercised (required)")
	dialect := flag.String("dialect", currentDialect(), "the dialect this version was exercised against (defaults to the table's newest row's dialect)")
	date := flag.String("date", time.Now().UTC().Format("2006-01-02"), "the date to record in verifiedBy (UTC, YYYY-MM-DD)")
	flag.Parse()

	if *version == "" {
		fmt.Fprintln(os.Stderr, "routerosappendrow: -version is required")
		os.Exit(2)
	}

	src, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routerosappendrow: %v\n", err)
		os.Exit(1)
	}

	verifiedBy := fmt.Sprintf("exercised on CHR %s, %s", *version, *date)
	edited, err := appendRow(src, *version, *dialect, verifiedBy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	formatted, err := format.Source(edited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routerosappendrow: formatting %s: %v\n", *file, err)
		os.Exit(1)
	}

	if err := os.WriteFile(*file, formatted, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "routerosappendrow: writing %s: %v\n", *file, err)
		os.Exit(1)
	}
	fmt.Printf("appended: {From: %q, To: %q, Dialect: %q, VerifiedBy: %q}\n", *version, *version, *dialect, verifiedBy)
}
