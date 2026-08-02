// Package store holds the in-memory, time-windowed event buffer. There is
// no database: a fixed-capacity ring buffer is the hard memory ceiling,
// and the retention window is enforced at query time rather than via a
// periodic eviction scan.
package store

import (
	"sync"
	"time"
)

// Store is a fixed-capacity ring buffer of Events, safe for concurrent use.
// The intended access pattern is a single writer goroutine calling Insert
// (fed by a buffered channel from the syslog listeners) and many readers
// calling Query/Stats concurrently from HTTP handlers.
type Store struct {
	mu       sync.RWMutex
	buf      []Event
	capacity int
	head     int // index the next Insert will write to
	count    int // number of valid elements currently held (<=capacity)
	nextID   uint64
	window   time.Duration

	totalByAction map[Action]uint64
	total         uint64

	// secBuckets/secBucketTime implement a rolling per-second event-rate
	// counter over the last 60s without scanning the buffer: bucket i
	// holds the count for unix second secBucketTime[i], and is reset
	// lazily the next time that slot is reused for a new second.
	secBuckets    [60]uint64
	secBucketTime [60]int64
}

// New creates a Store holding at most capacity events, logically windowed
// to the given retention duration.
func New(capacity int, window time.Duration) *Store {
	if capacity <= 0 {
		capacity = 1
	}
	return &Store{
		buf:           make([]Event, capacity),
		capacity:      capacity,
		window:        window,
		totalByAction: make(map[Action]uint64),
	}
}

// Insert assigns the event a sequence ID and stores it, overwriting the
// oldest entry once the buffer is full. It returns the event as stored
// (with ID populated) so the caller can hand it off to the WebSocket hub
// without a second lookup.
func (s *Store) Insert(e Event) Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	s.nextID++
	e.ID = s.nextID
	// Production callers never set ReceivedAt, so it's always stamped here
	// with the ingest goroutine's own clock. Tests are allowed to pre-set it
	// to simulate events received at a specific past time for retention-
	// window assertions.
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = now
	}

	s.buf[s.head] = e
	s.head = (s.head + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}

	s.total++
	s.totalByAction[e.Action]++

	sec := now.Unix()
	idx := sec % 60
	if s.secBucketTime[idx] != sec {
		s.secBucketTime[idx] = sec
		s.secBuckets[idx] = 0
	}
	s.secBuckets[idx]++

	return e
}

// Stats is a point-in-time snapshot of store-wide counters.
type Stats struct {
	Total           uint64            `json:"total"`
	ByAction        map[Action]uint64 `json:"byAction"`
	EventsPerSecond float64           `json:"eventsPerSecond"`
	Capacity        int               `json:"capacity"`
	Count           int               `json:"count"`
	Window          time.Duration     `json:"windowNs"`
}

// Stats returns current totals and a rolling events/sec rate averaged over
// the last 10 seconds.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byAction := make(map[Action]uint64, len(s.totalByAction))
	for k, v := range s.totalByAction {
		byAction[k] = v
	}

	now := time.Now().Unix()
	var sum uint64
	const window = 10
	for i := int64(0); i < window; i++ {
		sec := now - i
		idx := sec % 60
		if idx < 0 {
			idx += 60
		}
		if s.secBucketTime[idx] == sec {
			sum += s.secBuckets[idx]
		}
	}

	return Stats{
		Total:           s.total,
		ByAction:        byAction,
		EventsPerSecond: float64(sum) / window,
		Capacity:        s.capacity,
		Count:           s.count,
		Window:          s.window,
	}
}
