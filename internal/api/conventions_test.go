// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/entities"
)

// Tests for #1538 (internal conventions C14-C17). Written from the issue
// alone; each fails on the code as it stood at batch-b a3259ba6.

// goSources returns the non-test .go files under dirs (relative to the
// package directory) with their contents.
func goSources(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range dirs {
		files, err := filepath.Glob(filepath.Join(d, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out[f] = string(b)
		}
	}
	if len(out) == 0 {
		t.Fatal("no Go sources found")
	}
	return out
}

// C14: the config editor's refusals are plain text like every other route.
func TestC14WriteReauthIsPlainText(t *testing.T) {
	rec := httptest.NewRecorder()
	writeReauth(rec)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "json") {
		t.Errorf("reauth Content-Type = %q, want plain text", ct)
	}
	if b := strings.TrimSpace(rec.Body.String()); strings.HasPrefix(b, "{") || b == "" {
		t.Errorf("reauth body = %q, want a plain-text message (the signal moves to a header)", b)
	}
}

func TestC14WriteEditorErrorIsPlainText(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEditorError(rec, http.StatusNotFound, "no such snapshot")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "json") {
		t.Errorf("editor error Content-Type = %q, want plain text", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "no such snapshot" {
		t.Errorf("editor error body = %q, want the bare message", got)
	}
}

// The tune-logging 500 cannot be provoked through the handler (the check
// is mechanical), so pin the source: no {"error": ...} JSON body in the
// api package except setup-only mode, which the ADR exempts.
func TestC14NoErrorJSONBodiesOutsideSetup(t *testing.T) {
	re := regexp.MustCompile(`map\[string\](string|any)\{\s*"error"`)
	for f, src := range goSources(t, ".") {
		if b := filepath.Base(f); b == "setup.go" || b == "setuponly.go" {
			continue
		}
		if re.MatchString(src) {
			t.Errorf("%s writes a JSON {\"error\":...} body; MikroView's own routes answer plain text", f)
		}
	}
}

// C15: one wording for a role gate and one for a bad body.
func TestC15NoStrayRefusalWording(t *testing.T) {
	banned := regexp.MustCompile(`http\.Error\(w, "(forbidden|bad request|invalid JSON body)"`)
	for f, src := range goSources(t, ".") {
		for _, m := range banned.FindAllString(src, -1) {
			t.Errorf("%s: %s -- use \"<role> role required\" / \"invalid request body\"", f, m)
		}
	}
}

// C16: a DELETE with nothing to say answers 204 and no body.
func TestC16DefinitionDeleteIs204(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	entry := mustDecodeDefinition(t, postJSON(t, &http.Client{}, ts.URL+"/api/definitions",
		createDefinitionRequest{Name: "e", Expectation: &expectationRequest{Ports: []int{22}}}))
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/definitions/"+entry.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE definition = %d, want 204", resp.StatusCode)
	}
}

func TestC16EntityDeleteIs204(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	postJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "192.0.2.50"}).Body.Close()
	resp := deleteJSON(t, client, ts.URL+"/api/entities", entityRequest{Type: entities.TypeHost, Key: "192.0.2.50"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE entity = %d, want 204", resp.StatusCode)
	}
}

// C17: slog attribute key for an error is "err", never "error".
func TestC17LogErrorAttrIsErr(t *testing.T) {
	re := regexp.MustCompile(`(?i)log\.(Error|Warn|Info|Debug)\([^\n]*"error",`)
	for f, src := range goSources(t, "../decommission", "../api") {
		for _, m := range re.FindAllString(src, -1) {
			t.Errorf("%s: log attribute key \"error\" in %s; use \"err\"", f, m)
		}
	}
}

// A missing path value is a path error, not a body error (#1538 review).
func TestKeepRequestEmptyPathSaysInvalidPath(t *testing.T) {
	s, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	asAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.keepRequest(w, r); ok {
			t.Error("keepRequest accepted empty path values")
		}
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != "invalid request path" {
		t.Errorf("got %d %q, want 400 \"invalid request path\"", rec.Code, rec.Body.String())
	}
}
