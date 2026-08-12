// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"sync/atomic"
	"time"
)

// Limiter gates a log line that can repeat at a rate the *other* end
// controls -- a reconnecting router, a flood of rejected connections, a
// client that went away -- to at most one written line per interval.
// Occurrences in between are counted, not lost: Allow hands back the
// running total so the line that does get written carries what the
// window suppressed.
//
// This started as three identical hand-rolled copies (the syslog ingest
// queue, the detection queue, the watchlist evaluation queue -- see
// issue #322); any new log site whose trigger rate is externally
// controlled should use this rather than growing a fourth.
type Limiter struct {
	interval time.Duration
	last     atomic.Int64  // unix nanos of the last line actually written
	total    atomic.Uint64 // occurrences ever, suppressed or not

	// now is a test seam; nil means time.Now. Timing-based tests against
	// the real clock are exactly the flakiness this repo has already
	// been bitten by (the persistence debounce tests), so the tests set
	// this instead of sleeping.
	now func() time.Time
}

// NewLimiter returns a Limiter that lets one line through per interval.
func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval}
}

// Allow records one occurrence. ok reports whether the caller should
// write its line now -- the first occurrence always logs immediately;
// after that, at most one per interval. total is the occurrence count
// so far including this one: put it in the message, so the suppressed
// occurrences are visible in the next line rather than just gone.
//
// Callers that already keep their own counter (e.g. one a /api/stats
// field reads) can ignore total and keep using theirs -- the gate is
// the point, the count is a convenience.
func (l *Limiter) Allow() (total uint64, ok bool) {
	total = l.total.Add(1)
	nowFn := l.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UnixNano()
	last := l.last.Load()
	if now-last < int64(l.interval) {
		return total, false
	}
	// CAS so exactly one of several concurrent callers in the same
	// window writes the line -- the same shape all three hand-rolled
	// predecessors used, for the same race.
	return total, l.last.CompareAndSwap(last, now)
}
