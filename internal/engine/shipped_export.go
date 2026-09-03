// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// This file is where the shipped programmatic definitions implement
// Snapshotted (#795): the windowed state each one carries in a Keyed[V],
// rendered whole for the periodic snapshot and restored on boot.
//
// It is written as one file rather than a method on the end of each
// definition's own file because the interesting content is the same
// decision five times over -- which of a definition's in-memory
// structures are worth carrying, and what the elapsed time makes
// meaningless -- and that reads better in one place than as five
// paragraphs that have to cross-reference each other.
//
// # What is carried, and what is not
//
// Carried: the rings (per-minute counts, distinct ports/hosts/pairs) and
// the per-source day bookkeeping that is not a Baseline. Not carried:
// baselines, which already survive through StateStore (state.go) and
// would be two sources of truth if they were here as well; and anything
// derived from an event's contents beyond a count or an identifier,
// which #795 rules out for the snapshot as a whole.
//
// # The day-state expiry rule, and why today-or-yesterday
//
// Two definitions keep per-calendar-day bookkeeping that a later day's
// first event closes out and folds into a baseline: activity_spike's
// hourDay/hourPeak (rollHourBucket) and off_hours' day/count (its
// Evaluate). Both fold *the day that just ended* -- the accumulating day
// is always either today's, still open, or yesterday's, about to be
// closed by the next event that arrives.
//
// So a restored day older than that has no honest destination. Folding
// it would enter a reading from days ago into the baseline as though it
// were the day that just closed, which is a wrong number presented with
// full confidence; keeping it unfolded would leave a bucket that never
// closes. It is dropped instead, which puts the bucket back in exactly
// the state it has before its hour is ever seen -- rollHourBucket and
// off_hours' Evaluate both already handle that ("first time this hour
// has ever been seen"), so nothing downstream needs to learn a new case.
// One day's reading is lost; nothing false is asserted.
//
// The same rule retires activity_spike's maturity streak: it counts
// consecutive calendar days that ended with no candidate spike on this
// source, and emitBucketFiring puts that number in front of an operator
// as "N day(s) of consistent history at this hour". Days when this
// process was not running are days nothing was observed, so they can
// neither extend the streak nor be silently skipped over inside it --
// see #420 design item 5, "may only claim what is true."

// dayIsRecent reports whether a recorded calendar day ("2006-01-02", the
// format both activityDay and off_hours' Evaluate write) is today's or
// yesterday's relative to now -- see this file's doc comment for why
// those are the only two a restored fold can honestly belong to.
func dayIsRecent(day string, now time.Time) bool {
	if day == "" {
		return false
	}
	return day == activityDay(now) || day == activityDay(now.AddDate(0, 0, -1))
}

// countRingKeyedState exports a Keyed[*CountRing] -- the shape
// activity_spike's counts and rule_spike's hits both have.
func countRingKeyedState(k *Keyed[*CountRing]) (json.RawMessage, error) {
	return k.Export(func(r *CountRing) (json.RawMessage, error) { return r.ExportState() })
}

// restoreCountRingKeyed is countRingKeyedState's inverse, rebuilding each
// ring at the definition's *current* window rather than whatever it was
// when the snapshot was written -- see window_export.go on re-bucketing.
func restoreCountRingKeyed(k *Keyed[*CountRing], raw json.RawMessage, window time.Duration, now time.Time) error {
	return k.Import(raw, func(raw json.RawMessage) (*CountRing, error) {
		r := NewCountRing(window)
		if err := r.ImportState(raw, now); err != nil {
			return nil, err
		}
		return r, nil
	})
}

// distinctRingKeyedState and restoreDistinctRingKeyed are the same pair
// for a Keyed[*DistinctRing[T]] -- dest_spread's destinations and pairs.
func distinctRingKeyedState[T comparable](k *Keyed[*DistinctRing[T]]) (json.RawMessage, error) {
	return k.Export(func(r *DistinctRing[T]) (json.RawMessage, error) { return r.ExportState() })
}

func restoreDistinctRingKeyed[T comparable](k *Keyed[*DistinctRing[T]], raw json.RawMessage, window time.Duration, now time.Time) error {
	return k.Import(raw, func(raw json.RawMessage) (*DistinctRing[T], error) {
		r := NewDistinctRing[T](window)
		if err := r.ImportState(raw, now); err != nil {
			return nil, err
		}
		return r, nil
	})
}

