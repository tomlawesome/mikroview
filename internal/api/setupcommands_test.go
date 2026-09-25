// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// testRetentionKey builds a usable retention key for tests that need
// one -- mirrors internal/retention's and internal/backupvault's own
// test helpers of the same name and shape, kept local here since this
// package cannot import a _test.go file from another package.
func testRetentionKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func postSetupCommands(t *testing.T, base string, req setupCommandsRequest) setupCommandsResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/api/setup/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/setup/commands: status = %d", resp.StatusCode)
	}
	var out setupCommandsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestHandleSetupCommandsBlanksEveryAddressDependentBlockWithNoAddress
// covers #1213: an empty address is no longer refused at the door (that
// used to be the whole point of the field the operator's browser filled
// in for them). It is instead read the same way every other missing
// precondition here is -- every block that embeds it comes back blank
// with the "no-address" key, RuleTagging (which needs no address at all)
// renders regardless, and the request itself still succeeds.
func TestHandleSetupCommandsBlanksEveryAddressDependentBlockWithNoAddress(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetupInstance.BackupPort = "47022"
	key := testRetentionKey(t)
	v, err := backupvault.Open(t.TempDir(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Vault = v
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	// Every other precondition met, so if address were not itself gating
	// these blocks, they would render.
	out := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Token: "tok-123", Kinds: []string{"filter-rule"}, Device: "rb5009",
	})
	for name, step := range map[string]commandStep{
		"caTrust":  out.Steps.CaTrust,
		"syslog":   out.Steps.Syslog,
		"push":     out.Steps.Push,
		"schedule": out.Steps.Schedule,
	} {
		if step.Commands != "" {
			t.Errorf("%s commands = %q, want empty with no address", name, step.Commands)
		}
		if !slicesEqual(step.Blocked, []string{"no-address"}) {
			t.Errorf("%s.Blocked = %v, want [no-address]", name, step.Blocked)
		}
	}
	if !slicesEqual(out.Steps.Backup.Blocked, []string{"no-address"}) {
		t.Errorf("Backup.Blocked = %v, want [no-address] with every other precondition met", out.Steps.Backup.Blocked)
	}
	if !slicesEqual(out.Steps.BackupSchedule.Blocked, []string{"no-address"}) {
		t.Errorf("BackupSchedule.Blocked = %v, want [no-address]", out.Steps.BackupSchedule.Blocked)
	}
	if out.Steps.RuleTagging.Commands == "" {
		t.Error("RuleTagging.Commands is empty, want it to render regardless -- it embeds no address")
	}
}

// TestHandleSetupCommandsRejectsAMalformedAddress covers the other half
// of #1213's contract change: empty is now fine (not answered yet), but
// a non-empty value still has to be a plausible address, since it is
// about to sit bare inside a RouterOS command.
func TestHandleSetupCommandsRejectsAMalformedAddress(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	body, _ := json.Marshal(setupCommandsRequest{Address: "not a valid host\naddress"})
	resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a malformed address", resp.StatusCode)
	}
}

// TestHandleSetupCommandsNoVersionPicked covers the "picked": null case
// and that the table/steps still render against the single existing
// dialect with nothing chosen.
func TestHandleSetupCommandsNoVersionPicked(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443"})

	if out.Picked != nil {
		t.Errorf("Picked = %+v, want nil when no version was sent", out.Picked)
	}
	// Compared against the dialect table rather than against literals:
	// what this endpoint owes the caller is the table's own bounds and
	// its own rows, so that is the thing to assert. Literals here meant
	// every added row broke an API test that had nothing to say about
	// the change -- and the fix for that is always to bump the number,
	// which tests nothing.
	if out.RouterOS.Minimum != routeros.MinimumVersion || out.RouterOS.Newest != routeros.NewestVersion() {
		t.Errorf("RouterOS bounds = %+v, want minimum %q and newest %q",
			out.RouterOS, routeros.MinimumVersion, routeros.NewestVersion())
	}
	if len(out.RouterOS.Rows) != len(routeros.Rows) {
		t.Fatalf("RouterOS.Rows has %d entries, want %d -- the endpoint must surface every row",
			len(out.RouterOS.Rows), len(routeros.Rows))
	}
	if !strings.Contains(out.Steps.CaTrust.Commands, "https://mv.example.net:8443/ca.crt") {
		t.Errorf("caTrust commands missing the address: %s", out.Steps.CaTrust.Commands)
	}
	if out.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with no token/kinds", out.Steps.Push.Commands)
	}
	if out.Steps.RuleTagging.Note != "" {
		t.Errorf("ruleTagging note = %q, want empty with no version picked", out.Steps.RuleTagging.Note)
	}
}

