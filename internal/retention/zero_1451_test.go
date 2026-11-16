// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// #1451: paths no other test reached.

func TestFrameLogResetEmptiesTheFileAndKeepsItWritable(t *testing.T) {
	ff := smallFormat()
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "f.mvevt")
	l, err := OpenFrameLog(path, key, ff)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, p := range []string{"one", "two", "three"} {
		if err := l.Append([]byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	if l.Frames() != 3 {
		t.Fatalf("Frames() = %d, want 3", l.Frames())
	}
	if err := l.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if l.Frames() != 0 {
		t.Errorf("Frames() after Reset = %d, want 0", l.Frames())
	}
	got, err := readAll(t, path, key, ff)
	if err != nil || len(got) != 0 {
		t.Fatalf("after Reset got %q, %v; want no frames", got, err)
	}
	if err := l.Append([]byte("fresh")); err != nil {
		t.Fatalf("Append after Reset: %v", err)
	}
	got, err = readAll(t, path, key, ff)
	if err != nil || !reflect.DeepEqual(got, []string{"fresh"}) {
		t.Fatalf("after Reset+Append got %q, %v; want [fresh]", got, err)
	}
}

// A failed append that cannot be cut back marks the log broken: it names
// the cause and refuses every later append rather than writing after a
// tail of unknown state.
func TestFrameLogAppendThatCannotBeCutBackBreaksTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.mvevt")
	l, err := OpenFrameLog(path, testKey(t), smallFormat())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	l.f.Close() // every write, and the cut back, now fail
	err = l.Append([]byte("lost"))
	if err == nil {
		t.Fatal("Append on a dead file returned nil")
	}
	if l.broken == nil {
		t.Error("log not marked broken after a failed cut back")
	}
	if err2 := l.Append([]byte("again")); err2 == nil {
		t.Error("a broken log accepted another append")
	}
}

func TestKeyDocumentSealRoundTripBindsTheInfoString(t *testing.T) {
	k := testKey(t)
	plain := []byte("router backup bytes")
	sealed, err := k.SealDocument("backup/r1", plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Error("sealed document holds the plaintext")
	}
	got, err := k.OpenDocument("backup/r1", sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("OpenDocument = %q, %v", got, err)
	}
	if _, err := k.OpenDocument("backup/r2", sealed); err == nil {
		t.Error("a document sealed for one slot opened under another")
	}
	prefix := []byte("PREFIX")
	appended, err := k.SealDocumentAppend(append([]byte(nil), prefix...), "backup/r1", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(appended, prefix) {
		t.Fatal("SealDocumentAppend did not append onto dst")
	}
	got, err = k.OpenDocument("backup/r1", appended[len(prefix):])
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("OpenDocument of appended seal = %q, %v", got, err)
	}
}

func TestStoreReplayDaysAndReplayDay(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	s := openStore(t, dir, key)
	d1 := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	d2 := d1.Add(24 * time.Hour)
	s.Append(event(1, d1))
	s.Append(event(2, d1.Add(time.Minute)))
	s.Append(event(3, d2))
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}

	days, err := s.Days()
	if err != nil || !reflect.DeepEqual(days, []string{"2026-09-03", "2026-09-04"}) {
		t.Fatalf("Store.Days() = %v, %v", days, err)
	}
	pkgDays, err := Days(dir)
	if err != nil || !reflect.DeepEqual(pkgDays, days) {
		t.Fatalf("Days(dir) = %v, %v; want %v", pkgDays, err, days)
	}

	var all []uint64
	w := s.Replay(time.Time{}, func(e store.Event) { all = append(all, e.ID) })
	if w.Err != nil || !reflect.DeepEqual(all, []uint64{1, 2, 3}) {
		t.Fatalf("Store.Replay = %v, %v", all, w.Err)
	}

	var ids []uint64
	n, err := s.ReplayDay("2026-09-03", time.Time{}, func(e store.Event) { ids = append(ids, e.ID) })
	if err != nil || n != 2 || !reflect.DeepEqual(ids, []uint64{1, 2}) {
		t.Fatalf("ReplayDay = %d %v, %v; want 2 [1 2]", n, ids, err)
	}
	ids = nil
	n, err = s.ReplayDay("2026-09-03", d1.Add(time.Minute), func(e store.Event) { ids = append(ids, e.ID) })
	if err != nil || n != 1 || !reflect.DeepEqual(ids, []uint64{1}) {
		t.Fatalf("ReplayDay with cutoff = %d %v, %v; want 1 [1]", n, ids, err)
	}
}

func TestTodayIsTheUTCDayFilesAreNamedBy(t *testing.T) {
	want := time.Now().UTC().Format("2006-01-02")
	got := Today()
	if got != want {
		// Tolerate the test straddling UTC midnight.
		if again := time.Now().UTC().Format("2006-01-02"); got != again {
			t.Errorf("Today() = %q, want %q", got, want)
		}
	}
}
