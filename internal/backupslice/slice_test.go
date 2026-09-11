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
		res, err := r.Slice(device, id, i, c, now)
		if err != nil {
			t.Fatalf("Slice %d: %v", i, err)
		}
		wantDone := i == len(chunks)-1
		if res.Done != wantDone {
			t.Fatalf("Slice %d done = %v, want %v", i, res.Done, wantDone)
		}
		if !wantDone {
			continue
		}
		// The completed transfer reports what actually arrived, which is
		// what the caller audits: the push script sends neither kind nor
		// size on a slice (#1122).
		if res.Kind != b.Kind || res.Bytes != int64(len(data)) {
			t.Fatalf("the final slice reported %+v, want kind %q and %d bytes", res, b.Kind, len(data))
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
	// ...and marked as the sink's failure rather than the caller's, so
	// an HTTP caller can answer it as a server fault (#1122).
	if !errors.Is(err, ErrSink) {
		t.Fatalf("Slice = %v, want it to wrap ErrSink too", err)
	}
	if r.InFlight() != 0 {
		t.Fatalf("InFlight = %d after a sink error, want 0 (transfer dropped)", r.InFlight())
	}
}

// TestTheByteBudgetIsChargedOnBytesReceivedNotOnDeclarations covers both
// halves of the accounting #1121 got wrong. Begin used to reserve the
// caller's declared TotalBytes even though it allocates nothing, so four
// devices declaring 16MiB pinned the whole 64MiB budget for fifteen
// minutes while holding one slice each -- and nothing at all was charged
// for the bytes that did arrive.
func TestTheByteBudgetIsChargedOnBytesReceivedNotOnDeclarations(t *testing.T) {
	r := New(&fakeSink{})
	now := time.Now()
	const perDevice = backupvault.MaxFileBytes
	big := Begin{Kind: backupvault.KindRsc, TotalBytes: perDevice, TotalSlices: perDevice / maxSliceBytes}

	// Five declarations of 16MiB is 80MiB, more than the whole budget.
	// Every one is admitted: a declaration is not bytes in hand, and a
	// sender that declares 16MiB may send nothing at all.
	var ids []string
	for i := 0; i < 5; i++ {
		id, err := r.Begin(fmt.Sprintf("rb%d", i), big, now)
		if err != nil {
			t.Fatalf("Begin %d = %v, want nil -- a declaration reserves nothing", i, err)
		}
		ids = append(ids, id)
	}

	// Three of them now deliver all but the last slice of their 16MiB,
	// so the bytes really are resident and really are charged -- three
	// quarters of the budget, held by transfers still in flight.
	for i := 0; i < 3; i++ {
		device := fmt.Sprintf("rb%d", i)
		chunks := sliceUp(make([]byte, perDevice))
		for j, c := range chunks[:len(chunks)-1] {
			if _, err := r.Slice(device, ids[i], j, c, now); err != nil {
				t.Fatalf("%s slice %d = %v, want nil", device, j, err)
			}
		}
	}

	// The fourth is refused part-way, where the bytes in hand fill the
	// budget -- not at begin, and not never.
	var accepted int
	var refusal error
	for j, c := range sliceUp(make([]byte, perDevice)) {
		if _, err := r.Slice("rb3", ids[3], j, c, now); err != nil {
			refusal = err
			break
		}
		accepted++
	}
	if !errors.Is(refusal, ErrBusy) {
		t.Fatalf("the slice past the budget = %v, want ErrBusy", refusal)
	}
	if accepted == 0 {
		t.Fatal("the fourth transfer was refused its first slice -- the budget is still being charged on declarations")
	}
	if got := int64(accepted) * maxSliceBytes; got > maxInFlightBytes {
		t.Fatalf("%d bytes were accepted past the %d-byte budget", got, maxInFlightBytes)
	}
}

// TestACompletingTransferKeepsItsSlotUntilTheSinkReturns is the other
// end of #1121's accounting: a transfer used to leave both maps before
// Store ran, so the whole file sat in memory charged to nobody while the
// vault sealed and wrote it, and the device could start another push on
// top of it immediately.
func TestACompletingTransferKeepsItsSlotUntilTheSinkReturns(t *testing.T) {
	sink := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	r := New(sink)
	now := time.Now()
	data := bytes.Repeat([]byte("a"), maxSliceBytes)
	id, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: int64(len(data)), TotalSlices: 1}, now)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := r.Slice("router-1", id, 0, data, now)
		done <- err
	}()
	<-sink.entered // the file is now inside Store

	if n := r.InFlight(); n != 1 {
		t.Fatalf("InFlight = %d while the sink still holds the file, want 1 -- its bytes are resident and must stay charged", n)
	}
	if _, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now); !errors.Is(err, ErrBusy) {
		t.Fatalf("Begin while this device's last transfer is in the sink = %v, want ErrBusy", err)
	}
	// The per-slice buffers go the moment the whole file exists, so the
	// two copies are never both resident across the write.
	r.mu.Lock()
	tr := r.transfers[id]
	r.mu.Unlock()
	if tr == nil {
		t.Fatal("the completing transfer is not held at all, so nothing is charged for the bytes in the sink")
	}
	if tr.chunks != nil {
		t.Fatalf("the transfer still holds %d slice buffers alongside the reassembled file", len(tr.chunks))
	}
	// No further slice belongs to a transfer that is already finished.
	if _, err := r.Slice("router-1", id, 1, []byte("x"), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a slice for a transfer already in the sink = %v, want ErrNotFound", err)
	}

	close(sink.release)
	if err := <-done; err != nil {
		t.Fatalf("the completed transfer = %v, want nil", err)
	}
	if n := r.InFlight(); n != 0 {
		t.Fatalf("InFlight = %d once the sink returned, want 0", n)
	}
	if _, err := r.Begin("router-1", Begin{Kind: backupvault.KindRsc, TotalBytes: 10, TotalSlices: 1}, now); err != nil {
		t.Fatalf("Begin once the sink is done = %v, want nil", err)
	}
}

// TestRefusalTextsAreWrittenForTheirReaders: every one of these reaches
// a router (as an HTTP body) or an admin (as an audit detail), and
// neither can do anything with the name of a Go package (#1122).
func TestRefusalTextsAreWrittenForTheirReaders(t *testing.T) {
	for _, err := range []error{
		ErrNotFound, ErrOutOfOrder, ErrSliceTooLarge, ErrBadTotalBytes,
		ErrBadTotalSlices, ErrTotalExceeded, ErrUnknownKind, ErrNotABackup,
		ErrBusy, ErrCorrupt, ErrSink, ErrServer, ErrTooManySlices,
	} {
		if strings.HasPrefix(err.Error(), "backupslice:") {
			t.Errorf("%q names this package to whoever reads it", err)
		}
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
