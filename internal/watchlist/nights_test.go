// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"testing"
	"time"
)

// nightlyWindow is 22:00-06:00 UTC, the crossing-midnight shape the design
// calls the normal case.
var nightlyWindow = Window{Start: 22 * 60, End: 6 * 60, Zone: "UTC"}

// watching is an Observation for a process that has been up since well
// before any night these tests fill, with the pathway logged.
func watching(t *testing.T, since string) Observation {
	t.Helper()
	return Observation{Since: mustUTC(t, since), Covered: true}
}

func states(nights []Night) []NightState {
	out := make([]NightState, len(nights))
	for i, n := range nights {
		out[i] = n.State
	}
	return out
}

func sameStates(got, want []NightState) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestKeptMeansPresenceNotSize pins section 3 of the design: any match
// attributed to the entry whose time falls inside the window keeps the
// night. One packet is as good as a thousand, and Count/First are recorded
// anyway so a later size judgement has something to work from.
func TestKeptMeansPresenceNotSize(t *testing.T) {
	var nights []Night
	first := mustUTC(t, "2026-08-31T23:15:00Z")
	nights, notable := RecordMatch(nights, nightlyWindow, first)
	if !notable {
		t.Fatal("the first match of a night should be worth writing down")
	}
	if len(nights) != 1 || nights[0].State != NightKept {
		t.Fatalf("got %+v, want one kept night", nights)
	}
	if !nights[0].Opened.Equal(mustUTC(t, "2026-08-31T22:00:00Z")) {
		t.Errorf("the night is anchored at %s, want the 22:00 opening", nights[0].Opened)
	}
	if !nights[0].First.Equal(first) || nights[0].Count != 1 {
		t.Errorf("got first=%s count=%d, want %s and 1", nights[0].First, nights[0].Count, first)
	}

	// A second match in the same night bumps the count and is not
	// notable -- nothing structural changed, so a caller on the
	// evaluation path can skip the persist.
	later := mustUTC(t, "2026-09-01T02:00:00Z")
	nights, notable = RecordMatch(nights, nightlyWindow, later)
	if notable {
		t.Error("a repeat inside an already-kept night should not be notable")
	}
	if len(nights) != 1 || nights[0].Count != 2 {
		t.Fatalf("got %+v, want one night with a count of 2", nights)
	}
	if !nights[0].First.Equal(first) {
		t.Errorf("First moved to %s; it should stay at the first match", nights[0].First)
	}

	// An earlier match arriving out of order moves First back.
	earlier := mustUTC(t, "2026-08-31T22:05:00Z")
	nights, _ = RecordMatch(nights, nightlyWindow, earlier)
	if !nights[0].First.Equal(earlier) {
		t.Errorf("First = %s, want the earlier match %s", nights[0].First, earlier)
	}
}

func TestRecordMatchOutsideTheWindowChangesNothing(t *testing.T) {
	nights, notable := RecordMatch(nil, nightlyWindow, mustUTC(t, "2026-08-31T12:00:00Z"))
	if notable || len(nights) != 0 {
		t.Fatalf("a midday match wrote %+v; it says nothing about any night", nights)
	}
	// An entry with no window has no nights to keep.
	nights, notable = RecordMatch(nil, Window{}, mustUTC(t, "2026-08-31T23:00:00Z"))
	if notable || len(nights) != 0 {
		t.Fatalf("a windowless entry wrote %+v", nights)
	}
}

// TestFillRecordsAnUnwatchedNightAsNotObserved is the single most
// important rule in issue #680: a night mikroview was not running for is
// "not observed", never "empty". Empty is a claim about the network, and
// this app does not get to make it out of an absence of its own.
func TestFillRecordsAnUnwatchedNightAsNotObserved(t *testing.T) {
	// The process came up at 09:00 on 1 September, having been down for
	// the night that opened at 22:00 on the 31st and closed at 06:00.
	obs := Observation{Since: mustUTC(t, "2026-09-01T09:00:00Z"), Covered: true}
	now := mustUTC(t, "2026-09-02T09:00:00Z")

	nights := FillNights(nil, nightlyWindow, now, obs)
	if len(nights) == 0 {
		t.Fatal("the fill wrote nothing")
	}
	// The night of the 1st opened at 22:00, after the process was up, and
	// nothing matched: that one is honestly empty.
	last := nights[len(nights)-1]
	if !last.Opened.Equal(mustUTC(t, "2026-09-01T22:00:00Z")) {
		t.Fatalf("newest night opened at %s, want 2026-09-01T22:00:00Z", last.Opened)
	}
	if last.State != NightEmpty {
		t.Errorf("the night the process watched throughout is %q, want %q", last.State, NightEmpty)
	}
	// Every night before the process started must be "not observed".
	for _, n := range nights[:len(nights)-1] {
		if n.State != NightUnobserved {
			t.Errorf("the night opening %s is %q, want %q -- mikroview was not running for it", n.Opened, n.State, NightUnobserved)
		}
	}
}

