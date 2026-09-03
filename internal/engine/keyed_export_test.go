// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// countRingCodec is the encode/decode pair every Keyed[*CountRing] in
// this package exports through -- written once here so the tests use the
// same shape the definitions do.
func countRingCodec(window time.Duration, now time.Time) (func(*CountRing) (json.RawMessage, error), func(json.RawMessage) (*CountRing, error)) {
	encode := func(r *CountRing) (json.RawMessage, error) { return r.ExportState() }
	decode := func(raw json.RawMessage) (*CountRing, error) {
		r := NewCountRing(window)
		if err := r.ImportState(raw, now); err != nil {
			return nil, err
		}
		return r, nil
	}
	return encode, decode
}

func TestKeyedExportImportRoundTripsValuesAndActivity(t *testing.T) {
	k := NewKeyed[*CountRing]()
	for i, key := range []string{"10.0.0.1", "10.0.0.2"} {
		at := exportStart.Add(time.Duration(i) * time.Minute)
		r := k.GetOrCreate(key, at, func() *CountRing { return NewCountRing(time.Hour) })
		for n := 0; n <= i; n++ {
			r.Add(at, true)
		}
	}
	now := exportStart.Add(2 * time.Minute)
	encode, decode := countRingCodec(time.Hour, now)

	raw, err := k.Export(encode)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	restored := NewKeyed[*CountRing]()
	if err := restored.Import(raw, decode); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if restored.Len() != 2 {
		t.Fatalf("restored Len() = %d, want 2", restored.Len())
	}
	for key, want := range map[string]int{"10.0.0.1": 1, "10.0.0.2": 2} {
		r, ok := restored.Get(key)
		if !ok {
			t.Fatalf("key %q missing after import", key)
		}
		if got := r.Count(now, time.Hour); got != want {
			t.Errorf("restored count for %q = %d, want %d", key, got, want)
		}
	}
}

func TestKeyedImportHonoursMaxTrackedKeysNewestActivityFirst(t *testing.T) {
	withMaxTrackedKeys(t, 3)

	// Five entries in the document, each a minute more recently active
	// than the last: only the three newest may be restored.
	var state keyedState
	for i := 0; i < 5; i++ {
		r := NewCountRing(time.Hour)
		at := exportStart.Add(time.Duration(i) * time.Minute)
		r.Add(at, true)
		raw, err := r.ExportState()
		if err != nil {
			t.Fatalf("ExportState: %v", err)
		}
		state.Entries = append(state.Entries, keyedEntryState{
			Key:          fmt.Sprintf("10.0.0.%d", i),
			LastActivity: at,
			Value:        raw,
		})
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshalling a hand-built state: %v", err)
	}

	now := exportStart.Add(5 * time.Minute)
	_, decode := countRingCodec(time.Hour, now)
	k := NewKeyed[*CountRing]()
	if err := k.Import(raw, decode); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if k.Len() != 3 {
		t.Fatalf("restored Len() = %d, want maxTrackedKeys (3)", k.Len())
	}
	for _, key := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		if _, ok := k.Get(key); !ok {
			t.Errorf("expected the newest-active key %q to be restored", key)
		}
	}
	for _, key := range []string{"10.0.0.0", "10.0.0.1"} {
		if _, ok := k.Get(key); ok {
			t.Errorf("expected the oldest-active key %q to be shed by the cap", key)
		}
	}
}

func TestKeyedImportPreservesEvictionOrder(t *testing.T) {
	withMaxTrackedKeys(t, 2)

	var state keyedState
	for i, key := range []string{"old", "new"} {
		r := NewCountRing(time.Hour)
		at := exportStart.Add(time.Duration(i) * time.Hour)
		r.Add(exportStart, true)
		raw, err := r.ExportState()
		if err != nil {
			t.Fatalf("ExportState: %v", err)
		}
		state.Entries = append(state.Entries, keyedEntryState{Key: key, LastActivity: at, Value: raw})
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshalling a hand-built state: %v", err)
	}

	now := exportStart.Add(time.Hour)
	_, decode := countRingCodec(time.Hour, now)
	k := NewKeyed[*CountRing]()
	if err := k.Import(raw, decode); err != nil {
		t.Fatalf("Import: %v", err)
	}

	// The restored stamps are what eviction orders on: the next new key
	// must shed "old", not "new".
	k.GetOrCreate("newest", now.Add(time.Hour), func() *CountRing { return NewCountRing(time.Hour) })
	if _, ok := k.Get("old"); ok {
		t.Error("expected the least-recently-active restored key to be evicted first")
	}
	if _, ok := k.Get("new"); !ok {
		t.Error("expected the more-recently-active restored key to survive")
	}
}

func TestKeyedImportLeavesTheMapUntouchedOnADecodeFailure(t *testing.T) {
	k := NewKeyed[*CountRing]()
	raw := json.RawMessage(`{"entries":[{"key":"10.0.0.1","lastActivity":"2026-09-03T14:00:00Z","value":{"buckets":"not-an-array"}}]}`)
	decode := func(raw json.RawMessage) (*CountRing, error) {
		r := NewCountRing(time.Hour)
		if err := r.ImportState(raw, exportStart); err != nil {
			return nil, err
		}
		return r, nil
	}
	if err := k.Import(raw, decode); err == nil {
		t.Fatal("expected Import to fail on a value the decoder rejects")
	}
	if k.Len() != 0 {
		t.Errorf("Len() = %d after a failed import, want 0 -- nothing may be published", k.Len())
	}
}

func TestKeyedImportRejectsAMalformedDocument(t *testing.T) {
	k := NewKeyed[*CountRing]()
	_, decode := countRingCodec(time.Hour, exportStart)
	if err := k.Import(json.RawMessage(`{"entries":`), decode); err == nil {
		t.Fatal("expected Import to reject a truncated document")
	}
}
