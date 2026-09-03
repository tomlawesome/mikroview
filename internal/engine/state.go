// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var stateLog = logging.New("engine-state")

// BaselineState is one Baseline's persistable EMA state -- exactly what
// docs/decisions/evaluation-engine.md section 1 means by "the persisted
// stores (definitions, per-definition settings, engine state)": the
// value/variance a Baseline needs to resume warm after a restart, the
// sample count and first-seen time its BaselineFloor gates against. See
// Baseline.State (the export side) and RestoreBaseline (the import
// side).
type BaselineState struct {
	Value     float64   `json:"value"`
	Variance  float64   `json:"variance"`
	Samples   int       `json:"samples"`
	FirstSeen time.Time `json:"firstSeen"`
	Primed    bool      `json:"primed"`
}

// stateDocument is StateStore's on-disk shape: per-definition, per-key
// baseline state. A new BaselineState field decodes as its zero value
// against an older document, which is all the forward-growth this
// document has ever needed -- see #873 for why the speculative Version
// field this type once carried is gone: nothing read it, and Go's
// decoder already ignores an unrecognized "version" key in an older
// document, so removing the field breaks nothing (see
// TestOpenStateStoreLoadsADocumentCarryingAVersionKey).
type stateDocument struct {
	Definitions map[string]map[string]BaselineState `json:"definitions"`
}

// engineStateFlushInterval is the write-behind writer's MinInterval for
// StateStore -- deliberately coarse, per #400's requirement that
// baseline state must never be a write per event. An EMA baseline
// (emaAlpha = 0.02, see baseline.go) moves slowly by design: one
// genuinely spiky reading is meant to shift it only a couple of percent,
// so losing up to one flush interval of the *latest* value/variance/
// sample-count update to an unclean shutdown costs a warm-up a few
// minutes shorter than it could have been, not a wrong judgement --
// materially different from flags.persistMinInterval's 1s, which
// protects a hot per-event path rather than state that is, by
// construction, still meaningful minutes stale. 5 minutes is short
// enough that a restart during a long-running process loses very little
// warm-up, long enough that even a definition updating many keys stays
// nowhere near a per-event write. A var, not a const, so tests can
// shrink it, same convention as every other persist interval in this
// codebase.
var engineStateFlushInterval = 5 * time.Minute

// StateStore persists every definition's per-key Baseline state (#399's
// decision, recorded on that issue: implement the engine-state store).
// Opens fail-closed via persist.WriteBehind/persist.Open (#378) like
// every other store in this codebase -- an unreadable document refuses
// to start rather than silently beginning cold. The zero value is not
// usable; construct with OpenStateStore or OpenStateStoreWithBackend.
//
// Nothing registers a definition against this store yet (#401/#405's
// job -- see docs/decisions/evaluation-engine.md) so in production this
// document is empty; its own tests exercise it directly.
type StateStore struct {
	mu sync.RWMutex
	// wb is nil when persistence isn't configured -- see
	// persist.WriteBehind. Every method on it is a safe no-op on a nil
	// receiver.
	wb          *persist.WriteBehind
	definitions map[string]map[string]BaselineState
}

// OpenStateStore loads path if it exists (a missing file is the
// expected first-run case, not an error) and returns a StateStore that
// persists to it from then on. An empty path is the expected
// "persistence not configured" case: a fully usable, in-memory-only
// store is returned. A document that exists but cannot be read or
// parsed is a hard error (issue #378): the caller gets (nil, err)
// rather than a store whose live backend would overwrite that document
// on the first write. See persist.Open.
func OpenStateStore(path string) (*StateStore, error) {
	if path == "" {
		return OpenStateStoreWithBackend(nil)
	}
	return OpenStateStoreWithBackend(persist.NewFileBackend(path))
}

