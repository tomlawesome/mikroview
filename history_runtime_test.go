// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/settings"
	"github.com/tomlawesome/mikroview/internal/store"
)

// unpersistedSettings is a fully usable settings store that writes
// nowhere -- settings.Open("")'s documented "persistence not
// configured" mode. Where a test needs a change to survive a reopen it
// asks for a path instead.
func unpersistedSettings(t *testing.T) *settings.Store {
	t.Helper()
	set, err := settings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// retainedDayCount is how many day files are on disk.
func retainedDayCount(t *testing.T, dir string) int {
	t.Helper()
	files, err := retention.DaysHeld(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	return len(files)
}

// Turning it on takes what memory already holds and every event after
// (owner, 2026-09-03) -- so the proof is that events which were only
// ever in the ring end up in the files, without being written twice.
func TestHistoryTurnOnTakesWhatMemoryHolds(t *testing.T) {
	cfg := historyConfig(t, false, writeKeyFile(t))
	ring := store.New(1000, 72*time.Hour)
	for i := range 40 {
		ring.Insert(historyEvent(time.Now().UTC().Add(time.Duration(i-40)*time.Second), "10.4.0.1"))
	}

	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), ring)
	t.Cleanup(func() { hist.Close() })
	if hist.HistorySettings().Enabled {
		t.Fatal("the history came up on with the switch off")
	}
	if n := retainedDayCount(t, historyDirectory(cfg)); n != 0 {
		t.Fatalf("%d day file(s) on disk before anything was turned on", n)
	}

	if err := hist.ApplyHistory(true, 30, 1<<30); err != nil {
		t.Fatalf("turning it on: %v", err)
	}

	// One live event after the swap, which must also land.
	hist.Append(historyEvent(time.Now().UTC(), "10.4.0.2"))
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	seen := map[uint64]int{}
	total := 0
	days, err := hist.Days()
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	for _, day := range days {
		if _, err := hist.ReplayDay(day, time.Time{}, func(e store.Event) {
			seen[e.ID]++
			total++
		}); err != nil {
			t.Fatalf("ReplayDay(%s): %v", day, err)
		}
	}
	if total != 41 {
		t.Errorf("%d events were retained, want 41 -- the ring's 40 plus the one that arrived after", total)
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("event %d was written %d times -- the seam between the backfill and the live path duplicated it", id, n)
		}
	}
}

// Off has to mean the events are gone, and gone before the call
// returns -- not scheduled, not on the next flush.
func TestHistoryTurnOffLeavesTheDirectoryEmpty(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	dir := historyDirectory(cfg)
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), store.New(100, time.Hour))
	t.Cleanup(func() { hist.Close() })

	hist.Append(historyEvent(time.Now().UTC().Add(-2*time.Hour), "10.5.0.1"))
	hist.Append(historyEvent(time.Now().UTC().Add(-26*time.Hour), "10.5.0.2"))
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := retainedDayCount(t, dir); n != 2 {
		t.Fatalf("%d day file(s) retained, want 2 -- there was nothing to delete", n)
	}

	if err := hist.ApplyHistory(false, 30, 1<<30); err != nil {
		t.Fatalf("turning it off: %v", err)
	}

	if n := retainedDayCount(t, dir); n != 0 {
		t.Errorf("turning it off left %d day file(s) behind", n)
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("turning it off left %d file(s) in %s", len(entries), dir)
	}
	got := hist.HistorySettings()
	if got.Enabled || got.Held != nil {
		t.Errorf("after turning it off the state reads %+v, want off with nothing held", got)
	}
}

