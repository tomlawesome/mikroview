// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"testing"
	"time"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatalf("opening an unpersisted store: %v", err)
	}
	return s
}

func addWatch(t *testing.T, s *Store, cidr string) Watch {
	t.Helper()
	w, err := s.Add(Watch{CIDR: cidr, CreatedAt: created, CleanWindow: 6 * time.Hour, Covered: true})
	if err != nil {
		t.Fatalf("adding a watch for %s: %v", cidr, err)
	}
	return w
}

func TestAddNormalisesAndDefaultsTheWindow(t *testing.T) {
	s := openStore(t)
	w, err := s.Add(Watch{CIDR: "192.0.2.1/24", CreatedAt: created, Covered: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if w.CIDR != "192.0.2.0/24" {
		t.Errorf("stored range is %q, want the masked %q", w.CIDR, "192.0.2.0/24")
	}
	if w.CleanWindow != DefaultCleanWindow {
		t.Errorf("clean window is %s, want the default %s", w.CleanWindow, DefaultCleanWindow)
	}
	if w.ID == "" {
		t.Error("the stored watch has no id")
	}
}

func TestAddRefusesASecondWatchForTheSameRange(t *testing.T) {
	// Two watches for one range would double every violation and give
	// the map two ghosts for one zone.
	s := openStore(t)
	addWatch(t, s, "192.0.2.0/24")
	if _, err := s.Add(Watch{CIDR: "192.0.2.9/24", CreatedAt: created, CleanWindow: 6 * time.Hour}); err == nil {
		t.Error("a second watch for the same range was accepted")
	}
}

func TestARetiredRangeMayBeWatchedAgain(t *testing.T) {
	// A range can be retired, brought back and retired again. The second
	// decommission is its own story with its own clock, so the first one
	// having finished must not block it.
	s := openStore(t)
	first := addWatch(t, s, "192.0.2.0/24")
	if retired := s.Sweep(created.Add(7 * time.Hour)); len(retired) != 1 {
		t.Fatalf("sweep retired %d watches, want 1", len(retired))
	}
	second, err := s.Add(Watch{CIDR: "192.0.2.0/24", CreatedAt: created.Add(8 * time.Hour), CleanWindow: 6 * time.Hour, Covered: true})
	if err != nil {
		t.Fatalf("re-watching a retired range: %v", err)
	}
	if second.ID == first.ID {
		t.Error("the second decommission reused the first one's identity, so it inherits its history")
	}
}

func TestRecordTrafficHitsOnlyTheWatchesTheEventTouched(t *testing.T) {
	s := openStore(t)
	dead := addWatch(t, s, "192.0.2.0/24")
	other := addWatch(t, s, "198.51.100.0/24")

	hit := s.RecordTraffic("192.0.2.7", "10.0.0.1", created.Add(time.Hour))
	if len(hit) != 1 || hit[0].ID != dead.ID {
		t.Fatalf("recording traffic hit %d watches, want only the one whose range it touched", len(hit))
	}

	after, _ := s.Get(other.ID)
	if after.TrafficCount != 0 {
		t.Errorf("an unrelated watch took %d observations", after.TrafficCount)
	}
	if got := hit[0].StateAt(created.Add(time.Hour)); got != StateDraining {
		t.Errorf("the touched watch reports %q, want %q", got, StateDraining)
	}
}

func TestSweepRecordsRetirementAtTheInstantTheWindowElapsed(t *testing.T) {
	// A sweep that runs every minute must not make the retirement time a
	// property of its own cadence.
	s := openStore(t)
	addWatch(t, s, "192.0.2.0/24")

	if got := s.Sweep(created.Add(5 * time.Hour)); len(got) != 0 {
		t.Fatalf("sweep retired %d watches before the window elapsed", len(got))
	}
	retired := s.Sweep(created.Add(9 * time.Hour))
	if len(retired) != 1 {
		t.Fatalf("sweep retired %d watches, want 1", len(retired))
	}
	if !retired[0].RetiredAt.Equal(created.Add(6 * time.Hour)) {
		t.Errorf("retirement stamped %s, want %s -- the moment the window actually elapsed, not the moment the sweep noticed",
			retired[0].RetiredAt, created.Add(6*time.Hour))
	}
	if again := s.Sweep(created.Add(10 * time.Hour)); len(again) != 0 {
		t.Errorf("a second sweep retired %d already-retired watches", len(again))
	}
}

func TestSweepLeavesABrokenWatchAlone(t *testing.T) {
	// The whole point of StateBroken: a watch that could not see a
	// straggler must not be swept up as a clean decommission.
	s := openStore(t)
	w, err := s.Add(Watch{CIDR: "192.0.2.0/24", CreatedAt: created, CleanWindow: 6 * time.Hour, Covered: false})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := s.Sweep(created.Add(48 * time.Hour)); len(got) != 0 {
		t.Fatalf("sweep retired %d uncovered watches, want 0", len(got))
	}
	if err := s.SetCovered(w.ID, true); err != nil {
		t.Fatalf("SetCovered: %v", err)
	}
	if got := s.Sweep(created.Add(48 * time.Hour)); len(got) != 1 {
		t.Errorf("once coverage arrived the sweep retired %d watches, want 1", len(got))
	}
}

func TestForceRemoveTakesTheSegmentOffTheMapAndKeepsTheWatch(t *testing.T) {
	// The owner's ruling in one test: the two paths converge on the same
	// end state. Patient removal keeps the draining segment on the map
	// until it is quiet; force-remove takes it off the map and finishes
	// the same job from the watchlist. Nothing is silently dropped
	// either way.
	s := openStore(t)
	w := addWatch(t, s, "192.0.2.0/24")

	forced, err := s.ForceRemove(w.ID, "tom", "the switch is already in a skip", created.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForceRemove: %v", err)
	}
	if !forced.Detached {
		t.Error("the watch was not detached")
	}
	if forced.ForcedBy != "tom" || forced.ForcedReason == "" || forced.ForcedAt.IsZero() {
		t.Errorf("the override was not recorded: by=%q reason=%q at=%s", forced.ForcedBy, forced.ForcedReason, forced.ForcedAt)
	}

	// Off the map...
	if ghosts := s.Ghosts(); len(ghosts) != 0 {
		t.Errorf("the map still draws %d ghosts for a force-removed segment, want 0", len(ghosts))
	}
	// ...but still watching, on the same clock.
	active := s.Active()
	if len(active) != 1 || active[0].ID != w.ID {
		t.Fatalf("the detached watch is not active any more (%d active), so straggler evidence would stop surfacing", len(active))
	}
	if active[0].CleanWindow != w.CleanWindow {
		t.Errorf("force-remove changed the clean window to %s, want %s unchanged", active[0].CleanWindow, w.CleanWindow)
	}

	// It still takes observations, and it still retires on the same
	// window -- the convergence the ruling turns on.
	if hit := s.RecordTraffic("192.0.2.7", "10.0.0.1", created.Add(2*time.Hour)); len(hit) != 1 {
		t.Fatalf("a detached watch took %d observations, want 1", len(hit))
	}
	if got := s.Sweep(created.Add(9 * time.Hour)); len(got) != 1 {
		t.Errorf("a detached watch retired %d times on its clean window, want 1", len(got))
	}
}

func TestForceRemoveRefusesAnAlreadyRetiredWatch(t *testing.T) {
	s := openStore(t)
	w := addWatch(t, s, "192.0.2.0/24")
	s.Sweep(created.Add(7 * time.Hour))
	if _, err := s.ForceRemove(w.ID, "tom", "too late", created.Add(8*time.Hour)); err == nil {
		t.Error("force-removing an already-retired watch was accepted")
	}
}

func TestARetiredWatchIsKeptButLeavesTheMap(t *testing.T) {
	// Deleting the record would make a completed decommission
	// indistinguishable from one that never happened.
	s := openStore(t)
	addWatch(t, s, "192.0.2.0/24")
	s.Sweep(created.Add(7 * time.Hour))

	if len(s.List()) != 1 {
		t.Error("the retired watch was dropped from the record")
	}
	if len(s.Active()) != 0 {
		t.Error("a retired watch is still being evaluated")
	}
	if len(s.Ghosts()) != 0 {
		t.Error("a retired watch is still drawn on the map")
	}
}

func TestReadsDoNotAliasLiveState(t *testing.T) {
	// The deep-copy-at-the-goroutine-boundary contract (#376): the
	// evaluation goroutine mutates these records while request
	// goroutines read them.
	s := openStore(t)
	w, err := s.Add(Watch{
		CIDR: "192.0.2.0/24", CreatedAt: created, CleanWindow: 6 * time.Hour, Covered: true,
		LastKnown: []Straggler{{Address: "192.0.2.7", Name: "garage-cam"}},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.Get(w.ID)
	if !ok {
		t.Fatal("the watch is not readable back")
	}
	got.LastKnown[0].Name = "clobbered"

	again, _ := s.Get(w.ID)
	if again.LastKnown[0].Name != "garage-cam" {
		t.Errorf("mutating a read-out watch changed the stored one to %q", again.LastKnown[0].Name)
	}
}

func TestChangeHookFiresOnEveryMutation(t *testing.T) {
	// The next-event-effect contract: a watch accepted, forced or
	// retired has to reach the engine on the next event, not the next
	// restart.
	s := openStore(t)
	fired := 0
	s.SetOnChange(func() { fired++ })

	w := addWatch(t, s, "192.0.2.0/24")
	if fired != 1 {
		t.Fatalf("creating a watch fired the hook %d times, want 1", fired)
	}
	s.RecordTraffic("192.0.2.7", "", created.Add(time.Hour))
	s.ForceRemove(w.ID, "tom", "because", created.Add(2*time.Hour))
	s.Sweep(created.Add(9 * time.Hour))
	s.Delete(w.ID)
	if fired != 5 {
		t.Errorf("the hook fired %d times across five mutations, want 5", fired)
	}
}
