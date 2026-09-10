// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"testing"
	"time"
)

// exportStart is the fixed evaluation time every warm-restart test in
// this package works from: an explicit time.Date rather than
// time.Now(), so a boundary assertion means the same thing whenever it
// runs (the package's own convention -- see the shipped definitions'
// tests).
var exportStart = time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

func TestCountRingExportImportRoundTripsWithinTheWindow(t *testing.T) {
	r := NewCountRing(time.Hour) // span 1m, so retention is 60 minutes
	// Three buckets, one per minute, all comfortably inside the window.
	for i := 0; i < 3; i++ {
		at := exportStart.Add(time.Duration(i) * time.Minute)
		r.Add(at, true)
		r.Add(at, false)
	}
	now := exportStart.Add(3 * time.Minute)

	raw, err := r.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}

	restored := NewCountRing(time.Hour)
	if err := restored.ImportState(raw, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	wantTrue, wantTotal := r.Ratio(now, time.Hour)
	gotTrue, gotTotal := restored.Ratio(now, time.Hour)
	if gotTotal != wantTotal || gotTrue != wantTrue {
		t.Errorf("restored ring counted (true=%d, total=%d), want (true=%d, total=%d)",
			gotTrue, gotTotal, wantTrue, wantTotal)
	}
	if wantTotal != 6 {
		t.Fatalf("test setup: expected 6 events in the source ring, got %d", wantTotal)
	}
}

func TestCountRingImportDropsBucketsPastTheWindowAndNeverShiftsThem(t *testing.T) {
	// The boundary #795 turns on: a ring retains span*windowBucketCount
	// (60 minutes here), so a bucket 59 minutes old is still countable
	// and one 61 minutes old is gone -- dropped, never shifted forward
	// into the live window where it would read as recent traffic.
	source := NewCountRing(time.Hour)
	now := exportStart
	source.Add(now.Add(-59*time.Minute), true)
	source.Add(now.Add(-61*time.Minute), true)

	raw, err := source.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	var state countRingState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshalling the export: %v", err)
	}
	if len(state.Buckets) != 2 {
		t.Fatalf("expected the export to carry both buckets (staleness is judged at import), got %d", len(state.Buckets))
	}

	restored := NewCountRing(time.Hour)
	if err := restored.ImportState(raw, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if got := restored.Count(now, time.Hour); got != 1 {
		t.Errorf("restored count over the full window = %d, want 1 (the 59-minute-old bucket only)", got)
	}
	// Not shifted: the surviving event is still where it was, so a
	// query over the last five minutes sees nothing at all.
	if got := restored.Count(now, 5*time.Minute); got != 0 {
		t.Errorf("restored count over the last 5m = %d, want 0 -- a restored bucket must keep its own timestamp", got)
	}
}

func TestCountRingImportDropsBucketsStampedInTheFuture(t *testing.T) {
	source := NewCountRing(time.Hour)
	source.Add(exportStart.Add(10*time.Minute), true)
	raw, err := source.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}

	// #795's hostile-clock case, from the ring's side: the snapshot was
	// written before the host's clock went backwards.
	restored := NewCountRing(time.Hour)
	if err := restored.ImportState(raw, exportStart); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if got := restored.Count(exportStart, time.Hour); got != 0 {
		t.Errorf("restored count = %d, want 0 -- a bucket stamped after now must be dropped", got)
	}
}

func TestCountRingImportRefusesAnUnconstructedRing(t *testing.T) {
	var r CountRing // no span: never went through NewCountRing
	if err := r.ImportState(json.RawMessage(`{"buckets":[]}`), exportStart); err == nil {
		t.Fatal("expected ImportState on a zero-span ring to error")
	}
}

func TestDistinctRingExportImportRoundTripsWithinTheWindow(t *testing.T) {
	r := NewDistinctRing[int](time.Hour)
	for _, p := range []int{22, 80, 443} {
		r.Add(exportStart, p)
	}
	r.Add(exportStart.Add(time.Minute), 8080)
	now := exportStart.Add(2 * time.Minute)

	raw, err := r.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	restored := NewDistinctRing[int](time.Hour)
	if err := restored.ImportState(raw, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}

	values := restored.Values(now, time.Hour, nil)
	if len(values) != 4 {
		t.Fatalf("restored distinct values = %v, want 4 of them", values)
	}
	for _, p := range []int{22, 80, 443, 8080} {
		if _, ok := values[p]; !ok {
			t.Errorf("port %d missing from the restored ring: %v", p, values)
		}
	}
}

func TestDistinctRingImportDropsBucketsPastTheWindow(t *testing.T) {
	source := NewDistinctRing[string](time.Hour)
	now := exportStart
	source.Add(now.Add(-59*time.Minute), "10.0.0.1")
	source.Add(now.Add(-61*time.Minute), "10.0.0.2")

	raw, err := source.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	restored := NewDistinctRing[string](time.Hour)
	if err := restored.ImportState(raw, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	values := restored.Values(now, time.Hour, nil)
	if _, ok := values["10.0.0.1"]; !ok {
		t.Errorf("expected the 59-minute-old value to survive, got %v", values)
	}
	if _, ok := values["10.0.0.2"]; ok {
		t.Errorf("expected the 61-minute-old value to be dropped, got %v", values)
	}
}

func TestDistinctRingExportIsByteStableAcrossWrites(t *testing.T) {
	// A set has no order of its own; the export sorts by each value's
	// JSON encoding so the same contents always produce the same bytes.
	first := NewDistinctRing[string](time.Hour)
	second := NewDistinctRing[string](time.Hour)
	for _, h := range []string{"10.0.0.3", "10.0.0.1", "10.0.0.2"} {
		first.Add(exportStart, h)
	}
	for _, h := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.1"} {
		second.Add(exportStart, h)
	}
	a, err := first.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	b, err := second.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("exports differ for identical contents:\n%s\n%s", a, b)
	}
}

func TestDistinctRingImportHonoursThePerBucketCeiling(t *testing.T) {
	// maxDistinctPerBucket is a memory ceiling Add already enforces; a
	// snapshot must not be a way around it, however it was written.
	values := make([]int, 0, maxDistinctPerBucket+50)
	for p := 0; p < maxDistinctPerBucket+50; p++ {
		values = append(values, p)
	}
	state := distinctRingState[int]{Buckets: []distinctBucketState[int]{{At: exportStart, Values: values}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshalling a hand-built state: %v", err)
	}

	restored := NewDistinctRing[int](time.Hour)
	if err := restored.ImportState(raw, exportStart); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if got := restored.Count(exportStart, time.Hour, nil); got != maxDistinctPerBucket {
		t.Errorf("restored distinct count = %d, want it capped at %d", got, maxDistinctPerBucket)
	}
}

func TestRingImportRebucketsOntoADifferentSpan(t *testing.T) {
	// An operator retuned the definition's window between snapshot and
	// restart: buckets are placed by their own timestamps at the
	// receiving ring's span, so two 1-minute buckets merge into one
	// 3-minute bucket rather than landing on arbitrary slots.
	source := NewCountRing(time.Hour) // span 1m
	source.Add(exportStart, true)
	source.Add(exportStart.Add(time.Minute), true)
	raw, err := source.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}

	restored := NewCountRing(3 * time.Hour) // span 3m
	now := exportStart.Add(2 * time.Minute)
	if err := restored.ImportState(raw, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if got := restored.Count(now, 3*time.Hour); got != 2 {
		t.Errorf("restored count at a coarser span = %d, want 2 (both buckets merged, none lost)", got)
	}
}
