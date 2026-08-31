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
	// maxEvidencePairs shares maxEvidencePorts rather than getting its own
	// constant -- issue #654's owner-recorded decision, and deliberately
	// not maxEvidenceHosts's 20. A pair is at least as specific as a port
	// (it names the port *and* which host it was seen with), so capping
	// it below the port cap would make a flag's pairs list name *fewer
	// distinct hosts* than its own Hosts list already does today --
	// making the panel more precise about combinations at the cost of
	// being less informative overall, which is the one outcome #654
	// flagged as worth avoiding. This is the *display* cap: how many
	// pairs Pairs() ever hands back. See maxEvidencePairsTracked below
	// for the separate, larger cap on how many distinct pairs
	// AddPair/PairsTotal are willing to count in the first place.
	maxEvidencePairs = maxEvidencePorts
	// maxEvidencePairsTracked bounds the *storage* AddPair grows, not
	// just what Pairs() displays -- deliberately different from every
	// other category in this file, which all cap storage and display at
	// the same number. Ports/Hosts/Labels can get away with that because
	// nothing downstream needs their exact count past the display cap
	// (RenderEmission's {PortCount}/{HostCount} are already just
	// len(capped-slice)). Pairs is different: PairsTotal exists
	// specifically so a truncated display can state the true count
	// (#654's "50 of 214 pairs, never a silent short list"), which means
	// AddPair has to keep counting past the 50-item display cap to have
	// a true count worth stating.
	//
	// "Keep counting" cannot mean "without limit," though. Every
	// EvidenceSet here lives for one definition-key's whole window (up
	// to hours for some shipped detectors), and the traffic that raises
	// critical_port in the first place -- the definition EvidencePairs
	// is declared on -- is exactly the traffic capable of producing an
	// enormous distinct-pair count: a scanner sweeping ports across a
	// handful of internal hosts, replicated across every source key the
	// definition is tracking concurrently. An attacker choosing how many
	// distinct (host, port) combinations to generate would otherwise be
	// choosing how much memory this process holds open for them. That is
	// a resource risk this package does not accept anywhere else (every
	// other Add* here caps storage precisely to avoid it), and Pairs
	// wanting an honest count is not a good enough reason to make it the
	// one exception.
	//
	// So AddPair stops inserting at this ceiling, exactly like every
	// other category's cap -- it is simply set higher (4x the 50-item
	// display cap) so PairsTotal stays exact across the overwhelming
	// majority of real windows, and only degrades to "at least
	// maxEvidencePairsTracked" (see EvidenceSet.PairsTotalIsFloor) once a
	// window's true breadth actually exceeds it. That degrade is
	// deliberate and load-bearing, not a bug to fix by raising this
	// constant: an unbounded map sized by attacker-chosen traffic is
	// worse than an approximate count that says so, which is why
	// PairsTotalIsFloor exists and why the frontend must render "50 of
	// 200+" rather than a flat, precise-looking "50 of 200" once it's
	// true. Whoever next considers raising this number should re-read
	// this comment first: the ceiling is not arbitrary, it is the
	// deliberate boundary between "an evidence sample" and "a resource
	// an attacker gets to size."
	maxEvidencePairsTracked = maxEvidencePairs * 4
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

	// nat is the last NAT translation detail recorded -- see SetNAT for
	// why this one category is last-writer-wins rather than a set.
	nat *NATInfo

	// pairs is #654's addition: distinct (host, port) combinations
	// actually seen together on one event, as opposed to Ports and Hosts
	// above, which independently record "every port seen" and "every
	// host seen" with no memory of which went with which. Capped at
	// insertion like every other category, just at maxEvidencePairsTracked
	// rather than maxEvidencePairs -- see AddPair and that constant's own
	// doc comment for why the two caps differ.
	pairs map[HostPort]struct{}
	// pairsTotalIsFloor records whether AddPair has ever turned away a
	// genuinely new pair because pairs was already at
	// maxEvidencePairsTracked -- see PairsTotalIsFloor.
	pairsTotalIsFloor bool

	// srcMAC is the last source MAC address recorded -- see SetSrcMAC for
	// why this, like nat, is last-writer-wins rather than a set (a given
	// per-source/per-source-port key's events all share one device, so
	// there is nothing to accumulate a *set* of).
	srcMAC string
}

// HostPort is one (destination host, destination port) combination
// actually observed together on a single event -- #654's fix for
// Evidence.Ports and Evidence.Hosts being independent sets that
// together cannot say which port went with which host. See AddPair.
type HostPort struct {
	Host string
	Port int
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

// AddPair records hp as seen, capped at maxEvidencePairsTracked -- a
// higher ceiling than AddPort/AddHost/AddLabel's maxEvidencePorts/Hosts/
// Labels, but a ceiling all the same, and for the same reason theirs
// exist: this map lives for one definition-key's whole window, and nothing
// about the traffic that grows it is trusted. See maxEvidencePairsTracked's
// own doc comment for why storage is capped higher than the display cap
// (Pairs()) rather than not capped at all.
//
// Once at the ceiling, a genuinely new pair (one not already in the map)
// is dropped and pairsTotalIsFloor latches true -- see PairsTotalIsFloor.
// A duplicate of an already-tracked pair is checked for and returned on
// early, both below and above the ceiling, so re-seeing the same pair
// many times (the common case: one port getting hit repeatedly against
// the same host) never itself trips the floor.
//
// No template token references this category (see RenderEmission's own
// doc comment for the fixed set that exists), so there is no "was this
// ever Add-ed to" gate to maintain the way portsSeen/hostsSeen/labelsSeen
// do -- Pairs()/PairsTotal()/PairsTotalIsFloor() simply read whatever is
// in the map, empty or not.
func (s *EvidenceSet) AddPair(hp HostPort) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairs == nil {
		s.pairs = make(map[HostPort]struct{})
	}
	if _, ok := s.pairs[hp]; ok {
		return
	}
	if len(s.pairs) >= maxEvidencePairsTracked {
		s.pairsTotalIsFloor = true
		return
	}
	s.pairs[hp] = struct{}{}
}

