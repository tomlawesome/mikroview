// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// TestEnqueueNeverBlocksOnFullQueue mirrors internal/hub's
// TestBroadcastNeverBlocksOnFullSlowClient -- proves the ingest-path
// caller of Enqueue never stalls waiting on detection, even when nothing
// is draining the queue (e.g. Run hasn't been started, or has fallen
// behind), the whole point of moving Observe off the ingest goroutine.
func TestEnqueueNeverBlocksOnFullQueue(t *testing.T) {
	d, _ := newTestDetector(t, DefaultConfig()) // Run deliberately never started

	done := make(chan struct{})
	go func() {
		for i := 0; i < observeQueueSize+50; i++ {
			d.Enqueue(store.Event{ID: uint64(i), SrcIP: "203.0.113.9"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Enqueue blocked on a full, undrained detection queue")
	}
}

// TestRunProcessesEnqueuedEvents proves Run actually drains the queue
// and feeds events through Observe -- Enqueue alone (see above) only
// proves the non-blocking send; this closes the loop by confirming a
// detector that should fire, does, once Run has had a chance to catch up.
func TestRunProcessesEnqueuedEvents(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PortScanThreshold = 5
	cfg.PortScanWindow = time.Minute
	d, fs := newTestDetector(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	now := time.Now()
	for port := 1; port <= 5; port++ {
		d.Enqueue(evt("203.0.113.9", port, now))
	}

	deadline := time.After(2 * time.Second)
	for {
		if list := fs.List(); len(list) == 1 && list[0].Type == flags.TypePortScan {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("Run never processed the enqueued events into a port_scan flag; got %+v", fs.List())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