// TestFillRecordsAnUncoveredNightAsNotObserved is the other half of the
// same rule: a watch no firewall rule logs cannot be judged on nightly
// presence at all.
func TestFillRecordsAnUncoveredNightAsNotObserved(t *testing.T) {
	obs := Observation{Since: mustUTC(t, "2026-08-01T00:00:00Z"), Covered: false}
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-02T09:00:00Z"), obs)
	if len(nights) != MaxNights {
		t.Fatalf("got %d nights, want %d", len(nights), MaxNights)
	}
	for _, n := range nights {
		if n.State != NightUnobserved {
			t.Fatalf("night %s is %q, want %q -- nothing was logging this pathway", n.Opened, n.State, NightUnobserved)
		}
	}
}

// TestFillWithoutAnObservationSinceRefusesToClaimEmpty pins the honest
// default: a caller that cannot say when it started watching gets "not
// observed", not "empty".
func TestFillWithoutAnObservationSinceRefusesToClaimEmpty(t *testing.T) {
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-02T09:00:00Z"), Observation{Covered: true})
	for _, n := range nights {
		if n.State != NightUnobserved {
			t.Fatalf("night %s is %q with an unknown watching-since, want %q", n.Opened, n.State, NightUnobserved)
		}
	}
}

// TestFillIsIdempotent pins the lazy, restart-proof contract: filling
// twice writes nothing the second time, because a night is keyed by the
// instant its window opened and written once.
func TestFillIsIdempotent(t *testing.T) {
	obs := watching(t, "2026-08-01T00:00:00Z")
	now := mustUTC(t, "2026-09-02T09:00:00Z")
	first := FillNights(nil, nightlyWindow, now, obs)
	second := FillNights(first, nightlyWindow, now, obs)
	if len(second) != len(first) {
		t.Fatalf("a second fill grew the history from %d to %d", len(first), len(second))
	}
	for i := range first {
		if !first[i].Opened.Equal(second[i].Opened) || first[i].State != second[i].State {
			t.Fatalf("night %d changed on a second fill: %+v then %+v", i, first[i], second[i])
		}
	}
	// A kept night survives a later fill untouched: the fill only ever
	// writes nights nothing matched in.
	kept, _ := RecordMatch(second, nightlyWindow, mustUTC(t, "2026-09-02T23:00:00Z"))
	after := FillNights(kept, nightlyWindow, mustUTC(t, "2026-09-03T09:00:00Z"), obs)
	last := after[len(after)-1]
	if last.State != NightKept || last.Count != 1 {
		t.Errorf("the kept night was overwritten by a later fill: %+v", last)
	}
}

// TestSevenNightCapDropsTheOldest pins the bound that lets this history
// ride inside the definitions blob.
func TestSevenNightCapDropsTheOldest(t *testing.T) {
	obs := watching(t, "2026-08-01T00:00:00Z")
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-08T09:00:00Z"), obs)
	if len(nights) != MaxNights {
		t.Fatalf("got %d nights, want the %d-night cap", len(nights), MaxNights)
	}
	oldest := nights[0].Opened
	if want := mustUTC(t, "2026-09-01T22:00:00Z"); !oldest.Equal(want) {
		t.Fatalf("oldest night opened %s, want %s", oldest, want)
	}

	// One more day: an eighth night arrives, the oldest is dropped, and
	// the history stays exactly seven long and still in order.
	nights = FillNights(nights, nightlyWindow, mustUTC(t, "2026-09-09T09:00:00Z"), obs)
	if len(nights) != MaxNights {
		t.Fatalf("after an eighth night the history is %d long, want %d", len(nights), MaxNights)
	}
	if nights[0].Opened.Equal(oldest) {
		t.Error("the oldest night was kept; it should have been dropped")
	}
	if want := mustUTC(t, "2026-09-02T22:00:00Z"); !nights[0].Opened.Equal(want) {
		t.Errorf("oldest night is now %s, want %s", nights[0].Opened, want)
	}
	if want := mustUTC(t, "2026-09-08T22:00:00Z"); !nights[len(nights)-1].Opened.Equal(want) {
		t.Errorf("newest night is %s, want %s", nights[len(nights)-1].Opened, want)
	}
	for i := 1; i < len(nights); i++ {
		if !nights[i-1].Opened.Before(nights[i].Opened) {
			t.Fatalf("the history is out of order at %d", i)
		}
	}
}

