// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestTestHookRoutesAreAbsentWithoutTheFlag is the half of the switch that
// matters on a real deployment: with MV_TEST_HOOKS unset, the route is not
// registered, so the mux answers 404 -- "no such path", the same answer a
// typo gets -- rather than 403, which would confirm the endpoint exists
// and is merely closed today.
func TestTestHookRoutesAreAbsentWithoutTheFlag(t *testing.T) {
	s, _ := newTestServer(t)
	if s.TestHooks {
		t.Fatal("TestHooks defaults to on -- it must be off unless main saw MV_TEST_HOOKS=1")
	}
	for _, r := range s.routes() {
		if r.path == "/api/test/clock" {
			t.Fatalf("%s %s is in the route table with the flag off", r.method, r.path)
		}
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	for _, path := range []string{"/api/test/clock"} {
		resp := postJSON(t, &http.Client{}, ts.URL+path, map[string]any{})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s with the flag off = %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestTestClockAdvanceMovesTheDefinitionsReadPath is #1063 end to end at
// the unit level: a watch whose window is still ahead reports an intact
// ring, and the same watch reports a broken one once the clock has been
// moved past the window's close -- with no real time having passed.
//
// The window is computed from the wall clock rather than from a fixed
// instant because the honesty check the fill rests on compares each
// occurrence against when the definitions store opened (watchingSince,
// a real time.Now inside engine). A window pinned to a literal date
// would sit before that instant and be recorded "not observed", which is
// the correct behaviour and would prove nothing about the clock.
func TestTestClockAdvanceMovesTheDefinitionsReadPath(t *testing.T) {
	s, _ := newTestServer(t)
	s.TestHooks = true
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	const port = 47001
	// A logging rule covering the watch's port, so the empty window is
	// recorded as an observed "empty" night rather than "not observed" --
	// a watch nothing logs is never judged on nightly presence at all.
	pushFilterRules(t, s, "core", []ingest.FilterRule{
		{Ordinal: 0, Chain: "forward", Action: "drop", Log: true, DstPort: "47001"},
	})

	// Minutes past UTC midnight, nudged forward if the two-minute window
	// below would otherwise wrap onto tomorrow -- the same arithmetic the
	// live scenario does, and the same reason.
	now := time.Now().UTC()
	mins := now.Hour()*60 + now.Minute()
	if mins+2 >= 24*60 {
		s.Now = func() time.Time { return time.Now().Add(10 * time.Minute) }
		now = now.Add(10 * time.Minute)
		mins = now.Hour()*60 + now.Minute()
	}
	window := watchlist.Window{Start: watchlist.Clock(mins + 1), End: watchlist.Clock(mins + 2)}

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/definitions", createDefinitionRequest{
		Name:        "test-clock sentinel",
		Expectation: &expectationRequest{Ports: []int{port}, Window: &window},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the watch = %d, want 201", resp.StatusCode)
	}
	id := mustDecodeDefinition(t, resp).ID

	if got := ringFor(t, ts.URL, id); got.Broken {
		t.Fatalf("the ring is broken before the window has even opened: %+v", got)
	}

	// Past the close, in one call and no elapsed time.
	advanceTestClock(t, ts.URL, "5m")

	got := ringFor(t, ts.URL, id)
	if !got.Broken {
		t.Errorf("after advancing the clock past the window's close the ring is still intact: %+v -- "+
			"the definitions read path is not taking its time from Server.now", got)
	}
	if got.Reason != watchlist.RingNoMatchInWindow {
		t.Errorf("ring reason = %q, want %q", got.Reason, watchlist.RingNoMatchInWindow)
	}
}

// TestTestClockRefusesToGoBackwards pins the one direction the clock does
// not move. A night is keyed by the instant its window opened and written
// once, so rewinding would leave the store holding nights from a future
// that has been un-happened -- a state no deployment can reach and none
// of this code is written to survive.
func TestTestClockRefusesToGoBackwards(t *testing.T) {
	s, _ := newTestServer(t)
	s.TestHooks = true
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	for _, body := range []string{`-1m`, `not a duration`} {
		resp := postJSON(t, &http.Client{}, ts.URL+"/api/test/clock", testClockRequest{Advance: body})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("advance %q = %d, want 400", body, resp.StatusCode)
		}
	}
	if off := s.testClockOffset.Load(); off != 0 {
		t.Errorf("a refused advance still moved the clock by %v", time.Duration(off))
	}
}

// advanceTestClock moves the server's clock and returns nothing but a
// failed test if it refused.
func advanceTestClock(t *testing.T, base, advance string) {
	t.Helper()
	resp := postJSON(t, &http.Client{}, base+"/api/test/clock", testClockRequest{Advance: advance})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/test/clock advance=%s = %d, want 200", advance, resp.StatusCode)
	}
}

// ringFor reads one definition's recorded ring back through the list
// endpoint -- the same read path the live scenario polls, so the test
// exercises the fill rather than reaching into the store.
func ringFor(t *testing.T, base, id string) watchlist.Ring {
	t.Helper()
	resp, err := http.Get(base + "/api/definitions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Definitions []struct {
			ID          string `json:"id"`
			Expectation *struct {
				Ring watchlist.Ring `json:"ring"`
			} `json:"expectation"`
		} `json:"definitions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, d := range body.Definitions {
		if d.ID == id {
			if d.Expectation == nil {
				t.Fatalf("definition %s came back without an expectation", id)
			}
			return d.Expectation.Ring
		}
	}
	t.Fatalf("definition %s is not in the list response", id)
	return watchlist.Ring{}
}
