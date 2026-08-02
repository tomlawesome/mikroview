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
