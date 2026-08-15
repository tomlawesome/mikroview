// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock advances only when told to -- no sleeps, so none of the
// timing flakiness the persistence debounce tests were bitten by.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestLimiter(interval time.Duration) (*Limiter, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	l := NewLimiter(interval)
	l.now = clock.now
	return l, clock
}

func TestLimiterFirstOccurrenceLogsImmediately(t *testing.T) {
	l, _ := newTestLimiter(30 * time.Second)
	total, ok := l.Allow()
	if !ok || total != 1 {
		t.Errorf("first Allow() = (%d, %v), want (1, true) -- the first occurrence must never be suppressed", total, ok)
	}
}

func TestLimiterSuppressesWithinWindowAndCarriesTheCount(t *testing.T) {
	l, clock := newTestLimiter(30 * time.Second)
	l.Allow()

	for i := 0; i < 5; i++ {
		clock.advance(time.Second)
		if total, ok := l.Allow(); ok {
			t.Fatalf("Allow() ok inside the window (occurrence %d)", total)
		}
	}

	clock.advance(30 * time.Second)
	total, ok := l.Allow()
	if !ok {
		t.Fatal("Allow() suppressed after the window passed")
	}
	if total != 7 {
		t.Errorf("total = %d, want 7 -- the next written line must carry the suppressed occurrences", total)
	}
}

func TestLimiterOneLinePerWindowUnderConcurrency(t *testing.T) {
	l, _ := newTestLimiter(30 * time.Second)

	const callers = 32
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := l.Allow(); ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := allowed.Load(); n != 1 {
		t.Errorf("%d concurrent callers wrote a line, want exactly 1 -- the CAS exists for this race", n)
	}
}
