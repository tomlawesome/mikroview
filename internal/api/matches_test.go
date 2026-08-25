// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file covers matches.go: GET /api/matches, renamed by #407 from
// GET /api/watchlist/matches when the watchlist noun was retired -- the
// one thing on that prefix the definitions surface does not replace
// (handleMatchesQuery's own doc comment). Behaviour is unchanged from
// the tests this migrates; only the path moved.

func TestHandleMatchesQueryRequiresIdentity(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/matches")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 with no mac/ip query parameter, got %d", resp.StatusCode)
	}
}

func TestHandleMatchesQueryReturnsRecordedMatches(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if err := s.MatchLog.Append("e1",
		matchlog.Tuple{Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 8883},
		store.Event{Raw: "test"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/matches?mac=aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Matches []matchlog.Record `json:"matches"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Matches) != 1 || body.Matches[0].EntryID != "e1" {
		t.Fatalf("unexpected matches: %+v", body.Matches)
	}
}

func TestHandleMatchesQueryUnavailableWhenMatchLogNil(t *testing.T) {
	s, _ := newTestServer(t)
	s.MatchLog = nil
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/matches?mac=aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when the match log is unavailable, got %d", resp.StatusCode)
	}
}

func TestHandleMatchesQueryInvalidTimeParam(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/matches?mac=aa:bb:cc:dd:ee:ff&since=not-a-time")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an unparseable since, got %d", resp.StatusCode)
	}
}

// --- Bearer token boundary (mirrors tokens_test.go's own shape) --------

// A read-only bearer token must reach the match query -- the whole
// reason it lives on the read-only tier at all (birdcage-style
// correlation).
func TestBearerTokenCanQueryMatches(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "birdcage")

	resp := bearerGet(t, ts.URL+"/api/matches?mac=aa:bb:cc:dd:ee:ff", raw)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected a valid bearer token to reach the match query, got %d", resp.StatusCode)
	}
}

// --- entries=all: the bounded, all-entries mode (#586) -----------------

// seedMatches writes one match per (entry, source) pair a minute apart,
// oldest first, so the newest-first ordering below has something to be
// wrong about.
func seedMatches(t *testing.T, s *Server, rows []struct {
	entryID string
	source  matchlog.Identity
	port    int
}) {
	t.Helper()
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	for i, r := range rows {
		tuple := matchlog.Tuple{Source: r.source, DestIP: "10.0.0.9", Port: r.port}
		if err := s.MatchLog.Append(r.entryID, tuple, store.Event{Raw: "x"}, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("Append(%s): %v", r.entryID, err)
		}
	}
}

// threeEntriesFixture: three entries, three devices, one match each. The
// third is IP-only -- the shape a non-inverted entry with an empty
// Source records under, whose matches no per-identity request finds
// unless the caller already guesses the IP.
var threeEntriesFixture = []struct {
	entryID string
	source  matchlog.Identity
	port    int
}{
	{"e-a", matchlog.Identity{MAC: "aa:bb:cc:dd:ee:01"}, 1},
	{"e-b", matchlog.Identity{MAC: "aa:bb:cc:dd:ee:02"}, 2},
	{"e-c", matchlog.Identity{IP: "192.0.2.77"}, 3},
}

func getMatches(t *testing.T, ts *httptest.Server, query string) (int, []matchlog.Record) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/matches" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Matches []matchlog.Record `json:"matches"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding %s: %v", query, err)
		}
	}
	return resp.StatusCode, body.Matches
}

// The happy path, the bound, and an empty result, over one route.
func TestHandleMatchesQueryAllEntries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seed      bool
		query     string
		wantPorts []int
		why       string
	}{
		{
			name: "every entry newest first", seed: true, query: "?entries=all",
			wantPorts: []int{3, 2, 1},
			why:       "one merged list across all three entries, newest LastSeen first -- what the Matches tab reads",
		},
		{
			name: "reaches an entry no identity query would find", seed: true, query: "?entries=all&limit=1",
			wantPorts: []int{3},
			why:       "e-c's device is IP-only; without this mode its match is recorded but unreachable",
		},
		{
			name: "limit bounds the response", seed: true, query: "?entries=all&limit=2",
			wantPorts: []int{3, 2},
			why:       "the bound is the whole point: a limit below what is available truncates to the newest",
		},
		{
			name: "an absurd limit is clamped, not honoured", seed: true, query: "?entries=all&limit=999999999",
			wantPorts: []int{3, 2, 1},
			why:       "matchlog clamps to MaxLimit rather than trusting the caller; three records is all there is",
		},
		{
			name: "an empty log answers with an empty list", seed: false, query: "?entries=all",
			wantPorts: []int{},
			why:       "nothing recorded is a real answer here, unlike the per-identity path's empty identity",
		},
		{
			name: "a window nothing falls in answers with an empty list", seed: true,
			query:     "?entries=all&since=2030-01-01T00:00:00Z",
			wantPorts: []int{},
			why:       "since/until behave as they do on the per-identity path",
		},
		{
			name: "the window still filters", seed: true,
			query:     "?entries=all&until=2026-08-24T12:02:00Z",
			wantPorts: []int{2, 1},
			why:       "until is exclusive, so the record at exactly 12:02 is out",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			if tc.seed {
				seedMatches(t, s, threeEntriesFixture)
			}
			ts := httptest.NewServer(asAdmin(s.mux()))
			defer ts.Close()

			status, matches := getMatches(t, ts, tc.query)
			if status != http.StatusOK {
				t.Fatalf("GET /api/matches%s = %d, want 200 -- %s", tc.query, status, tc.why)
			}
			got := make([]int, 0, len(matches))
			for _, m := range matches {
				got = append(got, m.Tuple.Port)
			}
			if len(got) != len(tc.wantPorts) {
				t.Fatalf("GET /api/matches%s returned ports %v, want %v -- %s", tc.query, got, tc.wantPorts, tc.why)
			}
			for i := range got {
				if got[i] != tc.wantPorts[i] {
					t.Errorf("GET /api/matches%s returned ports %v, want %v -- %s", tc.query, got, tc.wantPorts, tc.why)
					break
				}
			}
		})
	}
}

