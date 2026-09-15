// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"fmt"
	"testing"
	"time"
)

// TestFNV1aHash64EmptyInputMatchesOffsetBasis checks the hand-rolled
// hash against FNV-1a's published test vector for an empty input (the
// hash of nothing is the offset basis itself, since the multiply/xor
// loop never runs) -- cheap insurance that the constants above weren't
// mistyped.
func TestFNV1aHash64EmptyInputMatchesOffsetBasis(t *testing.T) {
	const offsetBasis uint64 = 14695981039346656037
	if got := fnv1aHash64(nil); got != offsetBasis {
		t.Errorf("fnv1aHash64(nil) = %d, want the FNV-1a 64-bit offset basis %d", got, offsetBasis)
	}
}

// TestDuplicateSightingsFromOneSourceReportDrift is #1234's central
// positive case: one source sending every line twice, sustained past
// dupSightingsToReportDrift, is reported with the source and the
// apparent (modal) copy count.
func TestDuplicateSightingsFromOneSourceReportDrift(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
	})

	host := "198.51.100.10"
	for i := 0; i < dupSightingsToReportDrift; i++ {
		line := []byte(fmt.Sprintf("event %d", i))
		noteDuplicateLine(host, line) // the original
		noteDuplicateLine(host, line) // the duplicate rule re-firing it
		now = now.Add(time.Millisecond)
	}

	loss := Stats().Loss.Duplicate
	if !loss.Active {
		t.Fatalf("expected sustained duplication from one source to be reported active, got %+v", loss)
	}
	if loss.Host != host {
		t.Errorf("loss.duplicate.host = %q, want %q", loss.Host, host)
	}
	if loss.CopyCount != 2 {
		t.Errorf("loss.duplicate.copyCount = %d, want 2 (every line arrived exactly twice)", loss.CopyCount)
	}
	if loss.Recent != dupSightingsToReportDrift {
		t.Errorf("loss.duplicate.recent = %d, want %d", loss.Recent, dupSightingsToReportDrift)
	}
}

// TestDuplicateBurstUnderSightingThresholdDoesNotReportDrift is the
// gate's other edge: sustained duplication that never reaches
// dupSightingsToReportDrift is a couple of coincidental repeats, not a
// misconfigured router, and must stay unreported.
func TestDuplicateBurstUnderSightingThresholdDoesNotReportDrift(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
	})

	host := "198.51.100.40"
	for i := 0; i < dupSightingsToReportDrift-1; i++ {
		line := []byte(fmt.Sprintf("event %d", i))
		noteDuplicateLine(host, line)
		noteDuplicateLine(host, line)
		now = now.Add(time.Millisecond)
	}

	if loss := Stats().Loss.Duplicate; loss.Active {
		t.Errorf("expected %d duplicate sightings (one short of the threshold) not to report drift, got %+v", dupSightingsToReportDrift-1, loss)
	}
}

// TestDuplicateContentFromTwoSourcesDoesNotReportDrift guards the
// other false-positive this rule must avoid: two different routers
// that happen to log the same thing (e.g. both seeing the same
// Internet-wide scan) are not one router's duplicated logging rule.
// Detection is per-source by construction (each source has its own
// ring), so this is really testing that nothing accidentally shares
// state across hosts.
func TestDuplicateContentFromTwoSourcesDoesNotReportDrift(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
	})

	hostA := "198.51.100.30"
	hostB := "198.51.100.31"
	for i := 0; i < dupSightingsToReportDrift*2; i++ {
		line := []byte(fmt.Sprintf("shared event %d", i))
		noteDuplicateLine(hostA, line)
		noteDuplicateLine(hostB, line)
		now = now.Add(time.Millisecond)
	}

	if loss := Stats().Loss.Duplicate; loss.Active {
		t.Errorf("identical content arriving from two different sources should never look like one source's rule firing twice, got %+v", loss)
	}
}