// TestCapDropsTheOldestWhenAMatchArrives covers the other door into the
// history: RecordMatch appending an eighth night.
func TestCapDropsTheOldestWhenAMatchArrives(t *testing.T) {
	obs := watching(t, "2026-08-01T00:00:00Z")
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-08T09:00:00Z"), obs)
	oldest := nights[0].Opened

	nights, notable := RecordMatch(nights, nightlyWindow, mustUTC(t, "2026-09-08T23:00:00Z"))
	if !notable {
		t.Fatal("a match opening a new night should be notable")
	}
	if len(nights) != MaxNights {
		t.Fatalf("history is %d long, want %d", len(nights), MaxNights)
	}
	if nights[0].Opened.Equal(oldest) {
		t.Error("the oldest night survived an eighth arrival")
	}
	if nights[len(nights)-1].State != NightKept {
		t.Errorf("newest night is %q, want %q", nights[len(nights)-1].State, NightKept)
	}
}

// TestFillAfterALongOutageStaysBounded pins that coming back after weeks
// down costs a constant amount of work and writes at most seven nights,
// every one of them honestly "not observed".
func TestFillAfterALongOutageStaysBounded(t *testing.T) {
	obs := Observation{Since: mustUTC(t, "2026-09-30T09:00:00Z"), Covered: true}
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-30T10:00:00Z"), obs)
	if len(nights) != MaxNights {
		t.Fatalf("got %d nights after a long outage, want %d", len(nights), MaxNights)
	}
	for _, n := range nights {
		if n.State != NightUnobserved {
			t.Errorf("night %s is %q, want %q", n.Opened, n.State, NightUnobserved)
		}
	}
}

func TestFillDoesNothingWithoutAWindow(t *testing.T) {
	if got := FillNights(nil, Window{}, mustUTC(t, "2026-09-02T09:00:00Z"), watching(t, "2026-08-01T00:00:00Z")); got != nil {
		t.Fatalf("a windowless entry accrued %+v", got)
	}
}

// TestRingBreaksOnTheRunOfEmptyNights pins section 4: the ring is
// recorded, Since is the close of the first empty window in the current
// run, and the reason is the only one this package records.
func TestRingBreaksOnTheRunOfEmptyNights(t *testing.T) {
	obs := watching(t, "2026-08-01T00:00:00Z")
	nights := FillNights(nil, nightlyWindow, mustUTC(t, "2026-09-04T09:00:00Z"), obs)
	// Keep the first two nights, leave the rest empty.
	nights, _ = RecordMatch(nights, nightlyWindow, nights[0].Opened.Add(time.Hour))
	nights, _ = RecordMatch(nights, nightlyWindow, nights[1].Opened.Add(time.Hour))

	ring := UpdateRing(nights, nightlyWindow)
	if !ring.Broken {
		t.Fatal("a run of empty nights should break the ring")
	}
	if ring.Reason != RingNoMatchInWindow {
		t.Errorf("reason %q, want %q", ring.Reason, RingNoMatchInWindow)
	}
	// The run starts at the third night; Since is its close, six hours
	// after it opened.
	wantSince := nights[2].Opened.Add(8 * time.Hour)
	if !ring.Since.Equal(wantSince) {
		t.Errorf("Since = %s, want the close of the first empty night, %s", ring.Since, wantSince)
	}

	// A match tonight heals it.
	nights, _ = RecordMatch(nights, nightlyWindow, nights[len(nights)-1].Opened.Add(time.Hour))
	if got := UpdateRing(nights, nightlyWindow); got.Broken {
		t.Errorf("the ring is still broken after the most recent night was kept: %+v", got)
	}
}

