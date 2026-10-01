// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/settings"
)

// fakeRouterBackupSwitch stands in for main's backupRuntime, the same
// reasoning fakeHistoryControl gives for itself: these tests are about
// the endpoint -- who may call it, what it refuses, what it hands the
// switch -- not the real listener, which has no place in this package.
type fakeRouterBackupSwitch struct {
	mu         sync.Mutex
	open       bool
	port       string
	startErr   error
	startCalls int
	stopCalls  int
}

func (f *fakeRouterBackupSwitch) State() (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open {
		return false, ""
	}
	return true, f.port
}

func (f *fakeRouterBackupSwitch) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	if f.startErr != nil {
		return f.startErr
	}
	f.open = true
	return nil
}

func (f *fakeRouterBackupSwitch) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	f.open = false
}

// fakeRouterBackupNotifier stands in for notify.SMTPNotifier's SendNotice.
type fakeRouterBackupNotifier struct {
	mu      sync.Mutex
	notices []noticeCall
	err     error
}

type noticeCall struct {
	subject, body string
}

func (f *fakeRouterBackupNotifier) SendNotice(subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, noticeCall{subject, body})
	return f.err
}

func (f *fakeRouterBackupNotifier) calls() []noticeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]noticeCall{}, f.notices...)
}

// routerBackupSwitchServer wires a fakeRouterBackupSwitch and a real
// settings.Store (SetBackup/Backup are the point of several tests below,
// so a real store, not a fake, is what proves the write actually lands)
// into an admin-ready server.
func routerBackupSwitchServer(t *testing.T, sw *fakeRouterBackupSwitch) (*Server, *fakeRouterBackupNotifier, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	set, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = set
	s.RouterBackupSwitch = sw
	notifier := &fakeRouterBackupNotifier{}
	s.RouterBackupNotifier = notifier
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := registerAdmin(t, s, ts)
	return s, notifier, ts, admin
}

func putRouterBackupSwitch(t *testing.T, c *http.Client, ts *httptest.Server, open bool, password string) (*http.Response, routerBackupSwitchResponse, string) {
	t.Helper()
	resp := putJSON(t, c, ts.URL+"/api/settings/router-backups", routerBackupSwitchRequest{Open: open, Password: password})
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var parsed routerBackupSwitchResponse
	_ = json.Unmarshal(raw, &parsed)
	return resp, parsed, string(raw)
}

func TestRouterBackupSwitchRequiresAdmin(t *testing.T) {
	sw := &fakeRouterBackupSwitch{}
	s, _, ts, adminClient := routerBackupSwitchServer(t, sw)

	// TestRouterBackupsListNonAdminForbidden's own viewer-account recipe:
	// a second account, role viewer, signed in on its own client.
	resp := postJSON(t, adminClient, ts.URL+"/api/auth/users", map[string]string{"username": "viewer1", "password": "password123", "role": "viewer"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the viewer account: status = %d", resp.StatusCode)
	}
	viewer := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, viewer, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer1", Password: "password123"})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("viewer login status = %d", loginResp.StatusCode)
	}
	seedFactor(t, s, ts, "viewer1")

	vresp, _, body := putRouterBackupSwitch(t, viewer, ts, true, fixtureAdminPassword)
	if vresp.StatusCode != http.StatusForbidden {
		t.Fatalf("a viewer got %d, want 403: %s", vresp.StatusCode, body)
	}
	if sw.startCalls != 0 {
		t.Errorf("a refused viewer still started the drop box (%d call(s))", sw.startCalls)
	}
}

func TestRouterBackupSwitchUnavailableWithoutWiring(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := registerAdmin(t, s, ts)

	resp, _, body := putRouterBackupSwitch(t, admin, ts, true, fixtureAdminPassword)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("an instance built with no switch got %d, want 503: %s", resp.StatusCode, body)
	}
}

