// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is the write side of issue #680's nightly memory: the two
// doors an expectation's window history is kept up to date through.
//
// RecordWatchNight runs on the evaluation path, when a match arrives, and
// marks the night it landed in as kept. FillWatchNights runs on the read
// path and writes down every window that has closed since each entry last
// had a night recorded.
//
// Neither is a timer, deliberately. A timer firing at each window close
// misses every close the process was not running for -- a restart at
// 03:00 would silently lose the night before it, and the record would
// show a gap that looked like an empty night. Filling lazily instead
// means the history catches up the moment anything touches the store, and
// running it twice writes nothing the second time: a night is keyed by
// the instant its window opened, and written once.

// watchObservationFor is what this process can honestly say about its own
// ability to have seen a night for entry id -- see watchlist.Observation.
//
// Since is when this process opened its definitions store, which is the
// earliest window it could have watched end to end. A window that opened
// before that was either already recorded by the process before this one
// (the record is persisted, and a night is written once) or was not
// watched at all, and the second case must never be reported as "empty".
//
// silent is the entry's own sticky liveness mark (Entry.SilentOccurrences,
// issue #730), carried straight through to Observation.Silent -- see
// TickWatchLiveness for what writes it.
func (s *DefinitionsStore) watchObservationFor(covered bool, silent []time.Time) watchlist.Observation {
	return watchlist.Observation{Since: s.watchingSince, Covered: covered, Silent: silent}
}

// RecordWatchNight marks the night containing at as kept for expectation
// id, and counts the match -- called from MatchlogSinkWithNights on every
// match that reaches the log.
//
// Kept means presence: this is called for any match attributed to the
// entry, and the only question asked is whether its time falls inside the
// window. Similarity ("was it the usual size") would need a baseline this
// app does not have, and a night flipping to empty because traffic was
// lighter than usual would be a judgement the operator could not check.
//
// Silently no-ops for an unknown, non-expectation or windowless id, the
// same contract RecordObservation has and for the same reason: this runs
// on the evaluation goroutine, which has no reasonable action to take on
// an error, and an entry edited a moment after Match ran is a real,
// harmless race.
//
// The catch-up fill runs first, with coverage taken as true: a match on
// this entry has just arrived, which is direct evidence that a rule is
// logging its pathway. Without the fill, a match tonight would push the
// last-recorded night past a window that closed quietly yesterday, and
// that night could never be written down afterwards.
func (s *DefinitionsStore) RecordWatchNight(id string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateNightsLocked(id, func(e *watchlist.Entry) bool {
		e.Nights = watchlist.FillNights(e.Nights, e.Window, at, s.watchObservationFor(true, e.SilentOccurrences))
		var notable bool
		e.Nights, notable = watchlist.RecordMatch(e.Nights, e.Window, at)
		return notable
	})
}

// FillWatchNights writes down every window that has closed since each
// expectation's last recorded night, and returns how many entries
// changed.
//
// covered answers, per entry id, whether any firewall rule mikroview can
// see is logging that entry's pathway -- the caller's own coverage
// answer, since it comes from live router state rather than from this
// store. An entry missing from the map is not covered, and its nights are
// recorded "not observed" rather than "empty": a watch nothing logs
// cannot be judged on nightly presence at all, and calling those nights
// empty would report an absence of mikroview's own as a fact about the
// network.
func (s *DefinitionsStore) FillWatchNights(now time.Time, covered map[string]bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := 0
	for id := range s.raw {
		if s.updateNightsLocked(id, func(e *watchlist.Entry) bool {
			before := newestOpened(e.Nights)
			e.Nights = watchlist.FillNights(e.Nights, e.Window, now, s.watchObservationFor(covered[id], e.SilentOccurrences))
			return newestOpened(e.Nights).After(before)
		}) {
			changed++
		}
	}
	return changed
}

// TickWatchLiveness sticky-marks every expectation's currently open
// occurrence as device-silent, and returns how many entries changed.
//
// Called from the watch-liveness ticker (internal/engine/watch_liveness.go)
// only when its own sweep of the device registry found at least one
// device silent -- so every call here means "yes, mark", never "check and
// maybe mark": the device selection and the staleness definition itself
// both live on the caller, reused from device_silence rather than
// duplicated (issue #730's own instruction). What happens here is only
// watchlist.MarkSilent's bookkeeping: find the occurrence open at now for
// each entry's own window, and record it.
//
// Entries are not scoped to a device (Definition.Coverage's own doc
// comment makes the same call for firewall-rule coverage: "any device
// could feed the entry"), so this does not try to decide which device is
// "the" one behind a given entry's pathway where a boundary spans two --
// it applies the mark to every entry uniformly. That is deliberately the
// conservative direction: a device that might have carried this entry's
// traffic having gone silent is grounds enough to distrust an otherwise-
// empty night, not grounds to guess it was some other device's problem.
func (s *DefinitionsStore) TickWatchLiveness(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := 0
	for id := range s.raw {
		if s.updateNightsLocked(id, func(e *watchlist.Entry) bool {
			before := len(e.SilentOccurrences)
			e.SilentOccurrences = watchlist.MarkSilent(e.SilentOccurrences, e.Window, now)
			return len(e.SilentOccurrences) != before
		}) {
			changed++
		}
	}
	return changed
}

