// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

func cfgWithData(dir string) config.Config {
	var c config.Config
	c.Auth.StorePath = filepath.Join(dir, "users.json")
	return c
}

// Moving to Postgres is a one-way choice. These pin that it is enforced
// rather than merely documented -- a rule that lives only in prose is
// one hurried compose edit away from not applying.
func TestPostgresAdoptionIsRecordedAndBlocksAJSONStart(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithData(dir)

	// Before adoption, a JSON start is entirely normal.
	if err := refuseIfPostgresAdopted(cfg); err != nil {
		t.Fatalf("refused a deployment that never used Postgres: %v", err)
	}

	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatalf("markPostgresAdopted: %v", err)
	}

	err := refuseIfPostgresAdopted(cfg)
	if err == nil {
		t.Fatal("started on the JSON files after Postgres adoption -- stale or empty accounts would be served")
	}
	// The operator has to be able to act on this without reading source.
	for _, want := range []string{"one-way", "postgres.dsnFile", dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal message lacks %q: %s", want, err)
		}
	}
}

func TestPostgresAdoptionIsIdempotent(t *testing.T) {
	cfg := cfgWithData(t.TempDir())
	for i := 0; i < 3; i++ {
		if err := markPostgresAdopted(cfg); err != nil {
			t.Fatalf("mark %d: %v", i, err)
		}
	}
}

// The marker holds no secrets, but it sits in the same directory as the
// accounts file and inherits that directory's expectations.
func TestPostgresMarkerIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithData(dir)
	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(markerPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("marker mode is %v, want no group/other access", fi.Mode().Perm())
	}
}

// It has to explain itself to whoever finds it, since the person who
// hits this is usually mid-incident and not the person who set it up.
func TestPostgresMarkerExplainsItself(t *testing.T) {
	cfg := cfgWithData(t.TempDir())
	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(markerPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"one-way", "Postgres", "Back up the database"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("marker file does not mention %q:\n%s", want, body)
		}
	}
}

// With no auth.storePath configured, the marker still has to land
// somewhere predictable rather than the process's working directory.
func TestPostgresMarkerFallsBackToTheDefaultDataDir(t *testing.T) {
	var cfg config.Config
	if got, want := markerPath(cfg), filepath.Join(config.DefaultDataDir, postgresAdoptedMarker); got != want {
		t.Errorf("markerPath = %q, want %q", got, want)
	}
}
