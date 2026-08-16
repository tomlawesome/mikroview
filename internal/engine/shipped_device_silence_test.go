// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
)

// fakeDeviceLister is internal/detect/device_silence_test.go's helper of
// the same name, in this package's DeviceInfo terms: an exact,
// hand-built device list, covering shapes (a device configured but never
// seen at all, in particular) that would need an awkward dance with the
// real device.Registry API.
type fakeDeviceLister []DeviceInfo

func (f fakeDeviceLister) ListDevices() []DeviceInfo { return []DeviceInfo(f) }

// registryDeviceLister adapts a real *device.Registry -- main.go's
// deviceLister, kept test-locally so the integration test below can run
// against the genuine registry rather than a fake.
type registryDeviceLister struct{ reg *device.Registry }

func (a registryDeviceLister) ListDevices() []DeviceInfo {
	list := a.reg.List()
	out := make([]DeviceInfo, 0, len(list))
	for _, d := range list {
		out = append(out, DeviceInfo{ID: d.ID, Name: d.Name, LastSeen: d.LastSeen, Configured: d.Configured})
	}
	return out
}

func newShippedDeviceSilenceDefinition(t *testing.T, fs *flags.Store, staleAfter time.Duration, devices DeviceLister, enabled bool) *deviceSilenceDefinition {
	t.Helper()
	def := Definition{
		ID:          "device_silence",
		Name:        "Device silence",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     enabled,
		Params:      Params{"staleAfter": staleAfter.String()},
		ParamSchema: DeviceSilenceParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{Devices: devices})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(device_silence): %v", err)
	}
	d := built.(*deviceSilenceDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func TestShippedDeviceSilenceFlagsAConfiguredDeviceThatWentQuiet(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Name: "Core Router", Configured: true, LastSeen: now.Add(-20 * time.Minute)},
	}, true)

	d.Tick(now)

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag, got %+v", list)
	}
	if list[0].Type != flags.TypeDeviceSilence || list[0].Target != "core" {
		t.Errorf("unexpected flag: %+v", list[0])
	}
	if list[0].Cleared {
		t.Errorf("expected an active flag, got a cleared one: %+v", list[0])
	}
}

func TestShippedDeviceSilenceDoesNotFireBeforeTheThreshold(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Name: "Core Router", Configured: true, LastSeen: now.Add(-5 * time.Minute)},
	}, true)

	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no flag for a device well within the staleness threshold, got %+v", got)
	}
}

// TestShippedDeviceSilenceFiresExactlyAtTheThreshold is
// internal/detect/device_silence_test.go's test of the same name: #98's
// own plan calls for "fires within the configured threshold and not
// before", so the boundary itself is pinned -- elapsed == threshold
// fires, one second under does not.
func TestShippedDeviceSilenceFiresExactlyAtTheThreshold(t *testing.T) {
	now := time.Now()

	fs := newTestFlagsStore(t)
	justUnder := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-15*time.Minute + time.Second)},
	}, true)
	justUnder.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no flag one second under the threshold, got %+v", got)
	}

	fs2 := newTestFlagsStore(t)
	atThreshold := newShippedDeviceSilenceDefinition(t, fs2, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-15 * time.Minute)},
	}, true)
	atThreshold.Tick(now)
	if got := fs2.List(); len(got) != 1 {
		t.Fatalf("expected a flag exactly at the threshold, got %+v", got)
	}
}

func TestShippedDeviceSilenceIgnoresUnconfiguredDevices(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "10.0.0.5", Name: "10.0.0.5", Configured: false, LastSeen: now.Add(-time.Hour)},
	}, true)

	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected an auto-discovered (unconfigured) device to never be flagged, got %+v", got)
	}
}

// TestShippedDeviceSilenceIgnoresADeviceNeverSeenAtAll pins the
// "never contacted" exclusion: a freshly configured device with a zero
// LastSeen must not instantly flag on startup.
func TestShippedDeviceSilenceIgnoresADeviceNeverSeenAtAll(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "new-router", Name: "New Router", Configured: true},
	}, true)

	d.Tick(time.Now())
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a never-contacted device not to be flagged, got %+v", got)
	}
}

func TestShippedDeviceSilenceDisabledNeverFires(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-time.Hour)},
	}, false)

	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a disabled definition to never fire, got %+v", got)
	}
}

func TestShippedDeviceSilenceZeroThresholdDisablesEntirely(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 0, fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-24 * time.Hour)},
	}, true)

	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected staleAfter=0 to disable the definition entirely, got %+v", got)
	}
}

func TestShippedDeviceSilenceNilListerIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, nil, true)

	d.Tick(time.Now()) // must not panic
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no device registry configured to be inert, got %+v", got)
	}
}

// TestShippedDeviceSilence_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationDeviceSilence_FieldsRefireClearRevive, moved.
// Every pinned value is unchanged: the boundary at the real 15-minute
// default, Target, the byte-for-byte Detail, Confidence 0 at the
// boundary and 100 at 45 minutes, the zero Evidence, and the
// re-fire/clear/revive sequence.
func TestShippedDeviceSilence_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	devices := fakeDeviceLister{
		{ID: "core", Name: "Core Router", Configured: true, LastSeen: now.Add(-15 * time.Minute)},
	}
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, devices, true)
	d.Tick(now)

	f := dsFlag(fs, flags.TypeDeviceSilence)
	if f == nil {
		t.Fatal("expected a flag exactly at the 15-minute threshold")
	}
	if f.Target != "core" {
		t.Errorf("Target = %q, want %q", f.Target, "core")
	}
	wantDetail := "Core Router has sent no syslog for 15m0s, exceeding the 15m0s staleness threshold"
	if f.Detail != wantDetail {
		t.Errorf("Detail = %q, want %q", f.Detail, wantDetail)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 || f.Evidence.NAT != nil {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}

	// Re-fire: further past the threshold. elapsed=45m ->
	// overshootConfidence(2700, 900) == 100.
	d.Tick(now.Add(30 * time.Minute))
	f2 := dsFlag(fs, flags.TypeDeviceSilence)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 100 {
		t.Errorf("Confidence after re-fire = %v, want 100 (overshootConfidence(2700,900))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, now.Add(31*time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	d.Tick(now.Add(32 * time.Minute))
	f3 := dsFlag(fs, flags.TypeDeviceSilence)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// TestShippedDeviceSilenceIntegration is
// internal/detect/device_silence_test.go's TestDeviceSilenceIntegration:
// #98's own end-to-end verification plan against the real
// device.Registry rather than a fake -- two configured devices, one goes
// quiet, the flag fires for the quiet one and not the active one, within
// the configured threshold and not before.
func TestShippedDeviceSilenceIntegration(t *testing.T) {
	registry := device.NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
		{ID: "wifi-ap", Name: "WiFi AP", SourceIP: "192.168.1.2"},
	})
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 10*time.Minute, registryDeviceLister{reg: registry}, true)

	start := time.Now()
	registry.Resolve("192.168.1.1", start)
	registry.Resolve("192.168.1.2", start)

	d.Tick(start.Add(5 * time.Minute))
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected no flags while both devices are within the threshold, got %+v", got)
	}

	// "wifi-ap" keeps sending; "core" goes silent from here on.
	registry.Resolve("192.168.1.2", start.Add(5*time.Minute))
	d.Tick(start.Add(11 * time.Minute))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag (for the silent device), got %+v", list)
	}
	if list[0].Target != "core" {
		t.Errorf("expected the flag to target the silent device %q, got %q", "core", list[0].Target)
	}
}

// TestShippedDeviceSilenceTickInterval pins the cadence as the
// definition's own property rather than the driver's -- see Ticked.
func TestShippedDeviceSilenceTickInterval(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{}, true)
	if got := d.TickInterval(); got != time.Minute {
		t.Errorf("TickInterval = %s, want 1m0s (main.go's deviceSilenceCheckInterval, moved onto the definition)", got)
	}
}

// TestShippedDeviceSilenceIsNonReplayable pins the declaration: absence
// of events is not a predicate any per-event corpus walk can evaluate.
func TestShippedDeviceSilenceIsNonReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{}, true)

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify device_silence")
	}
	if receiptCapable {
		t.Fatal("expected device_silence to declare itself non-replayable")
	}
	if reason == "" {
		t.Error("a non-replayable declaration with no reason is the thing the contract exists to prevent")
	}
}

// TestShippedDeviceSilenceEvaluateIsInert pins that the per-event half
// genuinely does nothing -- the condition is the absence of events, so
// there is nothing an Evaluate call could contribute.
func TestShippedDeviceSilenceEvaluateIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	now := time.Now()
	d := newShippedDeviceSilenceDefinition(t, fs, 15*time.Minute, fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-time.Hour)},
	}, true)

	d.Evaluate(psEvt("203.0.113.9", 22, now))
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected Evaluate to be inert for a ticked definition, got %+v", got)
	}
}