// Shrinking the day count drops the days now, while the operator is
// watching -- a deployment with no traffic would otherwise wait
// indefinitely for a flush to apply the setting they just moved.
func TestHistoryShrinkingDaysPrunesAtOnce(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	dir := historyDirectory(cfg)
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), store.New(100, time.Hour))
	t.Cleanup(func() { hist.Close() })

	now := time.Now().UTC()
	for i := range 5 {
		hist.Append(historyEvent(now.Add(-time.Duration(i)*24*time.Hour), "10.6.0.1"))
	}
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := retainedDayCount(t, dir); n != 5 {
		t.Fatalf("%d day file(s) retained, want 5", n)
	}

	if err := hist.ApplyHistory(true, 2, 1<<30); err != nil {
		t.Fatalf("shrinking: %v", err)
	}

	if n := retainedDayCount(t, dir); n != 2 {
		t.Errorf("%d day file(s) after shrinking to 2 days -- the prune did not happen on the change", n)
	}
	got := hist.HistorySettings()
	if got.Days != 2 {
		t.Errorf("the state says %d days allowed, want 2", got.Days)
	}
	if got.Held == nil || got.Held.Days != 2 {
		t.Errorf("the state reports %+v held, want 2 days", got.Held)
	}
	if got.Capped {
		t.Error("capped is true after a day-count prune -- it names the byte cap, which is not what dropped these")
	}
}

// The corpus keeps reading across a swap: present, then nil, then
// present again. Everything downstream holds one stable object, so
// nothing has to be re-wired when the switch moves.
func TestHistoryReplaySurvivesASwap(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	ring := store.New(1000, 72*time.Hour)
	ring.Insert(historyEvent(time.Now().UTC().Add(-time.Minute), "10.7.0.9"))

	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), ring)
	t.Cleanup(func() { hist.Close() })
	corpus := engine.NewRetainedCorpus(ring, hist)

	hist.Append(historyEvent(time.Now().UTC().Add(-30*time.Hour), "10.7.0.1"))
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if w := corpus.Replay(func(store.Event) {}); w.Count != 2 {
		t.Fatalf("with the history on the corpus saw %d events, want 2", w.Count)
	}

	if err := hist.ApplyHistory(false, 30, 1<<30); err != nil {
		t.Fatalf("turning it off: %v", err)
	}
	w := corpus.Replay(func(store.Event) {})
	if w.Count != 1 {
		t.Errorf("with the history off the corpus saw %d events, want the ring's 1", w.Count)
	}
	if w.Truncated {
		t.Error("the corpus reports itself truncated with no history on disk at all -- nothing was dropped")
	}

	if err := hist.ApplyHistory(true, 30, 1<<30); err != nil {
		t.Fatalf("turning it back on: %v", err)
	}
	// Turning it back on took the ring, so its one event is now on disk
	// as well as in memory -- and the cutoff keeps it from being
	// counted twice.
	if w := corpus.Replay(func(store.Event) {}); w.Count != 1 {
		t.Errorf("after turning it back on the corpus saw %d events, want 1", w.Count)
	}
}

// No key mounted means the feature cannot run at all. The control is
// refused rather than accepted and quietly ignored.
func TestHistoryWithoutAKeyRefusesToTurnOn(t *testing.T) {
	cfg := historyConfig(t, false, "")
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), store.New(100, time.Hour))
	t.Cleanup(func() { hist.Close() })

	got := hist.HistorySettings()
	if got.Keyed || got.Enabled {
		t.Fatalf("a keyless instance reports %+v, want keyed and enabled both false", got)
	}
	if err := hist.ApplyHistory(true, 30, 1<<30); err == nil {
		t.Error("turning the history on with no key mounted was accepted")
	}
	if hist.HistorySettings().Enabled {
		t.Error("the history came on with no key mounted")
	}
}

// The stored settings win over config.yaml's, the same way the memory
// figure does -- and they survive the restart a change made in a
// browser otherwise would not.
func TestHistoryStoredSettingsWinOverTheConfigFile(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	path := historyDirectory(cfg) + ".settings.json"
	cfg.Store.SettingsStorePath = path

	set, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hist := newHistoryRuntime(quietLog(), cfg, set, store.New(100, time.Hour))
	if !hist.HistorySettings().Enabled {
		t.Fatal("the history did not come up on, with the config file's switch on")
	}
	if err := hist.ApplyHistory(false, 7, 2<<30); err != nil {
		t.Fatalf("turning it off: %v", err)
	}
	if err := hist.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Same deployment, same config file, restarted.
	reopened, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again := newHistoryRuntime(quietLog(), cfg, reopened, store.New(100, time.Hour))
	t.Cleanup(func() { again.Close() })
	got := again.HistorySettings()
	if got.Enabled {
		t.Error("the history came back on after a restart, ignoring the stored off")
	}
	if got.Days != 7 || got.MaxBytes != 2<<30 {
		t.Errorf("after a restart the caps read %d days / %d bytes, want the stored 7 / %d", got.Days, got.MaxBytes, int64(2<<30))
	}
}

