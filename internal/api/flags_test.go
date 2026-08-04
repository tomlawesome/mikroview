package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestHandleFlagsList(t *testing.T) {
	s, _ := newTestServer()
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "20 distinct ports in 60s", time.Now())

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Flags []flags.Flag `json:"flags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Flags) != 1 || body.Flags[0].Target != "203.0.113.9" || body.Flags[0].Type != flags.TypePortScan {
		t.Errorf("unexpected flags: %+v", body.Flags)
	}
}

func TestHandleFlagsClear(t *testing.T) {
	s, _ := newTestServer()
	s.Flags.Add(flags.TypeActivitySpike, "198.51.100.4", "500 events in 60s", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/"+id+"/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Cleared bool `json:"cleared"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Cleared {
		t.Error("expected cleared=true for a known, active flag")
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared {
		t.Errorf("expected the flag to be marked cleared in the store, got %+v", list)
	}
}

func TestHandleFlagsClearUnknownID(t *testing.T) {
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/does-not-exist/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (unknown ID is a no-op, not an error)", resp.StatusCode)
	}

	var body struct {
		Cleared bool `json:"cleared"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Cleared {
		t.Error("expected cleared=false for an unknown ID")
	}
}
