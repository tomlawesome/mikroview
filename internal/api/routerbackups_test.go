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
