// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestLiveScriptsCoverEveryStore is #595's recurrence guard, and the
// sibling of TestBackupCoversAllConfigPathFields above it.
//
// The store block a live-* script writes into its generated config was
// copy-pasted into four scripts. Three fell behind config.Config, and
// because checkStoresUsable (storage_preflight.go, #536) refuses to
// start on the first unwritable path, all three stopped being able to
// start a server at all -- silently, for long enough that nobody
// noticed three real checks had died. Nothing compared those configs
// against the stores the binary actually requires.
//
// This does that comparison. scripts/live-stores.sh is now the single
// copy; this test requires it to name every store backedUpStores does,
// so adding a store to config.Config and forgetting the harness fails
// the build instead of quietly breaking every standalone script.
//
// It checks the YAML *key*, not the path, because the key is what the
// config parser binds and the path is the caller's to choose.
func TestLiveScriptsCoverEveryStore(t *testing.T) {
	block, err := os.ReadFile("scripts/live-stores.sh")
	if err != nil {
		t.Fatalf("reading the shared store block: %v", err)
	}
	text := string(block)

	// Each store's YAML section and the key its path is written under.
	// Both halves matter: several stores use the key "storePath", so
	// checking the key alone passes even when a whole section has been
	// dropped -- which is exactly the drift this test exists to catch,
	// and exactly what an earlier version of it missed.
	type loc struct{ section, key string }
	want := map[string]loc{
		"auth":              {"auth", "storePath"},
		"tokens":            {"auth", "tokensStorePath"},
		"recovery_keys":     {"auth", "recoveryKeysPath"},
		"flags":             {"flags", "storePath"},
		"rule_usage":        {"flags", "ruleUsageStorePath"},
		"detector_settings": {"flags", "detectorSettingsStorePath"},
		"entities":          {"entities", "storePath"},
		"coverage":          {"coverage", "storePath"},
		"mac_registry":      {"deviceMac", "storePath"},
		"engine_state":      {"engine", "storePath"},
		"definitions":       {"engine", "definitionsStorePath"},
		"audit":             {"audit", "storePath"},
		"setup":             {"setup", "storePath"},
		"settings":          {"store", "settingsStorePath"},
		"watchlist":         {"watchlist", "storePath"},
		"suggestions":       {"watchlist", "suggestionsStorePath"},
		"match_log":         {"watchlist", "matchLogPath"},
	}

	sections := parseStoreBlock(text)

	for _, s := range backedUpStores(config.Config{}) {
		l, ok := want[s.Name]
		if !ok {
			t.Errorf("store %q is in backedUpStores but this test does not know where it lives -- add it here and to scripts/live-stores.sh, or the live-* scripts will silently stop starting", s.Name)
			continue
		}
		keys, ok := sections[l.section]
		if !ok {
			t.Errorf("store %q needs a %q section in scripts/live-stores.sh, and there is none -- the server refuses to start on the first store it cannot write (storage_preflight.go)", s.Name, l.section)
			continue
		}
		if !keys[l.key] {
			t.Errorf("store %q is missing: scripts/live-stores.sh has a %q section with no %q -- every live-* script writes that block", s.Name, l.section, l.key)
		}
	}
}

// parseStoreBlock reads the YAML the shared block prints into
// section -> set of keys. Deliberately tiny and shape-specific rather
// than a real YAML parse: the block is one file this repository owns,
// written in two forms (a nested mapping, or an inline one on the
// section line), and a dependency to read it would be absurd.
func parseStoreBlock(text string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	var current string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  ") {
			if current == "" {
				continue
			}
			if k, _, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
				out[current][k] = true
			}
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(name, " \t#") || name == "" {
			current = ""
			continue
		}
		current = name
		if _, seen := out[current]; !seen {
			out[current] = map[string]bool{}
		}
		// Inline form: `entities: {storePath: $dir/entities.json}`.
		if inline := strings.TrimSpace(rest); strings.HasPrefix(inline, "{") {
			for _, pair := range strings.Split(strings.Trim(inline, "{}"), ",") {
				if k, _, ok := strings.Cut(strings.TrimSpace(pair), ":"); ok {
					out[current][k] = true
				}
			}
		}
	}
	return out
}

