// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// TestSetupStatusOpenToViewer pins the viewer-readable settings widening
// (#490) for GET /api/setup/status: a signed-in non-admin now gets 200,
// and a signed-out caller is still refused. handleSetupStatus only ever
// reports observations mikroview made on its own side; the one write
// endpoint under /api/setup (POST /api/setup/mark, #487) is admin-only
// and pinned separately below.
func TestSetupStatusOpenToViewer(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: fixtureUserPassword, Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: fixtureUserPassword}).Body.Close()
	seedFactor(t, s, ts, "viewer") // #1253: needed before /api/setup/status below

	resp, err := viewerClient.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected a viewer to read setup status (#490), got %d", resp.StatusCode)
	}

	anonResp, err := http.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected a signed-out caller to still be refused, got %d", anonResp.StatusCode)
	}
}

// TestSetupMarkRecordsLedgerAndAudit pins the two writes the claim
// ledger's forced-past record depends on (#487): the mark itself, which
// every surface with a silence to explain reads back off
// GET /api/setup/status, and the audit entry, which is where the design
// record sends diagnostics to look and where the line stays as history
// after evidence arrives.
func TestSetupMarkRecordsLedgerAndAudit(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{
		Step: 2, Outcome: "forced", Note: "no router has opened a syslog connection",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/mark = %d, want 200", resp.StatusCode)
	}

	statusResp, err := adminClient.Get(ts.URL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var got setupStatus
	if err := json.NewDecoder(statusResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Marks) != 1 {
		t.Fatalf("setup status carried %d marks, want 1 -- the ledger is what every empty state reads", len(got.Marks))
	}
	m := got.Marks[0]
	if m.Step != 2 || m.Outcome != setup.MarkForced {
		t.Errorf("mark = step %d %q, want step 2 forced", m.Step, m.Outcome)
	}
	if m.Actor != "admin" {
		t.Errorf("mark actor = %q, want the session's own username -- the body must never be able to sign for somebody else", m.Actor)
	}
	if !strings.Contains(m.Note, "syslog") {
		t.Errorf("mark note = %q, want what was not observed", m.Note)
	}

	entries := s.Audit.Query(audit.Query{})
	var found bool
	for _, e := range entries.Entries {
		if e.Action == "setup.step_forced" && e.Target == "step 2" && e.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Errorf("no setup.step_forced audit entry for step 2 in %+v -- \"visibly recorded where diagnostics can reach it\" is the issue's done-when", entries.Entries)
	}
}

// TestSetupMarkRejectsNonsense keeps the ledger to the records the
// wizard has (#1291 added the seventh, #1360 the eighth) and the two outcomes it
// defines. A mark outside that is a client bug or a probe; either way
// it has nothing to describe, and must not reach the audit log as
// though it did.
func TestSetupMarkRejectsNonsense(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)

	for _, tc := range []struct {
		name string
		req  setupMarkRequest
	}{
		{"step zero", setupMarkRequest{Step: 0, Outcome: "skipped"}},
		// Past the last record, whatever that is: #1291's register step
		// records as seven and #1360's first-run tail as eight
		// (setupsteps.ts's RECORD_NUMBERS), and a literal here pinned
		// the old ceiling rather than "past the last".
		{"step past the last", setupMarkRequest{Step: setup.MaxStep + 1, Outcome: "skipped"}},
		{"unknown outcome", setupMarkRequest{Step: 1, Outcome: "finished"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", tc.req)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("POST %+v = %d, want 400", tc.req, resp.StatusCode)
			}
		})
	}

	if n := len(s.Setup.Marks()); n != 0 {
		t.Errorf("%d marks recorded from refused requests, want 0", n)
	}
	// Scoped to setup.go's own two mark actions, not the whole log: #1253
	// makes setUpAdmin enrol the admin a confirmed factor before this
	// test body runs a single request, which legitimately writes its own
	// unrelated account.totp_enabled entry ahead of the refused calls
	// below.
	var markEntries int
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "setup.step_skipped" || e.Action == "setup.step_forced" {
			markEntries++
		}
	}
	if markEntries != 0 {
		t.Errorf("%d setup-mark audit entries written for refused requests, want 0", markEntries)
	}
}

