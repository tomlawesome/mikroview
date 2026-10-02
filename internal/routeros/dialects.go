// SPDX-License-Identifier: AGPL-3.0-only

package routeros

// Row is one entry in mikroview's command-dialect table (#436): a
// version range that renders the same RouterOS commands (Dialect), how
// that was established (VerifiedBy), and anything worth knowing about
// this specific range that the commands themselves don't say (Note).
//
// Keyed by dialect, not by version, is the design: most releases carry
// forward the previous row's dialect unchanged, which is what keeps this
// table light. A row exists to record a version boundary, not because
// every version needs its own.
type Row struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Dialect    string `json:"dialect"`
	VerifiedBy string `json:"verifiedBy"`
	Note       string `json:"note"`
}

// Rows is mikroview's whole command-dialect table, in order. Today it is
// one dialect ("a") across four rows -- MinimumVersion through
// ReviewedVersion -- but the shape holds a second dialect the day
// RouterOS actually needs one (see the issue's "mixed-dialect estates"
// deferral for why that day hasn't been designed for yet).
//
// 7.24.0 keeps its own row, but no longer because of a dialect
// difference: #924 showed the "find bug" it was split out for was our
// own missing `where`, not anything RouterOS changed. The row stays
// because 7.24.0 is where /ip/service's address became available-from
// (#1411), which is worth seeing rather than folding away.
//
// VerifiedBy is honest, not aspirational: "exercised" means a real
// router ran these commands, "release notes read" means someone read
// what changed and found nothing that moved them. 7.23.3 and 7.24.2
// were exercised by the CHR job (#894) on 2026-09-04 -- the first two
// releases anything was ever actually run against. 7.24 was run on a
// real CHR on 2026-10-01 (#1411); the 7.24.1 row was read on 2026-08-29
// and has not been run since.
var Rows = []Row{
	{From: "7.18", To: "7.23.3", Dialect: "a", VerifiedBy: "exercised on CHR 7.23.3, 2026-09-04", Note: ""},
	{
		From: "7.24", To: "7.24", Dialect: "a", VerifiedBy: "exercised on CHR 7.24, 2026-10-01",
		Note: "7.24.0 was recorded as having a `find` argument-lookup bug fixed in 7.24.1. That was a misreading of #924: the bulk tagging command was missing `where`, which is a syntax error on every release tested, not a 7.24.0 defect. A real CHR 7.24 ran the push on 2026-10-01 (#1411, docs/routeros-verification-logs/7.24-push-ip-service-address.log): it is the release that renamed /ip/service's address restriction to available-from, which the push reads under either name.",
	},
	{From: "7.24.1", To: "7.24.1", Dialect: "a", VerifiedBy: "release notes read 2026-08-29", Note: ""},
	{From: "7.24.2", To: "7.24.2", Dialect: "a", VerifiedBy: "exercised on CHR 7.24.2, 2026-09-04", Note: ""},
	{From: "7.24.3", To: "7.24.3", Dialect: "a", VerifiedBy: "release notes read 2026-09-22",
		Note: "7.24.3 reworked certificate handling: it adds the `SSL.com Root Certification Authority ECC` root to the built-in store, removes `GoDaddy Class 2 CA` from it (the operator-facing warning for that is the `cert-store-7.24.3` entry in Upgrades), and allows importing a cross-signed certificate without replacing the one already there. Read only, never exercised: the CHR job has no mikroview server beside it, so the CA fetch times out and the import step is parsed rather than run. Also fixes two CVEs, crypto CVE-2026-67278 and user CVE-2026-16347; neither is reachable from anything mikroview pastes, since mikroview never logs in to the router."},
	{From: "7.24.4", To: "7.24.4", Dialect: "a", VerifiedBy: "exercised on CHR 7.24.4, 2026-09-22", Note: ""},
}

// RowFor returns the row whose [From, To] range contains version, and
// whether one was found. An unparseable version, or one outside every
// row's range, answers false -- the caller's job, not this function's,
// to decide what an uncovered version means (see VersionStanding).
func RowFor(version string) (Row, bool) {
	got, ok := parseVersion(version)
	if !ok {
		return Row{}, false
	}
	for _, row := range Rows {
		from, ok := parseVersion(row.From)
		if !ok {
			continue
		}
		to, ok := parseVersion(row.To)
		if !ok {
			continue
		}
		if compareVersions(got, from) >= 0 && compareVersions(got, to) <= 0 {
			return row, true
		}
	}
	return Row{}, false
}

// NewestVersion is the newest "to" bound across every row -- the table's
// own answer to "reviewed up to where", which ReviewedVersion restates
// as a constant (see that constant's doc comment for why it isn't
// simply this function, and TestReviewedVersionMatchesNewest for what
// keeps the two from drifting apart).
func NewestVersion() string {
	var newest []int
	var newestVersion string
	for _, row := range Rows {
		to, ok := parseVersion(row.To)
		if !ok {
			continue
		}
		if newestVersion == "" || compareVersions(to, newest) > 0 {
			newest = to
			newestVersion = row.To
		}
	}
	return newestVersion
}
