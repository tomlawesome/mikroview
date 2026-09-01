// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// The window and the nightly history live in the definitions blob (#680):
// a field addition, not a schema change, so the proof that matters is a
// real file-backed round trip -- the same shape
// TestObserveAndPermittedSurviveRestart takes for the observe state.

func nightWindow() watchlist.Window {
	return watchlist.Window{Start: 22 * 60, End: 6 * 60, Zone: "Europe/London"}
}

// sameWindow compares two windows; Window holds a slice, so it is not
// comparable with ==.
func sameWindow(a, b watchlist.Window) bool {
	if a.Start != b.Start || a.End != b.End || a.Zone != b.Zone || len(a.Days) != len(b.Days) {
		return false
	}
	for i := range a.Days {
		if a.Days[i] != b.Days[i] {
			return false
		}
	}
	return true
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v.UTC()
}

func mustGetEntry(t *testing.T, s *DefinitionsStore, id string) watchlist.Entry {
	t.Helper()
	e, ok, err := s.GetExpectation(id)
	if err != nil || !ok {
		t.Fatalf("GetExpectation(%q) = ok %v, err %v", id, ok, err)
	}
	return e
}

// TestWindowAndNightsSurviveARestart pins the whole persistence claim: the
// window an operator set and the nights recorded against it come back out
// of the definitions document unchanged, with no migration run.
func TestWindowAndNightsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	s, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	want := watchlist.Entry{
		ID:     "e1",
		Name:   "the nas",
		Ports:  []int{445},
		Window: nightWindow(),
		Nights: []watchlist.Night{
			{Opened: at(t, "2026-08-30T21:00:00Z"), State: watchlist.NightKept, First: at(t, "2026-08-30T22:14:00Z"), Count: 12},
			{Opened: at(t, "2026-08-31T21:00:00Z"), State: watchlist.NightEmpty},
			{Opened: at(t, "2026-09-01T21:00:00Z"), State: watchlist.NightUnobserved},
		},
		Ring: watchlist.Ring{Broken: true, Since: at(t, "2026-09-01T05:00:00Z"), Reason: watchlist.RingNoMatchInWindow},
	}
	if err := s.UpsertExpectation(want); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("closing: %v", err)
	}

	reopened, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })

	got := mustGetEntry(t, reopened, "e1")
	if !sameWindow(got.Window, want.Window) {
		t.Errorf("window came back as %+v, want %+v", got.Window, want.Window)
	}
	if len(got.Nights) != len(want.Nights) {
		t.Fatalf("got %d nights, want %d", len(got.Nights), len(want.Nights))
	}
	for i := range want.Nights {
		if !got.Nights[i].Opened.Equal(want.Nights[i].Opened) ||
			got.Nights[i].State != want.Nights[i].State ||
			got.Nights[i].Count != want.Nights[i].Count ||
			!got.Nights[i].First.Equal(want.Nights[i].First) {
			t.Errorf("night %d came back as %+v, want %+v", i, got.Nights[i], want.Nights[i])
		}
	}
	if got.Ring.Broken != want.Ring.Broken || got.Ring.Reason != want.Ring.Reason || !got.Ring.Since.Equal(want.Ring.Since) {
		t.Errorf("ring came back as %+v, want %+v", got.Ring, want.Ring)
	}
}

// TestAnEntryWithoutAWindowStoresNothingExtra pins that this is a field
// addition nobody pays for: an entry with no window carries no new params
// at all, so every existing definition's stored bytes are what they were.
func TestAnEntryWithoutAWindowStoresNothingExtra(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	sd, ok := s.Get("e1")
	if !ok {
		t.Fatal("the entry is missing")
	}
	for _, name := range []string{"windowJSON", "nightsJSON", "ringJSON"} {
		if _, present := sd.Definition.Params[name]; present {
			t.Errorf("param %q was written for an entry with no window", name)
		}
	}
	if got := mustGetEntry(t, s, "e1"); got.Window.Defined() {
		t.Errorf("an entry with no window came back with %+v", got.Window)
	}
}

// TestRecordWatchNightKeepsTheNightAMatchLandedIn drives the evaluation
// path's door.
func TestRecordWatchNightKeepsTheNightAMatchLandedIn(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	// 2026-09-01 23:30 BST is 22:30 UTC, inside the night that opened at
	// 22:00 BST (21:00 UTC).
	s.RecordWatchNight("e1", at(t, "2026-09-01T22:30:00Z"))
	got := mustGetEntry(t, s, "e1")
	if len(got.Nights) == 0 {
		t.Fatal("no night was recorded")
	}
	last := got.Nights[len(got.Nights)-1]
	if last.State != watchlist.NightKept || last.Count != 1 {
		t.Fatalf("newest night is %+v, want one kept match", last)
	}
	if want := at(t, "2026-09-01T21:00:00Z"); !last.Opened.Equal(want) {
		t.Errorf("the night is anchored at %s, want %s (22:00 BST)", last.Opened, want)
	}
	if got.Ring.Broken {
		t.Errorf("the ring is broken on a kept night: %+v", got.Ring)
	}

	// A match outside every window records nothing new.
	before := len(got.Nights)
	s.RecordWatchNight("e1", at(t, "2026-09-02T12:00:00Z"))
	if now := mustGetEntry(t, s, "e1"); len(now.Nights) != before {
		t.Errorf("a midday match changed the history: %+v", now.Nights)
	}

	// An unknown id is a silent no-op, not a panic or an error.
	s.RecordWatchNight("nope", at(t, "2026-09-02T22:30:00Z"))
}

