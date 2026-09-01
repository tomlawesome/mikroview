// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"net"
	"regexp"
	"strings"
	"time"
)

const (
	defaultLimit = 500
	maxLimit     = 5000
)

// maxScannedPerQuery bounds how many ring entries a single Query call
// examines before giving up and returning what it has, even if neither
// Limit nor the time window stopped it first. Query holds s.mu.RLock()
// for the whole scan, which blocks Insert (the sole ingest writer) for
// the same duration -- a selective filter (e.g. an operator
// investigating an IP that turns out to be rare or absent) can force
// scanning the entire in-window buffer while matching almost nothing,
// measured at ~60ms at the default 120MiB/~204,700-event capacity, and
// growing linearly with store.maxMemory from there. 50,000 keeps that
// worst case to roughly a quarter of the default-capacity figure
// regardless of how large an operator's maxMemory is configured, while
// staying far above what a broad/unfiltered query ever needs to scan to
// fill its own Limit (measured to exit within a few hundred events
// regardless of capacity). HasMore reports true exactly as it already
// does when Limit truncates the scan -- callers already treat that as
// "there may be more, don't read a short result as complete." A var
// rather than a const so tests can shrink it.
var maxScannedPerQuery = 50_000

// queryScanHook, if non-nil, is called once per ring entry Query's scan
// loop examines, whether or not that entry ends up matched. Test-only
// instrumentation (nil in production, so the cost is one nil check per
// entry): it lets a test pin *how many* entries a Query call examines
// without depending on wall-clock timing, which #501 and #744 already
// showed is unreliable on a loaded CI runner -- see
// TestStoreQueryBeforeIDScanCostDoesNotGrowWithDepth.
var queryScanHook func()

// clampLimit maps a caller-supplied limit onto [1, maxLimit] -- see
// internal/audit/query.go's identical helper for why this is written as
// plain comparisons.
func clampLimit(requested int) int {
	if requested <= 0 {
		return defaultLimit
	}
	if requested > maxLimit {
		return maxLimit
	}
	return requested
}

// Scope restricts a query to only-internal or only-external addresses on
// one side of a connection. ScopeAny (the zero value) applies no
// restriction, so an unset Query field behaves exactly as before this was
// added.
type Scope string

const (
	ScopeAny      Scope = ""
	ScopeInternal Scope = "internal"
	ScopeExternal Scope = "external"
)

// Query describes a filtered, windowed read against the Store. Zero values
// mean "no filter" for that field.
type Query struct {
	Device    string
	Action    Action
	Protocol  string
	Chain     string
	Interface string
	IP        string // matches src or dst; accepts a bare IP or CIDR
	Port      int    // matches src or dst port
	SrcScope  Scope  // restrict SrcIP to only-internal or only-external
	DstScope  Scope  // restrict DstIP to only-internal or only-external
	Rule      string // substring match (or regex, if RuleRegex) against the rule label / raw line
	RuleRegex bool
	Since     time.Time
	// Until is an optional upper bound (issue #29) -- paired with Since
	// this lets a caller pull a bounded before/after window around a
	// timestamp (e.g. "what was this IP doing right before/after a
	// honeypot hit"), not just an open-ended tail from Since to now.
	// Zero value means no upper bound, same "unset means no filter"
	// convention as every other Query field.
	Until   time.Time
	SinceID uint64 // only return events with ID > SinceID
	// BeforeID is an optional resume cursor for backward pagination
	// (issue #759): if nonzero, only events with ID < BeforeID are
	// considered, and the scan starts at that event's own ring position
	// directly instead of always walking from the newest held event.
	// IDs are assigned sequentially by Insert and never reused, so that
	// position is one O(1) computation from BeforeID's offset behind the
	// newest held ID (s.nextID) -- unlike Until, which can only *skip*
	// events newer than the bound one at a time while still starting the
	// walk from the newest event every call, making a caller that pages
	// backward by re-issuing a narrower Until on each call pay a scan
	// cost that grows with how many pages it has already paged through
	// (see MemoryCorpus.Replay in internal/engine/corpus.go, the caller
	// this was added for, and its own doc comment for the measurement).
	// If BeforeID names an ID no longer held (evicted since it was
	// issued), the scan finds nothing older than it to return -- but
	// that is not the same fact as "there is nothing left in history,"
	// so it does not get folded into an ordinary empty/HasMore=false
	// result: see Result.CursorEvicted, which this case sets, for why
	// the distinction is a caller-visible one. Zero means unset, same
	// convention as every other Query field.
	BeforeID uint64
	Limit    int
}

