// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// TestAnyDeviceSilentUsesDeviceSilenceIsOwnDefinitionForConfiguredDevices
// pins the reuse issue #730 asks for: a configured device with contact
// history counts as silent only when device_silence's own elapsed/
// staleAfter comparison says so -- not before the threshold. The
// never-contacted case is its own test (below): it is the one place this
// file departs from device_silence's comparison, not an extension of it.
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
}

// TestAnyDeviceSilentTreatsAConfiguredNeverSeenDeviceAsSilent pins the
// one place this file departs from device_silence's own comparison:
// deviceElapsedStale reports a zero LastSeen as not-stale (device_silence
// is an alarm, and must not fire on a router that is configured but not
// yet deployed), but a configured device that has never sent a single
// line is the strongest case there is for "this could not have been
// observed" -- so, unlike device_silence, it counts as silent here.
func TestAnyDeviceSilentTreatsAConfiguredNeverSeenDeviceAsSilent(t *testing.T) {
	now := time.Now()
	if !anyDeviceSilent(fakeDeviceLister{
		{ID: "core", Configured: true},
	}, 15*time.Minute, now) {
		t.Error("a configured device never contacted at all was not reported silent -- #730's whole point is that this cannot honestly be called an empty night")
	}
}

// TestAnyDeviceSilentTreatsAnAutoDiscoveredSourceMoreReadily pins the
// second settled question: an auto-discovered (never configured) source
// has no expected cadence, so device_silence itself skips it before ever
// comparing elapsed time -- but a watch behind one needs the opposite
// bias, not observed more readily, not less. anyDeviceSilent drops that
// Configured guard, so once such a source has gone quiet past
// staleAfter it counts exactly as a configured one would.
//
// There is no zero-LastSeen case to test here: internal/device.Registry.
// Resolve sets LastSeen on the very call that creates an auto-discovered
// entry, so one can never actually have a zero LastSeen -- see
// anyDeviceSilent's own doc comment for why that case does not get a
// carve-out the way the configured one does.
func TestAnyDeviceSilentTreatsAnAutoDiscoveredSourceMoreReadily(t *testing.T) {
	now := time.Now()

	// Recently active is not silent just for being auto-discovered.
	if anyDeviceSilent(fakeDeviceLister{
		{ID: "10.0.0.9", Configured: false, LastSeen: now.Add(-time.Minute)},
	}, 15*time.Minute, now) {
		t.Error("a recently active auto-discovered source was reported silent")
	}
	// Past the threshold, silent regardless of Configured -- the
	// exclusion device_silence applies here does not apply to this file.
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

// TestWatchLivenessTickerConfiguredNeverSeenDeviceProducesNotObserved is
// the end-to-end proof for the defect this test's absence let through: a
// configured device that has never sent a single line (zero LastSeen) is
// the strongest case there is for "this could not have been observed",
// and the night must close as not observed, never empty -- not just that
// anyDeviceSilent answers true in isolation, but that the mark it writes
// actually reaches FillWatchNights and changes the recorded state.
func TestWatchLivenessTickerConfiguredNeverSeenDeviceProducesNotObserved(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	now := at(t, "2026-09-02T01:00:00Z") // mid-window
	ticker := &watchLivenessTicker{
		store:      s,
		devices:    fakeDeviceLister{{ID: "core", Configured: true}}, // never seen: zero LastSeen
		staleAfter: 15 * time.Minute,
		enabled:    true,
	}
	ticker.Tick(now)

	marked := mustGetEntry(t, s, "e1")
	if len(marked.SilentOccurrences) != 1 {
		t.Fatalf("got %d silent marks, want 1: %+v", len(marked.SilentOccurrences), marked.SilentOccurrences)
	}

	// The device is still never-seen (nothing changes that), and the
	// night is later filled as if nothing were wrong at fill time -- the
	// sticky mark is what has to carry the finding forward.
	s.FillWatchNights(at(t, "2026-09-08T12:00:00Z"), map[string]bool{"e1": true})
	got := mustGetEntry(t, s, "e1")
	var found bool
	for _, n := range got.Nights {
		if !n.Opened.Equal(at(t, "2026-09-01T21:00:00Z")) {
			continue
		}
		found = true
		if n.State != watchlist.NightUnobserved {
			t.Errorf("the night behind a configured-but-never-seen device is %q, want %q", n.State, watchlist.NightUnobserved)
		}
	}
	if !found {
		t.Fatal("the marked night was not filled")
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
