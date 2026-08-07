// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// TestObserveRecoveredSwallowsPanic proves observeRecovered actually
// absorbs a panic rather than letting it escape -- if it didn't, this
// test would crash the whole `go test` process (an unrecovered panic
// in any goroutine, including the test's own, terminates the binary),
// not just fail an assertion.
func TestObserveRecoveredSwallowsPanic(t *testing.T) {
	d, _ := newTestDetector(t, DefaultConfig())
	// A nil SettingsStore makes observeScanAndSpike's very first line
	// (d.settings.Get(...)) a genuine nil-pointer dereference -- a
	// real panic, not a simulated one.
	d.settings = nil

	d.observeRecovered(evt("198.51.100.1", 22, time.Now()))
	// Reaching this line at all is the proof.
}

// TestRunSurvivesPanickingEvents proves Run's goroutine keeps consuming
// from observeQueue after a panic instead of silently exiting for good
// -- the failure mode the reliability audit specifically called out:
// recover() deferred at the top of Run itself would still only unwind
// as far as Run, ending the goroutine after the very first bad event.
// Uses observeQueue's buffered length as a race-detector-safe liveness
// signal (channel len() is safe to read concurrently) rather than
// touching any Detector field after Run starts, which would itself be
// a data race.
func TestRunSurvivesPanickingEvents(t *testing.T) {
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	// nil settings: every single event panics inside Observe, every
	// time -- if Run's recovery only worked once, or not at all, this
	// queue would never drain.
	d := NewWithSettings(DefaultConfig(), fs, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	const n = 20
	for i := 0; i < n; i++ {
		d.Enqueue(evt("198.51.100.1", 22, time.Now()))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(d.observeQueue) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Run appears to have stopped consuming after a panic -- %d of %d events still queued", len(d.observeQueue), n)
}