// TestHandleSetupCommandsPickedVersion covers a version the operator
// picked from the list: its standing, its row's dialect, and -- for
// 7.24.0 specifically -- the row's note surfacing on the ruleTagging
// step.
func TestHandleSetupCommandsPickedVersion(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.24"})

	if out.Picked == nil {
		t.Fatal("Picked = nil, want the resolved pick")
	}
	if out.Picked.Version != "7.24" || out.Picked.Standing != "reviewed" || out.Picked.Dialect != "a" {
		t.Errorf("Picked = %+v, want version 7.24, standing reviewed, dialect a", out.Picked)
	}
	if !strings.Contains(out.Steps.RuleTagging.Note, "find") {
		t.Errorf("ruleTagging note = %q, want 7.24's find-lookup-bug note", out.Steps.RuleTagging.Note)
	}
}

// TestHandleSetupCommandsPickedVersionOutsideEveryRow covers a version
// no row covers -- below the floor and ahead of the newest row both --
// which must still resolve to a standing and a (fallback) dialect, never
// an error: the design record says this never blocks.
func TestHandleSetupCommandsPickedVersionOutsideEveryRow(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	below := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.16"})
	if below.Picked == nil || below.Picked.Standing != "below-minimum" || below.Picked.Dialect != "a" {
		t.Errorf("Picked (below floor) = %+v, want standing below-minimum, dialect a (the fallback)", below.Picked)
	}

	ahead := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443", Version: "7.99"})
	if ahead.Picked == nil || ahead.Picked.Standing != "ahead-of-review" || ahead.Picked.Dialect != "a" {
		t.Errorf("Picked (ahead of newest) = %+v, want standing ahead-of-review, dialect a (the fallback)", ahead.Picked)
	}
}

