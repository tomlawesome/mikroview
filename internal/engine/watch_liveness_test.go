// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestAnyDeviceSilentUsesDeviceSilenceIsOwnDefinitionForConfiguredDevices
// pins the reuse issue #730 asks for: a configured device counts as
// silent only when device_silence's own elapsed/staleAfter comparison
// says so -- not before the threshold, and not for a never-contacted
// device (the same "never contacted is not silent" exclusion
// device_silence itself carries).
func TestAnyDeviceSilentUsesDeviceSilenceIsOwnDefinitionForConfiguredDevices(t *testing.T) {
	now := time.Now()

	if anyDeviceSilent(fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-5 * time.Minute)},
	}, 15*time.Minute, now) {
		t.Error("a configured device well within the threshold was reported silent")
	}
	if !anyDeviceSilent(fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now.Add(-15 * time.Minute)},
	}, 15*time.Minute, now) {
		t.Error("a configured device exactly at the threshold was not reported silent")
	}
	if anyDeviceSilent(fakeDeviceLister{
		{ID: "core", Configured: true},
	}, 15*time.Minute, now) {
		t.Error("a configured device never contacted at all was reported silent -- device_silence's own exclusion")
	}
}

// TestAnyDeviceSilentTreatsAnAutoDiscoveredSourceMoreReadily pins the
// second settled question: an auto-discovered (never configured) source
// has no expected cadence, so device_silence itself skips it -- but a
// watch behind one needs the opposite bias, not observed more readily,
// not less. A never-contacted auto-discovered source counts as silent
// outright, where a configured one (above) does not.
func TestAnyDeviceSilentTreatsAnAutoDiscoveredSourceMoreReadily(t *testing.T) {
	now := time.Now()

	if !anyDeviceSilent(fakeDeviceLister{
		{ID: "10.0.0.9", Configured: false},
	}, 15*time.Minute, now) {
		t.Error("a never-contacted auto-discovered source was not treated as silent -- issue #730 asks for the stricter bias here")
	}
	// One that HAS been contacted, and recently, is not silent just for
	// being auto-discovered -- the stricter rule is about absence of
	// contact, not about being unconfigured per se.
	if anyDeviceSilent(fakeDeviceLister{
		{ID: "10.0.0.9", Configured: false, LastSeen: now.Add(-time.Minute)},
	}, 15*time.Minute, now) {
		t.Error("a recently active auto-discovered source was reported silent")
	}
	// And the ordinary elapsed comparison still applies once it HAS been
	// contacted: past the threshold, silent regardless of Configured.
	if !anyDeviceSilent(fakeDeviceLister{
		{ID: "10.0.0.9", Configured: false, LastSeen: now.Add(-time.Hour)},
	}, 15*time.Minute, now) {
		t.Error("a stale auto-discovered source was not reported silent")
	}
}

// TestAnyDeviceSilentFleetWide pins the first settled question: an entry
// carries no device field (Definition.Coverage's own doc comment makes
// the identical call for firewall-rule coverage), so this does not try to
// resolve which device backs a given entry's pathway -- any device in the
// fleet going silent is enough, mirroring Coverage's "any one covering
// device is enough" in the opposite, more conservative direction.
func TestAnyDeviceSilentFleetWide(t *testing.T) {
	now := time.Now()
	devices := fakeDeviceLister{
		{ID: "core", Configured: true, LastSeen: now},                    // healthy
		{ID: "wifi-ap", Configured: true, LastSeen: now.Add(-time.Hour)}, // silent
	}
	if !anyDeviceSilent(devices, 15*time.Minute, now) {
		t.Error("one silent device among several healthy ones was not reported")
	}
}

// TestAnyDeviceSilentNilListerIsInert mirrors device_silence's own
// nil-tolerant contract.
func TestAnyDeviceSilentNilListerIsInert(t *testing.T) {
	if anyDeviceSilent(nil, 15*time.Minute, time.Now()) {
		t.Error("a nil device lister was reported silent")
	}
}

// TestWatchLivenessTickerMarksOnASilentSweep is the ticker's own
// end-to-end behaviour: a device silent at Tick time results in the
// currently open occurrence being marked, via DefinitionsStore.
func TestWatchLivenessTickerMarksOnASilentSweep(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	now := at(t, "2026-09-02T01:00:00Z") // mid-window
	ticker := &watchLivenessTicker{
		store:      s,
		devices:    fakeDeviceLister{{ID: "core", Configured: true, LastSeen: now.Add(-time.Hour)}},
		staleAfter: 15 * time.Minute,
		enabled:    true,
	}
	ticker.Tick(now)

	got := mustGetEntry(t, s, "e1")
	if len(got.SilentOccurrences) != 1 {
		t.Fatalf("got %d silent marks, want 1: %+v", len(got.SilentOccurrences), got.SilentOccurrences)
	}
}

