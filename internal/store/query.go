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
	Rule      string // substring match (or regex, if RuleRegex) against the rule label / raw line
	RuleRegex bool
	Since     time.Time
	SinceID   uint64 // only return events with ID > SinceID
	Limit     int
}

// Result is the response to a Query.
type Result struct {
	Events      []Event   `json:"events"`
	HasMore     bool      `json:"hasMore"`
	WindowStart time.Time `json:"windowStart"`
	ServerTime  time.Time `json:"serverTime"`
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
// extra bookkeeping.
//
// The window boundary is checked against Event.ReceivedAt, not Event.Time:
// ReceivedAt is the server's own receipt clock and is guaranteed monotonic
// with insertion order (single ingest goroutine). Event.Time is the
// RouterOS device's self-reported clock, which is not guaranteed monotonic
// across devices (or even a single device whose clock jumps) — breaking on
// it would let one stale-clocked event truncate the scan early and silently
// drop older-but-still-in-window events.
func (s *Store) Query(q Query) Result {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = defaultLimit
	case limit > maxLimit:
		limit = maxLimit
	}

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

	matched := make([]Event, 0, min(limit, s.count))
	hasMore := false

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

		if e.ID <= q.SinceID {
			break
		}
		if e.ReceivedAt.Before(windowStart) {
			break
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

	return Result{Events: matched, HasMore: hasMore, WindowStart: windowStart, ServerTime: now}
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
