// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

func TestCheckStoresUsableAcceptsAWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{}
	cfg.Auth.StorePath = filepath.Join(dir, "accounts.json")
	cfg.Entities.StorePath = filepath.Join(dir, "entities.json")

	if err := checkStoresUsable(cfg); err != nil {
		t.Fatalf("a writable directory must pass, got: %v", err)
	}
}

// The case the operator actually hits: a bind mount owned by the host
// user, which uid 65532 inside the container cannot write.
func TestCheckStoresUsableRefusesAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test depends on")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { // read+execute, no write
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	cfg := config.Config{}
	cfg.Auth.StorePath = filepath.Join(dir, "accounts.json")

	err := checkStoresUsable(cfg)
	if err == nil {
		t.Fatal("expected an unwritable store directory to stop startup, got nil")
	}
	if !strings.Contains(err.Error(), cfg.Auth.StorePath) {
		t.Errorf("the refusal must name the path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("the refusal must name which store, got: %v", err)
	}
}

// An existing file mikroview cannot open is the same failure as an
// unwritable directory, and used to be discovered only at the first
// write -- long after startup reported success.
func TestCheckStoresUsableRefusesAnUnreadableExistingFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test depends on")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	cfg := config.Config{}
	cfg.Auth.StorePath = path

	if err := checkStoresUsable(cfg); err == nil {
		t.Fatal("expected an unreadable existing store file to stop startup, got nil")
	}
}

// Leaving a path unset is a supported choice, not a fault -- the store
// simply does not persist. Refusing to start on one would override the
// operator's configuration, which is the opposite of this check's point.
func TestCheckStoresUsableIgnoresUnsetPaths(t *testing.T) {
	if err := checkStoresUsable(config.Config{}); err != nil {
		t.Fatalf("a configuration with no store paths must pass, got: %v", err)
	}
}

// On Postgres the file paths are unused, so their permissions say
// nothing about whether the deployment works.
func TestCheckStoresUsableSkipsPostgresDeployments(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test depends on")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	cfg := config.Config{}
	cfg.Auth.StorePath = filepath.Join(dir, "accounts.json")
	cfg.Postgres.DSNFile = "/run/secrets/pg-dsn"

	if err := checkStoresUsable(cfg); err != nil {
		t.Fatalf("a Postgres deployment must not be blocked by file permissions, got: %v", err)
	}
}