// TestHandleSetupCommandsListsRoutersWithKnownVersions covers the
// "routers" list: only routers whose version is known appear at all, and
// each carries its own standing and row note.
func TestHandleSetupCommandsListsRoutersWithKnownVersions(t *testing.T) {
	s, _ := newTestServer(t)
	p, err := ingest.DecodePayload(strings.NewReader(
		`{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.24","records":[{"address":"192.0.2.50","mac":"aa:bb:cc:dd:ee:01"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", p, time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.mux())
	defer ts.Close()
	out := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "mv.example.net:8443"})

	if len(out.Routers) != 1 {
		t.Fatalf("Routers has %d entries, want exactly the one device with a known version: %+v", len(out.Routers), out.Routers)
	}
	r := out.Routers[0]
	if r.ID != "core" || r.RouterOSVersion != "7.24" || r.Standing != "reviewed" {
		t.Errorf("Routers[0] = %+v, want core at 7.24, reviewed", r)
	}
	if !strings.Contains(r.Note, "find") {
		t.Errorf("Routers[0].Note = %q, want 7.24's find-lookup-bug note carried per-router", r.Note)
	}
}

// TestHandleSetupCommandsPushRendersOnlyWithTokenAndKinds covers the
// contract's rule that push commands are empty when no token/kinds are
// given: both must be present for a push script to render at all.
func TestHandleSetupCommandsPushRendersOnlyWithTokenAndKinds(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	tokenOnly := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Token: "tok-123"})
	if tokenOnly.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with a token but no kinds", tokenOnly.Steps.Push.Commands)
	}

	kindsOnly := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Kinds: []string{"filter-rule"}})
	if kindsOnly.Steps.Push.Commands != "" {
		t.Errorf("push commands = %q, want empty with kinds but no token", kindsOnly.Steps.Push.Commands)
	}

	both := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "h", Token: "tok-123", Kinds: []string{"filter-rule"}})
	if !strings.Contains(both.Steps.Push.Commands, "Bearer tok-123") {
		t.Errorf("push commands = %q, want the token embedded", both.Steps.Push.Commands)
	}

	// Schedule carries the script itself since #1131, so it follows
	// Push exactly: blank when there is nothing to save, and the whole
	// pastable block when there is.
	if tokenOnly.Steps.Schedule.Commands != "" || kindsOnly.Steps.Schedule.Commands != "" {
		t.Errorf("schedule commands rendered with nothing to schedule: %q / %q",
			tokenOnly.Steps.Schedule.Commands, kindsOnly.Steps.Schedule.Commands)
	}
	schedule := both.Steps.Schedule.Commands
	if !strings.HasPrefix(schedule, `:if ([:len [/system script find name=mv-push]] = 0) do={ /system script add name=mv-push policy=read,test source="`) {
		t.Errorf("schedule commands = %q, want the guarded script add that saves the push script", schedule)
	}
	if !strings.Contains(schedule, "Bearer tok-123") || !strings.HasSuffix(schedule, "\n/system script run mv-push") {
		t.Errorf("schedule commands = %q, want the push script inside it and one run now", schedule)
	}
	if strings.Contains(schedule, "paste the script") {
		t.Errorf("schedule commands still ask the operator to paste a script in: %q", schedule)
	}
}

// TestHandleSetupCommandsRendersTheEnrolLineWithAVerifiedToken is issue
// #1281's contract for step 2: the "Send logs" block ends with the
// enrolment line once the caller's echoed EnrolToken actually matches
// the named device's current pending token.
func TestHandleSetupCommandsRendersTheEnrolLineWithAVerifiedToken(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	if _, err := s.Devices.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Devices.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "mv.example.com", Device: "hap-ax3", EnrolToken: token,
	})
	want := `/log info "mikroview-enrol ` + token + `"`
	if !strings.HasSuffix(out.Steps.Syslog.Commands, want) {
		t.Errorf("syslog commands = %q, want it to end with %q", out.Steps.Syslog.Commands, want)
	}
}

