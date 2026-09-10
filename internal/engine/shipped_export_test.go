// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// newActivitySpikeWithState is newShippedActivitySpikeDefinition with a
// StateStore wired in -- the shape a warm restart actually has, where
// baselines resume from the state store (#399/#400) and the count rings
// they are judged against resume from the snapshot (#795).
func newActivitySpikeWithState(t *testing.T, fs *flags.Store, state *StateStore) *activitySpikeDefinition {
	t.Helper()
	def := Definition{
		ID:      "activity_spike",
		Name:    "Activity spike",
		Intent:  IntentDetection,
		Kind:    KindProgrammatic,
		Enabled: true,
		Params: Params{
			"threshold":               200,
			"window":                  (60 * time.Second).String(),
			"baselineMultiplier":      3.0,
			"warmupSamples":           20,
			"vpnInterfaces":           []string{},
			"vpnConfidenceMultiplier": 1.5,
			"updateCadence":           "perEvent",
			"baselineFloorDuration":   time.Duration(0).String(),
		},
		ParamSchema: ActivitySpikeParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{State: state})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(activity_spike): %v", err)
	}
	d := built.(*activitySpikeDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

// withEagerBaselinePersist makes every baseline reading reach the
// StateStore immediately, instead of once a minute -- the same var-not-a-
// const convention the rest of this package's tests use, so a test that
// simulates a restart does not have to run for a real minute first to
// have anything persisted to restart from.
func withEagerBaselinePersist(t *testing.T) {
	t.Helper()
	orig := baselinePersistInterval
	baselinePersistInterval = 0
	t.Cleanup(func() { baselinePersistInterval = orig })
}

func mustExportState(t *testing.T, d Snapshotted) []byte {
	t.Helper()
	raw, err := d.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	return raw
}

func TestActivitySpikeStateRoundTrips(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.4"
	for i := 0; i < 300; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: exportStart.Add(time.Duration(i) * 100 * time.Millisecond)})
	}
	now := exportStart.Add(30 * time.Second)

	raw := mustExportState(t, d)
	restoredFlags := newTestFlagsStore(t)
	restored := newShippedActivitySpikeDefinition(t, restoredFlags, nil, Scope{})
	if err := restored.ImportState(raw, now, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	before, ok := d.counts.Get(ip)
	if !ok {
		t.Fatal("test setup: expected a count ring for the source")
	}
	after, ok := restored.counts.Get(ip)
	if !ok {
		t.Fatal("expected the source's count ring to be restored")
	}
	if want, got := before.Count(now, d.window), after.Count(now, restored.window); got != want {
		t.Errorf("restored windowed count = %d, want %d", got, want)
	}

	src, ok := restored.sources.Get(ip)
	if !ok {
		t.Fatal("expected the source's freeze/day bookkeeping to be restored")
	}
	original, _ := d.sources.Get(ip)
	if src.frozen != original.frozen || !src.frozenSince.Equal(original.frozenSince) || src.peaked != original.peaked {
		t.Errorf("restored freeze state = (%v, %v, %v), want (%v, %v, %v)",
			src.frozen, src.frozenSince, src.peaked, original.frozen, original.frozenSince, original.peaked)
	}
	hour := exportStart.Hour()
	if src.hourDay[hour] != original.hourDay[hour] || src.hourPeak[hour] != original.hourPeak[hour] {
		t.Errorf("restored hour bucket = (%q, %d), want (%q, %d)",
			src.hourDay[hour], src.hourPeak[hour], original.hourDay[hour], original.hourPeak[hour])
	}

	// #795's own requirement: restoring state is not itself a firing.
	if len(restoredFlags.List()) != 0 {
		t.Errorf("ImportState raised %d flag(s); importing state must never emit", len(restoredFlags.List()))
	}
}

