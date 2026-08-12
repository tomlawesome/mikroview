// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"testing"
	"time"
)

// realRouterMAC is the casing a real RouterOS 7.23.3 actually emits --
// captured from a booted CHR under scripts/live-routeros.sh, in both the
// firewall syslog line and the pushed ARP table (#273):
//
//	firewall,info A|live-in| input: in:ether1 out:(unknown 0),
//	connection-state:new src-mac 52:55:0A:00:02:02, proto TCP (SYN),
//	172.17.0.1:55134->10.0.2.15:15902, len 44
//
//	{"address":"10.0.2.2","mac-address":"52:55:0A:00:02:02", ...}
//
// typedMAC is the same address written the conventional way: lowercase,
// which is what the watchlist entry form takes as free text, and what
// every other example in this repository uses. The two must be the same
// device.
const (
	realRouterMAC = "52:55:0A:00:02:02"
	typedMAC      = "52:55:0a:00:02:02"
)

func TestIdentityMatchesAcrossMACCase(t *testing.T) {
	typed := Identity{MAC: typedMAC}
	fromRouter := Identity{MAC: realRouterMAC}

	if !typed.MatchesSource(fromRouter) {
		t.Errorf("an entry typed as %s did not match the same device reported as %s", typedMAC, realRouterMAC)
	}
	if !fromRouter.MatchesSource(typed) {
		t.Errorf("matching is not symmetric: %s did not match %s", realRouterMAC, typedMAC)
	}

	// Still a real comparison, not one that matches everything.
	if typed.MatchesSource(Identity{MAC: "52:55:0a:00:02:03"}) {
		t.Error("a different MAC matched")
	}
}

// A device whose MAC arrives in one case and is queried in the other has
// one history, not two. Both halves matter and they fail differently:
// collapsing keyed on the raw string splits one device's repeated
// traffic into separate records, and a query keyed on it returns nothing
// at all.
func TestQueryAndCollapsingIgnoreMACCase(t *testing.T) {
	s := mustOpen(t, 100)
	now := time.Now()

	tuple := Tuple{Source: Identity{MAC: realRouterMAC}, DestIP: "10.0.2.15", Port: 15902}
	if err := s.Append("e1", tuple, testEvent("first"), now); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// The same device, same destination, same port -- only the casing
	// differs, as it would between a syslog line and a pushed table.
	lower := Tuple{Source: Identity{MAC: typedMAC}, DestIP: "10.0.2.15", Port: 15902}
	if err := s.Append("e1", lower, testEvent("second"), now.Add(time.Second)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := collect(t, s, Query{Source: Identity{MAC: typedMAC}})
	if len(got) != 1 {
		t.Fatalf("querying by the typed casing returned %d records, want 1 (the two appends should have collapsed into one)", len(got))
	}
	if got[0].Count != 2 {
		t.Errorf("Count = %d, want 2 -- the second append did not collapse onto the first", got[0].Count)
	}

	if n := len(collect(t, s, Query{Source: Identity{MAC: realRouterMAC}})); n != 1 {
		t.Errorf("querying by the router's own casing returned %d records, want 1", n)
	}
}
