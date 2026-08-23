// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func newShippedOffHoursDefinition(t *testing.T, fs *flags.Store, params Params, scope Scope) *offHoursDefinition {
	t.Helper()
	full := Params{
		"startHour":     23,
		"endHour":       6,
		"minSampleDays": 14,
		"minCount":      5,
		"updateCadence": "perEvent",
	}
	for k, v := range params {
		full[k] = v
	}
	def := Definition{
		ID:          "off_hours_activity",
		Name:        "Off-hours activity",
		Intent:      IntentDetection,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Scope:       scope,
		Params:      full,
		ParamSchema: OffHoursActivityParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(off_hours_activity): %v", err)
	}
	d := built.(*offHoursDefinition)
	d.SetSink(FlagsSink(fs))
	return d
}

func ohFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeOffHoursActivity {
			return &f
		}
	}
	return nil
}

// offHoursAt is internal/detect/off_hours_test.go's helper of the same
// name: a server-local instant at a given calendar day and clock hour.
// Local, not UTC, because the definition reads now.Hour() and formats
// now as a server-local calendar day -- the same thing internal/detect
// did, so a test in a non-UTC zone exercises the same code path an
// operator in one does.
func offHoursAt(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.Local)
}

func ohEvt(srcIP, country string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: "192.168.1.1", DstPort: dstPort, SrcCountry: country, ReceivedAt: at}
}

// assertFloatSigmaTail is internal/detect/characterization_test.go's
// helper of the same name: asserts s is a %.1f-formatted float followed
// by "σ" and wantTail. The digits themselves are a function of this
// test's own input sequence rather than a product contract, so the shape
// is pinned rather than the value -- see that file's header comment.
func assertFloatSigmaTail(t *testing.T, s, wantTail string) {
	t.Helper()
	if !strings.HasSuffix(s, "σ"+wantTail) {
		t.Errorf("expected %q to end in a σ figure followed by %q", s, wantTail)
		return
	}
	head := strings.TrimSuffix(s, "σ"+wantTail)
	if head == "" {
		t.Errorf("expected a %%.1f-formatted float before σ in %q", s)
		return
	}
	if _, err := fmt.Sscanf(head, "%f", new(float64)); err != nil {
		t.Errorf("expected a %%.1f-formatted float before σ in %q: %v", s, err)
	}
}

