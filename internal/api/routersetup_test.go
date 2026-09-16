// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// #1241: the router-side leg of the upgrade contract. The wizard's push
// script sends what it left on the router -- the mikroview logging
// action, the rules feeding it, and the wizard version that wrote the
// script -- and GET /api/devices says, per router, whether that is what
// the current wizard would leave.

// loggingPushBody is the ratified page, at the wizard version and rule
// topics given, addressed to the instance the fixture below describes.
func loggingPushBody(wizardVersion int, topics string) string {
	return fmt.Sprintf(`{"kind":"logging","page":1,"pages":1,"routerosVersion":"7.16.1","wizardVersion":%d,
 "records":[
  {"type":"action","name":"mikroview","target":"remote","remote":"10.0.0.5","remotePort":"6514","remoteProtocol":"tls","remoteLogFormat":"syslog","checkCertificate":"yes"},
  {"type":"rule","topics":%q,"action":"mikroview","disabled":"no"}
 ]}`, wizardVersion, topics)
}

// setupReportServer is newTestServer with a setup ledger and an instance
// that knows its own address and syslog port -- without both, there is
// nothing to compare a router's report against.
func setupReportServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	ledger, err := setup.Open("")
	if err != nil {
		t.Fatalf("setup.Open: %v", err)
	}
	s.Setup = ledger
	s.Setup.SetAddress("10.0.0.5:8443")
	s.SetupInstance = SetupInstance{SyslogPort: "6514"}
	return s
}

func noteReport(t *testing.T, s *Server, device string, wizardVersion int, topics string) {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(loggingPushBody(wizardVersion, topics)))
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	s.Setup.NoteLoggingReport(device, p, time.Now())
}

type deviceSetupReport struct {
	Standing       string `json:"standing"`
	ScriptVersion  int    `json:"scriptVersion"`
	CurrentVersion int    `json:"currentVersion"`
}

func devicesSetup(t *testing.T, s *Server) map[string]deviceSetupReport {
	t.Helper()
	ts := httptest.NewServer(s.mux())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Devices []struct {
			ID    string             `json:"id"`
			Setup *deviceSetupReport `json:"setup"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	out := map[string]deviceSetupReport{}
	for _, d := range body.Devices {
		if d.Setup == nil {
			t.Fatalf("%s carried no setup answer at all; \"never reported\" is an answer, not an absence", d.ID)
		}
		out[d.ID] = *d.Setup
	}
	return out
}

// #1241's "done when": one router on the current wizard, one a version
// behind, one that has never sent the page at all.
func TestHandleDevicesReportsRouterSetupStanding(t *testing.T) {
	s := setupReportServer(t)
	pushingRouter(t, s, "lab-crs", "203.0.113.9/24")
	pushingRouter(t, s, "old-hex", "198.51.100.1/24")
	s.Devices.Resolve("203.0.113.9", time.Now())
	s.Devices.Resolve("198.51.100.1", time.Now())

	noteReport(t, s, "core", routeros.WizardVersion, "firewall,info")
	noteReport(t, s, "lab-crs", routeros.WizardVersion-1, "firewall,info")
	// old-hex reports nothing: a router still running a script pasted
	// before the page existed.

	got := devicesSetup(t, s)
	if got["core"].Standing != string(setup.StandingCurrent) {
		t.Errorf("core's standing = %q, want %q", got["core"].Standing, setup.StandingCurrent)
	}
	if got["core"].CurrentVersion != routeros.WizardVersion {
		t.Errorf("core's currentVersion = %d, want %d", got["core"].CurrentVersion, routeros.WizardVersion)
	}
	behind := got["lab-crs"]
	if behind.Standing != string(setup.StandingBehind) {
		t.Errorf("lab-crs's standing = %q, want %q", behind.Standing, setup.StandingBehind)
	}
	if behind.ScriptVersion != routeros.WizardVersion-1 || behind.CurrentVersion != routeros.WizardVersion {
		t.Errorf("lab-crs reported script v%d against current v%d, want v%d and v%d",
			behind.ScriptVersion, behind.CurrentVersion, routeros.WizardVersion-1, routeros.WizardVersion)
	}
	if got["old-hex"].Standing != string(setup.StandingNeverReported) {
		t.Errorf("old-hex's standing = %q, want %q", got["old-hex"].Standing, setup.StandingNeverReported)
	}
}

// Same wizard version, different rule topics: still behind, because the
// operator's remedy is the same paste.
func TestHandleDevicesReportsTopicsDriftAsBehind(t *testing.T) {
	s := setupReportServer(t)
	noteReport(t, s, "core", routeros.WizardVersion, "firewall")

	if got := devicesSetup(t, s)["core"]; got.Standing != string(setup.StandingBehind) {
		t.Errorf("core's standing = %q, want %q -- its rule's topics are not what the wizard writes", got.Standing, setup.StandingBehind)
	}
}

// The whole path: a real push on the ingest endpoint, with a real ingest
// token, lands in the ledger the device API reads.
func TestIngestLoggingPageRecordsTheRouterSetup(t *testing.T) {
	ts, s, raw := ingestTestServer(t, "router-1")
	ledger, err := setup.Open("")
	if err != nil {
		t.Fatalf("setup.Open: %v", err)
	}
	s.Setup = ledger
	s.Setup.SetAddress("10.0.0.5:8443")

	resp := postIngest(t, ts, raw, loggingPushBody(routeros.WizardVersion, "firewall,info"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	want := routeros.WizardLogging("10.0.0.5:8443", "6514", "a")
	if got := s.Setup.RouterSetup("router-1", want); got.Standing != setup.StandingCurrent {
		t.Errorf("router-1's standing after its own push = %q, want %q", got.Standing, setup.StandingCurrent)
	}
	// Keyed by the token's device, never by anything the payload says
	// about itself -- the same rule every other pushed kind follows.
	if got := s.Setup.RouterSetup("core", want); got.Standing != setup.StandingNeverReported {
		t.Errorf("core's standing = %q, want %q -- router-1's token may not report for it", got.Standing, setup.StandingNeverReported)
	}
}
