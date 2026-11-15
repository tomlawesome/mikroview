// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// Tests for the guards #1202 added around sealed documents: the plaintext
// row guard on Postgres rows, adoption of a sealed file, and the staged
// file-schema stamp. They use the file backend and an in-package stub, so
// they run with or without Postgres.

func sealKey(t *testing.T, fill string) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat(fill, retention.MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// memBackend is a minimal in-memory Backend with no Version method, so the
// guard's fall-back to a full read can be seen, and with knobs to fail.
type memBackend struct {
	payload []byte
	version int64
	exists  bool
	loadErr error
	saveErr error
	saves   int
}

func (m *memBackend) Load(context.Context) (Snapshot, error) {
	if m.loadErr != nil {
		return Snapshot{}, m.loadErr
	}
	return Snapshot{Payload: m.payload, Version: m.version, Exists: m.exists}, nil
}

func (m *memBackend) Save(_ context.Context, p []byte, expect int64) (int64, error) {
	if m.saveErr != nil {
		return 0, m.saveErr
	}
	if expect != m.version {
		return 0, ErrConflict
	}
	m.payload, m.exists, m.version = append([]byte(nil), p...), true, m.version+1
	m.saves++
	return m.version, nil
}
func (m *memBackend) Close() error     { return nil }
func (m *memBackend) Describe() string { return "mem store" }

// A Postgres row holding a plaintext document is refused in the file
// backend's words and never handed on; a sealed row, a damaged one and an
// absent one all pass through for gauntlet to judge as it always has.
func TestPlaintextRowGuardRefusesOnlyPlaintext(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		payload string
		refused bool
	}{
		{"object without a sealed member", `{"users":[]}`, true},
		{"bare token list", `[]`, true},
		{"json scalar", `"hello"`, true},
		{"text envelope", `{"sealed":"AAAA","v":1}`, false},
		{"not json at all", "\x00\x01 binary-ish", false},
	} {
		inner := &memBackend{payload: []byte(tc.payload), version: 3, exists: true}
		snap, err := RefusePlaintextRows(inner).Load(ctx)
		if tc.refused {
			if !errors.Is(err, ErrPlaintextDocument) || len(snap.Payload) != 0 {
				t.Errorf("%s: Load = %q, %v; want ErrPlaintextDocument and no payload", tc.name, snap.Payload, err)
			}
			if !strings.Contains(err.Error(), "mem store") {
				t.Errorf("%s: the refusal does not name the row: %v", tc.name, err)
			}
		} else if err != nil || string(snap.Payload) != tc.payload {
			t.Errorf("%s: Load = %q, %v; want it passed through", tc.name, snap.Payload, err)
		}
	}

	if snap, err := RefusePlaintextRows(&memBackend{}).Load(ctx); err != nil || snap.Exists {
		t.Errorf("absent row: %v, %v; want a clean empty snapshot", snap, err)
	}
	boom := errors.New("connection reset")
	if _, err := RefusePlaintextRows(&memBackend{loadErr: boom}).Load(ctx); !errors.Is(err, boom) {
		t.Errorf("inner load failure = %v, want it passed on", err)
	}
}

func TestPlaintextRowGuardDelegatesWritesAndVersions(t *testing.T) {
	ctx := context.Background()
	inner := &memBackend{}
	g := RefusePlaintextRows(inner)

	if v, err := g.Save(ctx, []byte(`{"sealed":"x"}`), 0); err != nil || v != 1 {
		t.Fatalf("Save = %d, %v", v, err)
	}
	if _, err := g.Save(ctx, []byte(`{"sealed":"y"}`), 0); !errors.Is(err, ErrConflict) {
		t.Errorf("a stale create = %v, want ErrConflict from the row", err)
	}
	if g.Describe() != "mem store" || g.Close() != nil {
		t.Error("Describe/Close do not reach the wrapped backend")
	}
	// The wrapped backend has no Version method: the guard reads it whole.
	if v, ok, err := g.(VersionReader).Version(ctx); err != nil || !ok || v != 1 {
		t.Errorf("Version via full read = %d, %v, %v; want 1, true", v, ok, err)
	}

	// With a VersionReader underneath, the cheap check is used: the answer
	// is the inner one, and a plaintext row is not read, so not refused.
	vr := &versionedBackend{memBackend: memBackend{payload: []byte(`{"plain":true}`), version: 1, exists: true}}
	v, ok, err := RefusePlaintextRows(vr).(VersionReader).Version(ctx)
	if err != nil || !ok || v != 42 {
		t.Errorf("Version over a VersionReader = %d, %v, %v; want the inner 42, true", v, ok, err)
	}
	if vr.loads != 0 {
		t.Errorf("the staleness poll read the document %d time(s)", vr.loads)
	}
}

// versionedBackend adds a cheap Version to memBackend and counts loads.
type versionedBackend struct {
	memBackend
	loads int
}