// activitySpikeState is activity_spike's whole carried state: the
// per-source count rings, and the freeze/day bookkeeping that is not a
// Baseline (see activitySpikeSourceState). The two baselineSets are
// deliberately absent -- StateStore already carries them.
type activitySpikeState struct {
	Counts  json.RawMessage `json:"counts"`
	Sources json.RawMessage `json:"sources"`
}

// activitySpikeSourceStateDocument is activitySpikeSourceState's JSON
// form. Written out field by field rather than by tagging the live
// struct, so the persisted shape is a decision this file makes and can
// be read here, and so a field added to the in-memory struct is not
// silently persisted (or silently absent) by accident.
type activitySpikeSourceStateDocument struct {
	HourDay        [24]string `json:"hourDay"`
	HourPeak       [24]int    `json:"hourPeak"`
	Frozen         bool       `json:"frozen"`
	FrozenSince    time.Time  `json:"frozenSince"`
	Peaked         bool       `json:"peaked"`
	MaturityDay    string     `json:"maturityDay"`
	MaturityStreak int        `json:"maturityStreak"`
	DirtyToday     bool       `json:"dirtyToday"`
}

// ExportState satisfies Snapshotted.
func (d *activitySpikeDefinition) ExportState() (json.RawMessage, error) {
	counts, err := countRingKeyedState(d.counts)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting counts: %w", d.def.ID, err)
	}
	sources, err := d.sources.Export(func(st *activitySpikeSourceState) (json.RawMessage, error) {
		return json.Marshal(activitySpikeSourceStateDocument{
			HourDay:        st.hourDay,
			HourPeak:       st.hourPeak,
			Frozen:         st.frozen,
			FrozenSince:    st.frozenSince,
			Peaked:         st.peaked,
			MaturityDay:    st.maturityDay,
			MaturityStreak: st.maturityStreak,
			DirtyToday:     st.dirtyToday,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting sources: %w", d.def.ID, err)
	}
	return json.Marshal(activitySpikeState{Counts: counts, Sources: sources})
}

// ImportState satisfies Snapshotted.
//
// taken is not consulted: every timestamp this state is judged by is
// carried inside it (a ring bucket's own start time, a day bookkeeping
// entry's own calendar day), which is what lets a snapshot be restored
// correctly however long it sat on disk.
//
// A restored freeze keeps its frozenSince, so the backstop clock
// (activitySpikeFreezeBackstop) continues from where it was rather than
// restarting: a source frozen long before the restart hits its backstop
// on the next reading, exactly as it would have without one. A
// frozenSince ahead of now -- a snapshot written before the host's clock
// went backwards -- is clamped to now, because a backstop that can never
// elapse is a baseline frozen forever.
func (d *activitySpikeDefinition) ImportState(raw json.RawMessage, taken, now time.Time) error {
	var state activitySpikeState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.def.ID, err)
	}
	if len(state.Counts) > 0 {
		if err := restoreCountRingKeyed(d.counts, state.Counts, d.window, now); err != nil {
			return fmt.Errorf("engine: definition %q: restoring counts: %w", d.def.ID, err)
		}
	}
	if len(state.Sources) == 0 {
		return nil
	}
	err := d.sources.Import(state.Sources, func(raw json.RawMessage) (*activitySpikeSourceState, error) {
		var doc activitySpikeSourceStateDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		st := &activitySpikeSourceState{
			frozen:      doc.Frozen,
			frozenSince: doc.FrozenSince,
			peaked:      doc.Peaked,
		}
		if st.frozen && st.frozenSince.After(now) {
			st.frozenSince = now
		}
		for hour := 0; hour < 24; hour++ {
			if !dayIsRecent(doc.HourDay[hour], now) {
				continue // dropped, not folded -- see this file's doc comment
			}
			st.hourDay[hour] = doc.HourDay[hour]
			st.hourPeak[hour] = doc.HourPeak[hour]
		}
		if dayIsRecent(doc.MaturityDay, now) {
			st.maturityDay = doc.MaturityDay
			st.maturityStreak = doc.MaturityStreak
			st.dirtyToday = doc.DirtyToday
		}
		return st, nil
	})
	if err != nil {
		return fmt.Errorf("engine: definition %q: restoring sources: %w", d.def.ID, err)
	}

	// Resume each restored source's fallback baseline from the
	// StateStore. A frozen source is judged through baselines.snapshot,
	// which reads what is already in memory and never resumes anything
	// (see baselineSet.resume): without this, a source restored frozen
	// reads as having no baseline at all, so the restored window it is
	// meant to be judged against decides nothing until the freeze
	// backstop eventually forces a fold -- a restart made worse by
	// restoring, which is the opposite of the point.
	//
	// The hour buckets are deliberately left alone: they materialize on
	// the day rollover that folds them (rollHourBucket), exactly as they
	// did before this snapshot existed.
	d.sources.ForEach(func(key string, _ *activitySpikeSourceState) bool {
		d.baselines.resume(key, now)
		return true
	})
	return nil
}

