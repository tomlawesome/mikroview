// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

// MatchlogWrite is the intent router's expectation-intent output -- the
// matchlog counterpart to flags.Flag below. Deliberately not
// matchlog.Record itself: a Record carries store-assigned fields (ID,
// FirstSeen, LastSeen, Count) that belong to matchlog's own append/
// collapse lifecycle, not to translating one emission, exactly as
// flags.Flag's raise lifecycle is left to flags.Store below.
//
// Tuple and Event are what an expectation write needs beyond a
// definition's own judgement, and both come from the triggering event
// (Emission.TriggeringEvent, added by #406 -- see that field's own doc
// comment for why an accumulated judgement cannot reconstruct them).
// This type's earlier shape said only "the wiring that has an actual
// event in hand (#406) can supply those"; this is that wiring, and the
// supply happens here rather than at each sink so nothing open-codes
// the identity rule.
type MatchlogWrite struct {
	EntryID string
	Target  string
	Detail  string
	// Tuple is what matchlog collapses on: the event's own real,
	// specific source identity (MAC-preferred, IP fallback -- see
	// eventIdentity), the destination address, and the destination port.
	// Always the *event's* identity, never the definition's own
	// (possibly unscoped) source scoping, so an unscoped expectation
	// watching many devices still produces one record per device rather
	// than one shared record every device's traffic collapses into.
	Tuple matchlog.Tuple
	// Event is the full triggering event, kept as evidence -- see
	// matchlog.Record's own evidence-first doc comment.
	Event       store.Event
	Confidence  *int
	Provisional bool
}

// RoutedEmission is Route's output: exactly one of Detection or
// Expectation is set, chosen by def.Intent -- see Route.
type RoutedEmission struct {
	Detection   *flags.Flag
	Expectation *MatchlogWrite
	// EventTime is the triggering store.Event's ReceivedAt, carried
	// through from Emission.EventTime (added by #405 -- see that field's
	// own doc comment) -- neither flags.Flag nor MatchlogWrite has
	// anywhere to hold it (both leave their own timestamp fields to
	// whatever store actually raises them: flags.Store.AddProvisional's
	// own `now` parameter, matchlog's own Append), so a caller wiring
	// OnRoutedEmission onto a real store needs it available here, not
	// re-derived from wall-clock time.Now() -- which would silently
	// diverge from the event that actually caused this emission under
	// any real queueing delay, and would break every timestamp-based
	// characterization pin outright when replayed against historical
	// events (a test corpus, or Replay itself).
	EventTime time.Time
	// SourceIP carries Emission.SourceIP through -- see that field's own
	// doc comment for why a sink cannot just use Detection.Target.
	SourceIP string
}

// Route converts em, an Emission produced by def, into the shape def's
// Intent feeds -- docs/decisions/evaluation-engine.md section 3's whole
// point made concrete: Intent decides what an emission feeds and
// nothing else, so one function suffices for both kinds of definition
// instead of two divergent call paths growing apart over time. This is
// what keeps "two kinds" (declarative/programmatic, see Kind) from
// decaying into "two subsystems" the way internal/detect and
// internal/watchlist did (see the ADR's "problem" section) -- both
// kinds' evaluation code ends every Evaluate call by building one
// Emission and handing it to this one function.
//
// A detection-intent definition's emission becomes a flags.Flag, through
// the same field shape flags.Store.AddProvisional accepts (#405 is
// expected to call Route's result almost directly into that). Flag's
// own store-assigned fields (ID, FirstSeen, LastSeen, Count, ClearedAt)
// are left zero here -- those belong to flags.Store's raise lifecycle,
// not to translating one emission's judgement.
//
// An expectation-intent definition's emission becomes a MatchlogWrite --
// see its own doc comment for why that is not matchlog.Record itself.
//
// #405 is this package's first production call site: DeclarativeDefinition.Evaluate
// (declarative.go) calls Route on every threshold crossing and hands the
// result to OnRoutedEmission, which main.go wires onto a real
// flags.Store/matchlog.Store. Route is also exercised directly by its own
// tests, which is where "both intents can express the same emission" is
// proven -- see TestRouteProducesSymmetricOutputForBothIntents.
func Route(def Definition, em Emission) (RoutedEmission, error) {
	if em.DefinitionID != def.ID {
		return RoutedEmission{}, fmt.Errorf("engine: emission is for definition %q, not %q", em.DefinitionID, def.ID)
	}
	switch def.Intent {
	case IntentDetection:
		return RoutedEmission{Detection: routeToFlag(em), EventTime: em.EventTime, SourceIP: em.SourceIP}, nil
	case IntentExpectation:
		return RoutedEmission{Expectation: routeToMatchlog(def, em), EventTime: em.EventTime, SourceIP: em.SourceIP}, nil
	default:
		return RoutedEmission{}, fmt.Errorf("engine: definition %q has unknown intent %q", def.ID, def.Intent)
	}
}

