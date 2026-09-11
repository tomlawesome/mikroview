// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"net/netip"
	"testing"
)

func TestOpenEmptyPathIsDisabled(t *testing.T) {
	l, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	if code, ok := l.Country("8.8.8.8"); ok || code != "" {
		t.Errorf("expected a disabled Lookup to report ok=false, got %q, %v", code, ok)
	}
	l.Close() // must not panic on a Lookup with no db open
}

func TestOpenMissingFileStillUsable(t *testing.T) {
	l, err := Open("/nonexistent/does-not-exist.mmdb")
	if err == nil {
		t.Fatal("expected an error for a missing database file")
	}
	if code, ok := l.Country("8.8.8.8"); ok || code != "" {
		t.Errorf("a failed Open should still return a usable disabled Lookup, got %q, %v", code, ok)
	}
}

func TestIsPublic(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"192.168.1.1", false},
		{"10.0.0.1", false},
		{"172.16.5.1", false},
		{"127.0.0.1", false},
		{"169.254.1.1", false},
		{"0.0.0.0", false},
	}
	for _, c := range cases {
		ip, err := netip.ParseAddr(c.ip)
		if err != nil {
			t.Fatalf("netip.ParseAddr(%s): %v", c.ip, err)
		}
		if got := isPublic(ip); got != c.want {
			t.Errorf("isPublic(%s) = %v, want %v", c.ip, got, c.want)
		}
	}

	// An IPv4-in-IPv6 address is the same host as its IPv4 form, so it
	// must classify the same way. netip keeps the two distinct where
	// net.IP did not, so Country unmaps before asking (#1081).
	for _, c := range []struct {
		ip   string
		want bool
	}{
		{"::ffff:8.8.8.8", true},
		{"::ffff:192.168.1.1", false},
		{"::ffff:127.0.0.1", false},
	} {
		ip, err := netip.ParseAddr(c.ip)
		if err != nil {
			t.Fatalf("netip.ParseAddr(%s): %v", c.ip, err)
		}
		if got := isPublic(ip.Unmap()); got != c.want {
			t.Errorf("isPublic(%s unmapped) = %v, want %v", c.ip, got, c.want)
		}
	}
}
