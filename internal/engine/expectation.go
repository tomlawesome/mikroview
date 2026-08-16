// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file is issue #406's port of internal/watchlist onto this
// chassis: a watchlist entry is an expectation definition, evaluated by
// the one engine, and internal/watchlist stops being a second copy of it
// (its own queue, its own run loop, its own panic boundary, its own
// per-event linear scan).
//
// docs/decisions/evaluation-engine.md section 2 draws the line this file
// implements, and it is the same line #405 drew for the detection intent:
//
//   - A non-inverted entry ("record attempts against these ports") is a
//     condition set already -- ports, connection state, source identity,
//     destination address, address-list membership -- so it becomes a
//     DeclarativeDefinition, and rides the dispatch pre-index (dispatch.go)
//     like any other. That is not a tidiness argument: #397 measured the
//     old per-event linear scan at ~2x the ingest budget at 1,000 entries
//     and ~11x at 5,000, and the pre-index is the ADR's stated answer to
//     exactly that.
//   - An inverted entry ("this device should only ever reach X") is the
//     observed/permitted/violation state machine with live SourceList
//     resolution, which is Go, not a form -- so it stays Go, wearing the
//     envelope, as one programmatic definition holding every inverted
//     entry (see InvertedExpectations).
//
// Both produce observations and violations through internal/matchlog
// (Route's expectation branch, router.go), never flags. The two intents
// stay distinct all the way down, per #385's decision; nothing here
// collapses an expectation into a detection.
//
// The matching rules themselves are NOT reimplemented here.
// watchlist.Match and watchlist.Coverage stay where they are, called from
// here, because they are what #397's characterization suite pins: a port
// that reproduces a pinned rule in new words is a port that has to be
// re-proved, and there is nothing to gain by re-proving matchInverted's
// structural-noise exemption in a second dialect. What moves is the
// machinery around them -- the queue, the loop, the recovery boundary,
// the emission path -- which is what was duplicated.

// ExpectationSourceIdentityCondition builds the FieldSourceIdentity
// condition for one stored source identity -- the MAC-preferred,
// IP-fallback scoping matchNonInverted applied by hand. Exported so a
// caller assembling conditions for an expectation outside this package
// (a future builder UI, #407's API) cannot get the Values order wrong.
func ExpectationSourceIdentityCondition(id matchlog.Identity) Condition {
	return Condition{Field: FieldSourceIdentity, Operator: OpEquals, Values: []string{id.MAC, id.IP}}
}

// expectationWindow sizes the CountRing behind every non-inverted
// expectation definition.
//
// A watchlist expectation has no threshold-over-window shape of its own:
// matchNonInverted recorded a match on *every* matching event, and
// matchlog's own Tuple collapsing (one Record per (entry, identity,
// destination, port), with a count) is what turns repetition into a
// count. So the definition's threshold is 1 -- every match emits, exactly
// as before -- and the window only decides how much ring one key's state
// occupies before it ages out. A minute is short enough to cost nothing
// and long enough that {Count} in the rendered Detail is a number over a
// span an operator would recognize.
//
// A var, not a const, so a test can size it explicitly rather than
// depending on this value.
var expectationWindow = time.Minute

// ExpectationDefinitionFor converts one watchlist entry into the
// Definition envelope it is evaluated as -- the same conversion
// MigrateDefinitions performs when seeding the definitions document
// (definitions_migrate.go), called here so the definition a live entry
// evaluates as and the definition it migrates into are the same value,
// produced by the same code, rather than two conversions that could
// drift.
func ExpectationDefinitionFor(entry watchlist.Entry) (Definition, error) {
	return convertWatchlistEntry(&entry)
}

