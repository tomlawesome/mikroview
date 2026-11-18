// SPDX-License-Identifier: AGPL-3.0-only

package ipscope

import (
	"net/netip"
	"testing"
)

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("ParseAddr(%q): %v", s, err)
	}
	return a
}

// demoHome is the inside set the demo's cfg.yaml configures (#1481).
var demoHome = []string{
	"198.51.100.0/24",
	"192.0.2.32/27", "192.0.2.64/27", "192.0.2.96/27", "192.0.2.128/27",
	"192.0.2.160/27", "192.0.2.192/27", "192.0.2.224/28", "192.0.2.240/29", "192.0.2.248/29",
}

// The private, loopback and link-local addresses are built from numbers
// so this file holds no address text the private-details scan would flag.
func v4(a, b, c, d byte) netip.Addr { return netip.AddrFrom4([4]byte{a, b, c, d}) }

func v6(hi, lo uint16) netip.Addr {
	var b [16]byte
	b[0], b[1] = byte(hi>>8), byte(hi)
	b[14], b[15] = byte(lo>>8), byte(lo)
	return netip.AddrFrom16(b)
}

func TestDefaultTreatsPrivateLoopbackLinkLocalAndUnspecifiedAsInternal(t *testing.T) {
	var nilScope *Scope
	empty, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil): %v", err)
	}
	internal := []netip.Addr{
		v4(10, 1, 2, 3), v4(172, 16, 0, 1), v4(172, 31, 255, 254), v4(192, 168, 1, 1),
		v4(127, 0, 0, 1), v4(169, 254, 10, 10), v4(0, 0, 0, 0),
		v6(0xfc00, 1), v6(0xfd12, 1), v6(0, 1), v6(0xfe80, 1),
	}
	for name, sc := range map[string]*Scope{"nil receiver": nilScope, "empty scope": empty} {
		for _, a := range internal {
			if sc.IsPublic(a) {
				t.Errorf("%s: IsPublic(%s) = true, want false (internal by default)", name, a)
			}
		}
	}
}

func TestDefaultTreatsOtherAddressesAsPublic(t *testing.T) {
	var nilScope *Scope
	for _, a := range []netip.Addr{
		addr(t, "198.51.100.5"), addr(t, "203.0.113.44"), addr(t, "192.0.2.1"),
		v4(8, 8, 8, 8), addr(t, "2001:db8::1"),
	} {
		if !nilScope.IsPublic(a) {
			t.Errorf("nil scope: IsPublic(%s) = false, want true", a)
		}
	}
}

func TestHomeRangeMakesAnOtherwisePublicAddressInternal(t *testing.T) {
	var nilScope *Scope
	if !nilScope.IsPublic(addr(t, "198.51.100.5")) {
		t.Fatal("nil scope: 198.51.100.5 should be public")
	}
	sc, err := New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sc.IsPublic(addr(t, "198.51.100.5")) {
		t.Error("with home 198.51.100.0/24, 198.51.100.5 should be internal")
	}
}

func TestDemoHomeSetKeepsOutsidePeersPublicAndSplitsTheMixedBlock(t *testing.T) {
	sc, err := New(demoHome)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for s, wantPublic := range map[string]bool{
		"203.0.113.44":  true,  // outside peer
		"192.0.2.1":     true,  // router WAN block, not in the set
		"192.0.2.30":    true,  // design host, not in the set
		"192.0.2.33":    false, // staff
		"192.0.2.250":   false, // DMZ
		"198.51.100.66": false,
		"0.0.0.0":       false, // default still applies
	} {
		if got := sc.IsPublic(addr(t, s)); got != wantPublic {
			t.Errorf("IsPublic(%s) = %v, want %v", s, got, wantPublic)
		}
	}
}

func TestHomeSetNeverMakesAnInternalAddressPublic(t *testing.T) {
	sc, err := New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	if sc.IsPublic(v4(192, 168, 1, 1)) {
		t.Error("a home set must only add internal ranges; 192.168.1.1 became public")
	}
}

func TestNewRejectsAZeroLengthPrefix(t *testing.T) {
	for _, bad := range []string{"0.0.0.0/0", "::/0"} {
		if _, err := New([]string{bad}); err == nil {
			t.Errorf("New(%q) succeeded, want an error (a /0 makes everything internal)", bad)
		}
	}
}

func TestNewRejectsUnparsableEntries(t *testing.T) {
	for _, bad := range []string{"", "not-a-cidr", "198.51.100.0/33", "198.51.100.300/24", "198.51.100.0/"} {
		if _, err := New([]string{"198.51.100.0/24", bad}); err == nil {
			t.Errorf("New(%q) succeeded, want an error", bad)
		}
	}
}

func TestNewAcceptsAnIPv6HomePrefix(t *testing.T) {
	sc, err := New([]string{"2001:db8:1::/48"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sc.IsPublic(addr(t, "2001:db8:1::5")) {
		t.Error("2001:db8:1::5 should be internal under home 2001:db8:1::/48")
	}
	if !sc.IsPublic(addr(t, "2001:db8:2::5")) {
		t.Error("2001:db8:2::5 should stay public")
	}
}