// TestShippedOffHours_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationOffHours_FieldsRefireClearRevive, moved. Every
// pinned value is unchanged, including the boundary landing exactly on
// minCount.
//
// The warm-up shape matters and is preserved: fourteen distinct prior
// days of one event each at 03:00. A day's contribution is folded only
// on the *next* day's first event at that hour, so the baseline and
// variance are fixed for the whole of day 15 -- count is the only thing
// moving, which is what makes the boundary search below deterministic
// rather than something this test has to hand-derive.
func TestShippedOffHours_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.30"

	for i := 0; i < 14; i++ {
		d.Evaluate(ohEvt(ip, "GB", 100, offHoursAt(2024, time.March, 1+i, 3)))
	}
	if got := ohFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag from the 14-day steady warm-up, got %+v", got)
	}

	const minCount = 5
	day15 := offHoursAt(2024, time.March, 15, 3)
	boundary := 0
	for i := 1; i <= minCount+5; i++ {
		d.Evaluate(ohEvt(ip, "GB", 100+i, day15.Add(time.Duration(i)*time.Millisecond)))
		if ohFlagOfType(fs) != nil {
			boundary = i
			break
		}
	}
	if boundary == 0 {
		t.Fatalf("expected a flag within %d events on day 15, got none; flags=%+v", minCount+5, fs.List())
	}
	if boundary != minCount {
		t.Errorf("boundary event = %d, want %d (minCount is the binding gate given this warm-up's tiny baseline)", boundary, minCount)
	}

	f := ohFlagOfType(fs)
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if f.Country != "GB" {
		t.Errorf("Country = %q, want GB", f.Country)
	}
	wantPrefix := fmt.Sprintf("%d events at 03:00 vs a baseline of ", boundary)
	if !strings.HasPrefix(f.Detail, wantPrefix) {
		t.Errorf("Detail = %q, want prefix %q", f.Detail, wantPrefix)
	}
	wantMid := " for this host at this hour (14 days of history, "
	idx := strings.Index(f.Detail, wantMid)
	if idx < 0 {
		t.Errorf("Detail = %q, want to contain %q", f.Detail, wantMid)
	} else {
		assertFloatSigmaTail(t, f.Detail[idx+len(wantMid):], " above normal)")
	}
	if f.Confidence == nil || *f.Confidence <= 0 || *f.Confidence > 100 {
		t.Errorf("Confidence = %v, want a value in (0, 100]", f.Confidence)
	}
	if len(f.Evidence.Ports) != 0 || len(f.Evidence.Hosts) != 0 || f.Evidence.NAT != nil {
		t.Errorf("Evidence = %+v, want the zero value", f.Evidence)
	}
	if f.Count != 1 {
		t.Errorf("Count = %d, want 1", f.Count)
	}

	// Re-fire on the next event of the same evening.
	d.Evaluate(ohEvt(ip, "GB", 200, day15.Add(time.Duration(boundary+1)*time.Millisecond)))
	f2 := ohFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, day15.Add(time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	reviveAt := day15.Add(2 * time.Minute)
	d.Evaluate(ohEvt(ip, "GB", 300, reviveAt))
	f3 := ohFlagOfType(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
}

// TestShippedOffHoursNeverFiresWithoutEstablishedSampleDays is
// internal/detect/off_hours_test.go's test of the same name: the
// distinct-prior-days floor is what makes "one busy night" structurally
// incapable of firing, rather than merely unlikely.
func TestShippedOffHoursNeverFiresWithoutEstablishedSampleDays(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.30"

	// One night, enormously busy, at an off-hours hour.
	night := offHoursAt(2024, time.March, 1, 3)
	for i := 0; i < 500; i++ {
		d.Evaluate(ohEvt(ip, "", 100+i, night.Add(time.Duration(i)*time.Millisecond)))
	}
	if got := ohFlagOfType(fs); got != nil {
		t.Fatalf("expected one busy night with no prior history never to fire, got %+v", got)
	}
}

// TestShippedOffHoursRespectsAbsoluteCountFloor is
// internal/detect/off_hours_test.go's
// TestOffHoursRespectsAbsoluteCountFloorDespiteEstablishedBaseline: a
// host that has never been seen at an hour has a near-zero baseline, so
// a couple of events there would read as a huge deviation by z-score
// alone. minCount is the floor that stops that.
func TestShippedOffHoursRespectsAbsoluteCountFloor(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.30"

	for i := 0; i < 14; i++ {
		d.Evaluate(ohEvt(ip, "", 100, offHoursAt(2024, time.March, 1+i, 3)))
	}
	day15 := offHoursAt(2024, time.March, 15, 3)
	// One event under minCount(5), against a tiny baseline: a huge
	// z-score, and still no flag.
	for i := 1; i < 5; i++ {
		d.Evaluate(ohEvt(ip, "", 100+i, day15.Add(time.Duration(i)*time.Millisecond)))
	}
	if got := ohFlagOfType(fs); got != nil {
		t.Fatalf("expected fewer than minCount events never to fire however large the z-score, got %+v", got)
	}
}

// TestShippedOffHoursIgnoresActivityOutsideConfiguredWindow is
// internal/detect/off_hours_test.go's test of the same name -- and the
// half of it that matters most is the second: history for the excluded
// hour is still accumulated, which is what makes widening the window
// later a change that keeps its history rather than starting over.
func TestShippedOffHoursIgnoresActivityOutsideConfiguredWindow(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	ip := "198.51.100.30"

	// 14:00 is outside the default 23:00-06:00 window.
	for i := 0; i < 14; i++ {
		d.Evaluate(ohEvt(ip, "", 100, offHoursAt(2024, time.March, 1+i, 14)))
	}
	day15 := offHoursAt(2024, time.March, 15, 14)
	for i := 1; i <= 50; i++ {
		d.Evaluate(ohEvt(ip, "", 100+i, day15.Add(time.Duration(i)*time.Millisecond)))
	}
	if got := ohFlagOfType(fs); got != nil {
		t.Fatalf("expected activity outside the configured window never to fire, got %+v", got)
	}

	// The hour's baseline was tracked anyway.
	bl := d.baselines.get(offHoursKey(ip, 14), day15)
	snap := bl.Snapshot(day15)
	if snap.Samples < 14 {
		t.Errorf("Samples for the excluded hour = %d, want >= 14 -- history must accumulate for every hour, not just firing ones", snap.Samples)
	}
}

// TestShippedOffHoursDisabledIsInert is
// internal/detect/off_hours_test.go's
// TestOffHoursDetectorDisabledSuppressesFlag.
func TestShippedOffHoursDisabledIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})
	d.def.Enabled = false
	ip := "198.51.100.30"

	for i := 0; i < 14; i++ {
		d.Evaluate(ohEvt(ip, "", 100, offHoursAt(2024, time.March, 1+i, 3)))
	}
	day15 := offHoursAt(2024, time.March, 15, 3)
	for i := 1; i <= 50; i++ {
		d.Evaluate(ohEvt(ip, "", 100+i, day15.Add(time.Duration(i)*time.Millisecond)))
	}
	if got := ohFlagOfType(fs); got != nil {
		t.Fatalf("expected a disabled definition never to fire, got %+v", got)
	}
	if d.days.Len() != 0 {
		t.Errorf("expected a disabled definition to accumulate no state at all, got %d key(s)", d.days.Len())
	}
}

