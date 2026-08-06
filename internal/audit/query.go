package audit

import "time"

// defaultLimit/maxLimit mirror internal/store/query.go's own constants --
// same "generous default, hard ceiling" convention, scaled down since an
// audit log fills far more slowly than the raw event stream.
const (
	defaultLimit = 200
	maxLimit     = 2000
)

// Query selects a windowed slice of the audit log -- the same
// Since/Until/Limit windowed-query shape GET /api/events' store.Query
// already establishes (see internal/store/query.go), minus the event-
// specific filters (device, action, IP, ...) this log has no equivalent
// of.
type Query struct {
	// Since/Until bound the entries returned by Timestamp -- zero value
	// for either means "no bound on that side", same convention
	// store.Query's own Since/Until fields use.
	Since time.Time
	Until time.Time
	Limit int
}

// Result is the response to a Query -- same HasMore-signals-truncation
// shape as store.Result, so a caller can tell "that's everything in
// range" apart from "there's more, ask again with a narrower window."
type Result struct {
	Entries []Entry `json:"entries"`
	HasMore bool    `json:"hasMore"`
}

// Query returns entries matching q, newest-first internally while
// scanning but reversed to oldest-first in the response -- same
// walk-backward-then-reverse approach store.Store.Query uses, for the
// same reason: insertion order tracks receipt order, so a linear
// backward scan can stop as soon as it passes Since or fills Limit,
// without needing to touch every entry ever recorded.
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

	matched := make([]Entry, 0, min(limit, len(s.entries)))
	hasMore := false
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if !q.Since.IsZero() && e.Timestamp.Before(q.Since) {
			break
		}
		// Until can't break the scan the way Since does -- walking
		// newest-to-oldest, an entry past Until is skipped, not a signal
		// that everything older is also out of range (mirrors
		// store.Query's identical Until handling).
		if !q.Until.IsZero() && e.Timestamp.After(q.Until) {
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
	return Result{Entries: matched, HasMore: hasMore}
}
