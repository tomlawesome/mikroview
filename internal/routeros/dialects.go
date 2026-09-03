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
// one dialect ("a") across three rows -- MinimumVersion through
// ReviewedVersion, with 7.24.0 split out on its own because of the find
// bug its Note describes -- but the shape holds a second dialect the day
// RouterOS actually needs one (see the issue's "mixed-dialect estates"
// deferral for why that day hasn't been designed for yet).
//
// VerifiedBy is honest, not aspirational: "exercised" means a real
// router ran these commands, "release notes read" means someone read
// what changed and found nothing that moved them. The first row was
// exercised on the CHR MikroTik boots for scripts/live-routeros.sh; the
// other two were read on 2026-08-29, when this table replaced the
// review-log comment that used to carry these same facts in prose.
var Rows = []Row{
	{From: "7.18", To: "7.23.3", Dialect: "a", VerifiedBy: "exercised on CHR 7.23.3", Note: ""},
	{
		From: "7.24", To: "7.24", Dialect: "a", VerifiedBy: "release notes read 2026-08-29",
		Note: "7.24.0 has a `find` argument-lookup bug, fixed in 7.24.1: on this release, tag rules one at a time rather than with the bulk commands.",
	},
	{From: "7.24.1", To: "7.24.1", Dialect: "a", VerifiedBy: "release notes read 2026-08-29", Note: ""},
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