// TestHandleSetupCommandsOmitsTheEnrolLineWithoutAVerifiedToken covers
// every way the check can fail closed: no token sent, a token that
// names no pending record, a token for a different device, and a device
// with nothing pending at all. Every case must render step 2 exactly as
// it did before #1281 -- no enrolment line, nothing else changed.
func TestHandleSetupCommandsOmitsTheEnrolLineWithoutAVerifiedToken(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	if _, err := s.Devices.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices.Create("other", "other", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Devices.MintEnrolment("hap-ax3", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := s.Devices.MintEnrolment("other", "10.10.0.1", now)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		req  setupCommandsRequest
	}{
		{"no enrolToken at all", setupCommandsRequest{Address: "mv.example.com", Device: "hap-ax3"}},
		{"no device named", setupCommandsRequest{Address: "mv.example.com", EnrolToken: token}},
		{"an unrelated, well-formed token", setupCommandsRequest{Address: "mv.example.com", Device: "hap-ax3", EnrolToken: "zzzzzzzzzzzzzzzzzzzz"}},
		{"another device's real pending token", setupCommandsRequest{Address: "mv.example.com", Device: "hap-ax3", EnrolToken: otherToken}},
		{"a device with nothing pending", setupCommandsRequest{Address: "mv.example.com", Device: "core"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := postSetupCommands(t, ts.URL, tc.req)
			if strings.Contains(out.Steps.Syslog.Commands, "mikroview-enrol") {
				t.Errorf("syslog commands = %q, want no enrolment line", out.Steps.Syslog.Commands)
			}
		})
	}
}

// TestHandleSetupCommandsBackupRendersOnlyWhenReady covers #394's
// step 6: the script needs a device, a token, the drop box turned on
// (BackupPort set) and a retention key (Vault.Enabled()) all at once --
// any one missing leaves it blank, which is what the wizard reads as
// its "no key" state.
func TestHandleSetupCommandsBackupRendersOnlyWhenReady(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetupInstance.BackupPort = "47022"
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	// No vault at all (nil): the zero-value newTestServer fixture,
	// same as a build with no retention key configured.
	noKey := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "10.0.40.5", Token: "tok-123", Device: "rb5009",
	})
	if noKey.Steps.Backup.Commands != "" || noKey.Steps.BackupSchedule.Commands != "" {
		t.Errorf("backup commands rendered with no retention key: %+v", noKey.Steps)
	}

	key := testRetentionKey(t)
	v, err := backupvault.Open(t.TempDir(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Vault = v

	noDevice := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5", Token: "tok-123"})
	if noDevice.Steps.Backup.Commands != "" {
		t.Errorf("backup commands rendered with no device name: %q", noDevice.Steps.Backup.Commands)
	}

	ready := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "10.0.40.5:8443", Token: "tok-123", Device: "rb5009",
	})
	if !strings.Contains(ready.Steps.Backup.Commands, "user=rb5009") ||
		!strings.Contains(ready.Steps.Backup.Commands, "port=47022") ||
		!strings.Contains(ready.Steps.Backup.Commands, "address=10.0.40.5 ") {
		t.Errorf("backup commands = %q, want the device, port and host embedded", ready.Steps.Backup.Commands)
	}
	if !strings.Contains(ready.Steps.BackupSchedule.Commands, "mv-backup") {
		t.Errorf("backup schedule commands = %q, want the scheduler entry", ready.Steps.BackupSchedule.Commands)
	}
	if len(ready.Steps.Backup.Blocked) != 0 {
		t.Errorf("Backup.Blocked = %v, want none once every precondition is met", ready.Steps.Backup.Blocked)
	}
	if len(ready.Steps.BackupSchedule.Blocked) != 0 {
		t.Errorf("BackupSchedule.Blocked = %v, want none once every precondition is met", ready.Steps.BackupSchedule.Blocked)
	}
}

// TestHandleSetupCommandsBackupBlockedKeys covers #1217: the server
// names every missing precondition as a machine-readable key, all that
// apply rather than just the first, so the wizard can say why the
// backup step printed nothing instead of showing empty boxes. Wording
// stays out of Go entirely -- these keys are exactly what the frontend
// switches on.
func TestHandleSetupCommandsBackupBlockedKeys(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	// Nothing present at all: every key fires, in the order the wizard
	// wants them read -- config problems first, then the two the
	// operator fixes in the wizard.
	none := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5"})
	want := []string{"backups-off", "no-retention-key", "no-device", "no-token"}
	if !slicesEqual(none.Steps.Backup.Blocked, want) {
		t.Errorf("Backup.Blocked = %v, want %v", none.Steps.Backup.Blocked, want)
	}
	if !slicesEqual(none.Steps.BackupSchedule.Blocked, want) {
		t.Errorf("BackupSchedule.Blocked = %v, want %v (same condition as Backup)", none.Steps.BackupSchedule.Blocked, want)
	}

	// Exactly one precondition missing at a time -- the owner's actual
	// case (#1217's correction note): backups switched off in config,
	// everything else present.
	key := testRetentionKey(t)
	v, err := backupvault.Open(t.TempDir(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Vault = v
	s.SetupInstance.BackupPort = ""
	onlyBackupsOff := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "10.0.40.5", Token: "tok-123", Device: "rb5009",
	})
	if !slicesEqual(onlyBackupsOff.Steps.Backup.Blocked, []string{"backups-off"}) {
		t.Errorf("Backup.Blocked = %v, want just [backups-off]", onlyBackupsOff.Steps.Backup.Blocked)
	}

	s.SetupInstance.BackupPort = "47022"
	onlyNoDevice := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5", Token: "tok-123"})
	if !slicesEqual(onlyNoDevice.Steps.Backup.Blocked, []string{"no-device"}) {
		t.Errorf("Backup.Blocked = %v, want just [no-device]", onlyNoDevice.Steps.Backup.Blocked)
	}

	onlyNoToken := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5", Device: "rb5009"})
	if !slicesEqual(onlyNoToken.Steps.Backup.Blocked, []string{"no-token"}) {
		t.Errorf("Backup.Blocked = %v, want just [no-token]", onlyNoToken.Steps.Backup.Blocked)
	}

	// Several missing at once -- the owner's instance actually hit this:
	// two preconditions unmet together, and both keys must come back,
	// not just the first one found.
	s.SetupInstance.BackupPort = ""
	several := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5", Token: "tok-123"})
	if !slicesEqual(several.Steps.Backup.Blocked, []string{"backups-off", "no-device"}) {
		t.Errorf("Backup.Blocked = %v, want [backups-off no-device]", several.Steps.Backup.Blocked)
	}
}

