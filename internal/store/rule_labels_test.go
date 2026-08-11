// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// totalByRule is keyed on RouterOS log-prefixes, which arrive on
// unauthenticated syslog and are therefore chosen by whoever is sending.
// It had no cap at all, and nothing in this package ever evicted from it
// -- so a flood both grew the heap without bound (measured at +161.2 MB
// for 500,000 labels, from 57 MB of syslog) and permanently poisoned
// TopRules for the life of the process. internal/rules.Store capped the
// identical string at 20,000; the counter one statement away in main.go
// was missed. See #285.
func TestRuleLabelFloodIsBounded(t *testing.T) {
	orig := maxRuleLabels
	maxRuleLabels = 500
	defer func() { maxRuleLabels = orig }()

	s := New(100, time.Hour)
	for i := 0; i < 20_000; i++ {
		s.Insert(Event{RuleLabel: fmt.Sprintf("minted-%d", i), Action: ActionDrop})
	}

	if got := len(s.totalByRule); got > maxRuleLabels {
		t.Fatalf("totalByRule holds %d labels against a cap of %d", got, maxRuleLabels)
	}
}

// The cap is only useful if it sheds the noise rather than the signal. A
// real rule is one an operator configured on the router, so it fires
// repeatedly; minted labels are a long tail of ones.
func TestRuleLabelShedKeepsTheRulesThatActuallyFire(t *testing.T) {
	orig := maxRuleLabels
	maxRuleLabels = 500
	defer func() { maxRuleLabels = orig }()

	s := New(10_000, time.Hour)

	// A genuine rule, firing the way a real one does.
	for i := 0; i < 50; i++ {
		s.Insert(Event{RuleLabel: "wan-drop-invalid", Action: ActionDrop})
	}
	// A flood of one-shot labels, several times the cap.
	for i := 0; i < 5_000; i++ {
		s.Insert(Event{RuleLabel: fmt.Sprintf("minted-%d", i), Action: ActionDrop})
	}

	if _, kept := s.totalByRule["wan-drop-invalid"]; !kept {
		t.Fatal("the genuine, repeatedly-firing rule was shed in favour of one-shot minted labels")
	}
	top := s.Stats().TopRules
	if len(top) == 0 || top[0].Rule != "wan-drop-invalid" {
		t.Errorf("expected the genuine rule to lead TopRules, got %+v", top)
	}
}

// A shed must leave headroom, or the next new label sheds again and
// every subsequent event pays for a full sort -- the amortisation
// internal/evict exists for.
func TestRuleLabelShedLeavesHeadroom(t *testing.T) {
	orig := maxRuleLabels
	maxRuleLabels = 800
	defer func() { maxRuleLabels = orig }()

	s := New(10_000, time.Hour)
	for i := 0; i <= maxRuleLabels; i++ { // one past the cap, forcing a shed
		s.Insert(Event{RuleLabel: fmt.Sprintf("minted-%d", i), Action: ActionDrop})
	}

	if got := len(s.totalByRule); got >= maxRuleLabels {
		t.Fatalf("the shed left %d labels against a cap of %d -- no headroom", got, maxRuleLabels)
	}
}

// Raw is deliberately kept as the router sent it, which is why the
// parser excludes it from the 256-byte field clamp -- but it was not
// bounded at all beyond the listener's 64 KiB per-message limit, while
// the documented memory budget assumes an ordinary line. Both cannot be
// true: at 64 KiB lines the default 200,000 slots hold 12.55 GiB, a 107x
// overrun, from input nothing has to authenticate to send.
//
// 2 KiB is roughly five times the longest genuine RouterOS line, so this
// only ever fires on input a real router does not produce -- and when it
// does, the event says so rather than presenting a shortened line as
// verbatim. Owner decision on #285 finding 5.
func TestClampRawBoundsTheVerbatimLine(t *testing.T) {
	typical := "A|wan-in|forward: in:ether1 out:ether2, proto TCP, 192.0.2.1:1234->198.51.100.1:80, len 60"
	if got, truncated := ClampRaw(typical); truncated || got != typical {
		t.Errorf("a typical line was altered: truncated=%v, len=%d", truncated, len(got))
	}

	// The listener's own per-message ceiling -- the worst case that
	// actually reaches this.
	oversized := strings.Repeat("x", 64*1024)
	got, truncated := ClampRaw(oversized)
	if !truncated {
		t.Fatal("a 64 KiB line was not truncated -- the memory budget assumes ~624 bytes per event")
	}
	if len(got) != MaxRawBytes {
		t.Errorf("truncated length = %d, want %d", len(got), MaxRawBytes)
	}

	// Exactly at the cap is not truncation: the boundary must not
	// mark a line that fits.
	if _, truncated := ClampRaw(strings.Repeat("y", MaxRawBytes)); truncated {
		t.Error("a line exactly at the cap was reported as truncated")
	}
}

// The cap is only honest if the event carries the fact. A shortened line
// presented as verbatim would be worse than either alternative.
func TestOversizedRawIsMarkedOnTheStoredEvent(t *testing.T) {
	s := New(10, time.Hour)
	raw, truncated := ClampRaw(strings.Repeat("z", 64*1024))
	stored := s.Insert(Event{Action: ActionDrop, Raw: raw, RawTruncated: truncated})

	if !stored.RawTruncated {
		t.Error("RawTruncated is false on an event whose line was cut")
	}
	if len(stored.Raw) != MaxRawBytes {
		t.Errorf("stored Raw is %d bytes, want %d", len(stored.Raw), MaxRawBytes)
	}
}
