package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(yamlPath, []byte(`
listen:
  http: ":9000"
store:
  retention: 12h
  maxEvents: 5000
devices:
  - id: core
    name: "Core Router"
    sourceIp: 192.168.1.1
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("defaults only", func(t *testing.T) {
		cfg, err := Load("", nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTP != ":8080" || cfg.Store.MaxEvents != 200_000 {
			t.Errorf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("yaml overrides defaults", func(t *testing.T) {
		cfg, err := Load(yamlPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTP != ":9000" || cfg.Store.Retention != 12*time.Hour || cfg.Store.MaxEvents != 5000 {
			t.Errorf("yaml did not override: %+v", cfg)
		}
		if len(cfg.Devices) != 1 || cfg.Devices[0].SourceIP != "192.168.1.1" {
			t.Errorf("devices not loaded: %+v", cfg.Devices)
		}
	})

	t.Run("env overrides yaml", func(t *testing.T) {
		t.Setenv("MIKROVIEW_LISTEN_HTTP", ":7000")
		cfg, err := Load(yamlPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTP != ":7000" {
			t.Errorf("env did not override yaml: %+v", cfg)
		}
	})

	t.Run("flags override env", func(t *testing.T) {
		t.Setenv("MIKROVIEW_LISTEN_HTTP", ":7000")
		cfg, err := Load(yamlPath, []string{"-http", ":6000"})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTP != ":6000" {
			t.Errorf("flag did not override env: %+v", cfg)
		}
	})

	t.Run("geoip and reputation env vars", func(t *testing.T) {
		t.Setenv("MIKROVIEW_GEOIP_DB_PATH", "/data/GeoLite2-Country.mmdb")
		t.Setenv("MIKROVIEW_ABUSEIPDB_KEY", "test-key")
		cfg, err := Load("", nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.GeoIP.DBPath != "/data/GeoLite2-Country.mmdb" {
			t.Errorf("GeoIP.DBPath = %q, want the env value", cfg.GeoIP.DBPath)
		}
		if cfg.Reputation.AbuseIPDBKey != "test-key" {
			t.Errorf("Reputation.AbuseIPDBKey = %q, want the env value", cfg.Reputation.AbuseIPDBKey)
		}
	})
}

func TestLoadMissingConfigFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"), nil)
	if err == nil {
		t.Fatal("expected an error for a config path that doesn't exist, got nil")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Mismatched indentation/invalid YAML, not just an unknown field.
	if err := os.WriteFile(path, []byte("listen:\n  http: :8080\n foo:\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("expected an error for malformed YAML, got nil")
	}
}

// applyEnv (config.go:107-133) intentionally swallows a malformed
// MIKROVIEW_STORE_RETENTION/MIKROVIEW_STORE_MAX_EVENTS value rather than
// failing Load -- an operator's typo in one env var shouldn't stop the
// whole process from starting. This locks in that documented behavior:
// the malformed value is ignored and the prior (default/YAML) value wins.
func TestLoadInvalidEnvValuesFallBackSilently(t *testing.T) {
	t.Setenv("MIKROVIEW_STORE_RETENTION", "not-a-duration")
	t.Setenv("MIKROVIEW_STORE_MAX_EVENTS", "not-a-number")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatalf("Load returned an error for an invalid env var, want it silently ignored: %v", err)
	}
	want := defaults()
	if cfg.Store.Retention != want.Store.Retention {
		t.Errorf("Store.Retention = %v, want the default %v (invalid env value should be ignored)", cfg.Store.Retention, want.Store.Retention)
	}
	if cfg.Store.MaxEvents != want.Store.MaxEvents {
		t.Errorf("Store.MaxEvents = %v, want the default %v (invalid env value should be ignored)", cfg.Store.MaxEvents, want.Store.MaxEvents)
	}
}

func TestLoadInvalidFlag(t *testing.T) {
	_, err := Load("", []string{"-not-a-real-flag"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized flag, got nil")
	}
}
