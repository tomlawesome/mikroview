// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// flushForTest waits for s's write-behind writer to persist whatever is
// currently dirty -- issue #400 moved persistence off the caller's
// goroutine, so a test reopening the same path immediately after a Set
// now needs an explicit synchronous checkpoint. See
// flags.flushForTest, the twin of this helper.
func flushForTest(t *testing.T, s *SettingsStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

func TestOpenSettingsStoreEmptyPathUsesSeedVerbatim(t *testing.T) {
	seed := DefaultSettingsMap()
	seed[DetectorRuleSpike] = Settings{Enabled: false}

	s, err := OpenSettingsStore("", seed)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get(DetectorRuleSpike).Enabled {
		t.Error("expected the seed's disabled rule_spike to carry through with an empty path")
	}
	if !s.Get(DetectorPortScan).Enabled {
		t.Error("expected the seed's default-enabled port_scan to carry through")
	}
}

func TestOpenSettingsStorePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detector-settings.json")

	s, err := OpenSettingsStore(path, DefaultSettingsMap())
	if err != nil {
		t.Fatal(err)
	}
	s.Set(DetectorCriticalPort, Settings{
		Enabled: true,
		Scope:   Scope{Ports: []int{22, 3389}, PortsMode: ListModeAllow},
	})
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s)

	reopened, err := OpenSettingsStore(path, DefaultSettingsMap())
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Get(DetectorCriticalPort)
	if len(got.Scope.Ports) != 2 || got.Scope.PortsMode != ListModeAllow {
		t.Fatalf("expected the persisted scope to survive a reload, got %+v", got)
	}
}

// TestOpenSettingsStoreMalformedFileFailsClosed pins issue #378's
// policy: a document that exists but cannot be parsed is refused
// outright, not silently replaced by the seed. See
// flags.TestOpenMalformedFileFailsClosed for the full reasoning -- same
// fix, same shape, applied through the same shared persist.Open helper.
func TestOpenSettingsStoreMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detector-settings.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := OpenSettingsStore(path, DefaultSettingsMap())
	if err == nil {
		t.Fatal("expected a non-nil error for a malformed file, want fail-closed")
	}
	if s != nil {
		t.Error("expected a nil store on a load failure -- a non-nil store here would still carry a live backend")
	}
}

func TestOpenSettingsStoreFillsMissingDetectorFromSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detector-settings.json")

	// Simulate a settings file written by an older mikroview version that
	// only knew about 3 of the 9 detectors.
	partial, err := OpenSettingsStore(path, map[DetectorName]Settings{
		DetectorPortScan:      {Enabled: false},
		DetectorActivitySpike: {Enabled: false},
		DetectorCriticalPort:  {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	partial.Set(DetectorPortScan, Settings{Enabled: false}) // force a persist
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, partial)

	reopened, err := OpenSettingsStore(path, DefaultSettingsMap())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Get(DetectorPortScan).Enabled {
		t.Error("expected the on-disk entry to override the seed")
	}
	if !reopened.Get(DetectorRuleSpike).Enabled {
		t.Error("expected a detector absent from disk to be filled in from the seed")
	}
}

func TestSetReplacesWholesaleAndPersists(t *testing.T) {
	s := AllEnabledSettingsStore()
	s.Set(DetectorPortScan, Settings{Enabled: false})
	if s.Get(DetectorPortScan).Enabled {
		t.Error("expected Set to replace the entry")
	}
	// Unrelated detectors untouched.
	if !s.Get(DetectorActivitySpike).Enabled {
		t.Error("expected an unrelated detector to be unaffected by Set")
	}
}

func TestScopeMatchesHostAllowDenyEmptyAndCIDR(t *testing.T) {
	cases := []struct {
		name string
		sc   Scope
		ip   string
		want bool
	}{
		{"empty list always matches", Scope{}, "203.0.113.9", true},
		{"allow list, hit", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeAllow}, "203.0.113.9", true},
		{"allow list, miss", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeAllow}, "203.0.113.10", false},
		{"deny list, hit excluded", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny}, "203.0.113.9", false},
		{"deny list, miss admitted", Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny}, "203.0.113.10", true},
		{"CIDR allow, inside", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeAllow}, "203.0.113.200", true},
		{"CIDR allow, outside", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeAllow}, "198.51.100.1", false},
		{"CIDR deny, inside excluded", Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeDeny}, "203.0.113.200", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopeMatchesHost(tc.sc, tc.ip); got != tc.want {
				t.Errorf("scopeMatchesHost(%+v, %q) = %v, want %v", tc.sc, tc.ip, got, tc.want)
			}
		})
	}
}

func TestScopeMatchesPortAllowDeny(t *testing.T) {
	sc := Scope{Ports: []int{22, 3389}, PortsMode: ListModeAllow}
	if !scopeMatchesPort(sc, 22) {
		t.Error("expected 22 to match the allow list")
	}
	if scopeMatchesPort(sc, 80) {
		t.Error("expected 80 to not match the allow list")
	}

	deny := Scope{Ports: []int{22}, PortsMode: ListModeDeny}
	if scopeMatchesPort(deny, 22) {
		t.Error("expected 22 to be excluded by the deny list")
	}
	if !scopeMatchesPort(deny, 80) {
		t.Error("expected 80 to be admitted by the deny list")
	}
}

func TestScopeMatchesRuleAllowDeny(t *testing.T) {
	sc := Scope{Rules: []string{"r13"}, RulesMode: ListModeDeny}
	if scopeMatchesRule(sc, "r13") {
		t.Error("expected r13 to be excluded")
	}
	if !scopeMatchesRule(sc, "r14") {
		t.Error("expected r14 to be admitted")
	}
}

func TestScopeMatchesClassificationAnyInternalExternal(t *testing.T) {
	cases := []struct {
		scope store.Scope
		ip    string
		want  bool
	}{
		{store.ScopeAny, "192.168.1.10", true},
		{store.ScopeAny, "203.0.113.9", true},
		{store.ScopeInternal, "192.168.1.10", true},
		{store.ScopeInternal, "203.0.113.9", false},
		{store.ScopeExternal, "203.0.113.9", true},
		{store.ScopeExternal, "192.168.1.10", false},
		{store.ScopeInternal, "not-an-ip", false},
	}
	for _, tc := range cases {
		sc := Scope{Classification: tc.scope}
		if got := scopeMatchesHost(sc, tc.ip); got != tc.want {
			t.Errorf("classification %q, ip %q: got %v, want %v", tc.scope, tc.ip, got, tc.want)
		}
	}
}

func TestIsValidDetectorName(t *testing.T) {
	if !IsValidDetectorName(DetectorPortScan) {
		t.Error("expected port_scan to be valid")
	}
	if IsValidDetectorName("not_a_real_detector") {
		t.Error("expected an unknown name to be invalid")
	}
}

func TestValidateScopeRejectsBadModeAndClassification(t *testing.T) {
	if err := ValidateScope(Scope{}); err != nil {
		t.Errorf("expected the zero-value scope to be valid, got %v", err)
	}
	if err := ValidateScope(Scope{HostsMode: "maybe"}); err == nil {
		t.Error("expected an invalid hostsMode to be rejected")
	}
	if err := ValidateScope(Scope{PortsMode: "maybe"}); err == nil {
		t.Error("expected an invalid portsMode to be rejected")
	}
	if err := ValidateScope(Scope{RulesMode: "maybe"}); err == nil {
		t.Error("expected an invalid rulesMode to be rejected")
	}
	if err := ValidateScope(Scope{Classification: "maybe"}); err == nil {
		t.Error("expected an invalid classification to be rejected")
	}
}
