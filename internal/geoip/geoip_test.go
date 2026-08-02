package geoip

import (
	"net"
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
		ip := net.ParseIP(c.ip)
		if got := isPublic(ip); got != c.want {
			t.Errorf("isPublic(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}
