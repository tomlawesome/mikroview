// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// TestMemoryCorpusReplayDoesNotStallIngest is issue #403's own required
// pin: "Concurrent replay + burst-rate ingest test/benchmark proves no
// ingest stall." It drives store.Store.Insert on one goroutine at
// mikroview's measured burst rate (~3,900 events/sec -- the same figure
// internal/detect/dispatch_bench_test.go and declarative_bench_test.go
// already cite as the ingest budget's basis) while MemoryCorpus.Replay
// runs repeatedly on another, and asserts the structural property that
// rules out an ingest stall: a single Replay pass makes more than one
// store.Query round trip, so no single lock hold (Insert and Query
// share one sync.RWMutex) can span the *whole* pass -- see corpus.go's
// own doc comment for why that is the right claim, and why it rejects
// the "Snapshot" alternative (one lock held for the entire ring).
//
// This is the test's third run at deciding pass/fail. The first two
// (#501, then this same test before this change) compared two
// wall-clock measurements taken on the same contended CI runner: max
// single-Insert latency during the concurrent phase against a bound
// derived from the median Replay pass duration measured in that same
// phase. #501 failed by 31 microseconds (0.07%) on a 42.9ms bound;
// after #501 widened the reference sample to a median of several
// passes, it failed again by 0.219ms (1.0%) on a ~21.8ms bound (issue
// #744). Both failures were the same defect: a same-run comparison
// still compares two noisy measurements, and widening the sample
// narrows the noise without ever removing it.
//
// Percentiles were tried and rejected along the way: this test drives
// Insert from one goroutine, strictly sequentially, so a genuine
// full-pass stall (Replay holding one lock across its entire pass)
// blocks whichever single Insert call happens to be in flight, not a
// bulk share of the run's samples -- a p99 cutoff discards exactly that
// sample along with genuine scheduler noise. Proven by deliberately
// reintroducing the stall (corpus.go temporarily snapshotting the whole
// ring instead of paging): max Insert latency hit 107.9ms against a
// 51ms bound, a clear catch -- but p99 stayed at 15.57µs, which would
// have silently passed.
//
// #744's resolution (see the issue's own notes) is to stop comparing
// timings altogether and assert on something deterministic instead:
// MemoryCorpus.Replay's own afterReplayPageForTest hook (corpus.go)
// already lets a test count Query round trips per pass without
// depending on wall-clock timing -- the same technique
// internal/store's queryScanHook and
// TestQueryBeforeIDScanCostDoesNotGrowWithDepth already use for the
// identical reason. With a 20,000+ event seeded corpus and the
// production corpusPageSize (5000), every completed pass makes at
// least 4 round trips if Replay pages as designed, and exactly 1 if it
// ever regresses to a Snapshot-style single Query/lock covering the
// whole ring -- so "more than one round trip per pass" is a
// deterministic, runner-load-independent stand-in for "no single lock
// hold spans the whole pass," which is the property that actually rules
// out an ingest stall.
//
// The latency measurements this test used to fail on are kept and
// logged -- they're still useful evidence if a change makes ingest
// meaningfully slower under replay contention -- but they no longer
// decide pass/fail, per the issue's "do not fix it by widening the
// bound" instruction: the bound wasn't wrong, comparing two
// same-run measurements to each other was.
func TestMemoryCorpusReplayDoesNotStallIngest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency timing test in -short mode")
	}

	const (
		burstRate = 3900 // events/sec, see this test's own doc comment
		testDur   = 300 * time.Millisecond
	)
	insertInterval := time.Second / time.Duration(burstRate)

	s := store.New(50_000, time.Hour)

	// Seed a realistic corpus before either phase starts, so Replay has
	// real work to do (not an empty store) -- a few seconds' worth at
	// the burst rate.
	base := time.Now().Add(-time.Minute)
	for i := 0; i < 20_000; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Millisecond), fmt.Sprintf("10.9.%d.%d", i/250, i%250)))
	}

	corpus := NewMemoryCorpus(s)

	// pagesThisPass is only ever touched by the replay goroutine below:
	// afterReplayPageForTest runs synchronously inside corpus.Replay,
	// which that same goroutine calls and drains (reads + resets the
	// counter) between calls, so no additional synchronization is needed
	// for this counter specifically (contrast replayPassDurations/
	// replayPassPages below, which the main goroutine also reads after
	// close(stop) and so do need passMu).
	var pagesThisPass int
	afterReplayPageForTest = func() { pagesThisPass++ }
	defer func() { afterReplayPageForTest = nil }()

	stop := make(chan struct{})
	replayDone := make(chan struct{})
	var passMu sync.Mutex
	var replayPassDurations []time.Duration // filled only from *contended* passes -- kept for logging, see doc comment above
	var replayPassPages []int               // Query round trips per completed pass -- this is what decides pass/fail now
	go func() {
		defer close(replayDone)
		for {
			select {
			case <-stop:
				return
			default:
				passStart := time.Now()
				pagesThisPass = 0
				corpus.Replay(func(store.Event) {})
				elapsed := time.Since(passStart)
				passMu.Lock()
				replayPassDurations = append(replayPassDurations, elapsed)
				replayPassPages = append(replayPassPages, pagesThisPass)
				passMu.Unlock()
			}
		}
	}()

	var maxInsertLatency time.Duration
	var insertCount int
	deadline := time.Now().Add(testDur)
	next := time.Now()
	for time.Now().Before(deadline) {
		if until := time.Until(next); until > 0 {
			time.Sleep(until)
		}
		start := time.Now()
		s.Insert(corpusEvent(start, "10.8.0.1"))
		elapsed := time.Since(start)
		if elapsed > maxInsertLatency {
			maxInsertLatency = elapsed
		}
		insertCount++
		next = next.Add(insertInterval)
	}

	close(stop)
	<-replayDone

	passMu.Lock()
	passes := replayPassDurations
	pageCounts := replayPassPages
	passMu.Unlock()
	if len(passes) == 0 {
		// Only reachable if a single contended Replay pass took longer
		// than the whole testDur -- would mean the corpus/testDur sizing
		// is wrong for this environment, not that the property held or
		// failed to hold. Fail loudly rather than silently pass with no
		// pass to check pageCounts against.
		t.Fatalf("no full Replay pass completed during the %s concurrent-ingest window -- corpus or testDur needs adjusting for this environment", testDur)
	}
	sort.Slice(passes, func(i, j int) bool { return passes[i] < passes[j] })
	medianReplayDuration := passes[len(passes)/2]

	// Informational only, per #744: kept and logged because it's still
	// useful evidence of a regression, but it no longer decides pass/fail
	// -- see this test's own doc comment for why comparing it to a bound
	// derived from another same-run measurement was the defect, not the
	// bound's value.
	t.Logf("%d full Replay passes completed under the same contention as ingest; median pass = %s; inserted %d events over %s (target rate %d/s); max single Insert latency = %s (%.1f%% of median pass)",
		len(passes), medianReplayDuration, insertCount, testDur, burstRate, maxInsertLatency, 100*float64(maxInsertLatency)/float64(medianReplayDuration))

	// The deciding assertion: every completed pass made more than one
	// Query round trip. The seeded corpus (20,000+ events) is well over
	// 4x corpusPageSize (5000), so a Replay that pages as designed always
	// makes at least 4 round trips; a Replay that regressed to holding
	// one lock over the whole ring (the "Snapshot" design corpus.go's own
	// doc comment rejects) would make exactly 1. This is deterministic --
	// it depends on corpus size and page size, not on runner load -- so
	// it fails for the actual reason this test exists instead of a
	// margin against another timing sample.
	for i, pages := range pageCounts {
		if pages <= 1 {
			t.Fatalf("pass %d made %d store.Query round trip(s), want >1 -- a single round trip means Replay read (and held its lock over) the whole corpus in one call, the single-lock-spans-the-pass failure mode this test exists to rule out (see corpus.go's Snapshot-vs-iterate doc comment)",
				i, pages)
		}
	}
}

