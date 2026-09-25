// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"strings"
	"testing"
)

func loadExampleYAML(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/config.example.yaml")
	if err != nil {
		t.Fatalf("reading deploy/config.example.yaml: %v", err)
	}
	return raw
}

func keysOf(missing []MissingSetting) map[string]bool {
	out := make(map[string]bool, len(missing))
	for _, m := range missing {
		out[m.Key] = true
	}
	return out
}

// #1218's motivating cases -- an old config with none of these three set
// at all -- must all come back as missing against the real example file.
func TestMissingSettingsFindsTheIssuesMotivatingCases(t *testing.T) {
	missing, err := MissingSettings(loadExampleYAML(t), []byte("devices: []\n"))
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	got := keysOf(missing)
	for _, want := range []string{"geoip", "history", "backup"} {
		if !got[want] {
			t.Errorf("MissingSettings did not offer %q against a config that never sets it", want)
		}
	}
}

// A key the operator's own config already sets, however it is set, must
// not be offered again -- offering something already there is not "new".
func TestMissingSettingsExcludesWhatTheConfigAlreadySets(t *testing.T) {
	running := []byte("devices: []\nhistory:\n  enabled: false\n")
	missing, err := MissingSettings(loadExampleYAML(t), running)
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	got := keysOf(missing)
	if got["history"] {
		t.Error("history is set (even to its own zero value) and should not be offered")
	}
	if !got["backup"] {
		t.Error("backup is still unset and should still be offered")
	}
}

// listen, store and devices are shown live (uncommented) in the example
// file, because a working deployment always has some form of them --
// MissingSettings only ever proposes something to paste in commented
// out, so these must never appear regardless of whether the running
// config sets them.
func TestMissingSettingsNeverOffersARequiredLiveSection(t *testing.T) {
	missing, err := MissingSettings(loadExampleYAML(t), nil)
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	got := keysOf(missing)
	for _, never := range []string{"listen", "store", "devices"} {
		if got[never] {
			t.Errorf("MissingSettings offered %q, a required live section -- it should never be offered", never)
		}
	}
}

// A nil/empty running config -- MIKROVIEW_CONFIG unset entirely -- is
// "nothing set", not an error, and offers every optional section.
func TestMissingSettingsWithNoConfigFileOffersEveryOptionalSection(t *testing.T) {
	missing, err := MissingSettings(loadExampleYAML(t), nil)
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	if len(missing) == 0 {
		t.Fatal("expected at least one optional section to be offered with no config file at all")
	}
}

func TestMissingSettingsRejectsUnparsableRunningConfig(t *testing.T) {
	_, err := MissingSettings(loadExampleYAML(t), []byte("not: [valid: yaml"))
	if err == nil {
		t.Fatal("expected an error for unparsable YAML, got nil")
	}
}

// Each Block is the real, ready-to-paste text from the example file --
// never a synthesised one-liner -- so it stays in sync with
// docs/configuration.md's own explanation by construction.
func TestMissingSettingsBlockIsVerbatimFromTheExampleFile(t *testing.T) {
	missing, err := MissingSettings(loadExampleYAML(t), nil)
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	for _, m := range missing {
		if m.Key != "geoip" {
			continue
		}
		if !strings.Contains(m.Block, "# geoip:") {
			t.Errorf("geoip's block does not contain its own key line:\n%s", m.Block)
		}
		if !strings.Contains(m.Block, "dbPath") {
			t.Errorf("geoip's block does not contain dbPath:\n%s", m.Block)
		}
		for _, line := range strings.Split(m.Block, "\n") {
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				t.Errorf("geoip's block has a live (uncommented) line, but geoip: is meant to be pasted in commented out: %q", line)
			}
		}
		return
	}
	t.Fatal("geoip was not offered at all")
}

// ruleNames and hostNames share one explanatory comment block in the
// example file (config.example.yaml's own author's choice, not this
// package's); MissingSettings keys a shared block by whichever valid
// name it finds first, so setting one is read as having dealt with the
// block and suppresses the other too. Documented here as the known
// trade-off, not a bug to chase: an operator who has set one already has
// that same comment open in front of them.
func TestMissingSettingsTreatsSharedCommentBlockAsOneUnit(t *testing.T) {
	missing, err := MissingSettings(loadExampleYAML(t), []byte(`devices: []
ruleNames:
  r13: "Block known scanners"
`))
	if err != nil {
		t.Fatalf("MissingSettings: %v", err)
	}
	got := keysOf(missing)
	if got["hostNames"] {
		t.Error("hostNames was offered even though ruleNames -- sharing its comment block -- is already set")
	}
}

// A build's own field set (configTopLevelKeys), not a hand-maintained
// list, is what decides which key line closes a section -- this guards
// the reflect walk itself rather than any one field's presence.
func TestConfigTopLevelKeysFindsKnownFields(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range configTopLevelKeys() {
		keys[k] = true
	}
	for _, want := range []string{"listen", "store", "devices", "history", "backup", "setup"} {
		if !keys[want] {
			t.Errorf("configTopLevelKeys is missing %q", want)
		}
	}
}

// parseExampleSections' own boundary rule: a blank line stays inside the
// current section when what follows it is still indented, and only
// closes the section when what follows returns to zero indentation --
// exercised directly, on a small fixture, rather than only indirectly
// through the real (much larger) example file.
func TestParseExampleSectionsKeepsIndentedContinuationAcrossABlankLine(t *testing.T) {
	text := `listen:
  http: ":8080"

  # Reverse-proxy support. Leave commented out unless a proxy fronts it.
  # trustedProxies: ["private"]

store:
  retention: 24h
`
	valid := map[string]bool{"listen": true, "store": true}
	sections := parseExampleSections(text, valid)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d: %+v", len(sections), sections)
	}
	if sections[0].key != "listen" || sections[0].commented {
		t.Errorf("section 0 = %+v, want key=listen commented=false", sections[0])
	}
	if !strings.Contains(sections[0].block, "trustedProxies") {
		t.Errorf("listen's block dropped its indented continuation past the blank line:\n%s", sections[0].block)
	}
	if sections[1].key != "store" {
		t.Errorf("section 1 = %+v, want key=store", sections[1])
	}
}
