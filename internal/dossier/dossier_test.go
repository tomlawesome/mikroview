// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/oui"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeVendors is a VendorSource with a fixed answer, so the assembly
// tests do not need a registry or a network.
type fakeVendors struct {
	vendor oui.Vendor
	status oui.Status
}

func (f fakeVendors) Lookup(m oui.MAC) oui.Vendor {
	if m.LocallyAdministered {
		return oui.Vendor{OUI: m.OUI, Reason: "locally administered address -- no vendor exists to look up"}
	}
	v := f.vendor
	v.OUI = m.OUI
	return v
}

func (f fakeVendors) Status() oui.Status { return f.status }

var testNow = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// TestAssembleKnowingNothing is the absence contract: an address nobody
// has heard of produces a whole dossier that says so, block by block,
// and never a blank field a UI would have to interpret.
func TestAssembleKnowingNothing(t *testing.T) {
	d := Assemble(Input{IP: "10.0.10.77", Now: testNow})

	if d.Seen.Known || d.Names.Known || d.MAC.Known || d.Traffic.Known || d.Firewall.Known {
		t.Fatalf("something claimed knowledge of an unheard-of address: %+v", d)
	}
	if d.Address.Assignment != "unknown" {
		t.Errorf("address assignment = %q, want unknown", d.Address.Assignment)
	}
	if d.Identity.Suggested {
		t.Errorf("an identity was suggested with no evidence at all: %+v", d.Identity)
	}
	for _, note := range []string{d.Seen.Note, d.Names.Note, d.MAC.Note, d.Address.Note, d.Traffic.Note, d.Firewall.Note, d.Identity.Note} {
		if note == "" {
			t.Error("a block reported nothing known without saying why")
		}
	}
	if len(d.Absent) == 0 {
		t.Error("Absent is empty for a dossier that knows nothing")
	}
}

// iotEvents is a small IoT-ish host: MQTT to a local broker, NTP and
// HTTPS to two hosts outside the LAN, on a five-minute cadence.
func iotEvents() []store.Event {
	var evs []store.Event
	base := testNow.Add(-6 * time.Hour)
	for i := 0; i < 40; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Minute)
		evs = append(evs, store.Event{
			Time: at, DeviceID: "core", Action: store.Action("accept"),
			RuleLabel: "lan-out", Chain: "forward",
			Protocol: "tcp", SrcMAC: "d4:8a:fc:11:22:33",
			SrcIP: "10.0.10.5", SrcPort: 40000 + i, DstIP: "10.0.10.2", DstPort: 1883,
		})
		if i%6 == 0 {
			evs = append(evs, store.Event{
				Time: at.Add(time.Minute), DeviceID: "core", Action: store.Action("accept"),
				RuleLabel: "lan-out", Chain: "forward",
				Protocol: "udp", SrcMAC: "d4:8a:fc:11:22:33",
				SrcIP: "10.0.10.5", SrcPort: 51000, DstIP: "162.159.200.1", DstPort: 123,
				DstCountry: "US",
			})
		}
	}
	return evs
}