// BenchmarkMemoryCorpusReplay reports MemoryCorpus.Replay's own cost in
// isolation (no concurrent ingest) at a realistic default-capacity
// corpus size, for comparison against
// TestMemoryCorpusReplayDoesNotStallIngest's concurrent-ingest numbers.
func BenchmarkMemoryCorpusReplay(b *testing.B) {
	s := store.New(200_000, time.Hour)
	base := time.Now().Add(-6 * time.Minute)
	for i := 0; i < 200_000; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*2*time.Millisecond), fmt.Sprintf("10.7.%d.%d", i/250, i%250)))
	}
	corpus := NewMemoryCorpus(s)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		corpus.Replay(func(store.Event) {})
	}
}

// BenchmarkMemoryCorpusReplayManyPages shrinks corpusPageSize far below
// its production default against a large corpus, forcing many pagination
// round trips -- the shape issue #759 exists about: a per-page cost that
// grows with page number (a rescan-from-newest cursor) rather than
// staying flat (an O(1)-resume cursor). Replay's own public behavior
// (call Replay, get a CorpusWindow) is identical whichever cursor
// corpus.go pages with internally, so this benchmark's numbers are
// directly comparable before and after that internal change without
// needing two code paths side by side: a per-page cost that grows with
// page number shows up here as the whole pass's cost growing much faster
// than corpus size / page size alone would predict.
func BenchmarkMemoryCorpusReplayManyPages(b *testing.B) {
	origPageSize := corpusPageSize
	corpusPageSize = 100
	defer func() { corpusPageSize = origPageSize }()

	const n = 50_000
	s := store.New(n, time.Hour)
	base := time.Now().Add(-10 * time.Minute)
	for i := 0; i < n; i++ {
		s.Insert(corpusEvent(base.Add(time.Duration(i)*time.Microsecond), fmt.Sprintf("10.6.%d.%d", i/250, i%250)))
	}
	corpus := NewMemoryCorpus(s)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		corpus.Replay(func(store.Event) {})
	}
}
