// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

func vaultWithOnePush(t *testing.T) *backupvault.Vault {
	t.Helper()
	v, err := backupvault.Open(t.TempDir(), testRetentionKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A roomy fake filesystem, so a test about the list does not
	// depend on how full the host running it happens to be (#1125).
	v.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 50 << 30, 100 << 30, nil })
	backup := append([]byte{0x88, 0xac, 0xa1, 0xb1}, []byte("a backup")...)
	if err := v.Store("rb5009", backupvault.KindBackup, backup, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("rb5009", backupvault.KindRsc, []byte("export text"), time.Now()); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRouterBackupsListReportsDisabledWithNoKey(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Enabled {
		t.Error("Enabled = true with no vault configured")
	}
	if out.KeyUnreadable {
		t.Error("KeyUnreadable = true with no key configured at all -- that is #394's ordinary disabled state, not a fault")
	}
	if len(out.Routers) != 0 {
		t.Errorf("Routers = %v, want empty", out.Routers)
	}
}

// TestRouterBackupsListReportsKeyUnreadable covers #1264 finding 5: with
// no vault configured, Enabled is false whether history.keyFile was
// never set or was set to a file that could not be read -- KeyUnreadable
// is what tells those two apart, carried from
// SetupInstance.BackupKeyUnreadable rather than derived from Vault at
// all (Vault.Enabled() cannot see the difference either way).
func TestRouterBackupsListReportsKeyUnreadable(t *testing.T) {
	s := newAuthTestServer(t)
	s.SetupInstance.BackupKeyUnreadable = true
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Enabled {
		t.Error("Enabled = true with no vault configured")
	}
	if !out.KeyUnreadable {
		t.Error("KeyUnreadable = false with SetupInstance.BackupKeyUnreadable set -- the operator would be told to mint a new key over the broken one")
	}
}

func TestRouterBackupsListReportsTheDropBoxPort(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	s.SetupInstance.BackupPort = ":47022"
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Port != ":47022" {
		t.Errorf("Port = %q, want the configured drop-box port", out.Port)
	}
}

func TestRouterBackupsListNonAdminForbidden(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	adminClient := setUpAdmin(t, s, ts)

	resp := postJSON(t, adminClient, ts.URL+"/api/auth/users", map[string]string{"username": "viewer1", "password": "password123", "role": "viewer"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating the viewer account: status = %d", resp.StatusCode)
	}

	client := &http.Client{Jar: mustCookieJar(t)}
	loginResp := postJSON(t, client, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer1", Password: "password123"})
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("viewer login status = %d", loginResp.StatusCode)
	}
	seedFactor(t, s, ts, "viewer1") // #1253: needed before /api/router-backups below

	r, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET /api/router-backups status = %d, want 403", r.StatusCode)
	}
}

func TestRouterBackupsListReportsGenerationsAndMissed(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Enabled {
		t.Fatal("Enabled = false with a vault configured")
	}
	if len(out.Routers) != 1 || out.Routers[0].Device != "rb5009" {
		t.Fatalf("Routers = %+v, want one entry for rb5009", out.Routers)
	}
	if len(out.Routers[0].Generations) != 1 {
		t.Fatalf("Generations = %+v, want 1", out.Routers[0].Generations)
	}
	gen := out.Routers[0].Generations[0]
	if gen.Header != "plain" || gen.BackupBytes == 0 || gen.RscBytes == 0 {
		t.Errorf("generation = %+v, want a plain header and both sizes populated", gen)
	}
	// One push only: no interval, no missed count (#394's build note).
	if out.Routers[0].IntervalKnown {
		t.Errorf("IntervalKnown = true after a single push")
	}
}

func TestRouterBackupDownloadRoundTripsAndAudits(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	listResp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	var listed routerBackupsResponse
	json.NewDecoder(listResp.Body).Decode(&listed)
	listResp.Body.Close()
	genID := listed.Routers[0].Generations[0].ID

	dlResp, err := client.Get(ts.URL + "/api/router-backups/rb5009/" + genID + "/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", dlResp.StatusCode)
	}
	var body bytes.Buffer
	body.ReadFrom(dlResp.Body)
	want := append([]byte{0x88, 0xac, 0xa1, 0xb1}, []byte("a backup")...)
	if !bytes.Equal(body.Bytes(), want) {
		t.Fatalf("downloaded bytes differ: got %q want %q", body.Bytes(), want)
	}

	auditResp, err := client.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	var auditBody bytes.Buffer
	auditBody.ReadFrom(auditResp.Body)
	if !bytes.Contains(auditBody.Bytes(), []byte("router_backup.download")) {
		t.Fatalf("audit log does not mention router_backup.download: %s", auditBody.String())
	}
	if !bytes.Contains(auditBody.Bytes(), []byte("rb5009")) {
		t.Fatalf("audit log does not name the router: %s", auditBody.String())
	}
}

