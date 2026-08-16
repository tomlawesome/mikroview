// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "time"

// windowBucketCount, bucketSpanFor, CountRing and DistinctRing are
// internal/detect's countRing/distinctRing (see internal/detect/window.go,
// issue #76), lifted here per docs/decisions/evaluation-engine.md section
// 1 as the chassis's one windowed-state primitive -- #405 ports
// internal/detect onto these directly instead of its own copy, which is
// why they are exported here (they were package-private in internal/detect,
// where nothing outside the package needed them). internal/detect keeps
// its own unexported copy, unchanged, until that port lands: this is a
// copy of the mechanism and its reasoning, not a move, so nothing here
// changes internal/detect's behaviour.
//
// windowBucketCount is how many buckets every ring below carries,
// regardless of the window it's sized for -- generalizing the same
// lazily-reset rolling-bucket trick internal/store/ring.go's
// secBuckets/secBucketTime uses for its 60-second event-rate counter,
// but with a configurable bucket span instead of a fixed one second, so
// the same primitive serves definitions windowed anywhere from seconds
// (an activity spike) to hours (a low-and-slow scan) while keeping both
// Add and a window query O(windowBucketCount) rather than O(events).
const windowBucketCount = 60

// minBucketSpan floors bucketSpanFor so a short window (well under a
// minute) doesn't collapse to a sub-second span -- there's no value in
// bucketing finer than a second for anything this chassis evaluates, and
// it keeps the slot-number arithmetic in whole seconds.
const minBucketSpan = time.Second

// maxDistinctPerBucket is a memory ceiling on DistinctRing, not a
// behavior-affecting limit: every real definition threshold is expected
// to sit far below it, so it only ever matters as a backstop against a
// pathological number of distinct values in one bucket (e.g. spoofed
// source IPs).
const maxDistinctPerBucket = 256

// bucketSpanFor picks how much wall-clock time each of a ring's
// windowBucketCount buckets covers, given the window the ring needs to
// answer queries over. Bucket-boundary imprecision (a query can be off
// by up to one bucket's worth of time at the window edge) is the
// accepted trade for O(1) inserts and O(windowBucketCount) queries
// regardless of event volume.
func bucketSpanFor(window time.Duration) time.Duration {
	span := window / windowBucketCount
	if span < minBucketSpan {
		span = minBucketSpan
	}
	return span
}

// bucketSlot maps t to the array index and monotonic slot number it
// belongs to at the given span -- the slot number (not the index alone)
// is what's compared against a bucket's stored slot to tell a live
// bucket from a stale one still holding an earlier window's data,
// exactly as secBucketTime[idx] == sec does in internal/store/ring.go.
func bucketSlot(t time.Time, span time.Duration) (idx int, slot int64) {
	slot = t.UnixNano() / int64(span)
	idx = int(slot % windowBucketCount)
	if idx < 0 {
		idx += windowBucketCount
	}
	return idx, slot
}

// bucketsToScan is how many of a ring's buckets a query over window
// needs to visit -- capped at windowBucketCount, the ring's full
// capacity, so a query for a window wider than the ring was sized for
// doesn't run off the end of the array (it just can't see further back
// than the ring actually retains).
func bucketsToScan(window, span time.Duration) int64 {
	n := int64(window)/int64(span) + 1
	if n > windowBucketCount {
		n = windowBucketCount
	}
	if n < 1 {
		n = 1
	}
	return n
}

// countBucket holds one bucket's tally for CountRing: total is every
// Add regardless of isTrue, trueCount is the subset where isTrue was
// true -- e.g. a drop/reject-vs-accept ratio needs both from the same
// bucket, not two parallel rings.
type countBucket struct {
	slot      int64
	total     int
	trueCount int
}

// CountRing is a fixed-size rolling counter over a configurable span
// (see bucketSpanFor), replacing a per-key []time.Time sample slice plus
// a per-event linear rescan: Add is O(1), and Count/Ratio are
// O(windowBucketCount) no matter how many events actually occurred.
//
// Single-writer, same convention as internal/detect's original: meant to
// be owned by one key's state inside a Keyed[V] and driven from the
// engine's evaluation goroutine only. It has no read API of its own that
// needs the copy-on-read treatment (see Keyed's doc comment) -- Count and
// Ratio return plain ints, never a reference into the ring's own
// buckets.
type CountRing struct {
	span    time.Duration
	buckets [windowBucketCount]countBucket
}

// NewCountRing constructs a CountRing sized to answer queries over
// window.
func NewCountRing(window time.Duration) *CountRing {
	return &CountRing{span: bucketSpanFor(window)}
}

// Add records one event at t, optionally marking it as satisfying
// whatever true/false predicate the caller's Ratio call will later ask
// about (e.g. "was this a drop/reject"). Callers that only ever need a
// plain count pass true.
func (r *CountRing) Add(t time.Time, isTrue bool) {
	idx, slot := bucketSlot(t, r.span)
	b := &r.buckets[idx]
	if b.slot != slot {
		*b = countBucket{slot: slot}
	}
	b.total++
	if isTrue {
		b.trueCount++
	}
}

