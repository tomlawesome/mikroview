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
