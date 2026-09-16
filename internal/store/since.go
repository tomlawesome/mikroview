// SPDX-License-Identifier: AGPL-3.0-only

package store

// Since returns the events the ring still holds with ID > afterID, oldest
// first, up to max of them -- the read the evaluation engine drives its
// cursor from (#1109), in place of the bounded second queue it used to be
// fed through.
//
// Why a forward read rather than another Query: the engine wants "what
// have I not evaluated yet", which is the whole held tail past a cursor in
// ingest order, with no filters and no time window. Query answers the
// opposite question -- the newest N matching a filter, newest first -- and
// would have to reverse its result and re-derive the cursor arithmetic to
// serve this one.
//
// oldestHeld and newestHeld describe what the ring holds at the moment of
// the call, and are returned whether or not any event matched, because
// they are the only way the caller can tell "nothing new" from "the events
// you had not reached were evicted": eviction only ever takes from the
// oldest end, so oldestHeld > afterID+1 means everything between the two
// is gone for good. newestHeld is s.nextID -- the last ID Insert issued,
// which Reset and Resize deliberately never rewind (see their own
// comments), so it is a monotonic high-water mark and not a count of what
// survives. An empty ring reports oldestHeld = nextID + 1, i.e. "the
// oldest event still held is the next one to arrive", which keeps the
// oldestHeld > cursor test honest on a store that has just been reset
// rather than making the caller special-case emptiness.
//
// Position is O(1) from the ID offset, the same arithmetic Query.BeforeID
// uses (see its doc comment): IDs are sequential and never reused, so the
// distance between an ID and s.nextID is the distance between their ring
// slots.
func (s *Store) Since(afterID uint64, max int) (events []Event, oldestHeld, newestHeld uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	newestHeld = s.nextID
	if s.count == 0 {
		return nil, s.nextID + 1, newestHeld
	}
	oldestHeld = s.nextID - uint64(s.count) + 1
	if max <= 0 {
		return nil, oldestHeld, newestHeld
	}

	// The first ID this call may return: one past the cursor, or the
	// oldest survivor when the cursor has fallen behind eviction.
	startID := afterID + 1
	if startID < oldestHeld {
		startID = oldestHeld
	}
	if startID > newestHeld {
		return nil, oldestHeld, newestHeld
	}

	n := int(newestHeld - startID + 1)
	if n > max {
		n = max
	}
	// s.head is the slot the next Insert writes, so the newest held event
	// sits at head-1 and startID sits that many slots further back again.
	// The second % brings a negative index back into range -- Go's modulo
	// keeps the sign of the dividend, the same fix Resize spells out.
	idx := ((s.head-1-int(newestHeld-startID))%s.capacity + s.capacity) % s.capacity
	events = make([]Event, 0, n)
	for i := 0; i < n; i++ {
		events = append(events, s.buf[idx])
		idx++
		if idx == s.capacity {
			idx = 0
		}
	}
	return events, oldestHeld, newestHeld
}
