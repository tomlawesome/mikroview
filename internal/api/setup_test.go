// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

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

	adminClient := setUpAdmin(t, ts)

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

// TestSetupMarkRejectsNonsense keeps the ledger to the six steps the
// wizard has (round 45/#394 added the sixth) and the two outcomes it
// defines. A mark outside that is a client bug or a probe; either way
// it has nothing to describe, and must not reach the audit log as
// though it did.
func TestSetupMarkRejectsNonsense(t *testing.T) {
	s := newAuthTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)

	for _, tc := range []struct {
		name string
		req  setupMarkRequest
	}{
		{"step zero", setupMarkRequest{Step: 0, Outcome: "skipped"}},
		{"step past the last", setupMarkRequest{Step: 7, Outcome: "skipped"}},
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
	if n := len(s.Audit.Query(audit.Query{}).Entries); n != 0 {
		t.Errorf("%d audit entries written for refused requests, want 0", n)
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

	adminClient := setUpAdmin(t, ts)
	resp := postJSON(t, adminClient, ts.URL+"/api/setup/mark", setupMarkRequest{Step: 6, Outcome: "skipped"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/mark for step 6 = %d, want 200", resp.StatusCode)
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

	adminClient := setUpAdmin(t, ts)

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

	adminClient := setUpAdmin(t, ts)
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

	adminClient := setUpAdmin(t, ts)
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
	token, _, err := s.Devices.MintEnrolment("hap-ax3", now)
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
	adminClient := setUpAdmin(t, ts)
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
	adminClient2 := setUpAdmin(t, ts2)

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

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

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

	adminClient := setUpAdmin(t, ts)

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
	adminClient := setUpAdmin(t, ts)

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

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

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
	adminClient := setUpAdmin(t, ts)

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
