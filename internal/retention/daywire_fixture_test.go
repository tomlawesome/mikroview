// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #1470: the framing is extracted for the match log, and the day-file bytes
// must not change. testdata/events-2026-09-03.hex was written by the code
// before the extraction, with testKey, in two flushes (events 1 and 2, then
// 3). Replaying it identically afterwards is the "no wire change" proof.
func TestDayFileWrittenBeforeTheFramingExtractionStillReplays(t *testing.T) {
	enc, err := os.ReadFile(filepath.Join("testdata", "events-2026-09-03.hex"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(enc)))
	if err != nil {
		t.Fatalf("fixture is not hex: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileNameFor("2026-09-03")), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	got, w := collect(t, dir, testKey(t), time.Time{})
	if w.Err != nil {
		t.Fatalf("replaying the pre-extraction day file: %v", w.Err)
	}
	at := time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC)
	want := []time.Time{at, at.Add(time.Second), at.Add(time.Minute)}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.ID != uint64(i+1) || !e.ReceivedAt.Equal(want[i]) || e.Raw != event(1, at).Raw || e.SrcIP != "192.0.2.10" {
			t.Errorf("event %d came back altered: %+v", i, e)
		}
	}
}