// TestSetupMarkAcceptsTheSixthStep covers #1267: step 6 ("Back up the
// router", round 45/#394) used to be refused with "step must be 1-5",
// the same off-by-one TestSetupMarkRejectsNonsense's "past the last"
// case pinned at Step 6 rather than 7.
func TestSetupMarkAcceptsTheSixthStep(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{Step: 6, Outcome: "skipped"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/mark for step 6 = %d, want 200", resp.StatusCode)
	}
}

// TestSetupMarkRangeMessageMatchesMaxStep is #1304's F3: the refusal for
// a step outside the ledger's range used to hardcode "1-6" in this
// package while setup.MaxStep (the number that actually bounds a mark,
// per that constant's own comment) had already moved to 7 -- a second
// place carrying the step count, free to drift from the first. The
// message is now built from setup.MaxStep directly, so this test would
// catch a hardcoded literal here going stale again the way it did before
// #1291 added the seventh step.
func TestSetupMarkRangeMessageMatchesMaxStep(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{Step: setup.MaxStep + 1, Outcome: "skipped"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST step past the last = %d, want 400", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	want := fmt.Sprintf("step must be 1-%d and outcome one of skipped, forced\n", setup.MaxStep)
	if got := string(body); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestSetupMarkRejectsWitnessedOutcome: witnessed is a server-only
// outcome (#1221) -- a client that could write it could claim a step
// happened when it did not, so the one write path a client has must
// keep refusing it exactly as it refuses any other made-up outcome.
func TestSetupMarkRejectsWitnessedOutcome(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{Step: 1, Outcome: "witnessed"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST outcome=witnessed = %d, want 400", resp.StatusCode)
	}
	if n := len(s.Setup.Marks()); n != 0 {
		t.Errorf("%d marks recorded from a refused witnessed request, want 0", n)
	}
	if n := len(s.Setup.Witnessed()); n != 0 {
		t.Errorf("%d witnesses recorded from a refused witnessed request, want 0", n)
	}
}

// TestSetupStatusWitnessesLiveEvidence pins the write half of #1221:
// the moment handleSetupStatus can see step 1's evidence (a CA fetch)
// in the sources it just read, it remembers that, without waiting for
// anyone to ask.
func TestSetupStatusWitnessesLiveEvidence(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	s.Setup.NoteCAFetch("192.0.2.9", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	got := getSetupStatus(t, adminClient, ts.URL)

	if len(got.Witnesses) != 1 {
		t.Fatalf("Witnesses = %+v, want one entry for step 1", got.Witnesses)
	}
	if got.Witnesses[0].Step != 1 || !strings.Contains(got.Witnesses[0].Receipt, "192.0.2.9") {
		t.Errorf("witness = %+v, want step 1 naming the source that fetched the CA", got.Witnesses[0])
	}
}

// TestSetupStatusSyslogWitnessNeedsRealEvidenceNotJustAConnection is
// issue #1281's tightening of step 2's witness: before this issue, any
// address completing a TLS handshake satisfied it (NoteSyslogConnection
// alone), including one that never went on to send anything mikroview
// could attribute to the device being set up. A bare connection, with
// nothing enrolled and no configured device logging anything, must no
// longer witness the step; a device that has actually redeemed an
// enrolment token (AcceptedIP set) must.
func TestSetupStatusSyslogWitnessNeedsRealEvidenceNotJustAConnection(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	// A connection from an address nothing has enrolled -- the old
	// signal this issue removed.
	s.Setup.NoteSyslogConnection("198.51.100.9", time.Now())
	s.Devices = device.NewRegistry(nil)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	got := getSetupStatus(t, adminClient, ts.URL)
	for _, w := range got.Witnesses {
		if w.Step == 2 {
			t.Fatalf("step 2 witnessed on a bare, unattributed TLS connection alone: %+v", w)
		}
	}

	now := time.Now()
	if _, err := s.Devices.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Devices.MintEnrolment("hap-ax3", "198.51.100.9", now)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Devices.TryEnrol("198.51.100.9", []byte("mikroview-enrol "+token)) {
		t.Fatal("TryEnrol failed to redeem the freshly minted token")
	}

	got2 := getSetupStatus(t, adminClient, ts.URL)
	var sawStep2 bool
	for _, w := range got2.Witnesses {
		if w.Step == 2 {
			sawStep2 = true
			if !strings.Contains(w.Receipt, "198.51.100.9") {
				t.Errorf("step 2 receipt = %q, want it to name the enrolled address", w.Receipt)
			}
		}
	}
	if !sawStep2 {
		t.Fatalf("step 2 was not witnessed once the device was enrolled: %+v", got2.Witnesses)
	}
}

// TestSetupStatusWitnessOutlivesTheStoreThatSawIt is #1221's whole
// point, at the HTTP boundary: a witness written by one process is read
// back by a fresh one that never saw the router itself -- the ledger
// document is what makes that possible, not anything held in memory.
// Live evidence still wins where it exists: reconnecting after the
// "restart" below leaves the witness exactly as it was (the first
// observation, not a rewritten one) while Sources reports the fresh
// connection on its own.
func TestSetupStatusWitnessOutlivesTheStoreThatSawIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")

	before, err := setup.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before.NoteCAFetch("192.0.2.9", time.Now())

	s := newAuthTestServer(t)
	s.Setup = before
	ts := httptest.NewServer(s.Routes())
	adminClient := setUpAdmin(t, s, ts)
	// One read with the router still "connected", so the witness is
	// actually written before the process it lives in goes away.
	getSetupStatus(t, adminClient, ts.URL)
	ts.Close()

	// The "restart": a brand new Store, opened against the same
	// document, with none of the in-memory maps the first one built up.
	after, err := setup.Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	s2 := newAuthTestServer(t)
	s2.Setup = after
	ts2 := httptest.NewServer(s2.Routes())
	defer ts2.Close()
	adminClient2 := setUpAdmin(t, s2, ts2)

	got := getSetupStatus(t, adminClient2, ts2.URL)
	if len(got.Sources) != 0 {
		t.Errorf("Sources = %+v, want none -- the fresh store never saw a router", got.Sources)
	}
	if len(got.Witnesses) != 1 || got.Witnesses[0].Step != 1 {
		t.Fatalf("Witnesses = %+v, want step 1 to have survived the restart", got.Witnesses)
	}

	// Live evidence arrives again in the restarted process. It must not
	// disturb the witness (still the same receipt as before), and it
	// must show up in Sources on its own merits.
	after.NoteCAFetch("192.0.2.9", time.Now())
	got2 := getSetupStatus(t, adminClient2, ts2.URL)
	if len(got2.Sources) != 1 {
		t.Errorf("Sources after reconnecting = %+v, want the fresh connection reported", got2.Sources)
	}
	if len(got2.Witnesses) != 1 || got2.Witnesses[0].Receipt != got.Witnesses[0].Receipt {
		t.Errorf("witness changed after live evidence returned: %+v -> %+v, want it unchanged", got.Witnesses, got2.Witnesses)
	}
}

// TestSetupAddressAdminOnly pins handleSetupAddress's gate: the same
// tier as handleSetupMark beside it, since there is no read-only wizard
// and a viewer has no business changing what every RouterOS command in
// it is written against.
func TestSetupAddressAdminOnly(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: fixtureUserPassword, Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: fixtureUserPassword}).Body.Close()
	// Enrolled so the 403 below actually proves the admin-only gate,
	// rather than being masked by #1253's door refusing a still-factor-
	// less viewer for an unrelated reason.
	totpEnrolAndConfirm(t, viewerClient, ts, fixtureUserPassword)

	resp := postJSON(t, viewerClient, ts.URL+"/api/setup/address", setupAddressRequest{Address: "10.0.40.5:8443"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer POST /api/setup/address = %d, want 403", resp.StatusCode)
	}

	anonReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/setup/address", strings.NewReader(`{"address":"10.0.40.5:8443"}`))
	if err != nil {
		t.Fatal(err)
	}
	anonReq.Header.Set("Content-Type", "application/json")
	anonReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	anonResp, err := http.DefaultClient.Do(anonReq)
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed-out POST /api/setup/address = %d, want 401", anonResp.StatusCode)
	}

	if s.Setup.Address() != "" {
		t.Errorf("address = %q, want unset -- neither refused caller should have been able to write it", s.Setup.Address())
	}
}