// TestInOffHoursWindowWrapsPastMidnight is
// internal/detect/off_hours_test.go's test of the same name, moved
// unchanged -- including the deliberate start==end case, where a
// degenerate window disables the definition rather than matching
// everything.
func TestInOffHoursWindowWrapsPastMidnight(t *testing.T) {
	cases := []struct {
		hour, start, end int
		want             bool
	}{
		{23, 23, 6, true}, {0, 23, 6, true}, {5, 23, 6, true},
		{6, 23, 6, false}, {12, 23, 6, false}, {22, 23, 6, false},
		{2, 1, 5, true}, {0, 1, 5, false}, {5, 1, 5, false},
		{3, 4, 4, false}, {4, 4, 4, false},
	}
	for _, c := range cases {
		if got := inOffHoursWindow(c.hour, c.start, c.end); got != c.want {
			t.Errorf("inOffHoursWindow(%d, %d, %d) = %v, want %v", c.hour, c.start, c.end, got, c.want)
		}
	}
}

// TestShippedOffHoursIsNonReplayable pins the classification and the
// reason. #403 names "floor-exceeds-corpus" as one of the three honest
// non-replayable shapes, and a fourteen-distinct-day floor against an
// in-memory window measured in minutes is exactly it -- a replay could
// only ever return a confident zero, which is the dishonesty the
// contract exists to rule out.
func TestShippedOffHoursIsNonReplayable(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedOffHoursDefinition(t, fs, nil, Scope{})

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify off_hours -- it implements neither Replayable nor NonReplayable, or both")
	}
	if receiptCapable {
		t.Fatal("off_hours classified as replayable; its history floor structurally exceeds any in-memory corpus")
	}
	for _, want := range []string{"14", "distinct prior days", "minutes"} {
		if !strings.Contains(reason, want) {
			t.Errorf("NonReplayableReason %q does not mention %q -- the reason has to say why, not just that", reason, want)
		}
	}
}