func TestAssembleReadsAnIoTHost(t *testing.T) {
	in := Input{
		IP:          "10.0.10.5",
		Now:         testNow,
		Events:      iotEvents(),
		WindowStart: testNow.Add(-24 * time.Hour),
		Name:        NameFacts{Name: "sensor-hall", Source: "router-dhcp-lease"},
		Presence:    []Presence{{Iface: "bridge-lan", FirstSeen: testNow.Add(-30 * 24 * time.Hour), LastSeen: testNow}},
		EventMAC:    "d4:8a:fc:11:22:33",
		MACHistory:  &MACHistory{FirstSeen: testNow.Add(-30 * 24 * time.Hour), LastSeen: testNow},
		Routers: []RouterFacts{{
			Device: "core", DHCPPushed: true, ARPPushed: true,
			Lease:  &Lease{MAC: "D4:8A:FC:11:22:33", Hostname: "sensor-hall"},
			ARPMAC: "D4:8A:FC:11:22:33",
		}},
		Vendors: fakeVendors{
			vendor: oui.Vendor{Known: true, Name: "Espressif Inc.", Registry: "MA-L"},
			status: oui.Status{Source: oui.SourceURL, Loaded: true, Entries: 40114, FetchedAt: testNow.Add(-time.Hour)},
		},
		Lookups: Lookups{
			PortName: func(port int) string {
				if port == 1883 {
					return "MQTT"
				}
				return ""
			},
			PeerName:    func(ip string) string { return map[string]string{"10.0.10.2": "broker"}[ip] },
			RuleComment: func(device, label string) (string, bool) { return "allow LAN out", true },
		},
	}
	d := Assemble(in)

	// Names carry their provenance, not just a string.
	if d.Names.Name != "sensor-hall" || d.Names.Source != "router-dhcp-lease" || d.Names.SourceNote == "" {
		t.Errorf("names = %+v, want the name with its provenance in words", d.Names)
	}

	// First-seen comes from the register, which outlives the window.
	if !d.Seen.FirstSeen.Equal(testNow.Add(-30 * 24 * time.Hour)) {
		t.Errorf("firstSeen = %v, want the presence register's", d.Seen.FirstSeen)
	}
	if !strings.Contains(d.Seen.FirstSeenSource, "presence register") {
		t.Errorf("firstSeenSource = %q, want it to name the register", d.Seen.FirstSeenSource)
	}

	// Vendor, and the registry status beside it.
	if !d.MAC.Known || d.MAC.Address != "D4:8A:FC:11:22:33" {
		t.Fatalf("mac = %+v, want the observed address", d.MAC)
	}
	if !d.MAC.Vendor.Known || d.MAC.Vendor.Name != "Espressif Inc." {
		t.Errorf("vendor = %+v, want Espressif", d.MAC.Vendor)
	}
	if d.MAC.Registry.Source == "" || !d.MAC.Registry.Loaded {
		t.Errorf("registry status = %+v, want the vendor's provenance shown with it", d.MAC.Registry)
	}
	if d.MAC.LocallyAdministered {
		t.Error("a universally-administered address was reported as locally administered")
	}

	// Lease, from a push that actually covered it.
	if d.Address.Assignment != "lease" || d.Address.Device != "core" {
		t.Errorf("address = %+v, want the lease from core", d.Address)
	}

	// Traffic: the broker is the top destination, MQTT the top port.
	if len(d.Traffic.Destinations) == 0 || d.Traffic.Destinations[0].IP != "10.0.10.2" {
		t.Fatalf("destinations = %+v, want the broker first", d.Traffic.Destinations)
	}
	if d.Traffic.Destinations[0].Name != "broker" {
		t.Errorf("destination name = %q, want the friendly name", d.Traffic.Destinations[0].Name)
	}
	if len(d.Traffic.Ports) == 0 || d.Traffic.Ports[0].Port != 1883 || d.Traffic.Ports[0].Name != "MQTT" {
		t.Errorf("ports = %+v, want MQTT first with its known name", d.Traffic.Ports)
	}
	if d.Traffic.Cadence.Shape != "regular" {
		t.Errorf("cadence = %+v, want a regular shape for five-minute traffic", d.Traffic.Cadence)
	}

	// Firewall rules the traffic matched, with the pushed comment.
	if !d.Firewall.Known || len(d.Firewall.Rules) == 0 {
		t.Fatalf("firewall = %+v, want the matched rule", d.Firewall)
	}
	if d.Firewall.Rules[0].Label != "lan-out" || !d.Firewall.Rules[0].CommentKnown {
		t.Errorf("rule = %+v, want the label and the pushed comment", d.Firewall.Rules[0])
	}

	// And the suggestion, with its evidence.
	if d.Identity.Profile != "iot" {
		t.Errorf("identity = %+v, want the IoT-ish reading", d.Identity)
	}
	if d.Identity.Confidence != Fair || len(d.Identity.Evidence) == 0 {
		t.Errorf("identity = %+v, want fair confidence with evidence", d.Identity)
	}
	if d.SuggestedProbe == nil || !strings.Contains(d.SuggestedProbe.Command, "10.0.10.5") {
		t.Errorf("probe = %+v, want a command for this host", d.SuggestedProbe)
	}
}

// TestLocallyAdministeredIsItsOwnFinding: the bit is reported as a
// field, with words explaining that no vendor exists -- not as a failed
// vendor lookup.
func TestLocallyAdministeredIsItsOwnFinding(t *testing.T) {
	d := Assemble(Input{
		IP:       "10.0.10.31",
		Now:      testNow,
		EventMAC: "02:42:ac:11:00:02",
		Vendors:  fakeVendors{status: oui.Status{Loaded: true, Entries: 40114}},
	})
	if !d.MAC.LocallyAdministered {
		t.Fatalf("mac = %+v, want the locally-administered bit reported", d.MAC)
	}
	if !strings.Contains(d.MAC.LocallyAdministeredNote, "no vendor exists") {
		t.Errorf("note = %q, want it to say no vendor exists", d.MAC.LocallyAdministeredNote)
	}
	if d.MAC.Vendor.Known {
		t.Error("a vendor was named for a locally-administered address")
	}
	if d.Identity.Profile != "virtual" {
		t.Errorf("identity = %+v, want the virtual-interface reading", d.Identity)
	}
}

