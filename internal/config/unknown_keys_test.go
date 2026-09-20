// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigFile is a small helper shared by the tests below: write src
// to a fresh config.yaml in a temp dir and return its path.
func writeConfigFile(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// #1207: an unknown key used to be dropped with no comment at all
// (yaml.Decode with no KnownFields), which is how an operator's
// long-carried config.yaml kept naming listener fields that had been
// removed two releases earlier without them ever finding out. Now it
// refuses to start.
func TestUnknownConfigKeyRefusesToStart(t *testing.T) {
	path := writeConfigFile(t, "listen:\n  http: \":8080\"\n  totallyMadeUp: 1\n")

	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("Load accepted a config file with an unknown key")
	}
	for _, want := range []string{"listen.totallyMadeUp", "line 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// A key CHANGELOG.md records as removed or renamed gets its own
// message naming the version and what replaced it, not just "unknown
// field" -- this is the actual defect from the owner's v0.5.1 instance:
// listen.syslogUdp/syslogTcp, carried in config.yaml since before #189
// removed the plaintext syslog listeners, were silently ignored and the
// owner believed syslog ingest was on the old plaintext port.
func TestKnownRemovedKeyGetsItsSpecificMessage(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantKey   string
		wantInMsg []string
	}{
		{
			name:      "listen.syslogUdp",
			src:       "listen:\n  syslogUdp: \":1514\"\n",
			wantKey:   "listen.syslogUdp",
			wantInMsg: []string{"listen.syslogUdp", "v0.2.0", "#189", "syslogTls"},
		},
		{
			name:      "listen.syslogTcp",
			src:       "listen:\n  syslogTcp: \":1514\"\n",
			wantKey:   "listen.syslogTcp",
			wantInMsg: []string{"listen.syslogTcp", "v0.2.0", "#189", "syslogTls"},
		},
		{
			name:      "store.maxEvents",
			src:       "store:\n  maxEvents: 200000\n",
			wantKey:   "store.maxEvents",
			wantInMsg: []string{"store.maxEvents", "v0.2.0", "#244", "maxMemory"},
		},
		{
			name:      "watchlist.storePath",
			src:       "watchlist:\n  storePath: /var/lib/mikroview/watchlist.json\n",
			wantKey:   "watchlist.storePath",
			wantInMsg: []string{"watchlist.storePath", "v0.5.0", "#873"},
		},
		{
			name:      "flags.detectorSettingsStorePath",
			src:       "flags:\n  detectorSettingsStorePath: /var/lib/mikroview/detectors.json\n",
			wantKey:   "flags.detectorSettingsStorePath",
			wantInMsg: []string{"flags.detectorSettingsStorePath", "v0.5.0", "#873"},
		},
		{
			name:      "configDrift.storePath",
			src:       "configDrift:\n  storePath: /var/lib/mikroview/config-drift.json\n",
			wantKey:   "configDrift",
			wantInMsg: []string{"configDrift", "#1277", "#1218"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigFile(t, tc.src)
			_, err := Load(path, nil)
			if err == nil {
				t.Fatalf("Load accepted a config file still naming the removed key %s", tc.wantKey)
			}
			for _, want := range tc.wantInMsg {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

// The whole point is precision, not paranoia: a config file with only
// known, current keys must still load exactly as before.
func TestValidConfigFileStillLoads(t *testing.T) {
	path := writeConfigFile(t, `
listen:
  http: ":8080"
  syslogTls: ":6514"
store:
  retention: 24h
  maxMemory: 120000000
notify:
  webhook:
    url: "https://example.invalid/hook"
    headers:
      X-Token: "abc"
`)

	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load rejected a config file with only known keys: %v", err)
	}
	if cfg.Listen.HTTP != ":8080" {
		t.Errorf("Listen.HTTP = %q, want :8080", cfg.Listen.HTTP)
	}
	if cfg.Listen.SyslogTLS != ":6514" {
		t.Errorf("Listen.SyslogTLS = %q, want :6514", cfg.Listen.SyslogTLS)
	}
	if cfg.Notify.Webhook.URL != "https://example.invalid/hook" {
		t.Errorf("Notify.Webhook.URL = %q, want the configured url", cfg.Notify.Webhook.URL)
	}
}

// -validate-config must report the same refusal without starting --
// LoadWithProblems is the entry point it uses, and a load-time failure
// (as opposed to a Validate-time Problem) is expected to come back as a
// plain error with an empty Result, which is what runValidateConfig
// already falls back to err.Error() for.
func TestValidateConfigReportsUnknownKeyWithoutStarting(t *testing.T) {
	path := writeConfigFile(t, "listen:\n  syslogUdp: \":1514\"\n")

	cfg, result, err := LoadWithProblems(path, nil)
	if err == nil {
		t.Fatal("LoadWithProblems accepted a config file with a removed key")
	}
	if result.HasProblems() {
		t.Errorf("expected no Problems for a load-time failure (Validate never ran), got %+v", result)
	}
	if cfg.Listen.HTTP != "" {
		t.Error("expected a zero Config on failure")
	}
	if !strings.Contains(err.Error(), "listen.syslogUdp") {
		t.Errorf("error %q lacks the offending key", err)
	}
}
