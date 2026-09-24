// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// deviceTestServer is an admin session against a server with a fresh,
// empty device registry -- newTestServer's own "core" declaration would
// otherwise make several of these tests' starting state ("no devices at
// all") false from the moment the server exists.
func deviceTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	s := newAuthTestServer(t)
	s.Devices = device.NewRegistry(nil)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	admin := setUpAdmin(t, s, ts)
	return s, ts, admin
}

// mintBody is the body POST /api/devices/{id}/enrolment has taken since
// issue #1291: the admin's own password, re-proving identity at the
// moment of minting, and the one address the token may be redeemed
// from. testAdminPassword is what setUpAdmin registers the admin with.
const testAdminPassword = "password123"

func mintBody(addr string) deviceEnrolmentRequest {
	return deviceEnrolmentRequest{Password: testAdminPassword, ExpectedAddress: addr}
}

func TestDeviceCreateDeclaresANamelessDevice(t *testing.T) {
	_, ts, admin := deviceTestServer(t)

	resp := postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201, body = %s", resp.StatusCode, body)
	}
	var info device.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.ID != "hap-ax3" || info.SourceIP != "" || info.AcceptedIP != "" {
		t.Errorf("created device = %+v, want a bare device with no address", info)
	}
}

func TestDeviceCreateRejectsAnEmptyOrBadName(t *testing.T) {
	_, ts, admin := deviceTestServer(t)

	for _, name := range []string{"", "  ", "has a space", strings.Repeat("x", 65)} {
		resp := postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: name})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("name %q: status = %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestDeviceCreateConflictsOnADuplicateID(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// unsavableBackend is a device registry backend whose every Save
// fails, for the #1303 proof that a write the registry cannot keep is
// reported to the operator rather than quietly applied in memory.
type unsavableBackend struct{}

func (unsavableBackend) Load(context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (unsavableBackend) Save(context.Context, []byte, int64) (int64, error) {
	return 0, errors.New("disk full")
}
func (unsavableBackend) Close() error     { return nil }
func (unsavableBackend) Describe() string { return "/var/lib/mikroview/secret-path/devices.json" }

func TestDeviceCreateReportsAFailedSaveAndChangesNothing(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	reg, err := device.OpenRegistryWithBackend(unsavableBackend{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Devices = reg

	resp := postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "nothing was changed") {
		t.Errorf("body = %q, want the 'nothing was changed' sentence", body)
	}
	if strings.Contains(string(body), "secret-path") || strings.Contains(string(body), "disk full") {
		t.Errorf("body = %q leaks the backend's path or error; that belongs in the log only", body)
	}
	if got := s.Devices.List(); len(got) != 0 {
		t.Errorf("List() = %+v after a failed create, want empty", got)
	}
}

func TestDeviceDeleteClearsItAndRefusesAConfiguredOne(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	resp := deleteNoBody(t, admin, ts.URL+"/api/devices/hap-ax3")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if len(s.Devices.List()) != 0 {
		t.Errorf("List() = %+v, want empty after delete", s.Devices.List())
	}

	notFound := deleteNoBody(t, admin, ts.URL+"/api/devices/hap-ax3")
	defer notFound.Body.Close()
	if notFound.StatusCode != http.StatusNotFound {
		t.Errorf("deleting an already-deleted device: status = %d, want 404", notFound.StatusCode)
	}

	// A config.yaml-declared device is a distinct real deployment
	// scenario from a registry-created one -- swapped in directly on a
	// fresh server, since the handler's own status-code mapping is what
	// this test is about, not anything else "core" carries elsewhere.
	s2 := newAuthTestServer(t)
	s2.Devices = device.NewRegistry([]config.Device{{ID: "core", SourceIP: "192.168.1.1"}})
	ts2 := httptest.NewServer(s2.Routes())
	t.Cleanup(ts2.Close)
	admin2 := setUpAdmin(t, s2, ts2)

	refused := deleteNoBody(t, admin2, ts2.URL+"/api/devices/core")
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusBadRequest {
		t.Errorf("deleting a config.yaml device: status = %d, want 400", refused.StatusCode)
	}
}

func TestDeviceEnrolmentMintReplacesAndRerolls(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	first := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	var firstResp deviceEnrolmentResponse
	if err := json.NewDecoder(first.Body).Decode(&firstResp); err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if firstResp.Token == "" || firstResp.ExpiresAt.IsZero() {
		t.Fatalf("mint response = %+v, want a token and expiry", firstResp)
	}

	second := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	var secondResp deviceEnrolmentResponse
	if err := json.NewDecoder(second.Body).Decode(&secondResp); err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if secondResp.Token == firstResp.Token {
		t.Errorf("reroll produced the same token")
	}
}

func TestDeviceEnrolmentMintUnknownDeviceNotFound(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	resp := postJSON(t, admin, ts.URL+"/api/devices/nope/enrolment", mintBody("10.10.0.1"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeviceEnrolmentDeleteBurnsThePendingToken(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	mint := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	var minted deviceEnrolmentResponse
	json.NewDecoder(mint.Body).Decode(&minted)
	mint.Body.Close()

	del := deleteNoBody(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment")
	defer del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", del.StatusCode)
	}

	if s.Devices.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+minted.Token)) {
		t.Errorf("a burned pending token still redeemed")
	}

	again := deleteNoBody(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment")
	defer again.Body.Close()
	if again.StatusCode != http.StatusNotFound {
		t.Errorf("deleting an already-burned enrolment: status = %d, want 404", again.StatusCode)
	}
}

func TestDevicesRefusedListsAndBoundsAddresses(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	s.Devices.Refuse("10.10.0.9", []byte("junk"))

	resp, err := admin.Get(ts.URL + "/api/devices/refused")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got []device.Refused
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Address != "10.10.0.9" {
		t.Fatalf("Refused list = %+v, want the one refused address", got)
	}
}

// TestHandleDevicesReportsAcceptedIPAndEnrolment is GET /api/devices'
// new fields end to end (issue #1281): acceptedIp/enrolledAt on the
// device row, and the separate in-flight enrolment object.
func TestHandleDevicesReportsAcceptedIPAndEnrolment(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	mint := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	var minted deviceEnrolmentResponse
	json.NewDecoder(mint.Body).Decode(&minted)
	mint.Body.Close()

	resp, err := admin.Get(ts.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Devices []deviceView `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 1 {
		t.Fatalf("devices = %+v, want the one created device", body.Devices)
	}
	d := body.Devices[0]
	if d.AcceptedIP != "" {
		t.Errorf("acceptedIp = %q before any line redeemed the token, want empty", d.AcceptedIP)
	}
	if !d.Enrolment.Pending {
		t.Errorf("enrolment = %+v, want pending: true", d.Enrolment)
	}

	if !s.Devices.TryEnrol("10.10.0.1", []byte("mikroview-enrol "+minted.Token)) {
		t.Fatal("TryEnrol failed to redeem the freshly minted token")
	}

	resp2, err := admin.Get(ts.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var body2 struct {
		Devices []deviceView `json:"devices"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body2); err != nil {
		t.Fatal(err)
	}
	d2 := body2.Devices[0]
	if d2.AcceptedIP != "10.10.0.1" || d2.EnrolledAt.IsZero() {
		t.Errorf("device after redemption = %+v, want acceptedIp=10.10.0.1 and enrolledAt set", d2)
	}
	if d2.Enrolment.Pending {
		t.Errorf("enrolment = %+v, want pending: false once redeemed (single use)", d2.Enrolment)
	}
}

// TestDeviceEnrolmentMintRefusesWithoutAPassword is issue #1291's core
// claim at the HTTP boundary: holding an admin session is not enough to
// mint. A stolen session cookie, a cross-site request riding the
// admin's browser, or script injected into a page they are viewing all
// arrive exactly like this -- authenticated, admin, and with no
// password.
func TestDeviceEnrolmentMintRefusesWithoutAPassword(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment",
		deviceEnrolmentRequest{ExpectedAddress: "10.10.0.1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a mint with no password", resp.StatusCode)
	}
	// Nothing was minted, so the connection gate never opened.
	if s.Devices.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("a refused mint still left a token pending -- want nothing minted at all")
	}
}

// TestDeviceEnrolmentMintRefusesAWrongPassword: the re-proof is a real
// check, not a required-field formality.
func TestDeviceEnrolmentMintRefusesAWrongPassword(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment",
		deviceEnrolmentRequest{Password: "not-the-password", ExpectedAddress: "10.10.0.1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a mint with the wrong password", resp.StatusCode)
	}
	if s.Devices.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("a mint with the wrong password still left a token pending")
	}

	// The right password still works straight afterwards: the refusal
	// above counts against the login limiter, and one wrong attempt must
	// not lock the admin out of their own setup.
	ok := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusCreated {
		t.Errorf("status = %d after one wrong attempt, want 201", ok.StatusCode)
	}
}

// TestDeviceEnrolmentMintRerollAsksEveryTime: reroll goes through this
// same endpoint, so it re-proves too. That is the feature working, not
// a snag to smooth over.
func TestDeviceEnrolmentMintRerollAsksEveryTime(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1")).Body.Close()

	reroll := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment",
		deviceEnrolmentRequest{ExpectedAddress: "10.10.0.1"})
	defer reroll.Body.Close()
	if reroll.StatusCode != http.StatusUnauthorized {
		t.Errorf("reroll status = %d with no password, want 401 -- rerolling must ask every time", reroll.StatusCode)
	}
}