// lowSlowScanState is low_slow_scan's carried state: one lowSlowTrack
// per source, itself three rings.
type lowSlowScanState struct {
	Tracks json.RawMessage `json:"tracks"`
}

// lowSlowTrackDocument is lowSlowTrack's JSON form -- its three rings,
// each exported by the ring's own codec.
type lowSlowTrackDocument struct {
	Ports json.RawMessage `json:"ports"`
	Hosts json.RawMessage `json:"hosts"`
	Drops json.RawMessage `json:"drops"`
}

// ExportState satisfies Snapshotted.
//
// low_slow_scan is the definition warm restart matters most to: its
// window is hours, so a cold start throws away most of a day's
// observation of a scan deliberately paced to take that long (#20).
func (d *lowSlowScanDefinition) ExportState() (json.RawMessage, error) {
	tracks, err := d.tracks.Export(func(tr *lowSlowTrack) (json.RawMessage, error) {
		ports, err := tr.ports.ExportState()
		if err != nil {
			return nil, err
		}
		hosts, err := tr.hosts.ExportState()
		if err != nil {
			return nil, err
		}
		drops, err := tr.drops.ExportState()
		if err != nil {
			return nil, err
		}
		return json.Marshal(lowSlowTrackDocument{Ports: ports, Hosts: hosts, Drops: drops})
	})
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting tracks: %w", d.def.ID, err)
	}
	return json.Marshal(lowSlowScanState{Tracks: tracks})
}

// ImportState satisfies Snapshotted. taken is not consulted -- see
// activitySpikeDefinition.ImportState.
func (d *lowSlowScanDefinition) ImportState(raw json.RawMessage, taken, now time.Time) error {
	var state lowSlowScanState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.def.ID, err)
	}
	if len(state.Tracks) == 0 {
		return nil
	}
	err := d.tracks.Import(state.Tracks, func(raw json.RawMessage) (*lowSlowTrack, error) {
		var doc lowSlowTrackDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		tr := &lowSlowTrack{
			ports: NewDistinctRing[int](d.window),
			hosts: NewDistinctRing[string](d.window),
			drops: NewCountRing(d.window),
		}
		if err := tr.ports.ImportState(doc.Ports, now); err != nil {
			return nil, err
		}
		if err := tr.hosts.ImportState(doc.Hosts, now); err != nil {
			return nil, err
		}
		if err := tr.drops.ImportState(doc.Drops, now); err != nil {
			return nil, err
		}
		return tr, nil
	})
	if err != nil {
		return fmt.Errorf("engine: definition %q: restoring tracks: %w", d.def.ID, err)
	}
	return nil
}

// destSpreadState is one direction's carried state: the destination
// breadth ring the threshold is counted on, and the (destination, port)
// pairs the emission's evidence is built from (#641).
type destSpreadState struct {
	Dests json.RawMessage `json:"dests"`
	Pairs json.RawMessage `json:"pairs"`
}

// ExportState satisfies Snapshotted.
func (d *destSpreadDefinition) ExportState() (json.RawMessage, error) {
	dests, err := distinctRingKeyedState(d.dests)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting destinations: %w", d.def.ID, err)
	}
	pairs, err := distinctRingKeyedState(d.pairs)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting pairs: %w", d.def.ID, err)
	}
	return json.Marshal(destSpreadState{Dests: dests, Pairs: pairs})
}

