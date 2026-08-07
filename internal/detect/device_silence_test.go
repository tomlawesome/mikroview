// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
)

func newTestDeviceSilence(t *testing.T, cfg Config, devices DeviceLister) (*DeviceSilenceDetector, *flags.Store) {
	t.Helper()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return NewDeviceSilenceDetectorWithSettings(cfg, fs, AllEnabledSettingsStore(), devices), fs
}

// fakeDeviceLister lets a test hand DeviceSilenceDetector an exact,
// hand-built device.Info list -- covering shapes (a device configured but
// never seen at all, in particular) that would otherwise require an
// awkward dance with the real device.Registry's Resolve/NewRegistry API.
type fakeDeviceLister []device.Info

func (f fakeDeviceLister) List() []device.Info { return []device.Info(f) }

func TestDeviceSilenceFlagsAConfiguredDeviceThatWentQuiet(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1", Configured: true,
			FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-20 * time.Minute), EventCount: 500},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)

	d.Check(now)

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag, got %+v", list)
	}
	f := list[0]
	if f.Type != flags.TypeDeviceSilence || f.Target != "core" {
		t.Errorf("unexpected flag: %+v", f)
	}
	if f.Cleared {
		t.Errorf("expected an active flag, got a cleared one: %+v", f)
	}
}

func TestDeviceSilenceDoesNotFireBeforeTheThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1", Configured: true,
			FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-5 * time.Minute), EventCount: 500},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)

	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag for a device well within the staleness threshold, got %+v", fs.List())
	}
}

func TestDeviceSilenceFiresExactlyAtTheThreshold(t *testing.T) {
	// The test plan in issue #98 explicitly calls for "fires within the
	// configured threshold and not before" -- this pins the boundary
	// itself (elapsed == threshold fires, one second under does not)
	// rather than only testing comfortably-inside/comfortably-outside
	// cases.
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	justUnder := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-15*time.Minute + time.Second), EventCount: 1},
	}
	d, fs := newTestDeviceSilence(t, cfg, justUnder)
	d.Check(now)
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag one second under the threshold, got %+v", fs.List())
	}

	atThreshold := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-15 * time.Minute), EventCount: 1},
	}
	d2, fs2 := newTestDeviceSilence(t, cfg, atThreshold)
	d2.Check(now)
	if len(fs2.List()) != 1 {
		t.Fatalf("expected a flag exactly at the threshold, got %+v", fs2.List())
	}
}

func TestDeviceSilenceIgnoresUnconfiguredDevices(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "10.0.0.5", Name: "10.0.0.5", SourceIP: "10.0.0.5", Configured: false,
			FirstSeen: now.Add(-time.Hour), LastSeen: now.Add(-time.Hour), EventCount: 3},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)

	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected an auto-discovered (unconfigured) device to never be flagged, got %+v", fs.List())
	}
}

func TestDeviceSilenceIgnoresADeviceNeverSeenAtAll(t *testing.T) {
	// A freshly configured device that hasn't sent anything yet has a
	// zero LastSeen -- that's "never contacted," a different condition
	// from "went quiet after being active" (see DeviceSilenceDetector's
	// doc comment), and must never instantly flag on startup.
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "new-router", Name: "New Router", SourceIP: "192.168.1.2", Configured: true},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)

	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected a never-contacted device not to be flagged, got %+v", fs.List())
	}
}

func TestDeviceSilenceDisabledNeverFires(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-time.Hour), EventCount: 500},
	}

	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	seed := DefaultSettingsMap()
	seed[DetectorDeviceSilence] = Settings{Enabled: false}
	settings, err := OpenSettingsStore("", seed)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeviceSilenceDetectorWithSettings(cfg, fs, settings, devices)

	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected a disabled detector to never fire, got %+v", fs.List())
	}
}

func TestDeviceSilenceZeroThresholdDisablesEntirely(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 0
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-24 * time.Hour), EventCount: 500},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)

	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected DeviceStaleAfter=0 to disable the detector entirely, got %+v", fs.List())
	}
}

func TestDeviceSilenceReFiresAndUpdatesConfidenceAsSilenceContinues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 15 * time.Minute
	now := time.Now()

	devices := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-16 * time.Minute), EventCount: 500},
	}
	d, fs := newTestDeviceSilence(t, cfg, devices)
	d.Check(now)

	first := fs.List()[0]
	if first.Count != 1 {
		t.Fatalf("expected Count=1 after the first Check, got %d", first.Count)
	}

	// Same device, much further past the threshold now -- a re-fire
	// should update Count/LastSeen/Detail in place, not create a second
	// flag (flags.Store dedups by (Type, Target), same as every other
	// detector).
	laterDevices := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-16 * time.Minute), EventCount: 500},
	}
	d2 := NewDeviceSilenceDetectorWithSettings(cfg, fs, AllEnabledSettingsStore(), laterDevices)
	d2.Check(now.Add(45 * time.Minute))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected the same flag to be updated in place, got %+v", list)
	}
	if list[0].Count != 2 {
		t.Errorf("expected Count=2 after a re-fire, got %d", list[0].Count)
	}
}

// TestDeviceSilenceIntegration exercises the scenario from issue #98's
// own verification plan end to end against the real device.Registry (not
// the fake lister above): two configured devices, one goes quiet, the
// flag fires for the quiet one and not the active one, within the
// configured threshold and not before.
func TestDeviceSilenceIntegration(t *testing.T) {
	registry := device.NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
		{ID: "wifi-ap", Name: "WiFi AP", SourceIP: "192.168.1.2"},
	})

	cfg := DefaultConfig()
	cfg.DeviceStaleAfter = 10 * time.Minute
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeviceSilenceDetectorWithSettings(cfg, fs, AllEnabledSettingsStore(), registry)

	start := time.Now()
	registry.Resolve("192.168.1.1", start)
	registry.Resolve("192.168.1.2", start)

	// Both devices active a few minutes in -- well under the threshold,
	// nothing should fire yet.
	d.Check(start.Add(5 * time.Minute))
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flags while both devices are within the threshold, got %+v", fs.List())
	}

	// "wifi-ap" keeps sending; "core" goes silent from here on.
	registry.Resolve("192.168.1.2", start.Add(5*time.Minute))

	// Past the threshold for "core" (silent since start), still within it
	// for "wifi-ap" (last heard from at +5m).
	d.Check(start.Add(11 * time.Minute))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag (for the silent device), got %+v", list)
	}
	if list[0].Target != "core" {
		t.Errorf("expected the flag to target the silent device %q, got %q", "core", list[0].Target)
	}
}