// BuildExpectationDefinition builds the live DeclarativeDefinition for a
// non-inverted expectation, from the Definition envelope
// ExpectationDefinitionFor produced.
//
// Conditions, in the order matchNonInverted applied them:
//
//   - destinationPort in the entry's port list. First deliberately: it
//     is what BuildDispatchIndex's discriminantFor picks (dispatch.go),
//     and it is why 5,000 expectations cost an event the handful that
//     name its port rather than all 5,000. matchNonInverted's separate
//     `e.DstPort == 0` guard needs no counterpart -- a port list can only
//     hold 1-65535 (parsePort), so port 0 matches nothing anyway.
//   - connectionState in {"", "new"} -- watchlist.isTrackableConnState,
//     expressed as data, for the reason its own doc comment gives: a busy
//     accepted service's return traffic would otherwise swamp the entry.
//   - source scoping, exactly one of:
//     addressListMembership [device, list] when the entry is scoped to a
//     router's address list (#274 item 2 -- resolved live, per event,
//     against what the router has pushed), or sourceIdentity [mac, ip]
//     when it is scoped to a stored identity. Never both: Entry's own doc
//     comment records that an entry is scoped by an identity or by a
//     list, and silently intersecting them would make "no matches"
//     ambiguous.
//   - destinationAddress equals the entry's destination, when scoped.
//
// One piece of matchNonInverted is deliberately not a condition: its
// `id.Empty()` guard, which refused to record a match for an event
// carrying neither a source MAC nor a source IP. An unscoped expectation
// has no identity condition to hang that on, so it is enforced at the
// other end instead -- MatchlogSink drops an emission whose tuple has an
// empty identity (see its doc comment). The observable behaviour is
// identical (no record, no error), and matchlog would refuse such a
// record anyway (ErrEmptyIdentity).
//
// Evidence: none. matchlog.Record is evidence-first in a different sense
// than flags.Evidence -- it embeds the whole triggering event -- so
// accumulating a port/host set across a window would be a second,
// weaker copy of information the record already carries in full.
func BuildExpectationDefinition(def Definition, members AddressListMembership) (*DeclarativeDefinition, error) {
	if def.Intent != IntentExpectation {
		return nil, fmt.Errorf("engine: expectation definition %q has intent %q, want %q", def.ID, def.Intent, IntentExpectation)
	}
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	ports, err := paramPortList(params, "ports")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	portValues := make([]string, len(ports))
	for i, p := range ports {
		portValues[i] = strconv.Itoa(p)
	}

	conds := []Condition{
		{Field: FieldDestinationPort, Operator: OpInSet, Values: portValues},
		{Field: FieldConnectionState, Operator: OpInSet, Values: []string{"", "new"}},
	}

	listDevice, err := paramOptionalString(params, "sourceListDevice")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	listName, err := paramOptionalString(params, "sourceListList")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	sourceMAC, err := paramOptionalString(params, "sourceMac")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	sourceIP, err := paramOptionalString(params, "sourceIp")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	switch {
	case listDevice != "" && listName != "":
		conds = append(conds, Condition{
			Field: FieldAddressListMembership, Operator: OpEquals,
			Values: []string{listDevice, listName},
		})
	case sourceMAC != "" || sourceIP != "":
		conds = append(conds, ExpectationSourceIdentityCondition(matchlog.Identity{MAC: sourceMAC, IP: sourceIP}))
	}

	destIP, err := paramOptionalString(params, "destIp")
	if err != nil {
		return nil, fmt.Errorf("engine: expectation definition %q: %w", def.ID, err)
	}
	if destIP != "" {
		conds = append(conds, Condition{Field: FieldDestinationAddress, Operator: OpEquals, Values: []string{destIP}})
	}

	return NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:   conds,
		Key:          KeyPerSourcePort,
		Window:       expectationWindow,
		Threshold:    1,
		CountingMode: CountingTotal,
		DetailTemplate: fmt.Sprintf(
			"{Count} attempts against watched port {DestinationPort} from {SourceAddress} in %s", expectationWindow),
		Members: members,
	})
}