// TestFillWatchNightsRecordsEmptyOnlyWhereItCanBeSaid is the rule the
// whole issue turns on, at the store level: a covered entry the process
// watched throughout gets "empty"; an entry nothing logs gets "not
// observed"; and neither ever gets the other's answer.
func TestFillWatchNightsRecordsEmptyOnlyWhereItCanBeSaid(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	for _, id := range []string{"covered", "dark"} {
		if err := s.UpsertExpectation(watchlist.Entry{ID: id, Ports: []int{445}, Window: nightWindow()}); err != nil {
			t.Fatalf("UpsertExpectation(%q): %v", id, err)
		}
	}

	changed := s.FillWatchNights(at(t, "2026-09-08T12:00:00Z"), map[string]bool{"covered": true})
	if changed != 2 {
		t.Fatalf("FillWatchNights changed %d entries, want 2", changed)
	}

	cov := mustGetEntry(t, s, "covered")
	if len(cov.Nights) != watchlist.MaxNights {
		t.Fatalf("the covered entry has %d nights, want %d", len(cov.Nights), watchlist.MaxNights)
	}
	for _, n := range cov.Nights {
		if n.State != watchlist.NightEmpty {
			t.Errorf("covered night %s is %q, want %q", n.Opened, n.State, watchlist.NightEmpty)
		}
	}
	if !cov.Ring.Broken || cov.Ring.Reason != watchlist.RingNoMatchInWindow {
		t.Errorf("seven empty nights left the ring at %+v", cov.Ring)
	}

	dark := mustGetEntry(t, s, "dark")
	for _, n := range dark.Nights {
		if n.State != watchlist.NightUnobserved {
			t.Fatalf("a night on an entry nothing logs is %q, want %q -- that would be mikroview's own blind spot reported as silence on the network", n.State, watchlist.NightUnobserved)
		}
	}
	if dark.Ring.Broken {
		t.Errorf("unobserved nights broke the ring: %+v", dark.Ring)
	}

	// Idempotent: a second fill at the same instant changes nothing.
	if changed := s.FillWatchNights(at(t, "2026-09-08T12:00:00Z"), map[string]bool{"covered": true}); changed != 0 {
		t.Errorf("a repeat fill changed %d entries, want 0", changed)
	}
}

// TestFillWatchNightsAfterADowntimeNightRecordsNotObserved is the
// restart case end to end: the process was down for the night, so the
// night is "not observed" -- never "empty".
func TestFillWatchNightsAfterADowntimeNightRecordsNotObserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	first, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	first.watchingSince = at(t, "2026-09-01T08:00:00Z")
	if err := first.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{445}, Window: nightWindow()}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	// The first process sees the night of 1 September through: it opened
	// at 21:00 UTC, after this process came up at 08:00.
	first.FillWatchNights(at(t, "2026-09-02T08:00:00Z"), map[string]bool{"e1": true})
	// The first fill also writes down the nights before this process
	// existed. Those are "not observed"; only the one it watched end to
	// end is "empty".
	seen := mustGetEntry(t, first, "e1").Nights
	if len(seen) != watchlist.MaxNights {
		t.Fatalf("got %d nights, want %d: %+v", len(seen), watchlist.MaxNights, seen)
	}
	if got := seen[len(seen)-1]; got.State != watchlist.NightEmpty {
		t.Fatalf("the night this process watched throughout is %q, want %q", got.State, watchlist.NightEmpty)
	}
	for _, n := range seen[:len(seen)-1] {
		if n.State != watchlist.NightUnobserved {
			t.Fatalf("the night of %s predates this process but reads %q, want %q", n.Opened, n.State, watchlist.NightUnobserved)
		}
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("closing: %v", err)
	}

	// Down through the night of 2 September; back up at 09:00 on the 3rd.
	second, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })
	second.watchingSince = at(t, "2026-09-03T09:00:00Z")
	second.FillWatchNights(at(t, "2026-09-03T10:00:00Z"), map[string]bool{"e1": true})

	got := mustGetEntry(t, second, "e1").Nights
	newest := got[len(got)-1]
	if !newest.Opened.Equal(at(t, "2026-09-02T21:00:00Z")) {
		t.Fatalf("newest night opened %s, want the night of 2 September", newest.Opened)
	}
	if newest.State != watchlist.NightUnobserved {
		t.Fatalf("the night nothing was running for is %q, want %q -- reporting it as empty would be an absence of ours presented as a fact about the network", newest.State, watchlist.NightUnobserved)
	}
	// The night the first process did watch is untouched by the restart:
	// a night is written once, keyed by the instant its window opened.
	prev := got[len(got)-2]
	if !prev.Opened.Equal(at(t, "2026-09-01T21:00:00Z")) || prev.State != watchlist.NightEmpty {
		t.Errorf("the night the first process watched came back as %+v, want it still empty", prev)
	}
}

