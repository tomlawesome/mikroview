// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routerstate"
)

// #1451: paths no other test reached.

func TestFindDepartureMatchesDeviceAndRangeInAnyWrittenForm(t *testing.T) {
	s, _ := decommissionServer(t)
	lab := ingest.IPAddressEntry{Address: "192.0.2.1/24", Network: "192.0.2.0", Interface: "ether3", Comment: "lab"}
	desks := ingest.IPAddressEntry{Address: "198.51.100.1/24", Network: "198.51.100.0", Interface: "ether4", Comment: "desks"}
	pushIPAddresses(t, s, "core", lab, desks)
	pushIPAddresses(t, s, "core", desks) // lab disappears across two cycles

	d, ok := s.findDeparture("core", "192.0.2.0/24")
	if !ok || d.Device != "core" || d.CIDR != "192.0.2.0/24" || d.Interface != "ether3" {
		t.Fatalf("findDeparture(core, 192.0.2.0/24) = %+v, %v", d, ok)
	}
	// A host address inside the range is normalised to the network.
	if _, ok := s.findDeparture("core", "192.0.2.1/24"); !ok {
		t.Error("the range written as a host address was not found")
	}
	if _, ok := s.findDeparture("edge", "192.0.2.0/24"); ok {
		t.Error("found a departure on a device that never retired it")
	}
	if _, ok := s.findDeparture("core", "198.51.100.0/24"); ok {
		t.Error("found a range the router still carries")
	}
	if _, ok := s.findDeparture("core", "not a range"); ok {
		t.Error("found a departure for an unparseable range")
	}
	s.RouterState = nil
	if _, ok := s.findDeparture("core", "192.0.2.0/24"); ok {
		t.Error("found a departure with no router state")
	}
}

func TestStragglersFromCopiesTheFrozenSnapshot(t *testing.T) {
	if got := stragglersFrom(nil); got != nil {
		t.Errorf("stragglersFrom(nil) = %v, want nil", got)
	}
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	known := []routerstate.KnownHost{
		{Address: "192.0.2.7", Name: "garage-cam", Source: routerstate.HostSourceDHCPLease, SeenAt: at},
		{Address: "192.0.2.8", SeenAt: at.Add(time.Minute)},
	}
	got := stragglersFrom(known)
	if len(got) != 2 {
		t.Fatalf("got %d stragglers, want 2", len(got))
	}
	if got[0].Address != "192.0.2.7" || got[0].Name != "garage-cam" || got[0].Source != routerstate.HostSourceDHCPLease || !got[0].SeenAt.Equal(at) {
		t.Errorf("first straggler = %+v", got[0])
	}
	if got[1].Name != "" || got[1].Source != "" {
		t.Errorf("second straggler = %+v, want no name or source", got[1])
	}
	known[0].Name = "changed"
	if got[0].Name != "garage-cam" {
		t.Error("stragglersFrom aliased its input instead of copying")
	}
}

func TestVaultStateWord(t *testing.T) {
	if vaultStateWord(true) != "locked" || vaultStateWord(false) != "unlocked" {
		t.Errorf("vaultStateWord = %q / %q", vaultStateWord(true), vaultStateWord(false))
	}
}
