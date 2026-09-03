// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// This file is the warm-restart (#795) export/import half of window.go's
// two rolling-bucket primitives. Issue #795's decision, owner 2026-09-02:
// the derived state a running mikroview has learned -- per-minute
// buckets, baselines, episode windows -- is written whole every few
// minutes and read back on boot, so a restart does not throw away what
// the process already knew. Baselines already survive through StateStore
// (state.go); these rings did not, and they are what every windowed
// definition actually judges against.
//
// # Why a bucket carries a timestamp rather than its slot number
//
// A ring's internal bucket identity is a slot number -- wall-clock nanos
// divided by the ring's own span (see bucketSlot) -- so it only means
// anything alongside the span it was computed with. A snapshot that
// stored slot numbers would silently mis-place every bucket the moment
// an operator retuned the definition's window param, because the same
// slot number names a different instant at a different span.
//
// Storing the bucket's start time instead makes the document
// self-describing: import re-derives idx/slot with whatever span the
// receiving ring was constructed for, so a retuned window re-buckets the
// restored data rather than corrupting it. Two exported buckets that
// collapse onto one bucket at a coarser span are merged (counts summed,
// value sets unioned), which is exactly what the ring would hold had the
// events arrived at that span in the first place.
//
// # Expiry: dropped, never shifted
//
// A ring retains span*windowBucketCount of history and no more. Import
// keeps only the buckets still inside that retention relative to the
// caller's now, and drops the rest outright: shifting an expired bucket
// forward so it "fits" would present old traffic as recent, which is the
// one thing a restart must not do. Buckets stamped in the future (a
// snapshot written by a host whose clock has since gone backwards --
// #795's hostile-clock case) are dropped for the same reason.

// countBucketState is one CountRing bucket as it appears in a snapshot.
// At is the start of the bucket's own span, in UTC.
type countBucketState struct {
	At        time.Time `json:"at"`
	Total     int       `json:"total"`
	TrueCount int       `json:"trueCount"`
}

// countRingState is a CountRing's whole snapshot: every bucket currently
// holding anything, oldest first. The span is deliberately not recorded
// -- see this file's doc comment on why import re-derives placement from
// the receiving ring's span rather than trusting the writer's.
type countRingState struct {
	Buckets []countBucketState `json:"buckets"`
}

// bucketStart is the wall-clock instant slot begins at, for a ring of
// this span -- bucketSlot's inverse, and the only value a snapshot ever
// records for a bucket.
func bucketStart(slot int64, span time.Duration) time.Time {
	return time.Unix(0, slot*int64(span)).UTC()
}

// liveSlot reports the slot at holds in a ring of this span, and whether
// a bucket stamped at is still inside that ring's retention as of now:
// not in the future, and no further back than windowBucketCount-1 slots.
func liveSlot(at, now time.Time, span time.Duration) (int64, bool) {
	_, slot := bucketSlot(at, span)
	_, nowSlot := bucketSlot(now, span)
	if slot > nowSlot || nowSlot-slot >= windowBucketCount {
		return 0, false
	}
	return slot, true
}

// ExportState renders this ring's live buckets as JSON, for the periodic
// snapshot #795 writes. Buckets are emitted oldest first so the document
// is stable across writes rather than reordering with Go's map/array
// walk -- a snapshot that changes bytes without changing meaning is
// harder to diff and harder to test.
//
// Exports every bucket the array currently holds, including ones already
// aged out: staleness is judged at import against the importing
// process's own now (see ImportState), not against the writer's clock,
// so nothing is gained by filtering here and something is lost -- a
// snapshot written seconds before a restart would drop buckets the
// restarted process could still legitimately count.
//
// Single-writer, like the rest of CountRing: call it from the evaluation
// goroutine, or through the Keyed[V].Export walk that holds Keyed's own
// mutex.
func (r *CountRing) ExportState() (json.RawMessage, error) {
	state := countRingState{}
	for i := range r.buckets {
		b := &r.buckets[i]
		if b.total == 0 && b.trueCount == 0 {
			continue
		}
		state.Buckets = append(state.Buckets, countBucketState{
			At:        bucketStart(b.slot, r.span),
			Total:     b.total,
			TrueCount: b.trueCount,
		})
	}
	sort.Slice(state.Buckets, func(i, j int) bool {
		return state.Buckets[i].At.Before(state.Buckets[j].At)
	})
	return json.Marshal(state)
}