// TestFillWatchNightsIgnoresEntriesWithNoWindow pins that an entry with no
// window accrues no history at all -- there is nothing to be present or
// absent from.
func TestFillWatchNightsIgnoresEntriesWithNoWindow(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	if err := s.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	if changed := s.FillWatchNights(at(t, "2026-09-08T12:00:00Z"), map[string]bool{"e1": true}); changed != 0 {
		t.Errorf("FillWatchNights changed %d windowless entries, want 0", changed)
	}
	if got := mustGetEntry(t, s, "e1"); len(got.Nights) != 0 {
		t.Errorf("a windowless entry accrued %+v", got.Nights)
	}
}

// TestNightsRideOnAnInvertedEntryToo pins that both watchlist shapes carry
// the window: an inverted entry is watched on a schedule the same way.
func TestNightsRideOnAnInvertedEntryToo(t *testing.T) {
	s := mustOpenExpectationsStore(t)
	s.watchingSince = at(t, "2026-08-01T00:00:00Z")
	e := watchlist.Entry{
		ID:     "inv",
		Invert: true,
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		Window: nightWindow(),
	}
	if err := s.UpsertExpectation(e); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}
	s.RecordWatchNight("inv", at(t, "2026-09-01T22:30:00Z"))
	got := mustGetEntry(t, s, "inv")
	if !got.Invert {
		t.Fatal("the entry lost its inverted flag")
	}
	if !sameWindow(got.Window, nightWindow()) {
		t.Errorf("window came back as %+v", got.Window)
	}
	if len(got.Nights) == 0 || got.Nights[len(got.Nights)-1].State != watchlist.NightKept {
		t.Errorf("nights came back as %+v, want a kept one", got.Nights)
	}
}

// recordedNights is a NightRecorder that just remembers what it was told,
// for driving the sink without standing up a store.
type recordedNights struct {
	ids   []string
	times []time.Time
}

func (r *recordedNights) RecordWatchNight(entryID string, at time.Time) {
	r.ids = append(r.ids, entryID)
	r.times = append(r.times, at)
}

// TestSinkKeepsTheNightEvenWhenTheLogCannotStoreTheMatch pins the rule at
// the sink: once the match log fills, Append fails for every further
// match, but the traffic still happened and mikroview still saw it.
// Letting a limit of ours leave the night empty would turn it into a ring
// break an operator would read as silence on the network.
//
// A nil match log stands in for "the append did not happen", which is the
// same observable situation from the night's point of view.
func TestSinkKeepsTheNightEvenWhenTheLogCannotStoreTheMatch(t *testing.T) {
	rec := &recordedNights{}
	sink := MatchlogSinkWithNights(nil, rec)
	when := at(t, "2026-09-01T22:30:00Z")
	sink(RoutedEmission{
		EventTime: when,
		Expectation: &MatchlogWrite{
			EntryID: "e1",
			Tuple:   matchlog.Tuple{Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Port: 445},
		},
	})
	if len(rec.ids) != 1 || rec.ids[0] != "e1" || !rec.times[0].Equal(when) {
		t.Fatalf("the night was not kept: %+v at %+v", rec.ids, rec.times)
	}
}

// TestSinkKeepsNoNightForAnUnattributableMatch is the other half: with no
// source identity there is no device to say was present, which is the
// same reason the match log refuses such a record.
func TestSinkKeepsNoNightForAnUnattributableMatch(t *testing.T) {
	rec := &recordedNights{}
	sink := MatchlogSinkWithNights(nil, rec)
	sink(RoutedEmission{
		EventTime:   at(t, "2026-09-01T22:30:00Z"),
		Expectation: &MatchlogWrite{EntryID: "e1", Tuple: matchlog.Tuple{DestIP: "10.0.0.5", Port: 445}},
	})
	// A detection-intent emission is not this sink's business either.
	sink(RoutedEmission{EventTime: at(t, "2026-09-01T22:31:00Z")})
	if len(rec.ids) != 0 {
		t.Fatalf("nights were kept for %v", rec.ids)
	}
}
