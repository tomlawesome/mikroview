// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
)

func TestResolveConfiguredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
	})

	id := r.Resolve("192.168.1.1", time.Now())
	if id != "core" {
		t.Errorf("Resolve() = %q, want %q", id, "core")
	}

	devices := r.List()
	if len(devices) != 1 || !devices[0].Configured || devices[0].EventCount != 1 {
		t.Errorf("unexpected device state: %+v", devices)
	}
	if devices[0].FirstSeen.IsZero() {
		t.Errorf("expected FirstSeen to be set for a configured device, got zero value")
	}
}

func TestResolveAutoDiscoversUnknownSource(t *testing.T) {
	r := NewRegistry(nil)

	id := r.Resolve("10.0.0.5", time.Now())
	if id != "10.0.0.5" {
		t.Errorf("Resolve() = %q, want %q", id, "10.0.0.5")
	}

	devices := r.List()
	if len(devices) != 1 || devices[0].Configured {
		t.Errorf("expected one auto-discovered, unconfigured device: %+v", devices)
	}
}

func TestResolveIncrementsEventCount(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Resolve("10.0.0.5", now)
	r.Resolve("10.0.0.5", now.Add(time.Second))
	r.Resolve("10.0.0.5", now.Add(2*time.Second))

	devices := r.List()
	if len(devices) != 1 || devices[0].EventCount != 3 {
		t.Errorf("expected EventCount=3, got %+v", devices)
	}
}

// Different textual forms of the same address (here, IPv6 shorthand vs.
// its fully-expanded form) must resolve to the same device entry rather
// than silently splitting one router's events across two -- normalizeIP
// re-serializes through net.IP.String() specifically to collapse this.
func TestResolveNormalizesEquivalentIPForms(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()

	r.Resolve("::1", now)
	r.Resolve("0:0:0:0:0:0:0:1", now.Add(time.Second))

	devices := r.List()
	if len(devices) != 1 {
		t.Fatalf("expected both forms to resolve to one device, got %d: %+v", len(devices), devices)
	}
	if devices[0].EventCount != 2 {
		t.Errorf("EventCount = %d, want 2", devices[0].EventCount)
	}
	if devices[0].SourceIP != "::1" {
		t.Errorf("SourceIP = %q, want the normalized form %q", devices[0].SourceIP, "::1")
	}
}

// A source that isn't a parseable IP at all (shouldn't happen from a real
// syslog listener, which always supplies conn.RemoteAddr()'s host, but
// normalizeIP has no other caller to guarantee that) falls back to the
// raw string unchanged rather than losing the value.
func TestNormalizeIPFallsBackForUnparseableInput(t *testing.T) {
	r := NewRegistry(nil)
	id := r.Resolve("not-an-ip", time.Now())
	if id != "not-an-ip" {
		t.Errorf("Resolve() = %q, want the unparsed input returned unchanged", id)
	}
}

// List() must return an independent copy: mutating a returned Info (or
// the slice itself) must not corrupt the registry's own state, since
// callers on the /api/devices read path have no other isolation from
// concurrent ingest-side writes.
func TestListReturnsIndependentSnapshot(t *testing.T) {
	r := NewRegistry(nil)
	r.Resolve("10.0.0.5", time.Now())

	devices := r.List()
	devices[0].EventCount = 999
	devices[0].Name = "tampered"

	fresh := r.List()
	if fresh[0].EventCount == 999 || fresh[0].Name == "tampered" {
		t.Errorf("mutating a List() result affected subsequent List() output: %+v", fresh)
	}
}

func TestListIncludesConfiguredAndAutoDiscovered(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("192.168.1.1", time.Now())
	r.Resolve("10.0.0.9", time.Now())

	devices := r.List()
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices (1 configured + 1 auto-discovered), got %d: %+v", len(devices), devices)
	}

	var sawConfigured, sawDiscovered bool
	for _, d := range devices {
		if d.SourceIP == "192.168.1.1" && d.Configured {
			sawConfigured = true
		}
		if d.SourceIP == "10.0.0.9" && !d.Configured {
			sawDiscovered = true
		}
	}
	if !sawConfigured || !sawDiscovered {
		t.Errorf("expected one configured + one auto-discovered device, got %+v", devices)
	}
}

// TestMultihomedCandidatesFlagsSilentDeclaredDevice is issue #442's core
// scenario: a router is declared under one address, its syslog actually
// arrives from another (a different, VLAN-facing interface), so the
// declared device never receives an event and the real traffic
// auto-discovers as a second, undeclared device.
func TestMultihomedCandidatesFlagsSilentDeclaredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("10.10.0.1", time.Now())

	got := r.MultihomedCandidates()
	if len(got) != 1 {
		t.Fatalf("MultihomedCandidates() returned %d candidates, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.DeclaredID != "core-router" || c.DeclaredSourceIP != "192.168.1.1" {
		t.Errorf("unexpected declared side: %+v", c)
	}
	if len(c.Discovered) != 1 || c.Discovered[0].SourceIP != "10.10.0.1" || c.Discovered[0].Configured {
		t.Errorf("unexpected discovered side: %+v", c.Discovered)
	}
}

