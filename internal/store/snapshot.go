// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/snapshot"
)

// snapshotPartName is this part's key in a snapshot document. Stable
// across releases: it is how a later boot finds these bytes again.
const snapshotPartName = "store"

// minuteCounts is one per-minute bucket on the wire.
//
// The minute is written as its own unix-minute number and the counts are
// keyed by action name, rather than as the packed
// minuteBuckets/minuteBucketTime arrays this package uses in memory.
// Those arrays are indexed by minute-modulo-60 and by a slot position in
// actionSlots -- both internal details that a snapshot written by one
// build and read by another must not depend on. Adding an Action or
// changing timeSeriesMinutes would otherwise silently re-attribute
// restored counts to the wrong action or the wrong minute, which is
// worse than not restoring them.
type minuteCounts struct {
	Minute   int64             `json:"minute"`
	ByAction map[Action]uint64 `json:"byAction"`
}

// storeState is the store's slice of a snapshot document: lifetime
// tallies and the per-minute buckets behind Stats().TimeSeries.
//
// Deliberately absent: the events themselves, and the count of events
// held. #795 settled that the ring's raw log lines are never written to
// disk, so a restored store holds no lines and must not claim to --
// Count stays whatever the rebuilt ring actually has (zero at boot), and
// HourTops reports the restored minutes as incomplete for the same
// reason. Also absent: nextID, since IDs are this process's own handles
// on events it holds, and a restored one would be a handle on nothing.
type storeState struct {
	Total    uint64            `json:"total"`
	ByAction map[Action]uint64 `json:"byAction"`
	ByRule   map[string]uint64 `json:"byRule"`
	Minutes  []minuteCounts    `json:"minutes"`
}

// SnapshotPart returns this Store as a warm-restart part: the counters
// and per-minute buckets it has accumulated, never the events (#795).
//
// The interface belongs to internal/snapshot and the implementation to
// this package, so internal/snapshot never learns the shape of a Store
// and this package's state stays its own business.
func (s *Store) SnapshotPart() snapshot.Part { return storePart{s: s} }

type storePart struct{ s *Store }

func (p storePart) Name() string { return snapshotPartName }

func (p storePart) Export() (json.RawMessage, error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()

	state := storeState{
		Total:    p.s.total,
		ByAction: make(map[Action]uint64, len(p.s.totalByAction)),
		ByRule:   make(map[string]uint64, len(p.s.totalByRule)),
	}
	for action, count := range p.s.totalByAction {
		state.ByAction[action] = count
	}
	for rule, count := range p.s.totalByRule {
		state.ByRule[rule] = count
	}
	for i, minute := range p.s.minuteBucketTime {
		// Zero means the slot has never been used: a bucket for unix
		// minute 0 (1970) is not a thing this store can hold, since a
		// bucket only survives for the hour after it was written.
		if minute <= 0 {
			continue
		}
		byAction := make(map[Action]uint64, len(actionSlots))
		for slot, action := range actionSlots {
			if c := p.s.minuteBuckets[i][slot]; c > 0 {
				byAction[action] = c
			}
		}
		if len(byAction) == 0 {
			continue
		}
		state.Minutes = append(state.Minutes, minuteCounts{Minute: minute, ByAction: byAction})
	}

	return json.Marshal(state)
}

// Import restores the counters and whichever per-minute buckets are
// still inside the time-series window, and records that Stats() is now
// reporting partly-restored numbers.
//
// Buckets older than the window are dropped rather than restored into
// whatever slot their minute maps to: minuteBuckets is indexed modulo 60
// minutes, so an hour-old bucket would land on top of a current one and
// be reported as this minute's traffic. Same for a bucket stamped after
// now, which a clock step can produce.
//
// It refuses a Store that has already ingested. Restoring over live
// counters would add a snapshot's tallies to traffic this process has
// already counted, which is double counting that nothing downstream
// could detect; the only correct time to call this is at boot, before
// the ingest goroutine starts.
func (p storePart) Import(raw json.RawMessage, taken, now time.Time) error {
	var state storeState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}

	p.s.mu.Lock()
	defer p.s.mu.Unlock()

	if p.s.total > 0 {
		return fmt.Errorf("store: refusing to restore over %d already-counted event(s) -- a snapshot is only ever loaded at boot", p.s.total)
	}

	p.s.total = state.Total
	if state.ByAction != nil {
		p.s.totalByAction = state.ByAction
	}
	if state.ByRule != nil {
		p.s.totalByRule = state.ByRule
		// The cap applies to what comes off disk too: totalByRule's keys
		// are router log-prefixes chosen by whoever is sending syslog, so
		// a snapshot written while a label flood was in progress must not
		// reinstate more of them than a running store would hold. See
		// maxRuleLabels.
		if len(p.s.totalByRule) > maxRuleLabels {
			p.s.shedRuleLabelsLocked()
		}
	}

	nowMinute := now.Unix() / 60
	oldest := nowMinute - int64(timeSeriesMinutes-1)
	for _, bucket := range state.Minutes {
		if bucket.Minute < oldest || bucket.Minute > nowMinute {
			continue
		}
		idx := bucket.Minute % timeSeriesMinutes
		if idx < 0 {
			idx += timeSeriesMinutes
		}
		p.s.minuteBucketTime[idx] = bucket.Minute
		p.s.minuteBuckets[idx] = [len(actionSlots)]uint64{}
		for action, count := range bucket.ByAction {
			p.s.minuteBuckets[idx][actionSlot(action)] += count
		}
	}

	p.s.restoredTo = taken
	return nil
}

// RestoredTo reports the taken time of the snapshot this Store's
// counters came from, or the zero time on a cold start. Stats() carries
// the same answer; this exists for a caller that only wants to know
// whether a restore happened at all.
func (s *Store) RestoredTo() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restoredTo
}