// TestSetupAddressRejectsMalformed covers #1095's reasoning applied to
// this new field: the value is about to sit bare inside a RouterOS
// command, so a newline or a space must be refused rather than stored.
func TestSetupAddressRejectsMalformed(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)

	for _, bad := range []string{"", "10.0.40.5\nput another command here", "not a valid host", "10.0.40.5 8443"} {
		resp := postJSON(t, adminClient, ts.URL+"/api/setup/address", setupAddressRequest{Address: bad})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("address %q = %d, want 400", bad, resp.StatusCode)
		}
	}
	if s.Setup.Address() != "" {
		t.Errorf("address = %q, want unset -- every attempt above was refused", s.Setup.Address())
	}
}

// TestSetupAddressPersistsAndSurvivesReload pins #1213's whole point:
// the answer is stored beside the marks, so a restart mid-wizard does
// not lose it, and GET /api/setup/status and POST /api/setup/commands
// both read it back afterwards without being told again.
func TestSetupAddressPersistsAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	before, err := setup.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	s := newAuthTestServer(t)
	s.Setup = before
	ts := httptest.NewServer(s.Routes())
	adminClient := setUpAdmin(t, s, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/setup/address", setupAddressRequest{Address: "10.0.40.5:8443"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/address = %d, want 200", resp.StatusCode)
	}

	got := getSetupStatus(t, adminClient, ts.URL)
	if got.Instance.Address != "10.0.40.5:8443" {
		t.Errorf("Instance.Address = %q, want the value just stored", got.Instance.Address)
	}

	// The command endpoints fall back to the stored answer when the
	// caller omits one, the same way they already fall back to the
	// running configuration's syslog port. Built with the admin
	// client rather than postSetupCommands' bare http.Post, since this
	// server (unlike that helper's usual newTestServer fixture) has a
	// real auth store and needs a session and the CSRF header both.
	cmdsResp := postJSON(t, adminClient, ts.URL+"/api/setup/commands", setupCommandsRequest{})
	defer cmdsResp.Body.Close()
	if cmdsResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/commands = %d, want 200", cmdsResp.StatusCode)
	}
	var cmds setupCommandsResponse
	if err := json.NewDecoder(cmdsResp.Body).Decode(&cmds); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmds.Steps.CaTrust.Commands, "10.0.40.5:8443") {
		t.Errorf("caTrust commands = %q, want the stored address embedded", cmds.Steps.CaTrust.Commands)
	}
	ts.Close()

	// The "restart": a brand new Store, opened against the same document.
	after, err := setup.Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if after.Address() != "10.0.40.5:8443" {
		t.Errorf("address after reopening = %q, want it to have survived the restart", after.Address())
	}
}