// sum totals trueCount/total across the buckets covering the last
// window ending at now -- stale buckets (b.slot != the slot that array
// index should hold for this offset) contribute nothing, the same
// lazy-reset-at-query-time behavior internal/store.Store.Stats relies on
// for secBuckets.
func (r *CountRing) sum(now time.Time, window time.Duration) (trueCount, total int) {
	_, nowSlot := bucketSlot(now, r.span)
	n := bucketsToScan(window, r.span)
	for i := int64(0); i < n; i++ {
		slot := nowSlot - i
		idx := int(slot % windowBucketCount)
		if idx < 0 {
			idx += windowBucketCount
		}
		b := &r.buckets[idx]
		if b.slot == slot {
			total += b.total
			trueCount += b.trueCount
		}
	}
	return trueCount, total
}

// Count returns how many events were added within window of now,
// regardless of their isTrue value.
func (r *CountRing) Count(now time.Time, window time.Duration) int {
	_, total := r.sum(now, window)
	return total
}

// Ratio returns both the true-count and the total count within window
// of now, for a definition that needs a fraction (e.g. drop ratio)
// rather than a plain count.
func (r *CountRing) Ratio(now time.Time, window time.Duration) (trueCount, total int) {
	return r.sum(now, window)
}

// distinctBucket holds one bucket's exact set of distinct values seen.
// The map is allocated lazily and reused (cleared, not reallocated)
// across the bucket's lazy resets, the same way countBucket's ints are
// zeroed in place -- Add only pays an allocation once per bucket for the
// ring's lifetime, not once per reset.
type distinctBucket[T comparable] struct {
	slot   int64
	values map[T]struct{}
}

// DistinctRing is CountRing's counterpart for distinct-value breadth
// (distinct ports, distinct destination hosts, ...) instead of a raw
// count: same fixed-size rolling-bucket shape, but each bucket holds an
// exact set capped at maxDistinctPerBucket instead of an int.
//
// Same single-writer convention as CountRing: Count is allocation-free
// and safe to call on every event; Values allocates a fresh map every
// call and is only meant to be called once Count has already shown a
// threshold is crossed. Both already return a value that shares no
// backing storage with the ring's own buckets, so -- unlike Keyed's
// Snapshot -- there is nothing further to copy at this boundary; the
// copy already happens as part of computing the answer.
type DistinctRing[T comparable] struct {
	span    time.Duration
	buckets [windowBucketCount]distinctBucket[T]
	// scratch backs Count -- reused across calls (cleared, not
	// reallocated) so the cheap "did we cross the threshold" query the
	// hot path runs on every event never allocates.
	scratch map[T]struct{}
}

// NewDistinctRing constructs a DistinctRing sized to answer queries over
// window.
func NewDistinctRing[T comparable](window time.Duration) *DistinctRing[T] {
	return &DistinctRing[T]{span: bucketSpanFor(window), scratch: make(map[T]struct{})}
}

// Add records v as seen at t. Filtering (e.g. a definition's scope
// restricting which ports/hosts count) deliberately happens at query
// time in Count/Values, not here -- inserting every value unfiltered is
// what lets a live scope change take effect on the very next query,
// rather than only once samples recorded under the old scope age out of
// the window.
func (r *DistinctRing[T]) Add(t time.Time, v T) {
	idx, slot := bucketSlot(t, r.span)
	b := &r.buckets[idx]
	if b.slot != slot {
		b.slot = slot
		if b.values == nil {
			b.values = make(map[T]struct{})
		} else {
			clear(b.values)
		}
	}
	if len(b.values) >= maxDistinctPerBucket {
		return
	}
	b.values[v] = struct{}{}
}

// Count unions the buckets covering the last window ending at now and
// returns how many distinct values (after filter, if non-nil) that
// union holds. Allocation-free: it reuses r.scratch rather than
// building a fresh map, so it's cheap enough to call on every event to
// check a threshold before paying for Values' full set.
func (r *DistinctRing[T]) Count(now time.Time, window time.Duration, filter func(T) bool) int {
	clear(r.scratch)
	_, nowSlot := bucketSlot(now, r.span)
	n := bucketsToScan(window, r.span)
	for i := int64(0); i < n; i++ {
		slot := nowSlot - i
		idx := int(slot % windowBucketCount)
		if idx < 0 {
			idx += windowBucketCount
		}
		b := &r.buckets[idx]
		if b.slot != slot {
			continue
		}
		for v := range b.values {
			if filter != nil && !filter(v) {
				continue
			}
			r.scratch[v] = struct{}{}
		}
	}
	return len(r.scratch)
}

// Values is Count, but returns the actual union rather than its size --
// only meant to be called once Count has already shown a threshold is
// crossed (e.g. to build an emission's evidence), not on every event,
// since unlike Count it allocates a fresh map every call.
func (r *DistinctRing[T]) Values(now time.Time, window time.Duration, filter func(T) bool) map[T]struct{} {
	out := make(map[T]struct{})
	_, nowSlot := bucketSlot(now, r.span)
	n := bucketsToScan(window, r.span)
	for i := int64(0); i < n; i++ {
		slot := nowSlot - i
		idx := int(slot % windowBucketCount)
		if idx < 0 {
			idx += windowBucketCount
		}
		b := &r.buckets[idx]
		if b.slot != slot {
			continue
		}
		for v := range b.values {
			if filter != nil && !filter(v) {
				continue
			}
			out[v] = struct{}{}
		}
	}
	return out
}