// TestNotObservedNeitherBreaksNorHealsTheRing pins that a night mikroview
// could not see is not evidence in either direction.
func TestNotObservedNeitherBreaksNorHealsTheRing(t *testing.T) {
	base := mustUTC(t, "2026-09-01T22:00:00Z")
	day := 24 * time.Hour

	// Nothing but unobserved nights: no break, because there is nothing
	// to say.
	only := []Night{
		{Opened: base, State: NightUnobserved},
		{Opened: base.Add(day), State: NightUnobserved},
	}
	if got := UpdateRing(only, nightlyWindow); got.Broken {
		t.Errorf("unobserved nights broke the ring: %+v", got)
	}

	// An unobserved night after an empty one does not heal it, and does
	// not move Since either.
	mixed := []Night{
		{Opened: base, State: NightKept},
		{Opened: base.Add(day), State: NightEmpty},
		{Opened: base.Add(2 * day), State: NightUnobserved},
	}
	ring := UpdateRing(mixed, nightlyWindow)
	if !ring.Broken {
		t.Fatal("an empty night followed by an unobserved one should leave the ring broken")
	}
	if want := base.Add(day).Add(8 * time.Hour); !ring.Since.Equal(want) {
		t.Errorf("Since = %s, want the empty night's close %s", ring.Since, want)
	}

	// An unobserved night before an empty one does not extend the run
	// back past it.
	later := []Night{
		{Opened: base, State: NightUnobserved},
		{Opened: base.Add(day), State: NightEmpty},
	}
	if got := UpdateRing(later, nightlyWindow); !got.Since.Equal(base.Add(day).Add(8 * time.Hour)) {
		t.Errorf("Since = %s, want the empty night's close, not the unobserved one's", got.Since)
	}
}

func TestSummariseNights(t *testing.T) {
	base := mustUTC(t, "2026-09-01T22:00:00Z")
	day := 24 * time.Hour
	got := SummariseNights([]Night{
		{Opened: base, State: NightKept},
		{Opened: base.Add(day), State: NightKept},
		{Opened: base.Add(2 * day), State: NightEmpty},
		{Opened: base.Add(3 * day), State: NightUnobserved},
	})
	if got != (NightSummary{Kept: 2, Empty: 1, Unobserved: 1}) {
		t.Errorf("got %+v", got)
	}
}

// TestNightsAcrossADSTTransitionStayOnePerNight is the DST case as the
// history sees it: seven London nights across the spring transition are
// seven nights, not six or eight, and none of them is duplicated or lost
// because an hour vanished.
func TestNightsAcrossADSTTransitionStayOnePerNight(t *testing.T) {
	london := Window{Start: 0, End: 6 * 60, Zone: "Europe/London"}
	obs := Observation{Since: mustUTC(t, "2026-03-20T00:00:00Z"), Covered: true}
	nights := FillNights(nil, london, mustUTC(t, "2026-04-01T12:00:00Z"), obs)
	if len(nights) != MaxNights {
		t.Fatalf("got %d nights across the transition, want %d", len(nights), MaxNights)
	}
	loc, err := london.Location()
	if err != nil {
		t.Fatalf("loading Europe/London: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range nights {
		if n.State != NightEmpty {
			t.Errorf("night %s is %q, want %q", n.Opened, n.State, NightEmpty)
		}
		// Keyed on the local date, which is what a night is anchored to.
		// In UTC the 29th's night and the 30th's both start on the 29th
		// (00:00 BST on the 30th is 23:00 UTC on the 29th), which is
		// precisely the drift the zone exists to keep out of the model.
		day := n.Opened.In(loc).Format("2006-01-02")
		if seen[day] {
			t.Errorf("two nights anchored to %s", day)
		}
		seen[day] = true
	}
	if !sameStates(states(nights), []NightState{NightEmpty, NightEmpty, NightEmpty, NightEmpty, NightEmpty, NightEmpty, NightEmpty}) {
		t.Errorf("states %v", states(nights))
	}
	// The 29th's night is the short one, and it is still exactly one night.
	for _, n := range nights {
		if n.Opened.Equal(mustUTC(t, "2026-03-29T00:00:00Z")) {
			return
		}
	}
	t.Error("the spring-forward night is missing from the history")
}
