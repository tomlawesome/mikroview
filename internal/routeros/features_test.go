// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import "testing"

// Each flag flips at exactly its threshold and not a patch release
// before, and a router below the floor gets nothing at all (#1360).
func TestFeaturesFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reported string
		want     Features
		wantOK   bool
	}{
		// Below the floor: refused, no fallback (owner, answer 18).
		{"older than the floor", "7.12.1", Features{}, false},
		{"last patch before the floor", "7.17.2", Features{}, false},
		{"much older", "6.49.10", Features{}, false},
		// Unreadable is refused too, never guessed.
		{"empty", "", Features{}, false},
		{"not a version", "stable", Features{}, false},

		{"the floor", "7.18", Features{}, true},
		// The exact string a real router sends on a push.
		{"the floor as a router reports it", "7.18.2 (stable)", Features{}, true},
		{"the drawn older router", "7.19.4", Features{}, true},
		{"last line before :continue and the redirect default", "7.21.3", Features{}, true},

		{":continue and the redirect default arrive", "7.22", Features{LoopContinue: true, FetchFollowsRedirects: true}, true},
		{"7.22 as a beta is still 7.22", "7.22beta1", Features{LoopContinue: true, FetchFollowsRedirects: true}, true},
		{"last line before scheduler days", "7.23.3 (stable)", Features{LoopContinue: true, FetchFollowsRedirects: true}, true},

		{"scheduler days arrive", "7.24", Features{LoopContinue: true, FetchFollowsRedirects: true, SchedulerDays: true}, true},
		{"the drawn newer router", "7.24.4", Features{LoopContinue: true, FetchFollowsRedirects: true, SchedulerDays: true}, true},
		{"far ahead of review", "9.99.99", Features{LoopContinue: true, FetchFollowsRedirects: true, SchedulerDays: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FeaturesFor(tc.reported)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("FeaturesFor(%q) = %+v, %v; want %+v, %v", tc.reported, got, ok, tc.want, tc.wantOK)
			}
			if AtFloor(tc.reported) != tc.wantOK {
				t.Errorf("AtFloor(%q) = %v, want %v -- it must agree with FeaturesFor's ok", tc.reported, !tc.wantOK, tc.wantOK)
			}
		})
	}
}

// atLeast reads the thresholds with the same parser as a reported
// version, and a threshold that failed to parse would read as "not
// reached" for every router -- silently dropping a feature everywhere.
func TestFeatureThresholdsParse(t *testing.T) {
	min, ok := parseVersion(MinimumVersion)
	if !ok {
		t.Fatalf("MinimumVersion %q does not parse", MinimumVersion)
	}
	for _, v := range []string{loopContinueVersion, fetchFollowsRedirectsVersion, schedulerDaysVersion} {
		got, ok := parseVersion(v)
		if !ok {
			t.Errorf("threshold %q does not parse", v)
			continue
		}
		// A threshold at or below the floor would be a flag every
		// supported router has, which is a constant, not a feature.
		if compareVersions(got, min) <= 0 {
			t.Errorf("threshold %q is not above MinimumVersion %q", v, MinimumVersion)
		}
	}
}
