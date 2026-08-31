// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"encoding/json"
	"testing"
	"time"
)

func mustUTC(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v.UTC()
}

// TestWindowCrossingMidnightIsOneNight pins the case the design calls the
// normal one rather than the edge: End <= Start runs the window into the
// following date, as a single occurrence anchored to the date it opened.
func TestWindowCrossingMidnightIsOneNight(t *testing.T) {
	w := Window{Start: 22 * 60, End: 6 * 60, Zone: "UTC"}
	if !w.Defined() {
		t.Fatal("a 22:00-06:00 window should be defined")
	}

	// 23:30 on the 31st and 01:30 on the 1st are the same night.
	late := mustUTC(t, "2026-08-31T23:30:00Z")
	early := mustUTC(t, "2026-09-01T01:30:00Z")
	a, ok := w.OccurrenceAt(late)
	if !ok {
		t.Fatal("23:30 should fall inside the night that opened at 22:00")
	}
	b, ok := w.OccurrenceAt(early)
	if !ok {
		t.Fatal("01:30 should still be the previous date's night")
	}
	if !a.Open.Equal(b.Open) {
		t.Fatalf("the two instants landed in different nights: %s vs %s", a.Open, b.Open)
	}
	if want := mustUTC(t, "2026-08-31T22:00:00Z"); !a.Open.Equal(want) {
		t.Errorf("night opened at %s, want %s", a.Open, want)
	}
	if want := mustUTC(t, "2026-09-01T06:00:00Z"); !a.Close.Equal(want) {
		t.Errorf("night closed at %s, want %s", a.Close, want)
	}

	// Half-open: the closing instant itself is outside.
	if _, ok := w.OccurrenceAt(a.Close); ok {
		t.Error("06:00:00 exactly should be outside a [22:00, 06:00) window")
	}
	if _, ok := w.OccurrenceAt(a.Close.Add(-time.Nanosecond)); !ok {
		t.Error("the instant before the close should be inside")
	}
	// And the opening instant is inside.
	if _, ok := w.OccurrenceAt(a.Open); !ok {
		t.Error("22:00:00 exactly should be inside a [22:00, 06:00) window")
	}
	// 12:00, between two nights, belongs to neither.
	if _, ok := w.OccurrenceAt(mustUTC(t, "2026-09-01T12:00:00Z")); ok {
		t.Error("midday is outside a 22:00-06:00 window")
	}
}

// TestWindowDaysFilterOnTheOpeningDate pins that Days is read against the
// date the window opened, never the date it closed -- so a Saturday-night
// watch is one Saturday night, not a Saturday fragment plus a Sunday one.
func TestWindowDaysFilterOnTheOpeningDate(t *testing.T) {
	// 2026-08-29 is a Saturday; 2026-08-30 is a Sunday.
	w := Window{Start: 22 * 60, End: 6 * 60, Days: []time.Weekday{time.Saturday}, Zone: "UTC"}

	sat := mustUTC(t, "2026-08-29T23:00:00Z")
	o, ok := w.OccurrenceAt(sat)
	if !ok {
		t.Fatal("Saturday 23:00 should be inside the Saturday night")
	}
	if want := mustUTC(t, "2026-08-30T06:00:00Z"); !o.Close.Equal(want) {
		t.Errorf("the Saturday night closed at %s, want %s -- it runs into Sunday", o.Close, want)
	}
	// Sunday 02:00 is still the Saturday night.
	if o2, ok := w.OccurrenceAt(mustUTC(t, "2026-08-30T02:00:00Z")); !ok || !o2.Open.Equal(o.Open) {
		t.Error("Sunday 02:00 should still be Saturday's night")
	}
	// Sunday 23:00 is not a night at all: Sunday is not in Days.
	if _, ok := w.OccurrenceAt(mustUTC(t, "2026-08-30T23:00:00Z")); ok {
		t.Error("Sunday 23:00 should open no night when Days is Saturday only")
	}
}

