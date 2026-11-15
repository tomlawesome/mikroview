// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingReaderAt fails every read, standing in for a disk that errors
// while the tail of a file is being inspected.
type failingReaderAt struct{ err error }

func (r failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func smallFormat() FrameFormat {
	ff := dayFormat("2026-09-03")
	ff.MaxFrameBytes = 64
	return ff
}

// A zero length whose tail cannot be read is an error naming the file and
// offset, not a torn tail: the tail's state is unknown.
func TestCheckFrameLengthTailReadFailure(t *testing.T) {
	boom := errors.New("disk gone")
	torn, err := checkFrameLength(failingReaderAt{boom}, "x.mvevt", 3, 40, 60, 0, smallFormat())
	if torn || !errors.Is(err, boom) {
		t.Fatalf("torn=%v err=%v; want not torn, wrapping the read error", torn, err)
	}
	if !strings.Contains(err.Error(), "x.mvevt") || !strings.Contains(err.Error(), "offset 40") {
		t.Errorf("error does not name file and offset: %v", err)
	}
}

// A declared length too short to hold a nonce, or above the ceiling, is
// damage and refused as a frame that did not open.
func TestCheckFrameLengthRefusesImplausibleLengths(t *testing.T) {
	ff := smallFormat()
	data := make([]byte, 400)
	for name, n := range map[string]int64{
		"shorter than a nonce": 5,
		"exactly a nonce":      nonceBytes,
		"over the ceiling":     int64(ff.MaxFrameBytes) + 1,
	} {
		torn, err := checkFrameLength(bytes.NewReader(data), "x.mvevt", 2, 0, int64(len(data)), n, ff)
		if torn || !errors.Is(err, ErrFrameDidNotOpen) {
			t.Errorf("%s: torn=%v err=%v; want ErrFrameDidNotOpen", name, torn, err)
		}
	}
}

// zeroFilledTail: a zero run longer than one frame can be is damage, and
// a zero run with a non-zero byte after it is damage; a read error is
// reported.
func TestZeroFilledTail(t *testing.T) {
	ff := smallFormat()
	maxRun := 4 + int64(ff.MaxFrameBytes)

	long := make([]byte, maxRun+1)
	if z, err := zeroFilledTail(bytes.NewReader(long), 0, int64(len(long)), ff); z || err != nil {
		t.Errorf("run longer than a frame: zero=%v err=%v; want false, nil", z, err)
	}
	exact := make([]byte, maxRun)
	if z, err := zeroFilledTail(bytes.NewReader(exact), 0, int64(len(exact)), ff); !z || err != nil {
		t.Errorf("run of exactly one frame: zero=%v err=%v; want true, nil", z, err)
	}
	dirty := make([]byte, 30)
	dirty[29] = 1
	if z, err := zeroFilledTail(bytes.NewReader(dirty), 0, int64(len(dirty)), ff); z || err != nil {
		t.Errorf("zeros then a non-zero byte: zero=%v err=%v; want false, nil", z, err)
	}
	boom := errors.New("disk gone")
	if z, err := zeroFilledTail(failingReaderAt{boom}, 0, 30, ff); z || !errors.Is(err, boom) {
		t.Errorf("read failure: zero=%v err=%v; want false, the read error", z, err)
	}
}

// scanFrames on a handle it cannot size reports that, and a file with a
// zero length followed by non-zero bytes is refused rather than cut.
func TestScanFramesErrors(t *testing.T) {
	ff := smallFormat()
	dir := t.TempDir()

	closed, err := os.Create(filepath.Join(dir, "closed"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, _, _, err := scanFrames(closed, ff); err == nil || !strings.Contains(err.Error(), "sizing file") {
		t.Errorf("closed handle: err=%v; want a sizing error", err)
	}

	// A good header, then a zero length with a stray byte after it.
	path := filepath.Join(dir, "damaged.mvevt")
	l, err := OpenFrameLog(path, testKey(t), ff)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	b, _ := os.ReadFile(path)
	b = binary.BigEndian.AppendUint32(b, 0)
	b = append(b, 0, 0, 7)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, _, err := scanFrames(f, ff); !errors.Is(err, ErrFrameDidNotOpen) {
		t.Errorf("zero length then data: err=%v; want ErrFrameDidNotOpen", err)
	}
	// OpenFrameLog must refuse it too and leave the file as it was.
	if _, err := OpenFrameLog(path, testKey(t), ff); !errors.Is(err, ErrFrameDidNotOpen) {
		t.Errorf("OpenFrameLog: err=%v; want ErrFrameDidNotOpen", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, b) {
		t.Error("the refused file was changed")
	}
}