// TestWatchLivenessTickerDoesNothingWhenNoDeviceIsSilent is the quiet
// path: a healthy fleet leaves every entry untouched.
func TestWatchLivenessTickerDoesNothingWhenNoDeviceIsSilent(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	now := at(t, "2026-09-02T01:00:00Z")
	ticker := &watchLivenessTicker{
		store:      s,
		devices:    fakeDeviceLister{{ID: "core", Configured: true, LastSeen: now}},
		staleAfter: 15 * time.Minute,
		enabled:    true,
	}
	ticker.Tick(now)

	got := mustGetEntry(t, s, "e1")
	if len(got.SilentOccurrences) != 0 {
		t.Errorf("a healthy fleet still marked %+v", got.SilentOccurrences)
	}
}

// TestWatchLivenessTickerZeroThresholdIsOff and
// TestWatchLivenessTickerDisabledIsOff pin the third settled question:
// DeviceStaleAfter == 0, or device_silence itself disabled, switches this
// off entirely -- the same "off means off" contract device_silence
// declares for itself, reused rather than reinvented. Nights then fall
// back exactly to the pre-#730 coverage-only Observation.
func TestWatchLivenessTickerZeroThresholdIsOff(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	now := at(t, "2026-09-02T01:00:00Z")
	ticker := &watchLivenessTicker{
		store:      s,
		devices:    fakeDeviceLister{{ID: "core", Configured: true, LastSeen: now.Add(-24 * time.Hour)}},
		staleAfter: 0,
		enabled:    true,
	}
	ticker.Tick(now)

	if got := mustGetEntry(t, s, "e1"); len(got.SilentOccurrences) != 0 {
		t.Errorf("staleAfter=0 should disable the ticker entirely, got %+v", got.SilentOccurrences)
	}
}

func TestWatchLivenessTickerDisabledIsOff(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	now := at(t, "2026-09-02T01:00:00Z")
	ticker := &watchLivenessTicker{
		store:      s,
		devices:    fakeDeviceLister{{ID: "core", Configured: true, LastSeen: now.Add(-24 * time.Hour)}},
		staleAfter: 15 * time.Minute,
		enabled:    false, // device_silence itself is disabled
	}
	ticker.Tick(now)

	if got := mustGetEntry(t, s, "e1"); len(got.SilentOccurrences) != 0 {
		t.Errorf("a disabled device_silence should disable the ticker entirely, got %+v", got.SilentOccurrences)
	}
}

// TestDeviceStaleAfterFromReusesTheLiveDeviceSilenceThreshold and
// TestDeviceSilenceEnabledFromMirrorsTheBuiltDefinition pin
// Registry.Sync's own wiring: the ticker reuses whatever the operator has
// currently configured on device_silence, read back from the built
// object rather than a second copy.
func TestDeviceStaleAfterFromReusesTheLiveDeviceSilenceThreshold(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedDeviceSilenceDefinition(t, fs, 20*time.Minute, fakeDeviceLister{}, true)
	built := map[string]Evaluated{"device_silence": d}
	if got := deviceStaleAfterFrom(built); got != 20*time.Minute {
		t.Errorf("deviceStaleAfterFrom = %s, want 20m0s", got)
	}
	if got := deviceStaleAfterFrom(map[string]Evaluated{}); got != 0 {
		t.Errorf("deviceStaleAfterFrom with no device_silence built = %s, want 0", got)
	}
}

func TestDeviceSilenceEnabledFromMirrorsTheBuiltDefinition(t *testing.T) {
	fs := newTestFlagsStore(t)
	enabled := newShippedDeviceSilenceDefinition(t, fs, 20*time.Minute, fakeDeviceLister{}, true)
	disabled := newShippedDeviceSilenceDefinition(t, fs, 20*time.Minute, fakeDeviceLister{}, false)

	if !deviceSilenceEnabledFrom(map[string]Evaluated{"device_silence": enabled}) {
		t.Error("an enabled device_silence read back as disabled")
	}
	if deviceSilenceEnabledFrom(map[string]Evaluated{"device_silence": disabled}) {
		t.Error("a disabled device_silence read back as enabled")
	}
	if deviceSilenceEnabledFrom(map[string]Evaluated{}) {
		t.Error("no built device_silence at all read back as enabled")
	}
}
