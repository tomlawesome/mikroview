// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// flushForTest waits for r's write-behind writer to persist whatever is
// currently dirty -- issue #400 moved persistence off the caller's
// goroutine, so a test reopening the same path immediately after a Seen
// now needs an explicit synchronous checkpoint. See
// flags.flushForTest, the twin of this helper.
func flushForTest(t *testing.T, r *MACRegistry) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

// countingSaveBackend is an in-memory persist.Backend that counts Save
// calls -- see flags.countingSaveBackend, the twin of this type.
type countingSaveBackend struct {
	mu      sync.Mutex
	payload []byte
	version int64
	saves   int
}

func newCountingSaveBackend() *countingSaveBackend { return &countingSaveBackend{} }

func (b *countingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return persist.Snapshot{Payload: b.payload, Version: b.version, Exists: b.version != 0}, nil
}

func (b *countingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if expect != b.version {
		return 0, persist.ErrConflict
	}
	b.saves++
	b.payload = payload
	b.version++
	return b.version, nil
}

func (b *countingSaveBackend) Close() error     { return nil }
func (b *countingSaveBackend) Describe() string { return "counting test backend" }

func (b *countingSaveBackend) saveCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

func (b *countingSaveBackend) lastPayload() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.payload
}

func TestOpenMACRegistryEmptyPathIsUsable(t *testing.T) {
	r, err := OpenMACRegistry("")
	if err != nil {
		t.Fatalf("OpenMACRegistry(\"\") returned an error: %v", err)
	}
	if !r.Seen("aa:bb:cc:dd:ee:ff", time.Now()) {
		t.Error("expected an in-memory-only registry to still report the first sighting as new")
	}
}

func TestOpenMACRegistryMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mac-registry.json")
	r, err := OpenMACRegistry(path)
	if err != nil {
		t.Fatalf("OpenMACRegistry() on a missing file returned an error: %v", err)
	}
	if len(r.List()) != 0 {
		t.Errorf("expected an empty registry, got %d entries", len(r.List()))
	}
}

