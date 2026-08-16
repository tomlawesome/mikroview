// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"net"
	"strconv"

	"github.com/tomlawesome/mikroview/internal/store"
)

// dispatchField is the small set of fields the dispatch pre-index is
// allowed to key on -- docs/decisions/evaluation-engine.md's "Costs,
// stated" section names destination port, address class and chain by
// example; issue #402 adds rule label to that list explicitly. Far
// smaller than Field's full closed set on purpose: a discriminating
// field has to (a) appear in a definition's own conditions with a small,
// enumerable, *positive* value set (OpEquals or OpInSet -- never a
// negation, a range or a CIDR, none of which narrow a bucket at all) and
// (b) be cheap to read off an event without doing any of the work
// matching itself would do.
type dispatchField int

const (
	dispatchDestinationPort dispatchField = iota
	dispatchChain
	dispatchRuleLabel
	dispatchAddressClass
)

// discriminantValuesFor scans conds for a positive (OpEquals/OpInSet)
// condition on field, returning its Values verbatim -- the enumerable
// key(s) BuildDispatchIndex buckets the owning definition under.
func discriminantValuesFor(conds []Condition, field Field) ([]string, bool) {
	for _, c := range conds {
		if c.Field != field {
			continue
		}
		if c.Operator != OpEquals && c.Operator != OpInSet {
			continue
		}
		return append([]string(nil), c.Values...), true
	}
	return nil, false
}

// classDiscriminant looks for a matchesClassification condition on
// either address field with a concrete ("internal" or "external", never
// "any" -- which discriminates nothing, it matches every event) value.
// The returned key encodes which address field the classification was
// asserted on, since a source-address classification and a destination-
// address classification bucket independently (see Candidates).
func classDiscriminant(conds []Condition) (string, bool) {
	for _, c := range conds {
		if c.Operator != OpMatchesClassification {
			continue
		}
		if c.Field != FieldSourceAddress && c.Field != FieldDestinationAddress {
			continue
		}
		if len(c.Values) != 1 {
			continue
		}
		class := c.Values[0]
		if class != "internal" && class != "external" {
			continue
		}
		return classKey(c.Field, class), true
	}
	return "", false
}

func classKey(field Field, class string) string { return string(field) + ":" + class }

// discriminantFor picks conds' cheapest discriminating field, trying
// destination port first (the cheapest, most selective in practice --
// a handful of distinct values out of the 65535 the field could hold),
// then chain and rule label (both small, fixed, operator-defined
// vocabularies), then address class last (only two buckets, so it
// narrows the least of the four). Returns ok=false when none of conds
// qualifies -- the definition belongs in the always-consulted bucket.
func discriminantFor(conds []Condition) (dispatchField, []string, bool) {
	if vals, ok := discriminantValuesFor(conds, FieldDestinationPort); ok {
		return dispatchDestinationPort, vals, true
	}
	if vals, ok := discriminantValuesFor(conds, FieldChain); ok {
		return dispatchChain, vals, true
	}
	if vals, ok := discriminantValuesFor(conds, FieldRuleLabel); ok {
		return dispatchRuleLabel, vals, true
	}
	if key, ok := classDiscriminant(conds); ok {
		return dispatchAddressClass, []string{key}, true
	}
	return 0, nil, false
}

// DispatchIndex is the declarative dispatch pre-index
// (docs/decisions/evaluation-engine.md, "Costs, stated"): definitions
// bucketed by their cheapest discriminating field, so an event consults
// the handful of definitions that could actually match rather than every
// declarative definition linearly. Built once, from a snapshot of the
// definition set, by BuildDispatchIndex -- there is no method on this
// type that mutates it, which is what makes "rebuild only on
// definition-set change, never per event" true structurally rather than
// by convention: the only way to get a different DispatchIndex is to
// call BuildDispatchIndex again (typically wrapped by constructing a new
// DeclarativeSet, see NewDeclarativeSet), and nothing on the per-event
// path (Candidates) does that.
type DispatchIndex struct {
	byPort  map[int][]*DeclarativeDefinition
	byChain map[string][]*DeclarativeDefinition
	byRule  map[string][]*DeclarativeDefinition
	byClass map[string][]*DeclarativeDefinition
	// global holds every definition BuildDispatchIndex could not find a
	// discriminating field for -- consulted for every event,
	// unconditionally. AlwaysConsultedCount exposes its size (and Build
	// logs it) precisely so "every definition ended up global" -- the
	// index providing no narrowing at all -- is visible rather than a
	// silent, purely decorative pre-index.
	global []*DeclarativeDefinition
}