// The exact payload GET /api/settings/history serves on a keyed
// instance, built from real files rather than a stand-in. The
// endpoint's own wire-shape test pins the key names; this one pins that
// the figures behind them describe the disk.
func TestHistorySettingsDescribeTheRealDisk(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), store.New(100, time.Hour))
	t.Cleanup(func() { hist.Close() })

	now := time.Now().UTC()
	for i := range 3 {
		hist.Append(historyEvent(now.Add(-time.Duration(i)*24*time.Hour), "10.8.0.1"))
	}
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got := hist.HistorySettings()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GET /api/settings/history on a keyed instance: %s", encoded)

	if !got.Keyed || !got.Enabled {
		t.Errorf("state reads %+v, want keyed and enabled", got)
	}
	if got.Held == nil {
		t.Fatal("nothing reported held with three days on disk")
	}
	if got.Held.Days != 3 {
		t.Errorf("held.days = %d, want 3", got.Held.Days)
	}
	if got.Held.Oldest != now.Add(-48*time.Hour).Format("2006-01-02") {
		t.Errorf("held.oldest = %q, want %q", got.Held.Oldest, now.Add(-48*time.Hour).Format("2006-01-02"))
	}
	if got.Held.Newest != now.Format("2006-01-02") {
		t.Errorf("held.newest = %q, want %q", got.Held.Newest, now.Format("2006-01-02"))
	}
	if got.Held.Bytes <= 0 {
		t.Errorf("held.bytes = %d, want the size of three real files", got.Held.Bytes)
	}
	if got.BytesPerDay <= 0 {
		t.Errorf("bytesPerDay = %d, want the newest complete day's size", got.BytesPerDay)
	}
	if got.BytesPerDay >= got.Held.Bytes {
		t.Errorf("bytesPerDay (%d) is not smaller than the whole history (%d) -- it is one day's file, not all of them",
			got.BytesPerDay, got.Held.Bytes)
	}
	if got.Capped {
		t.Error("capped is true with three of thirty allowed days and no cap prune behind it")
	}
}

// The byte cap, not the day count, is what "full" means -- and the two
// are indistinguishable after the fact unless the store says which one
// dropped the day.
func TestHistoryReportsCappedWhenTheByteCapDropsADay(t *testing.T) {
	cfg := historyConfig(t, true, writeKeyFile(t))
	hist := newHistoryRuntime(quietLog(), cfg, unpersistedSettings(t), store.New(100, time.Hour))
	t.Cleanup(func() { hist.Close() })

	now := time.Now().UTC()
	for i := range 4 {
		hist.Append(historyEvent(now.Add(-time.Duration(i)*24*time.Hour), "10.9.0.1"))
	}
	if err := hist.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := hist.HistorySettings(); got.Capped {
		t.Fatal("capped before any cap was ever hit")
	}

	// A cap one byte under what is already held: the day count still
	// allows thirty, so anything dropped now can only have been dropped
	// by the cap.
	held := hist.HistorySettings().Held
	if held == nil {
		t.Fatal("nothing held")
	}
	if err := hist.ApplyHistory(true, 30, held.Bytes-1); err != nil {
		t.Fatalf("re-capping: %v", err)
	}

	got := hist.HistorySettings()
	if got.Held == nil || got.Held.Days >= 4 {
		t.Fatalf("the cap dropped nothing: %+v", got.Held)
	}
	if !got.Capped {
		t.Errorf("state reads %+v -- the byte cap dropped a day and capped is still false", got)
	}
}