// TestSetupBackupTransportAdminOnly pins handleSetupBackupTransport's
// gate (#955): the same tier as handleSetupAddress beside it, since this
// decides what every operator is told to paste into their router.
func TestSetupBackupTransportAdminOnly(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, s, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: fixtureUserPassword, Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: fixtureUserPassword}).Body.Close()
	// Enrolled so the 403 below actually proves the admin-only gate,
	// rather than being masked by #1253's door refusing a still-factor-
	// less viewer for an unrelated reason.
	totpEnrolAndConfirm(t, viewerClient, ts, fixtureUserPassword)

	resp := putJSON(t, viewerClient, ts.URL+"/api/setup/backup-transport", setupBackupTransportRequest{Transport: "https"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer PUT /api/setup/backup-transport = %d, want 403", resp.StatusCode)
	}

	anonReq, err := http.NewRequest(http.MethodPut, ts.URL+"/api/setup/backup-transport", strings.NewReader(`{"transport":"https"}`))
	if err != nil {
		t.Fatal(err)
	}
	anonReq.Header.Set("Content-Type", "application/json")
	anonReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	anonResp, err := http.DefaultClient.Do(anonReq)
	if err != nil {
		t.Fatal(err)
	}
	anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("signed-out PUT /api/setup/backup-transport = %d, want 401", anonResp.StatusCode)
	}

	if s.Setup.BackupTransport() != setup.BackupTransportSFTP {
		t.Errorf("transport = %q, want the sftp default -- neither refused caller should have been able to write it", s.Setup.BackupTransport())
	}

	// An admin may, and anything outside the two renderable values is
	// refused for them too.
	bad := putJSON(t, adminClient, ts.URL+"/api/setup/backup-transport", setupBackupTransportRequest{Transport: "ftp"})
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("admin PUT with transport=ftp = %d, want 400", bad.StatusCode)
	}
	ok := putJSON(t, adminClient, ts.URL+"/api/setup/backup-transport", setupBackupTransportRequest{Transport: "https"})
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("admin PUT /api/setup/backup-transport = %d, want 200", ok.StatusCode)
	}
	if s.Setup.BackupTransport() != setup.BackupTransportHTTPS {
		t.Errorf("transport = %q, want https", s.Setup.BackupTransport())
	}
}