func TestRouterBackupDownloadUnknownGenerationIs404(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/no-such-generation/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRouterBackupDownloadRejectsUnknownKind(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/x/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestRouterBackupsListCarriesTheLowSpaceFlag pins #1125's UI hook: the
// list carries `lowSpace`, and it carries what the vault actually
// thinks -- true while the disk is under the floor, false once it is
// back above it -- so Settings can warn without a second call.
func TestRouterBackupsListCarriesTheLowSpaceFlag(t *testing.T) {
	s := newAuthTestServer(t)
	vault := vaultWithOnePush(t)
	s.Vault = vault
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	lowSpace := func() bool {
		t.Helper()
		resp, err := client.Get(ts.URL + "/api/router-backups")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var raw map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		got, ok := raw["lowSpace"]
		if !ok {
			t.Fatalf("the router-backups list has no lowSpace field: %v", raw)
		}
		flag, ok := got.(bool)
		if !ok {
			t.Fatalf("lowSpace = %v (%T), want a boolean", got, got)
		}
		return flag
	}

	if lowSpace() {
		t.Fatal("lowSpace = true on a vault that has never seen a full disk")
	}

	// A 100GiB filesystem with 1GiB free is under the vault's floor
	// (5% of the total); 50GiB free is over it with the exit margin to
	// spare. The push after each change is what makes the vault look.
	push := func(free int64, n int) {
		t.Helper()
		vault.SetSpaceProbeForTest(func(string) (int64, int64, error) { return free, 100 << 30, nil })
		body := append([]byte{0x88, 0xac, 0xa1, 0xb1}, bytes.Repeat([]byte("x"), n)...)
		if err := vault.Store("rb5009", backupvault.KindBackup, body, time.Now()); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	push(1<<30, 10)
	if !lowSpace() {
		t.Fatal("lowSpace = false with the vault's filesystem under its floor")
	}
	push(50<<30, 11)
	if lowSpace() {
		t.Fatal("lowSpace = true after the disk recovered, so Settings keeps warning")
	}
}

// TestRouterBackupsListUnaffectedByAFailedSpaceProbe is R10 (v0.6.0
// audit, #1304): every SetSpaceProbeForTest use above returns a nil
// error, so backupvault's "could not measure free space -- carrying on
// unchanged" branch (updateSpaceModeLocked's space.err != nil case) had
// no coverage at the API layer at all. A push whose space check fails
// outright must still succeed, and must neither set nor clear lowSpace:
// an unmeasurable disk is not evidence the disk is full, and it is not
// evidence the disk has room either.
func TestRouterBackupsListUnaffectedByAFailedSpaceProbe(t *testing.T) {
	s := newAuthTestServer(t)
	vault := vaultWithOnePush(t)
	s.Vault = vault
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, s, ts)

	lowSpace := func() bool {
		t.Helper()
		resp, err := client.Get(ts.URL + "/api/router-backups")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/router-backups = %d, want 200", resp.StatusCode)
		}
		var raw map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		got, ok := raw["lowSpace"].(bool)
		if !ok {
			t.Fatalf("lowSpace field missing or not a boolean: %v", raw)
		}
		return got
	}

	probeErr := errors.New("statfs /var/lib/mikroview/router-backups: input/output error")
	push := func(n int) {
		t.Helper()
		body := append([]byte{0x88, 0xac, 0xa1, 0xb1}, bytes.Repeat([]byte("x"), n)...)
		if err := vault.Store("rb5009", backupvault.KindBackup, body, time.Now()); err != nil {
			t.Fatalf("Store with a failing space probe returned an error, want the write to succeed regardless: %v", err)
		}
	}

	// From a clean start, a push whose probe fails but still returns a
	// plausible under-floor free/total pair must not set lowSpace: the
	// numbers are exactly what a real low-space reading would look like,
	// which is the point -- only the error tells the difference, so a
	// caller that looked at free/total without checking it first would
	// get this one right by accident.
	vault.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 1 << 30, 100 << 30, probeErr })
	push(10)
	if lowSpace() {
		t.Fatal("lowSpace = true from a failed probe alone -- an unmeasured reading must never be trusted, even when its numbers look like a full disk")
	}

	// Put the vault into low-space mode with a real reading...
	vault.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 1 << 30, 100 << 30, nil })
	push(11)
	if !lowSpace() {
		t.Fatal("lowSpace = false with the vault's filesystem under its floor")
	}

	// ...then push again with a failing probe that reports plenty of
	// free space: if the error were ignored this would read as recovery
	// and clear the flag, which is the dangerous direction -- a
	// genuinely full disk would stop being protected on the strength of
	// a reading that was never actually taken.
	vault.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 90 << 30, 100 << 30, probeErr })
	push(12)
	if !lowSpace() {
		t.Fatal("lowSpace flipped to false on a failed probe reporting plenty of free space -- a reading that could not be taken must never clear a mode a real one set")
	}
}
