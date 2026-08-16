// SPDX-License-Identifier: AGPL-3.0-only

package flags

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// stallingSaveBackend blocks every Save call until released -- the same
// "stall until armed/released" shape internal/auth's own stallingBackend
// uses (internal/auth/stall_test.go), kept as its own small copy per
// this codebase's "each package keeps its own small private copy"
// convention rather than shared across packages.
type stallingSaveBackend struct {
	mu       sync.Mutex
	release  chan struct{}
	version  int64
	inFlight int
	maxIn    int
}

func newStallingSaveBackend() *stallingSaveBackend {
	return &stallingSaveBackend{release: make(chan struct{})}
}

func (b *stallingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *stallingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxIn {
		b.maxIn = b.inFlight
	}
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		b.mu.Lock()
		b.inFlight--
		b.mu.Unlock()
		return 0, ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight--
	b.version++
	return b.version, nil
}

func (b *stallingSaveBackend) Close() error     { return nil }
func (b *stallingSaveBackend) Describe() string { return "stalling test backend" }

func (b *stallingSaveBackend) maxInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxIn
}

// TestAddDoesNotBlockOnAStuckBackend is #400's central proof for this
// store, one of #377's three named copies: Add used to hold s.mu across
// persistLocked's own backend call, so a stalled backend blocked every
// Add -- and, transitively, every read taking s.mu.RLock, since a
// writer waiting on a stuck Lock starves later readers behind it -- for
// as long as the backend stayed stuck. With persist.WriteBehind, Add
// only ever encodes and hands the snapshot to the writer goroutine; the
// backend call happens entirely off this path.
func TestAddDoesNotBlockOnAStuckBackend(t *testing.T) {
	b := newStallingSaveBackend()
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	// The first Add's own write attempts immediately and blocks the
	// writer goroutine inside Save -- exactly the state a stuck backend
	// leaves the store in.
	s.Add(TypePortScan, "203.0.113.1", "first", time.Now())

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Add(TypeActivitySpike, "203.0.113.2", "still responsive", time.Now())
			s.List()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Add/List blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if got := b.maxInFlight(); got > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1 -- the writer goroutine must serialise its own backend I/O", got)
	}
}

// failingSaveBackend always fails Save, for the back-off proof below.
type failingSaveBackend struct {
	mu    sync.Mutex
	saves int
}

func (b *failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (b *failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.saves++
	b.mu.Unlock()
	return 0, errors.New("backend unavailable")
}
func (b *failingSaveBackend) Close() error     { return nil }
func (b *failingSaveBackend) Describe() string { return "failing test backend" }
func (b *failingSaveBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

// TestSustainedFailuresCostOneAttemptPerBackoffWindow is #377's proof
// for this store: a permanently failing backend under a sustained
// stream of Add calls (a real re-fire burst, the scenario
// persistMinInterval exists for) must not be attempted once per event.
func TestSustainedFailuresCostOneAttemptPerBackoffWindow(t *testing.T) {
	orig := persistMinInterval
	persistMinInterval = 200 * time.Millisecond
	defer func() { persistMinInterval = orig }()

	b := &failingSaveBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	now := time.Now()
	for i := 0; i < 500; i++ {
		s.Add(TypePortScan, "203.0.113.9", "re-fire", now)
	}

	time.Sleep(3 * persistMinInterval)
	if saves := b.count(); saves > 6 {
		t.Errorf("500 re-fires over ~3 back-off windows against a permanently failing backend produced %d attempts, want roughly one per window, not one per event", saves)
	}
}
