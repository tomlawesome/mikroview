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
		SetConfiguredSources(nil)
	})

	host := "198.51.100.10"
	SetConfiguredSources([]string{host})
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
		SetConfiguredSources(nil)
	})

	host := "198.51.100.40"
	SetConfiguredSources([]string{host})
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

// TestDuplicateSightingsFromThreeCopiesReportsThreeNotTwo is the v0.6.0
// pre-release audit's finding: a router whose mikroview logging block
// was pasted three times dispatches every event three times over, and
// the reported apparent copy count must say so. The multiplicity
// histogram used to be stuck reporting "2" regardless: the second copy
// of each event scored multiplicity 2, and only the third copy scored
// multiplicity 3, so an N-times-pasted event's weight spread evenly
// across buckets 2..N -- and since modalMultiplicityLocked's tie-break
// favors the smaller figure, the true, larger copy count could never
// win.
func TestDuplicateSightingsFromThreeCopiesReportsThreeNotTwo(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
		SetConfiguredSources(nil)
	})

	host := "198.51.100.11"
	SetConfiguredSources([]string{host})
	for i := 0; i < dupSightingsToReportDrift; i++ {
		line := []byte(fmt.Sprintf("event %d", i))
		noteDuplicateLine(host, line) // the original
		noteDuplicateLine(host, line) // the second copy
		noteDuplicateLine(host, line) // the third copy
		now = now.Add(time.Millisecond)
	}

	loss := Stats().Loss.Duplicate
	if !loss.Active {
		t.Fatalf("expected sustained triplication to be reported active, got %+v", loss)
	}
	if loss.CopyCount != 3 {
		t.Errorf("loss.duplicate.copyCount = %d, want 3 (every line arrived three times)", loss.CopyCount)
	}
}

// TestDuplicateSightingsSurviveInterleavedEvents is the case a single
// cluster slot could not hold. One source duplicating its logging rule
// duplicates every event it matches, so two events arrive interleaved
// -- a1, b1, a2, b2, a3, b3 -- and both are mid-cluster at once. With
// one slot, B's second copy evicted A, so A's third copy could not
// find its own open entry: it added a fresh count at the higher bucket
// and left A's stale count sitting in the lower one. The histogram
// then read a tie between 2 and 3 copies, and since ties favor the
// smaller figure, a source pasted three times still reported 2 --
// exactly the fault the cluster tracking was added to fix, one step
// further out.
func TestDuplicateSightingsSurviveInterleavedEvents(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
		SetConfiguredSources(nil)
	})

	host := "198.51.100.12"
	SetConfiguredSources([]string{host})
	for i := 0; i < dupSightingsToReportDrift; i++ {
		a := []byte(fmt.Sprintf("event a %d", i))
		b := []byte(fmt.Sprintf("event b %d", i))
		// Interleaved, the way two matched events really arrive.
		noteDuplicateLine(host, a)
		noteDuplicateLine(host, b)
		noteDuplicateLine(host, a)
		noteDuplicateLine(host, b)
		noteDuplicateLine(host, a)
		noteDuplicateLine(host, b)
		now = now.Add(time.Millisecond)
	}

	loss := Stats().Loss.Duplicate
	if !loss.Active {
		t.Fatalf("expected sustained triplication to be reported active, got %+v", loss)
	}
	if loss.CopyCount != 3 {
		t.Errorf("loss.duplicate.copyCount = %d, want 3 (both events arrived three times, interleaved)", loss.CopyCount)
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
		SetConfiguredSources(nil)
	})

	hostA := "198.51.100.30"
	hostB := "198.51.100.31"
	SetConfiguredSources([]string{hostA, hostB})
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
		SetConfiguredSources(nil)
	})

	host := "198.51.100.20"
	SetConfiguredSources([]string{host})
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
// modalMultiplicityLocked directly: whatever produced a genuine tie
// between two multiplicities in the histogram (two, equally common,
// distinct copy counts within the same episode), it must resolve to the
// smaller, always-safe figure.
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
		SetConfiguredSources(nil)
	})

	// DuplicateSightings is an all-time total, like Dropped/Oversized
	// beside it -- other tests in this package add to it too, so this
	// checks the delta this test itself caused, not an absolute value.
	before := tcpDuplicateSightingsTotal.Load()

	host := "198.51.100.50"
	SetConfiguredSources([]string{host})
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

// An undeclared sender must not be able to have Settings tell the
// operator that it -- or anything else -- has duplicate logging rules.
// The syslog listener asks for no client certificate, so anyone who can
// reach the port could otherwise send one line twice, twenty times, and
// put advice about a router mikroview has never heard of in front of
// the operator, displacing a true warning about one it has.
func TestDuplicateDriftIgnoresAnUndeclaredSource(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
		SetConfiguredSources(nil)
	})

	SetConfiguredSources([]string{"198.51.100.60"})
	for i := 0; i < dupSightingsToReportDrift*2; i++ {
		line := []byte(fmt.Sprintf("event %d", i))
		noteDuplicateLine("203.0.113.9", line)
		noteDuplicateLine("203.0.113.9", line)
		now = now.Add(time.Millisecond)
	}

	if loss := Stats().Loss.Duplicate; loss.Active {
		t.Errorf("an undeclared source was reported as drifting: %+v", loss)
	}
}

// The tracked-source cap evicts whichever source has been quiet longest
// rather than refusing every newcomer, so a burst of one-line senders
// cannot hold the map for the process's lifetime and lock a router that
// connects later out of detection entirely.
func TestDuplicateSourceCapEvictsTheQuietestSource(t *testing.T) {
	now := time.Now()
	setLossClock(func() time.Time { return now })
	t.Cleanup(func() {
		setLossClock(nil)
		clearDuplicateState()
		SetConfiguredSources(nil)
	})

	declared := make([]string, 0, dupTrackedSourcesCap+1)
	for i := 0; i < dupTrackedSourcesCap; i++ {
		declared = append(declared, fmt.Sprintf("198.51.100.%d", i))
	}
	latecomer := "203.0.113.77"
	declared = append(declared, latecomer)
	SetConfiguredSources(declared)

	// Fill the cap, each source one line, oldest first.
	for i := 0; i < dupTrackedSourcesCap; i++ {
		noteDuplicateLine(declared[i], []byte("filler"))
		now = now.Add(time.Millisecond)
	}

	// The latecomer arrives with the map already full, and must still
	// be tracked well enough to be reported.
	for i := 0; i < dupSightingsToReportDrift; i++ {
		line := []byte(fmt.Sprintf("late event %d", i))
		noteDuplicateLine(latecomer, line)
		noteDuplicateLine(latecomer, line)
		now = now.Add(time.Millisecond)
	}

	loss := Stats().Loss.Duplicate
	if !loss.Active || loss.Host != latecomer {
		t.Errorf("a source arriving after the cap filled was not tracked: %+v", loss)
	}
}