// Result is the response to a Query.
type Result struct {
	Events  []Event `json:"events"`
	HasMore bool    `json:"hasMore"`
	// CursorEvicted reports whether a BeforeID cursor (see Query.BeforeID)
	// named an event this call could not find among what the ring
	// currently holds. Eviction only ever removes from the oldest end,
	// so this means everything from that event downward -- and, if the
	// page that produced this cursor itself had HasMore set, possibly
	// more history beyond what any page ever got to read -- is gone.
	// Always false for a query that left BeforeID unset, and for one
	// whose cursor was still held.
	//
	// This is deliberately a separate signal from HasMore=false: "I
	// walked to the end of retained history" and "the ring evicted the
	// rest while I was reading it" are different facts about what a
	// caller received, and collapsing them into one silent "no more
	// events" result is exactly the kind of unmarked gap this codebase
	// otherwise refuses to present as an ordinary empty outcome (compare
	// a declined Receipt, issue #730's "not observed" watch night). A
	// caller resuming a paged walk via BeforeID that sees this set must
	// treat the walk as possibly short, not complete -- see
	// MemoryCorpus.Replay's own handling, which sets
	// CorpusWindow.Truncated on it rather than reporting an ordinary
	// end-of-history stop.
	CursorEvicted bool      `json:"cursorEvicted"`
	WindowStart   time.Time `json:"windowStart"`
	ServerTime    time.Time `json:"serverTime"`
}

// Query returns the most recent events matching q, newest constrained to
// the store's retention window, oldest-first in the response.
//
// It walks the ring backward from the most recently inserted event rather
// than binary-searching the full buffer: since insertion order tracks
// receipt order, a linear backward scan can stop as soon as it passes the
// window/cursor boundary or fills the limit — for a sparse filter this
// still costs scanning every in-window event once (unavoidable), but it
// never touches events outside the window at all, which is the same
// asymptotic win a binary-searched window bound would give without the
// extra bookkeeping. maxScannedPerQuery caps that "scan every in-window
// event" cost regardless of how sparse the filter or how large the
// window -- see its own doc comment for why.
//
// The window boundary is checked against Event.ReceivedAt, not Event.Time:
// ReceivedAt is the server's own receipt clock and is guaranteed monotonic
// with insertion order (single ingest goroutine). Event.Time is the
// RouterOS device's self-reported clock, which is not guaranteed monotonic
// across devices (or even a single device whose clock jumps) — breaking on
// it would let one stale-clocked event truncate the scan early and silently
// drop older-but-still-in-window events.
//
// A "before/after a timestamp" lookback (issue #29 -- e.g. pulling what an
// IP was doing around an external signal like a honeypot hit) is just
// Since/Until set to a window centered on that timestamp; no separate mode
// is needed. WindowStart in the returned Result already reports the actual
// applied lower bound (clamped to the retention window if Since asked for
// more history than exists), so a caller comparing it against the Since it
// requested can tell whether "before" context was truncated by retention
// rather than being silently missing.
func (s *Store) Query(q Query) Result {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := clampLimit(q.Limit)

	now := time.Now()
	windowStart := now.Add(-s.window)
	if q.Since.After(windowStart) {
		windowStart = q.Since
	}

	var ipNet *net.IPNet
	var ip net.IP
	if q.IP != "" {
		if _, n, err := net.ParseCIDR(q.IP); err == nil {
			ipNet = n
		} else {
			ip = net.ParseIP(q.IP)
		}
	}

	// Compiled once here rather than per-event in matchesFilters. Go's
	// regexp uses RE2, which is immune to catastrophic backtracking, so an
	// arbitrary user-supplied pattern can't blow up scan time the way a
	// backtracking engine's could. An invalid pattern is treated as "no
	// rule filter" rather than an error -- likely means the user is still
	// mid-typing it.
	var ruleRe *regexp.Regexp
	if q.RuleRegex && q.Rule != "" {
		ruleRe, _ = regexp.Compile("(?i)" + q.Rule)
	}

	// Explicit comparisons, not min(): see internal/audit/query.go's
	// clampLimit for why the allocation bound is spelled out rather than
	// left for the reader (and the scanner) to infer.
	capacity := s.count
	if capacity > limit {
		capacity = limit
	}
	matched := make([]Event, 0, capacity)
	hasMore := false

	idx := s.head - 1
	if idx < 0 {
		idx = s.capacity - 1
	}
	// remaining is how many currently-held entries this call will walk,
	// starting from idx. Ordinarily that's every held entry (s.count),
	// but a BeforeID cursor (see its own doc comment) moves idx and
	// shrinks remaining in O(1) to start past whatever an earlier page
	// already covered, rather than reaching the same position by
	// skipping one entry at a time the way Until does.
	remaining := s.count
	cursorEvicted := false
	if q.BeforeID != 0 {
		oldestHeldID := uint64(0)
		if s.count > 0 {
			oldestHeldID = s.nextID - uint64(s.count) + 1
		}
		if s.count == 0 || q.BeforeID <= oldestHeldID {
			// Nothing currently held is old enough to match: BeforeID's
			// own event is not among what's held (eviction only removes
			// from the oldest end, so if BeforeID's event is gone,
			// everything older than it is gone too). Report that via
			// CursorEvicted rather than folding it into an ordinary
			// empty result -- see that field's own doc comment for why
			// a caller needs to be able to tell the two apart.
			remaining = 0
			cursorEvicted = true
		} else {
			startID := q.BeforeID - 1 // the newest ID this call may return
			if startID > s.nextID {
				startID = s.nextID // defensive: a cursor this store never issued
			}
			offset := int(s.nextID - startID)
			idx -= offset
			for idx < 0 {
				idx += s.capacity
			}
			remaining = s.count - offset
		}
	}
	for i := 0; i < remaining; i++ {
		if i >= maxScannedPerQuery {
			hasMore = true
			break
		}
		if queryScanHook != nil {
			queryScanHook()
		}
		e := s.buf[idx]
		idx--
		if idx < 0 {
			idx = s.capacity - 1
		}

		if e.ID <= q.SinceID {
			break
		}
		if e.ReceivedAt.Before(windowStart) {
			break
		}
		// Until can't break the scan the way windowStart/SinceID do -- we're
		// walking newest-to-oldest, so an event past Until is skipped, not
		// a signal that everything older is also out of range.
		if !q.Until.IsZero() && e.ReceivedAt.After(q.Until) {
			continue
		}
		if !matchesFilters(e, q, ipNet, ip, ruleRe) {
			continue
		}
		if len(matched) >= limit {
			hasMore = true
			break
		}
		matched = append(matched, e)
	}

	for l, r := 0, len(matched)-1; l < r; l, r = l+1, r-1 {
		matched[l], matched[r] = matched[r], matched[l]
	}

	return Result{Events: matched, HasMore: hasMore, CursorEvicted: cursorEvicted, WindowStart: windowStart, ServerTime: now}
}

