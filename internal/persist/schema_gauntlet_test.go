// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 decision 2, "Downgrade", and build notes A5/C: schema migration 4,
// "accounts and tokens on gauntlet", is a stamp with no data change on
// both backends, so the existing downgrade guard (SchemaTooNewError)
// refuses a v0.6.x build before it opens anything.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaMigration4IsTheAccountsAndTokensOnGauntletStamp(t *testing.T) {
	if got := CurrentSchema(); got != 4 {
		t.Fatalf("CurrentSchema() = %d, want 4", got)
	}
	if len(schemaMigrations) != 4 {
		t.Fatalf("%d schema migrations listed, want 4", len(schemaMigrations))
	}
	m := schemaMigrations[3]
	if m.Number != 4 {
		t.Errorf("last migration number = %d, want 4", m.Number)
	}
	if !strings.Contains(m.Description, "gauntlet") {
		t.Errorf("migration 4 description = %q, want it to say accounts and tokens on gauntlet", m.Description)
	}
	// Both backends: a file step and a Postgres step (a comment-only
	// .sql file is allowed; the stamp is the point).
	if m.Postgres == "" {
		t.Error("migration 4 names no Postgres step; the stamp must land on both backends")
	}
}

// A stamp with no data change: an install at schema 3 with its stores in
// place comes out at 4 with every document byte for byte as it was.
func TestSchemaMigration4ChangesNoDocument(t *testing.T) {
	dir := t.TempDir()
	files := writeStoreFiles(t, dir)
	stampForTest(t, dir, 3, "v0.6.1")

	res, err := MigrateFileSchema(context.Background(), dir, "v0.7.0-test")
	if err != nil {
		t.Fatalf("MigrateFileSchema: %v", err)
	}
	if res.From != 3 || res.To != 4 {
		t.Errorf("went from %d to %d, want 3 -> 4", res.From, res.To)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s changed by a stamp-only migration:\n got %s\nwant %s", name, got, want)
		}
	}
}

// #1202 C.7: with the schema at 4, a build whose CurrentSchema() is 3
// (a v0.6.x release, simulated by the first three entries of the list)
// is refused with SchemaTooNewError, and nothing on disk changes. Paired
// with the same build opening a directory it does understand.
func TestDowngradeFromSchema4IsRefused(t *testing.T) {
	dir := t.TempDir()
	writeStoreFiles(t, dir)
	stampForTest(t, dir, 3, "v0.6.1")
	if _, err := MigrateFileSchema(context.Background(), dir, "v0.7.0-test"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if stored, _, err := ReadFileSchema(dir); err != nil || stored != 4 {
		t.Fatalf("schema after the upgrade = %d, %v; want 4", stored, err)
	}

	before := dirHash(t, dir)
	older := schemaMigrations[:3]
	_, err := migrateFileSchema(context.Background(), dir, "v0.6.1", older)
	var tooNew *SchemaTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("an older build opened schema-4 data: err = %v, want a *SchemaTooNewError", err)
	}
	if tooNew.Stored != 4 || tooNew.Known != 3 {
		t.Errorf("stored/known = %d/%d, want 4/3", tooNew.Stored, tooNew.Known)
	}
	if after := dirHash(t, dir); after != before {
		t.Error("the refusal changed the data directory")
	}

	// The pairing: the same older build is fine on schema-3 data.
	old := t.TempDir()
	writeStoreFiles(t, old)
	stampForTest(t, old, 3, "v0.6.1")
	if _, err := migrateFileSchema(context.Background(), old, "v0.6.1", older); err != nil {
		t.Errorf("the older build refused its own schema: %v", err)
	}
}