// TestLeaseVersusFixedOnlyAnswersFromAPushThatCoveredIt is the absence
// rule at its sharpest: a router that has never pushed a DHCP table
// cannot make an address "fixed".
func TestLeaseVersusFixedOnlyAnswersFromAPushThatCoveredIt(t *testing.T) {
	tests := []struct {
		name    string
		routers []RouterFacts
		want    string
		wantNot string
	}{
		{
			name:    "no router state at all",
			routers: nil,
			want:    "unknown",
		},
		{
			name:    "a router that has pushed ARP but never DHCP",
			routers: []RouterFacts{{Device: "core", ARPPushed: true, ARPMAC: "DC:A6:32:11:22:33"}},
			want:    "unknown",
			wantNot: "not-a-lease",
		},
		{
			name:    "a DHCP table that was pushed and does not list it",
			routers: []RouterFacts{{Device: "core", DHCPPushed: true}},
			want:    "not-a-lease",
		},
		{
			name:    "a DHCP table that lists it",
			routers: []RouterFacts{{Device: "core", DHCPPushed: true, Lease: &Lease{MAC: "DC:A6:32:11:22:33", Hostname: "nas"}}},
			want:    "lease",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Assemble(Input{IP: "10.0.10.8", Now: testNow, Routers: tc.routers})
			if d.Address.Assignment != tc.want {
				t.Errorf("assignment = %q, want %q (note: %s)", d.Address.Assignment, tc.want, d.Address.Note)
			}
			if d.Address.Note == "" {
				t.Error("the address block gave a verdict with no explanation")
			}
		})
	}
}

// TestVendorFeedOffIsSaidNotGuessed: with the OUI feed switched off,
// the card says the feed is off rather than reporting an unknown
// vendor, which would read as "this prefix is unassigned".
func TestVendorFeedOffIsSaidNotGuessed(t *testing.T) {
	d := Assemble(Input{IP: "10.0.10.8", Now: testNow, EventMAC: "DC:A6:32:11:22:33"})
	if d.MAC.Vendor.Known {
		t.Fatal("a vendor was named with no registry wired up")
	}
	if !strings.Contains(d.MAC.Vendor.Reason, "switched off") {
		t.Errorf("reason = %q, want it to name the switched-off feed", d.MAC.Vendor.Reason)
	}
	var mentioned bool
	for _, a := range d.Absent {
		if strings.Contains(a, "vendor") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("Absent = %v, want the missing vendor data listed", d.Absent)
	}
}

// TestInboundTrafficNamesTheTalkers checks the direction split: who
// reached this host, and on which of its own ports.
func TestInboundTrafficNamesTheTalkers(t *testing.T) {
	var evs []store.Event
	for i := 0; i < 30; i++ {
		at := testNow.Add(-time.Duration(i) * time.Minute)
		evs = append(evs,
			store.Event{Time: at, DeviceID: "core", Protocol: "tcp",
				SrcIP: "10.0.10.20", SrcPort: 50000 + i, DstIP: "10.0.10.8", DstPort: 445},
			store.Event{Time: at.Add(time.Second), DeviceID: "core", Protocol: "tcp",
				SrcIP: "10.0.10.21", SrcPort: 51000 + i, DstIP: "10.0.10.8", DstPort: 3389},
		)
	}
	d := Assemble(Input{IP: "10.0.10.8", Now: testNow, Events: evs})

	if len(d.Traffic.Talkers) != 2 {
		t.Fatalf("talkers = %+v, want the two hosts that reached it", d.Traffic.Talkers)
	}
	if len(d.Traffic.Destinations) != 0 {
		t.Errorf("destinations = %+v, want none -- this host reached nothing", d.Traffic.Destinations)
	}
	if d.Identity.Profile != "windows" {
		t.Errorf("identity = %+v, want the Windows-ish reading from SMB and RDP", d.Identity)
	}
	if d.SuggestedProbe == nil || !strings.Contains(d.SuggestedProbe.Command, "445,3389") {
		t.Errorf("probe = %+v, want it to scan the ports seen answering", d.SuggestedProbe)
	}
}