// TestSetupBackupTransportPersistsAndSurvivesReload is #955's "the
// choice is a property of the deployment, not the browser": stored
// beside the address, read back by GET /api/setup/status and rendered by
// POST /api/setup/commands without being told again, and still there
// after a restart.
func TestSetupBackupTransportPersistsAndSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	before, err := setup.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	s := newAuthTestServer(t)
	s.Setup = before
	ts := httptest.NewServer(s.Routes())
	adminClient := setUpAdmin(t, s, ts)

	if got := getSetupStatus(t, adminClient, ts.URL); got.Instance.BackupTransport != setup.BackupTransportSFTP {
		t.Errorf("Instance.BackupTransport before any choice = %q, want sftp", got.Instance.BackupTransport)
	}

	resp := putJSON(t, adminClient, ts.URL+"/api/setup/backup-transport", setupBackupTransportRequest{Transport: "https"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/setup/backup-transport = %d, want 200", resp.StatusCode)
	}

	if got := getSetupStatus(t, adminClient, ts.URL); got.Instance.BackupTransport != setup.BackupTransportHTTPS {
		t.Errorf("Instance.BackupTransport = %q, want the value just stored", got.Instance.BackupTransport)
	}

	// The commands endpoint renders whichever is stored, with no field
	// in the request saying so.
	cmdsResp := postJSON(t, adminClient, ts.URL+"/api/setup/commands", setupCommandsRequest{Address: "10.0.40.5:8443", Token: "tok-123"})
	defer cmdsResp.Body.Close()
	if cmdsResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/commands = %d, want 200", cmdsResp.StatusCode)
	}
	var cmds setupCommandsResponse
	if err := json.NewDecoder(cmdsResp.Body).Decode(&cmds); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmds.Steps.Backup.Commands, "/api/ingest/router-backup") {
		t.Errorf("backup commands = %q, want the HTTPS push script", cmds.Steps.Backup.Commands)
	}
	ts.Close()

	// The "restart": a brand new Store, opened against the same document.
	after, err := setup.Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if got := after.BackupTransport(); got != setup.BackupTransportHTTPS {
		t.Errorf("transport after reopening = %q, want it to have survived the restart", got)
	}
}