// TestDuplicateRingIsFixedSizeAndWraps proves the ring is a bounded,
// wrapping structure rather than something that grows to remember
// every line a source has ever sent: once dupRingSize newer lines have
// arrived, the oldest entry is gone, so re-sending its exact content
// is no longer detected as a duplicate.
func TestDuplicateRingIsFixedSizeAndWraps(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
	})

	host := "198.51.100.20"
	original := []byte("original line")
	noteDuplicateLine(host, original)

	for i := 0; i < dupRingSize; i++ {
		noteDuplicateLine(host, []byte(fmt.Sprintf("filler %d", i)))
	}

	before := tcpDuplicateSightingsTotal.Load()
	noteDuplicateLine(host, original)
	if got := tcpDuplicateSightingsTotal.Load(); got != before {
		t.Errorf("resending content evicted %d writes ago was still counted as a duplicate (total %d -> %d) -- the ring grew instead of wrapping", dupRingSize, before, got)
	}
}

// TestDuplicateModalMultiplicityFavorsSmallerOnTie exercises
// modalMultiplicityLocked directly: a source pasted three times over
// produces a mix of 2-copy and 3-copy sightings (see noteDuplicateLine's
// doc comment on why the first duplicate of any run under-reports its
// own group size), and a tie between them must resolve to the smaller,
// always-safe figure.
func TestDuplicateModalMultiplicityFavorsSmallerOnTie(t *testing.T) {
	var st sourceDupState
	st.multiplicity[0] = 5 // multiplicity 2
	st.multiplicity[1] = 5 // multiplicity 3

	modal, count := st.modalMultiplicityLocked()
	if modal != 2 || count != 5 {
		t.Errorf("modalMultiplicityLocked() = (%d, %d), want (2, 5) on a tie", modal, count)
	}
}

// TestClearLossResetsDuplicateState checks #1234's addition to issue
// #1015's "Clear all": a reported source, its per-source ring, and the
// all-time total all go back to zero.
func TestClearLossResetsDuplicateState(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
	})

	// DuplicateSightings is an all-time total, like Dropped/Oversized
	// beside it -- other tests in this package add to it too, so this
	// checks the delta this test itself caused, not an absolute value.
	before := tcpDuplicateSightingsTotal.Load()

	host := "198.51.100.50"
	for i := 0; i < dupSightingsToReportDrift; i++ {
		line := []byte(fmt.Sprintf("event %d", i))
		noteDuplicateLine(host, line)
		noteDuplicateLine(host, line)
		now = now.Add(time.Millisecond)
	}
	if !Stats().Loss.Duplicate.Active {
		t.Fatal("expected drift to be reported before testing the clear")
	}

	result := ClearLoss()
	if got := result.DuplicateSightings - before; got != dupSightingsToReportDrift {
		t.Errorf("ClearLoss().DuplicateSightings rose by %d during this test, want %d", got, dupSightingsToReportDrift)
	}

	loss := Stats().Loss.Duplicate
	if loss.Active || loss.Host != "" || loss.CopyCount != 0 {
		t.Errorf("loss.duplicate after clear = %+v, want a zeroed, inactive, hostless entry", loss)
	}
	if got := Stats().DuplicateSightings; got != 0 {
		t.Errorf("DuplicateSightings after clear = %d, want 0", got)
	}

	dupSourcesMu.Lock()
	tracked := len(dupSources)
	dupSourcesMu.Unlock()
	if tracked != 0 {
		t.Errorf("expected every source's ring to be dropped by the clear, %d still tracked", tracked)
	}

	// A source tracked before the clear must start a fresh episode, not
	// carry over the pre-clear sightings count: one pair alone must not
	// already look sustained.
	noteDuplicateLine(host, []byte("post-clear line"))
	noteDuplicateLine(host, []byte("post-clear line"))
	if loss := Stats().Loss.Duplicate; loss.Active {
		t.Errorf("one post-clear duplicate pair should not already report drift, got %+v", loss)
	}
}
