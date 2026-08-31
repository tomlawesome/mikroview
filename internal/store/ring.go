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
	"strconv"
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
//
// Every Action constant must appear here, and ActionUnknown must stay
// last: actionSlot falls back to the final slot for anything it does not
// recognise, so an action missing from this list would be silently
// counted as unknown in the time series while Stats.ByAction (a map)
// reported it correctly -- the two views of the same events disagreeing.
var actionSlots = [...]Action{
	ActionAccept, ActionDrop, ActionReject, ActionLog,
	ActionMarked, ActionNatted,
	ActionUnknown,
}

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
// that minute rather than listing every slot every time.
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
	// OldestHeld is the receipt time of the oldest event the ring still
	// holds, or the zero time when it holds none. It is deliberately not
	// the same thing as a query's WindowStart: that is now-minus-window,
	// the retention the operator configured, while this is how far back
	// the buffer actually reaches after capacity eviction has had its
	// say. On a busy day the two are far apart, and only this one can
	// answer "how much history is really here" -- #703's span control
	// offers a span from this and would otherwise claim a day of quiet
	// that was really an evicted buffer.
	OldestHeld time.Time `json:"oldestHeld"`
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

	// The ring's own oldest currently-held event, by the same indexing
	// hourTops uses below: index 0 while the buffer has not filled yet,
	// or s.head once it has, that being the next slot Insert overwrites
	// and so the oldest survivor. Zero time when nothing is held, which
	// is a real answer -- an empty buffer reaches back no distance at
	// all -- and not a missing one.
	var oldestHeld time.Time
	if s.count > 0 {
		oldestIdx := 0
		if s.count == s.capacity {
			oldestIdx = s.head
		}
		oldestHeld = s.buf[oldestIdx].ReceivedAt
	}

	out := Stats{
		Total:           s.total,
		ByAction:        byAction,
		TimeSeries:      timeSeries,
		EventsPerSecond: s.eventsPerSecondLocked(),
		Capacity:        s.capacity,
		Count:           s.count,
		Window:          s.window,
		OldestHeld:      oldestHeld,
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

// HourTop is one axis minute's top talker and top port (#644 round 21's
// table columns), computed from only the events the ring buffer
// currently holds -- never a persisted per-minute counter, and never
// the client's own capped tail. TimeBucket carries byAction alone for
// exactly this reason: a busy hour that has partially evicted deserves
// an honest "don't know," not a plausible-looking undercount.
type HourTop struct {
	Time time.Time `json:"time"`
	// Talker/Port are the winning source (SrcHostName when configured,
	// else the raw address -- lib/whisperStats.ts's topTalker uses this
	// same fallback over the client's own buffer) and destination port
	// (falling back to the source port when there is no destination one,
	// again matching whisperStats.ts's topPort) for this minute. Both
	// empty whenever Complete is false, or the minute genuinely held
	// nothing to count (e.g. ICMP-only traffic has no port).
	Talker string `json:"talker,omitempty"`
	Port   string `json:"port,omitempty"`
	// Complete is false once eviction has reached into this minute --
	// the buffer no longer holds every event it received in that
	// window, so Talker/Port are left blank rather than answering from
	// whatever fragment survived. See Store.HourTops.
	Complete bool `json:"complete"`
}

// HourTops answers #644 round 21's top-port/top-talker table columns:
// the same 60-minute axis Stats().TimeSeries covers (independently
// computed -- the two are read by different endpoints on different
// cadences, see handleStatsTops), each minute's winning source address
// and destination port, honest about which minutes it can actually
// answer for.
//
// A minute is Complete only once the ring's oldest currently-held event
// (O(1) -- see Insert's own head/count bookkeeping) predates that
// minute's start: since Insert evicts strictly oldest-first and
// ReceivedAt is monotonic with insertion order (single ingest
// goroutine), that is the exact point past which nothing from the
// minute could have been evicted. A minute that fails this check is
// left blank even if the buffer still holds some of its events --
// counting only the survivors would silently undercount, which is the
// dishonesty #644 asked this to avoid.
//
// The scan below walks backward from the newest event exactly like
// Query does, and stops the moment it passes the axis's start rather
// than touching the whole buffer -- bounded by how many events arrived
// in the last hour, not by capacity, and the whole tally happens under
// one RLock rather than copying first: Query already accepts the same
// trade-off (see its own doc comment measuring ~60ms at 50,000 events),
// and a linear tally here is cheaper per event than Query's per-event
// filter matching.
func (s *Store) HourTops() []HourTop {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	nowMinute := now.Unix() / 60
	axisStart := nowMinute - int64(timeSeriesMinutes-1)

	out := make([]HourTop, timeSeriesMinutes)
	for i := range out {
		out[i] = HourTop{Time: time.Unix((axisStart+int64(i))*60, 0).UTC(), Complete: true}
	}

	if s.count == 0 {
		return out
	}

	// The ring's own oldest currently-held event: index 0 while the
	// buffer hasn't filled yet (Insert has only ever appended), or
	// s.head once it has (the next slot Insert will overwrite is the
	// oldest surviving one) -- the same indexing Insert's own doc
	// comment relies on.
	oldestIdx := 0
	if s.count == s.capacity {
		oldestIdx = s.head
	}
	oldestReceived := s.buf[oldestIdx].ReceivedAt
	for i := range out {
		if oldestReceived.After(out[i].Time) {
			out[i].Complete = false
		}
	}

	talkerCounts := make([]map[string]uint64, timeSeriesMinutes)
	portCounts := make([]map[string]uint64, timeSeriesMinutes)
	hourStart := time.Unix(axisStart*60, 0)

	idx := s.head - 1
	if idx < 0 {
		idx = s.capacity - 1
	}
	for i := 0; i < s.count; i++ {
		e := s.buf[idx]
		idx--
		if idx < 0 {
			idx = s.capacity - 1
		}
		if e.ReceivedAt.Before(hourStart) {
			break
		}
		minuteIdx := int(e.ReceivedAt.Unix()/60 - axisStart)
		if minuteIdx < 0 || minuteIdx >= timeSeriesMinutes || !out[minuteIdx].Complete {
			// Out of range, or a minute already known incomplete --
			// no point tallying an answer that will be discarded.
			continue
		}
		if talker := talkerKey(e); talker != "" {
			if talkerCounts[minuteIdx] == nil {
				talkerCounts[minuteIdx] = make(map[string]uint64)
			}
			talkerCounts[minuteIdx][talker]++
		}
		if port := portKey(e); port != "" {
			if portCounts[minuteIdx] == nil {
				portCounts[minuteIdx] = make(map[string]uint64)
			}
			portCounts[minuteIdx][port]++
		}
	}

	for i := range out {
		if !out[i].Complete {
			continue
		}
		out[i].Talker = topOf(talkerCounts[i])
		out[i].Port = topOf(portCounts[i])
	}

	return out
}

// talkerKey is the top-talker identity HourTops counts by: SrcHostName
// when configured, else the raw source address -- mirrors
// lib/whisperStats.ts's topTalker exactly, so the table's server-
// computed column and the whisper's client-computed one never disagree
// about what "top talker" means, only about which events back the
// answer.
func talkerKey(e Event) string {
	if e.SrcHostName != "" {
		return e.SrcHostName
	}
	return e.SrcIP
}

// portKey mirrors lib/whisperStats.ts's topPort: the destination port,
// falling back to the source port when there is no destination one.
func portKey(e Event) string {
	if e.DstPort != 0 {
		return strconv.Itoa(e.DstPort)
	}
	if e.SrcPort != 0 {
		return strconv.Itoa(e.SrcPort)
	}
	return ""
}

// topOf picks counts' highest-count key, ties broken by the lower label
// so the result never depends on Go's randomised map iteration order --
// the same reasoning shedRuleLabelsLocked's sort documents, just as a
// running max instead of a full sort since only the winner is wanted.
// "" (no entries) for an empty map, which HourTops leaves as the
// column's honest empty value.
func topOf(counts map[string]uint64) string {
	best := ""
	var bestCount uint64
	for label, count := range counts {
		if count > bestCount || (count == bestCount && label < best) {
			best = label
			bestCount = count
		}
	}
	return best
}