// TestActivitySpikeRestoredCountsJudgeTheNextEvent is what warm restart
// is for: the event arriving just after a restart is judged against the
// window the process had already observed, not against an empty ring.
// Baselines come back from the StateStore, counts from the snapshot --
// both halves are needed, which is why the cold control below sees
// nothing at all.
//
// It is also the pin on baselineSet.resume: the ramp leaves this source
// frozen, and a frozen source is judged through baselines.snapshot,
// which never resumes a key by itself. Without the resume in
// ImportState this test fails with no flag at all, because the restored
// window would be judged against a baseline that reads as absent.
func TestActivitySpikeRestoredCountsJudgeTheNextEvent(t *testing.T) {
	withEagerBaselinePersist(t)
	state, err := OpenStateStore("")
	if err != nil {
		t.Fatalf("OpenStateStore: %v", err)
	}

	fs := newTestFlagsStore(t)
	d := newActivitySpikeWithState(t, fs, state)
	ip := "198.51.100.4"
	const events = 2000
	for i := 0; i < events; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: exportStart.Add(time.Duration(i) * 10 * time.Millisecond)})
	}
	taken := exportStart.Add(events * 10 * time.Millisecond)
	raw := mustExportState(t, d)

	// The restart: same StateStore (baselines), the snapshot just taken
	// (counts), and one further event.
	next := taken.Add(10 * time.Millisecond)
	warmFlags := newTestFlagsStore(t)
	warm := newActivitySpikeWithState(t, warmFlags, state)
	if err := warm.ImportState(raw, taken, taken); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if len(warmFlags.List()) != 0 {
		t.Fatalf("ImportState raised %d flag(s) before any event", len(warmFlags.List()))
	}
	warm.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new", ReceivedAt: next})

	got := asFlagOfType(warmFlags)
	if got == nil {
		t.Fatal("expected the first event after a warm restart to be judged against the restored window and fire")
	}
	if !strings.Contains(got.Detail, "2001 events") {
		t.Errorf("Detail = %q, want it to report the restored window's count (2001 events)", got.Detail)
	}

	// The cold control: the same restart without the snapshot sees one
	// event in the window and cannot say anything.
	coldFlags := newTestFlagsStore(t)
	cold := newActivitySpikeWithState(t, coldFlags, state)
	cold.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new", ReceivedAt: next})
	if f := asFlagOfType(coldFlags); f != nil {
		t.Errorf("cold start fired on a single event: %q", f.Detail)
	}
}

func TestActivitySpikeImportDropsDayStateOlderThanYesterday(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.4"
	old := exportStart.AddDate(0, 0, -3)
	for i := 0; i < 50; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
			ReceivedAt: old.Add(time.Duration(i) * time.Second)})
	}
	// A streak the source earned three days ago, which nothing has
	// observed since.
	src, _ := d.sources.Get(ip)
	src.maturityStreak = 4
	raw := mustExportState(t, d)
	hour := old.Hour()

	t.Run("dropped when the day is older than yesterday", func(t *testing.T) {
		restored := newShippedActivitySpikeDefinition(t, newTestFlagsStore(t), nil, Scope{})
		if err := restored.ImportState(raw, old, exportStart); err != nil {
			t.Fatalf("ImportState: %v", err)
		}
		st, ok := restored.sources.Get(ip)
		if !ok {
			t.Fatal("expected the source entry itself to survive")
		}
		if st.hourDay[hour] != "" || st.hourPeak[hour] != 0 {
			t.Errorf("hour bucket = (%q, %d), want it dropped rather than folded as though yesterday's",
				st.hourDay[hour], st.hourPeak[hour])
		}
		if st.maturityDay != "" || st.maturityStreak != 0 || st.dirtyToday {
			t.Errorf("maturity state = (%q, %d, %v), want it dropped -- days nothing observed cannot be claimed as consistent history",
				st.maturityDay, st.maturityStreak, st.dirtyToday)
		}
	})

	t.Run("kept when the day is yesterday", func(t *testing.T) {
		restored := newShippedActivitySpikeDefinition(t, newTestFlagsStore(t), nil, Scope{})
		nextDay := old.AddDate(0, 0, 1)
		if err := restored.ImportState(raw, old, nextDay); err != nil {
			t.Fatalf("ImportState: %v", err)
		}
		st, _ := restored.sources.Get(ip)
		if st.hourDay[hour] != activityDay(old) || st.hourPeak[hour] == 0 {
			t.Errorf("hour bucket = (%q, %d), want yesterday's peak kept so the next event folds it",
				st.hourDay[hour], st.hourPeak[hour])
		}
		if st.maturityStreak != 4 {
			t.Errorf("maturityStreak = %d, want 4 kept across a single day", st.maturityStreak)
		}
	})
}

