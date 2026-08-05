package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// fakeNotifier records every batch it's asked to send -- lets tests
// assert both "what was sent" and "how many separate sends happened"
// without a real destination.
type fakeNotifier struct {
	mu      sync.Mutex
	batches [][]flags.Flag
}

func (f *fakeNotifier) Send(batch []flags.Flag) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, batch)
	return nil
}

func (f *fakeNotifier) snapshot() [][]flags.Flag {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]flags.Flag, len(f.batches))
	copy(out, f.batches)
	return out
}

func waitForBatches(t *testing.T, n *fakeNotifier, want int) [][]flags.Flag {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b := n.snapshot(); len(b) >= want {
			return b
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d batch(es), got %d", want, len(n.snapshot()))
	return nil
}

func TestDispatcherBatchesWithinWindow(t *testing.T) {
	fake := &fakeNotifier{}
	d := NewDispatcher(30*time.Millisecond, []Notifier{fake})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	d.Enqueue(flags.Flag{Detail: "one"})
	d.Enqueue(flags.Flag{Detail: "two"})

	batches := waitForBatches(t, fake, 1)
	if len(batches[0]) != 2 {
		t.Fatalf("expected both flags to land in a single batch, got %+v", batches)
	}
}

func TestDispatcherFlushesOnCancel(t *testing.T) {
	fake := &fakeNotifier{}
	// A window far longer than the test itself -- the only way a batch
	// can land is via the cancellation flush, not the ticker.
	d := NewDispatcher(time.Hour, []Notifier{fake})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	d.Enqueue(flags.Flag{Detail: "shutdown flush"})
	time.Sleep(20 * time.Millisecond) // let Enqueue's value land in Run's select
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return after cancel")
	}

	batches := fake.snapshot()
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0].Detail != "shutdown flush" {
		t.Fatalf("expected one flushed batch containing the pending flag, got %+v", batches)
	}
}

func TestDispatcherDropsWhenQueueFull(t *testing.T) {
	fake := &fakeNotifier{}
	// Never runs Run -- pending is never drained, so it fills up.
	d := NewDispatcher(time.Hour, []Notifier{fake})
	for i := 0; i < queueSize+10; i++ {
		d.Enqueue(flags.Flag{Detail: "x"})
	}
	if len(d.pending) != queueSize {
		t.Errorf("expected the queue to stay capped at %d, got %d", queueSize, len(d.pending))
	}
}
