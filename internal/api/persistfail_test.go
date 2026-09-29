// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/coverage"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/prefs"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// The handler half of v0.6.0's R6 rule for every store that gained an
// error-returning write this release (#1345 Q1-F1, Q1-F2): a save the
// store cannot keep answers 500 with a fixed sentence, the backend's
// own error stays out of the body and goes to the log, and nothing is
// left changed. devices_test.go's
// TestDeviceCreateReportsAFailedSaveAndChangesNothing is the template.

// failAfterSetupBackend holds its document in memory and saves normally
// until failing is set, so a test can seed a store through its real
// methods and then make the one write under test fail. Describe names a
// path the body must never repeat.
type failAfterSetupBackend struct {
	mu      sync.Mutex
	failing bool
	payload []byte
	version int64
}

func (b *failAfterSetupBackend) Load(context.Context) (persist.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return persist.Snapshot{Payload: b.payload, Version: b.version, Exists: b.version > 0}, nil
}

func (b *failAfterSetupBackend) Save(_ context.Context, payload []byte, _ int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing {
		return 0, errors.New("disk full")
	}
	b.payload = append([]byte(nil), payload...)
	b.version++
	return b.version, nil
}

func (b *failAfterSetupBackend) Close() error     { return nil }
func (b *failAfterSetupBackend) Describe() string { return "/var/lib/mikroview/secret-path/store.json" }

func (b *failAfterSetupBackend) fail() {
	b.mu.Lock()
	b.failing = true
	b.mu.Unlock()
}

// servePersistFail runs one request through h in this goroutine, with
// apiLog swapped for a buffer (internal/api's tests do not run in
// parallel; see TestResetCodeNeverReachesTheAuditLogOrServerLogs), and
// checks the three things every failed-save branch owes: a 500 carrying
// want, no backend detail in the body, and the real error in the log.
func servePersistFail(t *testing.T, h http.Handler, req *http.Request, want string) {
	t.Helper()
	var logged strings.Builder
	realLog := apiLog
	apiLog = slog.New(slog.NewTextHandler(&logged, nil))
	defer func() { apiLog = realLog }()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, body)
	}
	if !strings.Contains(body, want) {
		t.Errorf("body = %q, want it to contain %q", body, want)
	}
	for _, leak := range []string{"secret-path", "disk full", "poison", "not valid json"} {
		if strings.Contains(body, leak) {
			t.Errorf("body = %q leaks %q; the backend's detail belongs in the log only", body, leak)
		}
	}
	if !strings.Contains(logged.String(), "level=ERROR") {
		t.Errorf("no error was logged for the failed save; log = %q", logged.String())
	}
}

func jsonRequest(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
}

func TestCoverageWritesReportAFailedSaveAndChangeNothing(t *testing.T) {
	s, _ := newTestServer(t)
	b := &failAfterSetupBackend{}
	cs, err := coverage.OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Put("ether1|bridge1", "management link", "admin"); err != nil {
		t.Fatal(err)
	}
	s.Coverage = cs
	b.fail()
	h := asAdmin(s.mux())

	servePersistFail(t, h, jsonRequest(http.MethodPut, "/api/coverage/declarations/ether2|bridge1", `{"reason":"uplink"}`), "could not save that declaration")
	servePersistFail(t, h, jsonRequest(http.MethodDelete, "/api/coverage/declarations/ether1|bridge1", ""), "could not delete that declaration")

	got := s.Coverage.List()
	if len(got) != 1 || got[0].Key != "ether1|bridge1" {
		t.Errorf("List() = %+v after two failed writes, want only the original declaration", got)
	}
}

func TestEntityWritesReportAFailedSaveAndChangeNothing(t *testing.T) {
	s, _ := newTestServer(t)
	b := &failAfterSetupBackend{}
	es, err := entities.OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.Upsert(entities.Entity{Type: "host", Key: "10.0.0.2", Label: "nas"}); err != nil {
		t.Fatal(err)
	}
	s.Entities = es
	b.fail()
	h := asAdmin(s.mux())

	servePersistFail(t, h, jsonRequest(http.MethodPost, "/api/entities", `{"type":"host","key":"10.0.0.3","label":"printer"}`), "could not save that entity")
	servePersistFail(t, h, jsonRequest(http.MethodDelete, "/api/entities", `{"type":"host","key":"10.0.0.2"}`), "could not delete that entity")

	if s.Entities.Exists("host", "10.0.0.3") {
		t.Error("the entity whose save failed exists anyway")
	}
	if !s.Entities.Exists("host", "10.0.0.2") {
		t.Error("the entity whose delete failed is gone anyway")
	}
}

