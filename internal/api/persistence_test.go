// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPersistenceServesAnAdminTheLiveBackend covers #677's settings
// persistence row: the response must carry whatever backend main.go
// actually resolved at boot, not a guess.
func TestPersistenceServesAnAdminTheLiveBackend(t *testing.T) {
	s, _ := newTestServer(t)
	s.Persistence = PersistenceInfo{Backend: "file", Dir: "/var/lib/mikroview"}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/persistence")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an admin got %d, want 200", resp.StatusCode)
	}
	var got PersistenceInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Backend != "file" || got.Dir != "/var/lib/mikroview" {
		t.Errorf("got %+v, want the server's configured Persistence value unchanged", got)
	}
}

// TestPersistenceReportsPostgresWithNoDir: the postgres backend has no
// filesystem path, and Dir must stay empty rather than carrying a stale
// or misleading file-backend value.
func TestPersistenceReportsPostgresWithNoDir(t *testing.T) {
	s, _ := newTestServer(t)
	s.Persistence = PersistenceInfo{Backend: "postgres"}
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/persistence")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got PersistenceInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Backend != "postgres" || got.Dir != "" {
		t.Errorf("got %+v, want backend=postgres and no directory", got)
	}
}

// TestPersistenceRefusesACallerWithNoSession is the negative case: a
// filesystem path is infrastructure detail, gated the same way
// /api/config/problems already is (see handlePersistence's own doc
// comment), so an anonymous caller must not see it.
func TestPersistenceRefusesACallerWithNoSession(t *testing.T) {
	s, _ := newTestServer(t)
	s.Persistence = PersistenceInfo{Backend: "file", Dir: "/var/lib/mikroview"}
	ts := httptest.NewServer(s.mux()) // no admin injected
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/persistence")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("an anonymous caller was served the persistence backend/directory: %s", body)
	}
}

// TestPersistenceRefusesANonAdminSession: #677's route follows
// /api/config/problems' reasoning (a directory is infrastructure
// detail), not Settings' usual viewer-tier reads, so a signed-in
// non-admin must be refused too, not just an anonymous caller.
func TestPersistenceRefusesANonAdminSession(t *testing.T) {
	s, _ := newTestServer(t)
	s.Persistence = PersistenceInfo{Backend: "file", Dir: "/var/lib/mikroview"}
	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/persistence")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a non-admin got %d, want 403", resp.StatusCode)
	}
}
