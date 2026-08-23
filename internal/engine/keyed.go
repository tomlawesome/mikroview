// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
)

// maxTrackedKeys bounds every per-key state map the chassis owns --
// generalizing internal/detect.maxTrackedSources (4096) to the one place
// this reasoning is now stated, per docs/decisions/evaluation-engine.md
// section 1's "per-source and per-target windowed state, with eviction"
// contract. Without it, a definition keying its state on something an
// attacker chooses (a source IP, in particular) could grow that state
// without bound. A var, not a const, so tests can shrink it without
// needing thousands of distinct keys -- same convention as
// internal/detect.maxTrackedSources and this package's own queueSize.
//
// One cap, applied independently by every Keyed[V] a definition
// constructs: a definition that tracks rings, evidence and a baseline
// all keyed on the same source IP is expected to use the same key across
// all three, so they fill and evict together in practice, even though
// nothing here enforces that coupling -- see Keyed's own doc comment.
var maxTrackedKeys = 4096

// entry wraps a tracked value with the last-activity timestamp
// GetOrCreate needs for eviction -- kept separate from V so a
// definition's own value type never has to expose one.
type entry[V any] struct {
	value        V
	lastActivity time.Time
}

// Keyed is the chassis's one per-source/per-target state primitive:
// a bounded map, keyed by whatever a definition calls its key (a source
// IP, a rule label, a serialized (source,target) pair -- the engine does
// not care), evicting the least-recently-active entries once it reaches
// maxTrackedKeys. Generalizes internal/detect.evictOldestByActivity
// (itself already built on internal/evict -- see that package's doc
// comment for why batch-shedding, not evicting back to exactly the cap,
// is the point: evicting to exactly the cap makes the very next new key
// overflow again, measured at an 87x slowdown before internal/evict
// fixed it there).
//
// Keyed's own mutex protects the map structure (insert/evict/lookup)
// only -- it does not protect V's own internals. A V that is itself read
// or written from more than one goroutine (see EvidenceSet, Baseline)
// owns its own concurrency safety; Keyed just guarantees the entry a
// caller gets back is the one for that key at that moment.
type Keyed[V any] struct {
	mu      sync.Mutex
	entries map[string]*entry[V]
}

// NewKeyed constructs an empty Keyed[V].
func NewKeyed[V any]() *Keyed[V] {
	return &Keyed[V]{entries: make(map[string]*entry[V])}
}

// GetOrCreate returns the value stored for key, constructing one via
// newValue if this is the first time key has been seen (or the first
// time since it was evicted), and marking key as active at now either
// way. Evicts a batch of the least-recently-active keys first if the map
// is already at maxTrackedKeys and key is new.
//
// Meant to be called from the engine's single evaluation goroutine, the
// same single-writer assumption detect.Detector.Observe and
// watchlist.Evaluator's per-event pass make -- concurrent calls to
// GetOrCreate are not supported (one Keyed[V] per evaluation goroutine,
// the same way there is one Detector/Evaluator per process). ForEach and
// Snapshot, below, are safe to call from any goroutine, including
// concurrently with GetOrCreate -- see their own doc comments for the
// two different contracts they offer.
func (k *Keyed[V]) GetOrCreate(key string, now time.Time, newValue func() V) V {
	k.mu.Lock()
	defer k.mu.Unlock()

	e, ok := k.entries[key]
	if !ok {
		if n := len(k.entries); n >= maxTrackedKeys {
			// evict.Target uses the map's own live size, not the
			// constant, matching internal/detect.evictOldestByActivity's
			// own call -- so a maxTrackedKeys lowered after entries were
			// already inserted (tests shrink it, same convention as
			// internal/detect.maxTrackedSources) still sheds a sane
			// batch rather than computing a negative/zero target.
			evict.DownTo(k.entries, evict.Target(n), func(e *entry[V]) time.Time {
				return e.lastActivity
			})
		}
		e = &entry[V]{value: newValue()}
		k.entries[key] = e
	}
	e.lastActivity = now
	return e.value
}

// Get returns the value stored for key without creating one, and
// whether key was found. Deliberately does not update lastActivity: a
// read must not itself count as the activity that keeps an otherwise-idle
// key alive, or an unrelated read path (an admin API, a replay pass)
// would silently change eviction order for state it isn't even
// evaluating.
func (k *Keyed[V]) Get(key string) (V, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	return e.value, true
}

// Len reports how many keys are currently tracked.
func (k *Keyed[V]) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.entries)
}

// ForEach iterates every current entry without copying the map or
// cloning its values -- the internal-evaluation path a definition's own
// Evaluate is expected to use (single evaluation goroutine only, see
// Engine.Evaluated's doc comment), the same shape
// watchlist.Store.entriesSnapshot exists for and documents: that
// function's own doc comment records a full copy-and-sort measured at up
// to 4.3ms/event at 5,000 entries (internal/watchlist/watchlist.go), a
// cost #376 already found once and this package is not going to
// reintroduce for a path that runs once per event. fn returning false
// stops iteration early.
//
// Not safe to call from any goroutine other than the one driving
// evaluation, and not safe to call while that goroutine is itself
// calling GetOrCreate on the same Keyed[V] (both take k.mu, so they
// won't race, but ForEach holds the lock for its entire iteration,
// which would block GetOrCreate -- exactly the reason
// watchlist.Evaluator's own per-event loop takes entriesSnapshot rather
// than iterating live under a lock it also needs for RecordObservation).
// See Snapshot for the copy-on-read read API any other caller must use
// instead.
func (k *Keyed[V]) ForEach(fn func(key string, v V) bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for key, e := range k.entries {
		if !fn(key, e.value) {
			return
		}
	}
}

// Snapshot is the copy-on-read boundary this package states once (see
// docs/decisions/evaluation-engine.md section 1's copy-on-read
// contract): a deep copy of every currently-tracked entry, safe to read
// from any goroutine -- including one reading while the evaluation
// goroutine concurrently calls GetOrCreate on the same Keyed[V] -- with
// no backing storage shared between the copy and Keyed's own map. See
// TestKeyedSnapshotRaceSafeAgainstConcurrentWrites for the -race proof.
//
// clone must return a value sharing no mutable state with v -- Keyed has
// no way to know how to deep-copy an arbitrary V itself, so it delegates
// that one responsibility to the caller (typically a thread-safe read
// method the V type already exposes, e.g. EvidenceSet.Ports/Hosts/
// Labels or Baseline.Snapshot, composed into a fresh V) rather than
// guessing.
func (k *Keyed[V]) Snapshot(clone func(v V) V) map[string]V {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make(map[string]V, len(k.entries))
	for key, e := range k.entries {
		out[key] = clone(e.value)
	}
	return out
}