// ImportState restores raw into this ring as of now, keeping only the
// buckets still inside its retention and merging any that land in the
// same bucket at this ring's span (see this file's doc comment).
//
// Meant to be called on a freshly constructed ring before any Add --
// merging rather than replacing is the conservative rule for the case
// where it is not, since dropping counts a live ring already holds would
// be a silent under-count.
func (r *CountRing) ImportState(raw json.RawMessage, now time.Time) error {
	if r.span <= 0 {
		return fmt.Errorf("engine: CountRing.ImportState on an unconstructed ring (span %v) -- construct with NewCountRing first", r.span)
	}
	var state countRingState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: CountRing.ImportState: %w", err)
	}
	for _, bs := range state.Buckets {
		slot, ok := liveSlot(bs.At, now, r.span)
		if !ok {
			continue
		}
		idx := int(slot % windowBucketCount)
		if idx < 0 {
			idx += windowBucketCount
		}
		b := &r.buckets[idx]
		if b.slot != slot {
			*b = countBucket{slot: slot}
		}
		b.total += bs.Total
		b.trueCount += bs.TrueCount
	}
	return nil
}

// distinctBucketState is one DistinctRing bucket as it appears in a
// snapshot -- the bucket's start time and the exact values it held.
type distinctBucketState[T comparable] struct {
	At     time.Time `json:"at"`
	Values []T       `json:"values"`
}

// distinctRingState is a DistinctRing's whole snapshot. Same shape and
// same reasoning as countRingState.
type distinctRingState[T comparable] struct {
	Buckets []distinctBucketState[T] `json:"buckets"`
}

// ExportState renders this ring's live buckets as JSON. Buckets are
// ordered oldest first and each bucket's values by their own JSON
// encoding, so the same ring contents always produce the same bytes: a
// set has no order of its own, and letting Go's map walk pick one would
// make every snapshot differ from the last for no reason (see
// CountRing.ExportState for the rest of that reasoning).
//
// Sorting by encoding rather than by the value is what keeps this
// generic over T: DistinctRing is instantiated at int, string and
// HostPort, which share no ordering, but every one of them has exactly
// one JSON form.
//
// Same single-writer contract as the rest of DistinctRing.
func (r *DistinctRing[T]) ExportState() (json.RawMessage, error) {
	state := distinctRingState[T]{}
	for i := range r.buckets {
		b := &r.buckets[i]
		if len(b.values) == 0 {
			continue
		}
		values, err := sortedByEncoding(b.values)
		if err != nil {
			return nil, fmt.Errorf("engine: DistinctRing.ExportState: %w", err)
		}
		state.Buckets = append(state.Buckets, distinctBucketState[T]{
			At:     bucketStart(b.slot, r.span),
			Values: values,
		})
	}
	sort.Slice(state.Buckets, func(i, j int) bool {
		return state.Buckets[i].At.Before(state.Buckets[j].At)
	})
	return json.Marshal(state)
}

// ImportState restores raw into this ring as of now, keeping only the
// buckets still inside its retention and unioning any that land in the
// same bucket at this ring's span. maxDistinctPerBucket is honoured
// exactly as Add honours it -- a snapshot cannot be a way past the
// memory ceiling, however it was written.
func (r *DistinctRing[T]) ImportState(raw json.RawMessage, now time.Time) error {
	if r.span <= 0 {
		return fmt.Errorf("engine: DistinctRing.ImportState on an unconstructed ring (span %v) -- construct with NewDistinctRing first", r.span)
	}
	var state distinctRingState[T]
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: DistinctRing.ImportState: %w", err)
	}
	for _, bs := range state.Buckets {
		slot, ok := liveSlot(bs.At, now, r.span)
		if !ok {
			continue
		}
		idx := int(slot % windowBucketCount)
		if idx < 0 {
			idx += windowBucketCount
		}
		b := &r.buckets[idx]
		if b.slot != slot {
			b.slot = slot
			if b.values == nil {
				b.values = make(map[T]struct{})
			} else {
				clear(b.values)
			}
		}
		for _, v := range bs.Values {
			if len(b.values) >= maxDistinctPerBucket {
				break
			}
			b.values[v] = struct{}{}
		}
	}
	return nil
}

// sortedByEncoding turns a value set into a slice ordered by each
// value's JSON encoding -- see DistinctRing.ExportState for why that is
// the ordering available to a comparable-but-not-ordered T.
func sortedByEncoding[T comparable](values map[T]struct{}) ([]T, error) {
	out := make([]T, 0, len(values))
	encoded := make(map[T]string, len(values))
	for v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		encoded[v] = string(b)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return encoded[out[i]] < encoded[out[j]] })
	return out, nil
}
