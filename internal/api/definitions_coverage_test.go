// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is watchlist_coverage_test.go's #407 migration: coverage
// moved from watchlist.Coverage (a function over an entry and a rule
// map) to engine.Definition.Coverage (a method, called once per
// definition inside definitionsCoverage/definitionCoverage --
// definitions.go), and the entry set it answers for moved from
// internal/watchlist.Store into engine.DefinitionsStore as expectation
// definitions. The rule itself -- and every scenario #367 pinned against
// it -- is unchanged; only the plumbing to reach it is. So each test
// below seeds an expectation directly into s.Definitions (mirroring how
// the old tests seeded a bare watchlist.Entry) and reads the answer back
// through GET /api/definitions, which is the one place a caller can see
// it now.

// pushFilterRules puts one device's filter table into the test server's
// RouterState the same way a real push does -- through Apply, not by
// reaching into the store, so the test exercises the same
// kindLocked/FilterRules path definitionsCoverage reads back through.
func pushFilterRules(t *testing.T, s *Server, device string, rules []ingest.FilterRule) {
	t.Helper()
	err := s.RouterState.Apply(device, ingest.Payload{
		Kind:        ingest.KindFilterRule,
		Page:        1,
		Pages:       1,
		FilterRules: rules,
	}, time.Now())
	if err != nil {
		t.Fatalf("Apply(%s): %v", device, err)
	}
}

// mustSeedExpectation upserts a minimal, directly-valid expectation
// definition (ID and Ports only, no HTTP round trip) -- these tests are
// about the coverage answer, not about creation, the same shortcut the
// old s.watchlistCoverage([]watchlist.Entry{entry}) call took.
func mustSeedExpectation(t *testing.T, s *Server, id string) {
	t.Helper()
	if err := s.Definitions.UpsertExpectation(watchlist.Entry{ID: id, Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
}

// TestDefinitionsCoverageRefusesNoLoggingWhenARouterNeverPushed is #367's
// own scenario, end to end: two routers both send syslog, only "core"
// has the optional state push configured, and core's rules all have
// logging switched off. "edge" carries the logging rules and never
// pushed, so its table is silently absent from the map
// engine.Definition.Coverage is handed.
//
// Before the fix, that map alone was enough for the API to answer
// CoverageNoLogging -- "no firewall rule on any router you have
// connected has logging turned on, so no traffic is being reported at
// all" -- for every entry, while edge's events were streaming into the
// live view on the adjacent page. The answer must be Unknown instead,
// and the gap must be visible in the response rather than swallowed.
//
// Closes #367.
func TestDefinitionsCoverageRefusesNoLoggingWhenARouterNeverPushed(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// Both routers are feeding events. newTestServer's registry has
	// "core" configured at 192.168.1.1; "edge" is discovered from its
	// own source address, which is what an unregistered router looks
	// like.
	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)

	// Only core pushed, and nothing in its table logs.
	pushFilterRules(t, s, "core", []ingest.FilterRule{{Chain: "input", Action: "accept"}})
	mustSeedExpectation(t, s, "e1")

	body := getDefinitions(t, ts)
	view, ok := findDefinition(body.Definitions, "e1")
	if !ok {
		t.Fatal("expected e1 to be listed")
	}
	if view.Coverage != engine.CoverageUnknown {
		t.Errorf("coverage = %q, want %q -- edge streams events but never pushed, so \"nothing anywhere logs\" is not a claim this evidence supports (#367)", view.Coverage, engine.CoverageUnknown)
	}
	if body.CoverageEvidence.Complete {
		t.Error("coverageEvidence.Complete = true, want false: a device feeding events pushed no filter table")
	}
	if len(body.CoverageEvidence.MissingDevices) != 1 || body.CoverageEvidence.MissingDevices[0] != "192.168.1.2" {
		t.Errorf("coverageEvidence.MissingDevices = %v, want [192.168.1.2] -- the gap must be surfaced, not only implied by the downgraded answer", body.CoverageEvidence.MissingDevices)
	}
}

// TestDefinitionsCoverageAnswersNoLoggingWhenEveryFeedingRouterPushed is
// the other half of #367's fix: the definite negative is still given
// when it is actually supported. Nothing here is missing from the
// evidence base, so CoverageNoLogging survives -- the fix must not turn
// the feature off, only stop it claiming more than it knows.
func TestDefinitionsCoverageAnswersNoLoggingWhenEveryFeedingRouterPushed(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now) // "core", configured
	pushFilterRules(t, s, "core", []ingest.FilterRule{{Chain: "input", Action: "accept"}})
	mustSeedExpectation(t, s, "e1")

	body := getDefinitions(t, ts)
	if !body.CoverageEvidence.Complete {
		t.Fatalf("coverageEvidence.Complete = false (missing %v), want true: the only device feeding events pushed its table", body.CoverageEvidence.MissingDevices)
	}
	view, ok := findDefinition(body.Definitions, "e1")
	if !ok {
		t.Fatal("expected e1 to be listed")
	}
	if view.Coverage != engine.CoverageNoLogging {
		t.Errorf("coverage = %q, want %q", view.Coverage, engine.CoverageNoLogging)
	}
}

