// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

func vaultWithOnePush(t *testing.T) *backupvault.Vault {
	t.Helper()
	v, err := backupvault.Open(t.TempDir(), testRetentionKey(t))
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
	client := setUpAdmin(t, ts)

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
	if len(out.Routers) != 0 {
		t.Errorf("Routers = %v, want empty", out.Routers)
	}
}

func TestRouterBackupsListReportsTheDropBoxPort(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	s.SetupInstance.BackupPort = ":47022"
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, ts)

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
	adminClient := setUpAdmin(t, ts)

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
	client := setUpAdmin(t, ts)

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
	client := setUpAdmin(t, ts)

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
	client := setUpAdmin(t, ts)

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
	client := setUpAdmin(t, ts)

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
	client := setUpAdmin(t, ts)

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
