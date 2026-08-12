// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

func baseEvent() store.Event {
	return store.Event{
		Action:  store.ActionAccept,
		SrcIP:   "192.168.1.50",
		SrcMAC:  "aa:bb:cc:dd:ee:ff",
		DstIP:   "10.0.0.5",
		DstPort: 22,
	}
}

// --- Non-inverted -----------------------------------------------------

func TestMatchOnPort(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22, 2222}}

	tuple, outcome := Match(entry, baseEvent())
	if outcome != Violation {
		t.Fatalf("expected Violation on a listed port, got %v", outcome)
	}
	if tuple.Port != 22 || tuple.DestIP != "10.0.0.5" {
		t.Errorf("unexpected tuple: %+v", tuple)
	}

	e := baseEvent()
	e.DstPort = 443
	if _, outcome := Match(entry, e); outcome != NoMatch {
		t.Errorf("matched a port not in the entry's list: %v", outcome)
	}
}

func TestMatchRequiresTrackableConnState(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}}

	e := baseEvent()
	e.ConnState = "new"
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("connState=new should be trackable, got %v", outcome)
	}

	e.ConnState = ""
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("connState=\"\" (unset) should be trackable, got %v", outcome)
	}

	e.ConnState = "established"
	if _, outcome := Match(entry, e); outcome != NoMatch {
		t.Errorf("connState=established (return traffic on an accepted service) must not match, got %v", outcome)
	}
}

func TestMatchWithNoDstPortNeverMatches(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}}
	e := baseEvent()
	e.DstPort = 0
	if _, outcome := Match(entry, e); outcome != NoMatch {
		t.Errorf("an event with no destination port must never match, got %v", outcome)
	}
}

func TestMatchUnscopedEntryMatchesAnySource(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}} // no Source set

	e1 := baseEvent()
	e1.SrcMAC, e1.SrcIP = "11:11:11:11:11:11", "192.168.1.1"
	e2 := baseEvent()
	e2.SrcMAC, e2.SrcIP = "22:22:22:22:22:22", "192.168.1.2"

	t1, outcome1 := Match(entry, e1)
	t2, outcome2 := Match(entry, e2)
	if outcome1 != Violation || outcome2 != Violation {
		t.Fatalf("an unscoped entry should match any source, got %v / %v", outcome1, outcome2)
	}
	// The tuple must carry each event's OWN identity, not the entry's
	// (empty) Source -- an unscoped entry watching many devices produces
	// one matchlog record per device, not a shared one.
	if t1.Source.MAC != "11:11:11:11:11:11" || t2.Source.MAC != "22:22:22:22:22:22" {
		t.Errorf("tuples did not carry the matching events' own identities: %+v / %+v", t1, t2)
	}
}

func TestMatchScopedBySourceMAC(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}

	e := baseEvent() // SrcMAC = aa:bb:cc:dd:ee:ff
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("expected a match: event's MAC equals the entry's scoped MAC, got %v", outcome)
	}

	other := baseEvent()
	other.SrcMAC = "99:99:99:99:99:99"
	if _, outcome := Match(entry, other); outcome != NoMatch {
		t.Errorf("a different MAC must not match a MAC-scoped entry, got %v", outcome)
	}
}

// The whole point of MAC-preferred identity (#243 section 1): a
// MAC-scoped entry must keep matching a device even after its IP
// changes under DHCP.
func TestMatchScopedBySourceMACSurvivesIPChange(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}

	e := baseEvent()
	e.SrcIP = "192.168.1.200" // different IP, same MAC
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("a MAC-scoped entry must match regardless of the event's IP, got %v", outcome)
	}
}

func TestMatchScopedBySourceIPFallback(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, Source: matchlog.Identity{IP: "192.168.1.50"}}

	e := baseEvent()
	e.SrcMAC = "" // no MAC known for this chain -- IP fallback
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("expected a match via IP fallback when the entry has no MAC, got %v", outcome)
	}

	other := baseEvent()
	other.SrcMAC = ""
	other.SrcIP = "192.168.1.99"
	if _, outcome := Match(entry, other); outcome != NoMatch {
		t.Errorf("a different IP must not match an IP-scoped entry, got %v", outcome)
	}
}

func TestMatchEventWithNoIdentityNeverMatches(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}} // unscoped

	e := baseEvent()
	e.SrcMAC, e.SrcIP = "", "" // nothing to attribute this to
	if _, outcome := Match(entry, e); outcome != NoMatch {
		t.Errorf("an event with neither MAC nor IP must never match, even an unscoped entry, got %v", outcome)
	}
}

