// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
)

func TestValidateRejectsUnparseableInput(t *testing.T) {
	for _, in := range []string{"", "not-an-ip", "203.0.113.0/999", "203.0.113.0/24/8"} {
		if _, err := Validate(in, nil); !errors.Is(err, ErrInvalidCIDR) {
			t.Errorf("Validate(%q) error = %v, want ErrInvalidCIDR", in, err)
		}
	}
}

func TestValidateAcceptsBareAddressAsSlash32(t *testing.T) {
	got, err := Validate("203.0.114.7", nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := netip.MustParsePrefix("203.0.114.7/32")
	if got != want {
		t.Errorf("Validate(bare address) = %v, want %v", got, want)
	}
}

func TestValidateCanonicalisesHostBits(t *testing.T) {
	// 203.0.113.0/24 is itself reserved documentation space (TEST-NET-3),
	// so this uses an address outside every reserved range to isolate
	// the thing being tested: masking, not the not-public check.
	got, err := Validate("203.0.114.7/24", nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := netip.MustParsePrefix("203.0.114.0/24")
	if got != want {
		t.Errorf("Validate(host bits set) = %v, want canonical %v", got, want)
	}
}

func TestValidateRejectsNonIPv4(t *testing.T) {
	for _, in := range []string{"2001:db8::/32", "2001:db8::1", "::ffff:203.0.114.7/120", "::ffff:203.0.114.7"} {
		if _, err := Validate(in, nil); !errors.Is(err, ErrNotIPv4) {
			t.Errorf("Validate(%q) error = %v, want ErrNotIPv4", in, err)
		}
	}
}

func TestValidateRejectsBroaderThanSlash24(t *testing.T) {
	for _, in := range []string{"203.0.114.0/23", "203.0.0.0/16", "203.0.114.0/0"} {
		if _, err := Validate(in, nil); !errors.Is(err, ErrTooBroad) {
			t.Errorf("Validate(%q) error = %v, want ErrTooBroad", in, err)
		}
	}
}

func TestValidateRejectsNonPublicSpace(t *testing.T) {
	cases := []string{
		"10.0.0.0/24",        // private (RFC 1918)
		"172.16.5.0/24",      // private (RFC 1918)
		"192.168.1.0/24",     // private (RFC 1918)
		"127.0.0.0/24",       // loopback
		"169.254.1.0/24",     // link-local unicast
		"224.0.0.0/24",       // link-local multicast
		"239.0.0.0/24",       // multicast
		"0.0.0.0/32",         // unspecified
		"0.0.0.0/24",         // "this network"
		"100.64.0.0/24",      // shared address space / CGNAT
		"192.0.0.0/24",       // IETF protocol assignments
		"192.0.2.0/24",       // TEST-NET-1
		"198.18.0.0/24",      // benchmarking
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"240.0.0.0/24",       // reserved
		"255.255.255.255/32", // limited broadcast
	}
	for _, in := range cases {
		if _, err := Validate(in, nil); !errors.Is(err, ErrNotPublic) {
			t.Errorf("Validate(%q) error = %v, want ErrNotPublic", in, err)
		}
	}
}

func TestValidateAcceptsOrdinaryPublicRange(t *testing.T) {
	if _, err := Validate("203.0.114.0/24", nil); err != nil {
		t.Errorf("Validate(ordinary public /24) = %v, want no error", err)
	}
}

func TestValidateRejectsRoutersOwnRange(t *testing.T) {
	// Build own from a real device.Registry with a redeemed enrolment,
	// the actual producer this check runs against in production since
	// issue #1281's audit (Add gets own from OwnRanges, which
	// device.Registry implements) -- not a hand-written fixture that
	// could drift from it. Before #1281 this built own from a
	// routerstate.Store's pushed /ip/address table instead; that
	// evidence source was removed because a router's own claim about its
	// address is not something to protect a range on.
	r := device.NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("router-1", "router-1", now); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token, _, err := r.MintEnrolment("router-1", "203.0.114.1", now)
	if err != nil {
		t.Fatalf("MintEnrolment: %v", err)
	}
	if !r.TryEnrol("203.0.114.1", []byte("mikroview-enrol "+token)) {
		t.Fatal("TryEnrol failed to redeem the freshly minted token")
	}

	own := r.OwnPrefixes()
	if len(own) != 1 {
		t.Fatalf("OwnPrefixes() = %v, want exactly the one enrolled address", own)
	}

	if _, err := Validate("203.0.114.0/24", own); !errors.Is(err, ErrRouterOwn) {
		t.Errorf("Validate(a /24 overlapping the router's own address) error = %v, want ErrRouterOwn", err)
	}
	// A disjoint public range must not be caught by the same check.
	if _, err := Validate("203.0.115.0/24", own); err != nil {
		t.Errorf("Validate(disjoint from router's own range) = %v, want no error", err)
	}
}
