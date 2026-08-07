// SPDX-License-Identifier: AGPL-3.0-only

// Package store holds the in-memory, time-windowed event buffer. There is
// no database: a fixed-capacity ring buffer is the hard memory ceiling,
// and the retention window is enforced at query time rather than via a
// periodic eviction scan.
package store

import (
	"sort"
	"sync"
	"time"
)

// topRulesLimit caps how many entries Stats.TopRules returns -- a
// leaderboard, not a full dump of every rule label ever seen.
const topRulesLimit = 10

// timeSeriesMinutes is how much history Stats.TimeSeries covers, at
// 1-minute resolution.
const timeSeriesMinutes = 60

// actionSlots fixes which array index each Action occupies in
// minuteBuckets, so per-minute-per-action counts live in a plain array
// instead of allocating a map on every Insert.
var actionSlots = [...]Action{ActionAccept, ActionDrop, ActionReject, ActionLog, ActionUnknown}

func actionSlot(a Action) int {
	for i, s := range actionSlots {
		if s == a {
			return i
		}
	}
	return len(actionSlots) - 1 // unrecognized action folds into the unknown slot
}

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
	totalByRule   map[string]uint64
	total         uint64

	// secBuckets/secBucketTime implement a rolling per-second event-rate
	// counter over the last 60s without scanning the buffer: bucket i
	// holds the count for unix second secBucketTime[i], and is reset
	// lazily the next time that slot is reused for a new second.
	secBuckets    [60]uint64
	secBucketTime [60]int64

	// minuteBuckets/minuteBucketTime implement the same lazily-reset
	// rolling-bucket trick as secBuckets above, but per-minute and broken
	// down by action, for Stats.TimeSeries -- bucket i holds counts for
	// unix minute minuteBucketTime[i], reset the next time that slot is
	// reused for a new minute.
	minuteBuckets    [timeSeriesMinutes][len(actionSlots)]uint64
	minuteBucketTime [timeSeriesMinutes]int64
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
		totalByRule:   make(map[string]uint64),
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
	if e.RuleLabel != "" {
		s.totalByRule[e.RuleLabel]++
	}

	sec := now.Unix()
	idx := sec % 60
	if s.secBucketTime[idx] != sec {
		s.secBucketTime[idx] = sec
		s.secBuckets[idx] = 0
	}
	s.secBuckets[idx]++

	minute := sec / 60
	midx := minute % timeSeriesMinutes
	if s.minuteBucketTime[midx] != minute {
		s.minuteBucketTime[midx] = minute
		s.minuteBuckets[midx] = [len(actionSlots)]uint64{}
	}
	s.minuteBuckets[midx][actionSlot(e.Action)]++

	return e
}

// RuleCount is one entry in Stats.TopRules.
type RuleCount struct {
	Rule  string `json:"rule"`
	Count uint64 `json:"count"`
}

// TimeBucket is one point in Stats.TimeSeries: counts by action for a
// single one-minute window. ByAction omits actions with a zero count for
// that minute rather than listing all five every time.
type TimeBucket struct {
	Time     time.Time         `json:"time"`
	ByAction map[Action]uint64 `json:"byAction"`
}

// Stats is a point-in-time snapshot of store-wide counters.
type Stats struct {
	Total           uint64            `json:"total"`
	ByAction        map[Action]uint64 `json:"byAction"`
	TopRules        []RuleCount       `json:"topRules"`
	TimeSeries      []TimeBucket      `json:"timeSeries"`
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

	topRules := make([]RuleCount, 0, len(s.totalByRule))
	for rule, count := range s.totalByRule {
		topRules = append(topRules, RuleCount{Rule: rule, Count: count})
	}
	sort.Slice(topRules, func(i, j int) bool {
		if topRules[i].Count != topRules[j].Count {
			return topRules[i].Count > topRules[j].Count
		}
		return topRules[i].Rule < topRules[j].Rule // stable tie-break, not insertion order
	})
	if len(topRules) > topRulesLimit {
		topRules = topRules[:topRulesLimit]
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

	nowMinute := now / 60
	timeSeries := make([]TimeBucket, timeSeriesMinutes)
	for i := 0; i < timeSeriesMinutes; i++ {
		minute := nowMinute - int64(timeSeriesMinutes-1-i)
		idx := minute % timeSeriesMinutes
		if idx < 0 {
			idx += timeSeriesMinutes
		}
		byAction := make(map[Action]uint64, len(actionSlots))
		if s.minuteBucketTime[idx] == minute {
			for slot, a := range actionSlots {
				if c := s.minuteBuckets[idx][slot]; c > 0 {
					byAction[a] = c
				}
			}
		}
		timeSeries[i] = TimeBucket{Time: time.Unix(minute*60, 0).UTC(), ByAction: byAction}
	}

	return Stats{
		Total:           s.total,
		ByAction:        byAction,
		TopRules:        topRules,
		TimeSeries:      timeSeries,
		EventsPerSecond: float64(sum) / window,
		Capacity:        s.capacity,
		Count:           s.count,
		Window:          s.window,
	}
}