func TestLowSlowScanStateRoundTrips(t *testing.T) {
	d := newShippedLowSlowScanDefinition(t, func(RoutedEmission) {}, lowSlowTestParams(), Scope{}, true)
	ip := "198.51.100.7"
	for i := 0; i < 12; i++ {
		d.Evaluate(store.Event{
			SrcIP: ip, DstIP: "192.168.1." + string(rune('a'+i)), DstPort: 1000 + i,
			Action: store.ActionDrop, ConnState: "new",
			ReceivedAt: exportStart.Add(time.Duration(i) * time.Minute),
		})
	}
	now := exportStart.Add(20 * time.Minute)
	raw := mustExportState(t, d)

	restored := newShippedLowSlowScanDefinition(t, func(RoutedEmission) {}, lowSlowTestParams(), Scope{}, true)
	if err := restored.ImportState(raw, now, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	before, _ := d.tracks.Get(ip)
	after, ok := restored.tracks.Get(ip)
	if !ok {
		t.Fatal("expected the source's track to be restored")
	}
	if want, got := before.ports.Count(now, d.window, nil), after.ports.Count(now, d.window, nil); got != want {
		t.Errorf("restored distinct ports = %d, want %d", got, want)
	}
	if want, got := before.hosts.Count(now, d.window, nil), after.hosts.Count(now, d.window, nil); got != want {
		t.Errorf("restored distinct hosts = %d, want %d", got, want)
	}
	wantDrops, wantTotal := before.drops.Ratio(now, d.window)
	gotDrops, gotTotal := after.drops.Ratio(now, d.window)
	if gotDrops != wantDrops || gotTotal != wantTotal {
		t.Errorf("restored drop tally = (%d/%d), want (%d/%d)", gotDrops, gotTotal, wantDrops, wantTotal)
	}
}

func TestDestSpreadStateRoundTrips(t *testing.T) {
	d := newShippedDestSpreadDefinition(t, "outbound_anomaly", func(RoutedEmission) {}, nil, Scope{}, true)
	ip := "192.168.1.50"
	for i := 0; i < 8; i++ {
		d.Evaluate(store.Event{
			SrcIP: ip, DstIP: "203.0.113." + string(rune('1'+i)), DstPort: 443, ConnState: "new",
			ReceivedAt: exportStart.Add(time.Duration(i) * time.Second),
		})
	}
	now := exportStart.Add(time.Minute)
	raw := mustExportState(t, d)

	restored := newShippedDestSpreadDefinition(t, "outbound_anomaly", func(RoutedEmission) {}, nil, Scope{}, true)
	if err := restored.ImportState(raw, now, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	beforeDests, _ := d.dests.Get(ip)
	afterDests, ok := restored.dests.Get(ip)
	if !ok {
		t.Fatal("expected the source's destination ring to be restored")
	}
	if want, got := beforeDests.Count(now, d.window, nil), afterDests.Count(now, d.window, nil); got != want {
		t.Errorf("restored distinct destinations = %d, want %d", got, want)
	}
	beforePairs, _ := d.pairs.Get(ip)
	afterPairs, ok := restored.pairs.Get(ip)
	if !ok {
		t.Fatal("expected the source's (destination, port) pair ring to be restored")
	}
	if want, got := beforePairs.Count(now, d.window, nil), afterPairs.Count(now, d.window, nil); got != want {
		t.Errorf("restored distinct pairs = %d, want %d", got, want)
	}
	// A pair is a struct key, so this is also the round trip that would
	// break first if the ring's value encoding stopped being faithful.
	values := afterPairs.Values(now, d.window, nil)
	if _, ok := values[HostPort{Host: "203.0.113.1", Port: 443}]; !ok {
		t.Errorf("restored pairs = %v, want the exact (host, port) pairs back", values)
	}
}

func TestRuleSpikeStateRoundTrips(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{})
	const rule = "drop-wan"
	for i := 0; i < 30; i++ {
		d.Evaluate(ruleEvt(rule, exportStart.Add(time.Duration(i)*time.Second)))
	}
	now := exportStart.Add(30 * time.Second)
	raw := mustExportState(t, d)

	restoredFlags := newTestFlagsStore(t)
	restored := newShippedRuleSpikeDefinition(t, restoredFlags, ShippedDeps{}, Scope{})
	if err := restored.ImportState(raw, now, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	before, _ := d.hits.Get(rule)
	after, ok := restored.hits.Get(rule)
	if !ok {
		t.Fatal("expected the rule's hit ring to be restored")
	}
	if want, got := before.Count(now, d.window), after.Count(now, restored.window); got != want {
		t.Errorf("restored hit count = %d, want %d", got, want)
	}
	if len(restoredFlags.List()) != 0 {
		t.Errorf("ImportState raised %d flag(s); importing state must never emit", len(restoredFlags.List()))
	}
}

func TestOffHoursStateRoundTripsAndExpiresStaleDays(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.11"
	at := time.Date(2026, 9, 3, 23, 30, 0, 0, time.UTC) // inside the 23-6 window
	for i := 0; i < 4; i++ {
		d.Evaluate(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 22, ConnState: "new",
			ReceivedAt: at.Add(time.Duration(i) * time.Minute)})
	}
	key := offHoursKey(ip, 23)
	raw := mustExportState(t, d)

	t.Run("same day keeps the accumulating count", func(t *testing.T) {
		restored := newShippedOffHoursDefinition(t, newTestFlagsStore(t), nil, Scope{})
		if err := restored.ImportState(raw, at, at.Add(5*time.Minute)); err != nil {
			t.Fatalf("ImportState: %v", err)
		}
		bucket, ok := restored.days.Get(key)
		if !ok {
			t.Fatal("expected the (source, hour) bucket to be restored")
		}
		if bucket.day != "2026-09-03" || bucket.count != 4 {
			t.Errorf("restored bucket = (%q, %d), want (2026-09-03, 4)", bucket.day, bucket.count)
		}
	})

	t.Run("a day older than yesterday is restored empty", func(t *testing.T) {
		restored := newShippedOffHoursDefinition(t, newTestFlagsStore(t), nil, Scope{})
		if err := restored.ImportState(raw, at, at.AddDate(0, 0, 3)); err != nil {
			t.Fatalf("ImportState: %v", err)
		}
		bucket, ok := restored.days.Get(key)
		if !ok {
			t.Fatal("expected the bucket's key to survive so its eviction order does")
		}
		if bucket.day != "" || bucket.count != 0 {
			t.Errorf("restored bucket = (%q, %d), want it empty -- a count from three days ago has no day left to be folded as",
				bucket.day, bucket.count)
		}
	})
}

func TestShippedDefinitionsImportRejectMalformedState(t *testing.T) {
	fs := newTestFlagsStore(t)
	definitions := map[string]Snapshotted{
		"activity_spike":     newShippedActivitySpikeDefinition(t, fs, nil, Scope{}),
		"low_slow_scan":      newShippedLowSlowScanDefinition(t, func(RoutedEmission) {}, lowSlowTestParams(), Scope{}, true),
		"outbound_anomaly":   newShippedDestSpreadDefinition(t, "outbound_anomaly", func(RoutedEmission) {}, nil, Scope{}, true),
		"rule_spike":         newShippedRuleSpikeDefinition(t, fs, ShippedDeps{}, Scope{}),
		"off_hours_activity": newShippedOffHoursDefinition(t, fs, nil, Scope{}),
	}
	for id, d := range definitions {
		if err := d.ImportState([]byte(`{"counts":`), exportStart, exportStart); err == nil {
			t.Errorf("%s: expected a truncated document to be an error, not a crash or a silent success", id)
		}
	}
	if len(fs.List()) != 0 {
		t.Errorf("a failed import raised %d flag(s)", len(fs.List()))
	}
}
