// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// Every standalone mode is dispatched on os.Args[1] in main() before the
// flag package is involved at all, so `mikroview --help` could name the
// six flags and nothing else -- an operator reading the binary's own
// help had no way to learn -backup, -restore or -recover-admin-account
// exist (#1176). config.OtherCommands is the block -h prints for them;
// this is what stops a tenth command being added to the dispatch and
// never reaching that block.
var dispatchedSubcommand = regexp.MustCompile(`os\.Args\[1\] == "(-[a-z-]+)"`)

func TestUsageNamesEveryDispatchedSubcommand(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}

	matches := dispatchedSubcommand.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("found no subcommand dispatch in main.go -- has the dispatch been rewritten? This test reads it as source text.")
	}

	seen := map[string]bool{}
	for _, m := range matches {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		// Listed as its own entry in the block, not merely mentioned in
		// the prose above it.
		entry := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `(\s|$)`)
		if !entry.MatchString(config.OtherCommands) {
			t.Errorf("main.go dispatches %s, but `mikroview -h` does not list it (config.OtherCommands)", name)
		}
	}
}
