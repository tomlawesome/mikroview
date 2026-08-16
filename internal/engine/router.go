// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// MatchlogWrite is the intent router's expectation-intent output -- the
// matchlog counterpart to flags.Flag below. Deliberately not
// matchlog.Record itself: a Record's Tuple (Source identity, DestIP,
// Port) and Event are derived from the raw triggering store.Event, which
// an Emission never carries (see Emission's own doc comment -- it is a
// definition's accumulated judgement, not the event that produced it).
// Only the wiring that has an actual event in hand (#406) can supply
// those; what Route can produce from a Definition and an Emission alone
// is everything a definition's own judgement determines -- which
// expectation entry this belongs to, the rendered detail, confidence
// where computed, and whether the emission was provisional.
type MatchlogWrite struct {
	EntryID     string
	Target      string
	Detail      string
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
	ev := flags.Evidence{Ports: em.Ports, Hosts: em.Hosts}
	if em.NAT != nil {
		ev.NAT = &flags.NATInfo{IP: em.NAT.IP, Port: em.NAT.Port, Raw: em.NAT.Raw}
	}
	return &flags.Flag{
		Type:        flags.Type(em.DefinitionID),
		Target:      em.Target,
		Detail:      em.Detail,
		Confidence:  copyIntPtr(em.Confidence),
		Evidence:    ev,
		Country:     em.Country,
		Provisional: em.Provisional,
	}
}

func routeToMatchlog(def Definition, em Emission) *MatchlogWrite {
	return &MatchlogWrite{
		EntryID:     def.ID,
		Target:      em.Target,
		Detail:      em.Detail,
		Confidence:  copyIntPtr(em.Confidence),
		Provisional: em.Provisional,
	}
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