// TestWindowHoldsItsClockTimeAcrossDST is the whole reason the zone is
// stored rather than an offset. Europe/London's 2026 transitions are
// 29 March (clocks forward at 01:00 GMT) and 25 October (back at 02:00
// BST). A 00:00-06:00 window means midnight to six on both dates; what
// changes is how long that is in real time, and where it sits in UTC.
//
// A fixed UTC window would have run 00:00Z-06:00Z on both, which is an
// hour adrift of the operator's night for half the year -- the silent
// drift that would start reporting empty nights every spring.
func TestWindowHoldsItsClockTimeAcrossDST(t *testing.T) {
	w := Window{Start: 0, End: 6 * 60, Zone: "Europe/London"}

	// Spring forward: 00:00 GMT to 06:00 BST is five real hours.
	spring, ok := w.OccurrenceAt(mustUTC(t, "2026-03-29T00:30:00Z"))
	if !ok {
		t.Fatal("00:30 UTC on the spring-forward date should be inside the window")
	}
	if want := mustUTC(t, "2026-03-29T00:00:00Z"); !spring.Open.Equal(want) {
		t.Errorf("spring night opened at %s, want %s", spring.Open, want)
	}
	if want := mustUTC(t, "2026-03-29T05:00:00Z"); !spring.Close.Equal(want) {
		t.Errorf("spring night closed at %s, want %s (06:00 BST)", spring.Close, want)
	}
	if got := spring.Close.Sub(spring.Open); got != 5*time.Hour {
		t.Errorf("the spring-forward night lasted %s, want 5h", got)
	}
	// 05:30 UTC is 06:30 BST -- past the close, and a naive UTC window
	// would still have called it inside.
	if _, ok := w.OccurrenceAt(mustUTC(t, "2026-03-29T05:30:00Z")); ok {
		t.Error("06:30 local is outside a 00:00-06:00 window; a fixed UTC window would have said otherwise")
	}

	// Autumn: 00:00 BST to 06:00 GMT is seven real hours.
	autumn, ok := w.OccurrenceAt(mustUTC(t, "2026-10-25T00:00:00Z"))
	if !ok {
		t.Fatal("00:00 UTC (01:00 BST) on the autumn date should be inside the window")
	}
	if want := mustUTC(t, "2026-10-24T23:00:00Z"); !autumn.Open.Equal(want) {
		t.Errorf("autumn night opened at %s, want %s (00:00 BST)", autumn.Open, want)
	}
	if want := mustUTC(t, "2026-10-25T06:00:00Z"); !autumn.Close.Equal(want) {
		t.Errorf("autumn night closed at %s, want %s (06:00 GMT)", autumn.Close, want)
	}
	if got := autumn.Close.Sub(autumn.Open); got != 7*time.Hour {
		t.Errorf("the autumn night lasted %s, want 7h", got)
	}
	// 05:30 UTC is 05:30 GMT, still inside -- the mirror of the spring case.
	if _, ok := w.OccurrenceAt(mustUTC(t, "2026-10-25T05:30:00Z")); !ok {
		t.Error("05:30 local on the autumn date should be inside the window")
	}
}

// TestWindowCrossingMidnightAcrossDST is the two features together: a
// 22:00-06:00 window that opens the evening before a spring-forward.
func TestWindowCrossingMidnightAcrossDST(t *testing.T) {
	w := Window{Start: 22 * 60, End: 6 * 60, Zone: "Europe/London"}
	o, ok := w.OccurrenceAt(mustUTC(t, "2026-03-28T23:00:00Z"))
	if !ok {
		t.Fatal("23:00 on the evening before the transition should be inside")
	}
	if want := mustUTC(t, "2026-03-28T22:00:00Z"); !o.Open.Equal(want) {
		t.Errorf("opened at %s, want %s", o.Open, want)
	}
	if want := mustUTC(t, "2026-03-29T05:00:00Z"); !o.Close.Equal(want) {
		t.Errorf("closed at %s, want %s (06:00 BST)", o.Close, want)
	}
	if got := o.Close.Sub(o.Open); got != 7*time.Hour {
		t.Errorf("the night lasted %s, want 7h -- an hour was skipped", got)
	}
}

// TestWindowZoneIsTheOnlyLocalTimeConcept pins that an empty zone is UTC,
// which is what every other timestamp in this codebase is in.
func TestWindowZoneDefaultsToUTC(t *testing.T) {
	w := Window{Start: 0, End: 6 * 60}
	loc, err := w.Location()
	if err != nil {
		t.Fatalf("resolving an empty zone: %v", err)
	}
	if loc != time.UTC {
		t.Errorf("an empty zone resolved to %v, want UTC", loc)
	}
	o, ok := w.OccurrenceAt(mustUTC(t, "2026-03-29T03:00:00Z"))
	if !ok {
		t.Fatal("03:00 UTC should be inside an unzoned 00:00-06:00 window")
	}
	if want := mustUTC(t, "2026-03-29T06:00:00Z"); !o.Close.Equal(want) {
		t.Errorf("an unzoned window closed at %s, want %s -- no DST applies", o.Close, want)
	}
}

