// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sync"
	"testing"
)

func TestEvidenceSetAccumulatesDistinctValues(t *testing.T) {
	s := NewEvidenceSet()
	for _, p := range []int{22, 80, 22, 443} {
		s.AddPort(p)
	}
	s.AddHost("203.0.113.5")
	s.AddHost("203.0.113.5")
	s.AddLabel("r1")

	if got := s.Ports(); len(got) != 3 {
		t.Errorf("Ports() = %v, want 3 distinct values", got)
	}
	if got := s.Hosts(); len(got) != 1 {
		t.Errorf("Hosts() = %v, want 1 distinct value", got)
	}
	if got := s.Labels(); len(got) != 1 {
		t.Errorf("Labels() = %v, want 1 distinct value", got)
	}
}

func TestEvidenceSetPortsSortedAscending(t *testing.T) {
	s := NewEvidenceSet()
	for _, p := range []int{443, 22, 8080, 80} {
		s.AddPort(p)
	}
	got := s.Ports()
	want := []int{22, 80, 443, 8080}
	if len(got) != len(want) {
		t.Fatalf("Ports() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Ports() = %v, want %v", got, want)
		}
	}
}

func TestEvidenceSetCapsEachCategory(t *testing.T) {
	s := NewEvidenceSet()
	for p := 0; p < maxEvidencePorts+25; p++ {
		s.AddPort(p)
	}
	for i := 0; i < maxEvidenceHosts+10; i++ {
		s.AddHost(fmt.Sprintf("10.0.0.%d", i))
	}
	for i := 0; i < maxEvidenceLabels+10; i++ {
		s.AddLabel(fmt.Sprintf("r%d", i))
	}
	if got := len(s.Ports()); got != maxEvidencePorts {
		t.Errorf("Ports() len = %d, want cap %d", got, maxEvidencePorts)
	}
	if got := len(s.Hosts()); got != maxEvidenceHosts {
		t.Errorf("Hosts() len = %d, want cap %d", got, maxEvidenceHosts)
	}
	if got := len(s.Labels()); got != maxEvidenceLabels {
		t.Errorf("Labels() len = %d, want cap %d", got, maxEvidenceLabels)
	}
}

func TestEvidenceSetPortsReturnsFreshSliceEachCall(t *testing.T) {
	s := NewEvidenceSet()
	s.AddPort(22)

	first := s.Ports()
	first[0] = 9999 // mutate the caller's copy

	second := s.Ports()
	if second[0] != 22 {
		t.Errorf("Ports() shared backing storage across calls: second call saw %v", second)
	}
}

func TestEvidenceSetTouchedDistinguishesNeverAddedFromEmpty(t *testing.T) {
	s := NewEvidenceSet()
	portsSeen, hostsSeen, labelsSeen := s.touched()
	if portsSeen || hostsSeen || labelsSeen {
		t.Fatalf("touched() = (%v, %v, %v) on a fresh EvidenceSet, want all false", portsSeen, hostsSeen, labelsSeen)
	}

	s.AddPort(22)
	portsSeen, hostsSeen, labelsSeen = s.touched()
	if !portsSeen || hostsSeen || labelsSeen {
		t.Fatalf("touched() = (%v, %v, %v) after AddPort, want (true, false, false)", portsSeen, hostsSeen, labelsSeen)
	}
}

// TestEvidenceSetReadRaceSafeAgainstConcurrentAdds is a -race proof for
// EvidenceSet's own copy-on-read contract (Ports/Hosts/Labels), the same
// class of boundary Keyed.Snapshot proves at the map level. Run with
// `go test -race`.
func TestEvidenceSetReadRaceSafeAgainstConcurrentAdds(t *testing.T) {
	s := NewEvidenceSet()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.AddPort(i % 100)
			s.AddHost(fmt.Sprintf("203.0.113.%d", i%20))
			s.AddLabel(fmt.Sprintf("r%d", i%5))
			i++
		}
	}()

	for i := 0; i < 500; i++ {
		_ = s.Ports()
		_ = s.Hosts()
		_ = s.Labels()
	}
	close(stop)
	wg.Wait()
}

// --- #654: Pairs/SrcMAC ---

func TestEvidenceSetAddPairAccumulatesDistinctPairs(t *testing.T) {
	s := NewEvidenceSet()
	s.AddPair(HostPort{Host: "10.0.0.1", Port: 22})
	s.AddPair(HostPort{Host: "10.0.0.1", Port: 22}) // duplicate
	s.AddPair(HostPort{Host: "10.0.0.1", Port: 23}) // same host, different port
	s.AddPair(HostPort{Host: "10.0.0.2", Port: 22}) // same port, different host

	got := s.Pairs()
	if len(got) != 3 {
		t.Fatalf("Pairs() = %v, want 3 distinct pairs", got)
	}
	if total := s.PairsTotal(); total != 3 {
		t.Errorf("PairsTotal() = %d, want 3", total)
	}
}

// TestEvidenceSetPairsSortedByHostThenPort pins Pairs()'s ordering --
// the evidence panel groups by host (#654's owner decision), which only
// reads as one coherent list if hosts and, within a host, ports both
// come out in a stable, predictable order.
func TestEvidenceSetPairsSortedByHostThenPort(t *testing.T) {
	s := NewEvidenceSet()
	for _, hp := range []HostPort{
		{Host: "10.0.0.2", Port: 22},
		{Host: "10.0.0.1", Port: 443},
		{Host: "10.0.0.1", Port: 22},
	} {
		s.AddPair(hp)
	}
	want := []HostPort{
		{Host: "10.0.0.1", Port: 22},
		{Host: "10.0.0.1", Port: 443},
		{Host: "10.0.0.2", Port: 22},
	}
	got := s.Pairs()
	if len(got) != len(want) {
		t.Fatalf("Pairs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Pairs() = %v, want %v", got, want)
		}
	}
}

