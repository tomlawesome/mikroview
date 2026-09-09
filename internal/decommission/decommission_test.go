// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"testing"
	"time"
)

// created is a fixed instant every test measures from, so a failure
// message reads as a story rather than as arithmetic.
var created = time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC)

func watchFor(t *testing.T, cidr string, window time.Duration) Watch {
	t.Helper()
	normalised, err := NormaliseCIDR(cidr)
	if err != nil {
		t.Fatalf("normalising %q: %v", cidr, err)
	}
	return Watch{
		ID:          "w1",
		CIDR:        normalised,
		CreatedAt:   created,
		CleanWindow: window,
		Covered:     true,
	}
}

func TestNormaliseCIDRMasksToTheNetwork(t *testing.T) {
	// RouterOS writes an /ip/address row as the interface's own address
	// with a prefix length, so the retired *range* is that masked. Two
	// spellings of one segment must become one identity, or a re-offer
	// from a different interface address would create a second watch for
	// the same range.
	for _, tc := range []struct{ in, want string }{
		{"192.0.2.1/24", "192.0.2.0/24"},
		{"192.0.2.0/24", "192.0.2.0/24"},
		{"  10.4.7.9/16 ", "10.4.0.0/16"},
		{"2001:db8:1:2::1/64", "2001:db8:1:2::/64"},
	} {
		got, err := NormaliseCIDR(tc.in)
		if err != nil {
			t.Fatalf("NormaliseCIDR(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("NormaliseCIDR(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "192.0.2.1", "not a range", "192.0.2.0/33"} {
		if _, err := NormaliseCIDR(bad); err == nil {
			t.Errorf("NormaliseCIDR(%q) accepted a value that is not a range", bad)
		}
	}
}

func TestAFreshWatchHolds(t *testing.T) {
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	if got := w.StateAt(created.Add(time.Minute)); got != StateHolding {
		t.Errorf("a watch that has never seen traffic reports %q, want %q", got, StateHolding)
	}
	if got := w.Remaining(created.Add(time.Hour)); got != 5*time.Hour {
		t.Errorf("countdown after an hour is %s, want 5h", got)
	}
}

func TestTrafficPutsTheSegmentIntoDraining(t *testing.T) {
	// The owner's framing: live traffic on a dead segment is a problem
	// being reported, not history being remembered. One straggler is the
	// whole finding -- there is no threshold to cross.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.RecordTraffic(created.Add(time.Hour))

	if got := w.StateAt(created.Add(time.Hour + time.Minute)); got != StateDraining {
		t.Errorf("after one straggler the segment reports %q, want %q", got, StateDraining)
	}
	if w.TrafficCount != 1 {
		t.Errorf("traffic count is %d, want 1", w.TrafficCount)
	}
}

func TestEveryStragglerResetsTheCleanWindowClock(t *testing.T) {
	// The ruling this test pins: "any traffic to or from the retired
	// CIDR is a violation; the clean-window clock resets". A segment
	// retires because it went quiet and stayed quiet, never because
	// enough wall-clock time passed while it kept talking.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)

	// Five hours in, one straggler. Without a reset the watch would have
	// retired an hour later.
	w.RecordTraffic(created.Add(5 * time.Hour))

	at := created.Add(6*time.Hour + time.Minute)
	if got := w.StateAt(at); got != StateDraining {
		t.Fatalf("with a straggler at +5h the segment reports %q at +6h1m, want %q -- the clock did not reset", got, StateDraining)
	}
	if got := w.Remaining(at); got == 0 {
		t.Error("the countdown reached zero despite a straggler resetting the clock")
	}

	// It retires six hours after the *straggler*, not after creation.
	if got := w.StateAt(created.Add(11 * time.Hour)); got != StateRetired {
		t.Errorf("at +11h (six hours after the last straggler) the segment reports %q, want %q", got, StateRetired)
	}
}

func TestASkewedRouterClockCannotRewindTheWindow(t *testing.T) {
	// Device clocks are not monotonic with arrival order
	// (internal/store), so an observation stamped in the past must not
	// shorten the window by moving the clock backwards.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.RecordTraffic(created.Add(4 * time.Hour))
	w.RecordTraffic(created.Add(time.Hour)) // stale, arriving late

	if w.TrafficCount != 2 {
		t.Errorf("traffic count is %d, want 2 -- a stale event is still an observation", w.TrafficCount)
	}
	if !w.LastTrafficAt.Equal(created.Add(4 * time.Hour)) {
		t.Errorf("the clock moved to %s, want it to stay at %s", w.LastTrafficAt, created.Add(4*time.Hour))
	}
}

func TestAFirstStragglerStampedBeforeCreationStillLeavesHolding(t *testing.T) {
	// The count is real, so the watch must not go on reporting "nothing
	// has ever straggled" while holding evidence to the contrary.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.RecordTraffic(created.Add(-time.Hour))

	if got := w.StateAt(created.Add(time.Minute)); got != StateDraining {
		t.Errorf("after a back-dated first straggler the segment reports %q, want %q", got, StateDraining)
	}
}

func TestACleanWindowRetiresTheSegment(t *testing.T) {
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	if got := w.StateAt(created.Add(6 * time.Hour)); got != StateRetired {
		t.Errorf("at exactly the clean window the segment reports %q, want %q", got, StateRetired)
	}
	if got := w.Remaining(created.Add(6 * time.Hour)); got != 0 {
		t.Errorf("countdown at retirement is %s, want 0", got)
	}
}

func TestAWatchNothingLogsIsBrokenAndNeverRetires(t *testing.T) {
	// The failure this prevents: a watch nobody is feeding would
	// otherwise announce a successful decommission -- the range went
	// quiet, the ghost left the map, job done -- purely because no
	// evidence could reach it.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.Covered = false

	if got := w.StateAt(created.Add(time.Minute)); got != StateBroken {
		t.Errorf("an uncovered watch reports %q, want %q", got, StateBroken)
	}
	if got := w.StateAt(created.Add(48 * time.Hour)); got != StateBroken {
		t.Errorf("an uncovered watch reports %q long past its window, want %q -- it must not retire on evidence it could not have seen", got, StateBroken)
	}
}

func TestARecordedRetirementOutranksEverything(t *testing.T) {
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.RetiredAt = created.Add(6 * time.Hour)
	w.Covered = false

	if got := w.StateAt(created.Add(7 * time.Hour)); got != StateRetired {
		t.Errorf("a watch already recorded as retired reports %q, want %q", got, StateRetired)
	}
	// A retired watch takes no further observations: its record is a
	// finished story, and a late event must not reopen it.
	w.RecordTraffic(created.Add(8 * time.Hour))
	if w.TrafficCount != 0 {
		t.Errorf("a retired watch recorded an observation (count %d)", w.TrafficCount)
	}
}

func TestMatchesBothDirections(t *testing.T) {
	// "ANY traffic to or from it": a forgotten rule still steering
	// packets at a dead range is as much a straggler as a device still
	// speaking from one.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	for _, tc := range []struct {
		name     string
		src, dst string
		want     bool
	}{
		{"from the dead range", "192.0.2.7", "10.0.0.1", true},
		{"to the dead range", "10.0.0.1", "192.0.2.7", true},
		{"within the dead range", "192.0.2.7", "192.0.2.9", true},
		{"nowhere near it", "10.0.0.1", "10.0.0.2", false},
		{"adjacent range", "192.0.3.7", "10.0.0.1", false},
		{"unparseable", "not-an-ip", "", false},
	} {
		if got := w.Matches(tc.src, tc.dst); got != tc.want {
			t.Errorf("%s: Matches(%q, %q) = %v, want %v", tc.name, tc.src, tc.dst, got, tc.want)
		}
	}
}

func TestDescribeNamesTheLikelyStragglerAndDegradesHonestly(t *testing.T) {
	// #460's worked example, and the absence rule beside it: enrichment
	// only where a push actually covered the address, never a guess.
	w := watchFor(t, "192.0.2.0/24", 6*time.Hour)
	w.LastKnown = []Straggler{{
		Address: "192.0.2.7",
		Name:    "garage-cam",
		Source:  "dhcp-lease",
		SeenAt:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}}

	if got, want := w.Describe("192.0.2.7"), `traffic from 192.0.2.7 -- was "garage-cam" until 2026-08-01`; got != want {
		t.Errorf("Describe named it %q, want %q", got, want)
	}
	if got, want := w.Describe("192.0.2.9"), "traffic from 192.0.2.9"; got != want {
		t.Errorf("an address no push ever named is described %q, want the bare %q", got, want)
	}
}

func TestValidateRefusesAWindowOutsideTheHoursScaleBand(t *testing.T) {
	for _, window := range []time.Duration{0, time.Minute, MinCleanWindow - time.Second, MaxCleanWindow + time.Second, 30 * 24 * time.Hour} {
		w := watchFor(t, "192.0.2.0/24", window)
		if err := w.Validate(); err == nil {
			t.Errorf("a clean window of %s was accepted", window)
		}
	}
	w := watchFor(t, "192.0.2.0/24", DefaultCleanWindow)
	if err := w.Validate(); err != nil {
		t.Errorf("the default clean window was refused: %v", err)
	}
}
