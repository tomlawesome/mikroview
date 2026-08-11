// SPDX-License-Identifier: AGPL-3.0-only

// Package store holds the in-memory, time-windowed event buffer. There is
// no database: a fixed-capacity ring buffer is the hard memory ceiling,
// and the retention window is enforced at query time rather than via a
// periodic eviction scan.
package store

import (
	"cmp"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
)

// topRulesLimit caps how many entries Stats.TopRules returns -- a
// leaderboard, not a full dump of every rule label ever seen.
const topRulesLimit = 10

// maxRuleLabels bounds totalByRule, whose keys are RouterOS log-prefixes
// arriving on unauthenticated syslog and therefore chosen by whoever is
// sending.
//
// It had no cap at all. internal/rules.Store already capped the
// identical string at 20,000 with a comment about exactly this flood,
// and totalByRule -- bumped from the same main.go line, one statement
// away -- was missed. Measured on the uncapped code: 500,000 distinct
// labels cost +161.2 MB of heap and took 2.2s and 57 MB of syslog to
// produce, and TopRules stayed poisoned for the process's lifetime
// because nothing in this package ever evicted.
//
// Matched to internal/rules deliberately: the same event populates both,
// so a value that is generous for one is generous for the other, and a
// mismatch would mean one silently holding labels the other had shed.
// A var rather than a const so tests can shrink it. See #285.
var maxRuleLabels = 20_000

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
		if _, known := s.totalByRule[e.RuleLabel]; !known && len(s.totalByRule) >= maxRuleLabels {
			s.shedRuleLabelsLocked()
		}
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

// shedRuleLabelsLocked drops the lowest-count rule labels once
// totalByRule is full, down to a batch below the cap so the next several
// thousand new labels do not each pay for another shed -- the same
// amortisation internal/evict documents.
//
// Lowest count first, rather than least-recently-seen: totalByRule holds
// no timestamps, and count is the better signal anyway. A genuine rule
// is one an operator configured on the router, so it fires repeatedly; a
// flood of minted labels is a long tail of ones. The trade-off is that a
// genuinely new rule seen for the first time during a flood can be shed
// before it accumulates -- acceptable for what is a stats leaderboard,
// and it is not detection state.
func (s *Store) shedRuleLabelsLocked() {
	type labelCount struct {
		label string
		count uint64
	}
	all := make([]labelCount, 0, len(s.totalByRule))
	for label, count := range s.totalByRule {
		all = append(all, labelCount{label: label, count: count})
	}
	slices.SortFunc(all, func(a, b labelCount) int {
		if a.count != b.count {
			return cmp.Compare(a.count, b.count)
		}
		return cmp.Compare(a.label, b.label) // deterministic, so a shed is reproducible in tests
	})

	remove := len(all) - evict.Target(maxRuleLabels)
	for i := 0; i < remove; i++ {
		delete(s.totalByRule, all[i].label)
	}
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
//
// Everything that reads shared state happens under the read lock;
// sorting the rule leaderboard deliberately does not. Sorting under
// RLock blocks Insert's Lock() for the whole sort, and Insert is the
// ingest goroutine -- so an expensive Stats() stalls ingestion of real
// router traffic. That was measured at 306 ms per Stats() call with a
// 301 ms concurrent Insert block; the label cap (maxRuleLabels) bounds
// the size, and doing the sort on the caller's own copy after unlocking
// means ingest never waits on it at all. /api/stats is polled every 5s
// per open browser tab and is reachable with a read-only token, so this
// is a cheap call to make often.
func (s *Store) Stats() Stats {
	s.mu.RLock()

	byAction := make(map[Action]uint64, len(s.totalByAction))
	for k, v := range s.totalByAction {
		byAction[k] = v
	}

	topRules := make([]RuleCount, 0, len(s.totalByRule))
	for rule, count := range s.totalByRule {
		topRules = append(topRules, RuleCount{Rule: rule, Count: count})
	}

	now := time.Now().Unix()
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

	out := Stats{
		Total:           s.total,
		ByAction:        byAction,
		TimeSeries:      timeSeries,
		EventsPerSecond: s.eventsPerSecondLocked(),
		Capacity:        s.capacity,
		Count:           s.count,
		Window:          s.window,
	}
	s.mu.RUnlock()

	// topRules is this call's own copy by now, so sorting it touches no
	// shared state and holds nothing Insert needs. See the doc comment.
	sort.Slice(topRules, func(i, j int) bool {
		if topRules[i].Count != topRules[j].Count {
			return topRules[i].Count > topRules[j].Count
		}
		return topRules[i].Rule < topRules[j].Rule // stable tie-break, not insertion order
	})
	if len(topRules) > topRulesLimit {
		topRules = topRules[:topRulesLimit]
	}
	out.TopRules = topRules
	return out
}

// EventsPerSecond reports the same rolling-10-second rate as
// Stats().EventsPerSecond, without building the byAction map, sorting
// TopRules, or constructing TimeSeries's 60 per-minute buckets -- wasted
// work for a caller that only ever reads this one field. Measured at
// ~124ns/0 allocs against Stats()'s ~17-19us/68 allocs for the same
// number, on the global-spike ticker (main.go), which runs every 10s and
// reads nothing else from the result.
func (s *Store) EventsPerSecond() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventsPerSecondLocked()
}

func (s *Store) eventsPerSecondLocked() float64 {
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
	return float64(sum) / window
}
