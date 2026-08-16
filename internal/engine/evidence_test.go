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
