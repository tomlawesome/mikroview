// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// Tests for the storage helpers #1202 added around moving the JSON files
// onto Postgres and sealing what is stored. None needs a database: the
// decisions under test are made on the storage value and the backend it is
// handed, so a stand-in pool (never dialled) or an in-memory backend is
// enough to see each outcome.

func testStorageKey(t *testing.T) *retention.Key {
	t.Helper()
	key, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// logCapture is a logger whose output the test can read back.
func logCapture() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// failingSaveBackend refuses every save as if another process had written
// first, or fails it outright, so writeTokensRow's two error branches can
// be told apart.
type failingSaveBackend struct {
	gpersist.Backend
	err error
}

func (f failingSaveBackend) Save(context.Context, []byte, int64) (int64, error) { return 0, f.err }

// The tokens row is written once, empty, and then left alone: a second
// call must not overwrite tokens that were issued in between.
func TestWriteTokensRowCreatesOnceAndNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	b := gpersist.NewMemory()

	if err := writeTokensRow(ctx, b); err != nil {
		t.Fatalf("first write: %v", err)
	}
	snap, err := b.Load(ctx)
	if err != nil || !snap.Exists || string(snap.Payload) != emptyTokensDocument {
		t.Fatalf("row after first write = %q (exists %v), %v; want the empty token list", snap.Payload, snap.Exists, err)
	}

	// A token is issued; a later start must not wipe it.
	if _, err := b.Save(ctx, []byte(`[{"id":"t1"}]`), snap.Version); err != nil {
		t.Fatal(err)
	}
	if err := writeTokensRow(ctx, b); err != nil {
		t.Fatalf("second write: %v", err)
	}
	snap, _ = b.Load(ctx)
	if string(snap.Payload) != `[{"id":"t1"}]` {
		t.Errorf("an existing tokens row was overwritten: %q", snap.Payload)
	}
}

func TestWriteTokensRowSwallowsAConflictButReportsAnyOtherFailure(t *testing.T) {
	ctx := context.Background()
	if err := writeTokensRow(ctx, failingSaveBackend{Backend: gpersist.NewMemory(), err: persist.ErrConflict}); err != nil {
		t.Errorf("a lost create race (another instance wrote first) is not a failure: %v", err)
	}
	boom := errors.New("disk on fire")
	err := writeTokensRow(ctx, failingSaveBackend{Backend: gpersist.NewMemory(), err: boom})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "sealed tokens row") {
		t.Errorf("a failed write = %v, want it to name the sealed tokens row and wrap the cause", err)
	}
}

// The file backend has no tokens row, no pool and nothing to adopt: the
// guarantees that exist for Postgres do nothing there, and say nothing.
func TestSealTokensRowAndAccountsCheckAreNoOpsWithoutPostgresOrAKey(t *testing.T) {
	ctx := context.Background()
	for name, s := range map[string]*storage{
		"file backend, key":    {key: testStorageKey(t)},
		"file backend, no key": {},
		"postgres but no key":  {pool: &persist.Pool{}},
		"nothing configured":   {cfg: config.Config{}},
	} {
		if err := s.sealTokensRow(ctx); err != nil {
			t.Errorf("%s: sealTokensRow = %v", name, err)
		}
		if err := checkAccountsBeforeTokensRow(ctx, s); err != nil {
			t.Errorf("%s: checkAccountsBeforeTokensRow = %v", name, err)
		}
	}
}