// TestHandleSetupCommandsBackupBlockedRetentionKeyUnreadable covers #1264
// finding 5: Vault.Enabled() being false covers both "no key configured"
// and "a key is configured but could not be read" -- so the blocked key
// must come from SetupInstance.BackupKeyUnreadable, not from Enabled()
// alone, or an operator whose key merely failed to load gets told the
// same "no-retention-key, set one" line as one who never had a key --
// and following it (minting a fresh key) strands every backup already
// encrypted under the old one.
func TestHandleSetupCommandsBackupBlockedRetentionKeyUnreadable(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetupInstance.BackupPort = "47022"
	s.SetupInstance.BackupKeyUnreadable = true
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "10.0.40.5", Token: "tok-123", Device: "rb5009",
	})
	want := []string{"retention-key-unreadable"}
	if !slicesEqual(out.Steps.Backup.Blocked, want) {
		t.Errorf("Backup.Blocked = %v, want %v -- a broken key must never be reported as no-retention-key", out.Steps.Backup.Blocked, want)
	}
	if !slicesEqual(out.Steps.BackupSchedule.Blocked, want) {
		t.Errorf("BackupSchedule.Blocked = %v, want %v (same condition as Backup)", out.Steps.BackupSchedule.Blocked, want)
	}
}

// TestHandleSetupCommandsHTTPSTransport covers #955's ruling: with
// "https" stored, step 6's two blocks are the slice-push script and its
// scheduler entry instead of the SFTP pair -- and they are not held back
// by the drop box's own preconditions, because the push goes through the
// ingest channel that is already open. Its only precondition beyond the
// address is the token, the same one step 4 has.
func TestHandleSetupCommandsHTTPSTransport(t *testing.T) {
	s, _ := newTestServer(t)
	s.Setup = setup.New()
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	// The default, with nothing chosen: the SFTP pair, which needs the
	// drop box turned on, a retention key and a device -- none of which
	// this fixture has.
	sftp := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5:8443", Token: "tok-123"})
	if !slicesEqual(sftp.Steps.Backup.Blocked, []string{"backups-off", "no-retention-key", "no-device"}) {
		t.Errorf("Backup.Blocked on the default transport = %v, want the drop box's own preconditions", sftp.Steps.Backup.Blocked)
	}

	if ok, err := s.Setup.SetBackupTransport(setup.BackupTransportHTTPS); err != nil || !ok {
		t.Fatalf("SetBackupTransport refused https: ok=%v err=%v", ok, err)
	}
	https := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5:8443", Token: "tok-123"})
	if len(https.Steps.Backup.Blocked) != 0 || len(https.Steps.BackupSchedule.Blocked) != 0 {
		t.Errorf("Backup.Blocked = %v / BackupSchedule.Blocked = %v, want none: the HTTPS push waits on nothing the drop box needs",
			https.Steps.Backup.Blocked, https.Steps.BackupSchedule.Blocked)
	}
	if !strings.Contains(https.Steps.Backup.Commands, "https://10.0.40.5:8443/api/ingest/router-backup") ||
		!strings.Contains(https.Steps.Backup.Commands, "Bearer tok-123") {
		t.Errorf("backup commands = %q, want the ingest URL and token embedded", https.Steps.Backup.Commands)
	}
	if strings.Contains(https.Steps.Backup.Commands, "mode=sftp") {
		t.Errorf("backup commands = %q, want no SFTP upload in the HTTPS script", https.Steps.Backup.Commands)
	}
	if !strings.Contains(https.Steps.BackupSchedule.Commands, "mv-backup-https") {
		t.Errorf("backup schedule commands = %q, want the mv-backup-https scheduler entry", https.Steps.BackupSchedule.Commands)
	}

	// The token is the one thing it does still wait on, the same as
	// step 4 -- and the address, which every block here needs.
	noToken := postSetupCommands(t, ts.URL, setupCommandsRequest{Address: "10.0.40.5:8443"})
	if !slicesEqual(noToken.Steps.Backup.Blocked, []string{"no-token"}) {
		t.Errorf("Backup.Blocked with no token = %v, want just [no-token]", noToken.Steps.Backup.Blocked)
	}
	noAddress := postSetupCommands(t, ts.URL, setupCommandsRequest{Token: "tok-123"})
	if !slicesEqual(noAddress.Steps.Backup.Blocked, []string{"no-address"}) {
		t.Errorf("Backup.Blocked with no address = %v, want just [no-address]", noAddress.Steps.Backup.Blocked)
	}
	if noAddress.Steps.Backup.Commands != "" {
		t.Errorf("backup commands with no address = %q, want blank", noAddress.Steps.Backup.Commands)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestHandleSetupCommandsRejectsUnsafeInput covers #1095: every field
// that reaches routeros' command templates must be rejected with 400
// before it can change the structure of the RouterOS commands an
// operator pastes verbatim -- a '"', '\', ';', space or newline in
// device or address, and a syslogPort outside 1..65535.
func TestHandleSetupCommandsRejectsUnsafeInput(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	cases := []struct {
		name string
		req  setupCommandsRequest
	}{
		{"device with a quote", setupCommandsRequest{Address: "mv.example.com", Device: `router"1`}},
		{"device with a backslash", setupCommandsRequest{Address: "mv.example.com", Device: `router\1`}},
		{"device with a semicolon", setupCommandsRequest{Address: "mv.example.com", Device: "router;1"}},
		{"device with a space", setupCommandsRequest{Address: "mv.example.com", Device: "router 1"}},
		{"device with a newline", setupCommandsRequest{Address: "mv.example.com", Device: "router\n1"}},
		{"address with a quote", setupCommandsRequest{Address: `mv.example.com"`}},
		{"address with a space", setupCommandsRequest{Address: "mv example.com"}},
		{"address with a semicolon", setupCommandsRequest{Address: "mv.example.com;reboot"}},
		// RouterOS expands $name inside a double-quoted string, which is
		// where Token lands twice (#1095 re-check on v0.5.0).
		{"token with a dollar", setupCommandsRequest{Address: "mv.example.com", Token: "abc$def"}},
		{"token with a quote", setupCommandsRequest{Address: "mv.example.com", Token: `abc"def`}},
		{"token with a backslash", setupCommandsRequest{Address: "mv.example.com", Token: `abc\def`}},
		// A comma in Token reaches PushBlock/loggingPushBlock's
		// http-header-field=("Content-Type: ...,Authorization: Bearer
		// <token>") bare -- RouterOS splits that value on commas into
		// separate headers regardless of quoting, so a comma there adds
		// a header rather than merely appearing inside one's value.
		{"token with a comma", setupCommandsRequest{Address: "mv.example.com", Token: "a,b"}},
		{"token with a colon and comma, header-injection shaped", setupCommandsRequest{Address: "mv.example.com", Token: "a:b,X-Injected:1"}},
		{"syslogPort non-numeric", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "abc"}},
		{"syslogPort zero", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "0"}},
		{"syslogPort out of range", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "70000"}},
		{"syslogPort listen address, bad port", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "127.0.0.1:abc"}},
		{"syslogPort listen address, zero port", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "[::]:0"}},
		// #1281's enrolment token: exactly 20 lowercase letters/digits,
		// nothing else.
		{"enrolToken too short", setupCommandsRequest{Address: "mv.example.com", EnrolToken: "abc123"}},
		{"enrolToken uppercase", setupCommandsRequest{Address: "mv.example.com", EnrolToken: "AAAAAAAAAAAAAAAAAAAA"}},
		{"enrolToken with a quote", setupCommandsRequest{Address: "mv.example.com", EnrolToken: `abcdefghijklmnopqrs"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestHandleSetupCommandsAcceptsSafeInput is the positive case beside
// TestHandleSetupCommandsRejectsUnsafeInput: ordinary values in every
// validated field must still render normally.
func TestHandleSetupCommandsAcceptsSafeInput(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	out := postSetupCommands(t, ts.URL, setupCommandsRequest{
		Address: "mv.example.com", SyslogPort: "5514", Device: "router-1.lan",
	})
	if !strings.Contains(out.Steps.CaTrust.Commands, "mv.example.com") {
		t.Errorf("caTrust commands missing the address: %s", out.Steps.CaTrust.Commands)
	}

	// An auto-discovered device's id is its source address
	// (internal/device.Registry.Resolve), so an IPv6 router's id carries
	// colons the charset rule alone would refuse.
	for _, device := range []string{"fd00::1", "192.0.2.7"} {
		body, err := json.Marshal(setupCommandsRequest{Address: "mv.example.com", Device: device})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("device %q: status = %d, want 200", device, resp.StatusCode)
		}
	}

	// Tokens internal/auth actually mints are hex; validSetupToken's
	// charset also allows '-' and '_' so a hand-entered or future token
	// shape in that alphabet is not refused.
	for _, token := range []string{"deadbeef1234567890abcdef12345678", "abc-123_XYZ"} {
		body, err := json.Marshal(setupCommandsRequest{Address: "mv.example.com", Token: token})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("token %q: status = %d, want 200", token, resp.StatusCode)
		}
	}

	// The wizard round-trips GET /api/setup/status's Instance.SyslogPort,
	// which is the raw listen.syslogTls address (main.go), not a bare
	// port: ":6514" in the shipped config, "127.0.0.1:16823" under
	// scripts/live-env.sh. Only the port part is templated (routeros.PortOf).
	for _, port := range []string{":6514", "127.0.0.1:16823", "0.0.0.0:6514", "[::]:6514"} {
		body, err := json.Marshal(setupCommandsRequest{Address: "mv.example.com", SyslogPort: port})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("syslogPort %q: status = %d, want 200", port, resp.StatusCode)
		}
	}
}

// TestValidSetupDeviceLengthMatchesTokenStore is #1304's Q9: this
// validator's device-name length bound used to be a second constant
// (maxSetupDeviceLen) kept equal to internal/auth's own
// maxDeviceIDLen only by a comment saying so. It is auth.MaxDeviceIDLen
// itself now, so this test pins the boundary against that shared
// constant directly -- it would catch either one changing without the
// other the moment they next needed to differ.
func TestValidSetupDeviceLengthMatchesTokenStore(t *testing.T) {
	atLimit := strings.Repeat("a", auth.MaxDeviceIDLen)
	if !validSetupDevice(atLimit) {
		t.Errorf("a device name of exactly auth.MaxDeviceIDLen (%d) characters was refused", auth.MaxDeviceIDLen)
	}
	overLimit := atLimit + "a"
	if validSetupDevice(overLimit) {
		t.Errorf("a device name one character over auth.MaxDeviceIDLen (%d) was accepted", auth.MaxDeviceIDLen)
	}
}
