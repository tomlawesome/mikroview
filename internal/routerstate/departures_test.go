// SPDX-License-Identifier: AGPL-3.0-only

package routerstate

import (
	"testing"
	"time"
)

// addressPage is one /ip/address page as the endpoint would have
// accepted it, so these tests exercise the real decode path like every
// other test in this package.
func addressPage(page, pages int, records string) string {
	return `{"kind":"ip-address","page":` + itoa(page) + `,"pages":` + itoa(pages) + `,"records":[` + records + `]}`
}

const (
	lab  = `{"address":"192.0.2.1/24","network":"192.0.2.0","interface":"ether3","comment":"lab"}`
	desk = `{"address":"198.51.100.1/24","network":"198.51.100.0","interface":"ether4","comment":"desks"}`
)

func TestNoDeparturesUntilTwoCompleteCyclesHaveBeenSeen(t *testing.T) {
	// A restart must not report every segment as departed on the first
	// push. The first complete cycle is a baseline, not a diff.
	s := New()
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))

	if got := s.Departures(); len(got) != 0 {
		t.Fatalf("the first complete cycle produced %d departures, want 0", len(got))
	}
}

func TestALaterCompleteCycleWithoutTheSegmentIsADeparture(t *testing.T) {
	// The honest moment a segment "goes": the operator retired the
	// subnet on the router, the router re-pushed, and the range that was
	// there is not there any more.
	s := New()
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))
	apply(t, s, "router-1", addressPage(1, 1, desk))

	got := s.Departures()
	if len(got) != 1 {
		t.Fatalf("got %d departures, want 1", len(got))
	}
	d := got[0]
	if d.CIDR != "192.0.2.0/24" {
		t.Errorf("departed range is %q, want the masked network %q", d.CIDR, "192.0.2.0/24")
	}
	if d.Address != "192.0.2.1/24" {
		t.Errorf("the row as the router wrote it is %q, want %q", d.Address, "192.0.2.1/24")
	}
	if d.Interface != "ether3" {
		t.Errorf("interface is %q, want the zone identity the map keys on", d.Interface)
	}
	if d.Name != "lab" {
		t.Errorf("name is %q, want the address comment %q", d.Name, "lab")
	}
	if d.Device != "router-1" {
		t.Errorf("device is %q, want %q", d.Device, "router-1")
	}
	if d.DepartedAt.IsZero() {
		t.Error("departure has no timestamp")
	}
}

func TestAnIncompleteCycleIsNeverADeparture(t *testing.T) {
	// This is the trap the whole file exists to avoid: a push that
	// dropped one of two pages would otherwise look like half the estate
	// being decommissioned at once. Absence of a page is not evidence.
	s := New()
	apply(t, s, "router-1", addressPage(1, 2, lab))
	apply(t, s, "router-1", addressPage(2, 2, desk))
	if got := s.Departures(); len(got) != 0 {
		t.Fatalf("the baseline cycle produced %d departures", len(got))
	}

	// A new two-page cycle begins and only its first page ever arrives.
	apply(t, s, "router-1", addressPage(1, 2, desk))
	if got := s.Departures(); len(got) != 0 {
		t.Fatalf("a half-arrived cycle produced %d departures, want 0 -- a missing page is not a deleted segment", len(got))
	}

	// The second page completes it, and nothing actually left.
	apply(t, s, "router-1", addressPage(2, 2, lab))
	if got := s.Departures(); len(got) != 0 {
		t.Errorf("the completed cycle produced %d departures, but it carries both ranges", len(got))
	}
}

func TestARangeThatComesBackIsNotADeparture(t *testing.T) {
	s := New()
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))
	if got := s.Departures(); len(got) != 0 {
		t.Errorf("an unchanged table produced %d departures", len(got))
	}
}

func TestADepartureCarriesTheRoutersLastKnownNamesInsideTheRange(t *testing.T) {
	// The enrichment #460 asks for, snapshotted at the moment it is
	// about to stop being knowable: the lease and ARP tables for a
	// retired range drain away over the pushes that follow.
	s := New()
	apply(t, s, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[`+
		`{"hostname":"garage-cam","mac":"aa:bb:cc:dd:ee:01","address":"192.0.2.7"},`+
		`{"hostname":"desk-pc","mac":"aa:bb:cc:dd:ee:02","address":"198.51.100.5"}]}`)
	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"records":[`+
		`{"address":"192.0.2.9","mac":"aa:bb:cc:dd:ee:03"}]}`)
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))
	apply(t, s, "router-1", addressPage(1, 1, desk))

	got := s.Departures()
	if len(got) != 1 {
		t.Fatalf("got %d departures, want 1", len(got))
	}
	known := got[0].LastKnown
	if len(known) != 2 {
		t.Fatalf("the departure carries %d known hosts, want 2 (the lease and the ARP entry inside the range)", len(known))
	}
	if known[0].Address != "192.0.2.7" || known[0].Name != "garage-cam" || known[0].Source != HostSourceDHCPLease {
		t.Errorf("first known host is %+v, want the named lease inside the range", known[0])
	}
	// An ARP entry proves the address was live without naming it, which
	// is still worth carrying -- and it must not invent a name.
	if known[1].Address != "192.0.2.9" || known[1].Name != "" {
		t.Errorf("second known host is %+v, want a bare address with no invented name", known[1])
	}
	for _, k := range known {
		if k.Address == "198.51.100.5" {
			t.Error("a host outside the retired range was carried into its departure")
		}
	}
}

