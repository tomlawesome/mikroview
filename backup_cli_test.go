// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// TestRestoreOverwritesACorruptStoreDocument is issue #378's other
// Done-when: fail-closed on an unreadable-but-present document must not
// lock an operator out of the one recovery path it recommends.
// -restore is dispatched (main.go's os.Args[1] == "-restore" branch)
// before openStorage or any store's OpenWithBackend ever runs, and
// writes each store's file directly (os.WriteFile + os.Rename) rather
// than going through auth.OpenWithBackend/persist.Open -- so a document
// that would make mustOpenStore refuse a normal boot must still be a
// document -restore can replace.
//
// This pins that behaviour end to end: an accounts file already
// corrupted on disk (auth.Open on it fails, exactly as mustOpenStore
// would refuse to boot on it) is successfully overwritten by
// `mikroview -restore <file> --force`, and the result is a store that
// opens cleanly afterwards.
func TestRestoreOverwritesACorruptStoreDocument(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "users.json")

	corrupt := []byte("{not valid json")
	if err := os.WriteFile(authPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	// Confirms the premise: this file really is the "refuses to boot"
	// case the rest of #378 is about.
	if _, err := auth.Open(authPath); err == nil {
		t.Fatal("test setup: expected the corrupt accounts file to fail auth.Open")
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	restored := []byte(`{"users":[]}`)
	backupPath := filepath.Join(dir, "mikroview.backup")
	if err := writeBackup(backupPath, true, map[string][]byte{"auth": restored}); err != nil {
		t.Fatalf("writeBackup: %v", err)
	}

	if code := runRestore([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runRestore(--force) = %d, want 0", code)
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	// backup.Write's own envelope encoding re-indents the nested raw
	// JSON (whitespace only), so compare the parsed shape rather than
	// raw bytes -- what matters here is that the corrupt content is
	// gone and the restored document is what was backed up.
	if strings.Contains(string(got), "not valid json") {
		t.Errorf("accounts file after restore still contains the corrupt content: %q", got)
	}
	var gotFile, wantFile struct {
		Users []struct{} `json:"users"`
	}
	if err := json.Unmarshal(got, &gotFile); err != nil {
		t.Fatalf("accounts file after restore is not valid JSON: %v (%q)", err, got)
	}
	if err := json.Unmarshal(restored, &wantFile); err != nil {
		t.Fatal(err)
	}
	if len(gotFile.Users) != len(wantFile.Users) {
		t.Errorf("accounts file after restore has %d users, want %d", len(gotFile.Users), len(wantFile.Users))
	}

	// The whole point of the remedy: the store opens cleanly now.
	s, err := auth.Open(authPath)
	if err != nil {
		t.Fatalf("auth.Open after restore: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("expected the restored (empty) accounts store, got %d accounts", s.Count())
	}
}

// TestRestoreWithoutForceRefusesToOverwriteAnExistingCorruptFile pins
// the companion case: without --force, -restore still refuses to
// overwrite an existing file, corrupt or not -- that guard is about
// "don't clobber live state by accident," unrelated to and unaffected by
// #378's fail-closed startup policy. The operator's way past it is the
// documented --force, exercised by
// TestRestoreOverwritesACorruptStoreDocument above.
func TestRestoreWithoutForceRefusesToOverwriteAnExistingCorruptFile(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "users.json")
	corrupt := []byte("{not valid json")
	if err := os.WriteFile(authPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	backupPath := filepath.Join(dir, "mikroview.backup")
	if err := writeBackup(backupPath, true, map[string][]byte{"auth": []byte(`{"users":[]}`)}); err != nil {
		t.Fatalf("writeBackup: %v", err)
	}

	if code := runRestore([]string{backupPath}); code == 0 {
		t.Fatal("runRestore() without --force succeeded against an existing file, want a refusal")
	}

	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(corrupt) {
		t.Error("the existing file was modified despite the refusal")
	}
}

// TestBackupRestoreCarriesSchemaSoNoMigrationReruns is #1244's Done-when:
// a deployment already migrated to this build's current schema must come
// back out of a restore stamped at that same schema, not as an
// unstamped (schema 0) fresh install -- schema.json was never on
// backedUpStores' list, so a restore into an empty data directory used
// to read as schema 0 and hand upgradeDataDirSchema a data directory
// that looks like it has never been migrated.
//
// This pins both halves: the restored schema document matches what was
// backed up, and the next start's upgrade check does not rewrite it --
// there is nothing pending, because there is nothing to redo.
func TestBackupRestoreCarriesSchemaSoNoMigrationReruns(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	authPath := filepath.Join(srcDir, "users.json")
	if err := os.WriteFile(authPath, []byte(`{"users":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// An ordinary, already-migrated deployment: stamped at this build's
	// current schema before any backup is taken.
	if _, err := persist.MigrateFileSchema(ctx, srcDir, version); err != nil {
		t.Fatalf("MigrateFileSchema (seeding the source data directory): %v", err)
	}
	wantSchema, wantVersion, err := persist.ReadFileSchema(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	if wantSchema == 0 {
		t.Fatal("test setup: the source data directory did not actually get stamped")
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	backupPath := filepath.Join(srcDir, "mikroview.backup")
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	// Restore into a fresh directory -- a disaster recovery onto a new
	// host, or the same host with its data directory gone.
	dstDir := t.TempDir()
	newAuthPath := filepath.Join(dstDir, "users.json")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", newAuthPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	gotSchema, gotVersion, err := persist.ReadFileSchema(dstDir)
	if err != nil {
		t.Fatalf("ReadFileSchema after restore: %v", err)
	}
	if gotSchema != wantSchema || gotVersion != wantVersion {
		t.Fatalf("restored schema = %d written by %q, want %d written by %q -- "+
			"a restore of already-migrated stores must not read as a fresh, unstamped install",
			gotSchema, gotVersion, wantSchema, wantVersion)
	}

	before, err := os.ReadFile(filepath.Join(dstDir, persist.SchemaDocumentName))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := upgradeDataDirSchema(cfg); err != nil {
		t.Fatalf("upgradeDataDirSchema after restore: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dstDir, persist.SchemaDocumentName))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the next start's upgrade check rewrote schema.json after a restore that was already "+
			"at the current schema -- it should have found nothing pending:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestRestoreRefusesASchemaDocumentNewerThanThisBuildKnows is the
// downgrade guard applied to a restore rather than a boot (#1244):
// restoring a bundle stamped by a newer build must be the same refusal
// as opening one, with nothing written, not a silent adoption of a
// schema number this build does not know how to interpret.
func TestRestoreRefusesASchemaDocumentNewerThanThisBuildKnows(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "users.json")

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	tooNew, err := json.Marshal(struct {
		Schema  int64  `json:"schema"`
		Version string `json:"version"`
	}{Schema: persist.CurrentSchema() + 1, Version: "v42.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(dir, "mikroview.backup")
	if err := writeBackup(backupPath, true, map[string][]byte{
		"auth":          []byte(`{"users":[]}`),
		schemaStoreName: tooNew,
	}); err != nil {
		t.Fatalf("writeBackup: %v", err)
	}

	if code := runRestore([]string{backupPath, "--force"}); code == 0 {
		t.Fatal("runRestore with a schema document newer than this build knows succeeded, want a refusal")
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Error("runRestore wrote the auth store despite refusing on the schema document -- nothing should have changed")
	}
	if _, err := os.Stat(filepath.Join(dir, persist.SchemaDocumentName)); !os.IsNotExist(err) {
		t.Error("runRestore wrote schema.json despite refusing")
	}
}

// TestWriteVaultBundleRefusesPathTraversal pins gosec G703's finding on
// pipeline 458: writeVaultBundle used to filepath.Join(dir, rel) with rel
// taken straight from the restore envelope, so a hostile envelope entry
// named "../escape" would land one directory above the vault instead of
// inside it. vaultPath is the fix -- this confirms the entry is refused,
// by name, and that refusing it never touches disk outside dir.
func TestWriteVaultBundleRefusesPathTraversal(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "vault")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	bundle := vaultBundle{Files: map[string][]byte{
		"../escape": []byte("hostile"),
	}}
	err := writeVaultBundle(dir, bundle)
	if err == nil {
		t.Fatal("writeVaultBundle with a \"../escape\" entry succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "../escape") {
		t.Errorf("error %q does not name the offending entry", err.Error())
	}

	if _, statErr := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(statErr) {
		t.Error("the traversal entry was written outside the vault directory")
	}
}

// TestWriteVaultBundleRefusesAbsolutePath is
// TestWriteVaultBundleRefusesPathTraversal's companion: an envelope entry
// naming an absolute path is refused the same way, rather than being
// joined (filepath.Join treats a second absolute-looking argument as
// just another path component, but there is no reason to trust an
// envelope entry that claims to be one).
func TestWriteVaultBundleRefusesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "planted")

	bundle := vaultBundle{Files: map[string][]byte{
		target: []byte("hostile"),
	}}
	if err := writeVaultBundle(dir, bundle); err == nil {
		t.Fatal("writeVaultBundle with an absolute entry succeeded, want a refusal")
	}

	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("the absolute-path entry was written to the path it named")
	}
}

// TestVaultBundleRoundTrip is the ordinary case
// TestWriteVaultBundleRefusesPathTraversal and
// TestWriteVaultBundleRefusesAbsolutePath must not have broken: a normal,
// nested entry name still writes under dir and reads back byte for byte.
func TestVaultBundleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const rel = "router/gen-1.backup"
	want := []byte("sealed router backup bytes")

	if err := writeVaultBundle(dir, vaultBundle{Files: map[string][]byte{rel: want}}); err != nil {
		t.Fatalf("writeVaultBundle: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dir, "router", "gen-1.backup")); err != nil {
		t.Fatalf("reading written file: %v", err)
	} else if string(got) != string(want) {
		t.Errorf("written file = %q, want %q", got, want)
	}

	bundle, err := readVaultBundle(dir)
	if err != nil {
		t.Fatalf("readVaultBundle: %v", err)
	}
	if got := bundle.Files[rel]; string(got) != string(want) {
		t.Errorf("readVaultBundle()[%q] = %q, want %q", rel, got, want)
	}
}

// TestRestoreWritesEveryFileAtomically guards what a unit test cannot
// otherwise see: a torn file needs the process to die between the open
// and the last byte, which no test can stage. os.WriteFile truncates
// the destination first and writes in place, so a crash there leaves a
// short file where a router backup used to be -- discovered at the one
// moment it is needed. persist.WriteFileAtomic writes a temp file,
// fsyncs and renames, so the destination is either the old bytes or
// the new ones and never half of either. Every other publish in this
// file already goes that way; writeVaultBundle was the one that did
// not (found by the v0.6.0 audit's Safety stage, #1257).
func TestRestoreWritesEveryFileAtomically(t *testing.T) {
	src, err := os.ReadFile("backup_cli.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, "os.WriteFile(") {
			t.Errorf("backup_cli.go:%d writes in place: %s\n\twant persist.WriteFileAtomic, so a crash mid-write cannot leave a short file",
				i+1, strings.TrimSpace(line))
		}
	}
}
