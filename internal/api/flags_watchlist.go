// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is the expected verdict's second half (#641). #640 made
// "expected" record a sized expectation, which stops the detector
// raising the same flag again; this makes it also record what the device
// was actually seen doing as permitted destinations on the device's
// inverted watchlist entry, so recognising traffic as legitimate and
// writing it down stop being two separate errands.
//
// Automatic and reversible, in the owner's words on #641: "an offer here
// is just extra clicks so long as it can be rolled back somewhere." So
// there is no form and no extra click on the way in, and everything
// written is recorded on the expectation (flags.PermittedRecord) so undo
// -- and the ledger, #640 part C -- can take it back exactly.
//
// Why it lives in this package rather than in flags.Store beside
// recordExpectationLocked: the entry the destinations land on is an
// expectation *definition* in internal/engine's store, and internal/flags
// neither imports that store nor should. This is the one layer that holds
// both.

// flagDeviceIdentity resolves which device a flag is about, the same
// MAC-preferred, IP-fallback way every other identity in this codebase is
// resolved (matchlog.Identity, watchlist.eventIdentity).
//
// Reports false where the flag names no device: a Target that is not an
// address is a rule label, a port, a device id or "global" depending on
// the detector (see flags.Flag.Target), and an inverted watchlist entry
// scoped to one of those would be a policy about nothing. net.ParseIP
// rather than a per-type allow-list, so a detector added later is handled
// by what its target actually is rather than by a list nobody remembers
// to extend.
func flagDeviceIdentity(f flags.Flag) (matchlog.Identity, bool) {
	id := matchlog.Identity{MAC: f.Evidence.SrcMAC}
	if net.ParseIP(f.Target) != nil {
		id.IP = f.Target
	}
	if id.Empty() {
		return matchlog.Identity{}, false
	}
	return id, true
}

