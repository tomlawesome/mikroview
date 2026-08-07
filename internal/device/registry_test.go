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
