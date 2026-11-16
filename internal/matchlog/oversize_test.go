// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/store"
)

// #1471: a record whose encoded line is over the line limit must still be
// recorded, with Raw cut by store.ClampRaw and RawTruncated set, rather than
// refused (which loses the match).

func recentRecords(t *testing.T, s *FileStore) []Record {
	t.Helper()
	var out []Record
	err := s.Recent(context.Background(), RecentQuery{}, func(r Record) bool {
		out = append(out, r)
		return true
	})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return out
}

func openOversize(t *testing.T, path string, key *retention.Key) *FileStore {
	t.Helper()
	s, err := Open(path, 100, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOversizeRawStillRecordedTruncated(t *testing.T) {
	for _, prov := range []bool{false, true} {
		name := "Append"
		if prov {
			name = "AppendProvisional"
		}
		t.Run(name, func(t *testing.T) {
			key := testKey(t)
			path := filepath.Join(t.TempDir(), "matchlog.jsonl")
			// 0x01 encodes as \u0001 (6 bytes), so 64 KiB of it is ~384 KiB
			// on the line, over maxLineBytes.
			orig := strings.Repeat("\x01", 64*1024)
			ev := store.Event{Action: store.ActionAccept, SrcIP: evIP, DstIP: evDest, Raw: orig}
			t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

			s := openOversize(t, path, key)
			var err error
			if prov {
				err = s.AppendProvisional(evEnt, secretTuple(1000), ev, t0, true)
			} else {
				err = s.Append(evEnt, secretTuple(1000), ev, t0)
			}
			if err != nil {
				t.Fatalf("%s of an oversized record: %v (the match is lost)", name, err)
			}

			check := func(label string, s *FileStore) {
				t.Helper()
				recs := recentRecords(t, s)
				if len(recs) != 1 {
					t.Fatalf("%s: %d records, want 1", label, len(recs))
				}
				e := recs[0].Event
				if !e.RawTruncated {
					t.Errorf("%s: RawTruncated is false, want true", label)
				}
				if len(e.Raw) > store.MaxRawBytes {
					t.Errorf("%s: len(Raw) = %d, want <= %d", label, len(e.Raw), store.MaxRawBytes)
				}
				if !strings.HasPrefix(orig, e.Raw) {
					t.Errorf("%s: Raw is not a prefix of the original", label)
				}
			}
			check("before reopen", s)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s2 := openOversize(t, path, key)
			defer s2.Close()
			check("after reopen", s2)
		})
	}
}

func TestSmallRawStoredUnchanged(t *testing.T) {
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "matchlog.jsonl")
	s := openOversize(t, path, key)
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if err := s.Append(evEnt, secretTuple(1000), secretEvent(), t0); err != nil {
		t.Fatalf("Append: %v", err)
	}
	recs := recentRecords(t, s)
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	if recs[0].Event.Raw != evRaw || recs[0].Event.RawTruncated {
		t.Errorf("small record changed: Raw=%q RawTruncated=%v", recs[0].Event.Raw, recs[0].Event.RawTruncated)
	}
	s.Close()
}
