// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is internal/watchlist/stall_test.go, moved when issue #407
// deleted watchlist.Store: #380's first item -- a stuck backend must
// never block a caller, and every backend Save call must carry a
// deadline -- is a property of whichever store persists mutations, and
// entries are DefinitionsStore's mutations now (UpsertExpectation and
// friends, definitions_expectations.go). The property, and the mechanism
// that provides it (persist.WriteBehind), are unchanged; only which
// store's public API is driven changed.
//
// TestUpsertExpectationDoesNotBlockOnAStuckBackend reuses
// stallingSaveBackend (state_test.go, same package) rather than
// redeclaring an equivalent type -- see this issue's brief. That type
// does not track whether a Save's context carried a deadline, so
// TestUpsertExpectationSaveCarriesADeadline below declares its own small
// backend for that one property instead of extending a type this file is
// not allowed to touch.

// stallingDeadlineBackend blocks every Save call until released, and
// reports whether the context it was given ever carried a deadline --
// #380's first item, the direct proof: OpenWithBackend and
// persistLocked both used to call the backend under
// context.Background(), with no deadline at all, for the store this
// replaces (watchlist.Store) and every sibling store alongside it.
type stallingDeadlineBackend struct {
	mu          sync.Mutex
	release     chan struct{}
	version     int64
	sawDeadline bool
}

func newStallingDeadlineBackend() *stallingDeadlineBackend {
	return &stallingDeadlineBackend{release: make(chan struct{})}
}

func (b *stallingDeadlineBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *stallingDeadlineBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	if _, ok := ctx.Deadline(); ok {
		b.sawDeadline = true
	}
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.version++
	return b.version, nil
}

func (b *stallingDeadlineBackend) Close() error     { return nil }
func (b *stallingDeadlineBackend) Describe() string { return "stalling deadline test backend" }

func (b *stallingDeadlineBackend) hasSeenDeadline() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sawDeadline
}

// TestUpsertExpectationDoesNotBlockOnAStuckBackend closes #380's first
// item for this store: every mutating method used to persist
// synchronously under context.Background() before #400/#407, so a
// stalled backend blocked the caller (and, since each held s.mu across
// it, every other caller) forever. With persist.WriteBehind, mutations
// only ever encode and hand the snapshot to the writer goroutine, and
// every backend call the writer performs now carries a deadline.
func TestUpsertExpectationDoesNotBlockOnAStuckBackend(t *testing.T) {
	orig := definitionsPersistMinInterval
	definitionsPersistMinInterval = time.Millisecond
	defer func() { definitionsPersistMinInterval = orig }()

	b := newStallingSaveBackend()
	s, err := OpenDefinitionsStoreWithBackend(b)
	if err != nil {
		t.Fatalf("OpenDefinitionsStoreWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = s.UpsertExpectation(watchlist.Entry{ID: "e2", Ports: []int{i%65000 + 1}})
			_, _ = s.ListExpectations()
		}
		close(done)
	}()

	// 60s here is a hang detector, not a performance budget: it bounds a
	// deadlock (the lock held across a backend call), not how fast the
	// goroutine runs. The real property under test is the maxInFlight()
	// assertion below, which is already timing-independent -- nobody
	// should read this number as a latency target and tighten it.
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("UpsertExpectation/ListExpectations blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if maxIn := b.maxInFlight(); maxIn > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1", maxIn)
	}
}

// TestUpsertExpectationSaveCarriesADeadline is the direct #380-first-item
// proof: every backend call this store makes now bounds its context,
// rather than running under context.Background() forever.
func TestUpsertExpectationSaveCarriesADeadline(t *testing.T) {
	orig := definitionsPersistMinInterval
	definitionsPersistMinInterval = time.Millisecond
	defer func() { definitionsPersistMinInterval = orig }()

	b := newStallingDeadlineBackend()
	s, err := OpenDefinitionsStoreWithBackend(b)
	if err != nil {
		t.Fatalf("OpenDefinitionsStoreWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.hasSeenDeadline() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no Save call ever carried a context deadline")
}
