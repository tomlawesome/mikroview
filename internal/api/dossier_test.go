// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/dossier"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/oui"
	"github.com/tomlawesome/mikroview/internal/store"
)

// getDossier asks the endpoint about one address and decodes the answer.
func getDossier(t *testing.T, base, ip string) (int, dossier.Dossier) {
	t.Helper()
	resp, err := http.Get(base + "/api/hosts/" + ip + "/dossier")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, dossier.Dossier{}
	}
	var d dossier.Dossier
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatalf("decoding the dossier: %v", err)
	}
	return resp.StatusCode, d
}

// seedDossierHost puts a small IoT-ish host into the test server's
// stores: MQTT to a local broker on a five-minute cadence, a DHCP lease
// pushed by the router, and rows in the presence register.
func seedDossierHost(t *testing.T, s *Server, st *store.Store) {
	t.Helper()
	now := time.Now()
	for i := 0; i < 30; i++ {
		at := now.Add(-time.Duration(30-i) * 5 * time.Minute)
		st.Insert(store.Event{
			Time: at, ReceivedAt: at, DeviceID: "core",
			Action: store.Action("accept"), RuleLabel: "lan-out", Chain: "forward",
			Protocol: "tcp", SrcMAC: "d4:8a:fc:11:22:33", InInterface: "bridge-lan",
			SrcIP: "192.168.1.20", SrcPort: 40000 + i, DstIP: "192.168.1.9", DstPort: 1883,
		})
		s.Hosts.Observe("bridge-lan", "192.168.1.20", "sensor-hall", at)
	}
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"dhcp-lease","page":1,"pages":1,`+
		`"records":[{"hostname":"sensor-hall","mac":"d4:8a:fc:11:22:33","address":"192.168.1.20"}]}`), now); err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"arp","page":1,"pages":1,`+
		`"records":[{"address":"192.168.1.20","mac":"d4:8a:fc:11:22:33"}]}`), now); err != nil {
		t.Fatal(err)
	}
	s.Naming = naming.Resolver{Entities: s.Entities, RouterHosts: s.RouterState}
}

func TestHandleHostDossier(t *testing.T) {
	s, st := newTestServer(t)
	seedDossierHost(t, s, st)
	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	status, d := getDossier(t, ts.URL, "192.168.1.20")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if d.IP != "192.168.1.20" {
		t.Errorf("ip = %q, want the address asked about", d.IP)
	}

	// The name, and which layer supplied it -- the lease hostname here,
	// which is what the naming layer prefers.
	if d.Names.Name != "sensor-hall" || d.Names.Source != naming.SourceRouterDHCPLease {
		t.Errorf("names = %+v, want the lease hostname with its provenance", d.Names)
	}

	// Lease versus fixed, from a push that actually covered it.
	if d.Address.Assignment != "lease" || d.Address.Device != "core" {
		t.Errorf("address = %+v, want the lease from core", d.Address)
	}

	// The MAC off the host's own events.
	if !d.MAC.Known || d.MAC.Address != "D4:8A:FC:11:22:33" {
		t.Errorf("mac = %+v, want the observed hardware address", d.MAC)
	}

	// Traffic: the broker, the port, and a cadence.
	if len(d.Traffic.Destinations) != 1 || d.Traffic.Destinations[0].IP != "192.168.1.9" {
		t.Errorf("destinations = %+v, want the broker", d.Traffic.Destinations)
	}
	if len(d.Traffic.Ports) == 0 || d.Traffic.Ports[0].Port != 1883 {
		t.Errorf("ports = %+v, want MQTT", d.Traffic.Ports)
	}
	if d.Traffic.Cadence.Shape != "regular" {
		t.Errorf("cadence = %+v, want a regular shape", d.Traffic.Cadence)
	}

	// First-seen from the presence register, which outlives the ring.
	if !d.Seen.Known || len(d.Seen.Interfaces) != 1 || d.Seen.Interfaces[0] != "bridge-lan" {
		t.Errorf("seen = %+v, want the boundary interface from the register", d.Seen)
	}

	// The rule its traffic matched.
	if !d.Firewall.Known || len(d.Firewall.Rules) == 0 || d.Firewall.Rules[0].Label != "lan-out" {
		t.Errorf("firewall = %+v, want the matched rule", d.Firewall)
	}

	// A suggestion, with evidence and a confidence in words.
	if !d.Identity.Suggested || d.Identity.Profile != "iot" {
		t.Errorf("identity = %+v, want the IoT-ish reading", d.Identity)
	}
	if len(d.Identity.Evidence) == 0 {
		t.Error("a suggestion arrived with no evidence")
	}
	switch d.Identity.Confidence {
	case dossier.Weak, dossier.Fair, dossier.Strong:
	default:
		t.Errorf("confidence = %q, want one of the three words", d.Identity.Confidence)
	}

	// The passive-observer line: a command for the operator, never run.
	if d.SuggestedProbe == nil || !strings.Contains(d.SuggestedProbe.Note, "never connects") {
		t.Errorf("probe = %+v, want a printed command with its never-run note", d.SuggestedProbe)
	}
}