// paramOptionalString reads a ParamTypeStringList param that carries at
// most one value -- the shape watchlistNonInvertedParamSchema uses for
// every optional single field, because there is no single-optional-string
// ParamType (see optionalStringList, definitions_migrate.go). Absent, or
// present and empty, both mean "not scoped".
func paramOptionalString(params Params, name string) (string, error) {
	vs, err := paramStringList(params, name)
	if err != nil {
		return "", err
	}
	if len(vs) == 0 {
		return "", nil
	}
	if len(vs) > 1 {
		return "", fmt.Errorf("engine: param %q holds %d values, want at most one", name, len(vs))
	}
	return vs[0], nil
}

// --- the inverted half --------------------------------------------------

// InvertedExpectationsID is the definition id the inverted expectation
// state machine registers under. Fixed rather than generated: it is one
// definition holding every inverted entry (see InvertedExpectations), so
// re-registering it after an entry-set change replaces the previous
// registration wholesale, which is exactly Engine.Register's contract.
const InvertedExpectationsID = "inverted_expectations"

// ObservationRecorder is the slice of the entry set InvertedExpectations
// needs while an entry is still observing: record that this device
// reached this destination, without firing anything.
//
// An interface rather than the concrete store for the same reason every
// ShippedDeps field is one (programmatic.go): it keeps the state machine
// testable without standing up persistence, and it keeps the dependency
// direction pointing away from whatever ends up owning the entry set.
// A nil recorder is a valid "nowhere to record" no-op -- an observing
// entry then simply accumulates nothing, the same way a nil sink means a
// definition produces nothing observable.
type ObservationRecorder interface {
	RecordObservation(entryID, destIP string, port int, t time.Time)
}

// invertedExpectation is one inverted entry and the envelope it wears.
// The entry is held by value: it is a snapshot taken when the set was
// built, and InvertedExpectations is rebuilt (not mutated) whenever the
// entry set changes -- the same "build once, replace on change, never
// mutate in place" contract DispatchIndex states for the declarative
// side, and the reason nothing here needs a lock.
type invertedExpectation struct {
	def   Definition
	entry watchlist.Entry
}

// InvertedExpectations is the one programmatic expectation definition:
// every inverted watchlist entry's observed/permitted/violation state
// machine, wearing the chassis's envelope.
//
// One definition rather than one per entry, for a structural reason
// rather than a convenience one. Definition.Validate enforces that
// provenance=custom implies kind=declarative -- no request shape may
// express a custom programmatic definition (see Kind's doc comment) --
// so an operator-authored entry whose logic is Go cannot be its own
// definition envelope without either lying about its kind or claiming to
// be shipped, and a shipped definition can never be deleted (see
// ErrDefinitionImmutable), which an operator's own watchlist entry
// obviously must be. The honest shape is the one the ADR's own sentence
// uses: the inverted watchlist entry *is* the programmatic expectation
// definition -- one piece of built-in Go logic, whose per-entry data is
// data.
//
// Each entry still routes under its own envelope (see emit), so a match
// lands in internal/matchlog under the entry's own id exactly as before.
//
// Dispatch: entries are bucketed by the device identity they scope, so
// an event consults only the entries that could possibly be about it.
// An inverted entry always scopes a source (Store.Upsert refuses one
// without -- ErrInvertedRequiresSource), which is what makes the index
// total rather than a best effort with an always-consulted remainder.
type InvertedExpectations struct {
	byIdentity map[string][]*invertedExpectation
	// count is how many entries this set holds, kept because byIdentity's
	// map length counts devices, not entries.
	count int

	observations ObservationRecorder

	// OnRoutedEmission is the seam main.go wires onto a real
	// matchlog.Store (see MatchlogSink), exactly as on
	// DeclarativeDefinition. nil is a valid no-op.
	OnRoutedEmission func(RoutedEmission)
}

