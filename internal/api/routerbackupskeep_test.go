// SPDX-License-Identifier: AGPL-3.0-only

package api

// The keep controls' HTTP surface (#1126): who may call them, what the
// three of them do to the router's block, what the audit log says
// afterwards -- and, as much to the point, what it does not say.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// patchJSON mirrors postJSON (auth_test.go) but for PATCH, with the
// same CSRF header the real frontend sends on every mutating request.
func patchJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func keepURL(ts *httptest.Server, generation string) string {
	return ts.URL + "/api/router-backups/rb5009/" + generation + "/protect"
}

// decodeRouterRow reads the router block every keep control answers
// with, failing the test if the status is not the one wanted.
func decodeRouterRow(t *testing.T, resp *http.Response, want int) routerBackupRouter {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, want, strings.TrimSpace(string(body)))
	}
	var row routerBackupRouter
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestKeepControlsAreAdminOnly(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()

	user := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, user, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()
	seedFactor(t, s, ts, "operator") // #1253: needed before the keep routes below

	body := routerBackupKeepRequest{Comment: "before the 7.16 upgrade"}
	for _, send := range []struct {
		method string
		do     func(*testing.T, *http.Client, string, any) *http.Response
	}{
		{http.MethodPost, postJSON},
		{http.MethodDelete, deleteJSON},
		{http.MethodPatch, patchJSON},
	} {
		resp := send.do(t, user, keepURL(ts, gen), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s the keep route as a non-admin = %d, want 403", send.method, resp.StatusCode)
		}
	}
	// Nothing was kept by any of those refusals.
	if got := len(keptRow(t, admin, ts).Protected); got != 0 {
		t.Errorf("kept pool = %d after three refused calls, want 0", got)
	}
}

// keptRow reads rb5009's block out of the list.
func keptRow(t *testing.T, client *http.Client, ts *httptest.Server) routerBackupRouter {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Routers) != 1 {
		t.Fatalf("routers = %d, want 1", len(out.Routers))
	}
	return out.Routers[0]
}

func TestKeepingABackupMovesItIntoTheKeptPool(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)

	row := decodeRouterRow(t, postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "before the 7.16 upgrade"}), http.StatusOK)
	if len(row.Generations) != 0 {
		t.Errorf("cycling set = %d, want 0 -- the only generation was kept", len(row.Generations))
	}
	if len(row.Protected) != 1 {
		t.Fatalf("kept pool = %d, want 1", len(row.Protected))
	}
	kept := row.Protected[0]
	if kept.ID != gen || kept.Comment != "before the 7.16 upgrade" {
		t.Errorf("kept entry = %+v, want %s with the comment", kept, gen)
	}
	if kept.ProtectedAt.IsZero() || kept.ProtectedBy == "" {
		t.Errorf("kept entry records no when/who: %+v", kept)
	}
	if kept.BackupArrivedAt.IsZero() || kept.BackupBytes == 0 {
		t.Errorf("kept entry lost the arrival facts: %+v", kept)
	}

	// The list agrees with what the control answered.
	listed := keptRow(t, admin, ts)
	if len(listed.Protected) != 1 || listed.Protected[0].ID != gen {
		t.Errorf("GET /api/router-backups protected = %+v, want the kept generation", listed.Protected)
	}

	// The audit entry names the generation and never the comment: the
	// comment is sealed in the vault index, and the audit log is read
	// by a wider set of people than may open a backup.
	detail, ok := auditDetail(s, "router_backup.protected")
	if !ok {
		t.Fatalf("no router_backup.protected audit entry: %+v", s.Audit.Query(audit.Query{}).Entries)
	}
	if !strings.Contains(detail, "generation="+gen) {
		t.Errorf("audit detail = %q, want it to name the generation", detail)
	}
	if strings.Contains(detail, "7.16") {
		t.Errorf("audit detail carries the comment: %q", detail)
	}
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if strings.Contains(e.Detail, "7.16") {
			t.Errorf("the comment reached the audit log in %s: %q", e.Action, e.Detail)
		}
	}

	// And the kept generation is still downloadable.
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Errorf("download of a kept generation = %d, want 200", got)
	}
}

