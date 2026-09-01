// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// Issue #730: a watch night was filed as "empty" purely on mikroview's own
// store-open time, which answers "was mikroview running" rather than "was
// the router still sending". This file adds the second, honest signal:
// whether the device behind an entry's watched pathway went quiet at any
// point while its window was open, sticky-marked on each tick rather than
// checked once at window close (watchlist.MarkSilent's own doc comment
// says why the check cannot wait until close: a router that recovers
// before the window shuts must still be remembered as having gone dark).
//
// # Three questions this file settles (issue #730 left them to the build)
//
// Which device an entry's pathway belongs to, where a boundary spans two:
// it does not try to decide. watchlist.Entry carries no device field --
// Definition.Coverage's own doc comment (coverage.go) already makes this
// exact call for firewall-rule coverage ("Entries are not scoped to a
// device... one router logging the right traffic is enough, even if five
// others do not"). Liveness mirrors that structurally rather than
// guessing narrower: if any device that could be carrying traffic has
// gone silent, every entry's currently open occurrence is marked, because
// there is no way to prove it was NOT the one behind a given entry's
// boundary. That is the conservative direction for a package whose whole
// point is refusing to claim more than it can prove.
//
// What an auto-discovered (never configured) source does: judged by the
// same elapsed/staleAfter comparison as everything else, with no
// exclusion for being unconfigured. device_silence guards with
// `if !info.Configured { continue }` before it ever compares elapsed
// time, because it has no expected cadence to raise a false alarm
// against (#98) -- correct for an alarm, wrong for this. anyDeviceSilent
// drops that guard, so an auto-discovered source that has gone quiet past
// staleAfter counts exactly as a configured one would -- more readily
// than device_silence, not less. (A separate zero-LastSeen carve-out for
// auto-discovered devices was considered and rejected: internal/device.
// Registry.Resolve sets LastSeen on the very call that creates an
// auto-discovered entry, so such a device can never actually have a zero
// LastSeen -- the case cannot arise, and code that "handled" it was dead.)
//
// What a CONFIGURED device with a zero LastSeen does: treated as silent
// here, unlike device_silence. deviceElapsedStale deliberately returns
// not-stale for a zero LastSeen -- device_silence is an alarm, and must
// not fire on a router that is configured but has not been deployed yet.
// This file makes a different claim, though: not "sound the alarm" but
// "can this window honestly be called empty", and a configured device
// that has never sent a single line is the strongest case there is for
// "we could not have observed this". So the exception lives in
// anyDeviceSilent, not in deviceElapsedStale: device_silence keeps its
// existing contract unchanged, and this file departs from it only here.
//
// What DeviceStaleAfter == 0 does: the same "off means off" contract
// device_silence itself declares, reused rather than reinvented -- see
// deviceSilenceDefinition's own doc comment. watchLivenessTicker goes
// inert whenever device_silence itself is disabled or its threshold is
// non-positive, and nights then fall back exactly to the pre-#730
// coverage-only Observation.
//
// # What this deliberately does not close
//
// LastSeen proves the device was sending *something*, not that the
// specific firewall rule behind a watched pathway was still logging it --
// a rule disabled or edited on the router looks identical to a genuinely
// quiet pathway from here. That narrower gap is not what this closes, and
// nothing in this file should be read as claiming it does.

// anyDeviceSilent reports whether any device in devices counts as silent
// for #730's purposes, reusing device_silence's own elapsed/staleAfter
// comparison (deviceElapsedStale, shipped_device_silence.go) for the
// cadence half of the answer -- applied with no Configured guard, unlike
// device_silence's own Tick, so an auto-discovered source is judged
// exactly as a configured one is rather than being skipped.
//
// One exception on top of that comparison: a CONFIGURED device with a
// zero LastSeen. deviceElapsedStale reports that as not-stale (by
// design -- see its own doc comment), because device_silence is an alarm
// that must not fire on a router configured but not yet deployed. This
// file is not an alarm, it is an observability claim, and a configured
// device that has never sent anything at all is the strongest case there
// is for "this could not have been watched" -- so it counts as silent
// here even though device_silence itself would never fire on it. An
// auto-discovered device gets no equivalent zero-LastSeen check: per
// internal/device.Registry.Resolve, one is never created with a zero
// LastSeen in the first place, so the case does not arise for it.
func anyDeviceSilent(devices DeviceLister, staleAfter time.Duration, now time.Time) bool {
	if devices == nil {
		return false
	}
	for _, info := range devices.ListDevices() {
		if _, stale := deviceElapsedStale(info.LastSeen, staleAfter, now); stale {
			return true
		}
		if info.Configured && info.LastSeen.IsZero() {
			return true
		}
	}
	return false
}

