// SPDX-License-Identifier: AGPL-3.0-only

package routeros

// Issue #1344: a router that upgrades past a RouterOS release with a
// breaking change got no warning -- the one note that existed (7.24.3's
// certificate-store change) was tied to the exact 7.24.3 row in Rows,
// and the per-router note the server computed from it was never
// rendered anywhere. This file is a separate list, keyed by "from this
// version onward" rather than by dialect range, because those are two
// different axes: a row in Rows is a version range that renders the
// same commands, and an Upgrade here is a change that persists from a
// release onward, whatever row that release falls in.

// Upgrade is one "from version X onward, this affects Y" warning the
// setup screen shows beside the commands, for every router whose
// reported version is at or past From. Separate from Rows: a row is a
// dialect range, an upgrade is a change that persists from a release
// onward, whatever row that release falls in.
type Upgrade struct {
	// ID is the stable key the frontend groups on and tests name.
	// Lower-case, hyphenated, never reused once shipped.
	ID string `json:"id"`
	// From is the first RouterOS release the change applies to,
	// inclusive. Every later version applies too; there is no upper
	// bound (see UpgradesFor).
	From string `json:"from"`
	// Steps names the command blocks the change touches, as keys the
	// frontend turns into step titles: "syslog", "push", "backup",
	// "droplist". Keys, not titles, so a renamed step (#1284) cannot
	// leave this pointing at a name that no longer exists.
	Steps []string `json:"steps"`
	// Heading is one sentence saying what RouterOS changed. Body says
	// what the operator will see and what to do. Both are the
	// finished, operator-facing text; the frontend adds only which
	// routers it concerns and which steps.
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// UpgradeSteps is every key Steps may carry. TestUpgradesWellFormed
// holds the list to it, and the frontend's label map mirrors it.
var UpgradeSteps = []string{"syslog", "push", "backup", "droplist"}

// Upgrades is mikroview's whole catalogue of upgrade warnings, in the
// order the setup screen groups them. Coverage of the six commands
// cert-store-7.24.3 names: syslog = the remote logging action
// (commands.go:254); push = the per-kind and logging /tool fetch lines
// in the push script (commands.go:416, 488); backup = the begin and
// slice fetches for both backup files (commands.go:643, 653); droplist
// = the drop-list scheduler fetch (internal/droplist/setup.go:69).
var Upgrades = []Upgrade{
	{
		ID:      "cert-store-7.24.3",
		From:    "7.24.3",
		Steps:   []string{"syslog", "push", "backup", "droplist"},
		Heading: "RouterOS 7.24.3 stopped trusting one public root certificate, GoDaddy Class 2.",
		Body: "Every command here that says check-certificate=yes checks MikroView's certificate against what the router trusts. " +
			"If you imported MikroView's own certificate in Trust the certificate, nothing changes. " +
			"If MikroView sits behind a certificate you bought and it chains to that GoDaddy root, then after the upgrade the router refuses every one of those commands with `SSL: ssl: no trusted CA certificate found` in its log, and logs, router state, backups and the drop list all stop arriving. " +
			"Check the chain your certificate uses; if it is that one, either import your CA on the router the way Trust the certificate does, or move MikroView to a certificate from a root the router still trusts.",
	},
}

// UpgradesFor returns every Upgrade whose From is at or below version,
// in Upgrades' order. An unparseable version returns nil: the same
// "say nothing rather than something wrong" rule VersionStanding
// follows, since a false warning in front of an operator is worse
// than none.
func UpgradesFor(version string) []Upgrade {
	got, ok := parseVersion(version)
	if !ok {
		return nil
	}
	var out []Upgrade
	for _, u := range Upgrades {
		from, ok := parseVersion(u.From)
		if !ok {
			continue
		}
		if compareVersions(got, from) >= 0 {
			out = append(out, u)
		}
	}
	return out
}
