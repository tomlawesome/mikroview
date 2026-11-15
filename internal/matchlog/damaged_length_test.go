// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// #1470 review: a damaged length in the middle of the file is damage, not
// a crash tail. Open must refuse it and leave the file exactly as it was,
// never cut it back to the frame before and so delete the whole frames
// after it.
func TestDamagedMiddleFrameLengthIsRefusedAndNothingIsCut(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    uint32
		pad  int // bytes appended so the declared length sits inside the file
	}{
		{"zero", 0, 0},
		{"shorter than a nonce", 5, 0},
		{"over the ceiling", uint32(Format.MaxFrameBytes + 1), Format.MaxFrameBytes + 1},
		{"over the ceiling and past the end of the file", uint32(Format.MaxFrameBytes + 1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := testKey(t)
			path, b := writeN(t, key, 3)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint32(raw[b[1]:b[1]+4], tc.n) // frame 2's length
			raw = append(raw, make([]byte, tc.pad)...)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}

			if s, err := Open(path, 100, key); err == nil {
				s.Close()
				t.Error("Open accepted a log with a damaged middle frame length")
			}
			if _, err := ReadLines(path, key); err == nil {
				t.Error("ReadLines accepted a log with a damaged middle frame length")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, raw) {
				t.Errorf("the file was changed: %d bytes, was %d", len(got), len(raw))
			}
		})
	}
}
