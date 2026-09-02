// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"sync"
	"testing"
	"time"
)

// heldIDs returns the IDs the ring currently holds, oldest first, read
// back through Query so the assertions below exercise the same walk
// every real reader uses rather than poking at the buffer directly. A
// resize that left head/count/capacity mutually inconsistent would show
// up here and nowhere else.
func heldIDs(t *testing.T, s *Store) []uint64 {
	t.Helper()
	res := s.Query(Query{Limit: 100000})
	out := make([]uint64, 0, len(res.Events))
	// Query already hands back oldest first.
	for _, e := range res.Events {
		out = append(out, e.ID)
	}
	return out
}

func fill(s *Store, n int) {
	for i := 0; i < n; i++ {
		s.Insert(Event{RuleLabel: "resize-test"})
	}
}

func TestResizeGrowKeepsEverything(t *testing.T) {
	s := New(4, time.Hour)
	fill(s, 6) // wraps: holds IDs 3,4,5,6

	kept, evicted := s.Resize(10)
	if kept != 4 || evicted != 0 {
		t.Fatalf("Resize(10) = (kept %d, evicted %d), want (4, 0)", kept, evicted)
	}
	if got, want := heldIDs(t, s), []uint64{3, 4, 5, 6}; !equalIDs(got, want) {
		t.Errorf("held %v, want %v -- growing must keep every event", got, want)
	}
	if s.Capacity() != 10 {
		t.Errorf("Capacity() = %d, want 10", s.Capacity())
	}

	// The grown room is real: six more events fit without evicting.
	fill(s, 6)
	if got, want := heldIDs(t, s), []uint64{3, 4, 5, 6, 7, 8, 9, 10, 11, 12}; !equalIDs(got, want) {
		t.Errorf("after filling the grown ring, held %v, want %v", got, want)
	}
}

func TestResizeShrinkEvictsOldestFirst(t *testing.T) {
	s := New(8, time.Hour)
	fill(s, 8) // full: IDs 1..8

	kept, evicted := s.Resize(3)
	if kept != 3 || evicted != 5 {
		t.Fatalf("Resize(3) = (kept %d, evicted %d), want (3, 5)", kept, evicted)
	}
	if got, want := heldIDs(t, s), []uint64{6, 7, 8}; !equalIDs(got, want) {
		t.Errorf("held %v, want %v -- a shrink must drop the oldest, not the newest", got, want)
	}

	// The shrunk ring still behaves like a ring: the next insert
	// overwrites the oldest survivor rather than growing past capacity.
	fill(s, 1)
	if got, want := heldIDs(t, s), []uint64{7, 8, 9}; !equalIDs(got, want) {
		t.Errorf("after one more insert, held %v, want %v", got, want)
	}
}

// A shrink from a ring that has not wrapped yet is the other half of the
// index arithmetic: head equals count rather than pointing at the oldest
// slot, so a version that only handled the wrapped case would keep the
// wrong events here.
func TestResizeShrinkBeforeTheRingHasWrapped(t *testing.T) {
	s := New(16, time.Hour)
	fill(s, 5) // IDs 1..5, head == 5, never wrapped

	kept, evicted := s.Resize(2)
	if kept != 2 || evicted != 3 {
		t.Fatalf("Resize(2) = (kept %d, evicted %d), want (2, 3)", kept, evicted)
	}
	if got, want := heldIDs(t, s), []uint64{4, 5}; !equalIDs(got, want) {
		t.Errorf("held %v, want %v", got, want)
	}
}

// Shrinking to exactly what is held keeps all of it, and leaves head at
// 0 rather than one past the end -- the off-by-one that would make the
// next Insert write outside the buffer.
func TestResizeToExactlyWhatIsHeld(t *testing.T) {
	s := New(10, time.Hour)
	fill(s, 4)

	kept, evicted := s.Resize(4)
	if kept != 4 || evicted != 0 {
		t.Fatalf("Resize(4) = (kept %d, evicted %d), want (4, 0)", kept, evicted)
	}
	fill(s, 1)
	if got, want := heldIDs(t, s), []uint64{2, 3, 4, 5}; !equalIDs(got, want) {
		t.Errorf("held %v, want %v", got, want)
	}
}

func TestResizeToSameCapacityIsANoOp(t *testing.T) {
	s := New(5, time.Hour)
	fill(s, 3)
	kept, evicted := s.Resize(5)
	if kept != 3 || evicted != 0 {
		t.Fatalf("Resize(5) = (kept %d, evicted %d), want (3, 0)", kept, evicted)
	}
}

func TestResizeNonPositiveCapacityBecomesOne(t *testing.T) {
	s := New(5, time.Hour)
	fill(s, 3)
	if kept, _ := s.Resize(0); kept != 1 {
		t.Fatalf("Resize(0) kept %d events, want 1", kept)
	}
	if s.Capacity() != 1 {
		t.Fatalf("Capacity() = %d, want 1", s.Capacity())
	}
}

// Stats must describe the resized ring, not the one before it -- the
// settings row reads capacity/count/oldestHeld straight off this.
func TestResizeUpdatesStats(t *testing.T) {
	s := New(8, time.Hour)
	for i := 0; i < 8; i++ {
		s.Insert(Event{ReceivedAt: time.Now().Add(time.Duration(i-8) * time.Minute)})
	}
	before := s.Stats()
	s.Resize(2)
	after := s.Stats()

	if after.Capacity != 2 || after.Count != 2 {
		t.Errorf("after shrink: capacity %d count %d, want 2 and 2", after.Capacity, after.Count)
	}
	if !after.OldestHeld.After(before.OldestHeld) {
		t.Errorf("oldestHeld went from %v to %v -- a shrink must move the buffer's reach forward",
			before.OldestHeld, after.OldestHeld)
	}
	if after.Total != before.Total {
		t.Errorf("Total changed from %d to %d -- lifetime counters are not what the ring holds",
			before.Total, after.Total)
	}
}

// The Done-when's "no crash mid-resize under load": inserts and queries
// running flat out while the ring is resized repeatedly, in both
// directions. Run with -race this is also the lock-discipline test.
func TestResizeUnderConcurrentLoad(t *testing.T) {
	s := New(500, time.Hour)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.Insert(Event{RuleLabel: "load"})
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.Query(Query{Limit: 50})
				s.Stats()
			}
		}
	}()

	for i := 0; i < 40; i++ {
		if i%2 == 0 {
			s.Resize(50)
		} else {
			s.Resize(2000)
		}
	}
	close(stop)
	wg.Wait()

	// Whatever survived must still be a coherent ring: contiguous IDs,
	// no more than capacity of them.
	held := heldIDs(t, s)
	if len(held) > s.Capacity() {
		t.Fatalf("holding %d events in a ring of %d", len(held), s.Capacity())
	}
	for i := 1; i < len(held); i++ {
		if held[i] != held[i-1]+1 {
			t.Fatalf("held IDs are not contiguous around index %d: %d then %d", i, held[i-1], held[i])
		}
	}
}

func equalIDs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
