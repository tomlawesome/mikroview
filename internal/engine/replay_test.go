// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"strings"
	"testing"
	"time"
)

func mustWindow(t *testing.T, start, end time.Time, count int) Window {
	t.Helper()
	w, err := NewWindow(start, end, count)
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	return w
}

// --- Window ---

func TestNewWindowRequiresNonZeroInstants(t *testing.T) {
	now := time.Now()
	if _, err := NewWindow(time.Time{}, now, 1); err == nil {
		t.Fatal("NewWindow succeeded with a zero start, want a hard failure")
	}
	if _, err := NewWindow(now, time.Time{}, 1); err == nil {
		t.Fatal("NewWindow succeeded with a zero end, want a hard failure")
	}
}

func TestNewWindowRejectsEndBeforeStart(t *testing.T) {
	now := time.Now()
	if _, err := NewWindow(now, now.Add(-time.Second), 1); err == nil {
		t.Fatal("NewWindow succeeded with end before start, want a hard failure")
	}
}

func TestNewWindowRejectsNegativeEventCount(t *testing.T) {
	now := time.Now()
	if _, err := NewWindow(now.Add(-time.Second), now, -1); err == nil {
		t.Fatal("NewWindow succeeded with a negative event count, want a hard failure")
	}
}

func TestNewWindowDerivesDurationFromStartEnd(t *testing.T) {
	now := time.Now()
	w := mustWindow(t, now.Add(-5*time.Minute), now, 42)
	if w.Duration() != 5*time.Minute {
		t.Errorf("Duration() = %s, want 5m0s", w.Duration())
	}
	if w.EventCount() != 42 {
		t.Errorf("EventCount() = %d, want 42", w.EventCount())
	}
	if !w.Start().Equal(now.Add(-5 * time.Minute)) {
		t.Errorf("Start() = %s, want %s", w.Start(), now.Add(-5*time.Minute))
	}
	if !w.End().Equal(now) {
		t.Errorf("End() = %s, want %s", w.End(), now)
	}
}

// TestWindowZeroValueNotRenderable pins issue #403's own requirement:
// "a test proves the zero value is not renderable." A Window obtained
// by bypassing NewWindow entirely (the one thing Go cannot stop a
// caller doing) must not format as though it were legitimate data.
func TestWindowZeroValueNotRenderable(t *testing.T) {
	var w Window
	defer func() {
		if recover() == nil {
			t.Fatal("Window{}.String() did not panic on the zero value -- it must refuse to render rather than print a fabricated-looking zero window")
		}
	}()
	_ = w.String()
}

// --- Receipt ---

func sampleAt(t time.Time) ReplaySample {
	return ReplaySample{At: t, Target: "198.51.100.7", Detail: "test detail", Ports: []int{22}}
}

func TestNewReceiptRequiresValidWindow(t *testing.T) {
	if _, err := NewReceipt(Window{}, 0, nil, false); err == nil {
		t.Fatal("NewReceipt succeeded with a zero-value Window, want a hard failure")
	}
}

func TestNewReceiptRejectsNegativeEmissionCount(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)
	if _, err := NewReceipt(w, -1, nil, false); err == nil {
		t.Fatal("NewReceipt succeeded with a negative emission count, want a hard failure")
	}
}

func TestNewReceiptRejectsSampleExceedingBound(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)
	sample := make([]ReplaySample, replaySampleBound+1)
	for i := range sample {
		sample[i] = sampleAt(time.Now())
	}
	if _, err := NewReceipt(w, len(sample), sample, false); err == nil {
		t.Fatal("NewReceipt succeeded with a sample exceeding replaySampleBound, want a hard failure")
	}
}

func TestNewReceiptRejectsSampleExceedingEmissionCount(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)
	sample := []ReplaySample{sampleAt(time.Now()), sampleAt(time.Now())}
	if _, err := NewReceipt(w, 1, sample, false); err == nil {
		t.Fatal("NewReceipt succeeded with len(sample) > emissionCount, want a hard failure")
	}
}

// TestReceiptZeroValueNotRenderable is Receipt's own half of issue
// #403's "a test proves the zero value is not renderable" requirement.
func TestReceiptZeroValueNotRenderable(t *testing.T) {
	var r Receipt
	defer func() {
		if recover() == nil {
			t.Fatal("Receipt{}.String() did not panic on the zero value -- it must refuse to render rather than print a fabricated-looking empty receipt")
		}
	}()
	_ = r.String()
}

func TestReceiptSampleIsCopyOnRead(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)
	r, err := NewReceipt(w, 1, []ReplaySample{sampleAt(time.Now())}, false)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}

	got := r.Sample()
	got[0].Target = "mutated"

	again := r.Sample()
	if again[0].Target == "mutated" {
		t.Fatal("mutating a Sample() result reached back into the Receipt's own state -- Sample must be copy-on-read")
	}
}

func TestReceiptSampleTruncatedReflectsBound(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)

	full, err := NewReceipt(w, 3, []ReplaySample{sampleAt(time.Now()), sampleAt(time.Now()), sampleAt(time.Now())}, false)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if full.SampleTruncated() {
		t.Error("SampleTruncated() = true when len(sample) == emissionCount, want false")
	}

	bounded, err := NewReceipt(w, 5, []ReplaySample{sampleAt(time.Now()), sampleAt(time.Now())}, false)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if !bounded.SampleTruncated() {
		t.Error("SampleTruncated() = false when len(sample) < emissionCount, want true")
	}
}

func TestReceiptAnyProvisionalComputedFromSample(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)

	none, err := NewReceipt(w, 1, []ReplaySample{sampleAt(time.Now())}, false)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if none.AnyProvisional() {
		t.Error("AnyProvisional() = true with no provisional sample, want false")
	}

	provisional := sampleAt(time.Now())
	provisional.Provisional = true
	some, err := NewReceipt(w, 2, []ReplaySample{sampleAt(time.Now()), provisional}, false)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if !some.AnyProvisional() {
		t.Error("AnyProvisional() = false with a provisional sample present, want true")
	}
}

func TestReceiptCorpusTruncatedPassesThrough(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-time.Minute), time.Now(), 10)
	r, err := NewReceipt(w, 0, nil, true)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if !r.CorpusTruncated() {
		t.Error("CorpusTruncated() = false, want true (constructed with corpusTruncated=true)")
	}
}

func TestReceiptStringIncludesWindowAndFlags(t *testing.T) {
	w := mustWindow(t, time.Now().Add(-10*time.Minute), time.Now(), 500)
	r, err := NewReceipt(w, 12, []ReplaySample{sampleAt(time.Now())}, true)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	s := r.String()
	if !strings.Contains(s, "12 emission") {
		t.Errorf("String() = %q, want it to state the emission count", s)
	}
	if !strings.Contains(s, "corpus truncated") {
		t.Errorf("String() = %q, want it to flag corpus truncation", s)
	}
	if !strings.Contains(s, "sample truncated") {
		t.Errorf("String() = %q, want it to flag sample truncation (12 emissions, 1 sampled)", s)
	}
}
