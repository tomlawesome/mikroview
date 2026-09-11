// SPDX-License-Identifier: AGPL-3.0-only

package backupslice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// fakeSink records every call it receives, so a test can assert both
// what was stored and -- for the negative tests, which are the point of
// this file -- that it was never called at all.
type fakeSink struct {
	calls []fakeSinkCall
	err   error // if set, Store returns this instead of recording
}

type fakeSinkCall struct {
	device, kind string
	data         []byte
}

func (f *fakeSink) Store(device, kind string, data []byte, now time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, fakeSinkCall{device, kind, append([]byte(nil), data...)})
	return nil
}

// sliceUp splits data into maxSliceBytes chunks, the same way the router
// script's /file read loop would.
func sliceUp(data []byte) [][]byte {
	var out [][]byte
	for len(data) > 0 {
		n := maxSliceBytes
		if n > len(data) {
			n = len(data)
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

// sendAll drives a whole transfer through Begin then Slice, failing the
// test on any unexpected refusal.
func sendAll(t *testing.T, r *Receiver, device string, b Begin, data []byte, now time.Time) string {
	t.Helper()
	id, err := r.Begin(device, b, now)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	chunks := sliceUp(data)
	for i, c := range chunks {
		done, err := r.Slice(device, id, i, c, now)
		if err != nil {
			t.Fatalf("Slice %d: %v", i, err)
		}
		wantDone := i == len(chunks)-1
		if done != wantDone {
			t.Fatalf("Slice %d done = %v, want %v", i, done, wantDone)
		}
	}
	return id
}

func TestHappyPathReassemblesAMultiSliceRSC(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	data := bytes.Repeat([]byte("/interface print detail\n"), 4000) // several slices
	chunks := sliceUp(data)

	sendAll(t, r, "router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: int64(len(data)), TotalSlices: len(chunks)}, data, now)

	if len(sink.calls) != 1 {
		t.Fatalf("sink got %d calls, want 1", len(sink.calls))
	}
	got := sink.calls[0]
	if got.device != "router-1" || got.kind != backupvault.KindRsc {
		t.Fatalf("sink call = %+v", got)
	}
	if !bytes.Equal(got.data, data) {
		t.Fatal("reassembled bytes did not match the original byte-exact")
	}
	if r.InFlight() != 0 {
		t.Fatalf("InFlight = %d after completion, want 0", r.InFlight())
	}
}

func TestHappyPathReassemblesAMultiSliceBackupWithAValidHeader(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	data := append([]byte{0x88, 0xac, 0xa1, 0xb1}, bytes.Repeat([]byte("x"), 70000)...)
	chunks := sliceUp(data)
	sum := sha256.Sum256(data)

	sendAll(t, r, "router-2", Begin{
		Kind:        backupvault.KindBackup,
		TotalBytes:  int64(len(data)),
		TotalSlices: len(chunks),
		SHA256:      hex.EncodeToString(sum[:]),
	}, data, now)

	if len(sink.calls) != 1 {
		t.Fatalf("sink got %d calls, want 1", len(sink.calls))
	}
	got := sink.calls[0]
	if got.kind != backupvault.KindBackup || !bytes.Equal(got.data, data) {
		t.Fatal("backup was not reassembled byte-exact")
	}
}

func TestBeginTransferIDsAreServerAssignedHexAndUnique(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()

	id1, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(id1) != 32 {
		t.Fatalf("transfer id length = %d, want 32 (16 bytes hex)", len(id1))
	}
	if _, err := hex.DecodeString(id1); err != nil {
		t.Fatalf("transfer id %q is not hex: %v", id1, err)
	}

	id2, err := r.Begin("router-3", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatal("two Begin calls produced the same transfer id")
	}
}

func TestBeginRejectsInvalidParameters(t *testing.T) {
	cases := []struct {
		name string
		b    Begin
		want error
	}{
		{"zero TotalBytes", Begin{Kind: backupvault.KindRsc, TotalBytes: 0, TotalSlices: 0}, ErrBadTotalBytes},
		{"negative TotalBytes", Begin{Kind: backupvault.KindRsc, TotalBytes: -1, TotalSlices: 1}, ErrBadTotalBytes},
		{"TotalBytes over the vault cap", Begin{Kind: backupvault.KindRsc, TotalBytes: backupvault.MaxFileBytes + 1, TotalSlices: 512}, ErrBadTotalBytes},
		{"TotalSlices too low", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 1}, ErrBadTotalSlices},
		{"TotalSlices too high", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 3}, ErrBadTotalSlices},
		{"unknown kind", Begin{Kind: "exe", TotalBytes: 100, TotalSlices: 1}, ErrUnknownKind},
		{"empty kind", Begin{Kind: "", TotalBytes: 100, TotalSlices: 1}, ErrUnknownKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &fakeSink{}
			r := New(sink)
			_, err := r.Begin("router-1", tc.b, time.Now())
			if !errors.Is(err, tc.want) {
				t.Fatalf("Begin = %v, want %v", err, tc.want)
			}
			if len(sink.calls) != 0 {
				t.Fatal("sink was called on a refused Begin")
			}
			if r.InFlight() != 0 {
				t.Fatalf("InFlight = %d, want 0 after a refused Begin", r.InFlight())
			}
		})
	}
}

func TestSliceRefusesUnknownOrWrongDeviceTransferIDIdentically(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()

	_, err := r.Slice("router-1", "0123456789abcdef0123456789abcdef", 0, []byte("x"), now)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v, want ErrNotFound", err)
	}

	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Slice("router-2", id, 0, []byte("0123456789"), now)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong device: %v, want ErrNotFound -- must be indistinguishable from an unknown id", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called for a cross-device slice")
	}
}

