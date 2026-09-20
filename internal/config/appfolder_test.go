// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useAppFolder points the loader at a temp folder for the duration of
// one test and returns it. Every test here needs it: the real
// /etc/mikroview belongs to a deployment, and a test that read it would
// pass or fail depending on the machine it ran on.
func useAppFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := appFolderRoot
	appFolderRoot = dir
	t.Cleanup(func() { appFolderRoot = previous })
	return dir
}

// writeAppFolderFile puts a file in the folder at the layout path the
// ruling fixes, creating the keys/ or certs/ directory as needed.
func writeAppFolderFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// clearAppFolderEnv keeps a developer's own environment out of the
// tests below, which assert on what the folder did and nothing else.
func clearAppFolderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"MIKROVIEW_GEOIP_DB_PATH",
		"MIKROVIEW_HISTORY_KEY_FILE",
		"MIKROVIEW_TLS_CERT_FILE",
		"MIKROVIEW_TLS_KEY_FILE",
	} {
		t.Setenv(name, "")
	}
}

// The bare `docker run` case: the folder is mounted, config.yaml is in
// it, and nothing sets MIKROVIEW_CONFIG.
func TestAppFolderConfigIsTheBuiltInDefault(t *testing.T) {
	root := useAppFolder(t)
	clearAppFolderEnv(t)
	writeAppFolderFile(t, root, appFolderConfigFile, "log:\n  level: debug\n")

	cfg, result, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug -- the app folder's config.yaml was not read", cfg.Log.Level)
	}
	if want := filepath.Join(root, appFolderConfigFile); result.ConfigPath != want {
		t.Errorf("ConfigPath = %q, want %q", result.ConfigPath, want)
	}
}

// The same container with an empty folder starts and runs on defaults.
func TestMissingAppFolderConfigIsNotAnError(t *testing.T) {
	useAppFolder(t)
	clearAppFolderEnv(t)

	cfg, result, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("an absent %s must not stop MikroView starting: %v", appFolderConfigFile, err)
	}
	if cfg.Listen.SyslogTLS == "" {
		t.Error("defaults were not applied")
	}
	if result.ConfigPath != "" {
		t.Errorf("ConfigPath = %q, want empty -- no config file was read", result.ConfigPath)
	}
}

// The distinction the default must not blur: a path somebody named and
// got wrong is still a startup failure.
func TestNamedConfigThatIsMissingIsStillAnError(t *testing.T) {
	useAppFolder(t)
	clearAppFolderEnv(t)

	named := filepath.Join(t.TempDir(), "not-there.yaml")
	if _, _, err := LoadWithProblems(named, nil); err == nil {
		t.Fatal("a config file named explicitly and missing loaded without error -- the operator's typo would run on defaults in silence")
	}
}

func TestAppFolderFillsGeoIPHistoryKeyAndTLSPair(t *testing.T) {
	root := useAppFolder(t)
	clearAppFolderEnv(t)
	geo := writeAppFolderFile(t, root, appFolderGeoIPFile, "not really a database")
	key := writeAppFolderFile(t, root, appFolderHistoryKey, strings.Repeat("k", 44))
	cert := writeAppFolderFile(t, root, appFolderTLSCertFile, "cert")
	tlsKey := writeAppFolderFile(t, root, appFolderTLSKeyFile, "key")

	cfg, _, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GeoIP.DBPath != geo {
		t.Errorf("geoip.dbPath = %q, want %q", cfg.GeoIP.DBPath, geo)
	}
	if cfg.History.KeyFile != key {
		t.Errorf("history.keyFile = %q, want %q", cfg.History.KeyFile, key)
	}
	if cfg.TLS.CertFile != cert || cfg.TLS.KeyFile != tlsKey {
		t.Errorf("tls.certFile/keyFile = %q/%q, want %q/%q", cfg.TLS.CertFile, cfg.TLS.KeyFile, cert, tlsKey)
	}
}

// Nothing in the folder leaves every one of those settings exactly as
// it was: the empty-folder container runs on defaults.
func TestEmptyAppFolderChangesNothing(t *testing.T) {
	useAppFolder(t)
	clearAppFolderEnv(t)

	cfg, _, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GeoIP.DBPath != "" || cfg.History.KeyFile != "" || cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		t.Errorf("an empty folder set paths anyway: geoip=%q history=%q cert=%q key=%q",
			cfg.GeoIP.DBPath, cfg.History.KeyFile, cfg.TLS.CertFile, cfg.TLS.KeyFile)
	}
}

// One file of the TLS pair is somebody part-way through installing
// their own certificate. Starting on the self-signed one instead would
// be the failure they notice last, so it is a startup error -- and it
// has to name the file that is missing, not the one that is there.
func TestOneHalfOfTheTLSPairIsAStartupError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present string
		missing string
	}{
		{"certificate without key", appFolderTLSCertFile, appFolderTLSKeyFile},
		{"key without certificate", appFolderTLSKeyFile, appFolderTLSCertFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := useAppFolder(t)
			clearAppFolderEnv(t)
			writeAppFolderFile(t, root, tc.present, "x")

			_, _, err := LoadWithProblems("", nil)
			if err == nil {
				t.Fatal("half a certificate pair started anyway, on a certificate the operator did not choose")
			}
			if want := filepath.Join(root, tc.missing); !strings.Contains(err.Error(), want) {
				t.Errorf("error does not name the missing file %q: %v", want, err)
			}
		})
	}
}

