// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// stallingSaveBackend blocks every Save call until released -- see
// flags.stallingSaveBackend, the twin of this type.
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

// TestSeenDoesNotBlockOnAStuckBackend is #400's central proof for this
// store, one of #377's three named copies: Seen used to hold r.mu across
// persistLocked's own backend call, so a stalled backend blocked every
// Seen and every read behind it. With persist.WriteBehind, Seen only
// ever encodes and hands the snapshot to the writer goroutine.
func TestSeenDoesNotBlockOnAStuckBackend(t *testing.T) {
	b := newStallingSaveBackend()
	r, err := OpenMACRegistryWithBackend(b)
	if err != nil {
		t.Fatalf("OpenMACRegistryWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		r.Close(ctx)
	}()

	r.Seen("11:11:11:11:11:11", time.Now())

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			r.Seen("22:22:22:22:22:22", time.Now())
			r.List()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Seen/List blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if got := b.maxInFlight(); got > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1", got)
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
// stream of Seen calls must not be attempted once per event.
func TestSustainedFailuresCostOneAttemptPerBackoffWindow(t *testing.T) {
	orig := macRegistryPersistMinInterval
	macRegistryPersistMinInterval = 200 * time.Millisecond
	defer func() { macRegistryPersistMinInterval = orig }()

	b := &failingSaveBackend{}
	r, err := OpenMACRegistryWithBackend(b)
	if err != nil {
		t.Fatalf("OpenMACRegistryWithBackend: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r.Close(ctx)
	}()

	now := time.Now()
	for i := 0; i < 500; i++ {
		r.Seen("33:33:33:33:33:33", now)
	}

	time.Sleep(3 * macRegistryPersistMinInterval)
	if saves := b.count(); saves > 6 {
		t.Errorf("500 Seen calls over ~3 back-off windows against a permanently failing backend produced %d attempts, want roughly one per window, not one per event", saves)
	}
}
