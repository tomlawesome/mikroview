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

func TestListenHTTPRedirectDefaultsAndOverrides(t *testing.T) {
	t.Run("defaults to :8081", func(t *testing.T) {
		cfg, err := Load("", nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTPRedirect != ":8081" {
			t.Errorf("Listen.HTTPRedirect = %q, want %q", cfg.Listen.HTTPRedirect, ":8081")
		}
	})

	t.Run("env overrides default", func(t *testing.T) {
		t.Setenv("MIKROVIEW_LISTEN_HTTP_REDIRECT", ":9081")
		cfg, err := Load("", nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTPRedirect != ":9081" {
			t.Errorf("Listen.HTTPRedirect = %q, want the env value :9081", cfg.Listen.HTTPRedirect)
		}
	})

	t.Run("flag overrides env, empty string disables it", func(t *testing.T) {
		t.Setenv("MIKROVIEW_LISTEN_HTTP_REDIRECT", ":9081")
		cfg, err := Load("", []string{"-http-redirect", ""})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.HTTPRedirect != "" {
			t.Errorf("Listen.HTTPRedirect = %q, want empty (disabled)", cfg.Listen.HTTPRedirect)
		}
	})
}

func TestFlagsEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_FLAGS_STORE_PATH", "/data/flags.json")
	t.Setenv("MIKROVIEW_FLAGS_PORT_SCAN_THRESHOLD", "30")
	t.Setenv("MIKROVIEW_FLAGS_PORT_SCAN_WINDOW", "2m")
	t.Setenv("MIKROVIEW_FLAGS_ACTIVITY_SPIKE_THRESHOLD", "500")
	t.Setenv("MIKROVIEW_FLAGS_ACTIVITY_SPIKE_WINDOW", "30s")
	t.Setenv("MIKROVIEW_FLAGS_CRITICAL_PORTS", "22, 3389,  8291")
	t.Setenv("MIKROVIEW_FLAGS_CRITICAL_PORT_THRESHOLD", "10")
	t.Setenv("MIKROVIEW_FLAGS_CRITICAL_PORT_WINDOW", "10m")
	t.Setenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_MULTIPLIER", "6.5")
	t.Setenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_MIN_EPS", "2.5")
	t.Setenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_WARMUP_SAMPLES", "50")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Flags{
		StorePath:                "/data/flags.json",
		PortScanThreshold:        30,
		PortScanWindow:           2 * time.Minute,
		ActivitySpikeThreshold:   500,
		ActivitySpikeWindow:      30 * time.Second,
		CriticalPorts:            []int{22, 3389, 8291},
		CriticalPortThreshold:    10,
		CriticalPortWindow:       10 * time.Minute,
		GlobalSpikeMultiplier:    6.5,
		GlobalSpikeMinEPS:        2.5,
		GlobalSpikeWarmupSamples: 50,
	}
	if cfg.Flags.StorePath != want.StorePath ||
		cfg.Flags.PortScanThreshold != want.PortScanThreshold ||
		cfg.Flags.PortScanWindow != want.PortScanWindow ||
		cfg.Flags.ActivitySpikeThreshold != want.ActivitySpikeThreshold ||
		cfg.Flags.ActivitySpikeWindow != want.ActivitySpikeWindow ||
		len(cfg.Flags.CriticalPorts) != len(want.CriticalPorts) ||
		cfg.Flags.CriticalPortThreshold != want.CriticalPortThreshold ||
		cfg.Flags.CriticalPortWindow != want.CriticalPortWindow ||
		cfg.Flags.GlobalSpikeMultiplier != want.GlobalSpikeMultiplier ||
		cfg.Flags.GlobalSpikeMinEPS != want.GlobalSpikeMinEPS ||
		cfg.Flags.GlobalSpikeWarmupSamples != want.GlobalSpikeWarmupSamples {
		t.Errorf("Flags = %+v, want %+v", cfg.Flags, want)
	}
	for i, p := range want.CriticalPorts {
		if cfg.Flags.CriticalPorts[i] != p {
			t.Errorf("CriticalPorts[%d] = %d, want %d", i, cfg.Flags.CriticalPorts[i], p)
		}
	}
}

func TestHostActivityEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_FLAGS_HOST_ACTIVITY_MULTIPLIER", "4.5")
	t.Setenv("MIKROVIEW_FLAGS_HOST_ACTIVITY_WARMUP_SAMPLES", "40")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Flags.HostActivityMultiplier != 4.5 {
		t.Errorf("HostActivityMultiplier = %v, want 4.5", cfg.Flags.HostActivityMultiplier)
	}
	if cfg.Flags.HostActivityWarmupSamples != 40 {
		t.Errorf("HostActivityWarmupSamples = %v, want 40", cfg.Flags.HostActivityWarmupSamples)
	}
}

func TestRuleSpikeWarmupSamplesEnvVarOverridesDefault(t *testing.T) {
	t.Setenv("MIKROVIEW_FLAGS_RULE_SPIKE_WARMUP_SAMPLES", "35")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Flags.RuleSpikeWarmupSamples != 35 {
		t.Errorf("RuleSpikeWarmupSamples = %v, want 35", cfg.Flags.RuleSpikeWarmupSamples)
	}
}

func TestLowSlowScanEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_WINDOW", "90m")
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_PORT_THRESHOLD", "12")
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_HOST_THRESHOLD", "7")
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_MIN_OBSERVATION", "20m")
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_DROP_RATIO", "0.6")
	t.Setenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_BASELINE_MULTIPLIER", "4.5")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Flags.LowSlowScanWindow != 90*time.Minute {
		t.Errorf("LowSlowScanWindow = %v, want 90m", cfg.Flags.LowSlowScanWindow)
	}
	if cfg.Flags.LowSlowScanPortThreshold != 12 {
		t.Errorf("LowSlowScanPortThreshold = %v, want 12", cfg.Flags.LowSlowScanPortThreshold)
	}
	if cfg.Flags.LowSlowScanHostThreshold != 7 {
		t.Errorf("LowSlowScanHostThreshold = %v, want 7", cfg.Flags.LowSlowScanHostThreshold)
	}
	if cfg.Flags.LowSlowScanMinObservation != 20*time.Minute {
		t.Errorf("LowSlowScanMinObservation = %v, want 20m", cfg.Flags.LowSlowScanMinObservation)
	}
	if cfg.Flags.LowSlowScanDropRatio != 0.6 {
		t.Errorf("LowSlowScanDropRatio = %v, want 0.6", cfg.Flags.LowSlowScanDropRatio)
	}
	if cfg.Flags.LowSlowScanBaselineMultiplier != 4.5 {
		t.Errorf("LowSlowScanBaselineMultiplier = %v, want 4.5", cfg.Flags.LowSlowScanBaselineMultiplier)
	}
}

func TestNotifySMTPEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_NOTIFY_BATCH_WINDOW", "15s")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_HOST", "smtp.example.com")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_PORT", "587")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_USERNAME", "alerts")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_PASSWORD", "hunter2")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_TLS_MODE", "starttls")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_FROM", "mikroview@example.com")
	t.Setenv("MIKROVIEW_NOTIFY_SMTP_TO", "ops@example.com, oncall@example.com")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notify.BatchWindow != 15*time.Second {
		t.Errorf("BatchWindow = %v, want 15s", cfg.Notify.BatchWindow)
	}
	if cfg.Notify.SMTP.Host != "smtp.example.com" {
		t.Errorf("SMTP.Host = %v, want smtp.example.com", cfg.Notify.SMTP.Host)
	}
	if cfg.Notify.SMTP.Port != 587 {
		t.Errorf("SMTP.Port = %v, want 587", cfg.Notify.SMTP.Port)
	}
	if cfg.Notify.SMTP.Username != "alerts" {
		t.Errorf("SMTP.Username = %v, want alerts", cfg.Notify.SMTP.Username)
	}
	if cfg.Notify.SMTP.Password != "hunter2" {
		t.Errorf("SMTP.Password = %v, want hunter2", cfg.Notify.SMTP.Password)
	}
	if cfg.Notify.SMTP.TLSMode != "starttls" {
		t.Errorf("SMTP.TLSMode = %v, want starttls", cfg.Notify.SMTP.TLSMode)
	}
	if cfg.Notify.SMTP.From != "mikroview@example.com" {
		t.Errorf("SMTP.From = %v, want mikroview@example.com", cfg.Notify.SMTP.From)
	}
	wantTo := []string{"ops@example.com", "oncall@example.com"}
	if len(cfg.Notify.SMTP.To) != len(wantTo) || cfg.Notify.SMTP.To[0] != wantTo[0] || cfg.Notify.SMTP.To[1] != wantTo[1] {
		t.Errorf("SMTP.To = %v, want %v", cfg.Notify.SMTP.To, wantTo)
	}
}

func TestNotifyPushoverEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_NOTIFY_PUSHOVER_TOKEN", "tok123")
	t.Setenv("MIKROVIEW_NOTIFY_PUSHOVER_USER", "usr456")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notify.Pushover.Token != "tok123" {
		t.Errorf("Pushover.Token = %v, want tok123", cfg.Notify.Pushover.Token)
	}
	if cfg.Notify.Pushover.User != "usr456" {
		t.Errorf("Pushover.User = %v, want usr456", cfg.Notify.Pushover.User)
	}
}

func TestTLSDefaultsToEnabledWithSecureCookie(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled to default to true")
	}
	if !cfg.Auth.SecureCookie {
		t.Error("expected Auth.SecureCookie to default to true, matching TLS being on by default")
	}
}

func TestAuthTokensStorePathEnvOverrides(t *testing.T) {
	t.Setenv("MIKROVIEW_AUTH_TOKENS_STORE_PATH", "/custom/tokens.json")
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.TokensStorePath != "/custom/tokens.json" {
		t.Errorf("Auth.TokensStorePath = %q, want the env override %q", cfg.Auth.TokensStorePath, "/custom/tokens.json")
	}
}

func TestLogLevelDefaultsToInfoAndEnvOverrides(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want the default \"info\"", cfg.Log.Level)
	}

	t.Setenv("MIKROVIEW_LOG_LEVEL", "debug")
	cfg, err = Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want the env override \"debug\"", cfg.Log.Level)
	}
}

// TestDefaultStoragePathsUnderVarLibMikroview confirms every persistence
// path is writable out of the box (see the Dockerfile creating/owning
// /var/lib/mikroview) rather than silently no-op-ing until an operator
// configures a storePath -- the auth.storePath case in particular used
// to mean the web setup form's only path (create an account) failed at
// submission with ErrNotPersisted.
func TestDefaultStoragePathsUnderVarLibMikroview(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"Flags.StorePath":                 cfg.Flags.StorePath,
		"Flags.DetectorSettingsStorePath": cfg.Flags.DetectorSettingsStorePath,
		"Auth.StorePath":                  cfg.Auth.StorePath,
		"Auth.TokensStorePath":            cfg.Auth.TokensStorePath,
		"TLS.StorePath":                   cfg.TLS.StorePath,
	}
	want := map[string]string{
		"Flags.StorePath":                 "/var/lib/mikroview/flags.json",
		"Flags.DetectorSettingsStorePath": "/var/lib/mikroview/detector-settings.json",
		"Auth.StorePath":                  "/var/lib/mikroview/users.json",
		"Auth.TokensStorePath":            "/var/lib/mikroview/tokens.json",
		"TLS.StorePath":                   "/var/lib/mikroview/tls",
	}
	for field, got := range cases {
		if got != want[field] {
			t.Errorf("%s = %q, want %q", field, got, want[field])
		}
	}
}

func TestTLSEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_TLS_ENABLED", "false")
	t.Setenv("MIKROVIEW_TLS_CERT_FILE", "/etc/mikroview/tls.crt")
	t.Setenv("MIKROVIEW_TLS_KEY_FILE", "/etc/mikroview/tls.key")
	t.Setenv("MIKROVIEW_TLS_HOSTS", "mikroview.local, 192.168.1.50")
	t.Setenv("MIKROVIEW_TLS_STORE_PATH", "/var/lib/mikroview/tls")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.Enabled {
		t.Error("expected TLS.Enabled = false")
	}
	if cfg.TLS.CertFile != "/etc/mikroview/tls.crt" {
		t.Errorf("TLS.CertFile = %v, want /etc/mikroview/tls.crt", cfg.TLS.CertFile)
	}
	if cfg.TLS.KeyFile != "/etc/mikroview/tls.key" {
		t.Errorf("TLS.KeyFile = %v, want /etc/mikroview/tls.key", cfg.TLS.KeyFile)
	}
	wantHosts := []string{"mikroview.local", "192.168.1.50"}
	if len(cfg.TLS.Hosts) != len(wantHosts) || cfg.TLS.Hosts[0] != wantHosts[0] || cfg.TLS.Hosts[1] != wantHosts[1] {
		t.Errorf("TLS.Hosts = %v, want %v", cfg.TLS.Hosts, wantHosts)
	}
	if cfg.TLS.StorePath != "/var/lib/mikroview/tls" {
		t.Errorf("TLS.StorePath = %v, want /var/lib/mikroview/tls", cfg.TLS.StorePath)
	}
}

func TestOIDCDefaultsToDisabled(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDC.IssuerURL != "" {
		t.Errorf("OIDC.IssuerURL = %q, want empty (OIDC not configured by default)", cfg.OIDC.IssuerURL)
	}
}

func TestOIDCEnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("MIKROVIEW_OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv("MIKROVIEW_OIDC_CLIENT_ID", "mikroview")
	t.Setenv("MIKROVIEW_OIDC_CLIENT_SECRET", "s3cret")
	t.Setenv("MIKROVIEW_OIDC_PUBLIC_BASE_URL", "https://mikroview.example.com")
	t.Setenv("MIKROVIEW_OIDC_SCOPES", "openid, profile, email, groups")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDC.IssuerURL != "https://idp.example" {
		t.Errorf("OIDC.IssuerURL = %q, want https://idp.example", cfg.OIDC.IssuerURL)
	}
	if cfg.OIDC.ClientID != "mikroview" {
		t.Errorf("OIDC.ClientID = %q, want mikroview", cfg.OIDC.ClientID)
	}
	if cfg.OIDC.ClientSecret != "s3cret" {
		t.Errorf("OIDC.ClientSecret = %q, want s3cret", cfg.OIDC.ClientSecret)
	}
	if cfg.OIDC.PublicBaseURL != "https://mikroview.example.com" {
		t.Errorf("OIDC.PublicBaseURL = %q, want https://mikroview.example.com", cfg.OIDC.PublicBaseURL)
	}
	wantScopes := []string{"openid", "profile", "email", "groups"}
	if len(cfg.OIDC.Scopes) != len(wantScopes) {
		t.Fatalf("OIDC.Scopes = %v, want %v", cfg.OIDC.Scopes, wantScopes)
	}
	for i, s := range wantScopes {
		if cfg.OIDC.Scopes[i] != s {
			t.Errorf("OIDC.Scopes[%d] = %q, want %q", i, cfg.OIDC.Scopes[i], s)
		}
	}
}

func TestOIDCYAMLOverridesDefaultsAndFlagsIsNotRequired(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(yamlPath, []byte(`
oidc:
  issuerUrl: "https://idp.example"
  clientId: "mikroview"
  publicBaseUrl: "https://mikroview.example.com"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(yamlPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDC.IssuerURL != "https://idp.example" || cfg.OIDC.ClientID != "mikroview" || cfg.OIDC.PublicBaseURL != "https://mikroview.example.com" {
		t.Errorf("OIDC from yaml = %+v, missing an expected field", cfg.OIDC)
	}
	// clientSecret wasn't set in the yaml above and no env var is set in
	// this test -- confirms nothing else silently fills it in.
	if cfg.OIDC.ClientSecret != "" {
		t.Errorf("OIDC.ClientSecret = %q, want empty", cfg.OIDC.ClientSecret)
	}
}

func TestFlagsCriticalPortsMalformedEntryIgnoresWholeValue(t *testing.T) {
	t.Setenv("MIKROVIEW_FLAGS_CRITICAL_PORTS", "22,not-a-port,3389")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := defaults().Flags.CriticalPorts
	if len(cfg.Flags.CriticalPorts) != len(want) {
		t.Fatalf("expected the malformed env value to be ignored entirely, falling back to defaults; got %v, want %v", cfg.Flags.CriticalPorts, want)
	}
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