// newestOpened is the instant the most recent recorded night opened, or
// the zero time. The newest night is what says whether a fill added
// anything: the history is capped, so an append can leave the length
// exactly where it was.
func newestOpened(nights []watchlist.Night) time.Time {
	if len(nights) == 0 {
		return time.Time{}
	}
	return nights[len(nights)-1].Opened
}

// updateNightsLocked decodes the entry at id, hands it to mutate, and
// writes it back when mutate says something worth persisting changed.
// The ring is recomputed from the resulting nights either way, since it
// is a function of them. Must be called with s.mu held.
//
// Writes the definition straight back into s.raw rather than going
// through writeExpectationLocked: this must not touch CreatedAt, the
// enabled flag, the scope or anything else on the envelope, and it runs
// on the evaluation path where a full convert-and-revalidate round trip
// would be paid per match.
func (s *DefinitionsStore) updateNightsLocked(id string, mutate func(*watchlist.Entry) bool) bool {
	raw, ok := s.raw[id]
	if !ok {
		return false
	}
	sd := decodeStored(id, raw)
	if !sd.Available || sd.Definition.Intent != IntentExpectation {
		return false
	}
	e, err := EntryFromDefinition(sd.Definition)
	if err != nil || !e.Window.Defined() {
		return false
	}

	notable := mutate(&e)
	ring := watchlist.UpdateRing(e.Nights, e.Window)
	if !notable && ring == e.Ring {
		return false
	}
	e.Ring = ring

	def := sd.Definition
	if err := setNightParams(&def, e); err != nil {
		definitionsLog.Error(fmt.Sprintf("expectation %q: encoding its nightly history failed: %v -- the night is lost", id, err))
		return false
	}
	encoded, err := json.Marshal(def)
	if err != nil {
		definitionsLog.Error(fmt.Sprintf("expectation %q: encoding the definition failed: %v -- the night is lost", id, err))
		return false
	}
	s.raw[id] = encoded
	s.persistLocked()
	return true
}

// setNightParams writes the nights, the ring and the sticky liveness mark
// back into d's nightsJSON/ringJSON/silentJSON params -- the JSON-in-a-
// string shape both watchlist schemas declare (definitions_convert.go).
// The window itself is not rewritten here: nothing on this path edits it.
func setNightParams(d *Definition, e watchlist.Entry) error {
	nights, err := json.Marshal(e.Nights)
	if err != nil {
		return err
	}
	ring, err := json.Marshal(e.Ring)
	if err != nil {
		return err
	}
	silent, err := json.Marshal(e.SilentOccurrences)
	if err != nil {
		return err
	}
	// Sized from d.Params alone, not len(d.Params)+3: the three keys below
	// grow the map at most once, and the arithmetic trips CodeQL's
	// allocation-size-overflow rule for no real benefit.
	params := make(Params, len(d.Params))
	for k, v := range d.Params {
		params[k] = v
	}
	params["nightsJSON"] = []string{string(nights)}
	params["ringJSON"] = []string{string(ring)}
	params["silentJSON"] = []string{string(silent)}
	d.Params = params

	// Re-sync the schema alongside the params it declares. This runs on
	// whatever bytes decodeStored handed back for this definition, which
	// for a watchlist entry created before silentJSON existed (or, for
	// that matter, before #680's nightsJSON/ringJSON) still carries the
	// on-disk ParamSchema from whenever it was last written -- and
	// nothing else upgrades a stored definition's schema. Writing
	// silentJSON into Params
	// without this would mean the very next decode of this same
	// definition fails ValidateParams on a param its own persisted schema
	// does not declare, and this entry silently stops tracking nights at
	// all from that point on. d.Kind is what convertNonInvertedEntry/
	// convertInvertedEntry already key this exact choice on.
	if d.Kind == KindProgrammatic {
		d.ParamSchema = watchlistInvertedParamSchema
	} else {
		d.ParamSchema = watchlistNonInvertedParamSchema
	}
	return nil
}

// NightRecorder is the slice of the definitions store MatchlogSinkWithNights
// needs: somewhere to mark the night a match landed in. An interface, so
// the sink can be driven in a test without standing up a store, and so a
// deployment with no definitions store (there is none in practice, but the
// sink's nil contract is load-bearing elsewhere) is a safe no-op.
type NightRecorder interface {
	RecordWatchNight(entryID string, at time.Time)
}
