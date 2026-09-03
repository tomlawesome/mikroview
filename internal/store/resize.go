// SPDX-License-Identifier: AGPL-3.0-only

package store

// Resize changes the ring's capacity in place, keeping the newest events
// and letting the oldest go first when the new capacity is smaller than
// what is currently held. It returns how many events survived and how
// many were dropped to make the new size fit.
//
// Growing keeps everything: a larger ring is the same events in a bigger
// box, and the extra room simply fills from arriving traffic. Shrinking
// is the lossy direction, and oldest-first is the only eviction order
// that matches what the ring already does on every Insert -- an operator
// who lowers store.maxMemory sees the same hours fall away that would
// have fallen away anyway, just sooner.
//
// Counters (total, totalByAction, totalByRule, the per-second and
// per-minute buckets) are deliberately untouched. They are lifetime
// tallies of what arrived, not a description of what is still held, so
// resizing the buffer is not an event in their world; nextID is left
// alone for the same reason, so IDs stay monotonic across a resize and
// no client can be handed an ID it has already seen.
//
// The new buffer is allocated before the write lock is taken. At the
// sizes this control offers (up to whatever the host can spare) the
// allocation is by far the longest part of the operation, and doing it
// under the lock would stall the ingest goroutine -- the one writer --
// for its whole duration. Only the copy and the swap need the lock.
func (s *Store) Resize(capacity int) (kept, evicted int) {
	if capacity <= 0 {
		// Same floor New applies: a capacity too small to hold one event
		// is treated as one, never as zero, so the buffer always has
		// somewhere to put the next arrival.
		capacity = 1
	}
	buf := make([]Event, capacity)

	s.mu.Lock()
	defer s.mu.Unlock()

	if capacity == s.capacity {
		return s.count, 0
	}

	keep := s.count
	if keep > capacity {
		keep = capacity
	}
	// s.head is the slot the next Insert writes, so the newest event
	// sits at head-1 and the newest `keep` of them start at head-keep.
	// Modulo arithmetic in Go keeps the sign of the dividend, hence the
	// second % to bring a negative index back into range.
	start := s.head - keep
	for i := 0; i < keep; i++ {
		idx := ((start+i)%s.capacity + s.capacity) % s.capacity
		buf[i] = s.buf[idx]
	}

	evicted = s.count - keep
	s.buf = buf
	s.capacity = capacity
	s.count = keep
	// keep % capacity, not keep: a resize that exactly fills the new
	// buffer must leave head at 0 (the oldest slot, which the next
	// Insert overwrites), not one past the end.
	s.head = keep % capacity
	return keep, evicted
}

// Capacity reports the ring's current element count ceiling. Stats()
// carries the same number, but a caller that only wants to know whether
// a resize is needed should not have to build a whole stats snapshot to
// find out.
func (s *Store) Capacity() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.capacity
}
