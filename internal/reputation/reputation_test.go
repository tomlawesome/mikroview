package reputation

import (
	"context"
	"errors"
	"testing"
)

// Covers the isPublic short-circuit only -- it rejects before any
// outbound HTTP call, so it's safe to run without internet access.
// Fetching real Shodan/AbuseIPDB data isn't covered here.
func TestLookupRejectsNonPublicIP(t *testing.T) {
	c := New("")
	cases := []string{"192.168.1.1", "10.0.0.5", "172.16.0.1", "127.0.0.1", "169.254.1.1", "0.0.0.0", "not-an-ip", ""}
	for _, ip := range cases {
		_, err := c.Lookup(context.Background(), ip)
		if !errors.Is(err, ErrNotPublic) {
			t.Errorf("Lookup(%q) err = %v, want ErrNotPublic", ip, err)
		}
	}
}
