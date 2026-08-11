// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"testing"
)

// The per-source cap bounds one address, not thirty-two. With a global
// 256 and 8 per source, 32 addresses fill the listener -- trivial for a
// single host holding a routed IPv6 /64 -- and because the idle timeout
// resets on every read, holding them costs almost nothing. A real router
// dialling in afterwards was accepted and immediately closed, its lines
// never reaching the pipeline.
//
// Reserving capacity for declared devices is the answer device.Registry
// already gives to the same class of problem. See #285 finding 8.
func TestReservedSlotsOnlyExistWhenDevicesAreDeclared(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })

	SetConfiguredSources(nil)
	if got := reservedSlots(); got != 0 {
		t.Errorf("reservedSlots with nothing declared = %d, want 0 -- holding capacity back for nobody only shrinks the pool", got)
	}

	SetConfiguredSources([]string{"192.0.2.1"})
	want := maxTCPConnections / reservedFraction
	if got := reservedSlots(); got != want {
		t.Errorf("reservedSlots = %d, want %d", got, want)
	}
	if reservedSlots() >= maxTCPConnections {
		t.Error("the reservation consumed the whole pool -- discovery is still the normal path and must keep capacity")
	}
}

// A declared device must be recognised regardless of which form its
// address arrives in: a dual-stack listener reports an IPv4 peer as
// "::ffff:192.0.2.1", while config.yaml holds "192.0.2.1".
func TestConfiguredSourcesMatchAcrossAddressForms(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })
	SetConfiguredSources([]string{" 192.0.2.1 ", "2001:db8::1"})

	for _, host := range []string{"192.0.2.1", "::ffff:192.0.2.1", "2001:db8::1", "2001:0db8:0000::1"} {
		if !isConfiguredSource(normaliseHost(host)) {
			t.Errorf("%q was not recognised as a declared device", host)
		}
	}
	for _, host := range []string{"198.51.100.9", "2001:db8::2", ""} {
		if isConfiguredSource(normaliseHost(host)) {
			t.Errorf("%q was treated as a declared device", host)
		}
	}
}

// Stats is what makes a blackout visible. Before it, the only trace was
// a repeated container-log WARN -- which means visible to nobody.
func TestStatsReportsCapacityAndReservation(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })
	SetConfiguredSources([]string{"192.0.2.1"})

	s := Stats()
	if s.Capacity != maxTCPConnections {
		t.Errorf("Capacity = %d, want %d", s.Capacity, maxTCPConnections)
	}
	if s.ReservedForConfigured != reservedSlots() {
		t.Errorf("ReservedForConfigured = %d, want %d", s.ReservedForConfigured, reservedSlots())
	}

	// A refusal of a declared router is the condition worth surfacing,
	// so it is counted separately from refusals in general.
	before := Stats()
	noteRejected("192.0.2.1")
	noteRejected("198.51.100.9")
	after := Stats()
	if after.Rejected != before.Rejected+2 {
		t.Errorf("Rejected = %d, want %d", after.Rejected, before.Rejected+2)
	}
	if after.RejectedConfigured != before.RejectedConfigured+1 {
		t.Errorf("RejectedConfigured = %d, want %d -- only the declared router should count", after.RejectedConfigured, before.RejectedConfigured+1)
	}
}
