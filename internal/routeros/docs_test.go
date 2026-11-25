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
//   - The two `/system script add name=mv-push|mv-backup-https` lines
//     (steps 4e, 7c-ii). Their source="..." in the doc is a rule, not
//     text ("<the blocks from 4c and 4c-ii as one line: first every \
//     becomes \\, ...>"), because the real escaped body is too long to
//     spell out in a guide. TestDocHandEscapeRuleMatchesGenerators
//     applies that rule to the doc's own blocks and holds the result to
//     ScheduleCommands and BackupPushScheduleCommands. Step 7c's
//     mv-backup line is printed in full, and TestDocBackupScriptIsOneLine
//     holds it to BackupScript.
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

// docFence returns the one fenced block in a doc that contains marker,
// fatal unless there is exactly one.
func docFence(t *testing.T, path, marker string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var matches []string
	parts := strings.Split(string(data), "```")
	// Odd indexes are the insides of fences.
	for i := 1; i < len(parts); i += 2 {
		if strings.Contains(parts[i], marker) {
			matches = append(matches, strings.Trim(parts[i], "\n"))
		}
	}
	if len(matches) != 1 {
		t.Fatalf("%s: %d fenced blocks contain %q, want exactly one", path, len(matches), marker)
	}
	return matches[0]
}

// #1360: routeros-setup.md 4c-iii prints the raw-rule and
// address-list-count blocks in full, with the doc's own placeholders --
// held to the generator byte for byte, so a change to either block that
// does not update the guide fails here.
func TestDocBlocklistPushBlocksMatchGenerators(t *testing.T) {
	setupDoc := filepath.Join("..", "..", "docs", "routeros-setup.md")
	for _, kind := range []string{"raw-rule", "address-list-count"} {
		t.Run(kind, func(t *testing.T) {
			want := routeros.PushBlock("<mikroview-host:port>", "<your ingest token>", kind, "a")
			got := docFence(t, setupDoc, `"kind"="`+kind+`"`)
			if got != want {
				t.Errorf("routeros-setup.md's %s block does not match PushBlock:\n doc:\n%s\n generator:\n%s", kind, got, want)
			}
		})
	}
}

// #1412: routeros-setup.md 7c prints the SFTP backup script for pasting
// by hand. Before RouterOS 7.19 the console drops a line break typed
// inside a quoted argument, so a multi-line source="..." saved as one
// run-on line on 7.18 -- MikroView's floor -- and the script did
// nothing. The wizard writes each break as \n (scriptSource); the guide
// prints the same one line, held to BackupScript byte for byte with the
// doc's own placeholders.
func TestDocBackupScriptIsOneLine(t *testing.T) {
	setupDoc := filepath.Join("..", "..", "docs", "routeros-setup.md")
	want := routeros.BackupScript("<mikroview-host>", "47022", "<device>", "<token>", "a")
	if strings.Contains(want, "\n") {
		t.Fatalf("BackupScript spans lines, which 7.18 would join into one:\n%s", want)
	}
	got := docLine(t, setupDoc, `/system script find name=mv-backup]`)
	if got != want {
		t.Errorf("routeros-setup.md step 7c's script line does not match BackupScript:\n doc:       %s\n generator: %s", got, want)
	}
}

// #1416: routeros-setup.md 4e and 7c-ii cannot print their script add in
// full, so they give the operator a rule for turning the blocks above
// them into the source="..." value. Before #1416 the rule escaped
// quotes, backslashes and dollars but left the line breaks in, and a
// 7.18 console drops a line break pasted inside a quoted string (the
// fault #1412 fixed in 7c), so the hand-built script saved as one
// run-on line. This test reads the rule the doc states, applies it step
// by step in the doc's order to the doc's own blocks, and holds the
// finished line to the generator the wizard uses, byte for byte -- so
// the rule, the blocks and the wizard cannot drift apart unnoticed.
// The old and new forms pasted into real CHRs:
// docs/routeros-verification-logs/{7.18.2,7.24.4}-hand-paste-scripts.log.
func TestDocHandEscapeRuleMatchesGenerators(t *testing.T) {
	setupDoc := filepath.Join("..", "..", "docs", "routeros-setup.md")
	// The rule as the doc words it. applyDocRule below performs exactly
	// these steps in this order; change one and the other must follow.
	const rule = `as one line: first every \ becomes \\, then every " becomes \", every $ becomes \$, and every line break becomes \n>`
	const same = `<the same one-line source>`
	applyDocRule := func(body string) string {
		body = strings.ReplaceAll(body, `\`, `\\`)
		body = strings.ReplaceAll(body, `"`, `\"`)
		body = strings.ReplaceAll(body, `$`, `\$`)
		return strings.ReplaceAll(body, "\n", `\n`)
	}

	for _, c := range []struct {
		name, marker, placeholder string
		body                      string
		generator                 func(body string) string
	}{
		{
			name:        "mv-push (step 4e)",
			marker:      `/system script find name=mv-push]`,
			placeholder: `<the blocks from 4c and 4c-ii ` + rule,
			// "all the blocks from 4c and 4c-ii concatenated in order"
			body: docFence(t, setupDoc, `"kind"="filter-rule"`) + "\n\n" + docFence(t, setupDoc, `"kind"="dhcp-lease"`),
			generator: func(body string) string {
				return routeros.ScheduleCommands(body, "a")
			},
		},
		{
			name:        "mv-backup-https (step 7c-ii)",
			marker:      `/system script find name=mv-backup-https]`,
			placeholder: `<the script above ` + rule,
			body:        docFence(t, setupDoc, `"kind"="backup"`),
			generator: func(body string) string {
				return routeros.BackupPushScheduleCommands(body, "a")
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			line := docLine(t, setupDoc, c.marker)
			if strings.Count(line, c.placeholder) != 1 || strings.Count(line, same) != 1 {
				t.Fatalf("routeros-setup.md's line no longer states the rule this test applies;\n want %q and %q in:\n %s", c.placeholder, same, line)
			}
			source := applyDocRule(c.body)
			got := strings.Replace(strings.Replace(line, c.placeholder, source, 1), same, source, 1)
			if strings.Contains(got, "\n") {
				t.Fatalf("the doc's rule leaves a line break in the command, which 7.18 drops")
			}
			want := strings.SplitN(c.generator(c.body), "\n", 2)[0]
			if got != want {
				t.Errorf("the doc's rule applied to its own blocks does not give the generator's script line:\n doc rule:  %s\n generator: %s", got, want)
			}
		})
	}

	// 7c-ii says BackupPushScript renders "the exact script above"; hold
	// it to that, so the body the rule is applied to is the wizard's own.
	if got, want := docFence(t, setupDoc, `"kind"="backup"`), routeros.BackupPushScript("<mikroview-host:port>", "<your ingest token>", "a"); got != want {
		t.Errorf("routeros-setup.md 7c-ii's script does not match BackupPushScript:\n doc:\n%s\n generator:\n%s", got, want)
	}
}
