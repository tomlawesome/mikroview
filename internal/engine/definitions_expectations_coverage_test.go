// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file moves internal/watchlist/characterization_test.go's two
// TestCharacterizationCoverage_* tests, which called the package-level
// watchlist.Coverage(entry, rulesByDevice) issue #407's fourth handover
// deleted: Coverage moved to internal/engine/coverage.go as
// Definition.Coverage(rulesByDevice) (coverage.go's own doc comment).
//
// Driven through the public entry point end to end -- build the
// expectation Definition for the entry with ExpectationDefinitionFor
// (the same conversion a live entry evaluates as, expectation.go), then
// call .Coverage(...) on it -- rather than against coverageForEntry
// directly (which coverage_test.go's own suite already exercises at
// close range), so this pin proves the move preserved behaviour through
// the whole conversion, not just that the ported function body still
// works in isolation. Every assertion and every word of reasoning below
// is carried over unchanged from characterization_test.go, including the
// #367 known-wrong-answer pin.

// TestCharacterizationCoverage_EachState pins Coverage's four possible
// answers side by side, at the same entry, so the four are read as one
// contrasting set rather than scattered across coverage_test.go's
// per-mechanism tests (which this does not replace -- see this file's
// header comment).
func TestCharacterizationCoverage_EachState(t *testing.T) {
	entry := watchlist.Entry{ID: "e", Ports: []int{22}}
	def, err := ExpectationDefinitionFor(entry)
	if err != nil {
		t.Fatalf("ExpectationDefinitionFor: %v", err)
	}

	if got := def.Coverage(nil); got != CoverageUnknown {
		t.Errorf("no pushed tables at all: Coverage = %v, want %v", got, CoverageUnknown)
	}
	noLogging := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept"}}}
	if got := def.Coverage(noLogging); got != CoverageNoLogging {
		t.Errorf("rules pushed, none logging: Coverage = %v, want %v", got, CoverageNoLogging)
	}
	outOfScope := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept", Log: true, DstPort: "80"}}}
	if got := def.Coverage(outOfScope); got != CoverageOutOfScope {
		t.Errorf("a logging rule that excludes this entry's port: Coverage = %v, want %v", got, CoverageOutOfScope)
	}
	ok := map[string][]ingest.FilterRule{"router-a": {{Chain: "input", Action: "accept", Log: true, DstPort: "22"}}}
	if got := def.Coverage(ok); got != CoverageOK {
		t.Errorf("a logging rule admitting this entry's port: Coverage = %v, want %v", got, CoverageOK)
	}
}

// TestCharacterizationCoverage_367IncompleteDeviceMapReadsAsNoLogging
// pins the exact mechanism #367 reports: Coverage(rulesByDevice) answers
// only from the rulesByDevice map it is handed, and cannot tell "no other
// router is watching" apart from "some other router is watching, but its
// rules simply were not included in this map." The real bug -- and the
// real fix -- is one level up, in internal/api.watchlistCoverage, which
// builds rulesByDevice only from routers that completed the optional
// filter-rule state push, silently omitting a router that streams live
// syslog (and is actively producing matches) but never did that push.
// Coverage() itself has no way to know a router is missing from its
// input, so it confidently returns CoverageNoLogging here -- exactly the
// "confident wrong answer" #367's severity section calls out, reproduced
// at the level this package can reach.
//
// This pin is expected to *survive* #367's fix unchanged: the fix
// changes what internal/api.watchlistCoverage passes in (refusing to
// answer NoLogging unless the pushed device set is known to be
// complete), not what Coverage() does with whatever map it is given.
// If a later change teaches Coverage() itself to reason about
// completeness, this pin is the one to revisit.
func TestCharacterizationCoverage_367IncompleteDeviceMapReadsAsNoLogging(t *testing.T) {
	entry := watchlist.Entry{ID: "e", Ports: []int{22}}
	def, err := ExpectationDefinitionFor(entry)
	if err != nil {
		t.Fatalf("ExpectationDefinitionFor: %v", err)
	}
	// Simulates watchlistCoverage's rulesByDevice after "edge" (which
	// carries the logging rule for port 22 in the real scenario #367
	// describes) never completed the optional state push and so is
	// silently absent -- only "core", whose rules don't log, appears.
	incomplete := map[string][]ingest.FilterRule{
		"core": {{Chain: "input", Action: "accept", Log: false}},
	}
	if got := def.Coverage(incomplete); got != CoverageNoLogging {
		t.Errorf("Coverage = %v, want %v (today's known-wrong answer -- see #367; the map is incomplete, not exhaustive)", got, CoverageNoLogging)
	}
}
