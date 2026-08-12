// SPDX-License-Identifier: AGPL-3.0-only

package suggest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/routerstate"
)

// Generate builds the full candidate batch from rs's current state,
// across every device that has pushed anything and every source #243
// slice 5 settled on. Feed the result to Store.Sync -- see
// RunPeriodicSync for the loop that does both on a schedule.
//
// Address lists are a defined Kind (KindAddressList) but not generated
// here yet -- see that Kind's own doc comment on why.
func Generate(rs *routerstate.Store) []Candidate {
	var out []Candidate
	for _, device := range rs.Devices() {
		out = append(out, generateDeviceCandidates(rs, device)...)
		out = append(out, generatePortCandidates(rs, device)...)
	}
	return out
}

// generateDeviceCandidates suggests watching what a device does
// (KindDevice, becomes an inverted entry if accepted), one per named
// device. Named devices only -- an anonymous lease is impossible to
// make a real decision about, and suggesting one per anonymous device
// on the network would be pure noise, not a useful suggestion. The name
// always comes from a DHCP lease's Hostname: ARP carries no name at
// all, so an ARP-only device (one with no corresponding DHCP lease)
// never produces a candidate under this rule -- an accepted limitation,
// not a bug, the same category as identity's own "IP-only doesn't
// survive a lease change" limitation (#243 section 1). ARP still has a
// real role here: where it and a DHCP lease agree on a MAC, ARP's
// address is used in preference to the lease's, since ARP reflects what
// the device is doing right now and a DHCP lease can be stale between
// the router's own renewal cycles.
func generateDeviceCandidates(rs *routerstate.Store, device string) []Candidate {
	leases, _, ok := rs.DHCPLeases(device)
	if !ok {
		return nil
	}
	arpIPByMAC := make(map[string]string)
	if arp, _, ok := rs.ARPEntries(device); ok {
		for _, a := range arp {
			if a.MAC != "" && a.Address != "" {
				arpIPByMAC[a.MAC] = a.Address
			}
		}
	}

	seen := make(map[string]bool)
	var out []Candidate
	for _, l := range leases {
		if l.Hostname == "" || l.MAC == "" {
			continue
		}
		ip := l.Address
		if current, ok := arpIPByMAC[l.MAC]; ok {
			ip = current
		}
		id := deviceCandidateID(device, l.MAC)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Candidate{
			ID:            id,
			Kind:          KindDevice,
			Name:          l.Hostname,
			Justification: fmt.Sprintf("DHCP lease naming %s at %s", l.MAC, ip),
			RouterDevice:  device,
			Source:        matchlog.Identity{MAC: l.MAC, IP: ip},
		})
	}
	return out
}

// generatePortCandidates suggests watching attempts against ports a
// drop/reject rule already scopes (KindPort, becomes a non-inverted
// entry if accepted), one per rule. A rule with no dst-port set (most
// "drop everything on this chain" default-deny rules) matches every
// port, which is not a specific port suggestion this feature can make
// -- those rules produce nothing here, not a candidate covering every
// port. Unscoped by source or destination, the same broadest-possible
// generalisation of the old Control Ports feature non-inverted matching
// already is.
func generatePortCandidates(rs *routerstate.Store, device string) []Candidate {
	rules, _, ok := rs.FilterRules(device)
	if !ok {
		return nil
	}
	var out []Candidate
	for _, r := range rules {
		if r.Action != "drop" && r.Action != "reject" {
			continue
		}
		ports := parsePortSpec(string(r.DstPort))
		if len(ports) == 0 {
			continue
		}
		justification := fmt.Sprintf("%s rule on chain %q", r.Action, r.Chain)
		if r.LogPrefix != "" {
			justification += fmt.Sprintf(" (log-prefix %q)", r.LogPrefix)
		}
		out = append(out, Candidate{
			ID:            portCandidateID(device, r.Chain, r.Action, r.Protocol, string(r.DstPort), r.SrcAddressList),
			Kind:          KindPort,
			Name:          portCandidateName(ports),
			Justification: justification,
			RouterDevice:  device,
			Ports:         ports,
		})
	}
	return out
}

func portCandidateName(ports []int) string {
	strs := make([]string, len(ports))
	for i, p := range ports {
		strs[i] = strconv.Itoa(p)
	}
	return "port " + strings.Join(strs, ", ")
}

// deviceCandidateID and portCandidateID build the stable, content-derived
// identity Sync relies on to recognise the same candidate again --
// deliberately not based on anything that shifts on its own (a rule's
// Ordinal reorders whenever any rule is added/removed/reordered, which
// would silently duplicate every port candidate on the next unrelated
// firewall edit) -- see suggest.go's Candidate.ID doc comment.
//
// The \x00 join byte means a real ID routinely contains a raw NUL --
// internal/api's /api/suggestions/{id}/... routes take this ID as a URL
// path segment, which works (verified in internal/api's own tests) but
// only when the caller percent-encodes it first (encodeURIComponent in
// JS turns \0 into %00); an un-escaped NUL is rejected before the
// request is even sent.
func deviceCandidateID(routerDevice, mac string) string {
	return strings.Join([]string{"device", routerDevice, mac}, "\x00")
}

// portCandidateID deliberately does not include which rule produced the
// candidate, so two filter rules agreeing on all six of these fields
// collapse into one suggestion -- and which rule the Justification names
// then depends on iteration order.
//
// Left as it is (#267, Uncertain). The candidate is a proposed watchlist
// entry, and two rules with identical chain, action, protocol, port and
// source list propose the identical entry: separating them would offer
// the operator the same thing twice. What is lost is only which of the
// duplicate rules gets cited as the reason, and the entry it produces is
// the same either way.
func portCandidateID(routerDevice, chain, action, protocol, dstPort, srcAddressList string) string {
	return strings.Join([]string{"port", routerDevice, chain, action, protocol, dstPort, srcAddressList}, "\x00")
}

// RunPeriodicSync generates and syncs on a fixed schedule until ctx is
// done -- the "stay in sync with the router" background half of #243
// slice 5's design (settled with the repo owner: no separate manual
// "soft refresh" button, this already does everything one would have).
// Runs once immediately on entry, not just after the first interval, so
// a freshly started mikroview doesn't wait a full interval before
// showing any suggestions at all.
func (s *Store) RunPeriodicSync(ctx context.Context, rs *routerstate.Store, interval time.Duration) {
	s.syncOnceRecovered(rs)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncOnceRecovered(rs)
		}
	}
}

// syncOnceRecovered isolates panic recovery to a single sync pass rather
// than RunPeriodicSync's whole lifetime -- recover only unwinds as far
// as the nearest deferring function, so a defer in RunPeriodicSync
// itself would end background syncing for good on the first bad pass,
// the same reasoning watchlist.Evaluator.evaluateRecovered's doc
// comment gives for its identical shape.
func (s *Store) syncOnceRecovered(rs *routerstate.Store) {
	defer logging.Recover(persistLog)
	if err := s.Sync(Generate(rs)); err != nil {
		persistLog.Warn(fmt.Sprintf("syncing suggestion candidates failed: %v", err))
	}
}
