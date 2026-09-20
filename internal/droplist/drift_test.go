// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"net/netip"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

func mustEntry(t *testing.T, cidr string) Entry {
	t.Helper()
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", cidr, err)
	}
	return Entry{CIDR: p.Masked(), AddedBy: "admin", AddedAt: time.Now(), Reason: "test"}
}

// TestHeldCountsBareSlash32AndMaskedRanges pins the two shapes RouterOS
// actually renders in a pushed /ip/firewall/address-list snapshot: a
// bare address for a /32 ("203.0.113.5") and an explicit mask for
// anything broader ("203.0.113.0/24") -- both must be recognised as
// holding the matching drop-list entry.
func TestHeldCountsBareSlash32AndMaskedRanges(t *testing.T) {
	entries := []Entry{
		mustEntry(t, "203.0.113.5/32"),
		mustEntry(t, "203.0.114.0/24"),
	}
	snapshot := []ingest.AddressListEntry{
		{List: ListName, Address: "203.0.113.5"},
		{List: ListName, Address: "203.0.114.0/24"},
	}
	if got := Held(entries, snapshot); got != 2 {
		t.Errorf("Held = %d, want 2", got)
	}
}

// TestHeldIgnoresWrongListName covers a snapshot row pushed under some
// other address list -- e.g. a router's own unrelated address-list
// entries -- which must never count toward the drop list's own drift
// number.
func TestHeldIgnoresWrongListName(t *testing.T) {
	entries := []Entry{mustEntry(t, "203.0.113.5/32")}
	snapshot := []ingest.AddressListEntry{
		{List: "some-other-list", Address: "203.0.113.5"},
	}
	if got := Held(entries, snapshot); got != 0 {
		t.Errorf("Held = %d, want 0 for a snapshot entry under an unrelated list name", got)
	}
}

// TestHeldIgnoresUnrelatedAddress covers a snapshot row that is under
// the right list but names a range with no matching drop-list entry.
func TestHeldIgnoresUnrelatedAddress(t *testing.T) {
	entries := []Entry{mustEntry(t, "203.0.113.5/32")}
	snapshot := []ingest.AddressListEntry{
		{List: ListName, Address: "198.51.100.9"},
	}
	if got := Held(entries, snapshot); got != 0 {
		t.Errorf("Held = %d, want 0 for an address the drop list never listed", got)
	}
}
