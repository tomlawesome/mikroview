// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
)

// quietLog keeps the startup lines openHistory writes out of the test
// output: what is being asserted is what it returns, not what it says.
func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func historyConfig(t *testing.T, enabled bool, keyFile string) config.Config {
	t.Helper()
	dir := t.TempDir()
	var cfg config.Config
	// dataDir resolves from auth.storePath, so this is what puts the
	// history under the test's own directory rather than /var/lib.
	cfg.Auth.StorePath = filepath.Join(dir, "users.json")
	cfg.History.KeyFile = keyFile
	cfg.History.Enabled = enabled
	cfg.History.Days = 30
	cfg.History.MaxBytes = 1 << 30
	return cfg
}

func writeKeyFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.key")
	if err := os.WriteFile(path, []byte(strings.Repeat("K", retention.MinKeyBytes)), 0o600); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	return path
}

func historyEvent(at time.Time, src string) store.Event {
	return store.Event{Time: at, ReceivedAt: at, SrcIP: src, DstIP: "10.0.0.1", DstPort: 22, Action: store.ActionDrop}
}

// The whole path, through the real encrypted files rather than a stand-in:
// events written to disk, a ring holding newer ones, and one replay
// reporting a window that starts in the history and ends in memory.
//
// The engine's own tests use a fake for the disk half. This is the test
// that would catch the fake and the real store drifting apart.
func TestHistoryReplaySpansDiskAndMemory(t *testing.T) {
	// Three events in memory, the newest of them now.
	ring := store.New(1000, 72*time.Hour)
	ringStart := time.Now().UTC().Add(-3 * time.Minute)
	for i := range 3 {
		ring.Insert(historyEvent(ringStart.Add(time.Duration(i)*time.Minute), "10.2.0.1"))
	}

	// Through the runtime owner, which is what main actually wires
	// (#910) -- coming up does not backfill, so the ring's three stay
	// memory-only.
	cfg := historyConfig(t, true, writeKeyFile(t))
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), ring)
	if !hist.HistorySettings().Enabled {
		t.Fatal("the history did not come up with a key present and the switch on")
	}
	t.Cleanup(func() { hist.Close() })

	// Two days of history on disk.
	old := time.Now().UTC().Add(-48 * time.Hour)
	mid := time.Now().UTC().Add(-24 * time.Hour)
	hist.Append(historyEvent(old, "10.1.0.1"))
	hist.Append(historyEvent(mid, "10.1.0.2"))
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	corpus := engine.NewRetainedCorpus(ring, hist)
	var got []store.Event
	w := corpus.Replay(func(e store.Event) { got = append(got, e) })

	if w.Count != 5 || len(got) != 5 {
		t.Fatalf("replay visited %d events (window says %d), want 5", len(got), w.Count)
	}
	if !w.Start.Equal(old) {
		t.Errorf("window starts at %v, want the oldest retained event %v -- the receipt would understate the history", w.Start, old)
	}
	if w.End.Before(ringStart) {
		t.Errorf("window ends at %v, before the ring's own events start at %v", w.End, ringStart)
	}
	if w.Truncated {
		t.Error("window reports truncated with everything read")
	}
	for i := 1; i < len(got); i++ {
		if got[i].ReceivedAt.Before(got[i-1].ReceivedAt) {
			t.Fatalf("events are not oldest first at %d", i)
		}
	}
}

// No key is the default install: memory-only, and nothing on disk.
func TestOpenHistoryWithoutAKeyIsOffAndSilentlyNormal(t *testing.T) {
	cfg := historyConfig(t, false, "")
	if hist := openHistory(quietLog(), cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory returned a store with no key configured")
	}
}

// The switch off deletes what an earlier run retained. Off has to mean
// the events are gone, or the setting is a lie.
func TestOpenHistoryDeletesTheHistoryWhenSwitchedOff(t *testing.T) {
	keyFile := writeKeyFile(t)
	cfg := historyConfig(t, true, keyFile)

	hist := openHistory(quietLog(), cfg)
	if hist == nil {
		t.Fatal("openHistory returned nothing with the switch on")
	}
	hist.Append(historyEvent(time.Now().UTC().Add(-time.Hour), "10.1.0.1"))
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	hist.Close()

	dir := historyDirectory(cfg)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("nothing was retained to begin with (%d entries, err %v)", len(entries), err)
	}

	// Same deployment, same key, switch turned off.
	cfg.History.Enabled = false
	if hist := openHistory(quietLog(), cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory returned a store with the switch off")
	}
	entries, err = os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the history directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("turning the switch off left %d file(s) behind", len(entries))
	}
}

// A key file that is set but unusable must not fall back to anything.
// There is no plaintext mode, so the only honest outcome is off.
func TestOpenHistoryRefusesAnUnusableKey(t *testing.T) {
	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("too short"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := historyConfig(t, true, short)
	if hist := openHistory(quietLog(), cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory accepted a key file below the length floor")
	}

	cfg = historyConfig(t, true, filepath.Join(t.TempDir(), "absent.key"))
	if hist := openHistory(quietLog(), cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory accepted a key file that does not exist")
	}
}
