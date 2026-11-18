// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #1542 (C13): the option flag inside a mode is `-force`, parsed by the
// flag package like every other flag, and `--force` keeps working because
// the flag package accepts both spellings. The three CLI entry points
// that took the hand-parsed `--force` are pinned here, once per spelling
// and per argument order where the order matters.

func force1542Env(t *testing.T) (authPath string) {
	t.Helper()
	dir := t.TempDir()
	authPath = filepath.Join(dir, "users.json")
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)
	return authPath
}

func TestBackupAcceptsSingleDashForce1542(t *testing.T) {
	authPath := force1542Env(t)
	if err := os.WriteFile(authPath, []byte(`{"users":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-force"}, {"--force"}} {
		dest := filepath.Join(t.TempDir(), "mikroview.backup")
		if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		a := append([]string{dest}, args...)
		if code := runBackup(a); code != 0 {
			t.Errorf("runBackup(%v) over an existing file = %d, want 0", a, code)
		}
		a = append(append([]string{}, args...), dest)
		if code := runBackup(a); code != 0 {
			t.Errorf("runBackup(%v) over an existing file = %d, want 0", a, code)
		}
	}
}

func TestRestoreAcceptsSingleDashForce1542(t *testing.T) {
	authPath := force1542Env(t)
	backupPath := filepath.Join(t.TempDir(), "mikroview.backup")
	if err := writeBackup(backupPath, true, map[string][]byte{"auth": []byte(`{"users":[]}`)}); err != nil {
		t.Fatalf("writeBackup: %v", err)
	}
	for _, args := range [][]string{{backupPath, "-force"}, {"-force", backupPath}, {backupPath, "--force"}} {
		if err := os.WriteFile(authPath, []byte("{not valid json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := runRestore(args); code != 0 {
			t.Errorf("runRestore(%v) over an existing store = %d, want 0", args, code)
		}
	}
}

func TestMigrateDataAcceptsSingleDashForce1542(t *testing.T) {
	src, _ := seedDataDir(t)
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(src, "users.json"))
	t.Setenv("MIKROVIEW_TLS_STORE_PATH", filepath.Join(src, "tls"))

	// A destination that is not empty (an empty tls/ left by an earlier
	// attempt) is refused without the flag; with it the move goes ahead.
	for _, flagArg := range []string{"-force", "--force"} {
		dest := t.TempDir()
		if err := os.Mkdir(filepath.Join(dest, "tls"), 0o700); err != nil {
			t.Fatal(err)
		}
		if code := runMigrateData([]string{dest, flagArg}); code != 0 {
			t.Errorf("runMigrateData(dest, %s) into a populated destination = %d, want 0", flagArg, code)
		}
	}
}

// The docs name the one spelling: `-force`, never the hand-parsed `--force`.
func TestDocsSpellForceWithOneDash1542(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("docs", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	if n := strings.Count(doc, "--force"); n != 0 {
		t.Errorf("docs/configuration.md still says --force %d time(s); the decision is -force", n)
	}
	if !strings.Contains(doc, "`-force`") {
		t.Error("docs/configuration.md never names `-force`")
	}
}

func TestForceFlagSpellings1542(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"dest", "-force"}, true},
		{[]string{"dest", "--force"}, true},
		{[]string{"dest", "-force=true"}, true},
		{[]string{"dest", "-force=false"}, false},
		{[]string{"dest", "-x"}, false},
		{[]string{"dest"}, false},
	} {
		if got := forceFlag(c.args); got != c.want {
			t.Errorf("forceFlag(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
