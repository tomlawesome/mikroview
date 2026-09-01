// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// Corpus is what a replay reads events from -- never internal/store's
// ring directly. Issue #403's own words are the reason: "an interface,
// NOT the ring: one in-memory implementation today reading
// internal/store's ring; a retention-backed one later must be a new
// implementation, not a caller rewrite. No caller may reference the
// store ring directly." A Replayable definition's Replay method (see
// replayability.go) takes a Corpus, never a *store.Store, so swapping
// MemoryCorpus for a future retention-backed corpus (docs/decisions/
// evaluation-engine.md's open question 2: "longer on-disk event
// retention is an anticipated future decision, not a rejected one")
// touches exactly one construction site, not every Replay call.
//
// Deployment reality every implementation of this interface must be
// honest about: the corpus is whatever this process currently has, and
// for the in-memory implementation that is short -- but how short is
// set by what the operator's router is told to log, which moves it by
// two orders of magnitude. Two measurements from one deployment,
// recorded 2026-08-16 in docs/routeros-setup.md's "Recommended logging
// posture" section: ~594 events/sec while two broad accept rules logged
// established traffic, and ~12-14 events/sec across the days after that
// logging was removed. So the default 120MiB event ring (~201,649
// events, see internal/config.assumedBytesPerEvent) holds roughly 4-6
// minutes under the noisy posture and about four hours under the
// recommended one -- hours at best, never days. Both are dated
// measurements, not constants: check the setup guide before relying on
// either. A Receipt
// states exactly the window it covered (see Window) so a caller never
// has to guess this; nothing in this package rounds a five-minute
// sample up to a daily estimate.
type Corpus interface {
	// Replay visits every event currently available, oldest first,
	// calling visit once per event, and reports the window actually
	// covered -- see CorpusWindow. Events reach visit in non-decreasing
	// ReceivedAt order (ties broken by ID) because every definition's
	// own replay logic (threshold-over-window counting in particular)
	// assumes the same forward-chronological order live evaluation
	// always sees.
	//
	// Replay must never hold a lock (or otherwise block) across the
	// whole pass: an implementation backed by a mutable, concurrently
	// written source reads it through short, independently-acquired
	// leases, never one continuous hold for however long the full
	// corpus takes to walk -- see MemoryCorpus's own doc comment for
	// why, and TestMemoryCorpusReplayDoesNotStallIngest for the pinned
	// proof.
	Replay(visit func(store.Event)) CorpusWindow
}

// CorpusWindow is what one Corpus.Replay call actually observed: the raw
// material a Receipt's Window (replay.go) is built from for a
// definition whose corpus turned out long enough, or that a Decline is
// built from for one whose corpus did not. Deliberately a different,
// looser type than Window -- Start/End/Count can be the zero
// time.Time/0 (an empty corpus is a valid, common state: a freshly
// started process, or a deployment with no traffic yet), where Window
// exists specifically to make that state unconstructable. See Window's
// own doc comment.
type CorpusWindow struct {
	// Start/End are the earliest/latest ReceivedAt among the events
	// actually visited -- not a pre-declared target window, so a corpus
	// shorter than requested (or a retention/eviction race against
	// concurrent ingest -- see MemoryCorpus.Replay) is reported exactly
	// as what was actually seen, never as what was hoped for.
	Start, End time.Time
	// Count is how many events were visited.
	Count int
	// Truncated reports whether this Corpus's own bounded-read policy
	// (see maxCorpusEvents) stopped the pass before every available
	// event was visited -- distinct from a definition's own Receipt
	// declining for being shorter than its window (replay.go's
	// Decline): a corpus can be simultaneously truncated (more history
	// existed than this call was willing to hold) and still long enough
	// to satisfy a given definition's window.
	Truncated bool
}

