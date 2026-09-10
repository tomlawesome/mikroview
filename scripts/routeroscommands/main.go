// SPDX-License-Identifier: AGPL-3.0-only

// Command routeroscommands prints the RouterOS commands the setup
// wizard renders (#436), straight from internal/routeros -- the same
// functions POST /api/setup/commands calls -- so anything that needs
// "the commands mikroview tells an operator to paste in" reads them from
// the one table instead of holding a second copy that can drift from
// the wizard's own (#894).
//
// -step=all (the default) prints the "starting" blocks: CA trust,
// syslog, and rule tagging -- docs/routeros-setup.md's steps 1-3, the
// ones that get a router logging to mikroview. Push and schedule (step
// 4) need a live mikroview instance and a minted ingest token to mean
// anything; #894's weekly CHR exercise has neither, so they are
// deliberately not part of "the starting commands" this command emits.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// defaultDialect mirrors internal/api/setupcommands.go's own
// defaultDialect: the dialect used when nothing else picks one. Reading
// Rows[0] here rather than duplicating a hard-coded "a" is what keeps
// this command unable to silently disagree with the wizard about what
// "no version picked" renders.
func defaultDialect() string {
	if len(routeros.Rows) == 0 {
		return ""
	}
	return routeros.Rows[0].Dialect
}

// render answers one -step, using exactly the internal/routeros
// functions the wizard's handler calls. Errors are returned rather than
// exiting so main can add the "routeroscommands:" prefix in one place.
func render(step, dialect, address, syslogPort string) (string, error) {
	switch step {
	case "catrust":
		if address == "" {
			return "", fmt.Errorf("-address is required for -step=catrust")
		}
		return routeros.CaTrustCommands(address, dialect), nil
	case "syslog":
		if address == "" || syslogPort == "" {
			return "", fmt.Errorf("-address and -syslog-port are required for -step=syslog")
		}
		return routeros.SyslogCommands(address, syslogPort, dialect), nil
	case "ruletagging":
		return routeros.RuleTaggingCommands(dialect), nil
	case "all":
		if address == "" || syslogPort == "" {
			return "", fmt.Errorf("-address and -syslog-port are required for -step=all")
		}
		return strings.Join([]string{
			routeros.CaTrustCommands(address, dialect),
			routeros.SyslogCommands(address, syslogPort, dialect),
			routeros.RuleTaggingCommands(dialect),
		}, "\n\n"), nil
	default:
		return "", fmt.Errorf("unknown -step %q (want catrust, syslog, ruletagging, or all)", step)
	}
}

func main() {
	dialect := flag.String("dialect", defaultDialect(), "dialect to render (defaults to the table's own default dialect)")
	address := flag.String("address", "", "mikroview address (host[:port]) the CA-trust and syslog commands should point at")
	syslogPort := flag.String("syslog-port", "", "syslog listen address (e.g. \":6514\") or bare port for the syslog step")
	step := flag.String("step", "all", "catrust, syslog, ruletagging, or all")
	flag.Parse()

	out, err := render(*step, *dialect, *address, *syslogPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routeroscommands: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
