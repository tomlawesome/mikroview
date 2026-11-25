// SPDX-License-Identifier: AGPL-3.0-only

package routerstate

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// #1451: paths no other test reached.

func TestInAddressListIsAnExactTextMatchPerDeviceAndList(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"address-list","page":1,"pages":1,"records":[{"list":"blocked","address":"198.51.100.9","comment":"","dynamic":false},{"list":"blocked","address":"198.51.100.0/24","comment":"","dynamic":false},{"list":"other","address":"198.51.100.77","comment":"","dynamic":false}]}`)
	cases := []struct {
		device, list, ip string
		want             bool
	}{
		{"router-1", "blocked", "198.51.100.9", true},
		{"router-1", "other", "198.51.100.77", true},
		{"router-1", "blocked", "198.51.100.77", false}, // listed, but on another list
		{"router-1", "blocked", "198.51.100.10", false},
		{"router-1", "blocked", "198.51.100.0/24", true}, // text, not a containment test
		{"router-2", "blocked", "198.51.100.9", false},   // another device
		{"", "blocked", "198.51.100.9", false},
		{"router-1", "", "198.51.100.9", false},
		{"router-1", "blocked", "", false},
	}
	for _, c := range cases {
		if got := s.InAddressList(c.device, c.list, c.ip); got != c.want {
			t.Errorf("InAddressList(%q, %q, %q) = %v, want %v", c.device, c.list, c.ip, got, c.want)
		}
	}
}

func TestPushedKindsReportsOnlyTablesThatArrived(t *testing.T) {
	s := New()
	if got := s.PushedKinds("router-1"); got != nil {
		t.Errorf("PushedKinds for an unknown device = %v, want nil", got)
	}
	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.0.2.5","mac":"aa:bb:cc:dd:ee:05"}]}`)
	apply(t, s, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"a","mac":"aa:bb:cc:dd:ee:01","address":"192.0.2.1"}]}`)
	// Page 2 of 2 arriving alone is still reported: any accepted page
	// makes the kind count as pushed, complete or not.
	apply(t, s, "router-1", `{"kind":"filter-rule","page":2,"pages":2,"records":[{"ordinal":2,"comment":"c","chain":"forward","action":"drop","srcAddressList":"","logPrefix":""}]}`)
	got := s.PushedKinds("router-1")
	if len(got) != 3 || got[ingest.KindARP].IsZero() || got[ingest.KindDHCPLease].IsZero() || got[ingest.KindFilterRule].IsZero() {
		t.Fatalf("PushedKinds = %v, want exactly arp, dhcp-lease and filter-rule with their arrival times", got)
	}
	if other := s.PushedKinds("router-2"); other != nil {
		t.Errorf("another device's kinds = %v", other)
	}

	// A page the size cap refuses is not a table that arrived.
	old := maxRecordsPerKind
	maxRecordsPerKind = 1
	defer func() { maxRecordsPerKind = old }()
	over := decode(t, `{"kind":"address-list","page":1,"pages":1,"records":[{"list":"a","address":"192.0.2.1","comment":"","dynamic":false},{"list":"a","address":"192.0.2.2","comment":"","dynamic":false}]}`)
	if err := s.Apply("router-1", over, time.Now()); err == nil {
		t.Fatal("Apply of a page over the cap succeeded, want a refusal")
	}
	got = s.PushedKinds("router-1")
	if _, ok := got[ingest.KindAddressList]; ok || len(got) != 3 {
		t.Errorf("PushedKinds after a refused page = %v, want still only arp, dhcp-lease and filter-rule", got)
	}
}