func TestRouterBackupSwitchOpenRequiresTheCorrectPassword(t *testing.T) {
	sw := &fakeRouterBackupSwitch{}
	_, notifier, ts, admin := routerBackupSwitchServer(t, sw)

	resp, _, body := putRouterBackupSwitch(t, admin, ts, true, "not-the-password")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password got %d, want 401: %s", resp.StatusCode, body)
	}
	if strings.TrimSpace(body) != "incorrect password" {
		t.Errorf("the refusal reads %q, want the other re-checks' \"incorrect password\"", strings.TrimSpace(body))
	}
	if sw.startCalls != 0 {
		t.Errorf("a wrong password still started the drop box (%d call(s))", sw.startCalls)
	}
	if len(notifier.calls()) != 0 {
		t.Error("a wrong password still sent the change email")
	}
}

func TestRouterBackupSwitchOpenStartsTheListenerAndStoresThePosition(t *testing.T) {
	sw := &fakeRouterBackupSwitch{port: "47022"}
	s, notifier, ts, admin := routerBackupSwitchServer(t, sw)

	resp, parsed, body := putRouterBackupSwitch(t, admin, ts, true, fixtureAdminPassword)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("opening with the right password got %d, want 200: %s", resp.StatusCode, body)
	}
	if !parsed.Open || parsed.Port != "47022" {
		t.Errorf("response = %+v, want open on 47022", parsed)
	}
	if sw.startCalls != 1 {
		t.Errorf("Start was called %d time(s), want 1", sw.startCalls)
	}

	pos, ok := s.Settings.Backup()
	if !ok || !pos.Enabled {
		t.Fatalf("stored switch position = (%+v, %v), want an enabled position stored", pos, ok)
	}
	if pos.ChangedBy != fixtureAdminUsername {
		t.Errorf("ChangedBy = %q, want %q", pos.ChangedBy, fixtureAdminUsername)
	}
	if time.Since(pos.ChangedAt) > time.Minute {
		t.Errorf("ChangedAt = %v, want close to now", pos.ChangedAt)
	}

	e := findAuditEntry(t, admin, ts, "router_backup.opened")
	if e.Target != "switch" {
		t.Errorf("audit target %q, want switch", e.Target)
	}
	if e.Actor != fixtureAdminUsername {
		t.Errorf("audit actor %q, want %q", e.Actor, fixtureAdminUsername)
	}
	if !strings.Contains(e.Detail, "47022") {
		t.Errorf("audit detail %q does not name the port", e.Detail)
	}

	calls := notifier.calls()
	if len(calls) != 1 {
		t.Fatalf("the notifier was called %d time(s), want 1", len(calls))
	}
	if !strings.Contains(calls[0].subject, "opened") {
		t.Errorf("notice subject %q does not say opened", calls[0].subject)
	}
	if !strings.Contains(calls[0].body, fixtureAdminUsername) || !strings.Contains(calls[0].body, "47022") {
		t.Errorf("notice body %q does not name the admin and the port", calls[0].body)
	}
}

// A bind failure -- the configured port already taken by something else
// -- must come back as this request's own 409, with nothing written:
// no stored position, no audit line, no email. The design's own
// "bind-then-store" ordering (routerbackupswitch.go's doc comment).
func TestRouterBackupSwitchOpenPortInUseStoresNothing(t *testing.T) {
	sw := &fakeRouterBackupSwitch{startErr: fmt.Errorf("listen tcp :47022: address already in use")}
	s, notifier, ts, admin := routerBackupSwitchServer(t, sw)

	resp, _, body := putRouterBackupSwitch(t, admin, ts, true, fixtureAdminPassword)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a bind failure got %d, want 409: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "already in use") {
		t.Errorf("the refusal %q does not carry the bind error", body)
	}
	if _, ok := s.Settings.Backup(); ok {
		t.Error("a failed open still stored a switch position")
	}
	if len(notifier.calls()) != 0 {
		t.Error("a failed open still sent the change email")
	}
}