func TestMatchScopedByDestIP(t *testing.T) {
	entry := Entry{ID: "e1", Ports: []int{22}, DestIP: "10.0.0.5"}

	if _, outcome := Match(entry, baseEvent()); outcome != Violation {
		t.Errorf("expected a match: event's DstIP equals the entry's scoped DestIP, got %v", outcome)
	}

	other := baseEvent()
	other.DstIP = "10.0.0.6"
	if _, outcome := Match(entry, other); outcome != NoMatch {
		t.Errorf("a different destination must not match a dest-scoped entry, got %v", outcome)
	}
}

func TestMatchRequiresAllScopesTogether(t *testing.T) {
	entry := Entry{
		ID:     "e1",
		Ports:  []int{22},
		Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		DestIP: "10.0.0.5",
	}

	if _, outcome := Match(entry, baseEvent()); outcome != Violation {
		t.Fatalf("expected a match: source, dest and port all satisfy the entry, got %v", outcome)
	}

	wrongDest := baseEvent()
	wrongDest.DstIP = "10.0.0.9"
	if _, outcome := Match(entry, wrongDest); outcome != NoMatch {
		t.Errorf("right source/port but wrong dest must not match a dest-scoped entry, got %v", outcome)
	}

	wrongSource := baseEvent()
	wrongSource.SrcMAC = "99:99:99:99:99:99"
	if _, outcome := Match(entry, wrongSource); outcome != NoMatch {
		t.Errorf("right dest/port but wrong source must not match a source-scoped entry, got %v", outcome)
	}
}

// --- Inverted -----------------------------------------------------------

func invertedEntry() Entry {
	return Entry{ID: "e1", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}
}

func TestMatchInvertedRequiresSourceDevice(t *testing.T) {
	entry := invertedEntry()

	e := baseEvent() // SrcMAC matches
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Fatalf("expected the device's own traffic to be evaluated, got %v", outcome)
	}

	other := baseEvent()
	other.SrcMAC = "99:99:99:99:99:99"
	if _, outcome := Match(entry, other); outcome != NoMatch {
		t.Errorf("a different device's traffic must not be evaluated by this entry, got %v", outcome)
	}
}

func TestMatchInvertedIgnoresPorts(t *testing.T) {
	// Ports is a non-inverted-only field -- an inverted entry watches
	// every port its device touches, so a populated (but irrelevant)
	// Ports list must not filter anything out.
	entry := invertedEntry()
	entry.Ports = []int{22} // irrelevant to inverted matching

	e := baseEvent()
	e.DstPort = 443 // not in Ports, must still be evaluated
	if _, outcome := Match(entry, e); outcome != Violation {
		t.Errorf("Ports must be ignored for an inverted entry, got %v", outcome)
	}
}

func TestMatchInvertedRequiresTrackableConnState(t *testing.T) {
	entry := invertedEntry()

	e := baseEvent()
	e.ConnState = "established"
	if _, outcome := Match(entry, e); outcome != NoMatch {
		t.Errorf("an accepted service's own return traffic must not fire, got %v", outcome)
	}
}

func TestMatchInvertedPermittedDestinationNeverFires(t *testing.T) {
	entry := invertedEntry()
	entry.Permitted = []PermittedDest{{DestIP: "10.0.0.5", Port: 22}}

	if _, outcome := Match(entry, baseEvent()); outcome != NoMatch {
		t.Errorf("a permitted destination must never fire, got %v", outcome)
	}

	other := baseEvent()
	other.DstIP = "10.0.0.9" // not permitted
	if _, outcome := Match(entry, other); outcome == NoMatch {
		t.Error("an unpermitted destination must still be evaluated")
	}
}

// The core observe/promote split (#243 section 5): while Observing,
// nothing fires -- an unpermitted destination becomes an Observed
// candidate, not a Violation.
func TestMatchInvertedObservingRecordsRatherThanFires(t *testing.T) {
	entry := invertedEntry()
	entry.Observing = true

	tuple, outcome := Match(entry, baseEvent())
	if outcome != Observed {
		t.Fatalf("expected Observed while the entry is still observing, got %v", outcome)
	}
	if tuple.DestIP != "10.0.0.5" || tuple.Port != 22 {
		t.Errorf("unexpected tuple: %+v", tuple)
	}
}

func TestMatchInvertedNotObservingFires(t *testing.T) {
	entry := invertedEntry()
	entry.Observing = false // promoted out of observe mode

	_, outcome := Match(entry, baseEvent())
	if outcome != Violation {
		t.Fatalf("expected Violation once the entry has left observe mode, got %v", outcome)
	}
}

