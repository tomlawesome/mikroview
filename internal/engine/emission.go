// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Emission is what RenderEmission produces: a definition's firing
// judgement, expressed only in terms of what its EvidenceSet actually
// accumulated. #405/#406 map this onto flags.Store.AddWithDetail or
// matchlog.Store.Append, whichever intent
// (docs/decisions/evaluation-engine.md section 3) the definition
// declares -- this issue is the contract those ports build on, not the
// port itself, so nothing constructs an Emission from real traffic yet.
type Emission struct {
	// DefinitionID identifies which Definition produced this emission --
	// grown onto Emission by #401 so Route (router.go) can check it
	// against the Definition it's routing for, rather than trusting a
	// caller never to mix the two up. Not set by RenderEmission (which
	// has no notion of which definition it's rendering for): a
	// definition's own evaluation code sets it after RenderEmission
	// returns, the same way Target/Confidence below are populated.
	DefinitionID string
	// Target is what this emission is about -- a source IP, "global",
	// a device ID, a rule label, whatever the definition's own Kind
	// keys its judgement on (the same range flags.Flag.Target and
	// matchlog.Tuple already cover). Populated by the definition's own
	// evaluation code, not by RenderEmission.
	Target string
	// Detail is RenderEmission's rendered text -- see its doc comment.
	Detail string
	// Ports/Hosts/Labels are the same accumulated values Detail was
	// rendered from, exposed structurally (e.g. for flags.Evidence)
	// rather than requiring a caller to re-derive them by re-parsing
	// Detail.
	Ports  []int
	Hosts  []string
	Labels []string
	// Confidence is 0-100, set only by a definition that makes a
	// statistical judgment call rather than a deterministic threshold
	// crossing -- the Emission-level counterpart to flags.Flag.Confidence
	// and mirroring its "nil means not scored" convention. Populated by
	// the definition's own evaluation code, not by RenderEmission, which
	// has no confidence computation of its own.
	Confidence *int
	// Provisional marks an emission produced while the definition's
	// baseline (see Baseline/Snapshot.Ready) had not yet cleared its
	// history floor -- see docs/decisions/evaluation-engine.md section 1
	// and Baseline's own doc comment for why a definition is allowed to
	// emit during warm-up at all, provided it is never silent about it.
	// A definition that never uses a Baseline leaves this false, which
	// RenderEmission does by default -- Provisional is set by the
	// caller, not derived here, since RenderEmission has no baseline of
	// its own to consult.
	Provisional bool
}

// emissionToken matches a {Name} placeholder in a Detail template --
// deliberately not text/template syntax: this package's placeholder
// substitution is intentionally far narrower than a general templating
// engine (no conditionals, no loops, no method calls), in keeping with
// docs/decisions/evaluation-engine.md's "No DSL" decision for match
// conditions -- a Detail string has even less reason to grow one. It
// also keeps RenderEmission off text/template entirely, which this
// repository's injection-sink audit forbids outright
// (injection_sinks_test.go, docs/decisions/injection-audit.md) since it
// does not escape for any output context; a hand-rolled, single-purpose
// substitution over a fixed, known field set sidesteps that class of
// risk rather than requesting an exemption from it.
var emissionToken = regexp.MustCompile(`\{([A-Za-z]+)\}`)

// RenderEmission builds an Emission from evidence and a Detail template,
// never from a pre-rendered string a definition assembled itself by
// interpolating raw event fields -- see EvidenceSet's doc comment for
// why that indirection is the whole point: it is the mechanism behind
// #379's wrong-naming findings (a flag claiming a port/host/label it
// never actually recorded). detailTemplate may reference {Ports}/
// {Hosts}/{Labels} (the accumulated, sorted values) and {PortCount}/
// {HostCount}/{LabelCount} (their lengths) -- nothing else.
//
// Referencing a name evidence was never Add-ed to (see
// EvidenceSet.touched), or any name outside that fixed set, is a hard
// render error, not a silently empty value: RenderEmission only ever
// substitutes a token whose name is a key in a data set built from the
// categories evidence actually touched, so a template asking for
// {Hosts} when nothing ever called AddHost fails exactly the way it
// would if {Hosts} were misspelled -- the un-accumulated-value mistake
// #379 found is structural here, not a matter of care. See
// TestRenderEmissionFailsOnUnaccumulatedValue.
func RenderEmission(evidence *EvidenceSet, detailTemplate string, provisional bool) (Emission, error) {
	if evidence == nil {
		evidence = NewEvidenceSet()
	}

	em := Emission{Provisional: provisional}
	data := map[string]string{}

	portsSeen, hostsSeen, labelsSeen := evidence.touched()
	if portsSeen {
		em.Ports = evidence.Ports()
		data["Ports"] = formatInts(em.Ports)
		data["PortCount"] = strconv.Itoa(len(em.Ports))
	}
	if hostsSeen {
		em.Hosts = evidence.Hosts()
		data["Hosts"] = formatStrings(em.Hosts)
		data["HostCount"] = strconv.Itoa(len(em.Hosts))
	}
	if labelsSeen {
		em.Labels = evidence.Labels()
		data["Labels"] = formatStrings(em.Labels)
		data["LabelCount"] = strconv.Itoa(len(em.Labels))
	}

	var unaccumulated []string
	detail := emissionToken.ReplaceAllStringFunc(detailTemplate, func(tok string) string {
		name := tok[1 : len(tok)-1]
		val, ok := data[name]
		if !ok {
			unaccumulated = append(unaccumulated, name)
			return tok
		}
		return val
	})
	if len(unaccumulated) > 0 {
		sort.Strings(unaccumulated)
		return Emission{}, fmt.Errorf(
			"engine: emission Detail template named %v, which its EvidenceSet never accumulated (Add never called for it, or the name is unknown)",
			unaccumulated)
	}

	em.Detail = detail
	return em, nil
}

func formatInts(vs []int) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

func formatStrings(vs []string) string {
	return strings.Join(vs, ", ")
}
