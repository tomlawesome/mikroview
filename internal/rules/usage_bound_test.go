// SPDX-License-Identifier: AGPL-3.0-only

package rules

import (
	"fmt"
	"testing"
	"time"
)

// TestRuleUsageIsBounded: byRule is keyed on the rule label parsed out
// of an unauthenticated syslog line, so the key space is
// attacker-chosen. Proven before the fix: 60,000 distinct labels were
// all retained with no eviction.
func TestRuleUsageIsBounded(t *testing.T) {
	prev := maxRuleEntries
	maxRuleEntries = 50
	t.Cleanup(func() { maxRuleEntries = prev })

	s, _ := Open("")
	now := time.Now()
	for i := 0; i < 5000; i++ {
		s.Touch(fmt.Sprintf("junk-%d", i), now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(s.List()); got > maxRuleEntries {
		t.Errorf("store holds %d rules, want <= %d", got, maxRuleEntries)
	}
}

// The cap was enforced by evicting back to exactly it, which leaves the
// store full -- so the *next* new label overflows too and pays for
// another full sort, and so does every one after that. Touch runs
// synchronously on the ingest goroutine for essentially every event
// carrying a rule label, and the label comes off unauthenticated syslog,
// so that is a state an attacker can hold the store in indefinitely:
// measured at 724 ns per Touch on an empty store against 7,455 ns at the
// cap. Shedding a batch amortises the sort. See #285.
func TestRuleUsageShedLeavesHeadroom(t *testing.T) {
	prev := maxRuleEntries
	maxRuleEntries = 800
	t.Cleanup(func() { maxRuleEntries = prev })

	s, _ := Open("")
	now := time.Now()
	for i := 0; i <= maxRuleEntries; i++ { // one past the cap, forcing a shed
		s.Touch(fmt.Sprintf("junk-%d", i), now.Add(time.Duration(i)*time.Millisecond))
	}

	after := len(s.List())
	if after >= maxRuleEntries {
		t.Fatalf("the shed left the store at %d against a cap of %d -- no headroom, so the next new label sheds again",
			after, maxRuleEntries)
	}

	// Filling the headroom must not trigger another shed.
	for i := 0; i < maxRuleEntries-after; i++ {
		s.Touch(fmt.Sprintf("fresh-%d", i), now.Add(time.Hour))
	}
	if got := len(s.List()); got != maxRuleEntries {
		t.Errorf("filling the headroom gave %d entries, want %d -- a shed ran that should not have", got, maxRuleEntries)
	}
}

// TestRuleUsageEvictionKeepsActiveRules: under a flood the genuine
// rules are the ones still firing, so oldest-LastSeen-first eviction
// must keep them and shed the junk.
func TestRuleUsageEvictionKeepsActiveRules(t *testing.T) {
	prev := maxRuleEntries
	maxRuleEntries = 20
	t.Cleanup(func() { maxRuleEntries = prev })

	s, _ := Open("")
	base := time.Now()

	for i := 0; i < 2000; i++ {
		s.Touch(fmt.Sprintf("junk-%d", i), base.Add(time.Duration(i)*time.Millisecond))
	}
	// A real rule, touched most recently of all.
	s.Touch("wan-in-drop", base.Add(time.Hour))

	var found bool
	for _, u := range s.List() {
		if u.Rule == "wan-in-drop" {
			found = true
		}
	}
	if !found {
		t.Error("the most-recently-active rule was evicted; eviction must shed the stalest entries, not the live ones")
	}
}
