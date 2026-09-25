// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
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
//
// io.Discard, not os.NewFile(0, os.DevNull): that wrapped descriptor 0
// in an *os.File whose finalizer closes the descriptor once the logger
// is collected. The next file the package opened then got number 0,
// and the next collected logger closed that one -- os.ReadFile on an
// unrelated source file failing with "bad file descriptor" (#939).
func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
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

// retainOneDay leaves one day of history on disk under cfg's directory,
// written through a real store with cfg's key, and returns the directory.
func retainOneDay(t *testing.T, cfg config.Config) string {
	t.Helper()
	cfg.History.Enabled = true
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
	if n := retainedDayCount(t, dir); n == 0 {
		t.Fatal("nothing was retained to begin with")
	}
	return dir
}

// capturedLog records what openHistory says, so a test can hold it to
// telling the operator where the kept files are.
func capturedLog() (*slog.Logger, *strings.Builder) {
	var b strings.Builder
	return slog.New(slog.NewTextHandler(&b, nil)), &b
}

// Off at startup keeps what an earlier run retained. A missing
// history block reads exactly like enabled: false, so deleting on it
// turned one lost line of config into a month of lost evidence.
func TestOpenHistoryKeepsTheHistoryWhenSwitchedOff(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	dir := retainOneDay(t, cfg)

	cfg.History.Enabled = false
	log, out := capturedLog()
	if hist := openHistory(log, cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory returned a store with the switch off")
	}
	if n := retainedDayCount(t, dir); n != 1 {
		t.Errorf("switching it off left %d day file(s), want the 1 retained -- nothing may be deleted without history.deleteWhenOff", n)
	}
	for _, want := range []string{"level=WARN", dir, "history.deleteWhenOff: true", "history.enabled: true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the startup log does not mention %q:\n%s", want, out.String())
		}
	}
}

// The same with the key file gone from the config: no key is not
// permission to delete either.
func TestOpenHistoryKeepsTheHistoryWithNoKey(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	dir := retainOneDay(t, cfg)

	cfg.History.Enabled = false
	cfg.History.KeyFile = ""
	log, out := capturedLog()
	if hist := openHistory(log, cfg); hist != nil {
		hist.Close()
		t.Fatal("openHistory returned a store with no key configured")
	}
	if n := retainedDayCount(t, dir); n != 1 {
		t.Errorf("no key configured left %d day file(s), want the 1 retained -- nothing may be deleted without history.deleteWhenOff", n)
	}
	for _, want := range []string{"level=WARN", dir, "history.deleteWhenOff: true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the startup log does not mention %q:\n%s", want, out.String())
		}
	}
}

// Nothing on disk, nothing to warn about: the default install stays at
// its one info line.
func TestOpenHistoryOffWithNothingRetainedDoesNotWarn(t *testing.T) {
	log, out := capturedLog()
	if hist := openHistory(log, historyConfig(t, false, "")); hist != nil {
		hist.Close()
		t.Fatal("openHistory returned a store with no key configured")
	}
	if strings.Contains(out.String(), "level=WARN") {
		t.Errorf("warned with nothing retained:\n%s", out.String())
	}
}

// history.deleteWhenOff: true is the operator asking for the old
// behaviour, and gets it on both off paths.
func TestOpenHistoryDeletesWhenOffOnlyWhenAsked(t *testing.T) {
	for _, noKey := range []bool{false, true} {
		cfg := historyConfig(t, true, writeKeyFile(t))
		dir := retainOneDay(t, cfg)

		cfg.History.Enabled = false
		cfg.History.DeleteWhenOff = true
		if noKey {
			cfg.History.KeyFile = ""
		}
		if hist := openHistory(quietLog(), cfg); hist != nil {
			hist.Close()
			t.Fatal("openHistory returned a store with the switch off")
		}
		if n := retainedDayCount(t, dir); n != 0 {
			t.Errorf("noKey=%v: history.deleteWhenOff: true left %d day file(s) behind", noKey, n)
		}
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
