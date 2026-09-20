// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestUIAllowLoadsFromTheFileAndDefaultsToEveryone covers issue #1287's
// upgrade property at the config layer: absent, the key parses to an
// empty list, which internal/api reads as "admit everybody". It also
// pins that "ui" is a key the loader recognises at all -- an unknown
// one refuses to start (see explainYAMLError).
func TestUIAllowLoadsFromTheFileAndDefaultsToEveryone(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.UI.Allow) != 0 {
		t.Errorf("UI.Allow = %v on defaults, want empty -- an upgrade must not lock anyone out", cfg.UI.Allow)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  allow: [\"192.168.1.0/24\", \"10.0.0.7\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path, nil)
	if err != nil {
		t.Fatalf("ui.allow was not accepted as a configuration key: %v", err)
	}
	if !reflect.DeepEqual(cfg.UI.Allow, []string{"192.168.1.0/24", "10.0.0.7"}) {
		t.Errorf("UI.Allow = %v, want the two entries from the file", cfg.UI.Allow)
	}
}

func TestParseUIAllow(t *testing.T) {
	prefixes, err := ParseUIAllow([]string{"192.168.1.0/24", " 10.0.0.7 ", ""})
	if err != nil {
		t.Fatalf("ParseUIAllow: %v", err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("got %d prefixes, want 2 (a blank entry is skipped): %v", len(prefixes), prefixes)
	}
	if got := prefixes[1].String(); got != "10.0.0.7/32" {
		t.Errorf("a bare address parsed to %q, want a single-host prefix", got)
	}

	if _, err := ParseUIAllow([]string{"not-an-address"}); err == nil {
		t.Error("a junk entry was accepted -- an entry silently skipped is either a lockout or a wall that is not there")
	}
	// No "private" shorthand here, unlike trustedProxies -- see
	// ParseUIAllow's doc comment.
	if _, err := ParseUIAllow([]string{"private"}); err == nil {
		t.Error(`"private" was accepted for ui.allow, but the whole LAN plus CGNAT plus link-local is not a list anyone means to write for who may administer MikroView`)
	}
}

// TestValidateRejectsABadUIAllowEntry: -validate-config has to catch
// this before a deploy rather than after, because the setting is only
// editable in the file.
func TestValidateRejectsABadUIAllowEntry(t *testing.T) {
	cfg := defaults()
	cfg.UI.Allow = []string{"192.168.1.0/24", "nonsense"}

	var found bool
	for _, p := range cfg.Validate().Fatal {
		if p.Code == "CFG-0004" && p.Key == "ui.allow" {
			found = true
			if p.Example == "" {
				t.Error("CFG-0004 has no paste-ready example")
			}
		}
	}
	if !found {
		t.Errorf("a bad ui.allow entry produced no fatal CFG-0004 problem: %+v", cfg.Validate().Fatal)
	}
}