// getSetupStatus is the shared GET /api/setup/status round trip these
// witness tests all need.
func getSetupStatus(t *testing.T, client *http.Client, baseURL string) setupStatus {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/setup/status = %d, want 200", resp.StatusCode)
	}
	var got setupStatus
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// getSetupStatusInstance reads GET /api/setup/status as a generic map so
// the #1507 tests below fail on the missing wire fields themselves
// rather than on a compile error.
func getSetupStatusInstance(t *testing.T, client *http.Client, baseURL string) map[string]any {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/setup/status = %d, want 200", resp.StatusCode)
	}
	var raw struct {
		Instance map[string]any `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	return raw.Instance
}

// TestSetupStatusServesTheCertificateCoverageVerdict is #1507 (decision
// #1437): Go owns the answer to "does the certificate cover the wizard's
// address", served as instance.certificateCoversAddress beside
// instance.effectiveHosts. Names compare case-insensitively, and a bare
// IPv6 literal is not cut at its last colon.
func TestSetupStatusServesTheCertificateCoverageVerdict(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hosts     []string
		address   string
		want      bool
		wantHosts []string
	}{
		{"uncovered name", []string{"router.example.net"}, "other.example.net:8443", false, []string{"router.example.net"}},
		{"covered name in a different case", []string{"router.example.net"}, "ROUTER.Example.NET:8443", true, []string{"router.example.net"}},
		{"covered name, configured in upper case", []string{"Router.EXAMPLE.net"}, "router.example.net", true, []string{"Router.EXAMPLE.net"}},
		{"covered bare IPv6 literal", []string{"2001:db8::10"}, "2001:db8::10", true, []string{"2001:db8::10"}},
		{"covered bracketed IPv6 with port", []string{"2001:db8::10"}, "[2001:db8::10]:8443", true, []string{"2001:db8::10"}},
		{"uncovered bare IPv6 literal", []string{"192.0.2.10"}, "2001:db8::10", false, []string{"192.0.2.10"}},
		{"unset hosts fall back to the generated defaults", nil, "127.0.0.1:8443", true, []string{"localhost", "127.0.0.1"}},
		{"unset hosts do not cover another address", nil, "192.0.2.99:8443", false, []string{"localhost", "127.0.0.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthTestServer(t)
			s.Setup = setup.New()
			s.SetupInstance = SetupInstance{TLSEnabled: true, Hosts: tc.hosts, SyslogPort: "6514"}
			if _, err := s.Setup.SetAddress(tc.address); err != nil {
				t.Fatalf("SetAddress: %v", err)
			}
			ts := httptest.NewServer(s.Routes())
			defer ts.Close()
			adminClient := setUpAdmin(t, s, ts)

			inst := getSetupStatusInstance(t, adminClient, ts.URL)
			got, ok := inst["certificateCoversAddress"].(bool)
			if !ok {
				t.Fatalf("instance.certificateCoversAddress missing or not a bool: %v", inst["certificateCoversAddress"])
			}
			if got != tc.want {
				t.Errorf("certificateCoversAddress = %v, want %v", got, tc.want)
			}
			eff, _ := inst["effectiveHosts"].([]any)
			var gotHosts []string
			for _, h := range eff {
				gotHosts = append(gotHosts, fmt.Sprint(h))
			}
			if strings.Join(gotHosts, ",") != strings.Join(tc.wantHosts, ",") {
				t.Errorf("effectiveHosts = %v, want %v", gotHosts, tc.wantHosts)
			}
		})
	}
}

// TestSetupStatusCoverageVerdictIsTrueWhenNoCertificateIsInPlay keeps
// the front end's old short-circuit (#374): with neither HTTP TLS nor
// syslog TLS on there is nothing to cover.
func TestSetupStatusCoverageVerdictIsTrueWhenNoCertificateIsInPlay(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	s.SetupInstance = SetupInstance{TLSEnabled: false, Hosts: []string{"192.0.2.10"}, SyslogPort: ""}
	if _, err := s.Setup.SetAddress("192.0.2.99:8443"); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	adminClient := setUpAdmin(t, s, ts)

	inst := getSetupStatusInstance(t, adminClient, ts.URL)
	if got, ok := inst["certificateCoversAddress"].(bool); !ok || !got {
		t.Errorf("certificateCoversAddress = %v, want true when no certificate is in play", inst["certificateCoversAddress"])
	}
}

// getSetupStatusInstanceFor is getSetupStatusInstance with the typed
// address passed as the status's optional address query parameter.
func getSetupStatusInstanceFor(t *testing.T, client *http.Client, baseURL, typed string) map[string]any {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/setup/status?address=" + url.QueryEscape(typed))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/setup/status?address=%s = %d, want 200", typed, resp.StatusCode)
	}
	var raw struct {
		Instance map[string]any `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	return raw.Instance
}

