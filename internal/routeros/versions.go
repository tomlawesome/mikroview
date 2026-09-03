// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"fmt"
	"strconv"
	"strings"
)

// Issue #436: mikroview presents RouterOS commands -- the wizard's
// logging setup, the rule-tagging batch, the push script -- and RouterOS
// command syntax drifts between releases. Nothing recorded which version
// those commands were written against except a comment, and nothing
// noticed when MikroTik shipped a release nobody had reviewed.
//
// This file answers "where does a reported version stand" by comparing
// it against dialects.go's table (Rows, RowFor, NewestVersion), which is
// the actual record of what has been reviewed and what it renders. Where
// the answer is *shown* is a separate question; this package only
// answers it.

const (
	// MinimumVersion is the oldest RouterOS the commands mikroview
	// presents are expected to work on. Stated in docs/routeros-setup.md
	// as the supported floor, and the lower bound of dialects.go's first
	// row.
	MinimumVersion = "7.18"

	// ReviewedVersion is the newest RouterOS release whose changes have
	// actually been read against the commands in this repository. Held
	// as its own constant, separate from computing it from dialects.Rows
	// every time, because scripts/routeros-freshness.sh's failure
	// message names this constant directly and something has to be the
	// stable thing a person reads. TestReviewedVersionMatchesNewest is
	// what stops it silently drifting from dialects.NewestVersion() --
	// bump both together, in the same change that adds or extends a row,
	// or that test fails.
	//
	// Reviewed, not exercised: 7.23.3 was verified against a real
	// router; 7.24 and 7.24.1 were read from release notes only. See
	// dialects.go's Rows for which, when, and what each review found --
	// in particular the 7.24.0 find-lookup bug recorded on that row's
	// Note.
	//
	// One review finding has nowhere in a per-row Note to live, because
	// it isn't about any one version range: 7.24 made the console
	// "produce runtime errors for bad command parameters", where earlier
	// releases tolerated them silently. Nothing mikroview emits relies
	// on a bad parameter being tolerated, but it raises the cost of any
	// command that turns out to have one.
	ReviewedVersion = "7.24.1"
)

// Standing is where a router's reported version sits relative to what
// mikroview's commands were written for.
type Standing int

const (
	// StandingUnknown means the version could not be read at all -- a
	// router that has never pushed, or a string this parser does not
	// recognise. Deliberately its own answer rather than folded into
	// "supported": mikroview does not know, and saying so is different
	// from saying it is fine.
	StandingUnknown Standing = iota
	// StandingBelowMinimum means older than MinimumVersion. The commands
	// may not exist in that syntax at all.
	StandingBelowMinimum
	// StandingReviewed means a row in dialects.Rows covers this version
	// -- someone has actually checked the commands against it (or the
	// release notes for it).
	StandingReviewed
	// StandingAheadOfReview means newer than every row's upper bound.
	// Not an error and usually harmless: it means nobody has read that
	// release against these commands, which is a statement about
	// mikroview rather than about the router.
	StandingAheadOfReview
)

// String renders Standing the way the API contract does (#436): the wire
// value both POST /api/setup/commands and GET /api/devices's
// routerosStanding use.
func (s Standing) String() string {
	switch s {
	case StandingBelowMinimum:
		return "below-minimum"
	case StandingReviewed:
		return "reviewed"
	case StandingAheadOfReview:
		return "ahead-of-review"
	default:
		return "unknown"
	}
}

// VersionStanding classifies the version a router reported, against
// dialects.go's table rather than a single marker.
//
// reported is what arrives on a push -- `/system/resource get version`,
// which yields strings like "7.23.3 (stable)" or "7.19beta2". Anything
// this cannot parse is StandingUnknown rather than a guess: a wrong
// confident answer here would put a warning in front of an operator
// about a router that is fine, which is worse than saying nothing.
func VersionStanding(reported string) Standing {
	got, ok := parseVersion(reported)
	if !ok {
		return StandingUnknown
	}
	min, ok := parseVersion(MinimumVersion)
	if !ok {
		return StandingUnknown
	}
	if compareVersions(got, min) < 0 {
		return StandingBelowMinimum
	}
	if _, ok := RowFor(reported); ok {
		return StandingReviewed
	}
	newest, ok := parseVersion(NewestVersion())
	if !ok {
		return StandingUnknown
	}
	if compareVersions(got, newest) > 0 {
		return StandingAheadOfReview
	}
	// At or above the floor, at or below the newest row's bound, but
	// covered by no row: a gap in the table itself, which today's
	// contiguous Rows never produce. Read the same as "nobody has
	// reviewed this release", because that is exactly what it means.
	return StandingAheadOfReview
}

// CompareToReviewed reports whether candidate is newer than
// ReviewedVersion, for the freshness check to call on whatever MikroTik
// currently publishes as stable. The error is for an unparseable
// candidate, which the caller must not read as "not newer".
func CompareToReviewed(candidate string) (newer bool, err error) {
	got, ok := parseVersion(candidate)
	if !ok {
		return false, fmt.Errorf("routeros: cannot read %q as a RouterOS version", candidate)
	}
	reviewed, ok := parseVersion(ReviewedVersion)
	if !ok {
		return false, fmt.Errorf("routeros: ReviewedVersion %q is not a readable version", ReviewedVersion)
	}
	return compareVersions(got, reviewed) > 0, nil
}

// parseVersion reads the leading numeric components of a RouterOS
// version string. "7.23.3 (stable)" and "7.19beta2" both parse, to
// {7,23,3} and {7,19}: the trailing channel or pre-release marker is
// dropped rather than ordered, because ordering it would be inventing a
// rule MikroTik has not stated and this comparison does not need one.
func parseVersion(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var out []int
	for _, part := range strings.Split(s, ".") {
		digits := part
		for i, r := range part {
			if r < '0' || r > '9' {
				digits = part[:i]
				break
			}
		}
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
		// A component with trailing non-digits ends the version --
		// "7.19beta2" is 7.19, and nothing after it is a number this
		// comparison should read.
		if digits != part {
			break
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// compareVersions orders two parsed versions, shorter-is-lower when one
// is a prefix of the other (7.23 is older than 7.23.1).
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}
