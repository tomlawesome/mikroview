// SPDX-License-Identifier: AGPL-3.0-only

package routeros

// Issue #1360: the blocklist builder writes commands whose syntax depends
// on the router's own RouterOS version (owner, answer 15: offer what that
// version has). dialects.go's rows answer a different question -- "does
// this release render the same wizard commands" -- and stay one dialect;
// the builder keys on these thresholds instead (BUILD.md, "Version
// features, not dialect rows").
//
// Each threshold is the release whose CHANGELOG
// (https://download.mikrotik.com/routeros/<version>/CHANGELOG) first
// carries the feature; catalogue.md's "Sources" lists them. Each line
// quoted below was re-read from those changelogs on 2026-10-01.
const (
	// loopContinueVersion: 7.22 "added :continue and :break commands for
	// various loops". Before it a loader skips a bad line with an
	// `:onerror` flag.
	loopContinueVersion = "7.22"
	// fetchFollowsRedirectsVersion: 7.18 "added http-max-redirect-count
	// parameter, allows to follow redirects"; 7.22 "increased default
	// maximum redirect count to 2". Before 7.22 the loader sets it.
	fetchFollowsRedirectsVersion = "7.22"
	// schedulerDaysVersion: 7.24 "added days to scheduler".
	schedulerDaysVersion = "7.24"
	// fetchTrustsBuiltinRootsVersion: 7.19 "certificate - added built-in
	// root certificate authorities store". From it a loader fetches with
	// check-certificate=yes and needs nothing imported; before it the
	// router has no roots to check a list's server against, so the fetch
	// goes unchecked rather than failing every time.
	fetchTrustsBuiltinRootsVersion = "7.19"
)

// SchedulerDaysSince is the release that brought the scheduler's days=,
// which the builder's page tags its "weekdays" choice with.
const SchedulerDaysSince = schedulerDaysVersion

// Features is what a RouterOS version offers the blocklist builder.
type Features struct {
	// LoopContinue: the scripting language has `:continue`.
	LoopContinue bool
	// FetchFollowsRedirects: `/tool fetch` follows redirects without
	// being told to (http-max-redirect-count defaults to 2).
	FetchFollowsRedirects bool
	// SchedulerDays: `/system scheduler` takes `days=`.
	SchedulerDays bool
	// FetchTrustsBuiltinRoots: the router carries a built-in root CA
	// store, so `/tool fetch check-certificate=yes` can verify a public
	// HTTPS server out of the box.
	FetchTrustsBuiltinRoots bool
}

// FeaturesFor reports what the reported version offers. ok is false when the
// version cannot be read or is below MinimumVersion: the builder refuses
// such a router rather than guessing (owner, answer 18: no fallback below
// the floor), and the zero Features is returned with it.
func FeaturesFor(version string) (Features, bool) {
	got, ok := parseVersion(version)
	if !ok || !atLeast(got, MinimumVersion) {
		return Features{}, false
	}
	return Features{
		LoopContinue:            atLeast(got, loopContinueVersion),
		FetchFollowsRedirects:   atLeast(got, fetchFollowsRedirectsVersion),
		SchedulerDays:           atLeast(got, schedulerDaysVersion),
		FetchTrustsBuiltinRoots: atLeast(got, fetchTrustsBuiltinRootsVersion),
	}, true
}

// AtFloor reports whether version is at or above MinimumVersion. An
// unreadable version is not at the floor: MikroView does not know, and
// the builder must not write commands for a router it cannot place.
func AtFloor(version string) bool {
	got, ok := parseVersion(version)
	return ok && atLeast(got, MinimumVersion)
}

// atLeast compares a parsed version against one of this package's own
// threshold constants, which TestFeatureThresholdsParse proves readable.
func atLeast(got []int, threshold string) bool {
	t, ok := parseVersion(threshold)
	return ok && compareVersions(got, t) >= 0
}
