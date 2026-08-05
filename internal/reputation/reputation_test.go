package reputation

import (
	"context"
	"errors"
	"testing"
)

// Covers the isPublic short-circuit only -- it rejects before any
// outbound HTTP call, so it's safe to run without internet access.
// Fetching real Shodan/AbuseIPDB data isn't covered here.
func TestRiskFloorIsTorTakesPriority(t *testing.T) {
	r := Result{IsTor: true, UsageType: "Data Center/Web Hosting/Transit"}
	floor, ok := r.RiskFloor()
	if !ok || floor != TorExitNodeFloor {
		t.Errorf("RiskFloor() = (%d, %v), want (%d, true) -- IsTor should win over UsageType", floor, ok, TorExitNodeFloor)
	}
}

func TestRiskFloorHostingUsageType(t *testing.T) {
	cases := []string{
		"Data Center/Web Hosting/Transit",
		"data center",
		"Web Hosting",
	}
	for _, usageType := range cases {
		floor, ok := Result{UsageType: usageType}.RiskFloor()
		if !ok || floor != HostingProviderFloor {
			t.Errorf("RiskFloor() for UsageType %q = (%d, %v), want (%d, true)", usageType, floor, ok, HostingProviderFloor)
		}
	}
}

func TestRiskFloorNoSignalReturnsNotOK(t *testing.T) {
	cases := []Result{
		{},
		{UsageType: "Fixed Line ISP"},
		{UsageType: "Mobile ISP"},
		{UsageType: "Commercial"},
	}
	for _, r := range cases {
		if _, ok := r.RiskFloor(); ok {
			t.Errorf("RiskFloor() for %+v = ok=true, want false", r)
		}
	}
}

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
