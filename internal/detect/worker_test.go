// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

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

// TestRunProcessesEnqueuedEvents is gone with the last thing it could
// observe. It proved Run drains the queue and feeds events through
// Observe, and was retargeted with each port (critical_port ->
// repeated_drops -> internal_recon -> mail_sender -> known_bad_ip ->
// netclass); with netclass's port (issue #405) Observe has no detector
// left to call, so there is no observable effect for this test to wait
// on. The queue/worker/drain guarantee it stood for is the chassis's now
// and is pinned there: internal/engine/engine_test.go's Run/drain and
// backpressure tests, over the queue this one is about to be deleted
// alongside.