func TestSliceRefusesOutOfOrderAndDropsTheWholeTransfer(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 2}, now)
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Slice("router-1", id, 1, bytes.Repeat([]byte("a"), 100), now)
	if !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("Slice with index 1 first = %v, want ErrOutOfOrder", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called on an out-of-order slice")
	}

	// The whole transfer was dropped, not just the bad slice -- even the
	// correct next index now comes back ErrNotFound.
	_, err = r.Slice("router-1", id, 0, bytes.Repeat([]byte("a"), 100), now)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Slice after the drop = %v, want ErrNotFound", err)
	}
}

func TestSliceRefusesASliceOverTheCap(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Slice("router-1", id, 0, bytes.Repeat([]byte("a"), maxSliceBytes+1), now)
	if !errors.Is(err, ErrSliceTooLarge) {
		t.Fatalf("Slice = %v, want ErrSliceTooLarge", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called on an oversized slice")
	}
}

func TestSliceRefusesAccumulatedBytesOverTheDeclaredTotal(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	// TotalBytes=40000 needs 2 slices; sending two max-size (32768) slices
	// accumulates 65536 bytes, well over the declared total, even though
	// each individual slice is within the per-slice cap.
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Slice("router-1", id, 0, bytes.Repeat([]byte("a"), maxSliceBytes), now); err != nil {
		t.Fatalf("first slice: %v", err)
	}
	_, err = r.Slice("router-1", id, 1, bytes.Repeat([]byte("a"), maxSliceBytes), now)
	if !errors.Is(err, ErrTotalExceeded) {
		t.Fatalf("Slice = %v, want ErrTotalExceeded", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called once the declared total was exceeded")
	}
}

func TestSliceRefusesABackupWithoutARouterOSHeader(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindBackup, TotalBytes: 100, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Slice("router-1", id, 0, bytes.Repeat([]byte("x"), 100), now)
	if !errors.Is(err, ErrNotABackup) {
		t.Fatalf("Slice = %v, want ErrNotABackup", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called for a backup with no valid header")
	}
}

// TestSliceAllowsTheRscKindWithNoHeaderCheck proves the header rule
// applies only to KindBackup -- arbitrary text must sail through as a
// single-slice .rsc.
func TestSliceAllowsTheRscKindWithNoHeaderCheck(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	data := []byte("# not a backup header at all\n/export\n")
	sendAll(t, r, "router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: int64(len(data)), TotalSlices: 1}, data, now)
	if len(sink.calls) != 1 {
		t.Fatalf("sink got %d calls, want 1", len(sink.calls))
	}
}

func TestBeginDiscardsThePreviousUnfinishedTransferForTheSameDevice(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	firstID, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatal("transfer ids must not repeat")
	}
	if r.InFlight() != 1 {
		t.Fatalf("InFlight = %d, want 1 -- a new Begin must discard the old transfer, not stack", r.InFlight())
	}
	_, err = r.Slice("router-1", firstID, 0, []byte("0123456789"), now)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Slice on the discarded transfer = %v, want ErrNotFound", err)
	}
	// The replacement transfer is unaffected.
	_, err = r.Slice("router-1", secondID, 0, []byte("0123456789"), now)
	if err != nil {
		t.Fatalf("Slice on the replacement transfer: %v", err)
	}
}

func TestBeginEnforcesTheGlobalInFlightDeviceCap(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	for i := 0; i < 32; i++ {
		device := fmt.Sprintf("router-%d", i)
		if _, err := r.Begin(device, Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now); err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
	}
	if r.InFlight() != 32 {
		t.Fatalf("InFlight = %d, want 32", r.InFlight())
	}

	_, err := r.Begin("router-33", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("the 33rd device's Begin = %v, want ErrBusy", err)
	}

	// A device already in flight can still restart its own transfer --
	// the cap is on distinct devices, not on Begin calls.
	if _, err := r.Begin("router-0", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now); err != nil {
		t.Fatalf("re-Begin for an already-in-flight device = %v, want nil", err)
	}
	if r.InFlight() != 32 {
		t.Fatalf("InFlight = %d after a re-Begin, want still 32", r.InFlight())
	}
}

func TestSweepDropsIdleTransfersButNotFreshOnes(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	start := time.Now()
	idleID, err := r.Begin("router-idle", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, start)
	if err != nil {
		t.Fatal(err)
	}
	later := start.Add(20 * time.Minute)
	freshID, err := r.Begin("router-fresh", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, later)
	if err != nil {
		t.Fatal(err)
	}

	if n := r.Sweep(later); n != 1 {
		t.Fatalf("Sweep dropped %d, want 1 (only the idle one)", n)
	}
	if r.InFlight() != 1 {
		t.Fatalf("InFlight = %d after Sweep, want 1", r.InFlight())
	}
	if _, err := r.Slice("router-idle", idleID, 0, []byte("0123456789"), later); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Slice on the swept transfer = %v, want ErrNotFound", err)
	}
	if _, err := r.Slice("router-fresh", freshID, 0, []byte("0123456789"), later); err != nil {
		t.Fatalf("the fresh transfer's slice was refused: %v", err)
	}
}

func TestSliceRefusesAFinalSliceThatDoesNotReachTheDeclaredTotal(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Slice("router-1", id, 0, bytes.Repeat([]byte("a"), maxSliceBytes), now); err != nil {
		t.Fatalf("first slice: %v", err)
	}
	// Only 5000 more bytes arrive on the final index, short of the
	// 40000 declared -- valid on its own (under caps) but wrong overall.
	_, err = r.Slice("router-1", id, 1, bytes.Repeat([]byte("a"), 5000), now)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("short final slice = %v, want ErrCorrupt", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called on a short reassembly")
	}
}

func TestSliceRefusesAFinalSliceWhoseSHA256DoesNotMatch(t *testing.T) {
	sink := &fakeSink{}
	r := New(sink)
	now := time.Now()
	data := bytes.Repeat([]byte("x"), 100)
	id, err := r.Begin("router-1", Begin{
		Kind:        backupvault.KindRsc,
		TotalBytes:  int64(len(data)),
		TotalSlices: 1,
		SHA256:      strings.Repeat("00", sha256.Size),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Slice("router-1", id, 0, data, now)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Slice = %v, want ErrCorrupt", err)
	}
	if len(sink.calls) != 0 {
		t.Fatal("sink was called despite a checksum mismatch")
	}
}

func TestSliceReturnsASinkErrorAsIsAndDropsTheTransfer(t *testing.T) {
	wantErr := errors.New("boom")
	sink := &fakeSink{err: wantErr}
	r := New(sink)
	now := time.Now()
	data := []byte("0123456789")
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: int64(len(data)), TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Slice("router-1", id, 0, data, now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Slice = %v, want %v", err, wantErr)
	}
	if r.InFlight() != 0 {
		t.Fatalf("InFlight = %d after a sink error, want 0 (transfer dropped)", r.InFlight())
	}
}

// TestBeginRefusesBeyondTheMemoryBudget covers the bound the device
// count alone does not give: a handful of devices each declaring a
// 16MiB file would reserve more than any memory-limited container has,
// so the budget is counted in declared bytes rather than in routers.
func TestBeginRefusesBeyondTheMemoryBudget(t *testing.T) {
	r := New(&fakeSink{})
	now := time.Now()
	big := Begin{Kind: backupvault.KindRsc, TotalBytes: backupvault.MaxFileBytes, TotalSlices: backupvault.MaxFileBytes / maxSliceBytes}

	// Three at the per-file cap: 48MiB of the 64MiB budget.
	for i := 0; i < 3; i++ {
		if _, err := r.Begin(fmt.Sprintf("rb%d", i), big, now); err != nil {
			t.Fatalf("Begin while the budget has room = %v, want nil", err)
		}
	}

	// A real backup is well under half a megabyte, so one still fits
	// alongside them -- the budget refuses sizes, not routers.
	if _, err := r.Begin("small", Begin{Kind: backupvault.KindRsc, TotalBytes: 1024, TotalSlices: 1}, now); err != nil {
		t.Fatalf("Begin for a small file = %v, want nil", err)
	}

	// A fourth at the per-file cap would take the total past the budget.
	if _, err := r.Begin("one-too-many", big, now); !errors.Is(err, ErrBusy) {
		t.Fatalf("Begin past the byte budget = %v, want ErrBusy", err)
	}
}

// blockingSink is a vault that takes its time: Store parks until the
// test lets it go, standing in for the seal-plus-two-atomic-writes a
// real backupvault.Store performs on a slow disk.
type blockingSink struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingSink) Store(device, kind string, data []byte, now time.Time) error {
	b.entered <- struct{}{}
	<-b.release
	return nil
}

// TestASlowSinkDoesNotBlockAnotherDevicesSlice is #1121's first item:
// the vault write used to run with the receiver lock held, so one
// router's final slice stalled every other router's POST for the length
// of a disk write -- and a stalled POST is a /tool fetch timeout, which
// aborts that router's whole transfer.
func TestASlowSinkDoesNotBlockAnotherDevicesSlice(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	r := New(sink)
	now := time.Now()

	slow := []byte("0123456789")
	slowID, err := r.Begin("router-slow", Begin{Kind: backupvault.KindRsc, TotalBytes: int64(len(slow)), TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	fastID, err := r.Begin("router-fast", Begin{Kind: backupvault.KindRsc, TotalBytes: 40000, TotalSlices: 2}, now)
	if err != nil {
		t.Fatal(err)
	}

	slowDone := make(chan error, 1)
	go func() {
		_, err := r.Slice("router-slow", slowID, 0, slow, now)
		slowDone <- err
	}()
	<-sink.entered // the slow router's file is now inside Store

	fastDone := make(chan error, 1)
	go func() {
		_, err := r.Slice("router-fast", fastID, 0, bytes.Repeat([]byte("a"), maxSliceBytes), now)
		fastDone <- err
	}()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatalf("the second device's slice = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second device's slice was still blocked two seconds into another device's vault write")
	}

	close(sink.release)
	if err := <-slowDone; err != nil {
		t.Fatalf("the slow transfer = %v, want nil", err)
	}
}

// TestRunPeriodicSweepReclaimsAnIdleTransferWithNoIngestTraffic is
// #1121's second item: sweeping only from the ingest handler meant an
// abandoned buffer sat there -- holding part of the shared byte budget
// other devices are refused against -- until some router happened to
// push again, which on a quiet install is the next night.
func TestRunPeriodicSweepReclaimsAnIdleTransferWithNoIngestTraffic(t *testing.T) {
	r := New(&fakeSink{})
	start := time.Now()
	var clock atomic.Int64
	clock.Store(start.UnixNano())
	r.now = func() time.Time { return time.Unix(0, clock.Load()) }

	if _, err := r.Begin("router-idle", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, start); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		r.runPeriodicSweep(ctx, time.Millisecond)
	}()

	// Nothing pushes anything from here on: only the clock moves.
	clock.Store(start.Add(idleExpiry + time.Minute).UnixNano())
	deadline := time.Now().Add(2 * time.Second)
	for r.InFlight() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the abandoned transfer was still held after the background sweep had two seconds to reclaim it")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep goroutine outlived its context")
	}
}

