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
