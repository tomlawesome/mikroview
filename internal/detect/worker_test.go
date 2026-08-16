// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/blocklist"
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
// detector that should fire, does, once Run has had a chance to catch
// up. It has been retargeted with each port (critical_port ->
// repeated_drops -> internal_recon -> mail_sender) and lands here on
// known_bad_ip, one of the two reinforcement passes internal/detect
// still evaluates (issue #405). Nothing about known_bad_ip's own
// behaviour is under test: it is picked because it fires
// deterministically off a single event, which is all this test needs
// from whichever detector it borrows.
func TestRunProcessesEnqueuedEvents(t *testing.T) {
	d, fs := newTestDetector(t, DefaultConfig())
	bl := newFakeKnownBadIPs()
	bl.setMatch("203.0.113.9", blocklist.MatchResult{Label: "Spamhaus DROP", Range: "203.0.113.0/24"})
	d.WithKnownBadIPs(bl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	d.Enqueue(store.Event{SrcIP: "203.0.113.9", DstIP: "192.168.1.1", DstPort: 22, ReceivedAt: time.Now()})

	deadline := time.After(2 * time.Second)
	for {
		if list := fs.List(); len(list) == 1 && list[0].Type == flags.TypeKnownBadIP {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("Run never processed the enqueued event into a known_bad_ip flag; got %+v", fs.List())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
