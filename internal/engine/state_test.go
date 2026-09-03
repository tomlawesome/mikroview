// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

func flushForTest(t *testing.T, s *StateStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

func TestOpenStateStoreEmptyPathIsUsable(t *testing.T) {
	s, err := OpenStateStore("")
	if err != nil {
		t.Fatalf("OpenStateStore(\"\"): %v", err)
	}
	s.Set("port_scan", "203.0.113.9", BaselineState{Value: 4.2, Samples: 10})
	got, ok := s.Get("port_scan", "203.0.113.9")
	if !ok {
		t.Fatal("expected an in-memory-only store to still retain a Set within the same process")
	}
	if got.Value != 4.2 || got.Samples != 10 {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestOpenStateStoreMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-state.json")
	s, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore on a missing file: %v", err)
	}
	if got := s.Snapshot("anything"); len(got) != 0 {
		t.Errorf("expected an empty store, got %v", got)
	}
}

func TestStateStorePersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-state.json")

	s1, err := OpenStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	want := BaselineState{Value: 12.5, Variance: 3.1, Samples: 42, FirstSeen: now, Primed: true}
	s1.Set("host_baseline", "10.0.0.5", want)
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, s1)

	s2, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	got, ok := s2.Get("host_baseline", "10.0.0.5")
	if !ok {
		t.Fatal("expected the persisted state to survive reopening")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStateStoreSnapshotIsIndependentCopy(t *testing.T) {
	s, _ := OpenStateStore("")
	s.Set("global_spike", "global", BaselineState{Value: 1})

	snap := s.Snapshot("global_spike")
	snap["global"] = BaselineState{Value: 999}

	got, _ := s.Get("global_spike", "global")
	if got.Value == 999 {
		t.Error("mutating a Snapshot result affected the store's own state -- Snapshot must be a copy")
	}
}

func TestStateStoreDeleteDefinitionRemovesAllKeys(t *testing.T) {
	s, _ := OpenStateStore("")
	s.Set("rule_spike", "r1", BaselineState{Value: 1})
	s.Set("rule_spike", "r2", BaselineState{Value: 2})
	s.DeleteDefinition("rule_spike")

	if got := s.Snapshot("rule_spike"); len(got) != 0 {
		t.Errorf("expected DeleteDefinition to remove every key, got %v", got)
	}
}

// TestOpenStateStoreLoadsADocumentCarryingAVersionKey pins that removing
// stateDocument.Version (#873) did not also break loading a document
// that still has one on disk -- Go's JSON decoder ignores unknown
// fields, so a file written by an older binary, or one hand-written with
// a "version" key, must load exactly like one without it.
func TestOpenStateStoreLoadsADocumentCarryingAVersionKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-state.json")
	data := `{"version":1,"definitions":{"port_scan":{"203.0.113.9":{"value":4.2,"samples":10}}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := OpenStateStore(path)
	if err != nil {
		t.Fatalf("OpenStateStore on a document carrying a version key: %v", err)
	}
	got, ok := s.Get("port_scan", "203.0.113.9")
	if !ok {
		t.Fatal("expected the persisted state to load despite the extra version key")
	}
	if got.Value != 4.2 || got.Samples != 10 {
		t.Errorf("unexpected state: %+v", got)
	}
}

// TestOpenStateStoreMalformedFileFailsClosed pins issue #378's policy
// for this store too: a document that exists but cannot be parsed is
// refused outright, not treated as empty.
func TestOpenStateStoreMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := OpenStateStore(path)
	if err == nil {
		t.Fatal("expected a non-nil error for a malformed file, want fail-closed")
	}
	if s != nil {
		t.Error("expected a nil store on a load failure -- a non-nil store here would still carry a live backend")
	}
}

// unreadableBackend always fails Load -- see persist's own
// TestOpenWriteBehindNeverAttachesToAFailedLoad, reproduced here against
// the real StateStore constructor rather than persist.OpenWriteBehind
// directly, so the engine-state store's own adoption is the thing under
// test, not just the shared helper underneath it.
type unreadableBackend struct{}

func (unreadableBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, errors.New("disk on fire")
}
func (unreadableBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("must never be called")
}
func (unreadableBackend) Close() error     { return nil }
func (unreadableBackend) Describe() string { return "unreadable test backend" }

// TestOpenStateStoreWithBackendNeverAttachesToAFailedLoad is #400's
// critical invariant for this store: a document that exists but can't
// be loaded must produce no *StateStore at all, so there is no object a
// caller could ever Set against for a store whose load failed.
func TestOpenStateStoreWithBackendNeverAttachesToAFailedLoad(t *testing.T) {
	s, err := OpenStateStoreWithBackend(unreadableBackend{})
	var startupErr *persist.StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("OpenStateStoreWithBackend error = %v, want a *persist.StartupError", err)
	}
	if s != nil {
		t.Fatal("OpenStateStoreWithBackend returned a non-nil *StateStore against a failed load -- " +
			"a queued Set could now write over data nothing has actually confirmed is stale")
	}
}

// stallingSaveBackend blocks every Save call until released -- see
// flags.stallingSaveBackend, the twin of this type.
type stallingSaveBackend struct {
	mu       sync.Mutex
	release  chan struct{}
	version  int64
	inFlight int
	maxIn    int
}

func newStallingSaveBackend() *stallingSaveBackend {
	return &stallingSaveBackend{release: make(chan struct{})}
}

func (b *stallingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *stallingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxIn {
		b.maxIn = b.inFlight
	}
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		b.mu.Lock()
		b.inFlight--
		b.mu.Unlock()
		return 0, ctx.Err()
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight--
	b.version++
	return b.version, nil
}

func (b *stallingSaveBackend) Close() error     { return nil }
func (b *stallingSaveBackend) Describe() string { return "stalling test backend" }

func (b *stallingSaveBackend) maxInFlight() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxIn
}

// TestStateStoreSetDoesNotBlockOnAStuckBackend proves Set never holds
// its lock across a backend call, same proof every other adopted store
// carries.
func TestStateStoreSetDoesNotBlockOnAStuckBackend(t *testing.T) {
	orig := engineStateFlushInterval
	engineStateFlushInterval = time.Millisecond
	defer func() { engineStateFlushInterval = orig }()

	b := newStallingSaveBackend()
	s, err := OpenStateStoreWithBackend(b)
	if err != nil {
		t.Fatalf("OpenStateStoreWithBackend: %v", err)
	}
	defer func() {
		close(b.release)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Close(ctx)
	}()

	s.Set("port_scan", "1.1.1.1", BaselineState{Value: 1})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Set("port_scan", "2.2.2.2", BaselineState{Value: float64(i)})
			s.Snapshot("port_scan")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Set/Snapshot blocked against a stalled backend -- the lock must never be held across a backend call")
	}

	if got := b.maxInFlight(); got > 1 {
		t.Errorf("%d concurrent Save calls reached the backend, want at most 1", got)
	}
}

// TestBaselineStateRoundTripsThroughStateStore proves the persisted
// shape genuinely matches what Baseline needs to resume warm -- not
// just structurally plausible fields, but an actual Baseline reading
// the same z-score it would have before a restart.
func TestBaselineStateRoundTripsThroughStateStore(t *testing.T) {
	b := NewBaseline(time.Minute, BaselineFloor{MinSamples: 3}, UpdatePerEvent)
	now := time.Now()
	// Prime it: the first reading inside the window is discarded for
	// priming purposes (see Baseline.Reading's own doc comment), so seed
	// past the window first.
	b.Reading(now, 10)
	b.Reading(now.Add(2*time.Minute), 10)
	b.Reading(now.Add(3*time.Minute), 12)
	b.Reading(now.Add(4*time.Minute), 11)
	before := b.Snapshot(now.Add(5 * time.Minute))
	if !before.Primed {
		t.Fatal("test setup: expected the baseline to be primed by now")
	}

	s, _ := OpenStateStore("")
	s.Set("host_baseline", "10.0.0.5", b.State())

	restoredState, ok := s.Get("host_baseline", "10.0.0.5")
	if !ok {
		t.Fatal("expected the state just Set to be retrievable")
	}
	restored := RestoreBaseline(time.Minute, BaselineFloor{MinSamples: 3}, UpdatePerEvent, restoredState)
	after := restored.Snapshot(now.Add(5 * time.Minute))

	if after.Value != before.Value || after.Variance != before.Variance || after.Samples != before.Samples {
		t.Errorf("restored baseline state = %+v, want %+v", after, before)
	}
	if !after.Ready {
		t.Error("expected the restored baseline to already be past its floor, same as before persisting")
	}
}