func (v *versionedBackend) Load(ctx context.Context) (Snapshot, error) {
	v.loads++
	return v.memBackend.Load(ctx)
}

func (v *versionedBackend) Version(context.Context) (int64, bool, error) { return 42, true, nil }

// Version on a sealed file reads the version without decrypting, so a
// restore can overwrite a file sealed under a lost key.
func TestEncryptedFileVersionDoesNotNeedTheRightKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "doc.json")
	b := NewEncryptedFileBackend(path, "store", sealKey(t, "a"))
	if _, ok, err := b.Version(ctx); err != nil || ok {
		t.Fatalf("absent file: exists %v, %v", ok, err)
	}
	v1, err := b.Save(ctx, []byte(`{"a":1}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	other := NewEncryptedFileBackend(path, "store", sealKey(t, "b"))
	if _, err := other.Load(ctx); err == nil {
		t.Fatal("a document sealed under another key opened")
	}
	if v, ok, err := other.Version(ctx); err != nil || !ok || v != v1 {
		t.Errorf("Version under the wrong key = %d, %v, %v; want %d, true", v, ok, err, v1)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestAdoptSealedFileEdges(t *testing.T) {
	ctx := context.Background()
	key := sealKey(t, "k")
	dir := t.TempDir()
	src := filepath.Join(dir, "store.json")
	if _, err := NewEncryptedFileBackend(src, "store", key).Save(ctx, []byte(`{"n":1}`), 0); err != nil {
		t.Fatal(err)
	}
	srcBytes, _ := os.ReadFile(src)

	t.Run("nothing to do without a path or a destination", func(t *testing.T) {
		if res, err := AdoptSealedFile(ctx, "", "store", key, &memBackend{}); err != nil || res.Migrated {
			t.Errorf("empty path: %+v, %v", res, err)
		}
		if res, err := AdoptSealedFile(ctx, src, "store", key, nil); err != nil || res.Migrated {
			t.Errorf("nil destination: %+v, %v", res, err)
		}
	})
	t.Run("no key is an error, not a plaintext copy", func(t *testing.T) {
		dst := &memBackend{}
		if _, err := AdoptSealedFile(ctx, src, "store", nil, dst); err == nil || dst.saves != 0 {
			t.Errorf("err %v, saves %d; want a refusal that wrote nothing", err, dst.saves)
		}
	})
	t.Run("a destination with data is left alone", func(t *testing.T) {
		dst := &memBackend{payload: []byte(`{"live":true}`), version: 5, exists: true}
		res, err := AdoptSealedFile(ctx, src, "store", key, dst)
		if err != nil || res.Migrated || dst.saves != 0 || string(dst.payload) != `{"live":true}` {
			t.Errorf("res %+v err %v saves %d payload %q; want the live data untouched", res, err, dst.saves, dst.payload)
		}
	})
	t.Run("a destination that cannot be read fails before anything is written", func(t *testing.T) {
		dst := &memBackend{loadErr: errors.New("db down")}
		if _, err := AdoptSealedFile(ctx, src, "store", key, dst); err == nil || !strings.Contains(err.Error(), "before migrating") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a missing source is a fresh install", func(t *testing.T) {
		dst := &memBackend{}
		res, err := AdoptSealedFile(ctx, filepath.Join(dir, "absent.json"), "store", key, dst)
		if err != nil || res.Migrated || dst.saves != 0 {
			t.Errorf("res %+v err %v", res, err)
		}
	})
	t.Run("the wrong key fails and writes nothing", func(t *testing.T) {
		dst := &memBackend{}
		if _, err := AdoptSealedFile(ctx, src, "store", sealKey(t, "x"), dst); err == nil || dst.saves != 0 {
			t.Errorf("err %v, saves %d; want a failure that wrote nothing", err, dst.saves)
		}
	})
	t.Run("losing the create race is success, any other save failure is not", func(t *testing.T) {
		if res, err := AdoptSealedFile(ctx, src, "store", key, &memBackend{saveErr: ErrConflict}); err != nil || res.Migrated {
			t.Errorf("conflict: %+v, %v; want success without Migrated", res, err)
		}
		if _, err := AdoptSealedFile(ctx, src, "store", key, &memBackend{saveErr: errors.New("disk full")}); err == nil {
			t.Error("a failed save was reported as success")
		}
	})
	t.Run("a good move hands over the readable document and keeps the file", func(t *testing.T) {
		dst := &memBackend{}
		res, err := AdoptSealedFile(ctx, src, "store", key, dst)
		if err != nil || !res.Migrated || res.Bytes != len(`{"n":1}`) || string(dst.payload) != `{"n":1}` {
			t.Errorf("res %+v err %v payload %q", res, err, dst.payload)
		}
		if after, _ := os.ReadFile(src); string(after) != string(srcBytes) {
			t.Error("the source file changed")
		}
	})
}

func TestAdoptFileEdges(t *testing.T) {
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(src, []byte(`{"p":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, err := AdoptFile(ctx, "", &memBackend{}); err != nil || res.Migrated {
		t.Errorf("empty path: %+v, %v", res, err)
	}
	if res, err := AdoptFile(ctx, src, nil); err != nil || res.Migrated {
		t.Errorf("nil backend: %+v, %v", res, err)
	}
	if _, err := AdoptFile(ctx, src, &memBackend{loadErr: errors.New("db down")}); err == nil {
		t.Error("an unreadable destination was not reported")
	}
	if res, err := AdoptFile(ctx, filepath.Join(t.TempDir(), "absent.json"), &memBackend{}); err != nil || res.Migrated {
		t.Errorf("missing file: %+v, %v", res, err)
	}
	live := &memBackend{payload: []byte(`{"live":1}`), version: 1, exists: true}
	if res, err := AdoptFile(ctx, src, live); err != nil || res.Migrated || string(live.payload) != `{"live":1}` {
		t.Errorf("existing data: %+v, %v, %q", res, err, live.payload)
	}
	if res, err := AdoptFile(ctx, src, &memBackend{saveErr: ErrConflict}); err != nil || res.Migrated {
		t.Errorf("lost race: %+v, %v; want success without Migrated", res, err)
	}
	if _, err := AdoptFile(ctx, src, &memBackend{saveErr: errors.New("disk full")}); err == nil {
		t.Error("a failed save was reported as success")
	}
	// A directory where the file should be cannot be read.
	if _, err := AdoptFile(ctx, t.TempDir(), &memBackend{}); err == nil {
		t.Error("an unreadable source was reported as a fresh install")
	}
}

// The staged stamp (SchemaAuthSealed): a start can stamp up to the schema
// below it, leaving the sealing step pending until every store has opened.
func TestMigrateFileSchemaToStopsAtTheRequestedVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	to := SchemaAuthSealed - 1

	res, err := MigrateFileSchemaTo(ctx, dir, "v0.7.0-test", to)
	if err != nil {
		t.Fatal(err)
	}
	if res.To != to {
		t.Errorf("stopped at %d, want %d", res.To, to)
	}
	if got, by, _ := ReadFileSchema(dir); got != to || by != "v0.7.0-test" {
		t.Errorf("stamped %d by %q, want %d by v0.7.0-test", got, by, to)
	}

	// The rest lands on a later call.
	res, err = MigrateFileSchemaTo(ctx, dir, "v0.7.0-test", CurrentSchema())
	if err != nil || res.To != CurrentSchema() || res.From != to {
		t.Errorf("second pass: %+v, %v; want %d -> %d", res, err, to, CurrentSchema())
	}
	// Nothing left: a third pass changes nothing.
	if res, err := MigrateFileSchemaTo(ctx, dir, "later-build", CurrentSchema()); err != nil || len(res.Applied) != 0 {
		t.Errorf("third pass: %+v, %v", res, err)
	}
	if _, by, _ := ReadFileSchema(dir); by != "v0.7.0-test" {
		t.Errorf("a pass that changed nothing re-stamped the writer as %q", by)
	}
}

func TestSchemaGuardsRefuseNewerDataWithoutWriting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	doc := `{"schema":9999,"version":"v99.0.0"}`
	path := filepath.Join(dir, SchemaDocumentName)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	var tooNew *SchemaTooNewError
	if err := CheckFileSchema(dir); !errors.As(err, &tooNew) || tooNew.Stored != 9999 {
		t.Errorf("CheckFileSchema = %v, want *SchemaTooNewError at 9999", err)
	}
	// Even a staged call that would stop short compares against everything
	// this build knows.
	if _, err := MigrateFileSchemaTo(ctx, dir, "x", 1); !errors.As(err, &tooNew) {
		t.Errorf("MigrateFileSchemaTo = %v, want *SchemaTooNewError", err)
	}
	if got, _ := os.ReadFile(path); string(got) != doc {
		t.Errorf("the newer build's schema document was rewritten: %q", got)
	}
}

func TestCheckFileSchemaAcceptsFreshAndCurrentAndFailsClosedOnGarbage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := CheckFileSchema(dir); err != nil {
		t.Errorf("fresh directory: %v", err)
	}
	if _, err := MigrateFileSchema(ctx, dir, "v"); err != nil {
		t.Fatal(err)
	}
	if err := CheckFileSchema(dir); err != nil {
		t.Errorf("current directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, SchemaDocumentName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var se *StartupError
	if err := CheckFileSchema(dir); !errors.As(err, &se) {
		t.Errorf("garbage schema document = %v, want a *StartupError (guessing 'fresh' would defeat the guard)", err)
	}
}
