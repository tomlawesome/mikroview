// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestTestHookRoutesAreAbsentWithoutTheFlag is the half of the switch that
// matters on a real deployment: with MV_TEST_HOOKS unset, neither route is
// registered, so the mux answers 404 -- "no such path", the same answer a
// typo gets -- rather than 403, which would confirm the endpoint exists
// and is merely closed today.
func TestTestHookRoutesAreAbsentWithoutTheFlag(t *testing.T) {
	s, _ := newTestServer(t)
	if s.TestHooks {
		t.Fatal("TestHooks defaults to on -- it must be off unless main saw MV_TEST_HOOKS=1")
	}
	for _, r := range s.routes() {
		if r.path == "/api/test/clock" || r.path == "/api/test/reset" {
			t.Fatalf("%s %s is in the route table with the flag off", r.method, r.path)
		}
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	for _, path := range []string{"/api/test/clock", "/api/test/reset"} {
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

// TestTestResetClearsWhatItSaysAndKeepsTheRest is #1064's contract, which
// is as much about what survives as about what goes: a reset that signed
// the harness out, forgot the device its events arrive from, or reopened
// the setup wizard would be useless between scenarios however thoroughly
// it emptied the ring.
func TestTestResetClearsWhatItSaysAndKeepsTheRest(t *testing.T) {
	s, st := newTestServer(t)
	s.TestHooks = true
	s.Reseed = func() error {
		return engine.SeedShippedDefinitions(s.Definitions, nil, engine.DefaultShippedDefaults())
	}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// Traffic, a flag with an expectation recorded against it, a pushed
	// table, an operator watch and a match: one of everything the reset
	// claims to clear.
	for i := 0; i < 5; i++ {
		st.Insert(store.Event{SourceIP: "10.0.0.5", Action: store.ActionDrop, RuleLabel: "residue"})
	}
	s.Flags.Add(flags.TypePortScan, "10.0.0.5", "left behind by a sibling scenario", time.Now())
	s.Flags.Exclude(flags.TypePortScan, "10.0.0.5")
	pushFilterRules(t, s, "core", []ingest.FilterRule{{Ordinal: 0, Chain: "forward", Action: "drop", Log: true}})
	if err := s.Definitions.UpsertExpectation(watchlist.Entry{ID: "residue-watch", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MatchLog.Append("residue-watch", matchlog.Tuple{
		Source: matchlog.Identity{IP: "10.0.0.5"}, DestIP: "10.0.0.9", Port: 22,
	}, store.Event{SourceIP: "10.0.0.5"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Identity, which must all still be here afterwards.
	keeper, err := s.Auth.CreateUser("keeper", "password123", auth.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rawToken, _, err := s.Tokens.Create("kept token", auth.TokenKindIngest, "core", keeper, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	devicesBefore := len(s.Devices.List())

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/test/reset", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/test/reset = %d, want 200", resp.StatusCode)
	}

	if got := st.Stats().Total; got != 0 {
		t.Errorf("the event store still counts %d events", got)
	}
	if got := len(st.Query(store.Query{Limit: 100}).Events); got != 0 {
		t.Errorf("the ring still holds %d events", got)
	}
	if got := len(s.Flags.List()); got != 0 {
		t.Errorf("%d flags survived", got)
	}
	if got := len(s.Flags.ListExclusions()); got != 0 {
		t.Errorf("%d exclusions/expectations survived", got)
	}
	if _, _, ok := s.RouterState.FilterRules("core"); ok {
		t.Error("the pushed filter table survived")
	}
	if _, ok, _ := s.Definitions.GetExpectation("residue-watch"); ok {
		t.Error("the operator watch survived")
	}
	if got := s.MatchLog.Stats().Count; got != 0 {
		t.Errorf("%d matches survived", got)
	}

	// Re-seeded, not left empty: an instance evaluating nothing at all
	// would fail the next scenario in a way that looks like anything but
	// a reset.
	if got := len(s.Definitions.List()); got == 0 {
		t.Error("the shipped catalogue was not laid back down after the reset")
	}

	if _, ok := s.Auth.Get(keeper.ID); !ok {
		t.Error("the account was deleted -- accounts are identity and must survive")
	}
	if _, ok := s.Tokens.Authenticate(rawToken, auth.TokenKindIngest, time.Now()); !ok {
		t.Error("the ingest token stopped working -- live-env.sh issues one before any scenario runs")
	}
	if got := len(s.Devices.List()); got != devicesBefore {
		t.Errorf("the device registry went from %d to %d -- devices are identity here", devicesBefore, got)
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