func TestWindowClosedSinceIsOldestFirstAndBounded(t *testing.T) {
	w := Window{Start: 22 * 60, End: 6 * 60, Zone: "UTC"}
	now := mustUTC(t, "2026-09-10T12:00:00Z")

	occ := w.ClosedSince(time.Time{}, now, MaxNights)
	if len(occ) != MaxNights {
		t.Fatalf("got %d occurrences, want %d", len(occ), MaxNights)
	}
	for i := 1; i < len(occ); i++ {
		if !occ[i-1].Open.Before(occ[i].Open) {
			t.Fatalf("occurrences are not oldest-first at %d: %s then %s", i, occ[i-1].Open, occ[i].Open)
		}
	}
	// The newest closed one is the night that opened on the 9th.
	if want := mustUTC(t, "2026-09-09T22:00:00Z"); !occ[len(occ)-1].Open.Equal(want) {
		t.Errorf("newest occurrence opened at %s, want %s", occ[len(occ)-1].Open, want)
	}
	for _, o := range occ {
		if o.Close.After(now) {
			t.Errorf("occurrence closing at %s has not closed yet at %s", o.Close, now)
		}
	}

	// after excludes everything already recorded.
	after := occ[len(occ)-2].Open
	rest := w.ClosedSince(after, now, MaxNights)
	if len(rest) != 1 || !rest[0].Open.Equal(occ[len(occ)-1].Open) {
		t.Errorf("ClosedSince(after the second-newest) returned %d occurrences, want just the newest", len(rest))
	}
	// Idempotence: nothing left once the newest is recorded.
	if got := w.ClosedSince(occ[len(occ)-1].Open, now, MaxNights); len(got) != 0 {
		t.Errorf("ClosedSince(after the newest) returned %d occurrences, want none", len(got))
	}
}

func TestWindowClosedSinceWalksPastSkippedDays(t *testing.T) {
	// A Saturday-only window: seven nights is seven weeks of dates, and
	// the walk has to reach back through them.
	w := Window{Start: 22 * 60, End: 6 * 60, Days: []time.Weekday{time.Saturday}, Zone: "UTC"}
	occ := w.ClosedSince(time.Time{}, mustUTC(t, "2026-09-10T12:00:00Z"), MaxNights)
	if len(occ) != MaxNights {
		t.Fatalf("got %d Saturday nights, want %d", len(occ), MaxNights)
	}
	for _, o := range occ {
		if got := o.Open.Weekday(); got != time.Saturday {
			t.Errorf("a night opened on %s, want Saturday", got)
		}
	}
	if got := occ[len(occ)-1].Open.Sub(occ[len(occ)-2].Open); got != 7*24*time.Hour {
		t.Errorf("consecutive Saturday nights are %s apart, want 168h", got)
	}
}

func TestWindowValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    Window
		ok   bool
	}{
		{"zero is the absence of a window, not an invalid one", Window{}, true},
		{"a plain window", Window{Start: 0, End: 6 * 60}, true},
		{"a real zone", Window{Start: 0, End: 6 * 60, Zone: "Europe/London"}, true},
		{"a made-up zone", Window{Start: 0, End: 6 * 60, Zone: "Middle/Earth"}, false},
		{"a start past midnight", Window{Start: MinutesPerDay, End: 60}, false},
		{"a negative end", Window{Start: 60, End: -1}, false},
		{"a day that is not a day", Window{Start: 0, End: 60, Days: []time.Weekday{9}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.w.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

func TestValidateEntryRejectsAnUnloadableZone(t *testing.T) {
	e := Entry{ID: "e1", Ports: []int{22}, Window: Window{Start: 0, End: 6 * 60, Zone: "Middle/Earth"}}
	if err := ValidateEntry(e); err == nil {
		t.Fatal("an entry with an unloadable zone should be refused, not stored to drift silently")
	}
	e.Window.Zone = "Europe/London"
	if err := ValidateEntry(e); err != nil {
		t.Fatalf("a real zone should be accepted: %v", err)
	}
}

func TestClockRoundTripsAsHHMM(t *testing.T) {
	w := Window{Start: 22*60 + 30, End: 6 * 60, Days: []time.Weekday{time.Friday}, Zone: "Europe/London"}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if got, want := string(b), `{"start":"22:30","end":"06:00","days":[5],"zone":"Europe/London"}`; got != want {
		t.Fatalf("marshalled to %s, want %s", got, want)
	}
	var back Window
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if back.Start != w.Start || back.End != w.End || back.Zone != w.Zone || len(back.Days) != 1 {
		t.Errorf("round-tripped to %+v, want %+v", back, w)
	}
}

func TestParseClockIsStrict(t *testing.T) {
	for _, bad := range []string{"6", "6:00", "24:00", "22:60", "", "22:0a", "220:0"} {
		if c, err := ParseClock(bad); err == nil {
			t.Errorf("ParseClock(%q) = %v, want an error", bad, c)
		}
	}
	c, err := ParseClock("22:30")
	if err != nil || c != 22*60+30 {
		t.Errorf("ParseClock(\"22:30\") = %v, %v", c, err)
	}
	if got := Clock(0).String(); got != "00:00" {
		t.Errorf("Clock(0) rendered %q, want 00:00", got)
	}
}
