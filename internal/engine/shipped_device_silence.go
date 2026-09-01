// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("device_silence", buildDeviceSilenceDefinition)
}

// deviceSilenceCheckInterval is how often this definition sweeps the
// device registry -- lifted from main.go's own
// deviceSilenceCheckInterval, which is where it lived while
// internal/detect owned this detector.
//
// Deliberately coarser than global_spike's ten seconds, and for a
// different reason than that one's: global_spike's baseline advances one
// sample per tick, so its interval is part of what its statistic means,
// while this one's only decides how promptly an already-true condition
// is noticed. A device that has been silent for fifteen minutes is no
// less silent for being reported thirty seconds later. See Ticked's own
// doc comment for why the chassis makes each definition declare its own
// cadence rather than sharing one.
const deviceSilenceCheckInterval = 1 * time.Minute

// deviceSilenceDefinition is device_silence ported onto the chassis
// (issue #405, originally #98): a configured device that should be
// sending logs and has gone quiet.
//
// # Ticked, not per-event, and that is the whole point
//
// "Nothing arrived" is not a property any event carries -- there is no
// event for a condition to match against, which is
// docs/decisions/evaluation-engine.md section 2's own reason the
// programmatic kind is permanent rather than a stepping stone. internal/
// detect made the same call, driving this from a main.go ticker rather
// than from Observe. What changes with the port is only who owns the
// cadence: the ticker was main.go's, the interval is the definition's.
//
// # Two exclusions preserved exactly
//
// Only Configured devices are eligible: an auto-discovered source (seen
// on the wire, never added to config.yaml) has no expected cadence to
// fall silent from. And a device that has never sent a single event
// (LastSeen still zero) is skipped -- that is "never contacted", a
// distinct condition from "went quiet after being active", and firing on
// it would mean every freshly configured device alarms at startup before
// it has had a chance to send anything. The fleet-health API surfaces
// never-seen separately, just not as a flag.
//
// # Zero threshold means off
//
// staleAfter <= 0 disables the definition rather than meaning "instantly
// stale" -- the same "off means off" contract an unconfigured threshold
// has everywhere else in this codebase, and unchanged from
// internal/detect.
//
// # Scope is ignored, structurally
//
// internal/detect documented every Scope field as ignored for this
// detector (only Settings.Enabled applied), because a per-configured-
// device sweep is not keyed by anything a Scope restricts: there is no
// source address, no destination port and no rule label in play -- the
// subject is a device ID. That is preserved by simply never consulting
// Scope, rather than by consulting it and relying on it being empty.
type deviceSilenceDefinition struct {
	programmaticBase

	staleAfter time.Duration
	devices    DeviceLister
}

func buildDeviceSilenceDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	staleAfter, err := paramDuration(params, "staleAfter")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	return &deviceSilenceDefinition{
		programmaticBase: programmaticBase{def: def},
		staleAfter:       staleAfter,
		devices:          deps.Devices,
	}, nil
}

// Evaluate satisfies Evaluated and does nothing -- see this type's own
// doc comment. The chassis requires Evaluate on every definition rather
// than making it optional, so "this definition sees every event" is never
// something a reader has to go and check.
func (d *deviceSilenceDefinition) Evaluate(store.Event) {}

// TickInterval satisfies Ticked.
func (d *deviceSilenceDefinition) TickInterval() time.Duration { return deviceSilenceCheckInterval }

// Tick satisfies Ticked: one sweep of the configured device set.
//
// Like every other definition it never clears a flag once a device starts
// sending again -- that is a human's acknowledgement to make through the
// flags UI, the same lifecycle every flag type in this codebase has.
func (d *deviceSilenceDefinition) Tick(now time.Time) {
	if !d.def.Enabled || d.devices == nil || d.staleAfter <= 0 {
		return
	}
	for _, info := range d.devices.ListDevices() {
		if !info.Configured {
			continue
		}
		elapsed, stale := deviceElapsedStale(info.LastSeen, d.staleAfter, now)
		if !stale {
			continue
		}
		confidence := overshootConfidence(int(elapsed.Seconds()), int(d.staleAfter.Seconds()))
		d.emit(Emission{
			Target: info.ID,
			Detail: fmt.Sprintf("%s has sent no syslog for %s, exceeding the %s staleness threshold",
				info.Name, elapsed.Round(time.Second), d.staleAfter),
			Confidence: &confidence,
			// No SourceIP, no Country, no Evidence: the subject is a
			// device, not an address. internal/detect used
			// AddWithConfidence, which supplies none of the three.
			EventTime: now,
		})
	}
}

// deviceElapsedStale reports whether lastSeen is at least staleAfter
// behind now, and how long it has been -- device_silence's own
// elapsed-since-last-contact comparison, factored out so issue #730's
// watch-liveness ticker can reuse the identical definition rather than
// restate it (that issue's own instruction). A zero lastSeen ("never
// contacted") is never stale by this comparison -- device_silence's own
// doc comment on why: it is a distinct condition from "went quiet after
// being active", and firing on it would alarm every freshly configured
// device at startup. The watch-liveness ticker applies its own, stricter
// rule on top for a source this comparison alone would let through -- see
// that file's own doc comment.
func deviceElapsedStale(lastSeen time.Time, staleAfter time.Duration, now time.Time) (elapsed time.Duration, stale bool) {
	if lastSeen.IsZero() {
		return 0, false
	}
	elapsed = now.Sub(lastSeen)
	return elapsed, elapsed >= staleAfter
}

// DeviceStaleAfter reports the staleness threshold this live device_silence
// instance is currently configured with -- the same value Tick compares
// elapsed time against. Exported so issue #730's watch-liveness ticker can
// reuse the operator's own configured threshold exactly, rather than
// reading a second, potentially drifted copy of it (Registry.Sync looks
// this up on the built "device_silence" definition on every sync, since
// the operator can edit it at any time).
func (d *deviceSilenceDefinition) DeviceStaleAfter() time.Duration { return d.staleAfter }

// NonReplayableReason satisfies NonReplayable.
//
// #403 names this case by shape rather than by name: an
// absence-of-events definition. Its firing condition is "no event
// arrived from this device for fifteen minutes", which no per-event
// corpus walk can evaluate, because the thing that would have to be
// observed is the absence of the very events a corpus is made of. Worse,
// the quantity it actually compares is wall-clock now against a device
// registry's live LastSeen -- neither of which is in a corpus at all.
//
// A replay could be made to *look* like it works: walk the corpus,
// notice gaps between consecutive events per source, count the gaps
// longer than staleAfter. That would be a different detector. It would
// miss the device that fell silent at the end of the corpus and never
// came back -- the only case this definition exists for -- and it would
// invent detections for every device that was legitimately idle
// overnight and resumed. Reporting that number as this definition's
// receipt is precisely the confident-wrong answer #403's contract rules
// out.
//
// This is permanent against any event corpus, not a property of this
// particular one being short, so it is declared once rather than
// declined per call.
func (d *deviceSilenceDefinition) NonReplayableReason() string {
	return fmt.Sprintf(
		"device_silence fires on the absence of events -- a configured device whose registry LastSeen is more than %s behind wall-clock now -- and neither the absence nor the registry is something a per-event corpus contains, so no replay over one could report what this definition would have said",
		d.staleAfter)
}
