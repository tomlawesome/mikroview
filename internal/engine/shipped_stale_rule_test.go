// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/rules"
)

// rulesStoreLister is main.go's staleRuleLister -- kept test-locally so
// these tests run against the real *rules.Store rather than a fake,
// which is what internal/detect's own stale-rule tests did.
//
// Unlike main.go's original, this one honours the maxAge it is passed:
// the definition's own param is the authority now, which is the point of
// the param existing.
type rulesStoreLister struct{ ru *rules.Store }

func (a rulesStoreLister) StaleRules(maxAge time.Duration, now time.Time) []RuleUsage {
	if a.ru == nil {
		return nil
	}
	stale := a.ru.Stale(maxAge, now)
	out := make([]RuleUsage, 0, len(stale))
	for _, u := range stale {
		out = append(out, RuleUsage{Rule: u.Rule, FirstSeen: u.FirstSeen, LastSeen: u.LastSeen, Count: int(u.Count)})
	}
	return out
}

func newShippedStaleRuleDefinition(t *testing.T, maxAge time.Duration) (*staleRuleDefinition, *rules.Store, *flags.Store) {
	t.Helper()
	ru, err := rules.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs := newTestFlagsStore(t)
	def := Definition{
		ID:      "stale_rule",
		Name:    "Stale rule",
		Intent:  IntentDetection,
		Kind:    KindProgrammatic,
		Enabled: true,
		Params: Params{
			"maxAge":        maxAge.String(),
			"checkInterval": time.Hour.String(),
		},
		ParamSchema: StaleRuleParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{Rules: rulesStoreLister{ru: ru}})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(stale_rule): %v", err)
	}
	d := built.(*staleRuleDefinition)
	d.SetSink(FlagsSink(fs))
	return d, ru, fs
}

func TestShippedStaleRuleIgnoresFreshRules(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-time.Hour))
	d.Tick(now)

	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a recently-fired rule to not be flagged, got %+v", got)
	}
}

func TestShippedStaleRuleFlagsRuleIdleBeyondThreshold(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	lastSeen := now.Add(-40 * 24 * time.Hour)
	ru.Touch("r1", lastSeen)
	d.Tick(now)

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one stale_rule flag, got %+v", list)
	}
	if list[0].Type != flags.TypeStaleRule {
		t.Errorf("expected type %q, got %q", flags.TypeStaleRule, list[0].Type)
	}
	if list[0].Target != "r1" {
		t.Errorf("expected target to be the rule label, got %q", list[0].Target)
	}
	// The Detail's own shape, byte for byte on everything except the
	// idle-days figure, which is a %.1f of this test's own chosen
	// timestamps rather than a product contract.
	want := "no traffic in 40.0 days (last seen " + lastSeen.Format(time.RFC3339) +
		", first seen " + lastSeen.Format(time.RFC3339) + ", 1 hits total)"
	if list[0].Detail != want {
		t.Errorf("Detail = %q, want %q", list[0].Detail, want)
	}
	if list[0].Confidence != nil {
		t.Errorf("expected no confidence score (deterministic, unscored), got %v", *list[0].Confidence)
	}
}

// TestShippedStaleRuleThresholdIsExclusiveAtBoundary is
// internal/detect/stale_rule_test.go's test of the same name: exactly at
// maxAge is not yet stale; one second past it is.
func TestShippedStaleRuleThresholdIsExclusiveAtBoundary(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-30*24*time.Hour))
	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a rule exactly at the threshold to not be flagged yet, got %+v", got)
	}

	d.Tick(now.Add(30*24*time.Hour + time.Second))
	if got := fs.List(); len(got) != 1 {
		t.Fatalf("expected the rule to be flagged once past the threshold, got %+v", got)
	}
}

func TestShippedStaleRuleOnlyFlagsRulesPastThreshold(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("fresh", now.Add(-time.Hour))
	ru.Touch("stale", now.Add(-45*24*time.Hour))
	d.Tick(now)

	list := fs.List()
	if len(list) != 1 || list[0].Target != "stale" {
		t.Fatalf("expected only the stale rule to be flagged, got %+v", list)
	}
}

// TestShippedStaleRuleRepeatedSweepsDedupeRatherThanDuplicate confirms
// repeated sweeps while a rule stays stale update the same episode in
// place rather than raising a fresh one each time.
func TestShippedStaleRuleRepeatedSweepsDedupeRatherThanDuplicate(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Tick(now)
	d.Tick(now.Add(time.Hour))
	d.Tick(now.Add(2 * time.Hour))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected repeated sweeps to update one flag episode, got %d: %+v", len(list), list)
	}
	if list[0].Count != 3 {
		t.Errorf("expected Count to track 3 re-fires, got %d", list[0].Count)
	}
}

func TestShippedStaleRuleClearedFlagIsRevivedIfStillStale(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Tick(now)
	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("setup: expected one flag, got %+v", list)
	}
	if _, ok := fs.SetVerdict(list[0].ID, flags.VerdictChecked, "operator", now); !ok {
		t.Fatal("setup: expected Clear to succeed on the active flag")
	}

	d.Tick(now.Add(time.Hour))
	list = fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag entry after revival, got %+v", list)
	}
	if list[0].Cleared {
		t.Error("expected a still-stale rule to revive the cleared flag rather than leave it cleared")
	}
}

func TestShippedStaleRuleDisabledIsInert(t *testing.T) {
	d, ru, fs := newShippedStaleRuleDefinition(t, 30*24*time.Hour)
	d.def.Enabled = false
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Tick(now)
	if got := fs.List(); len(got) != 0 {
		t.Fatalf("expected a disabled definition to never fire, got %+v", got)
	}
}

// TestShippedStaleRuleTickIntervalComesFromItsParam pins the cadence as
// operator-configurable, which it always was (main.go read
// config.Flags.StaleRuleCheckInterval into its own ticker) -- Ticked
// makes the definition declare it, so the declaration has to read the
// param rather than a constant.
func TestShippedStaleRuleTickIntervalComesFromItsParam(t *testing.T) {
	def := Definition{
		ID:      "stale_rule",
		Name:    "Stale rule",
		Intent:  IntentDetection,
		Kind:    KindProgrammatic,
		Enabled: true,
		Params: Params{
			"maxAge":        (30 * 24 * time.Hour).String(),
			"checkInterval": (6 * time.Hour).String(),
		},
		ParamSchema: StaleRuleParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	built, err := BuildShippedProgrammaticDefinition(def, ShippedDeps{})
	if err != nil {
		t.Fatalf("BuildShippedProgrammaticDefinition(stale_rule): %v", err)
	}
	if got := built.(*staleRuleDefinition).TickInterval(); got != 6*time.Hour {
		t.Errorf("TickInterval = %s, want 6h0m0s (from the checkInterval param)", got)
	}
}

// TestShippedStaleRuleIsNonReplayable pins the declaration and the shape
// of its reason: absence of events, judged against long-lived usage
// records an event corpus does not contain.
func TestShippedStaleRuleIsNonReplayable(t *testing.T) {
	d, _, _ := newShippedStaleRuleDefinition(t, 30*24*time.Hour)

	receiptCapable, reason, ok := Replayability(d)
	if !ok {
		t.Fatal("Replayability could not classify stale_rule")
	}
	if receiptCapable {
		t.Fatal("expected stale_rule to declare itself non-replayable")
	}
	if !strings.Contains(reason, "absence") {
		t.Errorf("reason = %q, want it to name the absence-of-events shape it declines on", reason)
	}
}
