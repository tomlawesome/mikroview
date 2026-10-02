// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// blocklistTestServer is an admin session on the gated routes, with a
// router (rb5009) that has pushed on version.
func blocklistTestServer(t *testing.T, version string) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s, ts, admin := droplistTestServer(t)
	if version != "" {
		pushBlocklistPage(t, s, "rb5009", `{"kind":"filter-rule","page":1,"pages":1,"routerosVersion":"`+version+` (stable)","wizardVersion":6,"records":[]}`)
	}
	return s, ts, admin
}

// pushBlocklistPage applies one pushed page to device, decoded the way
// the ingest endpoint decodes it.
func pushBlocklistPage(t *testing.T, s *Server, device, body string) {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	if err := s.RouterState.Apply(device, p, time.Now()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func getBuilder(t *testing.T, client *http.Client, url string) (int, blocklistBuilderResponse) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out blocklistBuilderResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, out
}

func postBlocklistCommands(t *testing.T, client *http.Client, url string, body any) (int, blocklistCommandsResponse, string) {
	t.Helper()
	resp := postJSON(t, client, url+"/api/blocklist/builder/commands", body)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out blocklistCommandsResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decoding %s: %v", raw, err)
		}
	}
	return resp.StatusCode, out, string(raw)
}

var etDefaults = routeros.BlocklistChoice{Key: "et", Direction: "both", Log: true, Refresh: "weekdays"}

func TestBlocklistRoutesAreAdminOnly(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.24.4")
	for _, role := range []string{"user", "viewer"} {
		postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: role + "1", Password: "password456", Role: role}).Body.Close()
		c := &http.Client{Jar: mustCookieJar(t)}
		postJSON(t, c, ts.URL+"/api/auth/login", credentialsRequest{Username: role + "1", Password: "password456"}).Body.Close()
		seedFactor(t, s, ts, role+"1")
		if code, _ := getBuilder(t, c, ts.URL+"/api/blocklist/builder?device=rb5009"); code != http.StatusForbidden {
			t.Errorf("GET as %s: %d, want 403", role, code)
		}
		if code, _, _ := postBlocklistCommands(t, c, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}}); code != http.StatusForbidden {
			t.Errorf("POST as %s: %d, want 403", role, code)
		}
	}
	anon := &http.Client{}
	if code, _ := getBuilder(t, anon, ts.URL+"/api/blocklist/builder?device=rb5009"); code != http.StatusUnauthorized {
		t.Errorf("GET signed out: %d, want 401", code)
	}
	if code, _ := getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009"); code != http.StatusOK {
		t.Errorf("GET as admin: %d, want 200", code)
	}
}

func TestBlocklistBuilderReadsTheRoutersOwnVersionAndHold(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.24.4")
	pushBlocklistPage(t, s, "rb5009", `{"kind":"address-list-count","page":1,"pages":1,"records":[`+
		`{"list":"mv-bl-spamhaus","family":"ip","count":1692,"loadedAt":"2026-10-01 04:17:02"},`+
		`{"list":"mv-bl-spamhaus6","family":"ipv6","count":91,"loadedAt":"2026-10-01 04:17:05"},`+
		`{"list":"mv-bl-et","family":"ip","count":0,"loadedAt":""}]}`)
	pushBlocklistPage(t, s, "rb5009", `{"kind":"raw-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"family":"ip","comment":"mikroview blocklist: spamhaus (from)","chain":"prerouting","action":"drop","srcAddressList":"mv-bl-spamhaus","dstAddressList":null,"logPrefix":"D|bl-spamhaus|","log":true,"disabled":false,"packets":412,"bytes":20000}]}`)

	code, got := getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Standing != "ok" || got.RouterOSVersion != "7.24.4" || got.ReportedAt.IsZero() || got.ReviewedVersion != routeros.ReviewedVersion {
		t.Errorf("standing %q version %q reported %v reviewed %q", got.Standing, got.RouterOSVersion, got.ReportedAt, got.ReviewedVersion)
	}
	if len(got.Catalogue) != 7 || got.Catalogue[0].Key != "spamhaus" || len(got.LeftOut) != 10 {
		t.Fatalf("catalogue %d, left out %d", len(got.Catalogue), len(got.LeftOut))
	}
	et := got.Catalogue[1]
	if et.RefreshDefault != "weekdays" || len(et.Refresh) != 3 || et.Refresh[1].Value != "weekdays" || et.Refresh[1].Since != "7.24" {
		t.Errorf("ET on 7.24.4 offers %+v defaulting to %q", et.Refresh, et.RefreshDefault)
	}
	sp := got.Lists[0]
	if sp.Key != "spamhaus" || sp.State != "held" || sp.Count != 1692 || sp.Count6 != 91 || sp.LoadedAt != "2026-10-01 04:17:02" {
		t.Errorf("Spamhaus row %+v", sp)
	}
	if len(sp.Rules) != 1 || sp.Rules[0] != routeros.RawRuleKey("ip", "mikroview blocklist: spamhaus (from)") {
		t.Errorf("Spamhaus rules %v", sp.Rules)
	}
	if got.Lists[1].State != "off" || !strings.Contains(got.Lists[1].Undo, "mv-bl-et") {
		t.Errorf("ET row %+v", got.Lists[1])
	}
	if !got.PushCurrent {
		t.Error("the router pushes both new kinds, but pushCurrent is false")
	}
	if got.UndoAll != routeros.BlocklistUndoAll() {
		t.Error("undoAll is not the generator's")
	}
}

