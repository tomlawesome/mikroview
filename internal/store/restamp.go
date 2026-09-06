// SPDX-License-Identifier: AGPL-3.0-only

package store

// Restamp applies rewrite to every event currently held in the ring,
// under the write lock. It exists for one caller: internal/api's entity
// upsert/delete handlers, which use it to re-resolve the friendly-name
// fields already stamped onto buffered events when the entity behind
// them changes (#993). Names are resolved once, at ingest (see main.go),
// so without this every later fetch of an already-buffered event would
// resurface the pre-edit name -- silently undoing a rename on whatever
// screen that fetch lands on.
//
// A one-shot rewrite of what the buffer already holds, deliberately not
// a resolve-at-query layer: the same decision the frontend's
// appState.relabel() comment records. Resolution stays an ingest-time
// act, and a rewrite cannot outlive the buffer it edited.
//
// rewrite must touch only display fields (the *Name fields). Identity,
// ordering and counting fields (ID, times, IPs, ports, labels) are what
// the ring's own bookkeeping -- totals, buckets, cursors -- was built
// from, and this does not recompute any of it.
func (s *Store) Restamp(rewrite func(e *Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.count; i++ {
		rewrite(&s.buf[(s.head-s.count+i+s.capacity)%s.capacity])
	}
}
