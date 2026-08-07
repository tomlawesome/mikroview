package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// Calls checkOffHoursActivity directly with a hand-controlled clock,
// same technique TestActivitySpikeNeverFiresBeforeMinimumSampleFloor
// uses for host_baseline.go -- sidesteps Observe's connState/settings
// filtering and, more importantly, gives exact control over which
// calendar day each call lands on, which is what sampleDays actually
// counts.
func offHoursAt(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
}

// TestOffHoursNeverFiresWithoutEstablishedSampleDays is this feature's
// core false-positive guard, isolated: a host gets several events at an
// hour it has never once been seen at before. OffHoursMinCount is set
// low enough that the absolute-count floor alone wouldn't block this --
// only the sampleDays gate (0 prior distinct days at this hour) can be
// the reason it doesn't fire.
func TestOffHoursNeverFiresWithoutEstablishedSampleDays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OffHoursMinCount = 3
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.20"
	base := offHoursAt(2024, time.March, 1, 3) // 03:00 -- inside the default 23:00-06:00 window

	for i := 0; i < 5; i++ {
		d.checkOffHoursActivity(w, ip, "", base.Add(time.Duration(i)*time.Second))
	}

	for _, f := range fs.List() {
		if f.Type == flags.TypeOffHoursActivity {
			t.Fatalf("expected no off_hours_activity flag from a single day's first-ever activity at this hour, got %+v", f)
		}
	}
}

// TestOffHoursFlagsGenuineDeviationFromEstablishedHourlyBaseline is the
// distinct, opposite case from the sampleDays test above: 14 distinct
// prior days of a steady, small pattern at 03:00 (this hour's baseline
// is genuinely established), then a real burst on day 15 -- far above
// that baseline, well past every gate. This is the "the whole point of
// this design" pairing the task calls for: one scenario that must not
// fire and one that must, differing only in how much real history backs
// the hour.
func TestOffHoursFlagsGenuineDeviationFromEstablishedHourlyBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OffHoursMinSampleDays = 14
	cfg.OffHoursMinCount = 5
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.21"

	// 14 distinct prior days, one event/day at 03:00 -- a steady,
	// unremarkable pattern that must never flag on its own.
	for i := 0; i < 14; i++ {
		d.checkOffHoursActivity(w, ip, "", offHoursAt(2024, time.March, 1+i, 3))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected the steady warm-up phase to never flag, got %+v", fs.List())
	}

	// Day 15: a genuine burst, far above the established baseline --
	// sampleDays has just cleared OffHoursMinSampleDays (the prior 14
	// days), so this is exactly the moment the gate opens.
	burst := offHoursAt(2024, time.March, 15, 3)
	for i := 0; i < 20; i++ {
		d.checkOffHoursActivity(w, ip, "", burst.Add(time.Duration(i)*time.Millisecond))
	}

	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeOffHoursActivity || list[0].Target != ip {
		t.Fatalf("expected an off_hours_activity flag for %s, got %+v", ip, list)
	}
	if list[0].Confidence == nil || *list[0].Confidence <= 0 || *list[0].Confidence > 100 {
		t.Fatalf("expected a confidence score in (0, 100], got %+v", list[0].Confidence)
	}
	if list[0].Detail == "" {
		t.Error("expected a human-readable detail string explaining why this fired")
	}
}

// TestOffHoursRespectsAbsoluteCountFloorDespiteEstablishedBaseline is
// the issue's other explicitly-required guard: even once an hour's
// sampleDays clears the floor, a near-zero baseline means a tiny count
// can still read as a huge z-score deviation. OffHoursMinCount is an
// independent absolute floor specifically so 2-3 events never fire on
// z-score alone.
func TestOffHoursRespectsAbsoluteCountFloorDespiteEstablishedBaseline(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OffHoursMinSampleDays = 14
	cfg.OffHoursMinCount = 5
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.22"

	for i := 0; i < 14; i++ {
		d.checkOffHoursActivity(w, ip, "", offHoursAt(2024, time.March, 1+i, 3))
	}

	// Day 15: sampleDays now clears the floor, and the near-zero
	// baseline (steady ~1/day) means even 2 events would read as a
	// large z-score deviation -- but OffHoursMinCount=5 must still
	// block it.
	day15 := offHoursAt(2024, time.March, 15, 3)
	d.checkOffHoursActivity(w, ip, "", day15)
	d.checkOffHoursActivity(w, ip, "", day15.Add(time.Millisecond))

	for _, f := range fs.List() {
		if f.Type == flags.TypeOffHoursActivity {
			t.Fatalf("expected the absolute count floor to block a flag from only 2 events, got %+v", f)
		}
	}
}

