// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/configsnap"
)

// editorConfig is a running config.yaml as the editor would find it:
// old keys, and a secret.
const editorConfig = `listen:
  syslogUdp: ":1514"
  http: ":8080"
reputation:
  abuseIPDBKey: "editor-secret-1347"
`

type editorFixture struct {
	s     *Server
	ts    *httptest.Server
	admin *http.Client
	path  string
}

func newEditorFixture(t *testing.T) editorFixture {
	t.Helper()
	s := newAuthTestServer(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(editorConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	snaps, err := configsnap.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigEditor = &ConfigEditor{Path: path, StartupText: []byte(editorConfig), RunningVersion: "0.6.1", Snapshots: snaps}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := registerAdmin(t, s, ts)
	return editorFixture{s: s, ts: ts, admin: admin, path: path}
}

func (f editorFixture) open(t *testing.T, password string) *http.Response {
	t.Helper()
	return postJSON(t, f.admin, f.ts.URL+"/api/config/editor/open", editorOpenRequest{Password: password})
}

func (f editorFixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := f.admin.Get(f.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeInto(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func wantReauth(t *testing.T, resp *http.Response, what string) {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusUnauthorized || body["reauth"] != true {
		t.Errorf("%s after the unlock lapsed: %d %v, want 401 {\"reauth\":true}", what, resp.StatusCode, body)
	}
}

func auditActions(s *Server) []string {
	var out []string
	for _, e := range s.Audit.Query(audit.Query{Limit: 1000}).Entries {
		out = append(out, e.Action)
	}
	return out
}

func TestConfigEditorOpenNeedsThePassword(t *testing.T) {
	f := newEditorFixture(t)

	resp := f.open(t, "not-the-password")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d, want 401", resp.StatusCode)
	}
	// Nothing was unlocked by the failed attempt.
	wantReauth(t, f.get(t, "/api/config/editor/reveal"), "reveal after a wrong password")

	resp = f.open(t, fixtureAdminPassword)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open: %d", resp.StatusCode)
	}
	var out editorOpenResponse
	decodeInto(t, resp, &out)
	if strings.Contains(out.Text, "editor-secret-1347") || !strings.Contains(out.Text, config.SecretPlaceholder("reputation.abuseIPDBKey")) {
		t.Errorf("open did not mask the secret:\n%s", out.Text)
	}
	if out.Path != f.path || out.ChangedSinceStart || out.Header != nil || out.SchemaGuess != 1 ||
		out.RunningVersion != "0.6.1" || out.RunningSchema != config.CurrentSchema {
		t.Errorf("open response = %+v", out)
	}
	if acts := auditActions(f.s); !slices.Contains(acts, "config.editor.open") {
		t.Errorf("no config.editor.open audit entry: %v", acts)
	}

	// Changed on disk since start: said so.
	if err := os.WriteFile(f.path, []byte(editorConfig+"# edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decodeInto(t, f.open(t, fixtureAdminPassword), &out)
	if !out.ChangedSinceStart {
		t.Error("a file changed on disk since start-up was not reported")
	}
}

func TestConfigEditorDownloadRevealAndReauth(t *testing.T) {
	f := newEditorFixture(t)
	var opened editorOpenResponse
	decodeInto(t, f.open(t, fixtureAdminPassword), &opened)

	// Reveal, within the unlock.
	var revealed struct {
		Secrets map[string]string `json:"secrets"`
	}
	decodeInto(t, f.get(t, "/api/config/editor/reveal"), &revealed)
	if revealed.Secrets["reputation.abuseIPDBKey"] != `"editor-secret-1347"` {
		t.Errorf("reveal = %v", revealed.Secrets)
	}

	// Validate: the removed key is on its line.
	var validated struct {
		Problems []config.TextProblem `json:"problems"`
	}
	decodeInto(t, postJSON(t, f.admin, f.ts.URL+"/api/config/validate", editorTextRequest{Text: opened.Text}), &validated)
	if len(validated.Problems) == 0 || validated.Problems[0].Key != "listen.syslogUdp" || validated.Problems[0].Line != 2 {
		t.Errorf("validate = %+v", validated.Problems)
	}

	// Carry forward: rewritten, masked, and the old text snapshotted.
	var carried carryForwardResponse
	decodeInto(t, postJSON(t, f.admin, f.ts.URL+"/api/config/carry-forward", editorTextRequest{Text: opened.Text}), &carried)
	if strings.Contains(carried.Text, "syslogUdp") || strings.Contains(carried.Text, "editor-secret-1347") {
		t.Errorf("carry-forward text:\n%s", carried.Text)
	}
	snaps := f.s.ConfigEditor.Snapshots.List()
	if len(snaps) != 1 || snaps[0].Reason != configsnap.ReasonBeforeCarryForward || snaps[0].Schema != 1 {
		t.Fatalf("snapshots after carry forward = %+v", snaps)
	}
	if old, _ := f.s.ConfigEditor.Snapshots.Get(snaps[0].ID); old.Text != editorConfig {
		t.Errorf("the before-carry-forward snapshot is not the text as it was, secrets put back:\n%s", old.Text)
	}
	// Pressing it again on the same text takes no second snapshot.
	postJSON(t, f.admin, f.ts.URL+"/api/config/carry-forward", editorTextRequest{Text: opened.Text}).Body.Close()
	if n := f.s.ConfigEditor.Snapshots.Count(); n != 1 {
		t.Errorf("a repeated carry forward took another snapshot: %d", n)
	}

	// Download: the real secret, the header, the name.
	resp := postJSON(t, f.admin, f.ts.URL+"/api/config/download", editorTextRequest{Text: carried.Text})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download: %d %s", resp.StatusCode, body)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="config.v0.6.1.yaml"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !strings.Contains(string(body), `abuseIPDBKey: "editor-secret-1347"`) {
		t.Errorf("download does not carry the real secret:\n%s", body)
	}
	if h, ok := config.ParseHeader(string(body)); !ok || h.Schema != config.CurrentSchema || h.WrittenBy != "v0.6.1" {
		t.Errorf("download header = %+v, %v", h, ok)
	}

	// A manual snapshot, read back within the unlock.
	var meta snapshotView
	decodeInto(t, postJSON(t, f.admin, f.ts.URL+"/api/config/snapshots", snapshotCreateRequest{Text: carried.Text, Note: "before the move"}), &meta)
	var got snapshotGetResponse
	decodeInto(t, f.get(t, "/api/config/snapshots/"+meta.ID), &got)
	if got.Note == nil || *got.Note != "before the move" || got.Why != configsnap.ReasonManual || strings.Contains(got.Text, "editor-secret-1347") || !strings.Contains(got.Text, "<<secret:") {
		t.Errorf("snapshot get = %+v", got)
	}

	// The unlock lapses.
	f.s.editor.mu.Lock()
	for _, st := range f.s.editor.sessions {
		st.unlockedAt = time.Now().Add(-editorUnlockWindow - time.Minute)
	}
	f.s.editor.mu.Unlock()
	wantReauth(t, postJSON(t, f.admin, f.ts.URL+"/api/config/download", editorTextRequest{Text: carried.Text}), "download")
	wantReauth(t, f.get(t, "/api/config/editor/reveal"), "reveal")
	wantReauth(t, f.get(t, "/api/config/snapshots/"+meta.ID), "snapshot get")

	// Validate and carry forward do not need the unlock.
	resp = postJSON(t, f.admin, f.ts.URL+"/api/config/validate", editorTextRequest{Text: carried.Text})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("validate after the unlock lapsed: %d", resp.StatusCode)
	}

	// Delete, audited.
	req, _ := http.NewRequest(http.MethodDelete, f.ts.URL+"/api/config/snapshots/"+meta.ID, nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := f.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", resp.StatusCode)
	}
	acts := auditActions(f.s)
	for _, want := range []string{"config.editor.open", "config.snapshot", "config.download", "config.snapshot.delete"} {
		if !slices.Contains(acts, want) {
			t.Errorf("no %s audit entry: %v", want, acts)
		}
	}
	for _, e := range f.s.Audit.Query(audit.Query{Limit: 1000}).Entries {
		if strings.Contains(e.Detail+e.Target, "editor-secret-1347") {
			t.Errorf("an audit entry carries a secret value: %+v", e)
		}
	}
}

func TestConfigEditorDownloadRefusesAnUnresolvedSecret(t *testing.T) {
	f := newEditorFixture(t)
	f.open(t, fixtureAdminPassword).Body.Close()
	text := "oidc:\n  clientSecret: " + config.SecretPlaceholder("oidc.clientSecret") + "\n"
	resp := postJSON(t, f.admin, f.ts.URL+"/api/config/download", editorTextRequest{Text: text})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a placeholder with no value downloaded: %d", resp.StatusCode)
	}
}

func TestConfigSnapshotsUnavailableWithoutAStore(t *testing.T) {
	f := newEditorFixture(t)
	f.s.ConfigEditor.Snapshots = nil
	f.s.ConfigEditor.SnapshotsUnavailable = "no retention key"
	var list snapshotsListResponse
	decodeInto(t, f.get(t, "/api/config/snapshots"), &list)
	if list.Available || list.Reason == nil || *list.Reason != "no retention key" || list.Snapshots == nil {
		t.Errorf("list without a store = %+v", list)
	}
	resp := postJSON(t, f.admin, f.ts.URL+"/api/config/snapshots", snapshotCreateRequest{Text: "listen: {}\n"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("create without a store: %d, want 503", resp.StatusCode)
	}
}

func TestSetupOnlyServesOnlyItsRoutes(t *testing.T) {
	f := newEditorFixture(t)
	ts := httptest.NewServer(f.s.SetupOnlyRoutes())
	defer ts.Close()

	for _, c := range []*http.Client{{}, f.admin} {
		resp, err := c.Get(ts.URL + "/api/events")
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]string
		decodeInto(t, resp, &body)
		if resp.StatusCode != http.StatusServiceUnavailable || body["error"] != SetupOnlyMessage {
			t.Errorf("/api/events in setup-only mode: %d %v, want 503 with the setup-only message", resp.StatusCode, body)
		}
	}

	resp, err := http.Get(ts.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	decodeInto(t, resp, &health)
	if resp.StatusCode != http.StatusOK || health["mode"] != "setup-only" || health["status"] != "ok" {
		t.Errorf("healthz in setup-only mode: %d %v", resp.StatusCode, health)
	}

	// Sign-in and the editor are reachable; the editor still needs an
	// admin session behind it.
	resp, err = http.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/auth/session in setup-only mode: %d", resp.StatusCode)
	}
	anon, err := http.Get(ts.URL + "/api/config/snapshots")
	if err != nil {
		t.Fatal(err)
	}
	anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous editor route in setup-only mode: %d, want 401", anon.StatusCode)
	}
	resp = postJSON(t, f.admin, ts.URL+"/api/config/editor/open", editorOpenRequest{Password: fixtureAdminPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("editor open in setup-only mode: %d", resp.StatusCode)
	}
}

// Setup-only mode serves a subset of the normal table, and each of its
// routes must enforce what authzMatrix says for the same pattern -- a
// route here with no row would be a gap the matrix never looked at.
func TestSetupOnlyRoutesAreInTheAuthorizationMatrix(t *testing.T) {
	s, _ := newTestServer(t)
	declared := map[string]bool{}
	for _, r := range authzMatrix {
		declared[r.method+" "+r.path] = true
	}
	for _, r := range s.setupOnlyRoutes() {
		if !declared[r.method+" "+r.path] {
			t.Errorf("setup-only route %s %s has no row in authzMatrix", r.method, r.path)
		}
	}
}

func TestConfigEditorSummaryNeedsNoUnlock(t *testing.T) {
	f := newEditorFixture(t)
	postJSON(t, f.admin, f.ts.URL+"/api/config/snapshots", snapshotCreateRequest{Text: "listen: {}\n"}).Body.Close()

	resp := f.get(t, "/api/config/editor/summary")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("summary without opening the editor: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "editor-secret-1347") || strings.Contains(string(body), "syslogUdp") {
		t.Errorf("summary carries the config's text: %s", body)
	}
	var got editorSummaryResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := editorSummaryResponse{Path: f.path, SchemaGuess: 1, RunningVersion: "0.6.1", RunningSchema: config.CurrentSchema, SnapshotCount: 1}
	if got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
	if !strings.Contains(string(body), `"header":null`) {
		t.Errorf("a headerless file's header is not null: %s", body)
	}

	// The list's shape: when/why/note, reason null when available.
	resp = f.get(t, "/api/config/snapshots")
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{`"reason":null`, `"when":"`, `"why":"manual"`, `"note":null`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("snapshot list lacks %s: %s", want, body)
		}
	}
}