// corpusPageSize is how many events MemoryCorpus.Replay requests per
// underlying store.Store.Query call. Matches store.Query's own maxLimit
// clamp (internal/store/query.go's clampLimit) so one page requests the
// largest batch Query will honor, keeping the number of round trips --
// and so the number of independent RLock acquire/release cycles a large
// corpus costs -- as small as the store's own public API allows,
// without importing that unexported constant across the package
// boundary (store deliberately does not export it: see clampLimit's own
// doc comment on internal/audit/query.go's identical helper).
//
// A var, not a const, so a test can shrink it to exercise multi-page
// pagination (the BeforeID cursor, and reassembly into chronological
// order) without needing thousands of events -- same convention as
// queueSize/maxTrackedKeys elsewhere in this package.
var corpusPageSize = 5000

// maxCorpusEvents bounds how many events a single Replay call will ever
// hold in memory or evaluate, independent of how large an operator's
// own -max-memory ring happens to be configured. The default 120MiB
// ring holds ~201,649 events (see this file's package-level doc
// comment); this is set generously above that so a default deployment's
// replay is never truncated by this bound at all, while still capping a
// single Replay call's worst-case memory (a few hundred MB of copied
// store.Event values, at ~624 bytes/event) and pagination cost (a few
// hundred Query round trips, at corpusPageSize per page) to a known
// ceiling for a deployment that configured a much larger ring, rather
// than letting a single replay call's cost scale unbounded with however
// large maxMemory happens to be set. See CorpusWindow.Truncated.
//
// A var for the same test-shrinking reason as corpusPageSize above.
var maxCorpusEvents = 1_000_000

// MemoryCorpus is Corpus's in-memory implementation (issue #403's "one
// in-memory implementation today"): it reads events out of a live
// *store.Store -- the same ring ingest is writing to -- through that
// store's existing, public Query method, never through anything
// internal to store.Store.
//
// The choice this type embodies, stated once here because it shapes
// every method on it: internal/store/query.go's own Query already
// documents the risk directly -- "Query holds s.mu.RLock() for the
// whole scan, which blocks Insert... for the same duration" -- and
// bounds that per-call cost with two existing, public safeguards
// (maxScannedPerQuery, and Limit clamped to at most 5000). Two designs
// were available once that was understood:
//
//   - Snapshot: add a new method to store.Store that copies the entire
//     ring buffer under one lock. Rejected: it would be a brand new,
//     unbounded-duration single lock hold whose cost scales with
//     however large the ring is configured (a 10GiB maxMemory ring
//     holds millions of events) -- precisely the "a replay holding a
//     read lock is an ingest stall wearing a different hat" failure
//     mode issue #403 exists to rule out, and it would require store to
//     expose ring internals to a caller outside the package, which
//     "no caller may reference the store ring directly" forbids anyway.
//   - Iterate: call the existing, public Query repeatedly, each call
//     independently bounded and briefly holding/releasing s.mu.RLock(),
//     assembling the full corpus across many short leases instead of
//     one long one. This is what MemoryCorpus.Replay does.
//
// Iterating is also what Go's own sync.RWMutex makes safe against
// starving Insert even under a tight call-Query-immediately-again loop:
// the RWMutex documentation guarantees that once a blocked Lock() call
// (Insert) is waiting, no further RLock() call is granted until that
// writer has run -- so a fast, repeated Query loop cannot indefinitely
// starve a concurrent Insert; it can only ever delay one Insert by at
// most one page's worth of scan time. TestMemoryCorpusReplayDoesNotStallIngest
// pins that this holds in practice, not merely in theory, at the
// measured ~3,900 events/sec burst rate (internal/detect/dispatch_bench_test.go's
// own figure).
//
// Query's own backward-from-newest design (walking from "now" towards
// the past, per its own doc comment) means the first page this type
// reads back is always the corpus's newest slice, not its oldest --
// Replay pages backward via Query's BeforeID cursor (issue #759), each
// page's exclusive upper bound being the previous page's own oldest
// event ID, then sorts everything it collected into forward-
// chronological order before handing events to the caller's visit
// function, exactly once each.
//
// The cursor is an ID, not a timestamp, because of what that costs per
// page. Before #759, this paged with a descending Until cursor instead:
// Until is an inclusive bound, so it only *narrows what a page keeps* --
// Query still has to walk from the newest held event on every call, so
// each successive page repaid the scan cost of every page already
// collected, and the *last* page of a multi-page pass (the one that
// finally reaches the oldest held event) ended up scanning close to the
// whole corpus in one RLock hold: precisely the single-lock-spans-the-
// pass failure mode this type exists to rule out, just reached via
// pagination instead of a single Snapshot call. Confirmed by
// instrumenting per-page Query durations: a final, nearly-empty page
// routinely cost as much as an earlier page that returned a full 5,000
// events. BeforeID fixes this by moving where the scan *starts*, not
// just what it keeps: Query resumes at the cursor's own ring position in
// O(1) (see BeforeID's doc comment in internal/store), so each page only
// ever examines that page's own share of the corpus, and pages never
// overlap -- no per-page dedup is needed (contrast the old Until-based
// version's seen map). See BenchmarkMemoryCorpusReplayManyPages for the
// measured before/after difference and
// TestStoreQueryBeforeIDScanCostDoesNotGrowWithDepth for the pinned
// per-page cost bound.
type MemoryCorpus struct {
	store *store.Store
}

