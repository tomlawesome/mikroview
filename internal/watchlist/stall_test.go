// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// stallingSaveBackend blocks every Save call until released, and reports
// whether the context it was given ever carried a deadline -- #380's
// first item for this store: OpenWithBackend and persistLocked both
// used to call the backend under context.Background(), with no deadline
// at all. See flags.stallingSaveBackend, the twin of this type.
type stallingSaveBackend struct {
	mu          sync.Mutex
	release     chan struct{}
	version     int64
	inFlight    int
	maxIn       int
	sawDeadline bool
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
	if _, ok := ctx.Deadline(); ok {
		b.sawDeadline = true
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

func (b *stallingSaveBackend) stats() (maxIn int, sawDeadline bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxIn, b.sawDeadline
}

// TestUpsertDoesNotBlockOnAStuckBackend closes #380's first item for
// this store: every mutating method used to persist synchronously under
// context.Background(), so a stalled backend blocked the caller (and,
// since each held s.mu across it, every other caller) forever. With
// persist.WriteBehind, mutations only ever encode and hand the snapshot
// to the writer goroutine, and every backend call the writer performs
// now carries a deadline.
func TestUpsertDoesNotBlockOnAStuckBackend(t *testing.T) {
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

	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = s.Upsert(Entry{ID: "e2", Ports: []int{i%65000 + 1}})
			s.List()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Upsert/List blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if maxIn, _ := b.stats(); maxIn > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1", maxIn)
	}
}

// TestUpsertSaveCarriesADeadline is the direct #380-first-item proof:
// every backend call this store makes now bounds its context, rather
// than running under context.Background() forever.
func TestUpsertSaveCarriesADeadline(t *testing.T) {
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

	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, sawDeadline := b.stats(); sawDeadline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no Save call ever carried a context deadline")
}