func matchesFilters(e Event, q Query, ipNet *net.IPNet, ip net.IP, ruleRe *regexp.Regexp) bool {
	if q.Device != "" && e.DeviceID != q.Device {
		return false
	}
	if q.Action != "" && e.Action != q.Action {
		return false
	}
	if q.Protocol != "" && !strings.EqualFold(e.Protocol, q.Protocol) {
		return false
	}
	if q.Chain != "" && !strings.EqualFold(e.Chain, q.Chain) {
		return false
	}
	if q.Interface != "" && e.InInterface != q.Interface && e.OutInterface != q.Interface {
		return false
	}
	if ipNet != nil {
		src := net.ParseIP(e.SrcIP)
		dst := net.ParseIP(e.DstIP)
		if !(src != nil && ipNet.Contains(src)) && !(dst != nil && ipNet.Contains(dst)) {
			return false
		}
	} else if ip != nil {
		if !ip.Equal(net.ParseIP(e.SrcIP)) && !ip.Equal(net.ParseIP(e.DstIP)) {
			return false
		}
	} else if q.IP != "" {
		if e.SrcIP != q.IP && e.DstIP != q.IP {
			return false
		}
	}
	if q.Port != 0 && e.SrcPort != q.Port && e.DstPort != q.Port {
		return false
	}
	if !scopeMatches(q.SrcScope, e.SrcIP) || !scopeMatches(q.DstScope, e.DstIP) {
		return false
	}
	if q.Rule != "" {
		if q.RuleRegex {
			if ruleRe != nil && !ruleRe.MatchString(e.RuleLabel) && !ruleRe.MatchString(e.Raw) {
				return false
			}
		} else {
			needle := strings.ToLower(q.Rule)
			if !strings.Contains(strings.ToLower(e.RuleLabel), needle) && !strings.Contains(strings.ToLower(e.Raw), needle) {
				return false
			}
		}
	}
	return true
}

// scopeMatches reports whether addr satisfies scope. ScopeAny always
// matches (including an empty/unparseable addr, since most events don't
// have both a src and dst address -- e.g. ICMP has no ports but this
// applies equally to any address field). A specific scope excludes an
// address that can't be parsed at all, rather than guessing: an
// unclassifiable address shouldn't satisfy "only show me internal" or
// "only show me external."
func scopeMatches(scope Scope, addr string) bool {
	if scope == ScopeAny {
		return true
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	public := isPublicIP(ip)
	if scope == ScopeInternal {
		return !public
	}
	return public // ScopeExternal
}

// isPublicIP mirrors the same small check every other package that needs
// it (internal/geoip, internal/reputation, internal/detect) keeps its own
// copy of, rather than sharing one -- consistent with how this codebase
// already does it.
func isPublicIP(ip net.IP) bool {
	return !ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}
