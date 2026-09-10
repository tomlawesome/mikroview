// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/coverage"
)

func TestHandleCoverageList(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Coverage.Put("ether1|bridge1", "internal management link", "admin"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/coverage/declarations")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Declarations []coverage.Declaration `json:"declarations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Declarations) != 1 || body.Declarations[0].Key != "ether1|bridge1" {
		t.Errorf("unexpected declarations: %+v", body.Declarations)
	}
}

func TestHandleCoveragePutCreates(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putCoverage(t, ts.URL, "ether1|bridge1", `{"reason":"internal management link"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var d coverage.Declaration
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d.Key != "ether1|bridge1" || d.Reason != "internal management link" {
		t.Errorf("unexpected declaration: %+v", d)
	}
	if d.DeclaredBy != "admin" {
		t.Errorf("declaredBy = %q, want the session's username", d.DeclaredBy)
	}
	if d.DeclaredAt.IsZero() {
		t.Error("expected declaredAt to be set")
	}

	list := s.Coverage.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 declaration in the store, got %d", len(list))
	}
}

func TestHandleCoveragePutUpserts(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Coverage.Put("ether1|bridge1", "first reason", "someone"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := putCoverage(t, ts.URL, "ether1|bridge1", `{"reason":"second reason"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := s.Coverage.List()
	if len(list) != 1 || list[0].Reason != "second reason" {
		t.Errorf("expected the put to upsert in place, got %+v", list)
	}
}

func TestHandleCoveragePutRejectsInvalidBody(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	// Missing/empty reason -- coverage.Store.Put's own validation refuses
	// this, which the handler surfaces as 400.
	resp := putCoverage(t, ts.URL, "ether1|bridge1", `{"reason":""}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an empty reason", resp.StatusCode)
	}
	if len(s.Coverage.List()) != 0 {
		t.Error("expected no declaration to be stored on a rejected request")
	}
}

func TestHandleCoverageDelete(t *testing.T) {
	s, _ := newTestServer(t)
	if _, err := s.Coverage.Put("ether1|bridge1", "a reason", "admin"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/coverage/declarations/ether1|bridge1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if len(s.Coverage.List()) != 0 {
		t.Error("expected the declaration to be gone from the store")
	}
}

func TestHandleCoverageDeleteUnknownKeyIs404(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/coverage/declarations/does-not-exist", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// putCoverage issues a PUT to /api/coverage/declarations/{key} with a
// raw JSON body, mirroring flags_test.go/entities_test.go's direct
// http.Post use against s.mux() (no CSRF header needed -- that check
// lives in requireAuth, which s.mux() deliberately bypasses; see
// newTestServer's own doc comment).
func putCoverage(t *testing.T, base, key, jsonBody string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/coverage/declarations/"+key, bytes.NewReader([]byte(jsonBody)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
