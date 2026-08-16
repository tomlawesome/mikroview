// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// stallingSaveBackend blocks every Save call until released, and reports
// whether the context it was given ever carried a deadline -- #380's
// first item for this store: OpenSettingsStoreWithBackend and
// persistLocked both used to call the backend under
// context.Background(), with no deadline at all. See
// flags.stallingSaveBackend, the twin of this type.
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

// TestSetDoesNotBlockOnAStuckBackend closes #380's first item for this
// store: Set used to persist synchronously under context.Background(),
// so a stalled backend blocked the caller (and, since Set held s.mu
// across it, every other caller including Get/List) forever. With
// persist.WriteBehind, Set only ever encodes and hands the snapshot to
// the writer goroutine, and every backend call the writer performs now
// carries a deadline.
func TestSetDoesNotBlockOnAStuckBackend(t *testing.T) {
	b := newStallingSaveBackend()
	s, err := OpenSettingsStoreWithBackend(b, DefaultSettingsMap())
	if err != nil {
		t.Fatalf("OpenSettingsStoreWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	s.Set(DetectorPortScan, Settings{Enabled: false})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Set(DetectorActivitySpike, Settings{Enabled: i%2 == 0})
			s.List()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Set/List blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if maxIn, _ := b.stats(); maxIn > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1", maxIn)
	}
}

// TestSetSaveCarriesADeadline is the direct #380-first-item proof: every
// backend call this store makes now bounds its context, rather than
// running under context.Background() forever.
func TestSetSaveCarriesADeadline(t *testing.T) {
	b := newStallingSaveBackend()
	s, err := OpenSettingsStoreWithBackend(b, DefaultSettingsMap())
	if err != nil {
		t.Fatalf("OpenSettingsStoreWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	s.Set(DetectorPortScan, Settings{Enabled: false})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, sawDeadline := b.stats(); sawDeadline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no Save call ever carried a context deadline")
}
