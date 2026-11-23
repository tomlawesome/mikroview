// SPDX-License-Identifier: AGPL-3.0-only

package recoverykeys

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// #1484: first-time creation of the pepper file must check Sync and Close
// errors before returning the pepper.
//
// SEAM THE BUILDER MUST ADD (package recoverykeys, recovery.go): an interface
//
//	type pepperFile interface { io.Writer; io.StringWriter; Sync() error; Close() error }
//
// and a package var
//
//	var openPepperFile = func(name string, flag int, perm os.FileMode) (pepperFile, error) { return os.OpenFile(name, flag, perm) }
//
// that loadOrCreatePepper uses in place of os.OpenFile.
type failingPepperFile struct {
	*os.File
	syncErr, closeErr error
}

func (f *failingPepperFile) Sync() error {
	_ = f.File.Sync()
	return f.syncErr
}

func (f *failingPepperFile) Close() error {
	_ = f.File.Close()
	return f.closeErr
}

var _ io.Writer = (*failingPepperFile)(nil)

func TestPepperCreationFailsWhenSyncOrCloseFails(t *testing.T) {
	for name, tc := range map[string]struct{ syncErr, closeErr error }{
		"close": {nil, errors.New("close failed: disk full")},
		"sync":  {errors.New("sync failed: I/O error"), nil},
	} {
		t.Run(name, func(t *testing.T) {
			orig := openPepperFile
			t.Cleanup(func() { openPepperFile = orig })
			openPepperFile = func(n string, flag int, perm os.FileMode) (pepperFile, error) {
				f, err := os.OpenFile(n, flag, perm)
				if err != nil {
					return nil, err
				}
				return &failingPepperFile{File: f, syncErr: tc.syncErr, closeErr: tc.closeErr}, nil
			}
			path := filepath.Join(t.TempDir(), "pepper")
			p, err := loadOrCreatePepper(path)
			if err == nil {
				t.Fatal("loadOrCreatePepper returned nil error although sync/close failed")
			}
			if p != nil {
				t.Fatalf("loadOrCreatePepper returned a pepper (%d bytes) alongside error %v", len(p), err)
			}
			want := tc.syncErr
			if want == nil {
				want = tc.closeErr
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want it to wrap %v", err, want)
			}
		})
	}
}

func TestPepperCreationFailureLeavesNoFile(t *testing.T) {
	for name, tc := range map[string]struct{ syncErr, closeErr error }{
		"close": {nil, errors.New("close failed")},
		"sync":  {errors.New("sync failed"), nil},
	} {
		t.Run(name, func(t *testing.T) {
			orig := openPepperFile
			t.Cleanup(func() { openPepperFile = orig })
			openPepperFile = func(n string, flag int, perm os.FileMode) (pepperFile, error) {
				f, err := os.OpenFile(n, flag, perm)
				if err != nil {
					return nil, err
				}
				return &failingPepperFile{File: f, syncErr: tc.syncErr, closeErr: tc.closeErr}, nil
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "pepper")
			if _, err := loadOrCreatePepper(path); err == nil {
				t.Fatal("expected error")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("pepper file still present after failure (stat err = %v)", err)
			}
			// #1489: the temp file is cleaned up too.
			if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
				t.Fatalf("files left in the data dir after failure: %v", left)
			}
		})
	}
}
