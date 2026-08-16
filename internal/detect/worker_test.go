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
// detector that should act, does, once Run has had a chance to catch up.
// It has been retargeted with each port (critical_port -> repeated_drops
// -> internal_recon -> mail_sender -> known_bad_ip) and lands here on
// netclass, the one pass internal/detect still evaluates (issue #405).
//
// netclass raises no flag of its own -- it only reinforces one -- so the
// observable effect this waits on is a confidence floor landing on a
// pre-seeded flag rather than a new flag appearing. Nothing about
// netclass's own behaviour is under test; it is simply what is left to
// borrow.
func TestRunProcessesEnqueuedEvents(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())
	nc := newFakeNetClass()
	nc.setMatch("203.0.113.9", torMatch())
	d.WithNetClass(nc)

	now := time.Now()
	fs.AddWithDetail(flags.TypePortScan, "203.0.113.9", "seeded", 10, flags.Evidence{}, "", now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	d.Enqueue(store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 22, ReceivedAt: now})

	deadline := time.After(2 * time.Second)
	for {
		for _, f := range fs.List() {
			if f.Type == flags.TypePortScan && f.ReputationFloor != nil {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("Run never processed the enqueued event into a netclass reinforcement; got %+v", fs.List())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