// openPoisonableDefinitions is a definitions store with a real backend,
// which PoisonForTest needs: an in-memory store never encodes anything,
// so it cannot fail to.
func openPoisonableDefinitions(t *testing.T) *engine.DefinitionsStore {
	t.Helper()
	defs, err := engine.OpenDefinitionsStore(filepath.Join(t.TempDir(), "definitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = defs.Close(context.Background()) })
	return defs
}

func TestDefinitionCreateReportsAFailedSaveAndChangesNothing(t *testing.T) {
	s, _ := newTestServer(t)
	s.Definitions = openPoisonableDefinitions(t)
	s.Definitions.PoisonForTest()

	servePersistFail(t, asAdmin(s.mux()), jsonRequest(http.MethodPost, "/api/definitions", `{"name":"SSH watch","expectation":{"ports":[22]}}`), "nothing was changed")

	entries, err := s.Definitions.ListExpectations()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("ListExpectations() = %+v after a failed create, want none", entries)
	}
}

func TestSuggestionsResetReportsAFailedSaveAndKeepsTheWatchlist(t *testing.T) {
	s, _ := newTestServer(t)
	s.Definitions = openPoisonableDefinitions(t)
	if err := s.Definitions.UpsertExpectation(watchlistEntryForTest("e1")); err != nil {
		t.Fatal(err)
	}
	s.Definitions.PoisonForTest()

	servePersistFail(t, asAdmin(s.mux()), jsonRequest(http.MethodPost, "/api/suggestions/reset", `{"confirm":true}`), "nothing was changed")

	if _, ok := getExpectation(t, s, "e1"); !ok {
		t.Error("the watchlist entry is gone although the reset reported failure")
	}
}

// seededSetupLedger is a setup ledger on b that already knows an
// address, a transport and an unacknowledged upgrade, so each setup
// write below has something to leave alone.
func seededSetupLedger(t *testing.T, b *failAfterSetupBackend) *setup.Store {
	t.Helper()
	ledger, err := setup.OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SetAddress("10.0.0.5:8443"); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SetBackupTransport("sftp"); err != nil {
		t.Fatal(err)
	}
	if !ledger.NoteUpgrade("v0.4.0", "v0.5.0", time.Now()) {
		t.Fatal("NoteUpgrade refused a real crossing")
	}
	return ledger
}

func TestSetupWritesReportAFailedSaveAndChangeNothing(t *testing.T) {
	s, _ := newTestServer(t)
	b := &failAfterSetupBackend{}
	s.Setup = seededSetupLedger(t, b)
	b.fail()
	h := asAdmin(s.mux())

	servePersistFail(t, h, jsonRequest(http.MethodPost, "/api/setup/mark", `{"step":6,"outcome":"skipped"}`), "nothing was changed")
	servePersistFail(t, h, jsonRequest(http.MethodPost, "/api/setup/address", `{"address":"10.0.0.9"}`), "nothing was changed")
	servePersistFail(t, h, jsonRequest(http.MethodPut, "/api/setup/backup-transport", `{"transport":"https"}`), "nothing was changed")
	servePersistFail(t, h, jsonRequest(http.MethodPost, "/api/upgrade/acknowledge", `{}`), "nothing was changed")

	if got := s.Setup.Marks(); len(got) != 0 {
		t.Errorf("Marks() = %+v after a failed mark, want none", got)
	}
	if got := s.Setup.Address(); got != "10.0.0.5:8443" {
		t.Errorf("Address() = %q after a failed change, want the original", got)
	}
	if got := s.Setup.BackupTransport(); got != "sftp" {
		t.Errorf("BackupTransport() = %q after a failed change, want the original", got)
	}
	if u, ok := s.Setup.Upgrade(); !ok || !u.AcknowledgedAt.IsZero() {
		t.Errorf("Upgrade() = %+v, %v after a failed acknowledgement, want it still unacknowledged", u, ok)
	}
}

func TestPreferencesPatchReportsAFailedSaveAndChangesNothing(t *testing.T) {
	s, _ := newTestServer(t)
	b := &failAfterSetupBackend{}
	ps, err := prefs.OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	s.Prefs = ps
	u, _, err := s.Auth.FindOrCreateOIDCUser("https://idp.example", "subject-placeholder", "frodo", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prefs.Merge(u.ID, []byte(`{"uiTheme":"teal"}`)); err != nil {
		t.Fatal(err)
	}
	b.fail()

	req := jsonRequest(http.MethodPatch, "/api/me/preferences", `{"version":1,"prefs":{"uiTheme":"amber"}}`)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: s.Sessions.Create(u.ID, time.Now()).ID})
	servePersistFail(t, s.mux(), req, "preferences could not be saved")

	got, _ := s.Prefs.Get(u.ID)
	if !strings.Contains(string(got), "teal") || strings.Contains(string(got), "amber") {
		t.Errorf("Get() = %s after a failed save, want the original record", got)
	}
}
