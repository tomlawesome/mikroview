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
//
// -step=blocklist -version=<v> prints the blocklist builder's block
// (#1360) for a router on that RouterOS version: all seven lists on,
// each with its catalogue defaults, part 1's push re-set with a
// placeholder address and token. It is what scripts/live-blocklist-chr.sh
// pastes into a real CHR, read from the same generator the builder's API
// calls. -undo=<key> prints one list's undo instead, and -undo=all the
// undo for everything. -nul separates top-level console commands with a
// NUL byte rather than a newline, since a loader script's source spans
// lines inside one command.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
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
		return routeros.SyslogCommands(address, syslogPort, dialect, ""), nil
	case "ruletagging":
		return routeros.RuleTaggingCommands(dialect), nil
	case "all":
		if address == "" || syslogPort == "" {
			return "", fmt.Errorf("-address and -syslog-port are required for -step=all")
		}
		return strings.Join([]string{
			routeros.CaTrustCommands(address, dialect),
			routeros.SyslogCommands(address, syslogPort, dialect, ""),
			routeros.RuleTaggingCommands(dialect),
		}, "\n\n"), nil
	default:
		return "", fmt.Errorf("unknown -step %q (want catrust, syslog, ruletagging, blocklist, or all)", step)
	}
}

// blocklistPlaceholderAddress and Token stand in for a live MikroView:
// the CHR check proves the block's syntax and the loaders, not the
// push's delivery, which part 2's own CHR run covered.
const (
	blocklistPlaceholderAddress = "mikroview.invalid:8443"
	blocklistPlaceholderToken   = "mv-placeholder-token"
)

// blocklistSteps renders -step=blocklist: every catalogue list on with
// its defaults for version, or the undo named by undo ("all" or a key).
func blocklistSteps(version, undo string) ([]string, error) {
	switch {
	case undo == "all":
		return strings.Split(routeros.BlocklistUndoAll(), "\n"), nil
	case undo != "":
		u, err := routeros.BlocklistUndo(undo)
		if err != nil {
			return nil, err
		}
		return strings.Split(u, "\n"), nil
	}
	feat, ok := routeros.FeaturesFor(version)
	if !ok {
		return nil, fmt.Errorf("-version %q is below MikroView's floor or unreadable", version)
	}
	var lists []routeros.BlocklistChoice
	for _, l := range blcatalogue.Lists() {
		lists = append(lists, routeros.BlocklistChoice{
			Key: l.Key, Direction: l.DefaultDirection, IPv6: l.IPv6, Log: true,
			Refresh: l.RefreshDefaultFor(feat.SchedulerDays),
		})
	}
	block, err := routeros.BlocklistBlock(routeros.BlocklistRequest{
		Device: "chr", RouterOSVersion: version, Lists: lists,
		Push: routeros.BlocklistPush{
			Address: blocklistPlaceholderAddress, Token: blocklistPlaceholderToken,
			Kinds: []string{"filter-rule", "address-list"},
		},
	})
	if err != nil {
		return nil, err
	}
	return block.Steps(), nil
}

func main() {
	dialect := flag.String("dialect", defaultDialect(), "dialect to render (defaults to the table's own default dialect)")
	address := flag.String("address", "", "MikroView address (host[:port]) the CA-trust and syslog commands should point at")
	syslogPort := flag.String("syslog-port", "", "syslog listen address (e.g. \":6514\") or bare port for the syslog step")
	step := flag.String("step", "all", "catrust, syslog, ruletagging, blocklist, or all")
	version := flag.String("version", "", "RouterOS version the blocklist block is written for (-step=blocklist)")
	undo := flag.String("undo", "", "with -step=blocklist: print the undo for one list key, or all")
	nul := flag.Bool("nul", false, "with -step=blocklist: separate console commands with NUL bytes")
	flag.Parse()

	if *step == "blocklist" {
		steps, err := blocklistSteps(*version, *undo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "routeroscommands: %v\n", err)
			os.Exit(2)
		}
		if *nul {
			for _, s := range steps {
				fmt.Print(s, "\x00")
			}
			return
		}
		fmt.Println(strings.Join(steps, "\n"))
		return
	}

	out, err := render(*step, *dialect, *address, *syslogPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routeroscommands: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
