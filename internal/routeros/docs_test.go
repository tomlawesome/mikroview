// SPDX-License-Identifier: AGPL-3.0-only

// #1279: docs/routeros-setup.md and docs/configuration.md print the
// exact RouterOS commands the wizard and the drop-list setup card
// generate, and nothing checked that the two still agreed.
// TestSetupDocAddsAreAllGuarded (commands_test.go) only checks that no
// doc line is a bare, unguarded `add` -- it says nothing about whether a
// guarded line's own text still matches what the generator that
// supposedly wrote it produces today. When disabled=no was added to the
// set branches in SchedulerAdd (commands.go) and droplist.NewSetup
// (v0.6.0 pre-release audit, Security stage), five doc blocks went
// stale and that test stayed green throughout.
//
// package routeros_test, not routeros: this file needs internal/droplist
// for the drop-list blocks, and droplist already imports routeros --
// checking those blocks from inside package routeros itself would be
// routeros importing droplist importing routeros, a cycle. An external
// test binary imports both without either production package importing
// the other's test code, so there is nothing to break.
package routeros_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/droplist"
	"github.com/tomlawesome/mikroview/internal/routeros"
)

// docLine finds the one guarded RouterOS line in a doc that mentions
// marker -- fatal if it is missing (the block was deleted or reworded
// out of recognition) or if more than one line matches (the block was
// duplicated, which is just as much a drift risk as going missing).
// Restricting to lines that open with `:if ([:len [` is deliberate: a
// prose sentence naming a command inline is describing it, not the
// command itself, the same distinction TestSetupDocAddsAreAllGuarded
// draws.
func docLine(t *testing.T, path, marker string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var matches []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ":if ([:len [") && strings.Contains(trimmed, marker) {
			matches = append(matches, trimmed)
		}
	}
	switch len(matches) {
	case 0:
		t.Fatalf("%s: no guarded line contains %q -- the block this test checks appears to have been deleted or reworded", path, marker)
	case 1:
		return matches[0]
	default:
		t.Fatalf("%s: %d guarded lines contain %q, want exactly one:\n%s", path, len(matches), marker, strings.Join(matches, "\n"))
	}
	return ""
}

// TestDocSchedulerAndRuleBlocksMatchGenerators is #1279's guard: the five
// doc blocks that went stale when disabled=no was added, each compared
// verbatim against the function that is supposed to have written it,
// fed the doc's own placeholder values.
//
// Excluded on purpose, and not a gap this test should close:
//   - The three `/system script add name=mv-push|mv-backup|mv-backup-https`
//     lines (steps 4e, 7c, 7c-ii). Their source="..." in the doc is prose
//     ("<the blocks from 4c and 4c-ii, escaped for the quotes: ...>"),
//     because the real escaped body is too long to spell out in a guide
//     -- there is no literal text here to compare a generator's output
//     against. TestScheduleCommands and friends already pin the real
//     scriptAdd output byte for byte; this file only reaches the
//     scheduler lines that follow them, which are printed in full.
//   - Steps 1-2's logging action/rule guards (SyslogCommands). They
//     predate the disabled=no fix and never needed it: /system logging
//     action and /system logging have no `disabled` property that a
//     re-paste could leave stuck off, so nothing about them went stale.
//   - routeros-setup.md's "If you're starting from a blank firewall"
//     `/ip firewall filter add` lines -- the doc says outright these are
//     an illustrative example of the operator's own ruleset, "not
//     something to paste in blind", so there is no single generator
//     output for a test to hold them against.
func TestDocSchedulerAndRuleBlocksMatchGenerators(t *testing.T) {
	setupDoc := filepath.Join("..", "..", "docs", "routeros-setup.md")
	configDoc := filepath.Join("..", "..", "docs", "configuration.md")

	t.Run("mv-push scheduler (routeros-setup.md step 4e)", func(t *testing.T) {
		// A single-line body keeps ScheduleCommands' three parts --
		// script add, scheduler add, run -- cleanly split on "\n":
		// scriptAdd's source="..." only grows embedded newlines when the
		// body itself has them.
		lines := strings.Split(routeros.ScheduleCommands(":local x 1", "a"), "\n")
		if len(lines) != 3 {
			t.Fatalf("ScheduleCommands returned %d lines, want 3 (script add, scheduler add, run):\n%s", len(lines), strings.Join(lines, "\n"))
		}
		want := docLine(t, setupDoc, `/system scheduler find name=mv-push]`)
		if lines[1] != want {
			t.Errorf("ScheduleCommands' scheduler line does not match docs/routeros-setup.md step 4e:\n generator: %s\n doc:       %s", lines[1], want)
		}
	})

	t.Run("mv-backup scheduler (routeros-setup.md step 7c)", func(t *testing.T) {
		got := routeros.BackupScheduleCommands("a")
		lines := strings.Split(got, "\n")
		if len(lines) != 2 {
			t.Fatalf("BackupScheduleCommands returned %d lines, want 2 (scheduler add, run):\n%s", len(lines), got)
		}
		want := docLine(t, setupDoc, `/system scheduler find name=mv-backup]`)
		if lines[0] != want {
			t.Errorf("BackupScheduleCommands does not match docs/routeros-setup.md step 7c:\n generator: %s\n doc:       %s", lines[0], want)
		}
	})

	t.Run("mv-backup-https scheduler (routeros-setup.md step 7c-ii)", func(t *testing.T) {
		got := routeros.BackupPushScheduleCommands(":local x 1", "a")
		lines := strings.Split(got, "\n")
		if len(lines) != 3 {
			t.Fatalf("BackupPushScheduleCommands returned %d lines, want 3 (script add, scheduler add, run):\n%s", len(lines), got)
		}
		want := docLine(t, setupDoc, `/system scheduler find name=mv-backup-https]`)
		if lines[1] != want {
			t.Errorf("BackupPushScheduleCommands' scheduler line does not match docs/routeros-setup.md step 7c-ii:\n generator: %s\n doc:       %s", lines[1], want)
		}
	})

	// configuration.md prints its drop-list block with its own
	// placeholder address and key -- <mikroview> and <key> -- rather than
	// a worked example's real values, so NewSetup is called with exactly
	// those two strings: it treats them as ordinary text and quotes them
	// like any other address/key, so the rendered command comes out
	// identical to the doc's.
	got := droplist.NewSetup("<mikroview>", "<key>")

	t.Run("mikroview-drop scheduler (configuration.md)", func(t *testing.T) {
		want := docLine(t, configDoc, `/system scheduler find name=mikroview-drop]`)
		if got.Scheduler != want {
			t.Errorf("droplist.NewSetup's Scheduler does not match docs/configuration.md:\n generator: %s\n doc:       %s", got.Scheduler, want)
		}
	})

	t.Run("drop-list raw rule (configuration.md)", func(t *testing.T) {
		want := docLine(t, configDoc, `/ip firewall raw find comment="mikroview drop list"]`)
		if got.Rule != want {
			t.Errorf("droplist.NewSetup's Rule does not match docs/configuration.md:\n generator: %s\n doc:       %s", got.Rule, want)
		}
	})
}
