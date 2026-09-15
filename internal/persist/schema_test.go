// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dirHash fingerprints a whole directory -- every file's path, mode and
// bytes -- so a test can say "nothing on disk changed" without listing
// what it expected to find. The downgrade guard's promise is exactly
// that, and a check that only looked at the files the test wrote would
// miss a stray temp file or a stamp it should never have made.
func dirHash(t *testing.T, dir string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(sum, "%s\x00%o\x00", rel, info.Mode().Perm())
		if d.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path) // #nosec G304 -- a t.TempDir() under this test's control
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(sum, "%d\x00", len(body))
		sum.Write(body)
		return nil
	})
	if err != nil {
		t.Fatalf("hashing %s: %v", dir, err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// writeStoreFiles puts a handful of documents in the data directory, as
// any install that has actually been used would have.
func writeStoreFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"users.json":    []byte(`{"users":[{"id":"1","name":"admin"}]}`),
		"flags.json":    []byte(`{"flags":[]}`),
		"entities.json": []byte(`{"entities":{"10.0.0.1":{"name":"gateway"}}}`),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func stampForTest(t *testing.T, dir string, schema int64, writtenBy string) {
	t.Helper()
	if err := stampFileSchema(dir, schema, writtenBy); err != nil {
		t.Fatalf("stamping schema %d: %v", schema, err)
	}
}

// TestMigrateFileSchema is the file backend's half of #1238: where the
// schema version comes from, what happens to an install that has never
// had one, and what happens to one written by a build from the future.
func TestMigrateFileSchema(t *testing.T) {
	current := CurrentSchema()

	cases := []struct {
		name string
		// setup prepares the data directory; whatever it returns is
		// handed to check.
		setup func(t *testing.T, dir string) any
		// wantErr is nil when the run is expected to succeed.
		wantErr func(t *testing.T, dir string, err error)
		check   func(t *testing.T, dir string, res FileSchemaResult, from any)
	}{
		{
			name:  "a fresh data directory is stamped at the current schema",
			setup: func(t *testing.T, dir string) any { return nil },
			check: func(t *testing.T, dir string, res FileSchemaResult, _ any) {
				if res.From != 0 || res.To != current {
					t.Errorf("went from %d to %d, want 0 -> %d", res.From, res.To, current)
				}
				schema, writtenBy, err := ReadFileSchema(dir)
				if err != nil {
					t.Fatalf("ReadFileSchema: %v", err)
				}
				if schema != current {
					t.Errorf("stamped schema = %d, want %d", schema, current)
				}
				if writtenBy != "v9.9.9-test" {
					t.Errorf("stamped build version = %q, want the running build's", writtenBy)
				}
			},
		},
		{
			name: "an unstamped directory with stores in it keeps every byte",
			setup: func(t *testing.T, dir string) any {
				return writeStoreFiles(t, dir)
			},
			check: func(t *testing.T, dir string, res FileSchemaResult, from any) {
				if res.From != 0 || res.To != current {
					t.Errorf("went from %d to %d, want 0 -> %d", res.From, res.To, current)
				}
				// No migration in this build changes a document, so the
				// documents must come out identical. When one does, this
				// is the test that has to be given the new expectation
				// deliberately.
				for name, want := range from.(map[string][]byte) {
					got, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatalf("reading %s back: %v", name, err)
					}
					if string(got) != string(want) {
						t.Errorf("%s changed:\n got %s\nwant %s", name, got, want)
					}
				}
			},
		},
		{
			name: "a directory already at the current schema is left alone",
			setup: func(t *testing.T, dir string) any {
				writeStoreFiles(t, dir)
				stampForTest(t, dir, current, "v9.9.9-test")
				return dirHash(t, dir)
			},
			check: func(t *testing.T, dir string, res FileSchemaResult, before any) {
				if res.From != current || res.To != current {
					t.Errorf("went from %d to %d, want %d -> %d", res.From, res.To, current, current)
				}
				if len(res.Applied) != 0 {
					t.Errorf("applied %v, want nothing on an up-to-date directory", res.Applied)
				}
				if after := dirHash(t, dir); after != before.(string) {
					t.Error("an up-to-date directory was rewritten -- a start that migrates nothing must write nothing")
				}
			},
		},
		{
			name: "data from a newer build is refused, and nothing is written",
			setup: func(t *testing.T, dir string) any {
				writeStoreFiles(t, dir)
				stampForTest(t, dir, current+1, "v42.0.0")
				return dirHash(t, dir)
			},
			wantErr: func(t *testing.T, dir string, err error) {
				var tooNew *SchemaTooNewError
				if !errors.As(err, &tooNew) {
					t.Fatalf("err = %v, want a *SchemaTooNewError", err)
				}
				if tooNew.Stored != current+1 || tooNew.Known != current {
					t.Errorf("stored/known = %d/%d, want %d/%d", tooNew.Stored, tooNew.Known, current+1, current)
				}
				for _, want := range []string{"v42.0.0", dir, "that build or a newer one", "Refusing to start"} {
					if !strings.Contains(tooNew.Error(), want) {
						t.Errorf("the refusal does not mention %q:\n%s", want, tooNew.Error())
					}
				}
			},
			check: func(t *testing.T, dir string, _ FileSchemaResult, before any) {
				if after := dirHash(t, dir); after != before.(string) {
					t.Error("the data directory changed on a refused downgrade -- nothing may be written on that path")
				}
			},
		},
		{
			name: "a newer build that did not record its version still refuses",
			setup: func(t *testing.T, dir string) any {
				stampForTest(t, dir, current+3, "")
				return dirHash(t, dir)
			},
			wantErr: func(t *testing.T, dir string, err error) {
				var tooNew *SchemaTooNewError
				if !errors.As(err, &tooNew) {
					t.Fatalf("err = %v, want a *SchemaTooNewError", err)
				}
				if !strings.Contains(tooNew.Error(), "does not record which build wrote it") {
					t.Errorf("the refusal reads badly with no version recorded:\n%s", tooNew.Error())
				}
			},
			check: func(t *testing.T, dir string, _ FileSchemaResult, before any) {
				if after := dirHash(t, dir); after != before.(string) {
					t.Error("the data directory changed on a refused downgrade")
				}
			},
		},
		{
			name: "a schema document that cannot be parsed fails closed",
			setup: func(t *testing.T, dir string) any {
				if err := os.WriteFile(filepath.Join(dir, SchemaDocumentName), []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
				return dirHash(t, dir)
			},
			wantErr: func(t *testing.T, dir string, err error) {
				var startup *StartupError
				if !errors.As(err, &startup) {
					t.Fatalf("err = %v, want a *StartupError", err)
				}
			},
			check: func(t *testing.T, dir string, _ FileSchemaResult, before any) {
				if after := dirHash(t, dir); after != before.(string) {
					t.Error("an unreadable schema document was written over rather than refused")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			from := tc.setup(t, dir)

			res, err := MigrateFileSchema(context.Background(), dir, "v9.9.9-test")
			switch {
			case tc.wantErr != nil:
				if err == nil {
					t.Fatal("MigrateFileSchema succeeded, want a refusal")
				}
				tc.wantErr(t, dir, err)
			case err != nil:
				t.Fatalf("MigrateFileSchema: %v", err)
			}
			if tc.check != nil {
				tc.check(t, dir, res, from)
			}
		})
	}
}

// TestMigrateFileSchemaResumesAfterAnInterruptedUpgrade is requirement 3:
// stamping after each migration lands is what makes an upgrade killed
// half-way resumable.
//
// The kill is simulated rather than real -- a migration that returns an
// error after the one before it has landed leaves exactly the on-disk
// state a SIGKILL between the two would -- and the second run is a
// separate call with the same list, which is a restart as far as the
// data directory is concerned.
func TestMigrateFileSchemaResumesAfterAnInterruptedUpgrade(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"generation":0}`)
	doc := filepath.Join(dir, "store.json")
	if err := os.WriteFile(doc, original, 0o600); err != nil {
		t.Fatal(err)
	}

	firstRuns := 0
	first := func(ctx context.Context, dir string) error {
		firstRuns++
		return WriteFileAtomic(filepath.Join(dir, "store.json"), []byte(`{"generation":1}`), 0o600)
	}
	// The interrupted one: it writes its replacement to a temp file and
	// dies before the rename, which is what "atomic per migration" has to
	// survive.
	secondFailed := func(ctx context.Context, dir string) error {
		f, err := os.CreateTemp(dir, "store.json.tmp-*")
		if err != nil {
			return err
		}
		if _, err := f.WriteString(`{"generation":2}`); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return errors.New("killed part-way through")
	}
	secondRuns := 0
	secondOK := func(ctx context.Context, dir string) error {
		secondRuns++
		return WriteFileAtomic(filepath.Join(dir, "store.json"), []byte(`{"generation":2}`), 0o600)
	}

	interrupted := []SchemaMigration{
		{Number: 1, Description: "first half", File: first},
		{Number: 2, Description: "second half", File: secondFailed},
	}
	res, err := migrateFileSchema(context.Background(), dir, "v1.0.0-test", interrupted)
	if err == nil {
		t.Fatal("the interrupted run reported success")
	}
	if res.To != 1 {
		t.Errorf("stopped at schema %d, want 1 -- the first migration landed and must be recorded", res.To)
	}
	schema, _, err := ReadFileSchema(dir)
	if err != nil {
		t.Fatalf("ReadFileSchema: %v", err)
	}
	if schema != 1 {
		t.Fatalf("stamped schema = %d after the interruption, want 1", schema)
	}
	// The failed migration's replacement never landed: the document is
	// still the first migration's output, not a mixture and not the
	// second's.
	if body, _ := os.ReadFile(doc); string(body) != `{"generation":1}` {
		t.Errorf("store.json = %s, want the first migration's output untouched by the second", body)
	}

	// Restart: same list, with the second migration now able to finish.
	resumed := []SchemaMigration{
		{Number: 1, Description: "first half", File: first},
		{Number: 2, Description: "second half", File: secondOK},
	}
	res, err = migrateFileSchema(context.Background(), dir, "v1.0.0-test", resumed)
	if err != nil {
		t.Fatalf("the resumed run failed: %v", err)
	}
	if res.From != 1 || res.To != 2 {
		t.Errorf("resumed run went from %d to %d, want 1 -> 2", res.From, res.To)
	}
	if firstRuns != 1 {
		t.Errorf("the first migration ran %d times, want 1 -- a migration that landed must never run again", firstRuns)
	}
	if secondRuns != 1 {
		t.Errorf("the second migration ran %d times, want 1", secondRuns)
	}
	if body, _ := os.ReadFile(doc); string(body) != `{"generation":2}` {
		t.Errorf("store.json = %s, want the second migration's output", body)
	}
	schema, writtenBy, err := ReadFileSchema(dir)
	if err != nil {
		t.Fatalf("ReadFileSchema: %v", err)
	}
	if schema != 2 || writtenBy != "v1.0.0-test" {
		t.Errorf("stamped %d by %q, want 2 by v1.0.0-test", schema, writtenBy)
	}
}

// A migration that fails before anything of its own has landed leaves the
// previous migration's stamp in place -- the run is resumable from there,
// not rolled back to zero.
func TestMigrateFileSchemaKeepsTheStampOfTheLastMigrationThatLanded(t *testing.T) {
	dir := t.TempDir()
	stampForTest(t, dir, 1, "v0.1.0")
	list := []SchemaMigration{
		{Number: 1, Description: "already done", File: func(context.Context, string) error {
			t.Error("a migration at or below the stored schema ran again")
			return nil
		}},
		{Number: 2, Description: "fails", File: func(context.Context, string) error {
			return errors.New("nope")
		}},
	}
	if _, err := migrateFileSchema(context.Background(), dir, "v0.2.0", list); err == nil {
		t.Fatal("expected the failing migration to be reported")
	}
	schema, writtenBy, err := ReadFileSchema(dir)
	if err != nil {
		t.Fatalf("ReadFileSchema: %v", err)
	}
	if schema != 1 || writtenBy != "v0.1.0" {
		t.Errorf("stamped %d by %q, want the pre-run stamp 1 by v0.1.0 to be untouched", schema, writtenBy)
	}
}

// CheckFileSchema is the guard on its own, for callers that want to know
// whether they may touch the data at all. It must never write -- not even
// the stamp a migrating caller would make.
func TestCheckFileSchemaWritesNothing(t *testing.T) {
	t.Run("fresh directory", func(t *testing.T) {
		dir := t.TempDir()
		before := dirHash(t, dir)
		if err := CheckFileSchema(dir); err != nil {
			t.Fatalf("CheckFileSchema on a fresh directory: %v", err)
		}
		if after := dirHash(t, dir); after != before {
			t.Error("CheckFileSchema wrote to the data directory")
		}
	})

	t.Run("newer data", func(t *testing.T) {
		dir := t.TempDir()
		stampForTest(t, dir, CurrentSchema()+1, "v42.0.0")
		before := dirHash(t, dir)
		err := CheckFileSchema(dir)
		var tooNew *SchemaTooNewError
		if !errors.As(err, &tooNew) {
			t.Fatalf("err = %v, want a *SchemaTooNewError", err)
		}
		if after := dirHash(t, dir); after != before {
			t.Error("CheckFileSchema wrote to the data directory on the refusal path")
		}
	})
}

// A stamp that cannot be written where nothing else was changed is
// recoverable -- the caller is told which case it is so it can carry on
// (see main's upgradeDataDirSchema) rather than refusing to start over a
// record it can rewrite next time.
func TestMigrateFileSchemaReportsAnUnrecordableStampSeparately(t *testing.T) {
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "data")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory mode this test depends on")
	}

	_, err := MigrateFileSchema(context.Background(), readOnly, "v1.0.0-test")
	if !errors.Is(err, ErrSchemaNotStamped) {
		t.Fatalf("err = %v, want it to wrap ErrSchemaNotStamped", err)
	}
}

// The schema document is what the guard reads on the next start, so its
// shape is part of the on-disk contract: a number, the build that wrote
// it, and nothing that needs a key to read.
func TestSchemaDocumentShape(t *testing.T) {
	dir := t.TempDir()
	if _, err := MigrateFileSchema(context.Background(), dir, "v1.2.3"); err != nil {
		t.Fatalf("MigrateFileSchema: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, SchemaDocumentName))
	if err != nil {
		t.Fatalf("reading the schema document: %v", err)
	}
	var doc struct {
		Schema    int64  `json:"schema"`
		Version   string `json:"version"`
		StampedAt string `json:"stampedAt"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the schema document is not plain JSON: %v (%s)", err, body)
	}
	if doc.Schema != CurrentSchema() || doc.Version != "v1.2.3" || doc.StampedAt == "" {
		t.Errorf("schema document = %+v, want schema %d written by v1.2.3 with a timestamp", doc, CurrentSchema())
	}
	info, err := os.Stat(filepath.Join(dir, SchemaDocumentName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("schema document mode = %04o, want 0600 like every other document here", perm)
	}
}

// The list is the numbering for both backends, so the two halves have to
// agree: every .sql file is an entry's postgres step, every entry's
// postgres step exists, and the numbers ascend.
func TestSchemaListAndSQLFilesAgree(t *testing.T) {
	if err := validateSchemaList(schemaMigrations); err != nil {
		t.Fatalf("the schema list is not valid: %v", err)
	}

	ms, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	listed := map[int64]string{}
	for _, m := range schemaMigrations {
		if m.Postgres != "" {
			listed[m.Number] = m.Postgres
		}
	}
	if len(ms) != len(listed) {
		t.Errorf("loadMigrations returned %d migrations, the list names %d postgres steps", len(ms), len(listed))
	}
	for _, m := range ms {
		if listed[m.version] != m.name {
			t.Errorf("schema version %d loaded %q, the list names %q", m.version, m.name, listed[m.version])
		}
	}
	if got := CurrentSchema(); got != schemaMigrations[len(schemaMigrations)-1].Number {
		t.Errorf("CurrentSchema = %d, want the last entry's number", got)
	}
}

func TestValidateSchemaListRejectsMisnumbering(t *testing.T) {
	cases := []struct {
		name string
		list []SchemaMigration
	}{
		{"out of order", []SchemaMigration{{Number: 2, Description: "b"}, {Number: 1, Description: "a"}}},
		{"duplicate", []SchemaMigration{{Number: 1, Description: "a"}, {Number: 1, Description: "b"}}},
		{"zero", []SchemaMigration{{Number: 0, Description: "a"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateSchemaList(tc.list); err == nil {
				t.Error("accepted a list that would skip or repeat a migration")
			}
		})
	}
}
