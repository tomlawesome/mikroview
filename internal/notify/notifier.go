// Package notify delivers newly-raised behavioral flags (see
// internal/flags) somewhere outside the mikroview UI, batched rather
// than one message per flag so a real incident (many flags firing in
// quick succession) doesn't flood whatever channel is receiving them.
// Send-only: nothing in this package ever reads from or acts on the
// destination, matching mikroview's "interrogation helper" scope.
package notify

import "github.com/tomlawesome/mikroview/internal/flags"

// Notifier delivers a batch of newly-raised flags somewhere outside the
// UI. Best-effort: an error is logged by Dispatcher, never retried --
// the same "helper signal, not critical alerting" scope as the rest of
// mikroview (see flags.Store.Open's doc comment making the same call
// about a corrupted flags file never blocking startup).
type Notifier interface {
	Send(batch []flags.Flag) error
}
