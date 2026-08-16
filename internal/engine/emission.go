// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
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
	// SourceIP is the triggering event's source address, carried
	// separately from Target because the two are not always the same
	// string -- repeated_drops' Target is a "<source> -> port <N>"
	// composite, while anything wanting to look the source up (see
	// ReputationSink) needs the address itself. Empty when the definition
	// has no single meaningful source (a global-keyed one). Populated by
	// the definition's own evaluation code, like Target.
	SourceIP string
	// Ports/Hosts/Labels are the same accumulated values Detail was
	// rendered from, exposed structurally (e.g. for flags.Evidence)
	// rather than requiring a caller to re-derive them by re-parsing
	// Detail.
	Ports  []int
	Hosts  []string
	Labels []string
	// NAT is the triggering event's NAT translation detail, when the
	// definition declared EvidenceNAT and the event carried one -- see
	// EvidenceSet.SetNAT.
	NAT *NATInfo
	// Confidence is 0-100, set only by a definition that makes a
	// statistical judgment call rather than a deterministic threshold
	// crossing -- the Emission-level counterpart to flags.Flag.Confidence
	// and mirroring its "nil means not scored" convention. Populated by
	// the definition's own evaluation code, not by RenderEmission, which
	// has no confidence computation of its own.
	Confidence *int
	// Country/EventTime carry the triggering store.Event's SrcCountry and
	// ReceivedAt forward -- added by #405, which is this package's first
	// real producer of an Emission from live traffic (router.go's own doc
	// comment on Route notes flags.Flag's store-assigned fields are left
	// zero "those belong to flags.Store's raise lifecycle," but Country
	// and the raise timestamp are not store-assigned: flags.Store.AddProvisional
	// needs both supplied by its caller on every call, the same way
	// internal/detect's own detectors read them straight off the
	// triggering event -- see e.g. observeCriticalPort's
	// AddWithDetail(..., e.SrcCountry, now) call). RenderEmission has no
	// event to read them from, so -- like Target/DefinitionID -- these are
	// set by the definition's own evaluation code after RenderEmission
	// returns, not by RenderEmission itself.
	Country   string
	EventTime time.Time
	// TriggeringEvent is the store.Event this emission fired on, carried
	// only by an expectation-intent definition and nil everywhere else.
	//
	// This is the one place Emission is not purely "a definition's
	// accumulated judgement", and it is deliberate rather than a leak
	// (#406). matchlog.Record is evidence-first by design -- its own doc
	// comment: "it embeds the full matched event so an operator
	// investigating has everything the live view would have shown, not a
	// summary reconstructed later from a smaller record" -- and it keys
	// on a matchlog.Tuple (source identity, destination address, port)
	// that is a property of the triggering packet, not of the window.
	// Neither can be rebuilt from Detail/Ports/Hosts/Labels: an accumulated
	// judgement has thrown that information away by construction. So an
	// expectation-intent definition sets this after RenderEmission
	// returns, exactly as it sets Target/Country/EventTime, and Route
	// (router.go) derives MatchlogWrite's Tuple and Event from it --
	// which is what MatchlogWrite's own doc comment always said would
	// have to happen: "Only the wiring that has an actual event in hand
	// (#406) can supply those."
	//
	// Route reads it on the expectation branch only. A detection-intent
	// definition is free to leave it set and it is simply ignored --
	// which is deliberate, because the alternative would be for a
	// definition's own Evaluate to branch on its Intent, and Intent
	// decides what an emission feeds and nothing else (see Intent's own
	// doc comment). A flags.Flag is an aggregate over a window; handing
	// it one event would be the single-event-stands-for-the-window claim
	// #379 found, so routeToFlag never looks at this field at all.
	TriggeringEvent *store.Event
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
// {Hosts}/{Labels} (the accumulated, sorted values), {PortCount}/
// {HostCount}/{LabelCount} (their lengths), and {Count} (see below) --
// nothing else.
//
// count is the threshold-crossing tally that caused this emission --
// CountRing.Count for CountingTotal, DistinctRing.Count for
// CountingDistinct (see DeclarativeDefinition.Evaluate/Replay, both of
// which already compute this value before calling RenderEmission) --
// exposed as {Count} so a Detail template can state "how many" without
// that number necessarily equalling any evidence category's own length:
// critical_port's #379 fix is the reason this parameter exists at all --
// "N attempts against critical ports {Ports} in <window>" needs N (the
// total attempt count) and {Ports} (the distinct port set) to both be
// real, independently-sized numbers in the same sentence, and PortCount
// alone cannot honestly be both. Unlike Ports/Hosts/Labels, {Count} is
// always available (never gated on "was anything accumulated") since a
// definition only ever calls RenderEmission once its own threshold has
// actually been crossed by some count.
//
// Referencing a name evidence was never Add-ed to (see
// EvidenceSet.touched), or any name outside the fixed set above, is a
// hard render error, not a silently empty value: RenderEmission only
// ever substitutes a token whose name is a key in a data set built from
// the categories evidence actually touched (plus the always-present
// Count), so a template asking for {Hosts} when nothing ever called
// AddHost fails exactly the way it would if {Hosts} were misspelled --
// the un-accumulated-value mistake #379 found is structural here, not a
// matter of care. See TestRenderEmissionFailsOnUnaccumulatedValue.
func RenderEmission(evidence *EvidenceSet, count int, detailTemplate string, provisional bool) (Emission, error) {
	if evidence == nil {
		evidence = NewEvidenceSet()
	}

	em := Emission{Provisional: provisional}
	data := map[string]string{"Count": strconv.Itoa(count)}

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
	// NAT is structural only -- it has no token, because it describes one
	// specific packet's rewrite rather than anything accumulated across
	// the window, and a Detail sentence naming it would make exactly the
	// single-event-stands-for-the-window claim #379 found.
	em.NAT = evidence.NAT()

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
