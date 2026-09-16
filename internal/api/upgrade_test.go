// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// #1240: the operator-facing half of the upgrade framework. The server
// says what this data directory last ran, how much of the fleet is
// still on the old setup, and whether an admin has said they are done.

// upgradeServer is newAuthTestServer with a setup ledger that knows the
// instance's own address and syslog port -- without both there is
// nothing to hold a router's report against (see routersetup.go).
func upgradeServer(t *testing.T) *Server {
	t.Helper()
	s := newAuthTestServer(t)
	ledger, err := setup.Open("")
	if err != nil {
		t.Fatalf("setup.Open: %v", err)
	}
	s.Setup = ledger
	s.Setup.SetAddress("10.0.0.5:8443")
	s.SetupInstance = SetupInstance{SyslogPort: "6514"}
	s.Version = "v0.5.0"
	return s
}

func getUpgrade(t *testing.T, c *http.Client, base string) upgradeResponse {
	t.Helper()
	resp, err := c.Get(base + "/api/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/upgrade = %d, want 200", resp.StatusCode)
	}
	var got upgradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// The issue's "done when", first half: an instance whose marker said
// v0.4.0 serves the notice with that version.
func TestUpgradeServesTheCrossing(t *testing.T) {
	s := upgradeServer(t)
	noticed := time.Now()
	if !s.Setup.NoteUpgrade("v0.4.0", "v0.5.0", noticed) {
		t.Fatal("NoteUpgrade refused a real crossing")
	}
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	got := getUpgrade(t, setUpAdmin(t, ts), ts.URL)
	if got.Previous != "v0.4.0" || got.Current != "v0.5.0" {
		t.Errorf("served %q -> %q, want v0.4.0 -> v0.5.0", got.Previous, got.Current)
	}
	if got.Acknowledged {
		t.Error("acknowledged before anyone pressed done")
	}
	if got.NoticedAt.IsZero() {
		t.Error("noticedAt is zero -- the notice cannot say when it noticed")
	}
}

// The other half: a fresh data directory has nothing to say, and the
// empty previous version is how the notice knows to stay quiet.
func TestUpgradeIsSilentOnAFreshInstall(t *testing.T) {
	s := upgradeServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	got := getUpgrade(t, setUpAdmin(t, ts), ts.URL)
	if got.Previous != "" {
		t.Errorf("previous = %q on an instance that has never crossed a version, want empty", got.Previous)
	}
	if got.Current != "v0.5.0" {
		t.Errorf("current = %q, want the running build v0.5.0", got.Current)
	}
	if got.Routers.Total != 0 || got.Routers.Behind != 0 {
		t.Errorf("router counts = %+v, want zeroes when there is no upgrade to count against", got.Routers)
	}
}

// The count comes from #1241's ledger, not from a guess: a router on the
// current wizard is not behind, one on an older script is, and one that
// has never sent the page is too -- "never said" is not evidence of
// being up to date.
func TestUpgradeRouterCountReflectsTheSetupLedger(t *testing.T) {
	s := upgradeServer(t)
	s.Setup.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	// Two routers known only from their own pushes (#1170: a syslog
	// source is never a router; a push is what puts one in the list).
	s.Devices.Ensure("edge", time.Now())
	s.Devices.Ensure("branch", time.Now())

	noteReport(t, s, "core", routeros.WizardVersion, "firewall,info")
	noteReport(t, s, "edge", routeros.WizardVersion, "firewall")
	// branch says nothing at all.

	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	got := getUpgrade(t, adminClient, ts.URL).Routers
	if got.Total != 3 {
		t.Errorf("total = %d, want the three declared and discovered routers", got.Total)
	}
	if got.Behind != 2 {
		t.Errorf("behind = %d, want 2 (one drifted, one that has never reported)", got.Behind)
	}
	if got.Reported != 2 {
		t.Errorf("reported = %d, want 2 -- the third has never sent the page, which is what `done` still exists for", got.Reported)
	}

	// Every router caught up: the count falls to zero, which is what
	// clears the notice without anyone pressing done.
	noteReport(t, s, "edge", routeros.WizardVersion, "firewall,info")
	noteReport(t, s, "branch", routeros.WizardVersion, "firewall,info")
	if after := getUpgrade(t, adminClient, ts.URL).Routers; after.Behind != 0 || after.Reported != 3 {
		t.Errorf("after every router reported the current setup: %+v, want behind 0 of 3 reported", after)
	}
}

// `done` is an instance-wide statement, so it is written down and
// audited with the admin's name.
func TestUpgradeAcknowledgePersistsAndAudits(t *testing.T) {
	s := upgradeServer(t)
	s.Setup.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	resp := postJSON(t, adminClient, ts.URL+"/api/upgrade/acknowledge", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/upgrade/acknowledge = %d, want 200", resp.StatusCode)
	}
	var got upgradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Acknowledged {
		t.Error("the acknowledge response itself does not read as acknowledged")
	}
	if later := getUpgrade(t, adminClient, ts.URL); !later.Acknowledged {
		t.Error("a later GET does not read as acknowledged -- the notice would come back")
	}

	found := false
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "upgrade.acknowledged" && e.Target == "v0.5.0" {
			found = true
		}
	}
	if !found {
		t.Error("expected an audit entry for upgrade.acknowledged against v0.5.0")
	}
}

// A viewer may read the two version strings and the count; only an admin
// may settle it. (The tier itself is on the record in authzMatrix; this
// pins the pair reading the same server.)
func TestUpgradeReadableByViewerWritableByAdminOnly(t *testing.T) {
	s := upgradeServer(t)
	s.Setup.NoteUpgrade("v0.4.0", "v0.5.0", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := setUpAdmin(t, ts)
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "viewer"}).Body.Close()
	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()

	if got := getUpgrade(t, viewerClient, ts.URL); got.Previous != "v0.4.0" {
		t.Errorf("a viewer's GET served previous = %q, want v0.4.0", got.Previous)
	}

	resp := postJSON(t, viewerClient, ts.URL+"/api/upgrade/acknowledge", struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST /api/upgrade/acknowledge from a viewer = %d, want 403", resp.StatusCode)
	}
}