// A JSON array containing null is syntactically valid, so it unmarshals
// without error into a slice with a nil *MACEntry element -- same
// defensive skip flags.Open's TestOpenSkipsNilArrayElements guards
// against.
func TestOpenMACRegistrySkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mac-registry.json")
	data := `[null, {"mac":"aa:bb:cc:dd:ee:ff","firstSeen":"2024-01-01T00:00:00Z","lastSeen":"2024-01-01T00:00:00Z"}, null]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := OpenMACRegistry(path) // must not panic
	if err != nil {
		t.Fatalf("OpenMACRegistry() returned an unexpected error: %v", err)
	}
	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected the one real entry to survive, got %d: %+v", len(list), list)
	}
	if list[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("expected the real entry's data to be intact, got %+v", list[0])
	}
}

// TestOpenMACRegistryMalformedFileFailsClosed pins issue #378's policy:
// a document that exists but cannot be parsed is refused outright, not
// treated as empty. See flags.TestOpenMalformedFileFailsClosed for the
// full reasoning -- same fix, same shape, applied through the same
// shared persist.Open helper.
func TestOpenMACRegistryMalformedFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mac-registry.json")
	original := []byte("not valid json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := OpenMACRegistry(path)
	if err == nil {
		t.Fatal("expected a non-nil error for a malformed file, want fail-closed")
	}
	if r != nil {
		t.Error("expected a nil registry on a load failure -- a non-nil registry here would still carry a live backend")
	}
	// See flags.TestOpenMalformedFileFailsClosed's identical assertion
	// for why this is the actual regression check: the file must be
	// exactly as it was, and there is no longer a store left that could
	// have written over it even if this assertion were skipped.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Errorf("the file changed across a failed OpenMACRegistry: before %q, after %q", original, after)
	}
}

// TestSeenFiresOnceThenNeverAgain is the core contract issue #103 phase 1
// needs: the very first sighting of a MAC is reported as new, and every
// subsequent sighting of the *same* MAC (even much later, even after
// other MACs have been seen in between) must never be reported as new
// again.
func TestSeenFiresOnceThenNeverAgain(t *testing.T) {
	r, _ := OpenMACRegistry("")
	now := time.Now()

	if !r.Seen("aa:bb:cc:dd:ee:ff", now) {
		t.Fatal("expected the first sighting of a MAC to report true (new)")
	}
	if r.Seen("aa:bb:cc:dd:ee:ff", now.Add(time.Second)) {
		t.Error("expected the second sighting of the same MAC to report false (not new)")
	}
	if r.Seen("aa:bb:cc:dd:ee:ff", now.Add(time.Hour)) {
		t.Error("expected a much later sighting of the same MAC to still report false (not new)")
	}

	// A different MAC is independently new.
	if !r.Seen("11:22:33:44:55:66", now.Add(time.Minute)) {
		t.Error("expected a different MAC's first sighting to report true (new)")
	}
}

func TestSeenEmptyMACIsNoOp(t *testing.T) {
	r, _ := OpenMACRegistry("")
	if r.Seen("", time.Now()) {
		t.Error("expected an empty MAC to report false, never treated as a new device")
	}
	if len(r.List()) != 0 {
		t.Errorf("expected an empty MAC to never be recorded, got %d entries", len(r.List()))
	}
}

func TestSeenTracksFirstAndLastSeen(t *testing.T) {
	r, _ := OpenMACRegistry("")
	t0 := time.Now()
	t1 := t0.Add(time.Hour)

	r.Seen("aa:bb:cc:dd:ee:ff", t0)
	r.Seen("aa:bb:cc:dd:ee:ff", t1)

	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	e := list[0]
	if !e.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen = %v, want %v (should not move on a re-sighting)", e.FirstSeen, t0)
	}
	if !e.LastSeen.Equal(t1) {
		t.Errorf("LastSeen = %v, want %v", e.LastSeen, t1)
	}
}

// Different textual forms of the same MAC (case differences) must
// collapse to one registry entry rather than each looking "new" -- same
// reasoning Registry's own TestResolveNormalizesEquivalentIPForms
// establishes for source IPs.
func TestSeenNormalizesMACCase(t *testing.T) {
	r, _ := OpenMACRegistry("")
	now := time.Now()

	if !r.Seen("AA:BB:CC:DD:EE:FF", now) {
		t.Fatal("expected the first sighting to report true (new)")
	}
	if r.Seen("aa:bb:cc:dd:ee:ff", now.Add(time.Second)) {
		t.Error("expected a different-case form of the same MAC to be recognized as already seen")
	}

	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected both forms to collapse to one entry, got %d: %+v", len(list), list)
	}
}

// TestPersistLockedRateLimitsWrites mirrors flags.Store's own test of the
// same name/reasoning: a sustained stream of Seen() calls (the ingest hot
// path, called on every event carrying a SrcMAC) must not hit the
// backend once per call. Issue #400: persistence is now write-behind, so
// this counts backend Save calls through a fake persist.Backend rather
// than racing a file write against a fixed sleep -- see
// flags.TestPersistLockedRateLimitsWrites for the same rewrite and why.
func TestMACRegistryPersistLockedRateLimitsWrites(t *testing.T) {
	b := newCountingSaveBackend()
	r, err := OpenMACRegistryWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	r.Seen("11:11:11:11:11:11", now)
	flushForTest(t, r) // the very first write attempts immediately; wait for it as a baseline
	if got := b.saveCount(); got != 1 {
		t.Fatalf("expected the first Seen to write immediately, got %d saves", got)
	}

	// Within the debounce window: must coalesce into the next flush
	// rather than producing its own attempt.
	r.Seen("22:22:22:22:22:22", now)
	r.Seen("33:33:33:33:33:33", now)
	flushForTest(t, r)
	if got := b.saveCount(); got != 2 {
		t.Errorf("expected 2 total saves after 3 Seens inside one debounce window (1 immediate + 1 coalesced flush), got %d", got)
	}
	if len(r.List()) != 3 {
		t.Fatalf("in-memory state should always have all 3 regardless of persistence timing, got %d", len(r.List()))
	}
	if payload := b.lastPayload(); !strings.Contains(string(payload), "33:33:33:33:33:33") {
		t.Errorf("expected the flushed write to include all 3 entries, got:\n%s", payload)
	}
}

// TestPersistenceRoundTrip proves the actual point of this whole store:
// a MAC seen before a restart must still be recognized as "already
// known" after reopening from disk -- the 24h event-retention window
// alone can't provide this, which is why this exists as a separate,
// durable store in the first place.
func TestMACRegistryPersistenceRoundTrip(t *testing.T) {
	orig := macRegistryPersistMinInterval
	macRegistryPersistMinInterval = 0
	defer func() { macRegistryPersistMinInterval = orig }()

	path := filepath.Join(t.TempDir(), "nested", "mac-registry.json")

	r1, err := OpenMACRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if !r1.Seen("aa:bb:cc:dd:ee:ff", now) {
		t.Fatal("expected the first sighting to report true (new)")
	}
	// #400: write-behind -- flush before reopening, see flushForTest.
	flushForTest(t, r1)

	r2, err := OpenMACRegistry(path)
	if err != nil {
		t.Fatalf("re-opening the persisted registry failed: %v", err)
	}
	if r2.Seen("aa:bb:cc:dd:ee:ff", now.Add(time.Hour)) {
		t.Error("expected a MAC persisted before restart to still be recognized as already known after reopening")
	}
	// Seen() persists unconditionally, even on an already-known MAC (it
	// still bumps LastSeen), so r2 now has its own pending write-behind
	// write in flight -- flush it before the test returns and t.TempDir()
	// removes the directory out from under it, or the two can race
	// ("directory not empty" from a write landing mid-RemoveAll).
	flushForTest(t, r2)

	list := r2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 persisted entry after reopening, got %d: %+v", len(list), list)
	}
	if !list[0].FirstSeen.Equal(now) {
		t.Errorf("expected FirstSeen to survive persistence unchanged, got %v, want %v", list[0].FirstSeen, now)
	}
}

func TestMACRegistryPruneEvictsOldestOverCap(t *testing.T) {
	orig := maxMACRegistryEntries
	maxMACRegistryEntries = 3
	defer func() { maxMACRegistryEntries = orig }()

	r, _ := OpenMACRegistry("")
	now := time.Now()

	r.Seen("11:11:11:11:11:11", now)
	r.Seen("22:22:22:22:22:22", now.Add(time.Minute))
	r.Seen("33:33:33:33:33:33", now.Add(2*time.Minute))
	r.Seen("44:44:44:44:44:44", now.Add(3*time.Minute)) // pushes over the cap

	// At most the cap, deliberately not exactly it -- see
	// TestMACRegistryShedsABatchSoTheNextNewMACIsFree.
	list := r.List()
	if len(list) > maxMACRegistryEntries {
		t.Fatalf("expected pruning to hold the registry at or under %d, got %d: %+v", maxMACRegistryEntries, len(list), list)
	}
	for _, e := range list {
		if e.MAC == "11:11:11:11:11:11" {
			t.Error("expected the oldest-by-LastSeen entry to be evicted")
		}
	}
	if len(list) == 0 || list[0].MAC != "44:44:44:44:44:44" {
		t.Errorf("expected the most-recently-seen MAC to survive, got %+v", list)
	}
}

// Evicting back to exactly the cap leaves the registry full, so the next
// new MAC overflows too and pays the whole scan again -- for every event
// thereafter. Seen runs on the single ingest goroutine keyed on a src-mac
// that comes straight off unauthenticated syslog, so that is a permanent
// state an attacker can hold the registry in: measured at 1,529 ns per
// Seen under the cap against 16-21 ms at it.
//
// The property that stops it is that a shed leaves headroom. Asserting
// on the headroom rather than on timings keeps this a contract test
// rather than a benchmark that fails on a busy machine.
func TestMACRegistryShedsABatchSoTheNextNewMACIsFree(t *testing.T) {
	orig := maxMACRegistryEntries
	maxMACRegistryEntries = 800
	defer func() { maxMACRegistryEntries = orig }()

	r, _ := OpenMACRegistry("")
	now := time.Now()
	for i := 0; i <= maxMACRegistryEntries; i++ { // one past the cap, forcing a shed
		r.Seen(fmt.Sprintf("02:00:00:%02x:%02x:%02x", i>>16&0xff, i>>8&0xff, i&0xff), now.Add(time.Duration(i)*time.Second))
	}

	after := len(r.List())
	if after >= maxMACRegistryEntries {
		t.Fatalf("the shed left the registry at %d against a cap of %d -- no headroom, so the next new MAC sheds again",
			after, maxMACRegistryEntries)
	}

	// Every insertion up to the headroom must now be free of a shed.
	headroom := maxMACRegistryEntries - after
	for i := 0; i < headroom; i++ {
		r.Seen(fmt.Sprintf("06:00:00:%02x:%02x:%02x", i>>16&0xff, i>>8&0xff, i&0xff), now.Add(time.Hour))
	}
	if got := len(r.List()); got != maxMACRegistryEntries {
		t.Errorf("filling the %d-entry headroom gave %d entries, want %d -- a shed ran that should not have",
			headroom, got, maxMACRegistryEntries)
	}
}

func TestMACRegistryListReturnsIndependentSnapshot(t *testing.T) {
	r, _ := OpenMACRegistry("")
	r.Seen("aa:bb:cc:dd:ee:ff", time.Now())

	list := r.List()
	list[0].MAC = "tampered"

	fresh := r.List()
	if fresh[0].MAC == "tampered" {
		t.Error("mutating a List() result affected subsequent List() output")
	}
}