// The no-push and below-floor standings: no version means no commands,
// and below the floor the lists read below-floor.
func TestBlocklistStandings(t *testing.T) {
	_, ts, admin := blocklistTestServer(t, "")
	_, got := getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009")
	if got.Standing != "no-push" || got.RouterOSVersion != "" {
		t.Errorf("never pushed: %q %q", got.Standing, got.RouterOSVersion)
	}
	code, cmd, _ := postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}})
	if code != http.StatusOK || len(cmd.Parts) != 0 || cmd.CopyText != "" || strings.Join(cmd.Blocked, ",") != "no-push" {
		t.Errorf("never pushed: %d %+v", code, cmd)
	}

	_, ts, admin = blocklistTestServer(t, "7.12.1")
	_, got = getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device=rb5009")
	if got.Standing != "below-floor" || got.Lists[0].State != "below-floor" {
		t.Errorf("below the floor: %q %q", got.Standing, got.Lists[0].State)
	}
	code, cmd, _ = postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}})
	if code != http.StatusOK || len(cmd.Parts) != 0 || strings.Join(cmd.Blocked, ",") != "below-floor" {
		t.Errorf("below the floor: %d %+v", code, cmd)
	}
}

// The block is written for the version the router pushed; the request
// cannot name one, and saying one anyway is refused rather than ignored.
func TestBlocklistCommandsUseThePushedVersion(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.19.4")
	s.Setup = nil
	code, cmd, raw := postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Address: "mikroview.lan:8443", Token: "tok-123",
		Lists: []routeros.BlocklistChoice{{Key: "et", Direction: "both", Log: true, Refresh: "daily"}}})
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	if !strings.Contains(cmd.CopyText, "http-max-redirect-count=2") || strings.Contains(cmd.CopyText, ":continue") {
		t.Error("the block is not written for 7.19.4")
	}
	if len(cmd.Parts) != 5 || cmd.Parts[0].Ink != "push" || !strings.Contains(cmd.Parts[0].Commands, "Bearer tok-123") {
		t.Errorf("parts %d, first %q", len(cmd.Parts), cmd.Parts[0].Ink)
	}
	// Weekdays is 7.24's; on the pushed 7.19.4 it is refused even though
	// a client might think the router newer.
	if code, _, _ := postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}}); code != http.StatusBadRequest {
		t.Errorf("weekdays on a 7.19.4 router: %d, want 400", code)
	}
	resp := postJSON(t, admin, ts.URL+"/api/blocklist/builder/commands", map[string]any{
		"device": "rb5009", "token": "tok", "routerosVersion": "7.24.4",
		"lists": []routeros.BlocklistChoice{etDefaults},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a body naming a version: %d, want 400", resp.StatusCode)
	}
}

