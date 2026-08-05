package notify

import (
	"context"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/logging"
)

var logger = logging.New("notify")

// queueSize bounds pending the same way hub.clientQueueSize bounds a
// WebSocket client's queue -- flags fire far less often than raw
// events, so this is a generous safety net, not a limit expected to be
// hit in normal use.
const queueSize = 1000

// Dispatcher batches newly-raised flags (see flags.Store.WithOnRaise)
// and flushes them to every registered Notifier on a fixed interval --
// a ticker, not a quiet-period debounce, so a sustained flood of flags
// during a real incident still gets a bounded max delay before
// alerting, rather than the batch window continuously resetting and
// never flushing. The shared piece other notification channels (e.g. a
// future push-notification Notifier) plug into rather than each
// re-implementing their own batching.
type Dispatcher struct {
	notifiers []Notifier
	window    time.Duration
	pending   chan flags.Flag
}

func NewDispatcher(window time.Duration, notifiers []Notifier) *Dispatcher {
	return &Dispatcher{notifiers: notifiers, window: window, pending: make(chan flags.Flag, queueSize)}
}

// Enqueue is the func passed to flags.Store.WithOnRaise -- non-blocking,
// dropping the incoming flag (with a log line) if the queue is somehow
// full, mirroring the drop-rather-than-block contract every other
// injected callback on the ingest path already has (e.g.
// internal/detect's lookupSlots semaphore).
func (d *Dispatcher) Enqueue(f flags.Flag) {
	select {
	case d.pending <- f:
	default:
		logger.Warn("dropped a flag notification -- dispatcher queue full")
	}
}

// Run drains pending on d.window's ticker, calling every Notifier's
// Send once per non-empty batch, until ctx is cancelled -- one last
// best-effort flush on cancellation so a shutdown during an active
// incident doesn't silently drop a pending batch. Meant to run in its
// own goroutine.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.window)
	defer ticker.Stop()

	var batch []flags.Flag
	for {
		select {
		case f := <-d.pending:
			batch = append(batch, f)
		case <-ticker.C:
			batch = d.flush(batch)
		case <-ctx.Done():
			d.flush(batch)
			return
		}
	}
}

func (d *Dispatcher) flush(batch []flags.Flag) []flags.Flag {
	if len(batch) == 0 {
		return batch
	}
	for _, n := range d.notifiers {
		if err := n.Send(batch); err != nil {
			logger.Warn(err.Error())
		}
	}
	return nil
}