// TestHandleHostDossierAcceptsAHostRegisterKey: the register's own
// endpoints are keyed `<iface>|<ip>`, so a caller holding that key gets
// the dossier rather than a 400.
func TestHandleHostDossierAcceptsAHostRegisterKey(t *testing.T) {
	s, st := newTestServer(t)
	seedDossierHost(t, s, st)
	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	status, d := getDossier(t, ts.URL, "bridge-lan%7C192.168.1.20")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if d.IP != "192.168.1.20" {
		t.Errorf("ip = %q, want the address out of the key", d.IP)
	}
}

// TestHandleHostDossierForAnUnknownAddress is the absence contract at
// the endpoint: 200 with an honest empty card, because "never seen" is
// an answer to the question and not a missing resource.
func TestHandleHostDossierForAnUnknownAddress(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	status, d := getDossier(t, ts.URL, "192.168.1.99")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 with an empty card", status)
	}
	if d.Seen.Known || d.MAC.Known || d.Traffic.Known {
		t.Errorf("something was claimed about an unseen address: %+v", d)
	}
	if len(d.Absent) == 0 {
		t.Error("Absent is empty for an address nothing is known about")
	}
	if d.Identity.Suggested {
		t.Errorf("an identity was suggested from nothing: %+v", d.Identity)
	}
}

func TestHandleHostDossierRefusesSomethingThatIsNotAnAddress(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/hosts/not-an-address/dossier")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleHostDossierReportsTheLocallyAdministeredBit: a container's
// MAC has no vendor by construction, and the card says so in words
// rather than leaving a blank vendor field.
func TestHandleHostDossierReportsTheLocallyAdministeredBit(t *testing.T) {
	s, st := newTestServer(t)
	now := time.Now()
	st.Insert(store.Event{
		Time: now, ReceivedAt: now, DeviceID: "core", Protocol: "tcp",
		SrcMAC: "02:42:ac:11:00:02", InInterface: "bridge-lan",
		SrcIP: "192.168.1.31", SrcPort: 40001, DstIP: "192.168.1.9", DstPort: 8086,
	})
	// A registry that has never fetched: the vendor answer must be
	// "no vendor data yet", never a guess.
	s.OUI = oui.New("", slog.New(slog.NewTextHandler(io.Discard, nil)))

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	status, d := getDossier(t, ts.URL, "192.168.1.31")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !d.MAC.LocallyAdministered {
		t.Fatalf("mac = %+v, want the locally-administered bit reported as its own field", d.MAC)
	}
	if !strings.Contains(d.MAC.LocallyAdministeredNote, "no vendor exists") {
		t.Errorf("note = %q, want it to say no vendor exists", d.MAC.LocallyAdministeredNote)
	}
	if d.MAC.Vendor.Known {
		t.Error("a vendor was named for a locally-administered address")
	}
	if d.MAC.Registry.Loaded {
		t.Errorf("registry = %+v, want an unfetched registry reported as unfetched", d.MAC.Registry)
	}
}

// TestHandleHostDossierSaysWhenNoDHCPTableWasPushed is the enrichment
// rule: a router push only answers for what it actually covered.
func TestHandleHostDossierSaysWhenNoDHCPTableWasPushed(t *testing.T) {
	s, st := newTestServer(t)
	now := time.Now()
	st.Insert(store.Event{
		Time: now, ReceivedAt: now, DeviceID: "core", Protocol: "tcp",
		SrcIP: "192.168.1.40", SrcPort: 40002, DstIP: "192.168.1.9", DstPort: 443,
	})
	// ARP was pushed; DHCP never was. That cannot make the address
	// fixed -- only a pushed DHCP table that omits it could.
	if err := s.RouterState.Apply("core", mustPayload(t, `{"kind":"arp","page":1,"pages":1,`+
		`"records":[{"address":"192.168.1.40","mac":"dc:a6:32:11:22:33"}]}`), now); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	status, d := getDossier(t, ts.URL, "192.168.1.40")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if d.Address.Assignment != "unknown" {
		t.Errorf("assignment = %q, want unknown -- no DHCP table has been pushed", d.Address.Assignment)
	}
	if !strings.Contains(d.Address.Note, "no DHCP table has been pushed") {
		t.Errorf("note = %q, want it to name the missing push", d.Address.Note)
	}
	// The ARP table did cover it, so the MAC is known -- from the
	// router's view of the host rather than from its own events.
	if !d.MAC.Known || !strings.Contains(d.MAC.Source, "ARP") {
		t.Errorf("mac = %+v, want the address from the pushed ARP table", d.MAC)
	}
}
