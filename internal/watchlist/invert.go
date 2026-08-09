// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

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

// maxObservedPerEntry bounds how many distinct destination/port pairs
// one entry's Observed candidate list holds -- the risk #243 open
// question 7 names directly: "an inverted entry in observe state would
// collect enormous volume before anyone promotes anything." 1,000 is a
// generous safety net for what a real device's traffic fingerprint looks
// like (a handful to a few dozen distinct destinations is typical), not
// a limit expected to be hit in normal use -- mirrors maxEntries' own
// reasoning.
var maxObservedPerEntry = 1000

// observeDropLogInterval rate-limits the "observation capacity reached"
// warning the same way evalQueueDropLogInterval rate-limits Evaluator's
// own overload log -- logging every dropped observation would add load
// during exactly the condition being reported.
const observeDropLogInterval = 30 * time.Second

var droppedObservations atomic.Uint64
var lastObserveDropLogNanos atomic.Int64

// ErrEntryNotFound is returned by RecordObservation, Promote and
// SetObserving for an ID that doesn't exist in the store.
var ErrEntryNotFound = errors.New("watchlist: no entry with that id")

// ErrNotInverted is returned by RecordObservation, Promote and
// SetObserving for a non-inverted entry -- none of the observe/promote
// machinery applies outside Invert mode.
var ErrNotInverted = errors.New("watchlist: entry is not inverted")

// RecordObservation upserts (or bumps) an observed candidate for the
// inverted entry id -- called from Evaluator on every Observed outcome,
// so this is a high-frequency path relative to Upsert/Delete (an
// operator's own, rare, interactive actions), unlike them it does not
// validate free text (the values come from Match, already derived from
// a real event, not operator input) and silently no-ops for an unknown
// or non-inverted entry rather than erroring -- Evaluator has no
// reasonable action to take on an error from its own hot path beyond
// what it already does for a full evaluation queue (see its own
// rate-limited drop log), and an entry that was inverted when Match ran
// but got edited to non-inverted a moment later before this call lands
// is a real, harmless race, not a bug to surface loudly.
//
// Once entry's Observed list is at maxObservedPerEntry, a genuinely new
// destination/port pair is dropped (not recorded) rather than growing
// without bound -- logged, rate-limited, the same shape as Evaluator's
// own queue-overflow warning. A repeat of an already-observed pair still
// updates its LastSeen/Count even once full, since that costs no new
// capacity.
func (s *Store) RecordObservation(id, destIP string, port int, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok || !e.Invert {
		return
	}
	for i := range e.Observed {
		if e.Observed[i].DestIP == destIP && e.Observed[i].Port == port {
			e.Observed[i].LastSeen = t
			e.Observed[i].Count++
			// Deliberately not persisted: persistLocked rewrites the
			// whole entries file (same cost model as
			// internal/entities/internal/audit), and a busy device in
			// observe mode could repeat-match the same destination at
			// the ingest rate -- persisting every bump would mean a
			// full-file rewrite per event, the exact cost matchlog's
			// append-only design exists to avoid. The trade-off: an
			// unclean shutdown can lose a repeat's latest Count/LastSeen
			// back to whatever was last persisted (the first occurrence,
			// or an earlier repeat that happened to coincide with some
			// other write). The candidate itself -- that this
			// destination was seen at all -- is never lost, only the
			// precise count/recency, which is acceptable for a review
			// list an operator hasn't looked at yet.
			return
		}
	}
	if len(e.Observed) >= maxObservedPerEntry {
		recordDroppedObservation()
		return
	}
	e.Observed = append(e.Observed, ObservedDest{DestIP: destIP, Port: port, FirstSeen: t, LastSeen: t, Count: 1})
	s.persistLocked()
}

func recordDroppedObservation() {
	total := droppedObservations.Add(1)
	now := time.Now().UnixNano()
	last := lastObserveDropLogNanos.Load()
	if now-last < int64(observeDropLogInterval) {
		return
	}
	if lastObserveDropLogNanos.CompareAndSwap(last, now) {
		persistLog.Warn(fmt.Sprintf("watchlist observation capacity reached -- %d new destination(s) not recorded so far (existing observations still update normally)", total))
	}
}

// Promote moves the given destination/port pairs from entry id's
// Observed list into its Permitted list, adding any pair not already
// present in either -- an operator may want to permit something the
// entry hasn't happened to observe yet, and that is a legitimate,
// deliberate choice, not an error. Does NOT change Observing: promotion
// is a per-tuple decision, distinct from leaving observe mode entirely
// (see SetObserving). A pair already in Permitted is left alone
// (idempotent).
func (s *Store) Promote(id string, dests []PermittedDest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		return ErrEntryNotFound
	}
	if !e.Invert {
		return ErrNotInverted
	}

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
	s.persistLocked()
	return nil
}

// SetObserving flips whether the inverted entry id is in observe mode.
// The raw mechanism only -- this package makes no judgement about when
// an operator (or a future assisted-promotion flow) should call it,
// which is #243 open question 3, deliberately left open.
func (s *Store) SetObserving(id string, observing bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		return ErrEntryNotFound
	}
	if !e.Invert {
		return ErrNotInverted
	}
	e.Observing = observing
	s.persistLocked()
	return nil
}
