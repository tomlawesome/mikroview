// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/suggest"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// getExpectation is a small helper mirroring the old
// s.Watchlist.Get(id): reads back one expectation definition as the
// entry it converts to, failing the test on a decode error rather than
// returning it -- every call site here already knows the id should
// exist.
func getExpectation(t *testing.T, s *Server, id string) (watchlist.Entry, bool) {
	t.Helper()
	e, ok, err := s.Definitions.GetExpectation(id)
	if err != nil {
		t.Fatal(err)
	}
	return e, ok
}

// seedCandidate syncs one candidate directly into s.Suggest -- bypassing
// generation from routerState, which the reset-specific tests below
// exercise for real.
func seedCandidate(t *testing.T, s *Server, c suggest.Candidate) {
	t.Helper()
	if err := s.Suggest.Sync([]suggest.Candidate{c}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleSuggestionsListFiltersByStatus(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	seedCandidate(t, s, suggest.Candidate{ID: "device\x00r1\x00aa", Kind: suggest.KindDevice, Name: "camera", RouterDevice: "r1"})
	seedCandidate(t, s, suggest.Candidate{ID: "port\x00r1\x00x", Kind: suggest.KindPort, Name: "port 22", RouterDevice: "r1", Ports: []int{22}})
	if err := s.Suggest.Hide("port\x00r1\x00x"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/api/suggestions?status=hide")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Candidates []suggest.Candidate `json:"candidates"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Candidates) != 1 || body.Candidates[0].Name != "port 22" {
		t.Errorf("expected exactly the one hidden candidate, got %+v", body.Candidates)
	}
}

func TestHandleSuggestionsAcceptDeviceCreatesInvertedEntry(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	id := "device\x00r1\x00aa:bb:cc:dd:ee:ff"
	seedCandidate(t, s, suggest.Candidate{
		ID: id, Kind: suggest.KindDevice, Name: "camera", RouterDevice: "r1",
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.10"},
	})

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+strings.ReplaceAll(id, "\x00", "%00")+"/accept", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	updated, ok := s.Suggest.Get(id)
	if !ok || updated.Status != suggest.StatusOn || updated.EntryID == "" {
		t.Fatalf("candidate not accepted correctly: %+v (ok=%v)", updated, ok)
	}
	entry, ok := getExpectation(t, s, updated.EntryID)
	if !ok {
		t.Fatal("no watchlist entry was created")
	}
	if !entry.Invert || !entry.Observing || len(entry.Permitted) != 0 {
		t.Errorf("expected an inverted entry starting Observing with an empty Permitted set, got %+v", entry)
	}
	if entry.Source.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("entry.Source = %+v, want the candidate's device", entry.Source)
	}
}

func TestHandleSuggestionsAcceptPortCreatesNonInvertedEntry(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	id := "port\x00r1\x00chain\x00drop\x00tcp\x003389\x00"
	seedCandidate(t, s, suggest.Candidate{
		ID: id, Kind: suggest.KindPort, Name: "port 3389", RouterDevice: "r1", Ports: []int{3389},
	})

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+strings.ReplaceAll(id, "\x00", "%00")+"/accept", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	updated, _ := s.Suggest.Get(id)
	entry, ok := getExpectation(t, s, updated.EntryID)
	if !ok {
		t.Fatal("no watchlist entry was created")
	}
	if entry.Invert || len(entry.Ports) != 1 || entry.Ports[0] != 3389 {
		t.Errorf("expected a non-inverted entry watching port 3389, got %+v", entry)
	}
}

func TestHandleSuggestionsAcceptRejectsNonOff(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	id := "port\x00r1\x00x"
	seedCandidate(t, s, suggest.Candidate{ID: id, Kind: suggest.KindPort, Name: "p", RouterDevice: "r1", Ports: []int{22}})
	if err := s.Suggest.Hide(id); err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+strings.ReplaceAll(id, "\x00", "%00")+"/accept", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for accepting a hidden candidate, got %d", resp.StatusCode)
	}
	if _, ok := getExpectation(t, s, id); ok {
		t.Error("no watchlist entry should have been created")
	}
}

func TestHandleSuggestionsAcceptUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/never-existed/accept", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleSuggestionsHideAndUnhide(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	id := "port\x00r1\x00x"
	seedCandidate(t, s, suggest.Candidate{ID: id, Kind: suggest.KindPort, Name: "p", RouterDevice: "r1", Ports: []int{22}})

	escaped := strings.ReplaceAll(id, "\x00", "%00")
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+escaped+"/hide", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got, _ := s.Suggest.Get(id); got.Status != suggest.StatusHide {
		t.Errorf("expected Status=hide, got %q", got.Status)
	}

	resp2 := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+escaped+"/unhide", nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
	if got, _ := s.Suggest.Get(id); got.Status != suggest.StatusOff {
		t.Errorf("expected Status=off after unhide, got %q", got.Status)
	}
}

func TestHandleSuggestionsHideUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/never-existed/hide", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleSuggestionsResetRequiresConfirm(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if err := s.Definitions.UpsertExpectation(watchlistEntryForTest("e1")); err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/reset", suggestResetRequest{Confirm: false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 without confirm:true, got %d", resp.StatusCode)
	}
	if _, ok := getExpectation(t, s, "e1"); !ok {
		t.Error("the watchlist must not be touched when confirm is false")
	}
}

func TestHandleSuggestionsResetWipesWatchlistAndRegeneratesCandidates(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	if err := s.Definitions.UpsertExpectation(watchlistEntryForTest("e1")); err != nil {
		t.Fatal(err)
	}
	seedCandidate(t, s, suggest.Candidate{ID: "port\x00r1\x00x", Kind: suggest.KindPort, Name: "p", RouterDevice: "r1", Ports: []int{22}})
	if err := s.Suggest.Accept("port\x00r1\x00x", "e1"); err != nil {
		t.Fatal(err)
	}

	p, err := ingest.DecodePayload(strings.NewReader(
		`{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"camera","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.10"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("r1", p, time.Now()); err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/reset", suggestResetRequest{Confirm: true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries, err := s.Definitions.ListExpectations()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected every expectation definition to be wiped, got %+v", entries)
	}
	if _, ok := s.Suggest.Get("port\x00r1\x00x"); ok {
		t.Error("expected the old accepted candidate to be gone, not just reset to off")
	}

	var body struct {
		Candidates []suggest.Candidate `json:"candidates"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Candidates) != 1 || body.Candidates[0].Status != suggest.StatusOff || body.Candidates[0].Name != "camera" {
		t.Fatalf("expected one freshly regenerated Off candidate from routerState, got %+v", body.Candidates)
	}
}

func TestHandleDefinitionsDeleteHidesOriginatingSuggestion(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	id := "device\x00r1\x00aa:bb:cc:dd:ee:ff"
	seedCandidate(t, s, suggest.Candidate{
		ID: id, Kind: suggest.KindDevice, Name: "camera", RouterDevice: "r1",
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	})
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/suggestions/"+strings.ReplaceAll(id, "\x00", "%00")+"/accept", nil)
	var acceptBody struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	json.NewDecoder(resp.Body).Decode(&acceptBody)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/definitions/"+acceptBody.Entry.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	delResp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", delResp.StatusCode)
	}

	got, ok := s.Suggest.Get(id)
	if !ok {
		t.Fatal("candidate should still exist, just re-hidden")
	}
	if got.Status != suggest.StatusHide {
		t.Errorf("expected the originating candidate to move to Hide, got %q", got.Status)
	}
	if got.EntryID != "" {
		t.Errorf("expected EntryID cleared once the entry it pointed to is gone, got %q", got.EntryID)
	}
}

// watchlistEntryForTest builds a minimal valid, directly-Upsert-able
// entry for tests that don't care about entry content, only that one
// exists.
func watchlistEntryForTest(id string) watchlist.Entry {
	return watchlist.Entry{ID: id, Ports: []int{22}}
}
