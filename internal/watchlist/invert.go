// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"net"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

// isStructurallyExempt reports whether ip is one-to-many discovery
// traffic (broadcast, multicast, link-local) rather than "contacting
// another host" (#243 section 6, layer 1) -- deterministic, no judgement
// involved, and exempt from inverted entries by default (see Entry's
// IncludeStructuralNoise field for the opt-in).
//
// Only the address itself is checked, not subnet-relative broadcast
// (e.g. 192.168.1.255 on a /24): that needs the device's own subnet mask,
// which an event does not carry. 255.255.255.255 (the limited broadcast
// address, valid on any subnet) is checked explicitly; 224.0.0.0/4 and
// ff00::/8 are covered by net.IP's own IsMulticast/IsLinkLocal* checks.
func isStructurallyExempt(ip string) bool {
	if ip == "255.255.255.255" {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsMulticast() || parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast()
}

// isPermitted reports whether (destIP, port) is in entry's promoted
// allow-list.
func (e Entry) isPermitted(destIP string, port int) bool {
	for _, p := range e.Permitted {
		if p.DestIP == destIP && p.Port == port {
			return true
		}
	}
	return false
}

// matchInverted implements "this device should only ever reach the
// destinations in Permitted" (#243 section 1/5/6). entry.Source is
// required (Store.Upsert enforces this) and scopes which device's
// traffic this entry is even about -- unlike a non-inverted entry, there
// is no "any source" reading here, since the whole policy is about one
// specific device's expected behaviour.
//
// Config-derived exemption (#243 section 6, layer 2 -- a device reaching
// the gateway/DNS/NTP server its own DHCP lease handed it) is NOT
// implemented here. internal/ingest.DHCPLease (RouterOS's pushed lease
// data) carries only Hostname/MAC/Address -- no gateway, DNS or NTP
// server fields at all, so there is nothing to compare against yet. That
// is a real gap in what mikroview ingests, not a scope decision made
// here; extending the RouterOS push payload to carry that data is its
// own piece of work, tracked as a follow-up on #243 rather than silently
// built around.
func matchInverted(entry Entry, e store.Event) (matchlog.Tuple, Outcome) {
	if entry.Source.Empty() {
		// Store.Upsert refuses this at write time (ErrInvertedRequiresSource)
		// -- reachable only via a hand-constructed Entry bypassing Upsert
		// (e.g. a stale value loaded before that check existed).
		return matchlog.Tuple{}, NoMatch
	}
	id := eventIdentity(e)
	if id.Empty() || !entry.Source.MatchesSource(id) {
		return matchlog.Tuple{}, NoMatch // not this entry's device
	}
	if !isTrackableConnState(e) {
		return matchlog.Tuple{}, NoMatch // an accepted service's own return traffic
	}
	if e.DstIP == "" {
		return matchlog.Tuple{}, NoMatch // nothing to evaluate against
	}
	if !entry.IncludeStructuralNoise && isStructurallyExempt(e.DstIP) {
		return matchlog.Tuple{}, NoMatch
	}
	if entry.isPermitted(e.DstIP, e.DstPort) {
		return matchlog.Tuple{}, NoMatch
	}

	tuple := matchlog.Tuple{Source: id, DestIP: e.DstIP, Port: e.DstPort}
	if entry.Observing {
		return tuple, Observed
	}
	return tuple, Violation
}

// ErrEntryNotFound is returned by a caller mutating an entry that does
// not exist -- kept here, with the observe/promote machinery it belongs
// to, so internal/api and internal/engine answer 404 from one sentinel
// rather than each inventing their own.
var ErrEntryNotFound = errors.New("watchlist: no entry with that id")

// ErrNotInverted is returned for a non-inverted entry -- none of the
// observe/promote machinery applies outside Invert mode.
var ErrNotInverted = errors.New("watchlist: entry is not inverted")

// Promote moves the given destination/port pairs into e's Permitted
// allow-list, adding any pair not already present in either list -- an
// operator may want to permit something the entry has not happened to
// observe yet, and that is a legitimate, deliberate choice, not an
// error. Does NOT change Observing: promotion is a per-tuple decision,
// distinct from leaving observe mode entirely. A pair already in
// Permitted is left alone (idempotent).
//
// Moved from watchlist.Store.Promote when issue #407 deleted that store:
// the rule is the entry's, not the storage's, so it lives on the entry
// and engine.DefinitionsStore.UpdateExpectation is what persists the
// result.
// Unpermit removes the given destination/port pairs from e's Permitted
// allow-list, leaving any pair not named alone. The exact reverse of
// Promote's first half, and deliberately not of its second: a pair
// removed here does not go back into Observed, because Observed is what
// this entry has *seen* and an unpermitted pair may never have been seen
// by the entry at all -- #641's expected verdict permits what a flag's
// evidence recorded, which is a different observer.
//
// Exists for that reversal (see internal/api's verdict handler): an
// expected verdict permits a flag's evidence pairs automatically, and
// undoing or re-judging that verdict has to take back exactly what it
// added, or the device would go on being allowed somewhere on the
// strength of a judgement the operator has retracted. Idempotent: a pair
// that is not permitted is a no-op, not an error.
func (e *Entry) Unpermit(dests []PermittedDest) {
	if len(dests) == 0 || len(e.Permitted) == 0 {
		return
	}
	drop := make(map[PermittedDest]struct{}, len(dests))
	for _, d := range dests {
		drop[d] = struct{}{}
	}
	kept := e.Permitted[:0]
	for _, p := range e.Permitted {
		if _, ok := drop[p]; ok {
			continue
		}
		kept = append(kept, p)
	}
	e.Permitted = kept
}

func (e *Entry) Promote(dests []PermittedDest) {
	for _, d := range dests {
		if !e.isPermitted(d.DestIP, d.Port) {
			e.Permitted = append(e.Permitted, d)
		}
		kept := e.Observed[:0]
		for _, o := range e.Observed {
			if o.DestIP == d.DestIP && o.Port == d.Port {
				continue // now decided -- remove from the review list
			}
			kept = append(kept, o)
		}
		e.Observed = kept
	}
}
