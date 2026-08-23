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
// runs repeatedly on another, and asserts that no single Insert call
// was ever blocked for anywhere near as long as one full Replay pass
// over the whole corpus takes -- see corpus.go's own doc comment for
// why that is the right claim: Insert and Query still share one
// sync.RWMutex, so a concurrent Query legitimately delays an Insert by
// however long that one Query call takes; what must never happen is a
// single lock hold spanning the *whole* replay pass, which is exactly
// what a snapshot-the-whole-ring design would cost instead of the
// iterate-in-bounded-pages design MemoryCorpus.Replay actually uses.
//
// The bound is deliberately relative (a fraction of one full Replay
// pass's own measured duration), not a fixed absolute duration: an
// absolute millisecond figure is exactly the kind of thing that is fast
// on a quiet workstation and flakes under go test -race's
// instrumentation overhead (measured: a fixed 10ms bound held
// comfortably without -race but failed at ~23ms under it) or a loaded
// CI runner. Comparing against a duration measured in the same run,
// under the same instrumentation, self-calibrates away that variance
// while still proving the structural claim: a page read costs a small
// fraction of the whole pass, not something approaching it.
//
// Which "one full pass" duration the bound is a fraction of is the part
// issue #501 fixed. It used to be a single Replay pass measured
// *before* the concurrent phase started -- uncontended, run alone. That
// number was then compared against the *maximum* single-Insert latency
// measured *during* the concurrent phase -- contended, competing with a
// continuously-running Replay for the same lock and the same CPU. Both
// sides are wall-clock measurements on a shared CI runner, but only one
// of them experiences the runner's actual load during the window that
// matters: #501's own CI failure was a 42.938ms max Insert latency
// against a 42.907ms bound (derived from an 85.8ms uncontended
// pass measured moments earlier, on a PR that touched no Go code) --
// missed by 31 microseconds, 0.07%, because the reference sample and
// the measurement it was compared against were not equally loaded.
//
// Percentiles were tried first (discard the top ~1% of Insert samples
// rather than asserting on the raw max) and rejected after being
// disproved directly: this test drives Insert from one goroutine,
// strictly sequentially -- the same "sole ingest writer" model
// MemoryCorpus's own doc comment describes production as using -- so a
// genuine full-pass stall (Replay holding one lock across its entire
// pass, the "Snapshot" design corpus.go's own doc comment rejects)
// blocks whichever single Insert call happens to be in flight when the
// lock is taken, not a bulk share of the ~1,150 samples in the run. A
// p99 cutoff discards exactly that one sample along with genuine
// scheduler noise. Proven by deliberately reintroducing that stall
// (corpus.go temporarily calling a snapshot-the-whole-ring helper
// instead of paging): max Insert latency hit 107.9ms against a 51ms
// bound, a clear catch -- but p99 stayed at 15.57µs, which would have
// silently passed. So the assertion stays on the max; what changed is
// only where the reference duration comes from.
//
// The fix: the "one full pass" reference is now the median of the
// Replay passes the background goroutine actually completes *during*
// the same contended window the Insert measurements come from, not a
// single sample taken before it. Both sides of the comparison now see
// the same runner load in the same few hundred milliseconds, which is
// what a same-run comparison was always supposed to buy -- #501's
// defect was that only one side of it actually did.
func TestMemoryCorpusReplayDoesNotStallIngest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency timing test in -short mode")
	}

	const (
		burstRate = 3900 // events/sec, see this test's own doc comment
		testDur   = 300 * time.Millisecond
		// stallFraction bounds max single Insert latency to at most this
		// fraction of one full, concurrently-run Replay pass (median of
		// the passes completed during the test -- see the doc comment
		// above for why it's no longer an uncontended pre-measurement).
		// The failure mode this test exists to catch is a single lock
		// spanning the entire corpus -- an Insert stalled for ~100% of a
		// pass -- so the bound only needs to sit far below that, not
		// close to the noise floor. It started life at 0.25 and failed
		// on a starved CI runner at 30% (15.0ms against a 12.4ms bound,
		// on a 49.6ms pass): one scheduler hiccup on a shared runner
		// costs ~10ms by itself, which is measurement noise at this
		// scale, not a stall. Half a pass still fails a whole-pass lock
		// by 2x.
		stallFraction = 0.5
	)
	// stallFloor absorbs absolute scheduler noise on starved runners: a
	// preempted goroutine can lose ~15ms without any lock being held at
	// all, so bounds below that measure the runner, not the code.
	const stallFloor = 20 * time.Millisecond
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

	stop := make(chan struct{})
	replayDone := make(chan struct{})
	var passMu sync.Mutex
	var replayPassDurations []time.Duration // filled only from *contended* passes -- see doc comment above
	go func() {
		defer close(replayDone)
		for {
			select {
			case <-stop:
				return
			default:
				passStart := time.Now()
				corpus.Replay(func(store.Event) {})
				elapsed := time.Since(passStart)
				passMu.Lock()
				replayPassDurations = append(replayPassDurations, elapsed)
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
	passMu.Unlock()
	if len(passes) == 0 {
		// Only reachable if a single contended Replay pass took longer
		// than the whole testDur -- would mean the corpus/testDur sizing
		// is wrong for this environment, not that the property held or
		// failed to hold. Fail loudly rather than silently skip the
		// assertion below with a zero-value bound.
		t.Fatalf("no full Replay pass completed during the %s concurrent-ingest window -- can't compute a same-contention bound; corpus or testDur needs adjusting for this environment", testDur)
	}
	sort.Slice(passes, func(i, j int) bool { return passes[i] < passes[j] })
	medianReplayDuration := passes[len(passes)/2]

	stallBound := time.Duration(float64(medianReplayDuration) * stallFraction)
	if stallBound < stallFloor {
		stallBound = stallFloor
	}

	t.Logf("%d full Replay passes completed under the same contention as ingest; median pass = %s; inserted %d events over %s (target rate %d/s); max single Insert latency = %s (bound = %s)",
		len(passes), medianReplayDuration, insertCount, testDur, burstRate, maxInsertLatency, stallBound)

	if maxInsertLatency > stallBound {
		t.Fatalf("max single Insert latency = %s, want <= %s (%.0f%% of one full, concurrently-run Replay pass = %s, the median of %d passes) -- a concurrent replay stalled ingest for longer than one bounded page read should ever cost",
			maxInsertLatency, stallBound, stallFraction*100, medianReplayDuration, len(passes))
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