// NewMemoryCorpus constructs a Corpus reading from s.
func NewMemoryCorpus(s *store.Store) *MemoryCorpus {
	return &MemoryCorpus{store: s}
}

// Replay satisfies Corpus. See MemoryCorpus's own doc comment for the
// snapshot-vs-iterate reasoning and the concurrency guarantee this
// method is built to uphold.
func (c *MemoryCorpus) Replay(visit func(store.Event)) CorpusWindow {
	var all []store.Event

	until := time.Now() // fixed once, for the first page only, so the
	// pass never grows to include events inserted after this call
	// started -- a replay always answers against a stable, well-defined
	// upper bound, not a moving target that depends on how long the pass
	// itself takes. Later pages don't use this: they cursor off the
	// previous page's own oldest event ID instead (beforeID below), an
	// exclusive, gap-free bound Until can't give per-page (see this
	// type's own doc comment for why that matters for per-page cost).
	var beforeID uint64
	truncated := false

	for {
		q := store.Query{Limit: corpusPageSize}
		if beforeID != 0 {
			q.BeforeID = beforeID
		} else {
			q.Until = until
		}
		res := c.store.Query(q)
		if len(res.Events) == 0 {
			break
		}

		// No dedup needed: BeforeID is an exclusive bound derived from
		// the previous page's own oldest ID, so pages never overlap
		// (contrast the old Until-based version, which needed a seen
		// map because Until is inclusive).
		all = append(all, res.Events...)

		if len(all) >= maxCorpusEvents {
			truncated = true
			break
		}
		if !res.HasMore {
			break // reached the start of what's currently retained
		}
		// res.Events is oldest-first within the page (Query's own
		// contract); its first entry's ID becomes the next page's
		// exclusive upper bound, so the next call resumes exactly where
		// this one left off instead of re-scanning it. If that event
		// gets evicted before the next call runs, Query's BeforeID
		// handling (internal/store/query.go) treats it as having
		// reached the start of retained history -- an empty page with
		// HasMore false, the same terminal case as above, not an error.
		beforeID = res.Events[0].ID
	}

	sort.Slice(all, func(i, j int) bool {
		if !all[i].ReceivedAt.Equal(all[j].ReceivedAt) {
			return all[i].ReceivedAt.Before(all[j].ReceivedAt)
		}
		return all[i].ID < all[j].ID
	})
	if len(all) > maxCorpusEvents {
		// Only the early-break above could overshoot, and only by at
		// most one page's worth -- trim down to exactly the cap,
		// keeping the most recent events (the tail, since all is now
		// ascending) and dropping the oldest, mirroring the ring's own
		// eviction preference for what a bounded read keeps.
		all = all[len(all)-maxCorpusEvents:]
		truncated = true
	}

	var start, end time.Time
	if len(all) > 0 {
		start = all[0].ReceivedAt
		end = all[len(all)-1].ReceivedAt
	}
	for _, e := range all {
		visit(e)
	}

	return CorpusWindow{Start: start, End: end, Count: len(all), Truncated: truncated}
}