// NewInvertedExpectations builds the set from every inverted entry in
// entries; non-inverted entries are ignored (they are declarative
// definitions -- see BuildExpectationDefinition). An entry whose
// envelope cannot be built is a hard error naming it, never a silently
// dropped expectation: an entry that exists but evaluates nothing is
// precisely the "absence of detection presented as absence of threat"
// failure #380's first item describes.
func NewInvertedExpectations(entries []watchlist.Entry, observations ObservationRecorder) (*InvertedExpectations, error) {
	x := &InvertedExpectations{
		byIdentity:   make(map[string][]*invertedExpectation),
		observations: observations,
	}
	for _, e := range entries {
		if !e.Invert {
			continue
		}
		if e.Source.Empty() {
			// watchlist.Store.Upsert refuses this (ErrInvertedRequiresSource),
			// and matchInverted returns NoMatch for it, so such an entry
			// could only ever have been loaded from a document written
			// before that check existed. Skipped rather than errored, for
			// the same reason matchInverted tolerates it: refusing to
			// start over one stale entry would be worse than not
			// evaluating the one thing that never evaluated anyway.
			continue
		}
		def, err := ExpectationDefinitionFor(e)
		if err != nil {
			return nil, fmt.Errorf("engine: inverted expectation %q: %w", e.ID, err)
		}
		key := e.Source.Key()
		x.byIdentity[key] = append(x.byIdentity[key], &invertedExpectation{def: def, entry: e})
		x.count++
	}
	return x, nil
}

// ID satisfies Evaluated.
func (x *InvertedExpectations) ID() string { return InvertedExpectationsID }

// Kind satisfies Evaluated -- the inverted state machine is Go, so
// KindProgrammatic's string form.
func (x *InvertedExpectations) Kind() string { return string(KindProgrammatic) }

// Len reports how many inverted entries this set holds -- for a caller
// (or a test) that wants to assert the set was actually built, and for
// the same visibility reason DispatchIndex.AlwaysConsultedCount exists.
func (x *InvertedExpectations) Len() int { return x.count }

// SetSink wires this definition's emission sink, the same shape the
// programmatic kind uses (programmaticBase.SetSink).
func (x *InvertedExpectations) SetSink(sink func(RoutedEmission)) { x.OnRoutedEmission = sink }

// NonReplayableReason satisfies NonReplayable (replayability.go).
//
// Recorded on issue #406 and stated here rather than left implicit: an
// inverted expectation's answer is a function of an observation period
// that runs for days -- which destinations this device has been seen
// reaching, and which of those an operator has since promoted. Replay's
// corpus is internal/store's in-memory ring, measured in minutes (#385's
// own aggregates put a full corpus at four to six minutes of traffic),
// so replaying the state machine over it would answer "what would this
// have said if the device had only ever existed for the last five
// minutes" -- a confident number about a question nobody asked. Declining
// with the reason is the contract's whole point; this is revisited when
// retention lands, not before.
func (x *InvertedExpectations) NonReplayableReason() string {
	return "an inverted expectation's judgement comes from an observation period measured in days -- which destinations this device has been seen reaching, and which of those have been promoted -- and replay's corpus is the in-memory event ring, measured in minutes; replaying it would answer a different question confidently"
}

// Evaluate satisfies Evaluated: runs e against every inverted entry that
// scopes e's own source device, per watchlist.Match's inverted rule.
// An Observed outcome records a candidate destination and fires nothing
// (#243 section 5); a Violation routes an emission under that entry's own
// envelope.
func (x *InvertedExpectations) Evaluate(e store.Event) {
	id := eventIdentity(e)
	if id.Empty() {
		// No device to attribute this to, so no inverted entry can be
		// about it -- matchInverted's own first-line answer, reached here
		// without consulting anything.
		return
	}
	for _, x2 := range x.byIdentity[id.Key()] {
		tuple, outcome := watchlist.Match(x2.entry, e)
		switch outcome {
		case watchlist.Violation:
			x.emit(x2, tuple, e)
		case watchlist.Observed:
			if x.observations != nil {
				x.observations.RecordObservation(x2.entry.ID, tuple.DestIP, tuple.Port, e.ReceivedAt)
			}
		}
	}
}