func routeToFlag(em Emission) *flags.Flag {
	ev := flags.Evidence{Ports: em.Ports, Hosts: em.Hosts, SrcMAC: em.SrcMAC}
	if em.NAT != nil {
		ev.NAT = &flags.NATInfo{IP: em.NAT.IP, Port: em.NAT.Port, Raw: em.NAT.Raw}
	}
	if len(em.Pairs) > 0 {
		ev.Pairs = make([]flags.HostPort, len(em.Pairs))
		for i, p := range em.Pairs {
			ev.Pairs[i] = flags.HostPort{Host: p.Host, Port: p.Port}
		}
		ev.PairsTotal = em.PairsTotal
		// PairsTotalIsFloor rides along with PairsTotal, not
		// independently: it only ever means something in relation to a
		// stated total, so there's nothing to carry when there are no
		// Pairs to begin with (see Emission.PairsTotalIsFloor's own doc
		// comment).
		ev.PairsTotalIsFloor = em.PairsTotalIsFloor
	}
	return &flags.Flag{
		Type:        flags.Type(em.DefinitionID),
		Target:      em.Target,
		Detail:      em.Detail,
		Confidence:  copyIntPtr(em.Confidence),
		Evidence:    ev,
		Country:     em.Country,
		Provisional: em.Provisional,
		// Size carries the definition's declared size through to the
		// store, which is what consults the expectation for this
		// (Type, Target) -- see flags.Store.add. ExpectedSize is left
		// zero here for the same reason ID/FirstSeen/Count are: it is
		// filled in by the store's raise lifecycle, from the expectation
		// it just failed to absorb, not by translating one emission.
		Size: copyIntPtr(em.Size),
	}
}

func routeToMatchlog(def Definition, em Emission) *MatchlogWrite {
	w := &MatchlogWrite{
		EntryID:     def.ID,
		Target:      em.Target,
		Detail:      em.Detail,
		Confidence:  copyIntPtr(em.Confidence),
		Provisional: em.Provisional,
	}
	if em.TriggeringEvent != nil {
		w.Event = *em.TriggeringEvent
		w.Tuple = matchlog.Tuple{
			Source: eventIdentity(w.Event),
			DestIP: w.Event.DstIP,
			Port:   w.Event.DstPort,
		}
	}
	return w
}

// eventIdentity resolves an event's source identity the MAC-preferred,
// IP-fallback way matchlog.Identity.MatchesSource compares against:
// SrcMAC when the parser found one, SrcIP otherwise.
//
// Which chains carry src-mac is a property of the firmware, not
// something to rely on: on a real RouterOS 7.23.3 both forward and input
// carry it (#273), while output -- traffic the router originates, so
// there is no incoming frame to read a source MAC from -- does not. The
// IP fallback is what makes that not matter.
func eventIdentity(e store.Event) matchlog.Identity {
	return matchlog.Identity{MAC: e.SrcMAC, IP: e.SrcIP}
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
