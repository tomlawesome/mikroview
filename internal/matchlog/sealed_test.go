// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
)

// #1470: the match log is a sealed, framed, append-only file. These tests
// are written from docs design note 1470 (header "MVMLG" + version + salt,
// one AES-GCM frame per line, AAD binding store name and position). They
// use only the public API and measure frame boundaries from file sizes, so
// they do not depend on the byte layout of a frame.

const (
	sealedMagic = "MVMLG"
	hdrEnd      = len(sealedMagic) + 1 + 16
)

func testKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	return k
}

func otherKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("z", retention.MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	return k
}

const (
	evIP   = "198.51.100.77"
	evMAC  = "52:54:00:ab:cd:ef"
	evRaw  = "firewall,info forward: in:ether1 out:ether2, src-mac 52:54:00:ab:cd:ef, proto TCP, 198.51.100.77:5555->203.0.113.9:443"
	evEnt  = "entry-secret-id"
	evDest = "203.0.113.9"
)

func secretEvent() store.Event {
	return store.Event{Action: store.ActionAccept, SrcIP: evIP, DstIP: evDest, Raw: evRaw}
}

func secretTuple(port int) Tuple {
	return Tuple{Source: Identity{MAC: evMAC, IP: evIP}, DestIP: evDest, Port: port}
}

func size(t *testing.T, path string) int {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(fi.Size())
}

