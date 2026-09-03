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
		// against these commands. Deliberately far ahead of any real
		// release, so bumping ReviewedVersion after a review does not
		// turn this case into a lie -- an earlier version of this test
		// pinned the then-current 7.24.1 here and broke the moment that
		// release was reviewed, which is the wrong thing to have to fix.
		{"newer than reviewed", "9.99.99", StandingAheadOfReview},
		{"newer major", "99.0", StandingAheadOfReview},
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
	// Expressed against ReviewedVersion rather than against whatever it
	// happens to be today, so reviewing a release and bumping the marker
	// does not require editing these expectations.
	for _, tc := range []struct {
		candidate string
		wantNewer bool
	}{
		{ReviewedVersion, false},
		{"9.99.99", true},
		{"99.0", true},
		// Older than any marker this project will hold.
		{"7.0", false},
		{"6.49.10", false},
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

// ReviewedVersion is a constant so scripts/routeros-freshness.sh's
// -print-reviewed can name it without running the comparison logic, but
// dialects.NewestVersion() is the table's own answer to the same
// question. This is what stops the two silently drifting apart: add or
// extend a row without also bumping ReviewedVersion (or the reverse) and
// this test fails.
func TestReviewedVersionMatchesNewest(t *testing.T) {
	if ReviewedVersion != NewestVersion() {
		t.Errorf("ReviewedVersion is %q but NewestVersion() (dialects.Rows' newest upper bound) is %q -- "+
			"bump ReviewedVersion in the same change that adds or extends a row in dialects.go",
			ReviewedVersion, NewestVersion())
	}
}

// The API contract (#436) fixes routerosStanding's wire values as
// kebab-case; this pins Standing.String() to it so a future edit can't
// quietly revert to the old camelCase names nothing here would otherwise
// catch.
func TestStandingStringIsKebabCase(t *testing.T) {
	for standing, want := range map[Standing]string{
		StandingUnknown:       "unknown",
		StandingBelowMinimum:  "below-minimum",
		StandingReviewed:      "reviewed",
		StandingAheadOfReview: "ahead-of-review",
	} {
		if got := standing.String(); got != want {
			t.Errorf("Standing(%d).String() = %q, want %q", standing, got, want)
		}
	}
}
