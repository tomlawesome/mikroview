// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed reference/menus.json
var menusJSON []byte

type refMenu struct {
	Path       string   `json:"path"`
	Console    string   `json:"console"`
	Properties []string `json:"properties"`
}

type refFile struct {
	Menus []refMenu `json:"menus"`
}

func loadReference(t *testing.T) refFile {
	t.Helper()
	var ref refFile
	if err := json.Unmarshal(menusJSON, &ref); err != nil {
		t.Fatalf("parsing reference/menus.json: %v", err)
	}
	if len(ref.Menus) == 0 {
		t.Fatal("reference/menus.json lists no menus, so this test would pass against anything")
	}
	return ref
}

// identifier reports whether tok looks like a bare console path segment
// -- letters, digits and dashes. Anything with '=', '[', '"' or ':' has
// stopped being part of the path and started being arguments.
func identifier(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// menuFor returns the longest menu path in the reference that prefixes
// this command line. "/ip firewall filter set [find ...]" matches
// "ip/firewall/filter" -- `set` is a verb, not a menu, and has no page
// of its own; "/tool fetch url=..." matches "tool/fetch", which does.
func menuFor(line string, known map[string]bool) (string, bool) {
	fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "/"))
	var segs []string
	for _, f := range fields {
		if !identifier(f) {
			break
		}
		segs = append(segs, f)
	}
	for i := len(segs); i > 0; i-- {
		if candidate := strings.Join(segs[:i], "/"); known[candidate] {
			return candidate, true
		}
	}
	return "", false
}

// TestEmittedCommandsUseKnownMenus is the guard #924 wanted: every
// console menu mikroview tells an operator to run must be one recorded
// in reference/menus.json, which scripts/routerosreference checks
// against MikroTik's own published reference.
//
// It catches a menu that was mistyped, renamed or invented. It cannot
// catch a menu that exists being driven with wrong syntax -- that is
// what the CHR exercise (#894) is for, and it is exactly how #924 was
// found.
func TestEmittedCommandsUseKnownMenus(t *testing.T) {
	ref := loadReference(t)
	known := map[string]bool{}
	for _, m := range ref.Menus {
		known[m.Path] = true
	}

	const (
		address = "203.0.113.10:8443"
		syslog  = "6514"
		dialect = "a"
	)
	blocks := map[string]string{
		"CaTrustCommands":     CaTrustCommands(address, dialect),
		"SyslogCommands":      SyslogCommands(address, syslog, dialect),
		"RuleTaggingCommands": RuleTaggingCommands(dialect),
		"ScheduleCommands":    ScheduleCommands(dialect),
	}

	checked := 0
	for name, block := range blocks {
		for _, line := range strings.Split(block, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "/") {
				// Comments, blank lines and scripting statements --
				// only console paths are in scope here.
				continue
			}
			checked++
			if _, ok := menuFor(trimmed, known); !ok {
				t.Errorf("%s emits a command on a menu not in reference/menus.json:\n  %s\n"+
					"Add the menu to the reference (and check it is real) or fix the command.", name, trimmed)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no console commands were checked, so this test proves nothing")
	}
	t.Logf("checked %d emitted console commands against %d known menus", checked, len(known))
}

// TestReferenceMenusAreWellFormed keeps the reference itself honest: a
// menu with no path or no properties would silently widen what the
// guard above accepts.
func TestReferenceMenusAreWellFormed(t *testing.T) {
	ref := loadReference(t)
	seen := map[string]bool{}
	for _, m := range ref.Menus {
		if m.Path == "" {
			t.Errorf("a menu entry has no path (console %q)", m.Console)
		}
		if strings.HasPrefix(m.Path, "/") {
			t.Errorf("menu %q: path is the documentation form (ip/firewall/filter), not the console form", m.Path)
		}
		if len(m.Properties) == 0 {
			t.Errorf("menu %q lists no properties", m.Path)
		}
		if seen[m.Path] {
			t.Errorf("menu %q appears twice", m.Path)
		}
		seen[m.Path] = true
	}
}
