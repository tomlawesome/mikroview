// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"strings"
	"testing"
)

func TestUpgradesFor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		wantIDs []string
		wantNil bool
	}{
		{"before From", "7.24.2", nil, false},
		{"at From", "7.24.3", []string{"cert-store-7.24.3"}, false},
		{"at From with a channel suffix", "7.24.3 (stable)", []string{"cert-store-7.24.3"}, false},
		{"a later patch", "7.24.4", []string{"cert-store-7.24.3"}, false},
		{"a later minor", "7.25", []string{"cert-store-7.24.3"}, false},
		{"a later minor with a pre-release suffix", "7.25beta1", []string{"cert-store-7.24.3"}, false},
		{"well before From", "7.18", nil, false},
		{"unparseable", "not a version", nil, true},
		{"empty", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := UpgradesFor(tc.version)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("UpgradesFor(%q) = %+v, want nil", tc.version, got)
				}
				return
			}
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("UpgradesFor(%q) = %+v, want IDs %v", tc.version, got, tc.wantIDs)
			}
			for i, id := range tc.wantIDs {
				if got[i].ID != id {
					t.Errorf("UpgradesFor(%q)[%d].ID = %q, want %q", tc.version, i, got[i].ID, id)
				}
			}
		})
	}
}

// TestUpgradesWellFormed pins the shape every catalogue entry must hold,
// so a new Upgrade cannot ship with a broken ID, an unparseable From, an
// unknown Steps key, or an empty operator-facing field.
func TestUpgradesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	validStep := map[string]bool{}
	for _, s := range UpgradeSteps {
		validStep[s] = true
	}
	for _, u := range Upgrades {
		if u.ID == "" {
			t.Errorf("Upgrade %+v has an empty ID", u)
		}
		if seen[u.ID] {
			t.Errorf("Upgrade ID %q is not unique", u.ID)
		}
		seen[u.ID] = true
		if _, ok := parseVersion(u.From); !ok {
			t.Errorf("Upgrade %q From %q does not parse as a version", u.ID, u.From)
		}
		if len(u.Steps) == 0 {
			t.Errorf("Upgrade %q has no Steps", u.ID)
		}
		for _, s := range u.Steps {
			if !validStep[s] {
				t.Errorf("Upgrade %q Steps has %q, want one of %v", u.ID, s, UpgradeSteps)
			}
		}
		if u.Heading == "" {
			t.Errorf("Upgrade %q has an empty Heading", u.ID)
		}
		if !strings.HasSuffix(u.Heading, ".") {
			t.Errorf("Upgrade %q Heading %q does not end with a full stop", u.ID, u.Heading)
		}
		if u.Body == "" {
			t.Errorf("Upgrade %q has an empty Body", u.ID)
		}
	}
}
