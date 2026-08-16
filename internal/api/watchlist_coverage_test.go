// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// pushFilterRules puts one device's filter table into the test server's
// RouterState the same way a real push does -- through Apply, not by
// reaching into the store, so the test exercises the same
// kindLocked/FilterRules path watchlistCoverage reads back through.
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

// TestWatchlistCoverageRefusesNoLoggingWhenARouterNeverPushed is #367's
// own scenario, end to end: two routers both send syslog, only "core"
// has the optional state push configured, and core's rules all have
// logging switched off. "edge" carries the logging rules and never
// pushed, so its table is silently absent from the map
// watchlist.Coverage is handed.
//
// Before the fix, that map alone was enough for the API to answer
// CoverageNoLogging -- "no firewall rule on any router you have
// connected has logging turned on, so no traffic is being reported at
// all" -- for every entry, while edge's events were streaming into the
// live view on the adjacent page. The answer must be Unknown instead,
// and the gap must be visible in the response rather than swallowed.
//
// Closes #367.
func TestWatchlistCoverageRefusesNoLoggingWhenARouterNeverPushed(t *testing.T) {
	s, _ := newTestServer(t)

	// Both routers are feeding events. newTestServer's registry has
	// "core" configured at 192.168.1.1; "edge" is discovered from its
	// own source address, which is what an unregistered router looks
	// like.
	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)

	// Only core pushed, and nothing in its table logs.
	pushFilterRules(t, s, "core", []ingest.FilterRule{{Chain: "input", Action: "accept"}})

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	coverage, evidence := s.watchlistCoverage([]watchlist.Entry{entry})

	if got := coverage["e1"]; got != watchlist.CoverageUnknown {
		t.Errorf("coverage = %q, want %q -- edge streams events but never pushed, so \"nothing anywhere logs\" is not a claim this evidence supports (#367)", got, watchlist.CoverageUnknown)
	}
	if evidence.Complete {
		t.Error("coverageEvidence.Complete = true, want false: a device feeding events pushed no filter table")
	}
	if len(evidence.MissingDevices) != 1 || evidence.MissingDevices[0] != "192.168.1.2" {
		t.Errorf("coverageEvidence.MissingDevices = %v, want [192.168.1.2] -- the gap must be surfaced, not only implied by the downgraded answer", evidence.MissingDevices)
	}
}

// TestWatchlistCoverageAnswersNoLoggingWhenEveryFeedingRouterPushed is
// the other half of #367's fix: the definite negative is still given
// when it is actually supported. Nothing here is missing from the
// evidence base, so CoverageNoLogging survives -- the fix must not turn
// the feature off, only stop it claiming more than it knows.
func TestWatchlistCoverageAnswersNoLoggingWhenEveryFeedingRouterPushed(t *testing.T) {
	s, _ := newTestServer(t)

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now) // "core", configured
	pushFilterRules(t, s, "core", []ingest.FilterRule{{Chain: "input", Action: "accept"}})

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	coverage, evidence := s.watchlistCoverage([]watchlist.Entry{entry})

	if !evidence.Complete {
		t.Fatalf("coverageEvidence.Complete = false (missing %v), want true: the only device feeding events pushed its table", evidence.MissingDevices)
	}
	if got := coverage["e1"]; got != watchlist.CoverageNoLogging {
		t.Errorf("coverage = %q, want %q", got, watchlist.CoverageNoLogging)
	}
}

// TestWatchlistCoverageOutOfScopeAlsoNeedsCompleteEvidence pins the
// second definite negative the same way. "Every logging rule was read,
// and each provably excludes this entry" is exactly as unsupportable as
// "nothing logs" when a router's rules were never read at all.
func TestWatchlistCoverageOutOfScopeAlsoNeedsCompleteEvidence(t *testing.T) {
	s, _ := newTestServer(t)

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)
	pushFilterRules(t, s, "core", []ingest.FilterRule{
		{Chain: "input", Action: "accept", Log: true, DstPort: "80"},
	})

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	coverage, _ := s.watchlistCoverage([]watchlist.Entry{entry})
	if got := coverage["e1"]; got != watchlist.CoverageUnknown {
		t.Errorf("coverage = %q, want %q -- out-of-scope is a definite negative too, and edge's rules were never read", got, watchlist.CoverageUnknown)
	}
}

// TestWatchlistCoverageOKSurvivesIncompleteEvidence pins the asymmetry
// the fix deliberately preserves: a positive answer needs one router
// demonstrably logging the right traffic, and stays true however many
// other routers went unread.
func TestWatchlistCoverageOKSurvivesIncompleteEvidence(t *testing.T) {
	s, _ := newTestServer(t)

	now := time.Now()
	s.Devices.Resolve("192.168.1.1", now)
	s.Devices.Resolve("192.168.1.2", now)
	pushFilterRules(t, s, "core", []ingest.FilterRule{
		{Chain: "input", Action: "accept", Log: true, DstPort: "22"},
	})

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	coverage, evidence := s.watchlistCoverage([]watchlist.Entry{entry})
	if evidence.Complete {
		t.Fatal("expected the evidence base to be incomplete in this fixture")
	}
	if got := coverage["e1"]; got != watchlist.CoverageOK {
		t.Errorf("coverage = %q, want %q -- a demonstrated positive is not weakened by unread routers", got, watchlist.CoverageOK)
	}
}

// TestWatchlistCoverageIgnoresDevicesThatHaveFedNothing pins the other
// direction of the completeness rule: a device declared in config.yaml
// that has never carried an event is not evidence of a gap, because
// nothing it could log is arriving to be missed.
func TestWatchlistCoverageIgnoresDevicesThatHaveFedNothing(t *testing.T) {
	s, _ := newTestServer(t)

	// "core" is configured but has never been Resolve()d, so its
	// EventCount is zero. A second router pushed a non-logging table.
	pushFilterRules(t, s, "edge", []ingest.FilterRule{{Chain: "input", Action: "accept"}})

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	_, evidence := s.watchlistCoverage([]watchlist.Entry{entry})
	if !evidence.Complete {
		t.Errorf("coverageEvidence.Complete = false (missing %v), want true: a silent configured device is not a coverage gap", evidence.MissingDevices)
	}
}