// TestDefinitionsCoverageOutOfScopeAlsoNeedsCompleteEvidence pins the
// second definite negative the same way. "Every logging rule was read,
// and each provably excludes this entry" is exactly as unsupportable as
// "nothing logs" when a router's rules were never read at all.
func TestDefinitionsCoverageOutOfScopeAlsoNeedsCompleteEvidence(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)
	pushFilterRules(t, s, "core", []ingest.FilterRule{
		{Chain: "input", Action: "accept", Log: true, DstPort: "80"},
	})
	mustSeedExpectation(t, s, "e1")

	body := getDefinitions(t, ts)
	view, ok := findDefinition(body.Definitions, "e1")
	if !ok {
		t.Fatal("expected e1 to be listed")
	}
	if view.Coverage != engine.CoverageUnknown {
		t.Errorf("coverage = %q, want %q -- out-of-scope is a definite negative too, and edge's rules were never read", view.Coverage, engine.CoverageUnknown)
	}
}

// TestDefinitionsCoverageOKSurvivesIncompleteEvidence pins the asymmetry
// the fix deliberately preserves: a positive answer needs one router
// demonstrably logging the right traffic, and stays true however many
// other routers went unread.
func TestDefinitionsCoverageOKSurvivesIncompleteEvidence(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)
	pushFilterRules(t, s, "core", []ingest.FilterRule{
		{Chain: "input", Action: "accept", Log: true, DstPort: "22"},
	})
	mustSeedExpectation(t, s, "e1")

	body := getDefinitions(t, ts)
	if body.CoverageEvidence.Complete {
		t.Fatal("expected the evidence base to be incomplete in this fixture")
	}
	view, ok := findDefinition(body.Definitions, "e1")
	if !ok {
		t.Fatal("expected e1 to be listed")
	}
	if view.Coverage != engine.CoverageOK {
		t.Errorf("coverage = %q, want %q -- a demonstrated positive is not weakened by unread routers", view.Coverage, engine.CoverageOK)
	}
}

// TestDefinitionsCoverageIgnoresDevicesThatHaveFedNothing pins the other
// direction of the completeness rule: a device declared in config.yaml
// that has never carried an event is not evidence of a gap, because
// nothing it could log is arriving to be missed.
func TestDefinitionsCoverageIgnoresDevicesThatHaveFedNothing(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// "core" is configured but has never been Resolve()d, so its
	// EventCount is zero. A second router pushed a non-logging table.
	pushFilterRules(t, s, "edge", []ingest.FilterRule{{Chain: "input", Action: "accept"}})
	mustSeedExpectation(t, s, "e1")

	body := getDefinitions(t, ts)
	if !body.CoverageEvidence.Complete {
		t.Errorf("coverageEvidence.Complete = false (missing %v), want true: a silent configured device is not a coverage gap", body.CoverageEvidence.MissingDevices)
	}
}
