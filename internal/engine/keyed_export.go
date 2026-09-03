// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// This file is the warm-restart (#795) export/import half of Keyed[V]:
// the bounded per-source/per-target map every windowed definition holds
// its rings and day bookkeeping in. See window_export.go's own doc
// comment for the decision and its date.
//
// Keyed cannot serialize V itself -- V is anything a definition wants,
// and this package's own convention is that Keyed delegates rather than
// guesses (see Keyed.Snapshot's clone callback, which exists for exactly
// the same reason). So Export/Import take the per-value codec as a
// callback and own only what Keyed owns: the key, the entry's
// last-activity time, and the cap.

// keyedEntryState is one tracked key in a snapshot: its key, the
// last-activity stamp Keyed's own eviction orders on, and whatever the
// caller's encoder made of the value.
type keyedEntryState struct {
	Key          string          `json:"key"`
	LastActivity time.Time       `json:"lastActivity"`
	Value        json.RawMessage `json:"value"`
}

// keyedState is a Keyed[V]'s whole snapshot, entries ordered by key so
// the document is stable across writes rather than reordering with Go's
// map walk.
type keyedState struct {
	Entries []keyedEntryState `json:"entries"`
}

// Export renders every tracked entry as JSON, using encode for the
// value. Takes k.mu for the whole walk, exactly as ForEach and Snapshot
// do -- which is what makes it safe to call from the snapshot writer's
// goroutine rather than the evaluation goroutine (GetOrCreate blocks
// for the duration, the same trade ForEach already documents).
//
// encode runs under that lock, so it must not call back into this
// Keyed[V]. Every caller in this package encodes a ring or a small
// struct, which is a pure read of the value itself.
//
// An encode failure fails the whole export rather than silently
// dropping that key: a codec that cannot render one entry will not
// render the others honestly either, and Engine.ExportState already
// isolates one definition's failure from the rest of the snapshot.
func (k *Keyed[V]) Export(encode func(v V) (json.RawMessage, error)) (json.RawMessage, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	state := keyedState{Entries: make([]keyedEntryState, 0, len(k.entries))}
	for key, e := range k.entries {
		raw, err := encode(e.value)
		if err != nil {
			return nil, fmt.Errorf("engine: Keyed.Export: key %q: %w", key, err)
		}
		state.Entries = append(state.Entries, keyedEntryState{
			Key:          key,
			LastActivity: e.lastActivity,
			Value:        raw,
		})
	}
	sort.Slice(state.Entries, func(i, j int) bool {
		return state.Entries[i].Key < state.Entries[j].Key
	})
	return json.Marshal(state)
}

// Import restores raw into this Keyed[V], decoding each value with
// decode and preserving each entry's last-activity stamp so the
// eviction order a restarted process starts with is the one it had.
//
// Honours maxTrackedKeys: at most that many entries are restored,
// newest activity first, so a snapshot written by a build with a larger
// cap (or a hand-edited one) cannot push this process past its own
// bound. Whatever is dropped is the oldest activity, the same thing
// GetOrCreate's own eviction would have shed first.
//
// Decoding happens before the lock is taken and before anything is
// published, so a malformed document leaves the map untouched: this
// either restores every entry it accepted or none. A decode failure is
// an error rather than a skipped key, for the same reason Export's is.
//
// Entries already present under a restored key are replaced. In the
// intended call order (import before the ingest loop starts -- see
// Engine.ImportState) there are none.
func (k *Keyed[V]) Import(raw json.RawMessage, decode func(raw json.RawMessage) (V, error)) error {
	var state keyedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("engine: Keyed.Import: %w", err)
	}

	entries := state.Entries
	// Newest activity first, ties broken on key so a snapshot whose
	// stamps collide still restores the same subset every time.
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].LastActivity.Equal(entries[j].LastActivity) {
			return entries[i].LastActivity.After(entries[j].LastActivity)
		}
		return entries[i].Key < entries[j].Key
	})
	if len(entries) > maxTrackedKeys {
		entries = entries[:maxTrackedKeys]
	}

	restored := make(map[string]*entry[V], len(entries))
	for _, es := range entries {
		if es.Key == "" {
			continue
		}
		v, err := decode(es.Value)
		if err != nil {
			return fmt.Errorf("engine: Keyed.Import: key %q: %w", es.Key, err)
		}
		restored[es.Key] = &entry[V]{value: v, lastActivity: es.LastActivity}
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	for key, e := range restored {
		k.entries[key] = e
	}
	return nil
}
