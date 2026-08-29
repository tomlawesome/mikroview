// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import "testing"

func TestVersionStanding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reported string
		want     Standing
	}{
		// The exact string a real router sends -- live-routeros-real
		// reads it off CHR 7.23.3.
		{"the version the commands were verified against", "7.23.3 (stable)", StandingReviewed},
		{"the stated floor", "7.18", StandingReviewed},
		{"between floor and reviewed", "7.20.1", StandingReviewed},
		{"older than the floor", "7.12.1", StandingBelowMinimum},
		{"much older", "6.49.10", StandingBelowMinimum},
		// Newer is not an error: it means nobody has read that release
		// against these commands.
		{"newer than reviewed", "7.24.1", StandingAheadOfReview},
		{"newer major", "8.0", StandingAheadOfReview},
		// A pre-release drops its marker rather than being ordered
		// against one -- 7.19beta2 is treated as 7.19.
		{"beta inside the range", "7.19beta2", StandingReviewed},
		// Unknown is its own answer, never a guess.
		{"empty", "", StandingUnknown},
		{"not a version", "stable", StandingUnknown},
		{"nothing numeric at all", "(stable)", StandingUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := VersionStanding(tc.reported); got != tc.want {
				t.Errorf("VersionStanding(%q) = %v, want %v", tc.reported, got, tc.want)
			}
		})
	}
}

// The freshness check calls this against whatever MikroTik publishes as
// current stable, so its answer decides whether CI shouts.
func TestCompareToReviewed(t *testing.T) {
	for _, tc := range []struct {
		candidate string
		wantNewer bool
	}{
		{ReviewedVersion, false},
		{"7.24.1", true},
		{"7.23.4", true},
		{"7.23.3", false},
		{"7.23.2", false},
		{"7.23", false},
		{"8.0", true},
	} {
		got, err := CompareToReviewed(tc.candidate)
		if err != nil {
			t.Errorf("CompareToReviewed(%q): unexpected error %v", tc.candidate, err)
			continue
		}
		if got != tc.wantNewer {
			t.Errorf("CompareToReviewed(%q) = %v, want %v", tc.candidate, got, tc.wantNewer)
		}
	}

	// An unreadable candidate must be an error, never a quiet "not
	// newer" -- that would turn a broken fetch into a passing check,
	// which is the failure this whole issue is about.
	if _, err := CompareToReviewed("not a version"); err == nil {
		t.Error("CompareToReviewed accepted an unreadable version, so a broken fetch would report everything is current")
	}
}

// The two markers must themselves be readable and in the right order,
// or every answer above is derived from nonsense.
func TestVersionMarkersAreSane(t *testing.T) {
	min, ok := parseVersion(MinimumVersion)
	if !ok {
		t.Fatalf("MinimumVersion %q does not parse", MinimumVersion)
	}
	reviewed, ok := parseVersion(ReviewedVersion)
	if !ok {
		t.Fatalf("ReviewedVersion %q does not parse", ReviewedVersion)
	}
	if compareVersions(min, reviewed) > 0 {
		t.Errorf("MinimumVersion %q is newer than ReviewedVersion %q", MinimumVersion, ReviewedVersion)
	}
}