// A value set in config wins over the folder, per file. This is what
// keeps every install that already names its paths working unchanged.
func TestConfigValueWinsOverTheAppFolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		folder string
		yaml   string
		want   func(Config) string
	}{
		{"geoip.dbPath", appFolderGeoIPFile, "geoip:\n  dbPath: %s\n", func(c Config) string { return c.GeoIP.DBPath }},
		{"history.keyFile", appFolderHistoryKey, "history:\n  keyFile: %s\n", func(c Config) string { return c.History.KeyFile }},
		{"tls.certFile", appFolderTLSCertFile, "tls:\n  certFile: %s\n  keyFile: /own/tls.key\n", func(c Config) string { return c.TLS.CertFile }},
		{"tls.keyFile", appFolderTLSKeyFile, "tls:\n  certFile: /own/tls.crt\n  keyFile: %s\n", func(c Config) string { return c.TLS.KeyFile }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := useAppFolder(t)
			clearAppFolderEnv(t)
			writeAppFolderFile(t, root, tc.folder, "from the folder")
			chosen := filepath.Join(t.TempDir(), "chosen")
			writeAppFolderFile(t, root, appFolderConfigFile, strings.Replace(tc.yaml, "%s", chosen, 1))

			cfg, _, err := LoadWithProblems("", nil)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tc.want(cfg); got != chosen {
				t.Errorf("%s = %q, want the configured %q -- the folder overrode a value the operator set", tc.name, got, chosen)
			}
		})
	}
}

// And an environment value wins too, per file.
func TestEnvValueWinsOverTheAppFolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		folder string
		env    string
		others map[string]string
		want   func(Config) string
	}{
		{"geoip.dbPath", appFolderGeoIPFile, "MIKROVIEW_GEOIP_DB_PATH", nil, func(c Config) string { return c.GeoIP.DBPath }},
		{"history.keyFile", appFolderHistoryKey, "MIKROVIEW_HISTORY_KEY_FILE", nil, func(c Config) string { return c.History.KeyFile }},
		{"tls.certFile", appFolderTLSCertFile, "MIKROVIEW_TLS_CERT_FILE",
			map[string]string{"MIKROVIEW_TLS_KEY_FILE": "/own/tls.key"}, func(c Config) string { return c.TLS.CertFile }},
		{"tls.keyFile", appFolderTLSKeyFile, "MIKROVIEW_TLS_KEY_FILE",
			map[string]string{"MIKROVIEW_TLS_CERT_FILE": "/own/tls.crt"}, func(c Config) string { return c.TLS.KeyFile }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := useAppFolder(t)
			clearAppFolderEnv(t)
			writeAppFolderFile(t, root, tc.folder, "from the folder")
			chosen := filepath.Join(t.TempDir(), "chosen")
			t.Setenv(tc.env, chosen)
			for name, value := range tc.others {
				t.Setenv(name, value)
			}

			cfg, _, err := LoadWithProblems("", nil)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tc.want(cfg); got != chosen {
				t.Errorf("%s = %q, want the environment's %q -- the folder overrode a value the environment set", tc.name, got, chosen)
			}
		})
	}
}

// The boot lines: one per optional file looked for, saying found or not
// found and where. A file somebody already named is not looked for and
// says nothing.
func TestAppFolderLookupsAreRecordedForTheBootLog(t *testing.T) {
	root := useAppFolder(t)
	clearAppFolderEnv(t)
	writeAppFolderFile(t, root, appFolderGeoIPFile, "db")

	_, result, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byKey := map[string]AppFolderLookup{}
	for _, l := range result.AppFolder {
		byKey[l.Key] = l
	}
	for _, key := range []string{"config file", "geoip.dbPath", "history.keyFile", "tls.certFile", "tls.keyFile"} {
		l, ok := byKey[key]
		if !ok {
			t.Errorf("no boot line for %s -- the operator cannot tell whether MikroView looked", key)
			continue
		}
		if !strings.Contains(l.String(), l.Path) {
			t.Errorf("%s says %q, which does not name the path looked at", key, l.String())
		}
	}
	if !byKey["geoip.dbPath"].Found {
		t.Error("the GeoIP database was there and the line says it was not")
	}
	if byKey["history.keyFile"].Found {
		t.Error("no key was there and the line says one was")
	}

	t.Setenv("MIKROVIEW_GEOIP_DB_PATH", filepath.Join(root, appFolderGeoIPFile))
	_, result, err = LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, l := range result.AppFolder {
		if l.Key == "geoip.dbPath" {
			t.Error("the folder was consulted for a setting the environment already named")
		}
	}
}

// A directory where a file should be -- what docker creates when the
// host path in a bind mount does not exist -- is not the file.
func TestADirectoryIsNotAnAppFolderFile(t *testing.T) {
	root := useAppFolder(t)
	clearAppFolderEnv(t)
	if err := os.MkdirAll(filepath.Join(root, appFolderGeoIPFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg, _, err := LoadWithProblems("", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GeoIP.DBPath != "" {
		t.Errorf("geoip.dbPath = %q, want empty -- a directory was taken for a database", cfg.GeoIP.DBPath)
	}
}
