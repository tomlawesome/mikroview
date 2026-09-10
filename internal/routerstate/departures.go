// SPDX-License-Identifier: AGPL-3.0-only

package routerstate

import (
	"net/netip"
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// This file answers one question for issue #460: **when does a network
// segment go?**
//
// The trigger the concept originally imagined -- an operator deleting a
// subnet in a topography editor -- does not exist and should not be
// invented: mikroview's topography is derived, not edited (the zone list
// is computed from the pushed /ip address table and observed boundaries;
// see frontend/src/lib/zones.svelte.ts). There is no in-app segment
// removal to hook.
//
// What does exist is the honest moment: a **later push that no longer
// carries the segment**. The operator retired the subnet on the router,
// the router re-pushed its address table on its usual 15-30 minute
// schedule, and the range that was there is not there any more. That is
// the operator's own act arriving as evidence rather than as a form
// submission, which is the shape everything else in this product takes.
//
// # The trap this file is mostly about
//
// This package's contract, stated in its own doc comment, is that a
// missing page means *less enrichment, never "those records were
// deleted"* -- absence of evidence is not evidence. Read carelessly, a
// departure detector is exactly the code that breaks that rule: a push
// that dropped one of four pages would look like three quarters of the
// estate being decommissioned at once.
//
// So a departure is only ever computed between two **complete cycles**.
// A cycle is complete when every page the push declared has arrived
// (len(pages) == pagesSeen). The comparison is between the last complete
// cycle and the new complete cycle, and a cycle that is still filling
// contributes nothing at all. A page that never arrives therefore
// produces silence, not a false decommission -- which is the same answer
// this package gives everywhere else.
//
// Two further refusals, for the same reason:
//
//   - Nothing is compared until a first complete cycle has been seen, so
//     a restart cannot report every segment as departed on the first
//     push, and cannot report a segment as departed because it was never
//     seen in the first place.
//   - Departures are held in memory only, like everything else here. A
//     restart forgets pending offers. That costs an unmade offer for a
//     segment retired while mikroview was down -- the next push simply
//     establishes a new baseline -- and it keeps this package's "there is
//     nothing to redact from a backup because it is never in one"
//     property intact for data that is, by construction, a list of the
//     operator's internal ranges and the hostnames inside them.

// maxDepartures bounds the pending-offer list per device, the same
// explicit-ceiling convention as maxDevices and maxRecordsPerKind. An
// operator answering offers keeps this near zero; a router flapping its
// address table is what the ceiling is for. Oldest is evicted, because
// the newest departure is the one an operator is most likely to be
// looking for.
var maxDepartures = 64

// Departure is one segment that a complete push cycle stopped carrying.
//
// It is an *offer*, not a decision: nothing is watched and nothing
// leaves the map until an operator answers it. Deliberately so -- a
// segment can vanish from an address table because it was retired, or
// because an interface was renamed, or because someone is mid-edit, and
// only the operator knows which.
type Departure struct {
	// Device is the router that stopped carrying it.
	Device string `json:"device"`
	// CIDR is the retired range, masked to its network.
	CIDR string `json:"cidr"`
	// Address is the row exactly as the router last wrote it
	// ("192.0.2.1/24"), kept beside CIDR because that is what the
	// operator will recognise in their own config.
	Address string `json:"address"`
	// Interface is the boundary the segment stood on -- the identity the
	// topography keys a zone on, so an accepted offer's ghost lands in
	// the lane its zone occupied.
	Interface string `json:"interface"`
	// Name is the address row's comment where it had one, else the
	// interface -- the zone's operator-facing name, frozen.
	Name string `json:"name"`
	// DepartedAt is when the complete cycle that no longer carried it
	// was applied.
	DepartedAt time.Time `json:"departedAt"`
	// LastKnown is every address inside the range that this router had a
	// name for at the moment it went, newest-named first.
	//
	// This is #460's enrichment, snapshotted here because it is about to
	// stop being knowable: the DHCP lease and ARP tables for a retired
	// range drain away over the following pushes, so a name looked up
	// later would be missing precisely when it is wanted. Only addresses
	// a push actually covered appear -- the absence rule, not a guess.
	LastKnown []KnownHost `json:"lastKnown,omitempty"`
}

// KnownHost is one address the router named, and which pushed table
// named it. Source uses this package's own HostSource* vocabulary; it is
// empty for an address seen only in ARP, which proves the address was
// live without supplying a name.
type KnownHost struct {
	Address string    `json:"address"`
	Name    string    `json:"name,omitempty"`
	Source  string    `json:"source,omitempty"`
	SeenAt  time.Time `json:"seenAt"`
}

// departureState is one device's address-table baseline, its in-flight
// cycle bookkeeping, and its pending offers. Held on deviceState.
type departureState struct {
	// baseline is the prefix set of the last complete ip-address cycle,
	// keyed by masked CIDR. nil until a first complete cycle arrives,
	// which is what makes "we have never seen a complete table" distinct
	// from "the complete table was empty".
	baseline map[string]ingest.IPAddressEntry
	// cyclePages is which pages of the *current* cycle have arrived, and
	// cycleTotal the page count that cycle declared.
	//
	// This bookkeeping is the difference between a correct answer and a
	// plausible one, and it is not obvious, so: kindState alone cannot
	// tell a complete cycle from a half-replaced one. A page replaces its
	// own slot, and a new cycle with the same page count does not clear
	// the old pages (see Apply), so len(pages) == pagesSeen is true
	// throughout a multi-page re-push. A range that merely *moved* from
	// page 1 to page 2 between cycles is therefore absent from the union
	// for the moment between the two pages landing -- and reading that
	// as a decommission would offer to watch a segment nobody retired.
	// Counting the pages seen since the last comparison is what closes
	// that window.
	cyclePages map[int]bool
	cycleTotal int
	pending    []Departure
}

// noteAddressCycleLocked is called from Apply after an ip-address page
// lands. It does nothing at all unless that page completed a cycle.
//
// Called with the store's write lock held.
func (ds *deviceState) noteAddressCycleLocked(device string, page ingest.Payload, now time.Time) {
	ks, ok := ds.kinds[ingest.KindIPAddress]
	if !ok || ks.pagesSeen == 0 {
		return
	}
	if ds.departures == nil {
		ds.departures = &departureState{}
	}
	dp := ds.departures
	if dp.cycleTotal != ks.pagesSeen || dp.cyclePages == nil {
		// A cycle declaring a different page count starts fresh, mirroring
		// Apply's own decision to drop the old pages rather than mix them.
		dp.cycleTotal = ks.pagesSeen
		dp.cyclePages = make(map[int]bool, ks.pagesSeen)
	}
	dp.cyclePages[page.Page] = true
	if len(dp.cyclePages) < dp.cycleTotal || len(ks.pages) != ks.pagesSeen {
		// Still filling. Absence of a page is not evidence of a deleted
		// segment -- see this file's doc comment.
		return
	}
	// A complete cycle: compare it, then start counting the next one.
	dp.cyclePages = make(map[int]bool, dp.cycleTotal)

	current := make(map[string]ingest.IPAddressEntry)
	for _, p := range ks.pages {
		for _, e := range p.IPAddresses {
			prefix, err := netip.ParsePrefix(e.Address)
			if err != nil {
				// A row this binary cannot parse is skipped rather than
				// treated as a range: it must not become a phantom
				// departure the next time round, and it must not mask a
				// real one either -- it simply is not part of the
				// comparison in either direction.
				continue
			}
			current[prefix.Masked().String()] = e
		}
	}

	prior := dp.baseline
	dp.baseline = current
	if prior == nil {
		// First complete cycle: this is the baseline, not a diff. Every
		// range being "new" is not every range being created.
		return
	}

	for cidr, was := range prior {
		if _, still := current[cidr]; still {
			continue
		}
		name := was.Comment
		if name == "" {
			name = was.Interface
		}
		dp.pending = append(dp.pending, Departure{
			Device:     device,
			CIDR:       cidr,
			Address:    was.Address,
			Interface:  was.Interface,
			Name:       name,
			DepartedAt: now,
			LastKnown:  ds.knownHostsInLocked(cidr, now),
		})
	}
	if over := len(dp.pending) - maxDepartures; over > 0 {
		dp.pending = dp.pending[over:]
	}
}

// knownHostsInLocked snapshots every address inside cidr that this
// device's pushed identity tables name, newest source first.
//
// Precedence matches Store.HostName's: a DNS static entry is the
// operator's own written intent and beats a DHCP lease's self-reported
// hostname. ARP contributes addresses with no name, which is still worth
// carrying -- "something answered at 192.0.2.7" is evidence even when
// nothing ever named it, and a violation from that address can say so
// honestly rather than implying nothing was ever there.
//
// Called with the store's lock held.
func (ds *deviceState) knownHostsInLocked(cidr string, now time.Time) []KnownHost {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil
	}
	prefix = prefix.Masked()
	inRange := func(addr string) bool {
		a, err := netip.ParseAddr(addr)
		return err == nil && prefix.Contains(a)
	}

	byAddr := make(map[string]KnownHost)
	// ARP first (address, no name), then DHCP over it, then DNS static
	// over that -- so the strongest claim wins the slot.
	if ks, ok := ds.kinds[ingest.KindARP]; ok {
		for _, p := range ks.pages {
			for _, e := range p.ARP {
				if inRange(e.Address) {
					byAddr[e.Address] = KnownHost{Address: e.Address, SeenAt: ks.updatedAt}
				}
			}
		}
	}
	if ks, ok := ds.kinds[ingest.KindDHCPLease]; ok {
		for _, p := range ks.pages {
			for _, l := range p.DHCPLeases {
				if l.Address == "" || l.Hostname == "" || !inRange(l.Address) {
					continue
				}
				byAddr[l.Address] = KnownHost{Address: l.Address, Name: l.Hostname, Source: HostSourceDHCPLease, SeenAt: ks.updatedAt}
			}
		}
	}
	if ks, ok := ds.kinds[ingest.KindDNSStatic]; ok {
		for _, p := range ks.pages {
			for _, e := range p.DNSStatic {
				if e.Address == "" || e.Name == "" || !inRange(e.Address) {
					continue
				}
				byAddr[e.Address] = KnownHost{Address: e.Address, Name: e.Name, Source: HostSourceDNSStatic, SeenAt: ks.updatedAt}
			}
		}
	}

	if len(byAddr) == 0 {
		return nil
	}
	out := make([]KnownHost, 0, len(byAddr))
	for _, h := range byAddr {
		if h.SeenAt.IsZero() {
			h.SeenAt = now
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// Departures returns every pending offer across every device, newest
// first. Read-only: answering one is AckDeparture's job.
func (s *Store) Departures() []Departure {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []Departure
	for _, ds := range s.devices {
		if ds.departures == nil {
			continue
		}
		out = append(out, ds.departures.pending...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DepartedAt.Equal(out[j].DepartedAt) {
			return out[i].CIDR < out[j].CIDR
		}
		return out[i].DepartedAt.After(out[j].DepartedAt)
	})
	return out
}

// AckDeparture drops one pending offer, whichever way it was answered.
//
// Yes and no both land here: the offer is answered once and does not
// come back on the next push, because the next push no longer carries
// the range either and would otherwise re-offer it forever. Deliberately
// one method rather than accept/decline pair -- this package has no
// opinion about what was decided, only about what was asked.
func (s *Store) AckDeparture(device, cidr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, ok := s.devices[device]
	if !ok || ds.departures == nil {
		return false
	}
	for i, d := range ds.departures.pending {
		if d.CIDR != cidr {
			continue
		}
		ds.departures.pending = append(ds.departures.pending[:i], ds.departures.pending[i+1:]...)
		return true
	}
	return false
}