// The negative cases. Every one of these is a caller who believes they
// asked something narrower than they did, or who omitted an identity by
// accident -- the failure matchlog.RecentQuery's doc comment is built
// around. None of them may answer 200.
func TestHandleMatchesQueryAllEntriesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  int
		why   string
	}{
		{"no identity and no mode", "", http.StatusBadRequest,
			"unchanged: an absent mac/ip is a caller who does not know the device, never 'every device'"},
		{"an empty mac is still no identity", "?mac=", http.StatusBadRequest,
			"a query parameter that arrived empty must not fall through to the all-entries mode"},
		{"entries=all with a mac", "?entries=all&mac=aa:bb:cc:dd:ee:01", http.StatusBadRequest,
			"refused rather than resolved one way: a caller who believes they filtered and did not is the misreading that matters"},
		{"entries=all with an ip", "?entries=all&ip=192.0.2.77", http.StatusBadRequest,
			"same reason as the mac case above"},
		{"entries with an unknown value", "?entries=everything", http.StatusBadRequest,
			"present-and-unparseable is refused, per badQueryParam's convention -- it must not silently mean the identity path"},
		{"entries=ALL is not entries=all", "?entries=ALL", http.StatusBadRequest,
			"the opt-in is an exact value; a near miss is a caller who did not opt in"},
		{"a malformed since is still refused in this mode", "?entries=all&since=not-a-time", http.StatusBadRequest,
			"a window that failed to parse must not answer 200 with an unwindowed result"},
		{"a malformed until is still refused in this mode", "?entries=all&until=not-a-time", http.StatusBadRequest,
			"same"},
		{"a non-numeric limit is still refused in this mode", "?entries=all&limit=lots", http.StatusBadRequest,
			"the bound is the safety property; a limit that failed to parse is not a bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			seedMatches(t, s, threeEntriesFixture)
			ts := httptest.NewServer(asAdmin(s.mux()))
			defer ts.Close()

			status, _ := getMatches(t, ts, tc.query)
			if status != tc.want {
				t.Errorf("GET /api/matches%s = %d, want %d -- %s", tc.query, status, tc.want, tc.why)
			}
		})
	}
}

func TestHandleMatchesQueryAllEntriesUnavailableWhenMatchLogNil(t *testing.T) {
	s, _ := newTestServer(t)
	s.MatchLog = nil
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if status, _ := getMatches(t, ts, "?entries=all"); status != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when the match log is unavailable, got %d", status)
	}
}

// --- Authorization: the mode carries the same gate, and no more -------

// An unauthenticated caller is refused, exactly as on the per-identity
// path. The authzMatrix guard asserts this for the route as a whole;
// this pins it for the mode, since "entries=all" is the parameter that
// widened what the route can return.
func TestAllEntriesModeIsNotReachableWithoutAuth(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	setUpAdmin(t, ts) // an account now exists, so auth is active

	resp, err := http.Get(ts.URL + "/api/matches?entries=all")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an anonymous caller got %d on the all-entries mode, want 401", resp.StatusCode)
	}
}

// A read-only bearer token reaches it -- the same tier the per-identity
// query already carried, which is the decision recorded on this route's
// authzMatrix row. Asserted rather than assumed: it is the widest thing
// this change makes reachable with the lowest-privilege credential
// mikroview issues, so it should fail loudly if someone narrows or
// widens it without touching that row.
func TestBearerTokenCanQueryAllEntries(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	raw := createToken(t, ts, admin, "matches-tab")

	resp := bearerGet(t, ts.URL+"/api/matches?entries=all", raw)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("a read-only bearer token got %d on the all-entries mode, want 200", resp.StatusCode)
	}
}

// A bogus bearer token reaches nothing, mode or no mode.
func TestInvalidBearerTokenCannotQueryAllEntries(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	setUpAdmin(t, ts)

	resp := bearerGet(t, ts.URL+"/api/matches?entries=all", "not-a-real-token")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an invalid bearer token got %d on the all-entries mode, want 401", resp.StatusCode)
	}
}