// A permitted destination must never fire OR get recorded as an
// observation, even while Observing -- it has already been decided.
func TestMatchInvertedPermittedWinsOverObserving(t *testing.T) {
	entry := invertedEntry()
	entry.Observing = true
	entry.Permitted = []PermittedDest{{DestIP: "10.0.0.5", Port: 22}}

	if _, outcome := Match(entry, baseEvent()); outcome != NoMatch {
		t.Errorf("a permitted destination must not be re-observed, got %v", outcome)
	}
}

func TestMatchInvertedStructuralNoiseExemptByDefault(t *testing.T) {
	entry := invertedEntry()
	entry.Observing = true

	cases := []string{
		"239.1.2.3",       // multicast
		"255.255.255.255", // limited broadcast
		"169.254.1.1",     // link-local
	}
	for _, ip := range cases {
		e := baseEvent()
		e.DstIP = ip
		if _, outcome := Match(entry, e); outcome != NoMatch {
			t.Errorf("structural noise to %s should be exempt by default, got %v", ip, outcome)
		}
	}

	// A genuine unicast destination must still be evaluated normally.
	e := baseEvent()
	e.DstIP = "8.8.8.8"
	if _, outcome := Match(entry, e); outcome != Observed {
		t.Errorf("an ordinary unicast destination must still be evaluated, got %v", outcome)
	}
}

func TestMatchInvertedStructuralNoiseOptIn(t *testing.T) {
	entry := invertedEntry()
	entry.Observing = true
	entry.IncludeStructuralNoise = true

	e := baseEvent()
	e.DstIP = "239.1.2.3" // multicast
	if _, outcome := Match(entry, e); outcome != Observed {
		t.Errorf("multicast should be evaluated once opted in, got %v", outcome)
	}
}

func TestIsStructurallyExempt(t *testing.T) {
	cases := []struct {
		ip     string
		exempt bool
	}{
		{"224.0.0.1", true},       // multicast
		{"239.255.255.250", true}, // multicast (SSDP)
		{"255.255.255.255", true}, // limited broadcast
		{"169.254.1.1", true},     // link-local
		{"ff02::1", true},         // IPv6 link-local multicast
		{"8.8.8.8", false},        // ordinary unicast
		{"10.0.0.5", false},       // ordinary unicast
		{"192.168.1.255", false},  // subnet broadcast -- not detectable without the mask, see doc comment
		{"not-an-ip", false},
		{"", false},
	}
	for _, tt := range cases {
		if got := isStructurallyExempt(tt.ip); got != tt.exempt {
			t.Errorf("isStructurallyExempt(%q) = %v, want %v", tt.ip, got, tt.exempt)
		}
	}
}

// Sanity check that Match's timestamp-free decision doesn't accidentally
// depend on wall-clock time anywhere in the inverted path.
func TestMatchInvertedIsDeterministic(t *testing.T) {
	entry := invertedEntry()
	e := baseEvent()
	_, first := Match(entry, e)
	time.Sleep(time.Millisecond)
	_, second := Match(entry, e)
	if first != second {
		t.Errorf("Match produced different outcomes for the same input: %v then %v", first, second)
	}
}

// A watchlist entry whose MAC an operator typed the conventional way --
// lowercase, as the entry form's free-text field takes it -- must match
// the same device as a real router reports it. RouterOS 7.23.3 emits
// src-mac uppercase (captured from a booted CHR, #273), so a byte-exact
// comparison meant such an entry silently never fired: no error, no
// empty state, just an entry that looked configured and did nothing.
// Both entry kinds route their source check through
// matchlog.Identity.MatchesSource, so both are covered here.
func TestMatchIgnoresMACCase(t *testing.T) {
	const (
		fromRouter = "52:55:0A:00:02:02" // as a real RouterOS emits it
		asTyped    = "52:55:0a:00:02:02" // as an operator writes it
	)

	e := baseEvent()
	e.SrcMAC = fromRouter
	e.DstPort = 15902

	nonInverted := Entry{ID: "e1", Ports: []int{15902}, Source: matchlog.Identity{MAC: asTyped}}
	if _, outcome := Match(nonInverted, e); outcome != Violation {
		t.Errorf("non-inverted entry typed as %s did not match traffic reported as %s: %v", asTyped, fromRouter, outcome)
	}

	inverted := Entry{ID: "e2", Invert: true, Source: matchlog.Identity{MAC: asTyped}, Observing: true}
	if _, outcome := Match(inverted, e); outcome != Observed {
		t.Errorf("inverted entry typed as %s did not observe traffic reported as %s: %v", asTyped, fromRouter, outcome)
	}

	// A genuinely different device is still a non-match.
	other := e
	other.SrcMAC = "52:55:0A:00:02:03"
	if _, outcome := Match(nonInverted, other); outcome != NoMatch {
		t.Errorf("matched a different MAC: %v", outcome)
	}
}