// writeN appends n distinct records to a fresh sealed log and returns the
// file path and the byte offset at which each frame ends (bounds[0] is the
// end of the header, bounds[i] the end of frame i).
func writeN(t *testing.T, key *retention.Key, n int) (string, []int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s, err := Open(path, 100, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bounds := []int{size(t, path)}
	if bounds[0] != hdrEnd {
		t.Fatalf("a new log is %d bytes, want just the %d-byte header", bounds[0], hdrEnd)
	}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		if err := s.Append(evEnt, secretTuple(1000+i), secretEvent(), t0.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		bounds = append(bounds, size(t, path))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path, bounds
}

func allRecords(t *testing.T, s *FileStore) []Record {
	t.Helper()
	return collect(t, s, Query{Source: Identity{MAC: evMAC, IP: evIP}})
}

func TestKeyedLogOnDiskHoldsNoPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s, err := Open(path, 100, testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	tup := secretTuple(443)
	if err := s.Append(evEnt, tup, secretEvent(), t0); err != nil {
		t.Fatal(err)
	}
	// Same tuple again: an "update" line.
	if err := s.Append(evEnt, tup, secretEvent(), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertSealed(t, path)

	// After Reset and a fresh record.
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(evEnt, secretTuple(444), secretEvent(), t0); err != nil {
		t.Fatal(err)
	}
	assertSealed(t, path)
}

func assertSealed(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte(sealedMagic)) {
		t.Errorf("file does not start with %q: %.12q", sealedMagic, raw)
	}
	for _, needle := range []string{evIP, evMAC, evRaw, evEnt, evDest, "firewall", `"kind"`, "ether1"} {
		if bytes.Contains(raw, []byte(needle)) {
			t.Errorf("match log on disk contains %q in the clear", needle)
		}
	}
}

func TestReopenReadsBackEveryRecord(t *testing.T) {
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s, err := Open(path, 100, key)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := s.Append(evEnt, secretTuple(1000+i), secretEvent(), t0.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	// An update to the first record.
	if err := s.Append(evEnt, secretTuple(1000), secretEvent(), t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := allRecords(t, s)
	recent := collectAllRecent(t, s)
	s.Close()
	if len(before) != 3 {
		t.Fatalf("before reopen: %d records, want 3", len(before))
	}

	s2, err := Open(path, 100, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	after := allRecords(t, s2)
	if len(after) != len(before) {
		t.Fatalf("after reopen: %d records, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].ID != before[i].ID || !after[i].LastSeen.Equal(before[i].LastSeen) || after[i].Count != before[i].Count || after[i].Event.Raw != evRaw {
			t.Errorf("record %d changed across reopen: before %+v after %+v", i, before[i], after[i])
		}
	}
	rAfter := collectAllRecent(t, s2)
	if len(rAfter) != len(recent) {
		t.Errorf("Recent: %d records after reopen, want %d", len(rAfter), len(recent))
	}
}

func collectAllRecent(t *testing.T, s *FileStore) []Record {
	t.Helper()
	var out []Record
	err := s.Recent(context.Background(), RecentQuery{Limit: 100}, func(r Record) bool {
		out = append(out, r)
		return true
	})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return out
}

func TestTornLastFrameIsCutBackOnOpenAndTheLogKeepsWorking(t *testing.T) {
	key := testKey(t)
	path, bounds := writeN(t, key, 3)
	if err := os.Truncate(path, int64(bounds[3]-5)); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path, 100, key)
	if err != nil {
		t.Fatalf("Open with a torn last frame: %v", err)
	}
	defer s.Close()
	if got := allRecords(t, s); len(got) != 2 {
		t.Fatalf("got %d records, want the 2 whole frames", len(got))
	}
	if n := size(t, path); n != bounds[2] {
		t.Errorf("file is %d bytes after open, want it cut to the last whole frame at %d", n, bounds[2])
	}
	if err := s.Append(evEnt, secretTuple(2000), secretEvent(), time.Now()); err != nil {
		t.Fatalf("Append after recovery: %v", err)
	}
	s.Close()

	s2, err := Open(path, 100, key)
	if err != nil {
		t.Fatalf("reopen after recovery: %v", err)
	}
	defer s2.Close()
	if got := allRecords(t, s2); len(got) != 3 {
		t.Fatalf("got %d records after recovery and reopen, want 3", len(got))
	}
}

// mustRefuse asserts Open and ReadLines both reject the file, and that a
// store opened before the damage fails a query instead of returning fewer.
func mustRefuse(t *testing.T, what, path string, key *retention.Key) {
	t.Helper()
	if s, err := Open(path, 100, key); err == nil {
		s.Close()
		t.Errorf("%s: Open succeeded", what)
	}
	if _, err := ReadLines(path, key); err == nil {
		t.Errorf("%s: ReadLines succeeded", what)
	}
}

func TestReorderedFramesAreDetected(t *testing.T) {
	key := testKey(t)
	path, b := writeN(t, key, 3)
	raw, _ := os.ReadFile(path)
	var out []byte
	out = append(out, raw[:b[0]]...)
	out = append(out, raw[b[1]:b[2]]...) // frame 2 first
	out = append(out, raw[b[0]:b[1]]...)
	out = append(out, raw[b[2]:]...)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, "swapped frames", path, key)
}

func TestDroppedMiddleFrameIsDetected(t *testing.T) {
	key := testKey(t)
	path, b := writeN(t, key, 3)
	raw, _ := os.ReadFile(path)
	out := append(append([]byte{}, raw[:b[1]]...), raw[b[2]:]...)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, "dropped middle frame", path, key)
}

func TestFrameSplicedFromAnotherLogIsDetected(t *testing.T) {
	key := testKey(t)
	path, b := writeN(t, key, 3)
	other, ob := writeN(t, key, 3) // same key, different salt
	raw, _ := os.ReadFile(path)
	oraw, _ := os.ReadFile(other)
	out := append([]byte{}, raw[:b[1]]...)
	out = append(out, oraw[ob[1]:ob[2]]...)
	out = append(out, raw[b[2]:]...)
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, "spliced frame", path, key)
}

func TestFlippedBitInAFrameIsDetected(t *testing.T) {
	key := testKey(t)
	path, b := writeN(t, key, 3)
	raw, _ := os.ReadFile(path)
	raw[b[1]+(b[2]-b[1])/2] ^= 0x01
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRefuse(t, "altered frame", path, key)
}

func TestWrongKeyIsAnErrorNotAnEmptyLog(t *testing.T) {
	path, _ := writeN(t, testKey(t), 2)
	mustRefuse(t, "wrong key", path, otherKey(t))
}

func TestQueryOnAnOpenStoreFailsOnATamperedFrame(t *testing.T) {
	key := testKey(t)
	path, b := writeN(t, key, 3)
	s, err := Open(path, 100, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw, _ := os.ReadFile(path)
	out := append(append([]byte{}, raw[:b[1]]...), raw[b[2]:]...) // drop frame 2
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	err = s.Query(context.Background(), Query{Source: Identity{MAC: evMAC, IP: evIP}}, func(Record) bool { return true })
	if err == nil {
		t.Error("Query returned no error over a log with a dropped frame")
	}
}

func TestPlaintextLogUnderAKeyIsRefusedAndLeftUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	body := []byte(`{"kind":"record","id":"aa","entryId":"e","tuple":{"source":{"mac":"` + evMAC + `"},"destIp":"` + evDest + `","port":443},"firstSeen":"2026-08-09T12:00:00Z","lastSeen":"2026-08-09T12:00:00Z"}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path, 100, testKey(t))
	if err == nil {
		s.Close()
		t.Fatal("Open of a plaintext log under a key succeeded")
	}
	if !errors.Is(err, persist.ErrPlaintextDocument) {
		t.Errorf("Open error = %v, want errors.Is persist.ErrPlaintextDocument", err)
	}
	if _, err := ReadLines(path, testKey(t)); !errors.Is(err, persist.ErrPlaintextDocument) {
		t.Errorf("ReadLines error = %v, want persist.ErrPlaintextDocument", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, body) {
		t.Errorf("plaintext file was changed: %q", got)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Errorf("plaintext file mtime changed to %v", fi.ModTime())
	}
}

func TestNilKeyIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matchlog.jsonl")
	if s, err := Open(path, 100, nil); err == nil {
		s.Close()
		t.Error("Open with a nil key succeeded")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("Open with a nil key created a file")
	}
	if _, err := ReadLines(path, nil); err == nil {
		t.Error("ReadLines with a nil key succeeded")
	}
	line := `{"kind":"record","id":"aa","lastSeen":"2026-08-09T12:00:00Z"}`
	if err := WriteSealed(path, nil, []byte(line)); err == nil {
		t.Error("WriteSealed with a nil key succeeded")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("WriteSealed with a nil key created a file")
	}
}

func TestReadLinesThenWriteSealedUnderAnotherKeyKeepsTheLog(t *testing.T) {
	k1, k2 := testKey(t), otherKey(t)
	path, _ := writeN(t, k1, 3)

	lines, err := ReadLines(path, k1)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if !bytes.Contains(lines, []byte(evRaw[:20])) || !bytes.Contains(lines, []byte(evMAC)) || bytes.Count(lines, []byte("\n")) < 2 {
		t.Errorf("ReadLines did not return plain JSON lines: %q", lines)
	}

	dst := filepath.Join(t.TempDir(), "matchlog.jsonl")
	if err := WriteSealed(dst, k2, lines); err != nil {
		t.Fatalf("WriteSealed: %v", err)
	}
	assertSealed(t, dst)
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(dst)
	if bytes.Equal(a, b) {
		t.Error("re-sealed file is byte-identical to the original")
	}

	s1, err := Open(path, 100, k1)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	s2, err := Open(dst, 100, k2)
	if err != nil {
		t.Fatalf("Open re-sealed log under its key: %v", err)
	}
	defer s2.Close()
	r1, r2 := allRecords(t, s1), allRecords(t, s2)
	if len(r1) != 3 || len(r2) != 3 {
		t.Fatalf("records: %d original, %d re-sealed, want 3 each", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID || !r1[i].LastSeen.Equal(r2[i].LastSeen) || r1[i].Count != r2[i].Count {
			t.Errorf("record %d differs after re-sealing: %+v vs %+v", i, r1[i], r2[i])
		}
	}
}

func TestReadLinesOfAbsentOrHeaderOnlyLogIsEmpty(t *testing.T) {
	key := testKey(t)
	dir := t.TempDir()
	if got, err := ReadLines(filepath.Join(dir, "absent.jsonl"), key); err != nil || len(got) != 0 {
		t.Errorf("absent: %q, %v; want empty, nil", got, err)
	}
	path := filepath.Join(dir, "empty.jsonl")
	s, err := Open(path, 10, key)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if got, err := ReadLines(path, key); err != nil || len(got) != 0 {
		t.Errorf("header-only: %q, %v; want empty, nil", got, err)
	}
}

func TestWriteSealedRefusesALineThatIsNotAFileLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	good := `{"kind":"record","id":"aa","lastSeen":"2026-08-09T12:00:00Z"}`
	overLimit := `{"kind":"record","id":"big","pad":"` + strings.Repeat("x", maxLineBytes) + `"}`
	if len(overLimit) <= maxLineBytes {
		t.Fatal("test line is not over the limit")
	}
	for _, bad := range []string{`{"kind":"wat","id":"x"}`, good + "\n" + overLimit} {
		if err := WriteSealed(path, testKey(t), []byte(bad)); err == nil {
			t.Errorf("WriteSealed accepted %.60q", bad)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("WriteSealed wrote a file for %.60q", bad)
			os.Remove(path)
		}
	}
}

const tornR1 = `{"kind":"record","id":"r1","entryId":"e1","tuple":{"source":{"mac":"` + evMAC + `"},"destIp":"` + evDest + `","port":22},"event":{"raw":"raw-1"},"firstSeen":"2026-08-09T12:00:00Z","lastSeen":"2026-08-09T12:00:00Z"}`
const tornR2 = `{"kind":"record","id":"r2","entryId":"e1","tuple":{"source":{"mac":"` + evMAC + `"},"destIp":"` + evDest + `","port":23},"event":{"raw":"raw-2"},"firstSeen":"2026-08-09T12:00:01Z","lastSeen":"2026-08-09T12:00:01Z"}`
const tornFragment = `{"kind":"record","id":"r3","entryId":"e1","tu`

// validateLinesCount calls ValidateLines through reflection so this file
// compiles whether it returns (skipped, err) or only err; the tests then
// fail on behaviour rather than on the build.
func validateLinesCount(t *testing.T, lines []byte) (int, error) {
	t.Helper()
	out := reflect.ValueOf(ValidateLines).Call([]reflect.Value{reflect.ValueOf(lines)})
	var err error
	if e, ok := out[len(out)-1].Interface().(error); ok {
		err = e
	}
	if len(out) < 2 {
		t.Errorf("ValidateLines returns no skipped count; want (skipped int, err error)")
		return 0, err
	}
	return int(out[0].Int()), err
}

// v0.6 skipped a line that did not decode (a torn write); a v0.6 backup
// carries the raw file, so WriteSealed must leave such a line out rather
// than refuse the log (v0.7.0 audit, O1-F1).
func TestWriteSealedLeavesOutATornLineAsV06Did(t *testing.T) {
	cases := []struct {
		name  string
		lines string
		want  int
	}{
		{"torn tail with no newline", tornR1 + "\n" + tornR2 + "\n" + tornFragment, 2},
		{"torn fragment glued to the next record", tornR1 + "\n" + tornFragment + tornR2 + "\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key := testKey(t)
			path := filepath.Join(t.TempDir(), "matchlog.jsonl")
			if err := WriteSealed(path, key, []byte(c.lines)); err != nil {
				t.Fatalf("WriteSealed refused a torn line v0.6 skipped: %v", err)
			}
			s, err := Open(path, 10, key)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			got := collect(t, s, Query{Source: Identity{MAC: evMAC}})
			if len(got) != c.want {
				t.Errorf("got %d records, want the %d whole ones", len(got), c.want)
			}
			skipped, err := validateLinesCount(t, []byte(c.lines))
			if err != nil || skipped != 1 {
				t.Errorf("ValidateLines = (%d, %v), want (1, nil)", skipped, err)
			}
		})
	}
}

func TestCapacityAccountingSurvivesReopen(t *testing.T) {
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s, err := Open(path, 2, key)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := s.Append("e", secretTuple(1000+i), secretEvent(), t0); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s2, err := Open(path, 2, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if st := s2.Stats(); st.Count != 2 || !st.Full {
		t.Errorf("Stats after reopen = %+v, want Count 2 and Full", st)
	}
	if err := s2.Append("e", secretTuple(9999), secretEvent(), t0); !errors.Is(err, ErrCapacityReached) {
		t.Errorf("new tuple at capacity: %v, want ErrCapacityReached", err)
	}
	if err := s2.Append("e", secretTuple(1000), secretEvent(), t0.Add(time.Minute)); err != nil {
		t.Errorf("repeat of an open tuple at capacity: %v, want it collapsed", err)
	}
}

// A power loss mid-append can leave a zero-filled tail (the file extended,
// the bytes not yet on disk). That is a crash tail, not damage (PROP-S2).
func TestZeroFilledTailIsCutBackAsACrashTail(t *testing.T) {
	for name, tail := range map[string]int{
		"four zero bytes":          4,
		"a frame's worth of zeros": 4 + Format.MaxFrameBytes,
	} {
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			path, bounds := writeN(t, key, 3)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write(make([]byte, tail)); err != nil {
				t.Fatal(err)
			}
			f.Close()

			lines, err := ReadLines(path, key)
			if err != nil {
				t.Fatalf("ReadLines with a zero-filled tail: %v", err)
			}
			if n := bytes.Count(lines, []byte("\n")); n != 3 {
				t.Errorf("ReadLines holds %d lines, want 3", n)
			}
			s, err := Open(path, 100, key)
			if err != nil {
				t.Fatalf("Open with a zero-filled tail: %v", err)
			}
			defer s.Close()
			if got := allRecords(t, s); len(got) != 3 {
				t.Fatalf("got %d records, want 3", len(got))
			}
			if n := size(t, path); n != bounds[3] {
				t.Errorf("file is %d bytes after open, want the zeros cut to %d", n, bounds[3])
			}
			if err := s.Append(evEnt, secretTuple(2000), secretEvent(), time.Now()); err != nil {
				t.Fatalf("Append after recovery: %v", err)
			}
			s.Close()
			s2, err := Open(path, 100, key)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer s2.Close()
			if got := allRecords(t, s2); len(got) != 4 {
				t.Errorf("got %d records after reopen, want 4", len(got))
			}
		})
	}

	// Guard (passes now): a zero run longer than one frame can be stays damage.
	t.Run("a zero run longer than a frame can be", func(t *testing.T) {
		key := testKey(t)
		path, _ := writeN(t, key, 3)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(make([]byte, 4+Format.MaxFrameBytes+1)); err != nil {
			t.Fatal(err)
		}
		f.Close()
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		mustRefuse(t, "over-long zero run", path, key)
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Error("the file was changed by a refused open")
		}
	})
}

// A crash while the header is being written leaves a short, zero-filled
// or prefix-only file. It holds no record, so it is started again (O1-R1).
func TestTornHeaderIsStartedAgainAndTheLogKeepsWorking(t *testing.T) {
	cases := map[string][]byte{
		"22 zeros":              make([]byte, hdrEnd),
		"11 zeros":              make([]byte, 11),
		"magic version zeros":   append([]byte(sealedMagic+"\x01"), make([]byte, 16)...),
		"a prefix of the magic": []byte("MVM"),
	}
	for name, torn := range cases {
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			path := filepath.Join(t.TempDir(), "matchlog.jsonl")
			if err := os.WriteFile(path, torn, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Check(path); err != nil {
				t.Fatalf("Check of a torn header: %v", err)
			}
			if got, err := ReadLines(path, key); err != nil || len(got) != 0 {
				t.Fatalf("ReadLines = %q, %v; want empty, nil", got, err)
			}
			s, err := Open(path, 100, key)
			if err != nil {
				t.Fatalf("Open of a torn header: %v", err)
			}
			defer s.Close()
			if n := size(t, path); n != hdrEnd {
				t.Errorf("file is %d bytes after open, want a fresh %d-byte header", n, hdrEnd)
			}
			if err := s.Append(evEnt, secretTuple(1000), secretEvent(), time.Now()); err != nil {
				t.Fatalf("Append: %v", err)
			}
			s.Close()
			s2, err := Open(path, 100, key)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer s2.Close()
			if got := allRecords(t, s2); len(got) != 1 {
				t.Errorf("got %d records after reopen, want 1", len(got))
			}
		})
	}
}

// Guards (pass now, must keep passing): a genuine header-only log keeps
// its exact bytes on reopen; a 23-byte zero file and a foreign 22-byte
// file are refused and left unchanged.
func TestTornHeaderRuleDoesNotTouchOtherFiles(t *testing.T) {
	key := testKey(t)
	t.Run("genuine header-only log", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "matchlog.jsonl")
		s, err := Open(path, 100, key)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		before, _ := os.ReadFile(path)
		s, err = Open(path, 100, key)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		s.Close()
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Error("a genuine header-only log was rewritten")
		}
	})
	for name, body := range map[string][]byte{
		"23 zero bytes":    make([]byte, hdrEnd+1),
		"foreign 22 bytes": []byte("not a frame log at all"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "matchlog.jsonl")
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			mustRefuse(t, name, path, key)
			if got, _ := os.ReadFile(path); !bytes.Equal(got, body) {
				t.Error("the refused file was changed")
			}
		})
	}
}