// TestMultihomedCandidatesEmptyWhenDeclaredDeviceHasTraffic guards
// against false positives: a declared device that has actually received
// its own events is not "silent" just because some other, unrelated
// device was also discovered.
func TestMultihomedCandidatesEmptyWhenDeclaredDeviceHasTraffic(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("192.168.1.1", time.Now())
	r.Resolve("10.10.0.1", time.Now())

	if got := r.MultihomedCandidates(); got != nil {
		t.Errorf("MultihomedCandidates() = %+v, want nil: declared device has traffic under its own id", got)
	}
}

// TestMultihomedCandidatesEmptyWithNoDiscoveredDevices guards the other
// false-positive direction: a declared device with no traffic yet is
// unremarkable on its own when nothing has been discovered either -- the
// router may simply not have started logging yet.
func TestMultihomedCandidatesEmptyWithNoDiscoveredDevices(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})

	if got := r.MultihomedCandidates(); got != nil {
		t.Errorf("MultihomedCandidates() = %+v, want nil: nothing discovered at all", got)
	}
}

// TestMultihomedCandidatesListsEveryDiscoveredDevice: Registry cannot
// itself tell which discovered device (if any) is the same physical
// router as a silent declared one, so with more than one discovered
// device it must report all of them rather than guessing at one -- the
// same "several rules share a prefix, the honest answer is all of them"
// rule RulesForLogPrefix follows.
func TestMultihomedCandidatesListsEveryDiscoveredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("10.10.0.1", time.Now())
	r.Resolve("10.10.0.2", time.Now())

	got := r.MultihomedCandidates()
	if len(got) != 1 || len(got[0].Discovered) != 2 {
		t.Fatalf("expected 1 candidate with 2 discovered devices, got %+v", got)
	}
}

// fixedNames is a NameLookup standing in for internal/naming.Resolver:
// device must not import it (see NameLookup's comment), and what the
// registry does with an answer is the same whatever produced it.
type fixedNames map[string]string

func (f fixedNames) Device(id string) string { return f[id] }

// TestListServesTheStoredName is issue #600's requirement at the layer
// it is stored: a rename one person saved is what every reader of List
// gets -- GET /api/devices, the wizard's command blocks, the silence
// flag's wording -- rather than the name only that person's browser
// knows about.
func TestListServesTheStoredName(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}})
	r.Resolve("192.168.1.1", time.Now())
	r.Resolve("10.0.0.9", time.Now())
	r.SetNames(fixedNames{"10.0.0.9": "lab crs"})

	got := map[string]string{}
	for _, info := range r.List() {
		got[info.ID] = info.Name
	}
	if got["10.0.0.9"] != "lab crs" {
		t.Errorf("discovered device name = %q, want the stored rename", got["10.0.0.9"])
	}
	if got["core"] != "Core Router" {
		t.Errorf("configured device name = %q, want config.yaml's own name", got["core"])
	}
}

// A declaration with no name, and a registry with no resolver at all,
// both still display as something: the raw id. Nothing renders an empty
// device cell, and removing a label restores the id the editor promised.
func TestListFallsBackToTheRawID(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "unnamed", SourceIP: "192.168.1.2"}})
	r.Resolve("192.168.1.2", time.Now())

	list := r.List()
	if len(list) != 1 || list[0].Name != "unnamed" {
		t.Fatalf("List() = %+v, want the id as the displayed name", list)
	}

	r.SetNames(fixedNames{"unnamed": "Spare"})
	if list = r.List(); list[0].Name != "Spare" {
		t.Errorf("Name = %q, want the stored rename once one exists", list[0].Name)
	}
}

// Order is stable, configured first then by id. Scenarios and the setup
// wizard read devices[0] as "the router this instance watches"; under
// the old map order a second device made that answer change from one
// request to the next.
func TestListOrdersConfiguredFirstThenByID(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "zeta", Name: "Zeta", SourceIP: "192.168.1.9"},
		{ID: "alpha", Name: "Alpha", SourceIP: "192.168.1.1"},
	})
	r.Resolve("10.0.0.9", time.Now())
	r.Resolve("10.0.0.2", time.Now())

	for i := 0; i < 20; i++ {
		var ids []string
		for _, info := range r.List() {
			ids = append(ids, info.ID)
		}
		want := []string{"alpha", "zeta", "10.0.0.2", "10.0.0.9"}
		for j := range want {
			if ids[j] != want[j] {
				t.Fatalf("List() ids = %v, want %v", ids, want)
			}
		}
	}
}