func TestADepartureFallsBackToTheInterfaceWhenTheRowHasNoComment(t *testing.T) {
	s := New()
	const unnamed = `{"address":"203.0.113.1/24","network":"203.0.113.0","interface":"ether9","comment":""}`
	apply(t, s, "router-1", addressPage(1, 1, unnamed+","+desk))
	apply(t, s, "router-1", addressPage(1, 1, desk))

	got := s.Departures()
	if len(got) != 1 {
		t.Fatalf("got %d departures, want 1", len(got))
	}
	if got[0].Name != "ether9" {
		t.Errorf("name is %q, want the interface as the fallback", got[0].Name)
	}
}

func TestAckDepartureAnswersTheOfferOnce(t *testing.T) {
	// Yes and no both land here: the offer is answered once and does not
	// come back, because the next push does not carry the range either
	// and would otherwise re-offer it forever.
	s := New()
	apply(t, s, "router-1", addressPage(1, 1, lab+","+desk))
	apply(t, s, "router-1", addressPage(1, 1, desk))

	if !s.AckDeparture("router-1", "192.0.2.0/24") {
		t.Fatal("acknowledging a pending offer reported nothing to acknowledge")
	}
	if got := s.Departures(); len(got) != 0 {
		t.Errorf("%d offers remain after the only one was answered", len(got))
	}
	if s.AckDeparture("router-1", "192.0.2.0/24") {
		t.Error("acknowledging the same offer twice reported a second one")
	}
	if s.AckDeparture("router-2", "192.0.2.0/24") {
		t.Error("acknowledging another device's offer reported a match")
	}

	// A further push that still does not carry the range must not
	// resurrect the answered offer.
	apply(t, s, "router-1", addressPage(1, 1, desk))
	if got := s.Departures(); len(got) != 0 {
		t.Errorf("a later push re-offered an answered departure (%d pending)", len(got))
	}
}

func TestDeparturesAreReportedNewestFirstAcrossDevices(t *testing.T) {
	s := New()
	for _, device := range []string{"router-1", "router-2"} {
		if err := s.Apply(device, decode(t, addressPage(1, 1, lab+","+desk)), time.Now()); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	if err := s.Apply("router-1", decode(t, addressPage(1, 1, desk)), time.Unix(1000, 0)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := s.Apply("router-2", decode(t, addressPage(1, 1, desk)), time.Unix(2000, 0)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := s.Departures()
	if len(got) != 2 {
		t.Fatalf("got %d departures, want 2", len(got))
	}
	if got[0].Device != "router-2" {
		t.Errorf("newest departure is from %q, want router-2 -- the list is not newest first", got[0].Device)
	}
}

func TestPendingDeparturesAreBounded(t *testing.T) {
	// A router flapping its address table must not grow this without
	// bound. The ceiling evicts the oldest, because the newest departure
	// is the one an operator is most likely to be looking for.
	s := New()
	prev := maxDepartures
	maxDepartures = 2
	defer func() { maxDepartures = prev }()

	rows := []string{
		`{"address":"192.0.2.1/24","network":"192.0.2.0","interface":"e1","comment":"a"}`,
		`{"address":"198.51.100.1/24","network":"198.51.100.0","interface":"e2","comment":"b"}`,
		`{"address":"203.0.113.1/24","network":"203.0.113.0","interface":"e3","comment":"c"}`,
	}
	apply(t, s, "router-1", addressPage(1, 1, rows[0]+","+rows[1]+","+rows[2]))
	apply(t, s, "router-1", addressPage(1, 1, rows[0]+","+rows[1]))
	apply(t, s, "router-1", addressPage(1, 1, rows[0]))
	apply(t, s, "router-1", addressPage(1, 1, ""))

	if got := s.Departures(); len(got) != 2 {
		t.Errorf("%d pending departures held, want the ceiling of 2", len(got))
	}
}
