// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/retention"
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

// #853 rule 6 and its drop-list addendum are over (#1202, rulings 2 and
// 13): no install runs without a key, so no store keeps persisting in
// the clear without one. With no key every store -- the auth stores and
// the drop list included, which used to be the exceptions -- gets nil,
// "no key, no storage".
func TestBackendForPersistsNothingWithoutAKey(t *testing.T) {
	dir := t.TempDir()
	s := &storage{}
	ctx := context.Background()

	for _, name := range []string{"auth", "tokens", "recovery_keys", "signins", "droplist", "flags"} {
		backend, err := s.backendFor(ctx, name, filepath.Join(dir, name+".json"))
		if err != nil {
			t.Fatalf("%s: backendFor: %v", name, err)
		}
		if backend != nil {
			t.Errorf("%s: backendFor returned %T with no key, want nil -- nothing is written in the clear", name, backend)
		}
	}
}

// The auth stores refuse outright without a key rather than reading as
// memory-only: an accounts store that quietly forgot every account on
// restart would hand the first-run screen to whoever reached it first.
// Start-up never gets here without one (requireKey); this is the
// backstop.
func TestAuthBackendForRefusesWithoutAKey(t *testing.T) {
	s := &storage{}
	for _, name := range []string{authStoreAccounts, authStoreTokens, authStoreRecoveryKeys, authStoreSignIns} {
		if b, err := s.authBackendFor(context.Background(), name, filepath.Join(t.TempDir(), name+".json")); err == nil {
			t.Errorf("%s: authBackendFor with no key returned %v and no error", name, b)
		}
	}
}

// With a key, each auth store's file is sealed, and one an earlier
// keyless release left in plain JSON is sealed in place on its first
// open (#1202, ruling 11) -- unlike every other store, which refuses a
// plaintext file.
func TestAuthBackendForSealsAPlaintextAuthFileInPlace(t *testing.T) {
	key, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	s := &storage{key: key}
	ctx := context.Background()
	for _, name := range []string{authStoreAccounts, authStoreTokens, authStoreRecoveryKeys, authStoreSignIns} {
		path := filepath.Join(t.TempDir(), name+".json")
		plain := []byte(`{"users":[]}`)
		if err := os.WriteFile(path, plain, 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := s.authBackendFor(ctx, name, path)
		if err != nil {
			t.Fatalf("%s: authBackendFor: %v", name, err)
		}
		snap, err := b.Load(ctx)
		if err != nil {
			t.Fatalf("%s: first open of a plaintext file: %v", name, err)
		}
		if string(snap.Payload) != string(plain) {
			t.Errorf("%s: Payload = %q, want the plaintext document", name, snap.Payload)
		}
		onDisk, _ := os.ReadFile(path)
		if json.Valid(onDisk) {
			t.Errorf("%s: the file is still plain JSON after its first open", name)
		}
	}

	// Paired: an ordinary store under the same key refuses the same file.
	path := filepath.Join(t.TempDir(), "flags.json")
	if err := os.WriteFile(path, []byte(`{"flags":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := s.backendFor(ctx, "flags", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Load(ctx); err == nil {
		t.Error("an ordinary store opened a plaintext file; only the auth stores seal in place")
	}
}