// OpenStateStoreWithBackend is OpenStateStore against any persist.Backend
// -- a JSON file by default, or Postgres when configured.
func OpenStateStoreWithBackend(b persist.Backend) (*StateStore, error) {
	s := &StateStore{definitions: make(map[string]map[string]BaselineState)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the engine-state store", persist.WriteBehindOptions{
		MinInterval: engineStateFlushInterval,
		OnSaveError: func(msg string) { stateLog.Error(msg) },
		OnConflict:  func(msg string) { stateLog.Warn(msg) },
	}, func(data []byte) error {
		var doc stateDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		for defID, keyed := range doc.Definitions {
			if defID == "" || keyed == nil {
				continue
			}
			s.definitions[defID] = keyed
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// Flush forces this store's write-behind writer to persist whatever is
// currently dirty now, without waiting out its usual debounce interval,
// and blocks until that attempt finishes or ctx expires -- see
// flags.Store.Flush's own doc comment for when this is the right call
// (a test, or a `-backup` CLI invocation racing a still-running
// process). A store with no backend configured (wb == nil) is a safe
// no-op.
func (s *StateStore) Flush(ctx context.Context) error {
	return s.wb.Flush(ctx)
}

// Close stops this store's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A store with no backend configured (wb == nil)
// is a safe no-op. Not safe to call Set after Close.
func (s *StateStore) Close(ctx context.Context) error {
	return s.wb.Close(ctx)
}

// Get returns the persisted BaselineState for one definition's key, and
// whether it was found -- what a definition consults on start to decide
// whether it has anything to resume warm from (see RestoreBaseline) or
// should construct a cold Baseline via NewBaseline instead.
func (s *StateStore) Get(definitionID, key string) (BaselineState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyed, ok := s.definitions[definitionID]
	if !ok {
		return BaselineState{}, false
	}
	st, ok := keyed[key]
	return st, ok
}

// Snapshot returns a copy of every key's persisted state for one
// definition -- the copy-on-read contract this package states once
// (docs/decisions/evaluation-engine.md section 1), same as
// Keyed.Snapshot/Baseline.Snapshot: safe to read from any goroutine,
// including one reading while Set is called concurrently for a
// different key, with no backing storage shared with the store's own
// map.
func (s *StateStore) Snapshot(definitionID string) map[string]BaselineState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyed := s.definitions[definitionID]
	out := make(map[string]BaselineState, len(keyed))
	for k, v := range keyed {
		out[k] = v
	}
	return out
}

// Set records key's current BaselineState for definitionID and persists
// it off the hot path on engineStateFlushInterval's coarse cadence --
// never a write per event. Intended call shape (once #401/#405 wire a
// definition onto Baseline): after folding in a reading, periodically
// call Set(definitionID, key, baseline.State()) rather than on every
// single Reading.
func (s *StateStore) Set(definitionID, key string, state BaselineState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keyed, ok := s.definitions[definitionID]
	if !ok {
		keyed = make(map[string]BaselineState)
		s.definitions[definitionID] = keyed
	}
	keyed[key] = state
	s.persistLocked()
}

// DeleteDefinition removes every persisted key for definitionID -- for
// when a definition itself is deleted (#401's job to call), so a
// removed definition's stale baseline state doesn't linger in the
// document forever.
func (s *StateStore) DeleteDefinition(definitionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.definitions[definitionID]; !ok {
		return
	}
	delete(s.definitions, definitionID)
	s.persistLocked()
}

// persistLocked encodes the current state and hands it to the
// write-behind writer (see persist.WriteBehind), which coalesces it
// with whatever else is pending and persists it off this goroutine, on
// engineStateFlushInterval's coarse cadence. Marshal failures are
// swallowed rather than surfaced to Set's caller: the in-memory state
// (which every read goes through) stays correct either way. Must be
// called with s.mu already held -- see flags.Store.persistLocked's own
// doc comment for the "lock covers the encode, not the backend call"
// contract this mirrors.
func (s *StateStore) persistLocked() {
	if s.wb == nil {
		return
	}
	doc := stateDocument{Definitions: s.definitions}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		stateLog.Error(fmt.Sprintf("encoding engine state for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	s.wb.MarkDirty(data)
}