// TestDeviceEnrolmentMintRequiresAnExpectedAddress: the enrolment
// window binds to one address (#1291), so there is no way to ask for a
// token that opens the port to everyone.
func TestDeviceEnrolmentMintRequiresAnExpectedAddress(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	for _, tc := range []struct{ name, addr string }{
		{"none", ""},
		{"a hostname", "router.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment",
				deviceEnrolmentRequest{Password: testAdminPassword, ExpectedAddress: tc.addr})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestDeviceRegisterRecordsIntentAndGrantsNoAddress is the ledger's
// final step end to end: it stamps the device and confers nothing.
func TestDeviceRegisterRecordsIntentAndGrantsNoAddress(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/registration",
		deviceRegisterRequest{Name: "hap-ax3"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200, body = %s", resp.StatusCode, body)
	}
	var info device.Info
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.RegisteredAt.IsZero() {
		t.Error("registeredAt is zero after registering, want it stamped")
	}
	if info.AcceptedIP != "" {
		t.Fatalf("acceptedIp = %q after registering, want registering to grant no address", info.AcceptedIP)
	}
	// And it opened no enrolment window either -- registering is not a
	// back door to the thing minting is now guarded for.
	if s.Devices.AcceptsConnectionFrom("10.10.0.1") {
		t.Error("registering opened the connection gate, want it to grant nothing at all")
	}
}

// TestDeviceRegisterRequiresAdmin: same tier as every other
// device-identity write in this file.
func TestDeviceRegisterRequiresAdmin(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	postJSON(t, admin, ts.URL+"/api/auth/users",
		createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()

	viewer := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewer, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "viewer", Password: "password456"}).Body.Close()
	seedFactor(t, s, ts, "viewer") // #1253: needed before /api/devices/.../registration below

	resp := postJSON(t, viewer, ts.URL+"/api/devices/hap-ax3/registration",
		deviceRegisterRequest{Name: "hap-ax3"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a non-admin", resp.StatusCode)
	}
}

// TestDeviceEnrolmentRebindMovesTheWindowWithoutANewToken is ruling
// 23a end to end: one click, no password, same token.
func TestDeviceEnrolmentRebindMovesTheWindowWithoutANewToken(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	mint := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1"))
	var minted deviceEnrolmentResponse
	json.NewDecoder(mint.Body).Decode(&minted)
	mint.Body.Close()

	// The router is really at .5, so the listener turned it away.
	s.Devices.RefuseConnection("10.10.0.5")

	resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment/address",
		deviceEnrolmentRebindRequest{Address: "10.10.0.5"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 204, body = %s", resp.StatusCode, body)
	}
	if !s.Devices.AcceptsConnectionFrom("10.10.0.5") {
		t.Error("the window did not move to the rebound address")
	}
	// The token the operator already pasted into the router still works.
	if !s.Devices.TryEnrol("10.10.0.5", []byte("mikroview-enrol "+minted.Token)) {
		t.Error("the original token no longer redeems after rebinding, want nothing re-pasted")
	}
}

// TestDeviceEnrolmentRebindRefusesAnAddressNeverTurnedAway: the rebind
// can only point at somewhere that already reached the listener.
func TestDeviceEnrolmentRebindRefusesAnAddressNeverTurnedAway(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", mintBody("10.10.0.1")).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment/address",
		deviceEnrolmentRebindRequest{Address: "203.0.113.7"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an address the listener never turned away", resp.StatusCode)
	}
	if s.Devices.AcceptsConnectionFrom("203.0.113.7") {
		t.Error("a refused rebind still opened the window")
	}
}

// TestDeviceEnrolmentMintDoesNotStarveTheAdminsLogin: the mint's
// password re-check (#1291) counts guesses on the same limiter as
// login, but it must not count them in the same bucket. There is only
// ever one admin (ErrSingleAdmin), so spending login's allowance on a
// run of typos at the Reroll dialog leaves nobody able to let them back
// in: if their session goes in that window -- a closed tab, cleared
// cookies, a second machine -- every sign-in is refused without the
// password ever being checked. The vault-unlock gate already keys its
// own bucket separately (vaultUnlockLimiterKey); this does the same.
func TestDeviceEnrolmentMintDoesNotStarveTheAdminsLogin(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	s.LoginLimiter = auth.NewLoginLimiter(3, time.Minute)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	// The admin fat-fingers their password until the mint stops even
	// checking it.
	var minted int
	for range 4 {
		resp := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment",
			deviceEnrolmentRequest{Password: "not-the-password", ExpectedAddress: "10.10.0.1"})
		resp.Body.Close()
		minted++
		if resp.StatusCode == http.StatusTooManyRequests {
			break
		}
	}
	if minted < 2 {
		t.Fatalf("the mint stopped checking after %d attempts -- the test proves nothing", minted)
	}

	// Their session is gone -- another machine, a cleared cookie jar --
	// and they sign in again with the right password.
	fresh := &http.Client{Jar: mustCookieJar(t)}
	login := postJSON(t, fresh, ts.URL+"/api/auth/login",
		credentialsRequest{Username: "admin", Password: testAdminPassword})
	defer login.Body.Close()
	if login.StatusCode == http.StatusTooManyRequests {
		t.Fatal("wrong passwords at the enrolment dialog locked the only admin out of signing in")
	}
	if login.StatusCode != http.StatusOK {
		t.Errorf("login status = %d, want 200", login.StatusCode)
	}
}
