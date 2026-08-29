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
// This file is the machine-readable half of that: one place saying what
// the command knowledge is good for, and the comparison that decides
// whether a given router falls outside it. Where the answer is *shown*
// is a separate question; this package only answers it.

const (
	// MinimumVersion is the oldest RouterOS the commands mikroview
	// presents are expected to work on. Stated in docs/routeros-setup.md
	// as the supported floor.
	MinimumVersion = "7.18"

	// ReviewedVersion is the newest RouterOS release whose changes have
	// actually been read against the commands in this repository.
	//
	// This is the marker the scheduled freshness check compares against
	// (scripts/routeros-freshness.sh). Bumping it is a claim that
	// someone reviewed that release's notes for command-set changes --
	// so bump it when that has been done, not when a release appears.
	//
	// 7.23.3 is where the current commands were verified: see
	// frontend/src/lib/setupsteps.ts's blockSpecs comment and
	// docs/routeros-setup.md, both written against a real 7.23.3 router.
	ReviewedVersion = "7.23.3"
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
	// StandingReviewed means at or below ReviewedVersion and at or above
	// MinimumVersion -- inside the range someone has actually checked.
	StandingReviewed
	// StandingAheadOfReview means newer than ReviewedVersion. Not an
	// error and usually harmless: it means nobody has read that release
	// against these commands, which is a statement about mikroview
	// rather than about the router.
	StandingAheadOfReview
)

func (s Standing) String() string {
	switch s {
	case StandingBelowMinimum:
		return "belowMinimum"
	case StandingReviewed:
		return "reviewed"
	case StandingAheadOfReview:
		return "aheadOfReview"
	default:
		return "unknown"
	}
}

// VersionStanding classifies the version a router reported.
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
	reviewed, ok := parseVersion(ReviewedVersion)
	if !ok {
		return StandingUnknown
	}
	if compareVersions(got, min) < 0 {
		return StandingBelowMinimum
	}
	if compareVersions(got, reviewed) > 0 {
		return StandingAheadOfReview
	}
	return StandingReviewed
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