func TestRouterBackupSwitchCloseNeedsNoPassword(t *testing.T) {
	sw := &fakeRouterBackupSwitch{open: true, port: "47022"}
	s, notifier, ts, admin := routerBackupSwitchServer(t, sw)

	resp, parsed, body := putRouterBackupSwitch(t, admin, ts, false, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("closing with no password got %d, want 200: %s", resp.StatusCode, body)
	}
	if parsed.Open || parsed.Port != "" {
		t.Errorf("response = %+v, want closed", parsed)
	}
	if sw.stopCalls != 1 {
		t.Errorf("Stop was called %d time(s), want 1", sw.stopCalls)
	}

	pos, ok := s.Settings.Backup()
	if !ok || pos.Enabled {
		t.Fatalf("stored switch position = (%+v, %v), want a closed position stored", pos, ok)
	}

	findAuditEntry(t, admin, ts, "router_backup.closed")

	calls := notifier.calls()
	if len(calls) != 1 || !strings.Contains(calls[0].subject, "closed") {
		t.Errorf("notice calls = %+v, want one call naming closed", calls)
	}
}

// The re-check shares the password-recheck bucket, so a run of guesses
// against the drop box's own switch is stopped the same way every other
// password re-check is (TestHistoryFilesDeleteIsRateLimited's own
// reasoning).
func TestRouterBackupSwitchOpenIsRateLimited(t *testing.T) {
	sw := &fakeRouterBackupSwitch{}
	_, _, ts, admin := routerBackupSwitchServer(t, sw)

	limited := false
	for range 50 {
		resp, _, _ := putRouterBackupSwitch(t, admin, ts, true, "not-the-password")
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("50 wrong passwords in a row were never rate-limited")
	}
	if sw.startCalls != 0 {
		t.Errorf("wrong passwords still started the drop box (%d call(s))", sw.startCalls)
	}
}

// --- the 7-day admin banner (#1361, owner's 4a) -----------------------

func TestRouterBackupSwitchChangedBannerNothingStored(t *testing.T) {
	s := newAuthTestServer(t)
	set, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = set

	if _, ok := s.routerBackupSwitchChanged(); ok {
		t.Error("a fresh instance with nothing stored reported a banner")
	}
}

func TestRouterBackupSwitchChangedBannerShowsARecentChange(t *testing.T) {
	s := newAuthTestServer(t)
	set, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = set
	if err := set.SetBackup(settings.Backup{Enabled: true, ChangedAt: time.Now(), ChangedBy: "alice"}); err != nil {
		t.Fatal(err)
	}

	p, ok := s.routerBackupSwitchChanged()
	if !ok {
		t.Fatal("a change from moments ago reported no banner")
	}
	if p.Severity != "warn" {
		t.Errorf("severity = %q, want warn", p.Severity)
	}
	if !strings.Contains(p.Message, "alice") || !strings.Contains(p.Message, "opened") {
		t.Errorf("message %q does not name who opened it", p.Message)
	}
}

// The owner's ruling on question 4a: the banner clears seven days after
// each change, not never and not on a per-admin read.
func TestRouterBackupSwitchChangedBannerClearsAfterSevenDays(t *testing.T) {
	s := newAuthTestServer(t)
	set, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = set
	if err := set.SetBackup(settings.Backup{
		Enabled:   false,
		ChangedAt: time.Now().Add(-8 * 24 * time.Hour),
		ChangedBy: "alice",
	}); err != nil {
		t.Fatal(err)
	}

	if _, ok := s.routerBackupSwitchChanged(); ok {
		t.Error("a change from eight days ago still shows the banner")
	}
}

func TestRouterBackupSwitchChangedBannerAppearsInConfigProblems(t *testing.T) {
	sw := &fakeRouterBackupSwitch{port: "47022"}
	s, _, ts, admin := routerBackupSwitchServer(t, sw)

	resp, _, body := putRouterBackupSwitch(t, admin, ts, true, fixtureAdminPassword)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("opening the switch failed: %d %s", resp.StatusCode, body)
	}

	res, err := admin.Get(ts.URL + "/api/config/problems")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var parsed struct {
		Problems []ConfigProblem `json:"problems"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range parsed.Problems {
		if p.Code == "router-backup-switch-changed" {
			found = true
		}
	}
	if !found {
		t.Errorf("problems = %+v, want the router-backup-switch-changed entry", parsed.Problems)
	}
	_ = s
}