// TestEvidenceSetPairsTotalExactBelowTrackingCeiling pins #654's cap
// trade-off below maxEvidencePairsTracked: Pairs() (the small display
// sample) stops growing at maxEvidencePairs, but PairsTotal() keeps
// counting every distinct pair exactly and PairsTotalIsFloor stays
// false, right up to the point storage itself would otherwise have to
// grow without bound.
func TestEvidenceSetPairsTotalExactBelowTrackingCeiling(t *testing.T) {
	s := NewEvidenceSet()
	const extra = 25 // maxEvidencePairs+extra must stay under maxEvidencePairsTracked
	for i := 0; i < maxEvidencePairs+extra; i++ {
		s.AddPair(HostPort{Host: fmt.Sprintf("10.0.0.%d", i), Port: 22})
	}
	if got := len(s.Pairs()); got != maxEvidencePairs {
		t.Errorf("len(Pairs()) = %d, want display cap %d", got, maxEvidencePairs)
	}
	if got := s.PairsTotal(); got != maxEvidencePairs+extra {
		t.Errorf("PairsTotal() = %d, want the exact count %d", got, maxEvidencePairs+extra)
	}
	if s.PairsTotalIsFloor() {
		t.Error("PairsTotalIsFloor() = true, want false: nowhere near maxEvidencePairsTracked yet")
	}
}

// TestEvidenceSetPairsTotalBecomesFloorAtTrackingCeiling is the memory-
// safety half of the same trade-off: once genuinely distinct pairs keep
// arriving past maxEvidencePairsTracked, AddPair stops inserting
// (bounding storage against attacker-chosen traffic -- see that
// constant's own doc comment for why this ceiling exists at all),
// PairsTotal pins at the ceiling rather than continuing to grow, and
// PairsTotalIsFloor flips true so a caller knows the number is a lower
// bound, not an exact count.
func TestEvidenceSetPairsTotalBecomesFloorAtTrackingCeiling(t *testing.T) {
	s := NewEvidenceSet()
	const overflow = 40
	for i := 0; i < maxEvidencePairsTracked+overflow; i++ {
		s.AddPair(HostPort{Host: fmt.Sprintf("10.0.%d.%d", i/256, i%256), Port: 22})
	}
	if got := len(s.Pairs()); got != maxEvidencePairs {
		t.Errorf("len(Pairs()) = %d, want display cap %d", got, maxEvidencePairs)
	}
	if got := s.PairsTotal(); got != maxEvidencePairsTracked {
		t.Errorf("PairsTotal() = %d, want it pinned at the tracking ceiling %d", got, maxEvidencePairsTracked)
	}
	if !s.PairsTotalIsFloor() {
		t.Error("PairsTotalIsFloor() = false, want true once distinct pairs exceeded maxEvidencePairsTracked")
	}
}

// TestEvidenceSetAddPairDuplicateAtCeilingDoesNotFlipFloor pins that
// re-seeing an already-tracked pair -- the common case, one host:port
// combination getting hit repeatedly -- never itself trips
// PairsTotalIsFloor, even once storage is completely full. Only a
// genuinely new pair being turned away means the count has stopped
// being exact.
func TestEvidenceSetAddPairDuplicateAtCeilingDoesNotFlipFloor(t *testing.T) {
	s := NewEvidenceSet()
	for i := 0; i < maxEvidencePairsTracked; i++ {
		s.AddPair(HostPort{Host: fmt.Sprintf("10.0.%d.%d", i/256, i%256), Port: 22})
	}
	if s.PairsTotalIsFloor() {
		t.Fatal("PairsTotalIsFloor() = true after filling exactly to the ceiling, want false: nothing new was ever turned away")
	}

	s.AddPair(HostPort{Host: "10.0.0.0", Port: 22}) // already tracked above
	if s.PairsTotalIsFloor() {
		t.Error("PairsTotalIsFloor() = true after re-adding an already-tracked pair at capacity, want false")
	}
	if got := s.PairsTotal(); got != maxEvidencePairsTracked {
		t.Errorf("PairsTotal() = %d, want unchanged at %d", got, maxEvidencePairsTracked)
	}
}

func TestEvidenceSetSrcMACLastWriterWins(t *testing.T) {
	s := NewEvidenceSet()
	if got := s.SrcMAC(); got != "" {
		t.Fatalf("SrcMAC() on a fresh EvidenceSet = %q, want empty", got)
	}
	s.SetSrcMAC("aa:bb:cc:dd:ee:01")
	s.SetSrcMAC("aa:bb:cc:dd:ee:02")
	if got := s.SrcMAC(); got != "aa:bb:cc:dd:ee:02" {
		t.Errorf("SrcMAC() = %q, want the last value written", got)
	}
}

// TestEvidenceSetSetSrcMACIgnoresEmpty mirrors SetNAT's own zero-value
// guard: an event with no MAC must never overwrite an already-recorded
// one with "".
func TestEvidenceSetSetSrcMACIgnoresEmpty(t *testing.T) {
	s := NewEvidenceSet()
	s.SetSrcMAC("aa:bb:cc:dd:ee:01")
	s.SetSrcMAC("")
	if got := s.SrcMAC(); got != "aa:bb:cc:dd:ee:01" {
		t.Errorf("SrcMAC() after SetSrcMAC(\"\") = %q, want the earlier value preserved", got)
	}
}
