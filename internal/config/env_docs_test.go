// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every MIKROVIEW_* environment variable applyEnv reads must appear in
// the configuration reference.
//
// An env override that only exists in Go source is an option nobody can
// find. MIKROVIEW_RECOVERY_PEPPER_FILE was in exactly that state (#268
// finding 12) -- and its own doc comment describes it as being for
// operators who want the pepper off the data volume, which is precisely
// the kind of hardening someone goes looking for and cannot find.
func TestEveryEnvOverrideIsDocumented(t *testing.T) {
	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("reading config.go: %v", err)
	}
	docs, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("reading the configuration reference: %v", err)
	}
	text := string(docs)

	var missing []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`os\.Getenv\("(MIKROVIEW_[A-Z0-9_]+)"\)`).FindAllStringSubmatch(string(source), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !strings.Contains(text, name) {
			missing = append(missing, name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no env overrides in config.go -- this test is not looking where it thinks it is")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d environment override(s) are undocumented in docs/configuration.md: %s",
			len(missing), strings.Join(missing, ", "))
	}
}
