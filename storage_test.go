// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/persist"
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

// The one-time JSON adoption must run exactly once (#294 item 1).
//
// The failure it prevents: restore the database to a snapshot from
// before a store was populated, with the original JSON still sitting on
// the data volume, and mikroview copied it straight back in -- deleted
// accounts and their password hashes returned, with an Info log line as
// the only signal.
//
// Tested through backendFor's own gate rather than against a live
// Postgres, because the decision being pinned is "may this boot adopt at
// all", which is made before any backend is touched.
func TestAdoptionIsRefusedOnceThisDeploymentHasAdopted(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithData(dir)

	if postgresAlreadyAdopted(cfg) {
		t.Fatal("a fresh data directory reports a previous adoption")
	}

	// The boot that moves this deployment onto Postgres.
	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatalf("markPostgresAdopted: %v", err)
	}

	// Every boot after it, including one where the database has been
	// rolled back to before the store existed.
	if !postgresAlreadyAdopted(cfg) {
		t.Error("a deployment that has run on Postgres does not report it, so adoption would run again")
	}
}

// The marker has to sit beside the JSON files, not in the database.
//
// #294 suggested recording the adoption in the database so a rollback
// would take the marker with it. That is the wrong way round, and this
// pins why: a rollback deep enough to lose the data is deep enough to
// lose a marker stored alongside it, and mikroview would re-adopt
// exactly as before. A guard has to survive the thing it guards against.
func TestAdoptionMarkerLivesBesideTheFilesItGuards(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithData(dir)
	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatalf("markPostgresAdopted: %v", err)
	}

	if got, want := filepath.Dir(markerPath(cfg)), filepath.Dir(cfg.Auth.StorePath); got != want {
		t.Errorf("marker is in %q, want it beside the stores in %q -- anywhere the database can roll back is useless here", got, want)
	}
}

// #853 rule 6 (owner decision, 2026-09-05): auth, tokens and
// recovery_keys hold only one-way hashes, so they keep persisting to a
// plain JSON file with no key configured, exactly as every mikroview
// release before #853. Every other store still follows "no key, no
// storage" and gets nil.
func TestBackendForKeepsTheHashedStoresWithoutAKey(t *testing.T) {
	dir := t.TempDir()
	s := &storage{}
	ctx := context.Background()

	for _, name := range []string{"auth", "tokens", "recovery_keys"} {
		path := filepath.Join(dir, name+".json")
		backend, err := s.backendFor(ctx, name, path)
		if err != nil {
			t.Fatalf("%s: backendFor: %v", name, err)
		}
		if backend == nil {
			t.Errorf("%s: backendFor returned nil with no key -- this store should keep persisting (#853 rule 6)", name)
			continue
		}
		if _, ok := backend.(*persist.FileBackend); !ok {
			t.Errorf("%s: backendFor returned %T, want a plain *persist.FileBackend", name, backend)
		}
	}

	backend, err := s.backendFor(ctx, "flags", filepath.Join(dir, "flags.json"))
	if err != nil {
		t.Fatalf("flags: backendFor: %v", err)
	}
	if backend != nil {
		t.Errorf("flags: backendFor returned %T with no key, want nil -- flags is not a hashed store", backend)
	}
}