// TestSetupStatusJudgesTheTypedAddress is #1507's review decision: the
// status takes an optional address query parameter, and addressHost,
// effectiveHosts and certificateCoversAddress are then computed for it
// rather than for the stored address. An invalid typed address is
// treated like none; with no parameter the stored address is used.
func TestSetupStatusJudgesTheTypedAddress(t *testing.T) {
	const stored = "router.example.net:8443" // covered by tls.hosts below
	for _, tc := range []struct {
		name         string
		query        bool
		typed        string
		wantCovers   bool
		wantHost     string
		wantHostsAre []string
	}{
		{"no parameter uses the stored address", false, "", true, "router.example.net", []string{"router.example.net"}},
		{"uncovered typed name overrides a covered stored one", true, "other.example.net:8443", false, "other.example.net", []string{"router.example.net"}},
		{"covered typed name in another case", true, "ROUTER.example.net", true, "ROUTER.example.net", []string{"router.example.net"}},
		{"bare IPv6 typed address gives the whole literal", true, "2001:db8::10", false, "2001:db8::10", []string{"router.example.net"}},
		{"bracketed IPv6 typed address with port", true, "[2001:db8::10]:8443", false, "2001:db8::10", []string{"router.example.net"}},
		{"invalid typed address is treated like none", true, "not a valid host\naddress", true, "router.example.net", []string{"router.example.net"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthTestServer(t)
			s.Setup = setup.New()
			s.SetupInstance = SetupInstance{TLSEnabled: true, Hosts: []string{"router.example.net"}, SyslogPort: "6514"}
			if _, err := s.Setup.SetAddress(stored); err != nil {
				t.Fatalf("SetAddress: %v", err)
			}
			ts := httptest.NewServer(s.Routes())
			defer ts.Close()
			adminClient := setUpAdmin(t, s, ts)

			var inst map[string]any
			if tc.query {
				inst = getSetupStatusInstanceFor(t, adminClient, ts.URL, tc.typed)
			} else {
				inst = getSetupStatusInstance(t, adminClient, ts.URL)
			}
			if got, _ := inst["certificateCoversAddress"].(bool); got != tc.wantCovers {
				t.Errorf("certificateCoversAddress = %v, want %v", inst["certificateCoversAddress"], tc.wantCovers)
			}
			if got, _ := inst["addressHost"].(string); got != tc.wantHost {
				t.Errorf("addressHost = %q, want %q", got, tc.wantHost)
			}
			eff, _ := inst["effectiveHosts"].([]any)
			var gotHosts []string
			for _, h := range eff {
				gotHosts = append(gotHosts, fmt.Sprint(h))
			}
			if strings.Join(gotHosts, ",") != strings.Join(tc.wantHostsAre, ",") {
				t.Errorf("effectiveHosts = %v, want %v", gotHosts, tc.wantHostsAre)
			}
		})
	}
}

// With nothing stored, a typed uncovered address is still judged: the
// browser's check used to block before the address was ever saved.
func TestSetupStatusJudgesATypedAddressWithNothingStored(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	s.SetupInstance = SetupInstance{TLSEnabled: true, Hosts: []string{"router.example.net"}, SyslogPort: "6514"}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	adminClient := setUpAdmin(t, s, ts)

	inst := getSetupStatusInstanceFor(t, adminClient, ts.URL, "192.0.2.99:8443")
	if got, _ := inst["certificateCoversAddress"].(bool); got {
		t.Error("certificateCoversAddress = true, want false for an uncovered typed address")
	}
	if got, _ := inst["addressHost"].(string); got != "192.0.2.99" {
		t.Errorf("addressHost = %q, want 192.0.2.99", got)
	}
}