// ImportState satisfies Snapshotted. taken is not consulted -- see
// activitySpikeDefinition.ImportState.
func (d *destSpreadDefinition) ImportState(raw json.RawMessage, taken, now time.Time) error {
	var state destSpreadState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.def.ID, err)
	}
	if len(state.Dests) > 0 {
		if err := restoreDistinctRingKeyed(d.dests, state.Dests, d.window, now); err != nil {
			return fmt.Errorf("engine: definition %q: restoring destinations: %w", d.def.ID, err)
		}
	}
	if len(state.Pairs) > 0 {
		if err := restoreDistinctRingKeyed(d.pairs, state.Pairs, d.window, now); err != nil {
			return fmt.Errorf("engine: definition %q: restoring pairs: %w", d.def.ID, err)
		}
	}
	return nil
}

// ruleSpikeState is rule_spike's carried state: one hit-count ring per
// rule label.
type ruleSpikeState struct {
	Hits json.RawMessage `json:"hits"`
}

// ExportState satisfies Snapshotted.
func (d *ruleSpikeDefinition) ExportState() (json.RawMessage, error) {
	hits, err := countRingKeyedState(d.hits)
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting hits: %w", d.def.ID, err)
	}
	return json.Marshal(ruleSpikeState{Hits: hits})
}

// ImportState satisfies Snapshotted. taken is not consulted -- see
// activitySpikeDefinition.ImportState.
//
// This is the other half of #368's restart scenario. The baseline side
// was answered by StateStore: a rule whose baseline was warm before a
// restart does not spend another window blind. The ring side was not --
// the restarted process compared that warm baseline against a rate
// derived from an empty ring, which reads as a collapse rather than a
// spike and so cost a detection rather than causing a false one. Both
// halves resume now.
func (d *ruleSpikeDefinition) ImportState(raw json.RawMessage, taken, now time.Time) error {
	var state ruleSpikeState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.def.ID, err)
	}
	if len(state.Hits) == 0 {
		return nil
	}
	if err := restoreCountRingKeyed(d.hits, state.Hits, d.window, now); err != nil {
		return fmt.Errorf("engine: definition %q: restoring hits: %w", d.def.ID, err)
	}
	return nil
}

// offHoursState is off_hours' carried state: the still-accumulating
// per-(source, hour) day counts. Its twenty-four baselines per source
// are StateStore's, not this document's.
type offHoursState struct {
	Days json.RawMessage `json:"days"`
}

// offHoursDayDocument is offHoursDay's JSON form.
type offHoursDayDocument struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// ExportState satisfies Snapshotted.
func (d *offHoursDefinition) ExportState() (json.RawMessage, error) {
	days, err := d.days.Export(func(b *offHoursDay) (json.RawMessage, error) {
		return json.Marshal(offHoursDayDocument{Day: b.day, Count: b.count})
	})
	if err != nil {
		return nil, fmt.Errorf("engine: definition %q: exporting days: %w", d.def.ID, err)
	}
	return json.Marshal(offHoursState{Days: days})
}

// ImportState satisfies Snapshotted. taken is not consulted -- see
// activitySpikeDefinition.ImportState.
//
// A bucket whose day is neither today's nor yesterday's is restored
// empty rather than dropped from the map: keeping the key preserves this
// source's place in Keyed's eviction order, and an empty bucket is
// exactly what off_hours' Evaluate treats as first sight of that hour,
// which is the truth about it.
func (d *offHoursDefinition) ImportState(raw json.RawMessage, taken, now time.Time) error {
	var state offHoursState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: definition %q: %w", d.def.ID, err)
	}
	if len(state.Days) == 0 {
		return nil
	}
	err := d.days.Import(state.Days, func(raw json.RawMessage) (*offHoursDay, error) {
		var doc offHoursDayDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		if !dayIsRecent(doc.Day, now) {
			return &offHoursDay{}, nil
		}
		return &offHoursDay{day: doc.Day, count: doc.Count}, nil
	})
	if err != nil {
		return fmt.Errorf("engine: definition %q: restoring days: %w", d.def.ID, err)
	}
	return nil
}