// R22: the adoption marker is written only once every store has moved.
// A start where a move failed leaves no marker, so the next start tries
// the move again rather than coming up with that store empty for good.
func TestRecordPostgresAdoptionWritesTheMarkerOnlyWhenNothingFailed(t *testing.T) {
	t.Run("file backend writes nothing", func(t *testing.T) {
		s := &storage{cfg: cfgWithData(t.TempDir())}
		if err := s.recordPostgresAdoption(); err != nil {
			t.Fatal(err)
		}
		if postgresAlreadyAdopted(s.cfg) {
			t.Error("a deployment on JSON files was marked as having adopted Postgres")
		}
	})
	t.Run("a failed move leaves no marker and says so", func(t *testing.T) {
		log, buf := logCapture()
		s := &storage{pool: &persist.Pool{}, cfg: cfgWithData(t.TempDir()), log: log, adoptFailed: true}
		if err := s.recordPostgresAdoption(); err != nil {
			t.Fatal(err)
		}
		if postgresAlreadyAdopted(s.cfg) {
			t.Error("the marker was written although a file did not move; the next start would not try again")
		}
		if !strings.Contains(buf.String(), "tried again at the next start") {
			t.Errorf("no warning that the move will be retried: %q", buf)
		}
	})
	t.Run("a complete move writes it", func(t *testing.T) {
		s := &storage{pool: &persist.Pool{}, cfg: cfgWithData(t.TempDir())}
		if err := s.recordPostgresAdoption(); err != nil {
			t.Fatal(err)
		}
		if !postgresAlreadyAdopted(s.cfg) {
			t.Error("a finished move left no marker, so removing the DSN later would fall back to stale files")
		}
	})
}

// finishSchemaUpgrade stamps the data directory's schema -- the guard
// that stops an older release reading sealed accounts wrongly -- but not
// on a start where a file failed to move onto Postgres (#1202 round 3).
func TestFinishSchemaUpgradeStampsTheDataDirUnlessAMoveFailed(t *testing.T) {
	ctx := context.Background()

	t.Run("adoption failed: nothing stamped", func(t *testing.T) {
		dir := t.TempDir()
		cfg := cfgWithData(dir)
		s := &storage{cfg: cfg, adoptFailed: true}
		if err := s.finishSchemaUpgrade(ctx, cfg); err != nil {
			t.Fatalf("finishSchemaUpgrade: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, persist.SchemaDocumentName)); !os.IsNotExist(err) {
			t.Errorf("a schema was recorded although a file did not move (stat err %v)", err)
		}
	})

	t.Run("normal start: the current schema is recorded", func(t *testing.T) {
		dir := t.TempDir()
		cfg := cfgWithData(dir)
		s := &storage{cfg: cfg}
		if err := s.finishSchemaUpgrade(ctx, cfg); err != nil {
			t.Fatalf("finishSchemaUpgrade: %v", err)
		}
		got, _, err := persist.ReadFileSchema(dir)
		if err != nil || got != persist.CurrentSchema() {
			t.Errorf("recorded schema = %d, %v; want %d", got, err, persist.CurrentSchema())
		}
	})

	t.Run("a stamp that cannot be written refuses start-up", func(t *testing.T) {
		// The data directory is a file, so nothing can be written in it.
		parent := t.TempDir()
		blocker := filepath.Join(parent, "data")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := cfgWithData(blocker)
		s := &storage{cfg: cfg}
		err := s.finishSchemaUpgrade(ctx, cfg)
		if err == nil || !strings.Contains(err.Error(), "refusing to start") {
			t.Errorf("finishSchemaUpgrade = %v, want a refusal to start without the schema stamp", err)
		}
	})
}

// A data directory a newer build wrote stops start-up, rather than being
// rewritten in this build's older shapes.
func TestUpgradeDataDirSchemaRefusesDataFromANewerBuild(t *testing.T) {
	dir := t.TempDir()
	doc := `{"schema":9999,"writtenBy":"v99.0.0"}`
	if err := os.WriteFile(filepath.Join(dir, persist.SchemaDocumentName), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	err := upgradeDataDirSchema(cfgWithData(dir))
	var tooNew *persist.SchemaTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("upgradeDataDirSchema = %v, want *persist.SchemaTooNewError", err)
	}
}

// requireKey is the one no-key refusal the server, setup-only mode and
// the account commands share (#1202 rulings 2 and 12).
func TestRequireKeyRefusesWithoutAUsableKeyAndSaysWhy(t *testing.T) {
	cfgWith := func(keyFile string) config.Config {
		var c config.Config
		c.History.KeyFile = keyFile
		return c
	}
	good := writeKeyFile(t)
	log, buf := logCapture()
	if !requireKey(log, cfgWith(good), "start") || buf.Len() != 0 {
		t.Fatalf("a usable key was refused or logged about: %q", buf)
	}

	log, buf = logCapture()
	if requireKey(log, cfgWith(""), "start the server") {
		t.Fatal("started without a key")
	}
	for _, want := range []string{"refusing to start the server", "CFG-0083", "no key is mounted"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("no-key refusal lacks %q: %s", want, buf)
		}
	}

	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	log, buf = logCapture()
	if requireKey(log, cfgWith(short), "run -generate-recovery-keys") {
		t.Fatal("started on a key that is too short")
	}
	if !strings.Contains(buf.String(), short) || strings.Contains(buf.String(), "no key is mounted") {
		t.Errorf("a configured-but-unusable key should be reported as that, naming the file: %s", buf)
	}
}

func TestReadDSNFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := readDSNFile(filepath.Join(dir, "absent")); err == nil || !strings.Contains(err.Error(), "reading DSN file") {
		t.Errorf("missing file: %v", err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDSNFile(empty); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("blank file: %v", err)
	}
	ok := filepath.Join(dir, "dsn")
	if err := os.WriteFile(ok, []byte("postgres://u:p@h/db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readDSNFile(ok); err != nil || got != "postgres://u:p@h/db" {
		t.Errorf("readDSNFile = %q, %v; want the DSN with its newline trimmed", got, err)
	}
}

// openStorage on the file backend: the key is loaded once for everything,
// and a key that cannot be used is remembered (keyErr) rather than lost.
func TestOpenStorageFileBackendKeyStates(t *testing.T) {
	ctx := context.Background()
	cfg := cfgWithData(t.TempDir())

	s, err := openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.pool != nil || s.key != nil || s.keyErr != nil {
		t.Errorf("no key configured: pool %v key %v keyErr %v, want all empty", s.pool, s.key, s.keyErr)
	}
	s.Close() // nothing to close; must not panic

	cfg.History.KeyFile = writeKeyFile(t)
	s, err = openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.key == nil || s.keyErr != nil {
		t.Fatalf("usable key: key loaded %v, keyErr %v", s.key != nil, s.keyErr)
	}

	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.History.KeyFile = short
	s, err = openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.key != nil || s.keyErr == nil {
		t.Fatalf("unusable key: key loaded %v, keyErr %v; want no key and the reason kept", s.key != nil, s.keyErr)
	}
}

func TestOpenStorageRefusesAnAdoptedDeploymentWithoutADSN(t *testing.T) {
	cfg := cfgWithData(t.TempDir())
	if err := markPostgresAdopted(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := openStorage(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "one-way") {
		t.Errorf("openStorage = %v, want the Postgres-adopted refusal", err)
	}
}

func TestOpenStorageReportsAnUnreadableDSNFile(t *testing.T) {
	cfg := cfgWithData(t.TempDir())
	cfg.Postgres.DSNFile = filepath.Join(t.TempDir(), "missing-dsn")
	if _, err := openStorage(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "DSN file") {
		t.Errorf("openStorage = %v, want an error naming the DSN file", err)
	}
}

// Adoption (the first move onto Postgres) copies each JSON file's
// document into the store, through the file's own seal, once.
func TestAdoptMovesASealedFileIntoAnEmptyStore(t *testing.T) {
	ctx := context.Background()
	key := testStorageKey(t)
	path := filepath.Join(t.TempDir(), "flags.json")
	if _, err := persist.NewEncryptedFileBackend(path, "flags", key).Save(ctx, []byte(`{"a":1}`), 0); err != nil {
		t.Fatal(err)
	}
	log, buf := logCapture()
	s := &storage{key: key, log: log}
	dst := gpersist.NewMemory()

	if err := s.adopt(ctx, "flags", dst, path); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	snap, _ := dst.Load(ctx)
	if !snap.Exists || string(snap.Payload) != `{"a":1}` {
		t.Errorf("the store holds %q (exists %v), want the file's readable document", snap.Payload, snap.Exists)
	}
	if s.adoptFailed || !strings.Contains(buf.String(), "migrated") {
		t.Errorf("adoptFailed %v, log %q; want a clean move that is logged", s.adoptFailed, buf)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the source file was deleted; turning Postgres off again would have nothing to come back on")
	}
}

func TestAdoptWithoutAKeyCopiesTheFileVerbatim(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(path, []byte(`{"b":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log, _ := logCapture()
	s := &storage{log: log}
	dst := gpersist.NewMemory()
	if err := s.adopt(ctx, "flags", dst, path); err != nil {
		t.Fatal(err)
	}
	if snap, _ := dst.Load(ctx); string(snap.Payload) != `{"b":2}` {
		t.Errorf("payload = %q", snap.Payload)
	}
}

// R22: a file that cannot be moved (here, plaintext left by a keyless
// release) fails the adoption, writes nothing into the store, and sets
// adoptFailed so no marker or schema stamp follows.
func TestAdoptFailureIsRecordedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(path, []byte(`{"users":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log, _ := logCapture()
	s := &storage{key: testStorageKey(t), log: log}
	dst := gpersist.NewMemory()

	err := s.adopt(ctx, "flags", dst, path)
	if !errors.Is(err, persist.ErrPlaintextDocument) {
		t.Fatalf("adopt = %v, want ErrPlaintextDocument", err)
	}
	if !s.adoptFailed {
		t.Error("adoptFailed not set, so the marker would be written over a store that did not move")
	}
	if snap, _ := dst.Load(ctx); snap.Exists {
		t.Errorf("the store was written despite the refusal: %q", snap.Payload)
	}
}

// After the move is recorded, a leftover file is never adopted again; what
// the operator is told depends on whether the store has its data.
func TestAdoptAfterTheMoveReportsLeftoverFilesAndNeverAdoptsThem(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgWithData(filepath.Dir(path))

	t.Run("store has data: an ordinary leftover", func(t *testing.T) {
		log, buf := logCapture()
		s := &storage{adoptedBefore: true, log: log, cfg: cfg}
		dst := gpersist.NewMemory()
		if _, err := dst.Save(ctx, []byte(`{"new":true}`), 0); err != nil {
			t.Fatal(err)
		}
		if err := s.adopt(ctx, "flags", dst, path); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "left over from before") || strings.Contains(buf.String(), "level=WARN") {
			t.Errorf("want an Info about a leftover file, got %q", buf)
		}
		if snap, _ := dst.Load(ctx); string(snap.Payload) != `{"new":true}` {
			t.Errorf("the store's data was replaced by the old file: %q", snap.Payload)
		}
	})

	t.Run("store empty although the file holds data: a loud warning, still not adopted", func(t *testing.T) {
		log, buf := logCapture()
		s := &storage{adoptedBefore: true, log: log, cfg: cfg}
		dst := gpersist.NewMemory()
		if err := s.adopt(ctx, "flags", dst, path); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"level=WARN", "holds data but", "will not be migrated", markerPath(cfg)} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("warning lacks %q: %s", want, buf)
			}
		}
		if snap, _ := dst.Load(ctx); snap.Exists {
			t.Errorf("a file was adopted after the move was recorded, bringing back old data: %q", snap.Payload)
		}
	})

	t.Run("no file, or an empty one, says nothing", func(t *testing.T) {
		log, buf := logCapture()
		s := &storage{adoptedBefore: true, log: log, cfg: cfg}
		empty := filepath.Join(t.TempDir(), "empty.json")
		if err := os.WriteFile(empty, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"", filepath.Join(t.TempDir(), "absent.json"), empty} {
			if err := s.adopt(ctx, "flags", gpersist.NewMemory(), p); err != nil {
				t.Fatal(err)
			}
		}
		if buf.Len() != 0 {
			t.Errorf("logged about nothing: %q", buf)
		}
	})
}