// TestBeginDoesNotAllocateTheDeclaredSizeUpFront is #1121's third item.
// Begin used to size a buffer from the caller's own declaration, so a
// token could declare 16MiB, send nothing, and repeat: the allocation
// below was a gigabyte of churn for a router that never sent a byte.
func TestBeginDoesNotAllocateTheDeclaredSizeUpFront(t *testing.T) {
	r := New(&fakeSink{})
	now := time.Now()
	declared := Begin{
		Kind:        backupvault.KindRsc,
		TotalBytes:  backupvault.MaxFileBytes,
		TotalSlices: backupvault.MaxFileBytes / maxSliceBytes,
	}

	const rounds = 64
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < rounds; i++ {
		// The same device each time: rule 6 discards the previous
		// transfer, which is exactly the loop a router restarting after
		// a refusal produces.
		if _, err := r.Begin("router-1", declared, now); err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
	}
	runtime.ReadMemStats(&after)

	// 64 declarations of 16MiB is a gigabyte if each one is allocated up
	// front. One file's worth is a generous ceiling for what these
	// begins should cost between them: transfer structs and map entries.
	if grew := after.TotalAlloc - before.TotalAlloc; grew > backupvault.MaxFileBytes {
		t.Fatalf("%d begins declaring %d bytes each allocated %d bytes, want well under %d",
			rounds, declared.TotalBytes, grew, backupvault.MaxFileBytes)
	}
}
