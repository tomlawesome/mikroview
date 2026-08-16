// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"sort"
	"sync"
)

// maxEvidencePorts/maxEvidenceHosts/maxEvidenceLabels cap how many
// entries an EvidenceSet ever hands back for each category -- lifted
// from internal/detect.maxEvidencePorts/maxEvidenceHosts (see
// internal/detect/evidence.go) per docs/decisions/evaluation-engine.md
// section 1's "evidence accumulation" contract: a port scan or a
// distributed brute-force can legitimately involve far more than is
// useful to display, and an emission's Detail already states the true
// total (via PortCount/HostCount/LabelCount, see RenderEmission) -- this
// is a bounded illustrative sample, not a complete dump.
// maxEvidenceLabels is new here (internal/detect's Evidence never
// tracked rule labels as a set): sized the same as maxEvidenceHosts
// rather than maxEvidencePorts, since a deployment's distinct rule
// labels are bounded by how many firewall rules it defines, a population
// much closer in scale to distinct hosts than to the full 0-65535 port
// space.
const (
	maxEvidencePorts  = 50
	maxEvidenceHosts  = 20
	maxEvidenceLabels = 20
)

// EvidenceSet accumulates the distinct ports, hosts and rule labels
// actually seen in one definition's window -- the chassis's evidence
// primitive per the ADR: sets, not last-event-wins, which is the
// mechanism behind #379's wrong-naming findings (a flag's Detail
// claiming a port/host it never actually recorded, because nothing
// stopped a detector interpolating whichever event field was at hand
// when the flag happened to fire). A definition accumulates into one of
// these across a window and hands it to RenderEmission, which is the
// only sanctioned way to turn it into human-readable text -- see that
// function's doc comment for how naming an un-accumulated value is made
// a hard failure rather than a silently empty one.
//
// Safe for concurrent use: Add{Port,Host,Label} is expected to run only
// on the evaluation goroutine, but Ports/Hosts/Labels (this type's
// read API, and so its copy-on-read boundary -- see
// docs/decisions/evaluation-engine.md section 1) may be called from any
// goroutine, including one reading while the evaluation goroutine
// concurrently adds -- see TestEvidenceSetReadRaceSafeAgainstConcurrentAdds.
type EvidenceSet struct {
	mu sync.Mutex

	ports  map[int]struct{}
	hosts  map[string]struct{}
	labels map[string]struct{}

	// portsSeen/hostsSeen/labelsSeen record whether this category was
	// ever Add-ed to at all, independent of whether it currently holds
	// any values (every Add could have landed on an already-present
	// value, or the category could be at cap) -- this is what
	// RenderEmission consults to tell "accumulated, happens to total
	// zero new items this call" from "never accumulated," the
	// distinction the un-accumulated-value rule actually needs.
	portsSeen  bool
	hostsSeen  bool
	labelsSeen bool
}

// NewEvidenceSet constructs an empty EvidenceSet.
func NewEvidenceSet() *EvidenceSet {
	return &EvidenceSet{}
}

// AddPort records p as seen, capped at maxEvidencePorts -- further calls
// once the cap is reached still mark the category as accumulated (see
// portsSeen above), they just stop growing the stored set.
func (s *EvidenceSet) AddPort(p int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.portsSeen = true
	if s.ports == nil {
		s.ports = make(map[int]struct{})
	}
	if len(s.ports) >= maxEvidencePorts {
		return
	}
	s.ports[p] = struct{}{}
}

// AddHost is AddPort for a destination host, capped at maxEvidenceHosts.
func (s *EvidenceSet) AddHost(h string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostsSeen = true
	if s.hosts == nil {
		s.hosts = make(map[string]struct{})
	}
	if len(s.hosts) >= maxEvidenceHosts {
		return
	}
	s.hosts[h] = struct{}{}
}

// AddLabel is AddPort for a firewall rule label, capped at
// maxEvidenceLabels.
func (s *EvidenceSet) AddLabel(l string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labelsSeen = true
	if s.labels == nil {
		s.labels = make(map[string]struct{})
	}
	if len(s.labels) >= maxEvidenceLabels {
		return
	}
	s.labels[l] = struct{}{}
}

// Ports returns the distinct ports accumulated so far, sorted ascending
// and capped at maxEvidencePorts -- a fresh slice sharing no backing
// array with s's internal map, the copy-on-read boundary this type's own
// doc comment states.
func (s *EvidenceSet) Ports() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, 0, len(s.ports))
	for p := range s.ports {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Hosts is Ports for accumulated destination hosts, sorted
// lexicographically and capped at maxEvidenceHosts.
func (s *EvidenceSet) Hosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.hosts))
	for h := range s.hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// Labels is Ports for accumulated rule labels, sorted lexicographically
// and capped at maxEvidenceLabels.
func (s *EvidenceSet) Labels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.labels))
	for l := range s.labels {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// touched reports which categories have ever been Add-ed to -- consulted
// by RenderEmission, never by a definition directly.
func (s *EvidenceSet) touched() (ports, hosts, labels bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.portsSeen, s.hostsSeen, s.labelsSeen
}
