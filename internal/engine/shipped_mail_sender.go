// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("unexpected_mail_sender", buildUnexpectedMailSenderDefinition)
}

// mailSenderDefinition is mail_sender ported onto the chassis (issue
// #405, originally #108): a LAN host with no admin-acknowledged reason
// to send mail originating an outbound connection to an external
// destination on an SMTP port.
//
// # Deterministic, and deliberately so
//
// Unlike almost everything else in the shipped catalogue there is no
// threshold, no window and no baseline: one qualifying connection is the
// whole signal, the same "have I ever seen this before" shape
// TypeNewDevice and TypeStaleRule have. A host that has never been
// tagged as a mail sender suddenly speaking SMTP outbound is a strong,
// simple compromised-device/spambot signal, and averaging it over a
// window would only delay saying so.
//
// # Programmatic, not declarative -- and this one is a close call
//
// Ports, direction and connection state are all expressible as
// conditions, and a threshold of one over any window would do. What is
// not expressible is the allowlist: the firing decision consults
// internal/entities for a tag on the source host, which is external data
// (see programmatic.go's own list of what the kind is for), changes
// while the process runs, and has no representation in the condition
// language at all. Expressing the easy 90% declaratively and bolting the
// allowlist on as a special case would be worse than saying plainly that
// this one is built in.
//
// # No scope, no confidence, no evidence -- preserved exactly
//
// internal/detect raised this through flags.Store.Add: no confidence
// score (there is no judgement to score -- it either happened or it did
// not), no evidence set, and no country badge (the source is a LAN
// address). All three are reproduced, and the empty Evidence in
// particular is pinned rather than left to drift into "we may as well
// populate Hosts now that we can."
type mailSenderDefinition struct {
	programmaticBase

	ports      map[int]bool
	trustedTag string

	entities EntityTagLookup
}

func buildUnexpectedMailSenderDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	ports, err := paramPortList(params, "ports")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	tags, err := paramStringList(params, "trustedTag")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	portSet := make(map[int]bool, len(ports))
	for _, p := range ports {
		portSet[p] = true
	}
	trustedTag := ""
	if len(tags) > 0 {
		trustedTag = tags[0]
	}

	return &mailSenderDefinition{
		programmaticBase: programmaticBase{def: def},
		ports:            portSet,
		trustedTag:       trustedTag,
		entities:         deps.Entities,
	}, nil
}

// Evaluate satisfies Evaluated.
//
// The direction gate (internal source, external destination) was applied
// by internal/detect's Observe before observeMailSender was ever called;
// it lives here now, which is where a definition's own preconditions
// belong.
//
// A nil entity lookup is "no allowlist configured", not an error -- the
// same nil-is-inert contract every optional ShippedDeps field carries,
// and exactly what internal/detect's own nil d.entities check did.
func (d *mailSenderDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || e.DstIP == "" || !d.def.Enabled {
		return
	}
	if !scopeMatchesSource(d.def.Scope, e.SrcIP) {
		return
	}
	if isPublicIPAddress(e.SrcIP) || !isPublicIPAddress(e.DstIP) {
		return
	}
	if !d.ports[e.DstPort] {
		return
	}
	if d.entities != nil && d.trustedTag != "" &&
		d.entities.HasTag(entities.TypeHost, e.SrcIP, d.trustedTag) {
		return
	}

	d.emit(Emission{
		Target: e.SrcIP,
		Detail: fmt.Sprintf("outbound connection to %s:%d (SMTP)", e.DstIP, e.DstPort),
		// No Confidence: internal/detect used flags.Store.Add, which
		// leaves it nil. There is no statistical judgement here to score.
		// No Evidence, no Country -- see this type's own doc comment.
		SourceIP:  e.SrcIP,
		EventTime: e.ReceivedAt,
	})
}

// NonReplayableReason satisfies NonReplayable.
//
// This is the reputation-shaped dishonesty #403 names, in a different
// costume: the firing decision is "was this host tagged
// trusted-mail-sender", and the only tag set a replay can consult is
// today's. A host tagged last week reads as never-flagged for every
// event in the corpus, including ones from before the tag existed; a
// host whose tag was removed reads as flagged for events from while it
// was still trusted. Neither is a statement about what this definition
// would have said at the time, which is the question a replay receipt
// claims to answer.
//
// The alternative -- replaying against no allowlist at all -- would be a
// count of every outbound SMTP connection in the corpus presented as a
// count of detections, which is worse: it is confidently wrong rather
// than honestly absent.
//
// Nothing about this is fixable by a longer corpus, so it is declared
// once rather than declined per call. If entity tags ever grow a history
// (tagged-at/untagged-at rather than a current set), this declaration is
// what should be revisited.
func (d *mailSenderDefinition) NonReplayableReason() string {
	return "unexpected_mail_sender fires on whether the source host is currently tagged as a trusted mail sender; entity tags carry no history, so a replay could only ever apply today's allowlist to yesterday's events and would report what this definition would say now, not what it would have said then"
}
