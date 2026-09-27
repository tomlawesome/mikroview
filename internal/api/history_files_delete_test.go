// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #1354: DELETE /api/settings/history/files, the one way retained
// history is deleted. Driven against a real auth store and a real
// session, because the password re-check is the point; the non-admin
// refusal is the authorization matrix's row.

// offWithFilesHistory is an instance whose history has been turned off
// with 27 days still on disk.
func offWithFilesHistory() *fakeHistoryControl {
	ctl := keyedHistory()
	ctl.state.Enabled = false
	return ctl
}

func historyFilesServer(t *testing.T, ctl *fakeHistoryControl) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	s.HistoryControl = ctl
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := registerAdmin(t, s, ts)
	return s, ts, admin
}

func deleteHistoryFiles(t *testing.T, c *http.Client, ts *httptest.Server, password string) (*http.Response, string) {
	t.Helper()
	resp := deleteJSON(t, c, ts.URL+"/api/settings/history/files", historyFilesDeleteRequest{Password: password})
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(raw)
}

func TestHistoryFilesDeleteRefusesAWrongPassword(t *testing.T) {
	ctl := offWithFilesHistory()
	_, ts, admin := historyFilesServer(t, ctl)

	resp, body := deleteHistoryFiles(t, admin, ts, "not-the-password")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password got %d, want 401: %s", resp.StatusCode, body)
	}
	if strings.TrimSpace(body) != "incorrect password" {
		t.Errorf("the refusal reads %q, want the other re-checks' \"incorrect password\"", strings.TrimSpace(body))
	}
	if n := ctl.deleteCount(); n != 0 {
		t.Errorf("a wrong password still deleted the files (%d call(s))", n)
	}
}

func TestHistoryFilesDeleteRefusesWhileHistoryIsOn(t *testing.T) {
	ctl := keyedHistory()
	_, ts, admin := historyFilesServer(t, ctl)

	resp, body := deleteHistoryFiles(t, admin, ts, fixtureAdminPassword)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("deleting while on got %d, want 409: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "turn history off first") {
		t.Errorf("the refusal reads %q, which does not say to turn history off first", strings.TrimSpace(body))
	}
	if n := ctl.deleteCount(); n != 0 {
		t.Errorf("a refused delete still reached the control (%d call(s))", n)
	}
}

func TestHistoryFilesDeleteDeletesAndIsAudited(t *testing.T) {
	ctl := offWithFilesHistory()
	_, ts, admin := historyFilesServer(t, ctl)

	resp, body := deleteHistoryFiles(t, admin, ts, fixtureAdminPassword)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deleting returned %d: %s", resp.StatusCode, body)
	}
	var got HistorySettings
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Held != nil || got.Enabled {
		t.Errorf("after deleting the response reads %+v, want off with nothing held", got)
	}
	if n := ctl.deleteCount(); n != 1 {
		t.Errorf("the control was asked to delete %d time(s), want 1", n)
	}

	e := findAuditEntry(t, admin, ts, "history.delete")
	if e.Target != "history" {
		t.Errorf("audit target %q, want history", e.Target)
	}
	if want := "27 days, 851443712 bytes, 2026-08-07–2026-09-02"; e.Detail != want {
		t.Errorf("audit detail %q, want %q", e.Detail, want)
	}
	if e.Actor != fixtureAdminUsername {
		t.Errorf("audit actor %q, want %q", e.Actor, fixtureAdminUsername)
	}
}

// The re-check shares the password-recheck bucket, so a run of guesses
// is stopped rather than left as an oracle behind a session cookie.
func TestHistoryFilesDeleteIsRateLimited(t *testing.T) {
	ctl := offWithFilesHistory()
	_, ts, admin := historyFilesServer(t, ctl)

	limited := false
	for range 50 {
		resp, _ := deleteHistoryFiles(t, admin, ts, "not-the-password")
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("50 wrong passwords in a row were never rate-limited")
	}
	if n := ctl.deleteCount(); n != 0 {
		t.Errorf("wrong passwords still deleted the files (%d call(s))", n)
	}
}
