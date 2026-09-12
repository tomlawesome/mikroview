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

	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/routeros"
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

// TestHandleSetupCommandsRequiresAddress covers the one required field
// the contract states: every other field is optional, address is not.
func TestHandleSetupCommandsRequiresAddress(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	body, _ := json.Marshal(setupCommandsRequest{})
	resp, err := http.Post(ts.URL+"/api/setup/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a missing address", resp.StatusCode)
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
	if !strings.HasPrefix(schedule, `/system script add name=mv-push policy=read,test source="`) {
		t.Errorf("schedule commands = %q, want the script add that saves the push script", schedule)
	}
	if !strings.Contains(schedule, "Bearer tok-123") || !strings.HasSuffix(schedule, "\n/system script run mv-push") {
		t.Errorf("schedule commands = %q, want the push script inside it and one run now", schedule)
	}
	if strings.Contains(schedule, "paste the script") {
		t.Errorf("schedule commands still ask the operator to paste a script in: %q", schedule)
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
	v, err := backupvault.Open(t.TempDir(), key)
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
		{"syslogPort non-numeric", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "abc"}},
		{"syslogPort zero", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "0"}},
		{"syslogPort out of range", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "70000"}},
		{"syslogPort listen address, bad port", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "127.0.0.1:abc"}},
		{"syslogPort listen address, zero port", setupCommandsRequest{Address: "mv.example.com", SyslogPort: "[::]:0"}},
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