// flagPermittedDests is the flag's evidence pairs as permitted
// destinations, de-duplicated and in the flag's own order.
//
// Pairs, never Ports x Hosts: those are two independent sets, and
// crossing them would permit combinations the device never made -- the
// fabrication #654 added Evidence.Pairs to rule out, and the reason this
// feature waited for it. A flag whose detector records no pairs
// therefore permits nothing at all, which is the honest outcome rather
// than a degraded one.
func flagPermittedDests(f flags.Flag) []watchlist.PermittedDest {
	seen := make(map[watchlist.PermittedDest]struct{}, len(f.Evidence.Pairs))
	out := make([]watchlist.PermittedDest, 0, len(f.Evidence.Pairs))
	for _, p := range f.Evidence.Pairs {
		if p.Host == "" || p.Port == 0 {
			continue
		}
		d := watchlist.PermittedDest{DestIP: p.Host, Port: p.Port}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}

// invertedEntryFor finds the inverted expectation scoping id, using the
// entry's own matching predicate (Source.MatchesSource) rather than a
// looser comparison of its own: the entry this permits on must be the
// entry that would actually evaluate this device's traffic, or the
// permission would sit somewhere it can never be consulted.
func (s *Server) invertedEntryFor(id matchlog.Identity) (watchlist.Entry, bool, error) {
	entries, err := s.Definitions.ListExpectations()
	if err != nil {
		return watchlist.Entry{}, false, err
	}
	for _, e := range entries {
		if e.Invert && e.Source.MatchesSource(id) {
			return e, true, nil
		}
	}
	return watchlist.Entry{}, false, nil
}

// permitFlagEvidence records f's evidence pairs as permitted
// destinations on its device's inverted entry, creating that entry in its
// observing state if the device has none.
//
// Reports the record of what it wrote, and false when there was nothing
// to write -- no pairs, or no device the flag names. Both are ordinary:
// most detectors record no pairs, and several have no device-shaped
// target at all.
//
// A created entry starts Observing, exactly as one created through the
// definitions API does (handleDefinitionsCreate) and for a reason this
// path makes sharper: nothing fires from an automatic step. An observing
// inverted entry records where its device goes and raises nothing, so an
// operator who pressed "expected" on one flag has not silently armed a
// fence around the device.
//
// Promotion goes through watchlist.Entry.Promote, the same rule the
// operator-driven promote handler uses -- a pair already permitted is
// left alone, and one the entry has not observed is still permitted,
// since permitting something not yet seen is a legitimate deliberate
// choice (see that method's own doc comment).
func (s *Server) permitFlagEvidence(f flags.Flag, actor string, now time.Time) (flags.PermittedRecord, bool, error) {
	dests := flagPermittedDests(f)
	if len(dests) == 0 {
		return flags.PermittedRecord{}, false, nil
	}
	id, ok := flagDeviceIdentity(f)
	if !ok {
		return flags.PermittedRecord{}, false, nil
	}

	entry, found, err := s.invertedEntryFor(id)
	if err != nil {
		return flags.PermittedRecord{}, false, err
	}

	rec := flags.PermittedRecord{Verdict: flags.VerdictExpected, At: now}
	if found {
		// What this verdict *adds*, not what it names: a pair already
		// permitted was permitted by something else, and taking it away
		// on undo would reverse a decision this verdict never made. The
		// same rule #640 applies to the expectation's size, which undo
		// restores to what the previous verdict left rather than to
		// nothing.
		var added []watchlist.PermittedDest
		if err := s.Definitions.UpdateExpectation(entry.ID, func(e *watchlist.Entry) error {
			if !e.Invert {
				return watchlist.ErrNotInverted
			}
			added = notYetPermitted(*e, dests)
			e.Promote(dests)
			return nil
		}); err != nil {
			return flags.PermittedRecord{}, false, err
		}
		if len(added) == 0 {
			// Everything this flag saw was already allowed. There is
			// nothing to record and nothing an undo would have to take
			// back -- an empty record would put a line on the ledger
			// claiming a change that did not happen.
			return flags.PermittedRecord{}, false, nil
		}
		rec.EntryID = entry.ID
		rec.Dests = hostPortsOf(added)
		s.Audit.Record(actor, "definition.promote", entry.ID, fmt.Sprintf("%d destination(s) from an expected verdict on %s", len(added), f.ID))
		return rec, true, nil
	}

	// No name: an entry created this way has no name the operator chose,
	// and the watchlist renders an unnamed entry from its own boundary
	// (Watchlist.svelte's boundaryLabel) rather than from a label this
	// code would have to invent.
	created := watchlist.Entry{
		ID:        newDefinitionEntryID(),
		Source:    id,
		Invert:    true,
		Observing: true,
		CreatedAt: now,
	}
	created.Promote(dests)
	if err := s.Definitions.UpsertExpectation(created); err != nil {
		return flags.PermittedRecord{}, false, err
	}
	rec.EntryID = created.ID
	rec.Dests = hostPortsOf(dests)
	rec.CreatedEntry = true
	s.Audit.Record(actor, "definition.create", created.ID, fmt.Sprintf("observing entry for %s, from an expected verdict on %s", identityLabel(id), f.ID))
	s.Audit.Record(actor, "definition.promote", created.ID, fmt.Sprintf("%d destination(s) from an expected verdict on %s", len(dests), f.ID))
	return rec, true, nil
}

// unpermitFlagEvidence takes back what rec recorded: the destinations
// come off the entry's allow-list, and an entry this verdict created is
// deleted if nothing else has happened to it.
//
// "Nothing else" is three conditions, all of them checked after the
// removal rather than assumed: no permitted destinations left, still
// observing, and nothing observed. An entry that has since been promoted
// into, taken out of observe mode, or has seen the device go somewhere is
// carrying state this undo has no claim on, so it stays -- empty of this
// verdict's destinations, but there.
//
// An entry that has since been deleted outright is not an error: there is
// nothing left to take back, which is the outcome this was asked for.
func (s *Server) unpermitFlagEvidence(rec flags.PermittedRecord, actor string) error {
	if rec.EntryID == "" || len(rec.Dests) == 0 {
		return nil
	}
	dests := permittedDestsOf(rec.Dests)
	err := s.Definitions.UpdateExpectation(rec.EntryID, func(e *watchlist.Entry) error {
		if !e.Invert {
			return watchlist.ErrNotInverted
		}
		e.Unpermit(dests)
		return nil
	})
	switch {
	case errors.Is(err, watchlist.ErrEntryNotFound), errors.Is(err, engine.ErrNoSuchDefinition):
		return nil
	case err != nil:
		return err
	}
	s.Audit.Record(actor, "definition.unpermit", rec.EntryID, fmt.Sprintf("%d destination(s), an expected verdict withdrawn", len(dests)))

	if !rec.CreatedEntry {
		return nil
	}
	entry, ok, err := s.Definitions.GetExpectation(rec.EntryID)
	if err != nil || !ok {
		return err
	}
	if len(entry.Permitted) > 0 || len(entry.Observed) > 0 || !entry.Observing {
		return nil
	}
	if err := s.Definitions.DeleteExpectation(rec.EntryID); err != nil {
		return err
	}
	s.Audit.Record(actor, "definition.delete", rec.EntryID, "the entry an expected verdict created, withdrawn with it")
	return nil
}

// withdrawPermittedFor is the whole reversal for one flag, in the order
// that keeps the record and the watchlist agreeing: the record comes off
// the expectation first, then the destinations come off the entry, and a
// failure at the second step puts the record back rather than leaving a
// device permitted with nothing on the ledger saying so.
//
// Called before the verdict itself changes, because undoing an expected
// verdict that created the expectation deletes the expectation -- and
// this record with it (see flags.Store.WithdrawPermitted).
func (s *Server) withdrawPermittedFor(w http.ResponseWriter, id, actor string) bool {
	rec, ok := s.Flags.WithdrawPermitted(id)
	if !ok {
		return true
	}
	if err := s.unpermitFlagEvidence(rec, actor); err != nil {
		s.Flags.RecordPermitted(id, rec)
		http.Error(w, "taking the permitted destinations back off the watchlist failed, so the verdict was left as it was: "+err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

// notYetPermitted is the subset of dests the entry does not already
// allow -- what a verdict actually changes, which is what undo has to be
// able to reverse. Read before Promote runs, since Promote is idempotent
// and afterwards there is no way to tell the two apart.
func notYetPermitted(e watchlist.Entry, dests []watchlist.PermittedDest) []watchlist.PermittedDest {
	have := make(map[watchlist.PermittedDest]struct{}, len(e.Permitted))
	for _, p := range e.Permitted {
		have[p] = struct{}{}
	}
	out := make([]watchlist.PermittedDest, 0, len(dests))
	for _, d := range dests {
		if _, ok := have[d]; ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

// hostPortsOf/permittedDestsOf convert between the two spellings of the
// same pair. They are separate types on purpose -- internal/flags owns
// its own persisted shape and does not import internal/watchlist (see
// flags.HostPort's doc comment) -- so this package, which imports both,
// is where the conversion belongs.
func hostPortsOf(dests []watchlist.PermittedDest) []flags.HostPort {
	out := make([]flags.HostPort, len(dests))
	for i, d := range dests {
		out[i] = flags.HostPort{Host: d.DestIP, Port: d.Port}
	}
	return out
}

func permittedDestsOf(pairs []flags.HostPort) []watchlist.PermittedDest {
	out := make([]watchlist.PermittedDest, len(pairs))
	for i, p := range pairs {
		out[i] = watchlist.PermittedDest{DestIP: p.Host, Port: p.Port}
	}
	return out
}

// identityLabel names a device for an audit line, MAC first for the same
// reason matching prefers it.
func identityLabel(id matchlog.Identity) string {
	if id.MAC != "" {
		return id.MAC
	}
	return id.IP
}
