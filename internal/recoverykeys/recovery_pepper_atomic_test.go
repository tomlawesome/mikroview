// SPDX-License-Identifier: AGPL-3.0-only

package recoverykeys

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// #1485 #1489: the pepper path must only ever appear holding a complete
// pepper. These tests know only that a temp file lives in the same directory
// and matches "<path>.tmp-*"; they do not depend on how it is published.

// hookPepperFile wraps the real file and runs hooks before WriteString/Sync.
type hookPepperFile struct {
	*os.File
	onWrite, onSync func()
}

func (f *hookPepperFile) WriteString(s string) (int, error) {
	if f.onWrite != nil {
		f.onWrite()
	}
	return f.File.WriteString(s)
}

func (f *hookPepperFile) Sync() error {
	if f.onSync != nil {
		f.onSync()
	}
	return f.File.Sync()
}

// seamOpen installs an openPepperFile that records the opened name and wraps
// the file with the given hooks.
func seamOpen(t *testing.T, names *[]string, onWrite, onSync func()) {
	t.Helper()
	orig := openPepperFile
	t.Cleanup(func() { openPepperFile = orig })
	openPepperFile = func(n string, flag int, perm os.FileMode) (pepperFile, error) {
		*names = append(*names, n)
		f, err := os.OpenFile(n, flag, perm)
		if err != nil {
			return nil, err
		}
		return &hookPepperFile{File: f, onWrite: onWrite, onSync: onSync}, nil
	}
}

func assertFileHoldsPepper(t *testing.T, path string, want []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading pepper file: %v", err)
	}
	if len(raw) != 64 {
		t.Fatalf("pepper file is %d bytes, want 64", len(raw))
	}
	got, err := hex.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("pepper file is not hex: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("pepper file holds %x, want %x", got, want)
	}
}

func assertNoTempSiblings(t *testing.T, path string) {
	t.Helper()
	left, err := filepath.Glob(path + ".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestPepperIsNeverVisibleBeforeItIsComplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pepper")
	var names []string
	var visible []string
	check := func(where string) func() {
		return func() {
			if _, err := os.Stat(path); err == nil {
				visible = append(visible, "pepper visible at path while still being written (during "+where+")")
			}
		}
	}
	seamOpen(t, &names, check("WriteString"), check("Sync"))

	p, err := loadOrCreatePepper(path)
	if err != nil {
		t.Fatalf("loadOrCreatePepper: %v", err)
	}
	for _, v := range visible {
		t.Error(v)
	}
	if len(names) != 1 {
		t.Fatalf("opened %d files, want 1: %v", len(names), names)
	}
	if names[0] == path {
		t.Errorf("pepper was written in place at %s, want a temp file", path)
	}
	if filepath.Dir(names[0]) != dir {
		t.Errorf("temp %s is not in the data dir %s", names[0], dir)
	}
	if len(p) != 32 {
		t.Fatalf("pepper is %d bytes, want 32", len(p))
	}
	assertFileHoldsPepper(t, path, p)
	assertNoTempSiblings(t, path)
}

func TestPepperCrashMidWriteLeavesNoFileAtPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pepper")
	var names []string
	seamOpen(t, &names, func() { panic("simulated crash mid-write") }, nil)

	func() {
		defer func() { _ = recover() }()
		_, _ = loadOrCreatePepper(path)
	}()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a crash mid-write left something at the pepper path (stat err = %v)", err)
	}

	// The next, undisturbed run must repair itself.
	orig := openPepperFile
	openPepperFile = func(n string, flag int, perm os.FileMode) (pepperFile, error) {
		return os.OpenFile(n, flag, perm)
	}
	t.Cleanup(func() { openPepperFile = orig })
	p, err := loadOrCreatePepper(path)
	if err != nil {
		t.Fatalf("second run after the crash: %v", err)
	}
	if len(p) != 32 {
		t.Fatalf("pepper is %d bytes, want 32", len(p))
	}
	assertFileHoldsPepper(t, path, p)
}

func TestPepperExistingFileIsReadNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pepper")
	want := bytes.Repeat([]byte{0xab}, 32)
	content := []byte(hex.EncodeToString(want))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadOrCreatePepper(path)
	if err != nil {
		t.Fatalf("loadOrCreatePepper: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("returned %x, want %x", got, want)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatalf("file changed: %q (err %v)", after, err)
	}
	assertNoTempSiblings(t, path)
}

// Post-fix guard: at the current head the path itself is opened O_EXCL, so
// the other writer's os.WriteFile truncates our in-progress file and this
// test fails there (we return our own pepper, the file holds theirs).
func TestPepperLostRaceReadsTheirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pepper")
	theirs := bytes.Repeat([]byte{0x5c}, 32)
	var names []string
	done := false
	seamOpen(t, &names, nil, func() {
		if done {
			return
		}
		done = true
		if err := os.WriteFile(path, []byte(hex.EncodeToString(theirs)), 0o600); err != nil {
			t.Errorf("rival write: %v", err)
		}
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

func TestPepperConcurrentFirstCreationAgrees(t *testing.T) {
	const n = 8
	path := filepath.Join(t.TempDir(), "pepper")
	start := make(chan struct{})
	peppers := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			peppers[i], errs[i] = loadOrCreatePepper(path)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("creator %d: %v", i, errs[i])
			continue
		}
		if len(peppers[i]) != 32 {
			t.Errorf("creator %d: pepper is %d bytes, want 32", i, len(peppers[i]))
		}
		if !bytes.Equal(peppers[i], peppers[0]) && errs[0] == nil {
			t.Errorf("creator %d returned %x, creator 0 returned %x", i, peppers[i], peppers[0])
		}
	}
	if t.Failed() {
		return
	}
	assertFileHoldsPepper(t, path, peppers[0])
	assertNoTempSiblings(t, path)
}
