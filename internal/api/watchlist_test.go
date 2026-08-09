// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

func TestHandleWatchlistEntriesCreateGeneratesAnID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := watchlistEntryRequest{Name: "SSH watch", Ports: []int{22}}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var got watchlist.Entry
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Error("expected a server-generated ID, got empty")
	}
	if got.Name != "SSH watch" || len(got.Ports) != 1 || got.Ports[0] != 22 {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	// Actually persisted, not just echoed back.
	stored, ok := s.Watchlist.Get(got.ID)
	if !ok {
		t.Fatal("entry was not persisted to the store")
	}
	if stored.Name != "SSH watch" {
		t.Errorf("stored entry = %+v, want Name=SSH watch", stored)
	}
}

// A new inverted entry must start Observing -- #243 section 5's rule,
// not an operator-settable request field.
func TestHandleWatchlistEntriesCreateInvertedStartsObserving(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req := watchlistEntryRequest{
		Name: "IoT camera", Invert: true,
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	}
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var got watchlist.Entry
	json.NewDecoder(resp.Body).Decode(&got)
	if !got.Observing {
		t.Error("a new inverted entry must start Observing")
	}
}

func TestHandleWatchlistEntriesCreateRejectsInvalidEntry(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// No ports, not inverted -- watchlist.ErrNoPorts.
	resp := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "broken"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an entry with no ports, got %d", resp.StatusCode)
	}
}

func TestHandleWatchlistEntriesListReturnsCreatedEntries(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "e1", Ports: []int{22}}).Body.Close()
	postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "e2", Ports: []int{443}}).Body.Close()

	resp, err := http.Get(ts.URL + "/api/watchlist/entries")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Entries []watchlist.Entry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(body.Entries))
	}
}

func TestHandleWatchlistEntriesUpdate(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "before", Ports: []int{22}})
	var entry watchlist.Entry
	json.NewDecoder(created.Body).Decode(&entry)
	created.Body.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries/"+entry.ID, watchlistEntryRequest{Name: "after", Ports: []int{22, 2222}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, ok := s.Watchlist.Get(entry.ID)
	if !ok {
		t.Fatal("entry vanished after update")
	}
	if got.Name != "after" || len(got.Ports) != 2 {
		t.Errorf("update did not apply: %+v", got)
	}
	if !got.CreatedAt.Equal(entry.CreatedAt) {
		t.Error("update must not change CreatedAt")
	}
}

func TestHandleWatchlistEntriesUpdateUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries/never-existed", watchlistEntryRequest{Ports: []int{22}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// Switching Invert true->false must clear the observe/promote state --
// it no longer means anything for a non-inverted entry.
func TestHandleWatchlistEntriesUpdateClearsStateWhenUninverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{
		Name: "cam", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	})
	var entry watchlist.Entry
	json.NewDecoder(created.Body).Decode(&entry)
	created.Body.Close()

	s.Watchlist.RecordObservation(entry.ID, "1.2.3.4", 443, entry.CreatedAt)
	if e, _ := s.Watchlist.Get(entry.ID); len(e.Observed) != 1 {
		t.Fatal("setup failed: expected an observation to exist before the update")
	}

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries/"+entry.ID, watchlistEntryRequest{
		Name: "cam", Invert: false, Ports: []int{22},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _ := s.Watchlist.Get(entry.ID)
	if got.Observing || len(got.Observed) != 0 || len(got.Permitted) != 0 {
		t.Errorf("expected Observing/Observed/Permitted cleared after un-inverting, got %+v", got)
	}
}

// Switching Invert false->true must start Observing, the same rule
// Create applies -- there is no meaningful permitted set yet.
func TestHandleWatchlistEntriesUpdateStartsObservingWhenInverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "e", Ports: []int{22}})
	var entry watchlist.Entry
	json.NewDecoder(created.Body).Decode(&entry)
	created.Body.Close()

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries/"+entry.ID, watchlistEntryRequest{
		Name: "e", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _ := s.Watchlist.Get(entry.ID)
	if !got.Observing {
		t.Error("expected Observing=true after switching to inverted")
	}
}

func TestHandleWatchlistEntriesDelete(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{Name: "e", Ports: []int{22}})
	var entry watchlist.Entry
	json.NewDecoder(created.Body).Decode(&entry)
	created.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/watchlist/entries/"+entry.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if _, ok := s.Watchlist.Get(entry.ID); ok {
		t.Error("entry still present after delete")
	}
}

func TestHandleWatchlistEntriesDeleteUnknownID(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/watchlist/entries/never-existed", nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// A PUT that omits Permitted/Observed (watchlistEntryRequest has no such
// fields at all) must not be able to wipe an entry's accumulated
// observations -- the whole reason those fields aren't in the request
// type.
func TestHandleWatchlistEntriesUpdateDoesNotClearObservedWhenStayingInverted(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	created := postJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries", watchlistEntryRequest{
		Name: "cam", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	})
	var entry watchlist.Entry
	json.NewDecoder(created.Body).Decode(&entry)
	created.Body.Close()

	s.Watchlist.RecordObservation(entry.ID, "1.2.3.4", 443, entry.CreatedAt)

	resp := putJSON(t, &http.Client{}, ts.URL+"/api/watchlist/entries/"+entry.ID, watchlistEntryRequest{
		Name: "cam renamed", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got, _ := s.Watchlist.Get(entry.ID)
	if len(got.Observed) != 1 {
		t.Errorf("expected the observation to survive an update that keeps Invert=true, got %+v", got.Observed)
	}
	if got.Name != "cam renamed" {
		t.Errorf("expected the name change to apply, got %q", got.Name)
	}
}
