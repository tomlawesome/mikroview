// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAll(t *testing.T, path string, key *Key, ff FrameFormat) ([]string, error) {
	t.Helper()
	var got []string
	err := ReadFrames(path, key, ff, func(_ uint64, p []byte) error {
		got = append(got, string(p))
		return nil
	})
	return got, err
}

// SealFrames output reads back in order; the wrong key is refused as a
// frame that did not open, naming the first frame.
func TestSealFramesRoundTripAndWrongKey(t *testing.T) {
	ff := smallFormat()
	key := testKey(t)
	raw, err := SealFrames(key, ff, [][]byte{[]byte("one"), []byte("two")})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "f.mvevt")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readAll(t, path, key, ff)
	if err != nil || len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %q, %v; want [one two], nil", got, err)
	}

	other, err := NewKeyFromMaterial([]byte(strings.Repeat("z", MinKeyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	got, err = readAll(t, path, other, ff)
	if !errors.Is(err, ErrFrameDidNotOpen) || len(got) != 0 {
		t.Fatalf("wrong key: got %q, %v; want ErrFrameDidNotOpen and no frames", got, err)
	}
	if !strings.Contains(err.Error(), "frame 0") {
		t.Errorf("error does not name the frame: %v", err)
	}

	boom := errors.New("stop")
	err = ReadFrames(path, key, ff, func(seq uint64, _ []byte) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("visit error: got %v, want it returned", err)
	}

	if _, err := SealFrames(nil, ff, nil); err == nil {
		t.Error("SealFrames with no key succeeded")
	}
}

func TestReadFramesRefusals(t *testing.T) {
	ff := smallFormat()
	key := testKey(t)
	dir := t.TempDir()

	if _, err := readAll(t, filepath.Join(dir, "missing"), key, ff); err == nil || !strings.Contains(err.Error(), "opening") {
		t.Errorf("missing file: %v; want an opening error", err)
	}
	if _, err := readAll(t, filepath.Join(dir, "missing"), nil, ff); err == nil || !strings.Contains(err.Error(), "needs a key") {
		t.Errorf("no key: %v; want a needs-a-key error", err)
	}

	foreign := filepath.Join(dir, "foreign")
	os.WriteFile(foreign, bytes.Repeat([]byte("x"), 40), 0o600)
	if _, err := readAll(t, foreign, key, ff); err == nil || !strings.Contains(err.Error(), "not a ") {
		t.Errorf("foreign file: %v; want a not-a-<format> error", err)
	}

	badVer := filepath.Join(dir, "badver")
	hdr := append([]byte(ff.Magic), ff.Version+1)
	hdr = append(hdr, make([]byte, saltBytes+4)...)
	os.WriteFile(badVer, hdr, 0o600)
	if _, err := readAll(t, badVer, key, ff); err == nil || !strings.Contains(err.Error(), "unsupported file format version") {
		t.Errorf("wrong version: %v; want an unsupported-version error", err)
	}

	// A header torn by a crash is an empty log, not an error.
	torn := filepath.Join(dir, "torn")
	os.WriteFile(torn, make([]byte, ff.headerBytes()), 0o600)
	if got, err := readAll(t, torn, key, ff); err != nil || len(got) != 0 {
		t.Errorf("torn header: got %q, %v; want nothing, nil", got, err)
	}
}

// Reopening a log recovers its frame count and drops a partial tail; the
// next append lands on the boundary and every frame reads back.
func TestOpenFrameLogReopenDropsPartialTail(t *testing.T) {
	ff := smallFormat()
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "f.mvevt")
	l, err := OpenFrameLog(path, key, ff)
	if err != nil {
		t.Fatal(err)
	}
	if l.Path() != path {
		t.Errorf("Path = %q, want %q", l.Path(), path)
	}
	if err := l.Append([]byte("a")); err != nil {
		t.Fatal(err)
	}
	l.Close()
	whole, _ := os.ReadFile(path)
	// A length that runs past the end of the file: a crash tail.
	os.WriteFile(path, append(append([]byte{}, whole...), 0, 0, 0, 40, 1, 2, 3), 0o600)

	l, err = OpenFrameLog(path, key, ff)
	if err != nil {
		t.Fatal(err)
	}
	if l.Frames() != 1 {
		t.Errorf("Frames = %d, want 1", l.Frames())
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, whole) {
		t.Error("the partial tail was not cut off")
	}
	if err := l.Append([]byte("b")); err != nil {
		t.Fatal(err)
	}
	l.Close()
	got, err := readAll(t, path, key, ff)
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %q, %v; want [a b], nil", got, err)
	}
}
