// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/engine"
)

// #1483: the backup file's Sync and Close errors must be checked before
// -backup reports success.
//
// SEAM THE BUILDER MUST ADD (package main, backup_cli.go): an interface
//
//	type backupFile interface { io.Writer; Sync() error; Close() error }
//
// and a package var
//
//	var openBackupFile = func(name string, flag int, perm os.FileMode) (backupFile, error) { return os.OpenFile(name, flag, perm) }
//
// that writeBackup uses in place of os.OpenFile.
type failingBackupFile struct {
	*os.File
	syncErr, closeErr error
}

func (f *failingBackupFile) Sync() error {
	_ = f.File.Sync()
	return f.syncErr
}

func (f *failingBackupFile) Close() error {
	_ = f.File.Close()
	return f.closeErr
}

var _ io.Writer = (*failingBackupFile)(nil)

func injectBackupFile(t *testing.T, syncErr, closeErr error) {
	t.Helper()
	orig := openBackupFile
	t.Cleanup(func() { openBackupFile = orig })
	openBackupFile = func(name string, flag int, perm os.FileMode) (backupFile, error) {
		f, err := os.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		return &failingBackupFile{File: f, syncErr: syncErr, closeErr: closeErr}, nil
	}
}

func TestWriteBackupReturnsCloseAndSyncErrors(t *testing.T) {
	for name, tc := range map[string]struct{ syncErr, closeErr error }{
		"close": {nil, errors.New("close failed: disk full")},
		"sync":  {errors.New("sync failed: I/O error"), nil},
	} {
		t.Run(name, func(t *testing.T) {
			injectBackupFile(t, tc.syncErr, tc.closeErr)
			dest := filepath.Join(t.TempDir(), "x.backup")
			err := writeBackup(dest, true, map[string][]byte{"auth": []byte(`{"users":[]}`)})
			if err == nil {
				t.Fatal("writeBackup returned nil although sync/close failed")
			}
			want := tc.syncErr
			if want == nil {
				want = tc.closeErr
			}
			if !errors.Is(err, want) && !strings.Contains(err.Error(), want.Error()) {
				t.Fatalf("writeBackup error = %v, want it to carry %v", err, want)
			}
		})
	}
}

func TestRunBackupFailsAndDoesNotReportSuccessWhenCloseFails(t *testing.T) {
	srcDir := t.TempDir()
	definitionsPath := filepath.Join(srcDir, "definitions.json")
	defs, err := engine.OpenDefinitionsStore(definitionsPath)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore: %v", err)
	}
	if err := engine.SeedShippedDefinitions(defs, engine.DefaultDetectorSettings(), engine.DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	if err := defs.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH", definitionsPath)

	injectBackupFile(t, nil, errors.New("close failed: disk full"))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	code := runBackup([]string{filepath.Join(srcDir, "mikroview.backup"), "--force"})
	os.Stdout = origStdout
	_ = w.Close()
	out, _ := io.ReadAll(r)

	if code == 0 {
		t.Fatal("runBackup = 0, want non-zero when the backup file's close fails")
	}
	if strings.Contains(string(out), "Treat it exactly") {
		t.Fatalf("runBackup printed its success notice despite the failure: %q", out)
	}
}
