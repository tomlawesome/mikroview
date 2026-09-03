// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// setMinuteBucket writes a per-minute bucket directly, which is the only
// way a test can place traffic in a past minute: Insert buckets by its
// own time.Now(), deliberately, so every inserted event lands in the
// current minute however its ReceivedAt is dated.
func setMinuteBucket(s *Store, minute int64, action Action, count uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := minute % timeSeriesMinutes
	if idx < 0 {
		idx += timeSeriesMinutes
	}
	s.minuteBucketTime[idx] = minute
	s.minuteBuckets[idx][actionSlot(action)] = count
}

func timeSeriesAt(stats Stats, minute int64) (TimeBucket, bool) {
	for _, b := range stats.TimeSeries {
		if b.Time.Unix()/60 == minute {
			return b, true
		}
	}
	return TimeBucket{}, false
}

// TestSnapshotRoundTripRestoresCountersAndRecentMinutes is #795's warm
// restart for the metrics hourline: export from one store, import into
// the store a restarted process would have, and the counters and the
// minutes still inside the window come back.
func TestSnapshotRoundTripRestoresCountersAndRecentMinutes(t *testing.T) {
	before := New(100, time.Hour)
	now := time.Now()
	nowMinute := now.Unix() / 60

	before.Insert(mkEvent(now, "core", ActionAccept))
	before.Insert(mkEvent(now, "core", ActionDrop))
	before.Insert(mkEvent(now, "core", ActionDrop))

	// A minute five back, still inside the hour the axis covers.
	setMinuteBucket(before, nowMinute-5, ActionDrop, 9)
	// A minute an hour and a half back: outside the window, and one whose
	// modulo-60 slot is the same as nowMinute-30, so restoring it would
	// show as traffic half an hour ago that never happened.
	setMinuteBucket(before, nowMinute-90, ActionAccept, 4)

	raw, err := before.SnapshotPart().Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	after := New(100, time.Hour)
	taken := now.Add(-2 * time.Minute)
	if err := after.SnapshotPart().Import(raw, taken, now); err != nil {
		t.Fatalf("Import: %v", err)
	}

	stats := after.Stats()
	if stats.Total != 3 {
		t.Errorf("Total = %d, want the 3 counted before the restart", stats.Total)
	}
	if stats.ByAction[ActionDrop] != 2 || stats.ByAction[ActionAccept] != 1 {
		t.Errorf("ByAction = %v, want the pre-restart tallies", stats.ByAction)
	}
	if len(stats.TopRules) != 1 || stats.TopRules[0].Rule != "lan-wan" || stats.TopRules[0].Count != 3 {
		t.Errorf("TopRules = %v, want the pre-restart rule leaderboard", stats.TopRules)
	}
	if stats.Count != 0 {
		t.Errorf("Count = %d, want 0: no event lines are ever snapshotted", stats.Count)
	}

	restored, ok := timeSeriesAt(stats, nowMinute-5)
	if !ok {
		t.Fatalf("no time-series bucket for the minute five back")
	}
	if restored.ByAction[ActionDrop] != 9 {
		t.Errorf("restored minute = %v, want 9 drops", restored.ByAction)
	}
	blank, ok := timeSeriesAt(stats, nowMinute-30)
	if !ok {
		t.Fatalf("no time-series bucket for the minute thirty back")
	}
	if len(blank.ByAction) != 0 {
		t.Errorf("minute thirty back = %v, want empty -- the 90-minute-old bucket shares its slot and must not be restored into it", blank.ByAction)
	}
	current, ok := timeSeriesAt(stats, nowMinute)
	if !ok {
		t.Fatalf("no time-series bucket for the current minute")
	}
	if current.ByAction[ActionDrop] != 2 || current.ByAction[ActionAccept] != 1 {
		t.Errorf("current minute = %v, want the three events' own minute restored", current.ByAction)
	}
}