// #1095's rule for this route: anything that would change the shape of
// the commands is refused.
func TestBlocklistCommandsRejectUnsafeInput(t *testing.T) {
	_, ts, admin := blocklistTestServer(t, "7.24.4")
	ok := blocklistCommandsRequest{Device: "rb5009", Address: "mikroview.lan:8443", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}}
	if code, _, raw := postBlocklistCommands(t, admin, ts.URL, ok); code != http.StatusOK {
		t.Fatalf("the valid request: %d %s", code, raw)
	}
	for name, mutate := range map[string]func(*blocklistCommandsRequest){
		"no device":              func(r *blocklistCommandsRequest) { r.Device = "" },
		"a device with a space":  func(r *blocklistCommandsRequest) { r.Device = "rb 5009" },
		"an address with quotes": func(r *blocklistCommandsRequest) { r.Address = `x" ; /system reset-configuration` },
		"an address with a line": func(r *blocklistCommandsRequest) { r.Address = "x\n/user add" },
		"a token with a comma":   func(r *blocklistCommandsRequest) { r.Token = "tok,X-Evil: 1" },
		"a token with a dollar":  func(r *blocklistCommandsRequest) { r.Token = "$[/user add]" },
		"an unknown list": func(r *blocklistCommandsRequest) {
			r.Lists = []routeros.BlocklistChoice{{Key: "firehol", Direction: "from", Refresh: "daily"}}
		},
		"a key carrying a command": func(r *blocklistCommandsRequest) {
			r.Lists = []routeros.BlocklistChoice{{Key: `et";/system reset-configuration;"`, Direction: "from", Refresh: "daily"}}
		},
		"a direction off the menu": func(r *blocklistCommandsRequest) {
			r.Lists = []routeros.BlocklistChoice{{Key: "et", Direction: `from" log=no`, Refresh: "daily"}}
		},
		"a refresh off the menu": func(r *blocklistCommandsRequest) {
			r.Lists = []routeros.BlocklistChoice{{Key: "et", Direction: "from", Refresh: "1s"}}
		},
		"a list twice": func(r *blocklistCommandsRequest) { r.Lists = []routeros.BlocklistChoice{etDefaults, etDefaults} },
		"more lists than there are": func(r *blocklistCommandsRequest) {
			r.Lists = make([]routeros.BlocklistChoice, 8)
			for i := range r.Lists {
				r.Lists[i] = etDefaults
			}
		},
	} {
		req := ok
		mutate(&req)
		code, _, raw := postBlocklistCommands(t, admin, ts.URL, req)
		if code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
		if strings.Contains(raw, "reset-configuration") || strings.Contains(raw, "X-Evil") {
			t.Errorf("%s: the refusal echoes the input: %s", name, raw)
		}
	}
	if code, _ := getBuilder(t, admin, ts.URL+"/api/blocklist/builder?device="+strings.Repeat("a", 65)); code != http.StatusBadRequest {
		t.Errorf("GET with an over-long device: %d", code)
	}
	if code, _ := getBuilder(t, admin, ts.URL+"/api/blocklist/builder"); code != http.StatusBadRequest {
		t.Errorf("GET with no device: %d", code)
	}
	big := append([]byte(`{"device":"rb5009","token":"`), bytes.Repeat([]byte("a"), 1<<21)...)
	big = append(big, `"}`...)
	hreq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/blocklist/builder/commands", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := admin.Do(hreq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an oversized body: %d", resp.StatusCode)
	}
}

// Part 1 re-sets the push with the kinds the router already pushes and
// the builder's two; without a token or an address it is left out and
// says why.
func TestBlocklistCommandsPushPart(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.24.4")
	s.Setup = nil
	pushBlocklistPage(t, s, "rb5009", `{"kind":"arp","page":1,"pages":1,"records":[]}`)
	_, cmd, _ := postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Address: "mikroview.lan:8443", Token: "tok-1", Lists: []routeros.BlocklistChoice{etDefaults}})
	want := routeros.ScheduleCommands(routeros.PushScript("mikroview.lan:8443", "tok-1", []string{"arp", "filter-rule", "raw-rule", "address-list-count"}, "a"), "a")
	if len(cmd.Parts) == 0 || cmd.Parts[0].Commands != want {
		t.Errorf("part 1 is not the push re-set with the router's kinds and the builder's two")
	}
	_, cmd, _ = postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Address: "mikroview.lan:8443", Lists: []routeros.BlocklistChoice{etDefaults}})
	if cmd.Parts[0].Ink == "push" || strings.Join(cmd.Blocked, ",") != "no-token" {
		t.Errorf("no token: first part %q, blocked %v", cmd.Parts[0].Ink, cmd.Blocked)
	}
	_, cmd, _ = postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{etDefaults}})
	if cmd.Parts[0].Ink == "push" || strings.Join(cmd.Blocked, ",") != "no-address" {
		t.Errorf("no address: first part %q, blocked %v", cmd.Parts[0].Ink, cmd.Blocked)
	}
}

// A variant switched off is removed when the router's push still shows it.
func TestBlocklistCommandsConvergeOnWhatTheRouterHolds(t *testing.T) {
	s, ts, admin := blocklistTestServer(t, "7.24.4")
	pushBlocklistPage(t, s, "rb5009", `{"kind":"raw-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"family":"ip","comment":"mikroview blocklist: et (to)","chain":"prerouting","action":"drop","srcAddressList":null,"dstAddressList":"mv-bl-et","logPrefix":"D|bl-et|","log":true,"disabled":false,"packets":0,"bytes":0}]}`)
	from := etDefaults
	from.Direction = "from"
	_, cmd, _ := postBlocklistCommands(t, admin, ts.URL, blocklistCommandsRequest{Device: "rb5009", Token: "tok", Lists: []routeros.BlocklistChoice{from}})
	if !strings.Contains(cmd.CopyText, `/ip firewall raw remove [find comment="mikroview blocklist: et (to)"]`) {
		t.Errorf("the (to) rule the router holds is not removed:\n%s", cmd.CopyText)
	}
}
