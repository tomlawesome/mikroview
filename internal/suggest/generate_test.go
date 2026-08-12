// SPDX-License-Identifier: AGPL-3.0-only

package suggest

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routerstate"
)

func TestParsePortSpecShapes(t *testing.T) {
	cases := []struct {
		spec string
		want []int
	}{
		{"", nil},
		{"22", []int{22}},
		{"22,23,3389", []int{22, 23, 3389}},
		{"1000-1003", []int{1000, 1001, 1002, 1003}},
		{"22,1000-1002", []int{22, 1000, 1001, 1002}},
		{"not-a-port", nil},
		{"70000", nil},     // out of range
		{"2000-1000", nil}, // inverted range
		{"1-100000", nil},  // would exceed maxPortsPerRule
	}
	for _, c := range cases {
		got := parsePortSpec(c.spec)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parsePortSpec(%q) = %v, want %v", c.spec, got, c.want)
		}
	}
}

func applyPayload(t *testing.T, rs *routerstate.Store, device, body string) {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if err := rs.Apply(device, p, time.Now()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestGenerateDeviceCandidatesOnlyNamedDevices(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[`+
		`{"hostname":"camera","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.10"},`+
		`{"hostname":"","mac":"aa:bb:cc:dd:ee:02","address":"192.168.1.11"}`+
		`]}`)

	got := generateDeviceCandidates(rs, "router-1")
	if len(got) != 1 {
		t.Fatalf("generateDeviceCandidates = %+v, want exactly the named lease", got)
	}
	if got[0].Name != "camera" || got[0].Source.MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("candidate = %+v, unexpected", got[0])
	}
	if got[0].Kind != KindDevice {
		t.Errorf("Kind = %q, want %q", got[0].Kind, KindDevice)
	}
}

func TestGenerateDeviceCandidatesPreferARPAddressOverStaleLease(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"camera","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.10"}]}`)
	applyPayload(t, rs, "router-1", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.99","mac":"aa:bb:cc:dd:ee:01"}]}`)

	got := generateDeviceCandidates(rs, "router-1")
	if len(got) != 1 || got[0].Source.IP != "192.168.1.99" {
		t.Errorf("candidate = %+v, want ARP's more current address (192.168.1.99)", got)
	}
}

func TestGenerateDeviceCandidateIDStableAcrossRegeneration(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"camera","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.10"}]}`)

	first := generateDeviceCandidates(rs, "router-1")
	second := generateDeviceCandidates(rs, "router-1")
	if len(first) != 1 || len(second) != 1 || first[0].ID != second[0].ID {
		t.Errorf("candidate ID not stable across identical regeneration: %+v vs %+v", first, second)
	}
}

func TestGeneratePortCandidatesOnlyDropAndReject(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"comment":"","chain":"input","action":"accept","srcAddressList":"","logPrefix":"","dstPort":"80","protocol":"tcp"},`+
		`{"ordinal":1,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"D|rdp|","dstPort":"3389","protocol":"tcp"},`+
		`{"ordinal":2,"comment":"","chain":"input","action":"reject","srcAddressList":"","logPrefix":"","dstPort":"23","protocol":"tcp"},`+
		`{"ordinal":3,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","dstPort":"","protocol":"tcp"}`+
		`]}`)

	got := generatePortCandidates(rs, "router-1")
	if len(got) != 2 {
		t.Fatalf("generatePortCandidates = %+v, want exactly the two drop/reject rules with a parseable dst-port", got)
	}
	for _, c := range got {
		if c.Kind != KindPort {
			t.Errorf("Kind = %q, want %q", c.Kind, KindPort)
		}
	}
}

func TestGeneratePortCandidateIDStableAcrossReorder(t *testing.T) {
	rs := routerstate.New()
	// Same two rules, different ordinals -- as if the operator inserted
	// a rule above both. The candidate identity must not move with them.
	applyPayload(t, rs, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","dstPort":"3389","protocol":"tcp"}]}`)
	before := generatePortCandidates(rs, "router-1")

	applyPayload(t, rs, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":5,"comment":"moved down","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","dstPort":"3389","protocol":"tcp"}]}`)
	after := generatePortCandidates(rs, "router-1")

	if len(before) != 1 || len(after) != 1 || before[0].ID != after[0].ID {
		t.Errorf("port candidate ID moved when only Ordinal/Comment changed: %+v vs %+v", before, after)
	}
}

func TestGenerateCombinesEveryDevice(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-a", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"camera","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.10"}]}`)
	applyPayload(t, rs, "router-b", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","dstPort":"3389","protocol":"tcp"}]}`)

	got := Generate(rs)
	if len(got) != 2 {
		t.Fatalf("Generate = %+v, want one device candidate from router-a and one port candidate from router-b", got)
	}
}

// Address-list candidates (#274 item 2): one per list a firewall rule
// already scopes by, not one per list the router happens to have and not
// one per address in it.
func TestGenerateAddressListCandidates(t *testing.T) {
	rs := routerstate.New()
	const device = "router-a"

	applyPayload(t, rs, device, `{"kind":"filter-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"chain":"input","action":"accept","srcAddressList":"mgmt","log":true},`+
		// The same list again -- one candidate, not two.
		`{"ordinal":1,"chain":"forward","action":"accept","srcAddressList":"mgmt","log":true},`+
		// A list no push ever supplied the contents of: an accepted
		// entry would match nothing with no way to say why.
		`{"ordinal":2,"chain":"input","action":"drop","srcAddressList":"ghosts","log":true},`+
		// No list at all.
		`{"ordinal":3,"chain":"input","action":"drop","log":true}`+
		`]}`)
	applyPayload(t, rs, device, `{"kind":"address-list","page":1,"pages":1,"records":[`+
		`{"list":"mgmt","address":"192.168.1.50"},`+
		`{"list":"mgmt","address":"192.168.1.51"},`+
		// A list the rules never reference -- routing or bookkeeping,
		// not a statement that these hosts matter.
		`{"list":"bogons","address":"10.0.0.0"}`+
		`]}`)

	var got []Candidate
	for _, c := range Generate(rs) {
		if c.Kind == KindAddressList {
			got = append(got, c)
		}
	}

	if len(got) != 1 {
		t.Fatalf("got %d address-list candidates, want 1 (mgmt only): %+v", len(got), got)
	}
	c := got[0]
	if c.AddressList != "mgmt" {
		t.Errorf("AddressList = %q, want mgmt", c.AddressList)
	}
	if c.RouterDevice != device {
		t.Errorf("RouterDevice = %q, want %q -- a list belongs to a router", c.RouterDevice, device)
	}
	if len(c.Ports) != 0 {
		t.Errorf("Ports = %v, want none -- a list rule says which hosts matter, not which ports", c.Ports)
	}
	// The justification is what an operator reads to decide, so it has
	// to say why this was suggested and how big the list is.
	for _, want := range []string{"mgmt", "address list"} {
		if !strings.Contains(c.Justification, want) {
			t.Errorf("justification does not mention %q: %s", want, c.Justification)
		}
	}
}

// Rules naming lists whose contents were never pushed produce nothing:
// an entry scoped to a list mikroview cannot resolve would match nothing
// with no way to explain it.
func TestGenerateAddressListCandidatesNeedsTheListContents(t *testing.T) {
	rs := routerstate.New()
	applyPayload(t, rs, "router-a", `{"kind":"filter-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"chain":"input","action":"accept","srcAddressList":"mgmt","log":true}`+
		`]}`)

	for _, c := range Generate(rs) {
		if c.Kind == KindAddressList {
			t.Errorf("suggested a list whose contents were never pushed: %+v", c)
		}
	}
}