// emit renders and routes one violation. The Detail names only what is
// true of this one event, because an inverted violation *is* one event:
// unlike a threshold-over-window definition there is no window for a
// sentence to misdescribe, which is the #379 discipline read in the
// direction it actually applies here.
func (x *InvertedExpectations) emit(ix *invertedExpectation, tuple matchlog.Tuple, e store.Event) {
	em, err := RenderEmission(nil, 1, "reached an unexpected destination", false)
	if err != nil {
		logger.Error(fmt.Sprintf("inverted expectation %q: RenderEmission failed: %v", ix.def.ID, err))
		return
	}
	em.DefinitionID = ix.def.ID
	em.Target = tuple.DestIP
	em.SourceIP = e.SrcIP
	em.Country = e.SrcCountry
	em.EventTime = e.ReceivedAt
	ev := e
	em.TriggeringEvent = &ev

	routed, err := Route(ix.def, em)
	if err != nil {
		logger.Error(fmt.Sprintf("inverted expectation %q: Route failed: %v", ix.def.ID, err))
		return
	}
	if x.OnRoutedEmission != nil {
		x.OnRoutedEmission(routed)
	}
}

// --- assembling both halves --------------------------------------------

// ExpectationSetID is the definition id the non-inverted expectations
// register under as one DeclarativeSet -- see NewDeclarativeSet for why
// a set, not one registration per definition: the chassis's own dispatch
// loop is a flat scan over what is registered, so the narrowing has to
// live one level below it.
const ExpectationSetID = "watchlist-expectations"

// ExpectationDeps is everything the expectation definitions need beyond
// the event stream, injected at construction -- the expectation-intent
// counterpart of ShippedDeps (programmatic.go), and nil-tolerant for the
// same reason: a deployment with no match log, no router state and no
// entry set still builds the whole thing, it simply produces nothing.
type ExpectationDeps struct {
	// Members resolves an entry's live address-list scoping (#274 item
	// 2). nil makes such an entry match nothing rather than guess, the
	// safe direction watchlist.MatchWithLists already chose.
	Members AddressListMembership
	// Sink receives every routed emission -- MatchlogSink in production.
	Sink func(RoutedEmission)
	// Observations records an observing inverted entry's candidate
	// destinations. See ObservationRecorder.
	Observations ObservationRecorder
}

// BuildExpectations turns one watchlist entry set into the two things
// the engine evaluates it as: a DeclarativeSet holding every
// non-inverted expectation behind a dispatch pre-index, and the single
// InvertedExpectations definition holding the rest.
//
// Both are returned rather than registered here, so the caller (main.go)
// decides when they go live -- and so re-registering after an entry edit
// is a Register call with the same two ids, replacing the previous pair
// wholesale rather than mutating anything an evaluation goroutine might
// be reading.
func BuildExpectations(entries []watchlist.Entry, deps ExpectationDeps) (*DeclarativeSet, *InvertedExpectations, error) {
	var decl []*DeclarativeDefinition
	for _, e := range entries {
		if e.Invert {
			continue
		}
		def, err := ExpectationDefinitionFor(e)
		if err != nil {
			return nil, nil, fmt.Errorf("engine: expectation %q: %w", e.ID, err)
		}
		dd, err := BuildExpectationDefinition(def, deps.Members)
		if err != nil {
			return nil, nil, err
		}
		dd.OnRoutedEmission = deps.Sink
		decl = append(decl, dd)
	}

	inverted, err := NewInvertedExpectations(entries, deps.Observations)
	if err != nil {
		return nil, nil, err
	}
	inverted.OnRoutedEmission = deps.Sink

	return NewDeclarativeSet(ExpectationSetID, decl), inverted, nil
}
