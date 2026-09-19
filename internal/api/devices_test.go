// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
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
	admin := setUpAdmin(t, ts)
	return s, ts, admin
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
	admin2 := setUpAdmin(t, ts2)

	refused := deleteNoBody(t, admin2, ts2.URL+"/api/devices/core")
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusBadRequest {
		t.Errorf("deleting a config.yaml device: status = %d, want 400", refused.StatusCode)
	}
}

func TestDeviceEnrolmentMintReplacesAndRerolls(t *testing.T) {
	_, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()

	first := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", nil)
	var firstResp deviceEnrolmentResponse
	if err := json.NewDecoder(first.Body).Decode(&firstResp); err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if firstResp.Token == "" || firstResp.ExpiresAt.IsZero() {
		t.Fatalf("mint response = %+v, want a token and expiry", firstResp)
	}

	second := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", nil)
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
	resp := postJSON(t, admin, ts.URL+"/api/devices/nope/enrolment", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeviceEnrolmentDeleteBurnsThePendingToken(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	postJSON(t, admin, ts.URL+"/api/devices", deviceCreateRequest{Name: "hap-ax3"}).Body.Close()
	mint := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", nil)
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
	mint := postJSON(t, admin, ts.URL+"/api/devices/hap-ax3/enrolment", nil)
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
