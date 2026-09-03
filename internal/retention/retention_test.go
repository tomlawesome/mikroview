// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func testKey(t *testing.T) *Key {
	t.Helper()
	k, err := NewKeyFromMaterial([]byte(strings.Repeat("k", MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	return k
}

func openStore(t *testing.T, dir string, key *Key) *Store {
	t.Helper()
	s, err := Open(Options{Dir: dir, Key: key, flushInterval: time.Hour, flushEvents: 1 << 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func event(id uint64, at time.Time) store.Event {
	return store.Event{
		ID:         id,
		Time:       at,
		ReceivedAt: at,
		DeviceID:   "router-1",
		SrcIP:      "192.0.2.10",
		DstIP:      "198.51.100.20",
		DstPort:    22,
		Action:     store.ActionDrop,
		RuleLabel:  "r13",
		Raw:        "firewall,info r13 drop input: in:ether1 out:(none)",
	}
}

func collect(t *testing.T, dir string, key *Key, cutoff time.Time) ([]store.Event, Window) {
	t.Helper()
	var got []store.Event
	w := ReplayDir(dir, key, cutoff, func(e store.Event) { got = append(got, e) })
	return got, w
}

// A retained event comes back exactly as it went in, ReceivedAt
// included -- store.Event tags that field json:"-", so a naive encoding
// would lose the one field every replay orders by.
func TestRoundTripPreservesReceivedAt(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)

	at := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC)
	s.Append(event(1, at))
	s.Append(event(2, at.Add(time.Second)))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	got, w := collect(t, dir, key, time.Time{})
	if w.Err != nil {
		t.Fatalf("replay: %v", w.Err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if !got[0].ReceivedAt.Equal(at) {
		t.Errorf("ReceivedAt = %v, want %v", got[0].ReceivedAt, at)
	}
	if got[0].Raw != event(1, at).Raw || got[0].DstPort != 22 || got[0].RuleLabel != "r13" {
		t.Errorf("event came back altered: %+v", got[0])
	}
	if w.Count != 2 || w.Days != 1 {
		t.Errorf("window = %+v, want 2 events over 1 day", w)
	}
	if !w.Start.Equal(at) || !w.End.Equal(at.Add(time.Second)) {
		t.Errorf("window bounds = %v..%v, want %v..%v", w.Start, w.End, at, at.Add(time.Second))
	}
}

// The file on disk must not contain the log line in the clear. This is
// the whole reason the feature is off without a key.
func TestRetainedFileHoldsNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)

	at := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC)
	s.Append(event(1, at))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, fileNameFor("2026-09-03")))
	if err != nil {
		t.Fatalf("reading retained file: %v", err)
	}
	for _, needle := range []string{"192.0.2.10", "198.51.100.20", "r13", "ether1", "router-1"} {
		if strings.Contains(string(raw), needle) {
			t.Errorf("retained file contains %q in the clear", needle)
		}
	}
}