// TestLiveScriptsUseTheSharedStoreBlock keeps the copies from coming
// back. A script that writes store paths inline has re-created exactly
// the drift #595 was.
func TestLiveScriptsUseTheSharedStoreBlock(t *testing.T) {
	for _, name := range []string{
		"scripts/live-env.sh",
		"scripts/live-cert-reload.sh",
		"scripts/live-logspam-check.sh",
		"scripts/live-tls-log-lines.sh",
	} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(b), "mv_store_block") {
			t.Errorf("%s does not use mv_store_block from scripts/live-stores.sh -- an inline store list is what #595 was", name)
		}
	}
}

// TestEveryLiveCheckIsRun is #624's guard.
//
// Both runners find their checks by glob, so a new one is picked up
// without a second edit -- that is what stops a check joining the
// graveyard three standalone scripts sat in for months, unable to start
// a server and producing no signal at all (#595).
//
// What the glob cannot protect is the exclusion lists. An exclusion is a
// decision -- "this needs a router booted", "this is a helper, not a
// check" -- and a list of them is exactly the thing that goes stale:
// run-scenarios.sh's own comment says so about the copy-pasted loop it
// replaced. So this test pins the exclusions. Adding one fails the build
// until it is recorded here with a reason, and removing a file that is
// still excluded fails too, rather than leaving dead config behind that
// reads as deliberate.
func TestEveryLiveCheckIsRun(t *testing.T) {
	excluded := map[string]string{
		"frontend/scripts/live-browser.mjs":       "the shared helper every scenario imports, not a scenario",
		"frontend/scripts/live-routeros-real.mjs": "needs a real CHR booted; run by make live-routeros-container",
		"scripts/live-env.sh":                     "the shared environment helper",
		"scripts/live-container.sh":               "the shared environment helper, container flavour",
		"scripts/live-stores.sh":                  "the shared store block, sourced not run",
		"scripts/live-slot.sh":                    "the shared port allocator, sourced not run (#660)",
		"scripts/live-routeros.sh":                "boots the CHR; driven by make live-routeros-container",
		"scripts/live-routeros-step0.sh":          "a probe driven by live-routeros.sh, not a standalone check",
		"scripts/live-rule-coverage-probe.sh":     "a probe driven by live-routeros.sh, not a standalone check",
	}

	runners := map[string]string{
		"frontend/scripts/live-*.mjs": "scripts/run-scenarios.sh",
		"scripts/live-*.sh":           "scripts/run-live-scripts.sh",
	}

	seen := map[string]bool{}
	for pattern, runner := range runners {
		body, err := os.ReadFile(runner)
		if err != nil {
			t.Fatalf("reading %s: %v", runner, err)
		}
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("globbing %s: %v", pattern, err)
		}
		if len(files) == 0 {
			t.Errorf("%s matched nothing -- either the checks moved or this guard is looking in the wrong place", pattern)
		}
		for _, f := range files {
			seen[f] = true
			reason, isExcluded := excluded[f]
			inRunner := strings.Contains(string(body), filepath.Base(f))
			switch {
			case isExcluded && reason == "":
				t.Errorf("%s is excluded with no reason given", f)
			case isExcluded && !inRunner:
				t.Errorf("%s is recorded here as excluded (%s) but %s does not mention it, so nothing is actually skipping it", f, reason, runner)
			case !isExcluded && inRunner:
				t.Errorf("%s is named in %s but not recorded here. If it is being skipped, say why in this test; a silent exclusion is how a check stops running without anyone noticing", f, runner)
			}
		}
	}

	// A stale exclusion is dead config that reads as a decision.
	for f := range excluded {
		if !seen[f] {
			t.Errorf("%s is excluded here but no longer exists -- remove the exclusion", f)
		}
	}
}

// Both phases have to actually be invoked, or the globbing above is
// describing a runner nothing calls -- which is the original failure one
// level up.
func TestLiveCheckRunsBothPhases(t *testing.T) {
	b, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	for _, runner := range []string{"scripts/run-scenarios.sh", "scripts/run-live-scripts.sh"} {
		if !strings.Contains(string(b), runner) {
			t.Errorf("make live-check does not run %s, so every check it finds is dead", runner)
		}
	}
}