// BuildDispatchIndex builds a DispatchIndex over defs. Conditions were
// already validated when each *DeclarativeDefinition was constructed
// (NewDeclarativeDefinition -> compileConditions), so the port values
// discriminantFor returns are always well-formed decimal strings in
// range here -- strconv.Atoi is not re-validated.
func BuildDispatchIndex(defs []*DeclarativeDefinition) *DispatchIndex {
	idx := &DispatchIndex{
		byPort:  make(map[int][]*DeclarativeDefinition),
		byChain: make(map[string][]*DeclarativeDefinition),
		byRule:  make(map[string][]*DeclarativeDefinition),
		byClass: make(map[string][]*DeclarativeDefinition),
	}
	for _, d := range defs {
		field, vals, ok := discriminantFor(d.conditions)
		if !ok {
			idx.global = append(idx.global, d)
			continue
		}
		switch field {
		case dispatchDestinationPort:
			for _, v := range vals {
				p, err := strconv.Atoi(v)
				if err != nil {
					continue
				}
				idx.byPort[p] = append(idx.byPort[p], d)
			}
		case dispatchChain:
			for _, v := range vals {
				idx.byChain[v] = append(idx.byChain[v], d)
			}
		case dispatchRuleLabel:
			for _, v := range vals {
				idx.byRule[v] = append(idx.byRule[v], d)
			}
		case dispatchAddressClass:
			for _, v := range vals {
				idx.byClass[v] = append(idx.byClass[v], d)
			}
		}
	}
	logDispatchIndexBuilt(len(defs), len(idx.global))
	return idx
}

func logDispatchIndexBuilt(total, global int) {
	logger.Info(fmt.Sprintf("declarative dispatch index built: %d definition(s), %d always-consulted (no discriminating field)", total, global))
	if total > 0 && global == total {
		logger.Warn("declarative dispatch index: every definition landed in the always-consulted bucket -- the pre-index is providing no narrowing at all")
	}
}

// classKeyFor computes the classKey an event's field address would hit
// in idx.byClass, if the address parses at all.
func classKeyFor(field Field, addr string) (string, bool) {
	ip := net.ParseIP(addr)
	if ip == nil {
		return "", false
	}
	class := "external"
	if !isPublicIP(ip) {
		class = "internal"
	}
	return classKey(field, class), true
}

// Candidates returns the bounded subset of definitions that could match
// e: every always-consulted definition, plus whatever each discriminating
// bucket holds for e's own destination port, chain, rule label and
// source/destination address class. A definition appears at most once
// per bucket family (each definition picked exactly one discriminating
// field at Build time -- see discriminantFor), so the result needs no
// deduplication.
func (idx *DispatchIndex) Candidates(e store.Event) []*DeclarativeDefinition {
	out := append([]*DeclarativeDefinition(nil), idx.global...)
	if e.DstPort != 0 {
		out = append(out, idx.byPort[e.DstPort]...)
	}
	if e.Chain != "" {
		out = append(out, idx.byChain[e.Chain]...)
	}
	if e.RuleLabel != "" {
		out = append(out, idx.byRule[e.RuleLabel]...)
	}
	if key, ok := classKeyFor(FieldSourceAddress, e.SrcIP); ok {
		out = append(out, idx.byClass[key]...)
	}
	if key, ok := classKeyFor(FieldDestinationAddress, e.DstIP); ok {
		out = append(out, idx.byClass[key]...)
	}
	return out
}

// AlwaysConsultedCount reports how many definitions landed in the
// always-consulted bucket -- see global's own doc comment.
func (idx *DispatchIndex) AlwaysConsultedCount() int { return len(idx.global) }

// DeclarativeSet groups many DeclarativeDefinitions behind one
// DispatchIndex and registers on an Engine as a single Evaluated --
// #398's own dispatch loop (Engine.evaluateEvent) is a flat scan over
// whatever is registered, unchanged by this issue, so narrowing "all
// declarative definitions" down to "the handful that could match" has to
// live one level below the chassis, inside whatever gets registered, not
// inside engine.go itself.
type DeclarativeSet struct {
	id    string
	index *DispatchIndex
}

// NewDeclarativeSet builds the pre-index once, at construction -- see
// DispatchIndex's own doc comment for why that is the only time it is
// ever rebuilt: a definition-set change means constructing a new
// DeclarativeSet (and Engine.Register-ing it under the same id, which
// replaces the prior registration -- see Engine.Register), never
// mutating an existing DispatchIndex in place.
func NewDeclarativeSet(id string, defs []*DeclarativeDefinition) *DeclarativeSet {
	return &DeclarativeSet{id: id, index: BuildDispatchIndex(defs)}
}

// ID satisfies Evaluated.
func (s *DeclarativeSet) ID() string { return s.id }

// Kind satisfies Evaluated.
func (s *DeclarativeSet) Kind() string { return string(KindDeclarative) }

// Evaluate satisfies Evaluated: consults only DispatchIndex.Candidates'
// bounded subset for e, not every declarative definition in the set --
// see TestDispatchIndexCandidatesIsABoundedSubset for the instrumented
// proof of "bounded," and BenchmarkDeclarativeDispatch
// (declarative_bench_test.go) for its cost.
func (s *DeclarativeSet) Evaluate(e store.Event) {
	for _, d := range s.index.Candidates(e) {
		d.Evaluate(e)
	}
}

// Index exposes the built DispatchIndex read-only -- mainly so a caller
// (or a test) can inspect AlwaysConsultedCount without re-deriving it.
func (s *DeclarativeSet) Index() *DispatchIndex { return s.index }
