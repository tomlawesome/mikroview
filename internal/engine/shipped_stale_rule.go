// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("stale_rule", buildStaleRuleDefinition)
}

// staleRuleDefinition is stale_rule ported onto the chassis (issue #405,
// originally #102): a firewall rule that has not fired in longer than
// maxAge is either dead weight or, worse, an unnecessary hole, and
// flagging it for human review closes attack surface at essentially no
// cost.
//
// # Ticked, and coarsely
//
// "Has not fired in a while" is a property of the passage of time, not
// of any event, so there is nothing an Evaluate call could contribute --
// the same absence-of-events shape device_silence has. The cadence is a
// param rather than a constant because it always was operator-set:
// main.go read config.Flags.StaleRuleCheckInterval into its own ticker.
// Ticked makes a definition declare its own cadence, so the declaration
// reads the param.
//
// # An accepted trade-off, carried over verbatim
//
// A rule the operator has already removed will still surface as stale
// until the resulting flag is manually cleared. mikroview has no
// visibility into the router's actually-configured rule set (it is
// passive-syslog-only), so "has not fired in a while" cannot be
// distinguished from "no longer exists". This is a product decision, not
// an oversight: the implied suggestion ("consider removing this rule") is
// a no-op if it is already gone, and the alternative failure mode -- a
// genuinely forgotten, still-open rule going unflagged -- is worse.
//
// # No scope, no confidence
//
// internal/detect raised this through flags.Store.Add: no confidence
// score, no evidence, no country. It was also the one detector with no
// DetectorName at all, so it had no enabled toggle and no scope either.
// It gets both here, because every definition wears the same envelope --
// what it does not get is a *different* default: shipped enabled and
// unscoped is exactly how it behaved.
type staleRuleDefinition struct {
	programmaticBase

	maxAge        time.Duration
	checkInterval time.Duration

	rules StaleRuleLister
}

func buildStaleRuleDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	maxAge, err := paramDuration(params, "maxAge")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	checkInterval, err := paramDuration(params, "checkInterval")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	return &staleRuleDefinition{
		programmaticBase: programmaticBase{def: def},
		maxAge:           maxAge,
		checkInterval:    checkInterval,
		rules:            deps.Rules,
	}, nil
}

// Evaluate satisfies Evaluated and does nothing -- see this type's own
// doc comment.
func (d *staleRuleDefinition) Evaluate(store.Event) {}

// TickInterval satisfies Ticked.
func (d *staleRuleDefinition) TickInterval() time.Duration { return d.checkInterval }

// Tick satisfies Ticked: one sweep of the rule-usage records.
//
// Re-firing on every sweep while a rule stays stale is intentional and
// harmless -- flags.Store updates an already-active flag in place rather
// than creating a duplicate, so this keeps LastSeen/Count current without
// spamming a new episode each time.
func (d *staleRuleDefinition) Tick(now time.Time) {
	if !d.def.Enabled || d.rules == nil || d.maxAge <= 0 {
		return
	}
	for _, u := range d.rules.StaleRules(d.maxAge, now) {
		idleDays := now.Sub(u.LastSeen).Hours() / 24
		d.emit(Emission{
			Target: u.Rule,
			Detail: fmt.Sprintf("no traffic in %.1f days (last seen %s, first seen %s, %d hits total)",
				idleDays, u.LastSeen.Format(time.RFC3339), u.FirstSeen.Format(time.RFC3339), u.Count),
			// No Confidence: internal/detect used flags.Store.Add. There
			// is no statistical judgement here to score -- the rule either
			// has fired inside maxAge or it has not.
			EventTime: now,
		})
	}
}

// NonReplayableReason satisfies NonReplayable.
//
// The same absence-of-events shape device_silence declares, with a second
// reason on top of it that is worth stating separately because it would
// survive even if the first were solved.
//
// First: the firing condition is that a rule label has NOT appeared for
// thirty days. A corpus is a list of events that did happen; the rules
// that matter here are precisely the ones with no events in it, and a
// corpus cannot distinguish "this rule exists and has been silent" from
// "this rule was never configured on this router at all". The comparison
// is against internal/rules' long-lived usage records -- FirstSeen,
// LastSeen and a lifetime hit count accumulated over months -- which are
// not derivable from an in-memory event window measured in minutes.
//
// Second, and this is the one a longer corpus would not fix: the
// judgement is config-versus-history, not traffic-versus-threshold. It
// says something about what the operator's rule set should be, and the
// honest answer depends on facts mikroview does not have (see this
// type's own doc comment on rules already removed). Replaying it with a
// swept maxAge would produce a table of "how many rules would be called
// stale at 15/30/60 days", which reads as a tuning receipt but is really
// just a histogram of rule idle times wearing a detection's clothes.
func (d *staleRuleDefinition) NonReplayableReason() string {
	return fmt.Sprintf(
		"stale_rule fires on the absence of a rule's events over %s, judged against long-lived rule-usage records rather than against traffic; an event corpus contains neither the absence nor the records, and cannot tell a rule that has been silent from one that was never configured, so no replay over one could report what this definition would have said",
		d.maxAge)
}
