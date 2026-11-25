// SPDX-License-Identifier: AGPL-3.0-only

package recoverykeys

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// #1489: a filesystem that refuses hard links must still get a pepper.

func seamLink(t *testing.T, fn func(oldname, newname string) error) {
	t.Helper()
	orig := linkPepperFile
	t.Cleanup(func() { linkPepperFile = orig })
	linkPepperFile = fn
}

func TestPepperIsCreatedWhereHardLinksAreUnsupported(t *testing.T) {
	for name, errno := range map[string]error{
		"ENOTSUP": syscall.ENOTSUP,
		"EPERM":   syscall.EPERM,
	} {
		t.Run(name, func(t *testing.T) {
			seamLink(t, func(string, string) error { return errno })
			path := filepath.Join(t.TempDir(), "pepper")
			got, err := loadOrCreatePepper(path)
			if err != nil {
				t.Fatalf("loadOrCreatePepper: %v", err)
			}
			if len(got) != 32 {
				t.Fatalf("pepper is %d bytes, want 32", len(got))
			}
			assertFileHoldsPepper(t, path, got)
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
			}
			assertNoTempSiblings(t, path)
			again, err := loadOrCreatePepper(path)
			if err != nil {
				t.Fatalf("second call: %v", err)
			}
			if !bytes.Equal(again, got) {
				t.Fatalf("second call returned %x, want %x", again, got)
			}
		})
	}
}

func TestPepperWithoutHardLinksLostRaceReadsTheirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pepper")
	theirs := bytes.Repeat([]byte{0x5c}, 32)
	seamLink(t, func(string, string) error {
		if err := os.WriteFile(path, []byte(hex.EncodeToString(theirs)), 0o600); err != nil {
			t.Errorf("rival write: %v", err)
		}
		return syscall.ENOTSUP
	})
	got, err := loadOrCreatePepper(path)
	if err != nil {
		t.Fatalf("loadOrCreatePepper: %v", err)
	}
	if !bytes.Equal(got, theirs) {
		t.Fatalf("returned %x, want the rival's %x", got, theirs)
	}
	assertFileHoldsPepper(t, path, theirs)
	assertNoTempSiblings(t, path)
}

func TestPepperWithoutHardLinksAndFinalCreateFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pepper")
	seamLink(t, func(string, string) error { return syscall.ENOTSUP })
	orig := openPepperFile
	t.Cleanup(func() { openPepperFile = orig })
	openPepperFile = func(n string, flag int, perm os.FileMode) (pepperFile, error) {
		if n == path {
			return nil, errors.New("create failed")
		}
		return os.OpenFile(n, flag, perm)
	}
	if _, err := loadOrCreatePepper(path); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pepper file present after failure (stat err = %v)", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left in the data dir after failure: %v", left)
	}
}