func TestKeepRefusesAMissingOrOverlongComment(t *testing.T) {
	_, ts, admin, gen := vaultLockFixture(t)
	for name, comment := range map[string]string{
		"missing":      "",
		"whitespace":   "   ",
		"over the cap": strings.Repeat("x", backupvault.MaxCommentRunes+1),
	} {
		resp := postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: comment})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("keeping with a %s comment = %d, want 400", name, resp.StatusCode)
		}
	}
	if got := len(keptRow(t, admin, ts).Protected); got != 0 {
		t.Errorf("kept pool = %d after refused comments, want 0", got)
	}
}

func TestKeepOnSomethingTheVaultDoesNotHoldIs404(t *testing.T) {
	_, ts, admin, gen := vaultLockFixture(t)
	body := routerBackupKeepRequest{Comment: "why"}

	unknownGen := postJSON(t, admin, keepURL(ts, "no-such-generation"), body)
	unknownGen.Body.Close()
	if unknownGen.StatusCode != http.StatusNotFound {
		t.Errorf("keeping an unknown generation = %d, want 404", unknownGen.StatusCode)
	}
	unknownDevice := postJSON(t, admin, ts.URL+"/api/router-backups/no-such-router/"+gen+"/protect", body)
	unknownDevice.Body.Close()
	if unknownDevice.StatusCode != http.StatusNotFound {
		t.Errorf("keeping a generation of an unknown router = %d, want 404", unknownDevice.StatusCode)
	}
	// Releasing one that is not kept, and commenting on one that is
	// not kept, are the same answer.
	release := deleteJSON(t, admin, keepURL(ts, gen), nil)
	release.Body.Close()
	if release.StatusCode != http.StatusNotFound {
		t.Errorf("releasing a generation that is not kept = %d, want 404", release.StatusCode)
	}
	comment := patchJSON(t, admin, keepURL(ts, gen), body)
	comment.Body.Close()
	if comment.StatusCode != http.StatusNotFound {
		t.Errorf("commenting on a generation that is not kept = %d, want 404", comment.StatusCode)
	}
}

func TestKeepingTheSameBackupTwiceIsAConflict(t *testing.T) {
	_, ts, admin, gen := vaultLockFixture(t)
	first := postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "before the upgrade"})
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first keep = %d, want 200", first.StatusCode)
	}
	second := postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "again"})
	second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Errorf("keeping the same generation twice = %d, want 409", second.StatusCode)
	}
}

func TestReleasingPutsItBackIntoTheCyclingSet(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "before the upgrade"}).Body.Close()

	row := decodeRouterRow(t, deleteJSON(t, admin, keepURL(ts, gen), nil), http.StatusOK)
	if len(row.Protected) != 0 {
		t.Errorf("kept pool = %+v after a release, want empty", row.Protected)
	}
	if len(row.Generations) != 1 || row.Generations[0].ID != gen {
		t.Fatalf("cycling set = %+v, want the released generation back in it", row.Generations)
	}
	if row.Generations[0].Comment != "" {
		t.Errorf("the released generation still carries %q", row.Generations[0].Comment)
	}
	detail, ok := auditDetail(s, "router_backup.unprotected")
	if !ok || !strings.Contains(detail, "generation="+gen) {
		t.Errorf("router_backup.unprotected audit detail = %q (found=%v), want it to name the generation", detail, ok)
	}
}

func TestChangingAKeptBackupsComment(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	postJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "before the upgrade"}).Body.Close()

	row := decodeRouterRow(t, patchJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: "before the 7.16 upgrade"}), http.StatusOK)
	if len(row.Protected) != 1 || row.Protected[0].Comment != "before the 7.16 upgrade" {
		t.Fatalf("kept pool = %+v, want the rewritten comment", row.Protected)
	}
	empty := patchJSON(t, admin, keepURL(ts, gen), routerBackupKeepRequest{Comment: ""})
	empty.Body.Close()
	if empty.StatusCode != http.StatusBadRequest {
		t.Errorf("emptying a kept backup's comment = %d, want 400", empty.StatusCode)
	}
	if got := keptRow(t, admin, ts).Protected[0].Comment; got != "before the 7.16 upgrade" {
		t.Errorf("a refused comment changed the stored one to %q", got)
	}
	detail, ok := auditDetail(s, "router_backup.comment_changed")
	if !ok || !strings.Contains(detail, "generation="+gen) {
		t.Errorf("router_backup.comment_changed audit detail = %q (found=%v), want it to name the generation", detail, ok)
	}
}