// A different key does not open the file, and the failure is reported
// rather than being read as an empty day.
func TestWrongKeyIsReportedNotSilentlyEmpty(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir, testKey(t))
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	s.Append(event(1, at))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	other, err := NewKeyFromMaterial([]byte(strings.Repeat("x", MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	got, w := collect(t, dir, other, time.Time{})
	if len(got) != 0 {
		t.Errorf("wrong key yielded %d events", len(got))
	}
	if w.Err == nil {
		t.Fatal("wrong key produced no error -- an unreadable history must never read as an empty one")
	}
}

// Renaming a day's file to another day makes it unreadable: the day is
// bound into both the file key and every frame's additional data, so
// yesterday's traffic cannot be presented as today's by moving a file.
func TestRenamingADayFileBreaksIt(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	s.Append(event(1, at))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s.Close()

	from := filepath.Join(dir, fileNameFor("2026-09-03"))
	to := filepath.Join(dir, fileNameFor("2026-09-04"))
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, w := collect(t, dir, key, time.Time{})
	if len(got) != 0 || w.Err == nil {
		t.Fatalf("renamed file still read: %d events, err %v", len(got), w.Err)
	}
}

// A crash mid-write leaves a partial frame. Everything written before
// it stays readable, and the next open trims the tail rather than
// appending after it.
func TestPartialTailIsToleratedThenTrimmed(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	s.Append(event(1, at))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s.Close()

	path := filepath.Join(dir, fileNameFor("2026-09-03"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// A length prefix promising more than follows: exactly what an
	// interrupted write leaves behind.
	partial := append(append([]byte{}, raw...), 0x00, 0x00, 0x10, 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, partial, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, w := collect(t, dir, key, time.Time{})
	if w.Err != nil {
		t.Fatalf("partial tail should not be an error: %v", w.Err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want the 1 written before the tail", len(got))
	}

	s2 := openStore(t, dir, key)
	s2.Append(event(2, at.Add(time.Minute)))
	if err := s2.Flush(); err != nil {
		t.Fatalf("Flush after reopen: %v", err)
	}
	got, w = collect(t, dir, key, time.Time{})
	if w.Err != nil {
		t.Fatalf("replay after reopen: %v", w.Err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events after appending past a trimmed tail, want 2", len(got))
	}
}

// Events are filed by their own day, not by the clock at flush time.
func TestEventsAreFiledByTheirOwnDay(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)

	before := time.Date(2026, 9, 3, 23, 59, 59, 0, time.UTC)
	after := time.Date(2026, 9, 4, 0, 0, 1, 0, time.UTC)
	s.Append(event(1, before))
	s.Append(event(2, after))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	for _, day := range []string{"2026-09-03", "2026-09-04"} {
		if _, err := os.Stat(filepath.Join(dir, fileNameFor(day))); err != nil {
			t.Errorf("no file for %s: %v", day, err)
		}
	}
	got, w := collect(t, dir, key, time.Time{})
	if len(got) != 2 || w.Days != 2 {
		t.Fatalf("got %d events over %d days, want 2 over 2", len(got), w.Days)
	}
	if !got[0].ReceivedAt.Equal(before) || !got[1].ReceivedAt.Equal(after) {
		t.Error("events did not come back oldest first across a day boundary")
	}
}

// The cutoff is what stops the ring and the files reporting the same
// event twice.
func TestReplayStopsAtCutoff(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	for i := range 5 {
		s.Append(event(uint64(i+1), base.Add(time.Duration(i)*time.Minute)))
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	cutoff := base.Add(3 * time.Minute)
	got, w := collect(t, dir, key, cutoff)
	if len(got) != 3 {
		t.Fatalf("got %d events before the cutoff, want 3", len(got))
	}
	if !w.End.Before(cutoff) {
		t.Errorf("window end %v is not before the cutoff %v", w.End, cutoff)
	}
}

// Both caps drop the oldest day first, and neither ever deletes the day
// currently being written.
func TestPruneDropsOldestFirstAndKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s, err := Open(Options{Dir: dir, Key: key, Days: 2, flushInterval: time.Hour, flushEvents: 1 << 30})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	for d := 1; d <= 4; d++ {
		s.Append(event(uint64(d), time.Date(2026, 9, d, 10, 0, 0, 0, time.UTC)))
		if err := s.Flush(); err != nil {
			t.Fatalf("Flush day %d: %v", d, err)
		}
	}
	files, err := s.dayFiles()
	if err != nil {
		t.Fatalf("dayFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("kept %d days, want 2", len(files))
	}
	if files[0].day != "2026-09-03" || files[1].day != "2026-09-04" {
		t.Errorf("kept %v, want the two newest days", []string{files[0].day, files[1].day})
	}

	// A byte cap far below one day's size still leaves the newest day.
	s.maxBytes = 1
	if err := s.prune(); err != nil {
		t.Fatalf("prune: %v", err)
	}
	files, err = s.dayFiles()
	if err != nil {
		t.Fatalf("dayFiles: %v", err)
	}
	if len(files) != 1 || files[0].day != "2026-09-04" {
		t.Fatalf("byte cap left %v, want only the newest day", files)
	}
}

// Turning the switch off takes the history with it.
func TestPurgeRemovesEverything(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)
	for d := 1; d <= 3; d++ {
		s.Append(event(uint64(d), time.Date(2026, 9, d, 10, 0, 0, 0, time.UTC)))
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := s.Purge(); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	got, w := collect(t, dir, key, time.Time{})
	if len(got) != 0 || w.Count != 0 {
		t.Fatalf("purge left %d events behind", len(got))
	}
}

// PurgeDir is the same thing without a key, for the startup case where
// retention is off and a previous run left files behind.
func TestPurgeDirNeedsNoKeyAndSparesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir, testKey(t))
	s.Append(event(1, time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)))
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s.Close()

	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("not ours"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := PurgeDir(dir); err != nil {
		t.Fatalf("PurgeDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileNameFor("2026-09-03"))); !errors.Is(err, os.ErrNotExist) {
		t.Error("retained file survived PurgeDir")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("PurgeDir removed a file that was not ours: %v", err)
	}
}

// A key file shorter than the AES-256 key it stands in for is refused
// outright rather than stretched.
func TestKeyFloorAndAbsence(t *testing.T) {
	if _, err := LoadKey(""); !errors.Is(err, ErrNoKey) {
		t.Errorf("empty path gave %v, want ErrNoKey", err)
	}
	dir := t.TempDir()
	short := filepath.Join(dir, "short.key")
	if err := os.WriteFile(short, []byte("too short\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadKey(short); !errors.Is(err, ErrKeyTooShort) {
		t.Errorf("short key gave %v, want ErrKeyTooShort", err)
	}

	good := filepath.Join(dir, "good.key")
	if err := os.WriteFile(good, append([]byte(strings.Repeat("m", MinKeyBytes)), '\n'), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	k, err := LoadKey(good)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	// The trailing newline must not be part of the key: a file written
	// by an editor and one written by printf have to derive the same
	// key, or every previously written day stops opening.
	direct, err := NewKeyFromMaterial([]byte(strings.Repeat("m", MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	salt := []byte("0123456789abcdef")
	a, err := k.fileKey(salt, "2026-09-03")
	if err != nil {
		t.Fatalf("fileKey: %v", err)
	}
	b, err := direct.fileKey(salt, "2026-09-03")
	if err != nil {
		t.Fatalf("fileKey: %v", err)
	}
	if string(a) != string(b) {
		t.Error("a trailing newline changed the derived key")
	}
}

// Retention refuses to run without a key. There is no plaintext mode to
// fall back to.
func TestOpenWithoutAKeyIsRefused(t *testing.T) {
	if _, err := Open(Options{Dir: t.TempDir()}); !errors.Is(err, ErrNoKey) {
		t.Errorf("Open without a key gave %v, want ErrNoKey", err)
	}
}