func TestStatsCarriesRestoredToAndLiveSince(t *testing.T) {
	cold := New(10, time.Hour)
	stats := cold.Stats()
	if stats.RestoredTo != nil {
		t.Errorf("RestoredTo = %v on a cold start, want nil", stats.RestoredTo)
	}
	if stats.LiveSince.IsZero() {
		t.Errorf("LiveSince is zero, want the moment the store started observing")
	}
	if !cold.RestoredTo().IsZero() {
		t.Errorf("RestoredTo() = %v on a cold start, want the zero time", cold.RestoredTo())
	}

	warm := New(10, time.Hour)
	taken := time.Now().Add(-4 * time.Minute)
	if err := warm.SnapshotPart().Import(json.RawMessage(`{"total":5}`), taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	stats = warm.Stats()
	if stats.RestoredTo == nil || !stats.RestoredTo.Equal(taken) {
		t.Errorf("RestoredTo = %v, want the snapshot's taken time %v", stats.RestoredTo, taken)
	}
	if !warm.RestoredTo().Equal(taken) {
		t.Errorf("RestoredTo() = %v, want %v", warm.RestoredTo(), taken)
	}
}

// TestHourTopsStaysHonestAboutRestoredMinutes holds #644's honesty
// requirement against #795's restore: the counters come back, the event
// lines do not, so a minute the ring has no lines for must read as
// unknown rather than as a top talker computed from nothing.
func TestHourTopsStaysHonestAboutRestoredMinutes(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()
	nowMinute := now.Unix() / 60

	if err := s.SnapshotPart().Import(json.RawMessage(fmt.Sprintf(
		`{"total":9,"minutes":[{"minute":%d,"byAction":{"drop":9}}]}`, nowMinute-5)),
		now.Add(-4*time.Minute), now); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Pretend this process started two minutes ago, i.e. after the
	// snapshot was written -- the real ordering, since the snapshot comes
	// from the process before this one. The axis then spans both restored
	// minutes and minutes this process lived through.
	s.mu.Lock()
	s.liveSince = now.Add(-2 * time.Minute)
	s.mu.Unlock()

	// No events inserted: this is the boot state a warm restart actually
	// starts in -- counters restored, ring empty -- and it keeps the
	// assertion about the restore's own effect, rather than mixing it
	// with the eviction rule HourTops already applies once the ring holds
	// something.
	liveMinute := nowMinute

	tops := s.HourTops()
	for _, top := range tops {
		minute := top.Time.Unix() / 60
		wantComplete := !top.Time.Before(s.liveSince)
		if top.Complete != wantComplete {
			t.Errorf("minute %d: Complete = %v, want %v (live since %v)", minute, top.Complete, wantComplete, s.liveSince)
		}
		if !top.Complete && (top.Talker != "" || top.Port != "") {
			t.Errorf("minute %d: Talker=%q Port=%q, want both blank on a minute whose lines are gone", minute, top.Talker, top.Port)
		}
	}

	// The restored minute specifically, since that is the one the
	// snapshot has counts for and no lines.
	for _, top := range tops {
		if top.Time.Unix()/60 == nowMinute-5 && top.Complete {
			t.Errorf("the restored minute reads as complete -- its counts came off disk, its event lines did not")
		}
		if top.Time.Unix()/60 == liveMinute && !top.Complete {
			t.Errorf("a minute this process lived through reads as incomplete, so the flag never clears")
		}
	}
}

// TestExportCarriesNoEventLines is the data-custody half of #795: the
// document holds counts and identifiers, never the raw log records or
// the addresses in them.
func TestExportCarriesNoEventLines(t *testing.T) {
	s := New(10, time.Hour)
	s.Insert(mkEvent(time.Now(), "core", ActionAccept))

	raw, err := s.SnapshotPart().Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, forbidden := range []string{"raw line", "192.168.1.50", "1.2.3.4"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the exported document contains %q -- snapshots hold no event lines", forbidden)
		}
	}
}

func TestImportRefusesAStoreThatHasAlreadyIngested(t *testing.T) {
	s := New(10, time.Hour)
	s.Insert(mkEvent(time.Now(), "core", ActionAccept))

	err := s.SnapshotPart().Import(json.RawMessage(`{"total":500}`), time.Now().Add(-time.Minute), time.Now())
	if err == nil {
		t.Fatalf("Import over live counters succeeded, want a refusal -- restoring then would double-count")
	}
	if got := s.Stats().Total; got != 1 {
		t.Errorf("Total = %d after the refused import, want the 1 live event", got)
	}
}

func TestImportRejectsBytesThatAreNotAStoreDocument(t *testing.T) {
	s := New(10, time.Hour)
	if err := s.SnapshotPart().Import(json.RawMessage(`["not", "a", "store"]`), time.Now(), time.Now()); err == nil {
		t.Errorf("Import of a foreign document succeeded, want an error so the loader can skip this part")
	}
	if !s.RestoredTo().IsZero() {
		t.Errorf("a failed import still marked the store as restored")
	}
}

func TestImportCapsRestoredRuleLabels(t *testing.T) {
	orig := maxRuleLabels
	maxRuleLabels = 100
	defer func() { maxRuleLabels = orig }()

	byRule := make(map[string]uint64, 500)
	for i := 0; i < 500; i++ {
		byRule[fmt.Sprintf("rule-%03d", i)] = uint64(i + 1)
	}
	raw, err := json.Marshal(storeState{Total: 500, ByRule: byRule})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	s := New(10, time.Hour)
	if err := s.SnapshotPart().Import(raw, time.Now(), time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	s.mu.RLock()
	held := len(s.totalByRule)
	s.mu.RUnlock()
	if held > maxRuleLabels {
		t.Errorf("restored %d rule labels against a cap of %d -- a snapshot must not reinstate a label flood", held, maxRuleLabels)
	}
}

func TestSnapshotPartNameIsStable(t *testing.T) {
	if got := New(1, time.Hour).SnapshotPart().Name(); got != "store" {
		t.Errorf("Name() = %q, want %q -- the key a later boot looks the bytes up under", got, "store")
	}
}
