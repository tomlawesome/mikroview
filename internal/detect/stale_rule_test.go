// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/rules"
)

func newTestStaleRule(t *testing.T, maxAge time.Duration) (*StaleRuleDetector, *rules.Store, *flags.Store) {
	t.Helper()
	ru, err := rules.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return NewStaleRuleDetector(ru, fs, maxAge), ru, fs
}

func TestStaleRuleIgnoresFreshRules(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-time.Hour))
	d.Check(now)

	if len(fs.List()) != 0 {
		t.Fatalf("expected a recently-fired rule to not be flagged, got %+v", fs.List())
	}
}

func TestStaleRuleFlagsRuleIdleBeyondThreshold(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Check(now)

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
}

func TestStaleRuleThresholdIsExclusiveAtBoundary(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	// Exactly at the boundary -- not yet older than maxAge, so not stale.
	ru.Touch("r1", now.Add(-30*24*time.Hour))
	d.Check(now)
	if len(fs.List()) != 0 {
		t.Fatalf("expected a rule exactly at the threshold to not be flagged yet, got %+v", fs.List())
	}

	// One second past the boundary -- now stale.
	d.Check(now.Add(30*24*time.Hour + time.Second))
	if len(fs.List()) != 1 {
		t.Fatalf("expected the rule to be flagged once past the threshold, got %+v", fs.List())
	}
}

func TestStaleRuleOnlyFlagsRulesPastThreshold(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("fresh", now.Add(-time.Hour))
	ru.Touch("stale", now.Add(-45*24*time.Hour))
	d.Check(now)

	list := fs.List()
	if len(list) != 1 || list[0].Target != "stale" {
		t.Fatalf("expected only the stale rule to be flagged, got %+v", list)
	}
}

// TestStaleRuleRepeatedChecksDedupeRatherThanDuplicate confirms repeated
// sweeps while a rule stays stale update the same flag episode in place
// (via flags' dedup-by-(Type,Target)) instead of raising a fresh one
// every sweep.
func TestStaleRuleRepeatedChecksDedupeRatherThanDuplicate(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Check(now)
	d.Check(now.Add(time.Hour))
	d.Check(now.Add(2 * time.Hour))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected repeated sweeps to update one flag episode, got %d: %+v", len(list), list)
	}
	if list[0].Count != 3 {
		t.Errorf("expected Count to track 3 re-fires, got %d", list[0].Count)
	}
}

func TestStaleRuleClearedFlagIsRevivedIfStillStale(t *testing.T) {
	d, ru, fs := newTestStaleRule(t, 30*24*time.Hour)
	now := time.Now()

	ru.Touch("r1", now.Add(-40*24*time.Hour))
	d.Check(now)
	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("setup: expected one flag, got %+v", list)
	}
	if !fs.Clear(list[0].ID, now) {
		t.Fatal("setup: expected Clear to succeed on the active flag")
	}

	d.Check(now.Add(time.Hour))
	list = fs.List()
	if len(list) != 1 {
		t.Fatalf("expected exactly one flag entry after revival, got %+v", list)
	}
	if list[0].Cleared {
		t.Error("expected a still-stale rule to revive the cleared flag rather than leave it cleared")
	}
}