// TestOffHoursIgnoresActivityOutsideConfiguredWindow proves the
// off-hours window itself gates firing, independent of the statistical
// checks: the same warm-up-then-burst pattern that fires at 03:00 (see
// TestOffHoursFlagsGenuineDeviationFromEstablishedHourlyBaseline) must
// never fire at 14:00 -- outside DefaultConfig's 23:00-06:00 window --
// even though sampleDays/count/z would all otherwise clear.
func TestOffHoursIgnoresActivityOutsideConfiguredWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OffHoursMinSampleDays = 14
	cfg.OffHoursMinCount = 5
	d, fs := newTestDetector(t, cfg)

	w := &sourceWindow{}
	ip := "198.51.100.23"

	for i := 0; i < 14; i++ {
		d.checkOffHoursActivity(w, ip, "", offHoursAt(2024, time.March, 1+i, 14))
	}
	burst := offHoursAt(2024, time.March, 15, 14)
	for i := 0; i < 20; i++ {
		d.checkOffHoursActivity(w, ip, "", burst.Add(time.Duration(i)*time.Millisecond))
	}

	for _, f := range fs.List() {
		if f.Type == flags.TypeOffHoursActivity {
			t.Fatalf("expected 14:00 activity to never flag under the default 23:00-06:00 window, got %+v", f)
		}
	}
}

// TestOffHoursDetectorDisabledSuppressesFlag confirms the detector
// respects its own on/off + scope toggle (like every other detector --
// see settings.go) via the real Observe path, not just the internal
// check function.
func TestOffHoursDetectorDisabledSuppressesFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OffHoursMinSampleDays = 14
	cfg.OffHoursMinCount = 5

	seed := DefaultSettingsMap()
	seed[DetectorOffHoursActivity] = Settings{Enabled: false}
	d, fs := newTestDetectorWithSettings(t, cfg, seed)

	ip := "198.51.100.24"
	for i := 0; i < 14; i++ {
		at := offHoursAt(2024, time.March, 1+i, 3)
		d.Observe(evt(ip, 100, at))
	}
	burst := offHoursAt(2024, time.March, 15, 3)
	for i := 0; i < 20; i++ {
		d.Observe(evt(ip, 100+i, burst.Add(time.Duration(i)*time.Millisecond)))
	}

	for _, f := range fs.List() {
		if f.Type == flags.TypeOffHoursActivity {
			t.Fatalf("expected off_hours_activity to never fire while disabled, got %+v", f)
		}
	}
}

// TestInOffHoursWindowWrapsPastMidnight covers inOffHoursWindow's own
// boundary logic directly, including the degenerate start==end case.
func TestInOffHoursWindowWrapsPastMidnight(t *testing.T) {
	cases := []struct {
		hour, start, end int
		want             bool
	}{
		{3, 23, 6, true},    // wraps past midnight, inside
		{23, 23, 6, true},   // wraps past midnight, at the start boundary
		{5, 23, 6, true},    // wraps past midnight, just before the end boundary
		{6, 23, 6, false},   // end boundary is exclusive
		{14, 23, 6, false},  // daytime, outside a wrapping window
		{10, 9, 17, true},   // non-wrapping window, inside
		{8, 9, 17, false},   // non-wrapping window, before start
		{17, 9, 17, false},  // non-wrapping window, end exclusive
		{12, 12, 12, false}, // degenerate zero-width window: never, not always
	}
	for _, tc := range cases {
		if got := inOffHoursWindow(tc.hour, tc.start, tc.end); got != tc.want {
			t.Errorf("inOffHoursWindow(%d, %d, %d) = %v, want %v", tc.hour, tc.start, tc.end, got, tc.want)
		}
	}
}