// Pairs returns the distinct pairs accumulated so far, sorted by host
// then port, capped at maxEvidencePairs (the smaller, display-only cap)
// -- the same copy-on-read boundary as Ports/Hosts/Labels, and the same
// illustrative-sample contract: PairsTotal/PairsTotalIsFloor are what
// state the true count, and whether it's exact, when this cap bites.
func (s *EvidenceSet) Pairs() []HostPort {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HostPort, 0, len(s.pairs))
	for p := range s.pairs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Port < out[j].Port
	})
	if len(out) > maxEvidencePairs {
		out = out[:maxEvidencePairs]
	}
	return out
}

// PairsTotal is the number of distinct pairs this window has seen,
// independent of the maxEvidencePairs display cap Pairs() applies -- what
// lets a caller state "50 of 214 pairs" instead of showing 50 and letting
// the list read as complete. Exact while PairsTotalIsFloor is false;
// pinned at maxEvidencePairsTracked (and a lower bound, not the true
// value) once it's true -- a caller must check PairsTotalIsFloor before
// presenting this number as precise.
func (s *EvidenceSet) PairsTotal() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pairs)
}

// PairsTotalIsFloor reports whether PairsTotal has stopped being exact --
// true once AddPair has turned away at least one genuinely new pair
// because storage was already at maxEvidencePairsTracked. A caller must
// render this case as "at least N", e.g. "50 of 200+", never a flat "50
// of 200": the latter reads as precise and is not (see
// maxEvidencePairsTracked's own doc comment for why exactness was traded
// away here at all).
func (s *EvidenceSet) PairsTotalIsFloor() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pairsTotalIsFloor
}

// NATInfo is one event's NAT translation detail -- store.Event's
// NatIP/NatPort/NatRaw, carried through an Emission so a detection-intent
// route can populate flags.Evidence.NAT (see routeToFlag, router.go).
// This package keeps its own copy of the shape rather than importing
// flags.NATInfo into the evidence primitive, for the same reason
// MatchlogWrite is not matchlog.Record: the sink types belong to the
// stores, and Emission is a definition's judgement, not a store row.
type NATInfo struct {
	IP   string
	Port int
	Raw  string
}

// SetNAT records the NAT translation detail of the event currently being
// folded in, replacing whatever a previous event set -- deliberately
// last-writer-wins rather than a set, unlike every other category on this
// type. NAT translation describes one specific packet's rewrite, not a
// property accumulated across a window, and flags.Evidence.NAT's own
// contract is "the triggering event's NAT translation info, when
// present" -- since a definition folds evidence in before rendering, the
// last value written is the triggering event's, which is exactly what
// that contract promises. A zero-valued info (no NAT fields on the
// event) is not recorded at all, so NAT stays nil rather than becoming
// an empty struct.
func (s *EvidenceSet) SetNAT(info NATInfo) {
	if info.IP == "" && info.Raw == "" && info.Port == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nat = &info
}

// NAT returns a copy of the last recorded NAT translation detail, or nil
// if none was ever recorded -- a copy, not the stored pointer, so this
// type's copy-on-read boundary holds for this category too.
func (s *EvidenceSet) NAT() *NATInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nat == nil {
		return nil
	}
	out := *s.nat
	return &out
}

// SetSrcMAC records the triggering event's source MAC address, replacing
// whatever a previous event set -- #654's fix for a flag identifying a
// device by IP alone, which stops matching the device the moment its
// DHCP lease changes (the same MAC-preferred identity
// matchlog.Identity.MatchesSource already relies on, per that type's own
// doc comment). Last-writer-wins for the same reason SetNAT is: within
// one key's window every event shares one source, so there is no set to
// accumulate, only a single value that keeps being reconfirmed.
//
// The caller (recordEvidence, declarative.go) is what enforces "only
// where the event has one and the source is local" -- this method
// itself just records whatever mac it is given, empty or not, the same
// division of responsibility SetNAT has (SetNAT's own zero-value check
// is about the *shape* of "nothing to record", not about whether NAT was
// the right category to record at all).
func (s *EvidenceSet) SetSrcMAC(mac string) {
	if mac == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.srcMAC = mac
}

// SrcMAC returns the last recorded source MAC address, or "" if none was
// ever recorded.
func (s *EvidenceSet) SrcMAC() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srcMAC
}

// sortedPortsCapped/sortedHostsCapped are
// internal/detect.sortedPortsCapped/sortedHostsCapped (see
// internal/detect/evidence.go) unchanged: a distinct-value set from a
// DistinctRing query, rendered as the sorted, capped slice
// flags.Evidence carries.
//
// A shipped *programmatic* definition needs these because it does not go
// through EvidenceSet at all: EvidenceSet exists so a declarative
// definition's Detail template can only ever name values that were
// actually accumulated (see RenderEmission), and a programmatic
// definition's Detail is genuinely computed rather than templated (see
// programmaticBase.emit). Its evidence still has to be sorted and capped
// the same way, so the two capping rules stay one pair of constants
// rather than becoming two.
func sortedPortsCapped(m map[int]struct{}) []int {
	out := make([]int, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Ints(out)
	if len(out) > maxEvidencePorts {
		out = out[:maxEvidencePorts]
	}
	return out
}

func sortedHostsCapped(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for h := range m {
		out = append(out, h)
	}
	sort.Strings(out)
	if len(out) > maxEvidenceHosts {
		out = out[:maxEvidenceHosts]
	}
	return out
}