// WatchLivenessTickerID is the fixed id watchLivenessTicker registers
// under. Not a stored definition -- see this type's own doc comment --
// so, unlike every other id in this package, it names nothing an operator
// can look up in the definitions API.
const WatchLivenessTickerID = "watchlist-liveness"

// watchLivenessTicker is registered directly onto the engine by
// Registry.Sync, the same way InvertedExpectations is: it has no
// operator-facing envelope of its own, because it raises nothing and
// enables/disables nothing an operator would toggle independently of
// device_silence (see this file's own doc comment on the zero-threshold
// question). Rebuilt fresh on every Sync call, which is safe because it
// holds no state of its own worth preserving -- the sticky mark it writes
// lives in the definitions store, on the entry, exactly where Nights and
// Ring already do.
type watchLivenessTicker struct {
	store      *DefinitionsStore
	devices    DeviceLister
	staleAfter time.Duration
	// enabled mirrors device_silence's own Definition.Enabled -- see this
	// file's doc comment on why DeviceStaleAfter == 0 (or device_silence
	// disabled outright) must switch this off too, not just the alarm.
	enabled bool
}

// ID satisfies Evaluated.
func (t *watchLivenessTicker) ID() string { return WatchLivenessTickerID }

// Kind satisfies Evaluated -- the sticky-mark bookkeeping is Go, so
// KindProgrammatic's string form, the same as InvertedExpectations.
func (t *watchLivenessTicker) Kind() string { return string(KindProgrammatic) }

// Evaluate satisfies Evaluated and does nothing: like device_silence,
// this is an absence-of-events condition, so there is no event for a
// per-event pass to contribute to it.
func (t *watchLivenessTicker) Evaluate(store.Event) {}

// TickInterval satisfies Ticked. Reused from device_silence rather than
// declared afresh: this shares the exact same "how promptly is an
// already-true condition noticed" reasoning that definition's own doc
// comment gives for its own cadence, over the exact same device registry.
func (t *watchLivenessTicker) TickInterval() time.Duration { return deviceSilenceCheckInterval }

// Tick satisfies Ticked: one sweep of the device registry, and if any
// device counts as silent, a sticky mark on every expectation's currently
// open occurrence.
func (t *watchLivenessTicker) Tick(now time.Time) {
	if !t.enabled || t.store == nil || t.staleAfter <= 0 {
		return
	}
	if !anyDeviceSilent(t.devices, t.staleAfter, now) {
		return
	}
	t.store.TickWatchLiveness(now)
}

// deviceStaleAfterDefinitionID is the shipped id device_silence registers
// under (shipped_device_silence.go's own registerShippedProgrammatic
// call) -- named here rather than repeating the bare string, since
// Registry.Sync reads this specific built definition back out to learn
// the threshold watchLivenessTicker reuses.
const deviceStaleAfterDefinitionID = "device_silence"

// deviceStaleAfterer is the slice of *deviceSilenceDefinition
// Registry.Sync needs to reuse its live threshold -- see
// deviceSilenceDefinition.DeviceStaleAfter's own doc comment for why this
// is read back from the built object rather than a second copy of
// config.
type deviceStaleAfterer interface {
	DeviceStaleAfter() time.Duration
}

// deviceStaleAfterFrom reads the currently configured device_silence
// threshold out of built, or zero if device_silence is not present or was
// not built as expected -- which watchLivenessTicker.Tick already treats
// as "off", the same as an explicit zero (see this file's own doc comment
// on that question).
func deviceStaleAfterFrom(built map[string]Evaluated) time.Duration {
	ds, ok := built[deviceStaleAfterDefinitionID].(deviceStaleAfterer)
	if !ok {
		return 0
	}
	return ds.DeviceStaleAfter()
}

// deviceSilenceEnabledFrom reports whether the built device_silence
// definition is itself enabled -- watchLivenessTicker mirrors that flag
// rather than running independently of it, so disabling device_silence
// switches this feature off too rather than leaving a second, harder to
// find control.
func deviceSilenceEnabledFrom(built map[string]Evaluated) bool {
	d, ok := built[deviceStaleAfterDefinitionID].(interface{ Definition() Definition })
	if !ok {
		return false
	}
	return d.Definition().Enabled
}
