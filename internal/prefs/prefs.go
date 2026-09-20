// SPDX-License-Identifier: AGPL-3.0-only

// Package prefs is issue #1283: one preferences record per user, held on
// the server instead of in the browser's localStorage, so a user's
// presets, widgets and layout follow them to any browser and a shared
// machine shows the next operator nothing of the last one.
//
// Modelled on internal/droplist's Store: mutex-protected, whole-document
// JSON persistence via internal/persist, synchronous and error-returning
// on write (tryPersistLocked) rather than write-behind -- there is no hot
// path here to protect from a disk wait the way internal/decommission's
// RecordTraffic is, and PUT /api/me/preferences returning 204 should mean
// the record is actually durable, not merely queued.
//
// This package never looks inside a record. What a "preference" is
// belongs entirely to the frontend modules that own each key (presets,
// top-talker widgets, colourway, and so on) -- see the API contract on
// #1283. Storing it as opaque json.RawMessage, keyed by user id, is what
// keeps this package from ever needing to change when the frontend adds
// or renames a key.
package prefs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var prefsLog = logging.New("prefs")

// document is the on-disk shape: every user's record in one file, keyed
// by user id -- the same whole-document-keyed-by-id shape
// internal/decommission's watches document uses, and for the same
// reason: a record this package doesn't understand (a future field, or
// one left behind by a user id that no longer resolves) is preserved
// across a rewrite rather than silently dropped.
type document struct {
	Records map[string]json.RawMessage `json:"records"`
}

// Store holds every user's preferences record. The zero value is not
// usable; construct with Open or OpenWithBackend.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	version int64
	records map[string]json.RawMessage
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists to it from then
// on. An empty path keeps everything in-memory only, the same optional-
// persistence contract every other small store in this codebase follows.
// A document that exists but cannot be read or parsed is a hard error
// (#378): the caller gets (nil, err) rather than a store whose live
// backend would overwrite that document on the first write.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, records: make(map[string]json.RawMessage)}

	version, existed, err := persist.Open(context.Background(), b, "the preferences store", func(data []byte) error {
		var doc document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		for id, raw := range doc.Records {
			// A JSON object containing `null` for a value, or an empty
			// key, is syntactically valid and would otherwise leave an
			// unreachable or malformed entry sitting in the map --
			// dropped here the same way internal/decommission's Open
			// skips a nil watch and an empty id.
			if id == "" || raw == nil {
				continue
			}
			// Re-compacted rather than kept as loaded: a hand-edited
			// file, or one that passed through the backup envelope's own
			// pretty-printing (internal/backup.Write), can arrive
			// indented, and this store otherwise never looks at a
			// record's bytes again before handing them straight back on
			// the next GET. Compacting once here is what keeps that
			// answer byte-stable regardless of how this document got
			// here.
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				return fmt.Errorf("preferences record for %q is not valid JSON: %w", id, err)
			}
			s.records[id] = append(json.RawMessage(nil), compact.Bytes()...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if existed {
		s.version = version
	}
	return s, nil
}

// Get returns userID's stored preferences and true, or (nil, false) if
// this user has no record -- the caller (the API handler) turns that
// into the documented empty-object default, not this package.
func (s *Store) Get(userID string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, ok := s.records[userID]
	if !ok {
		return nil, false
	}
	// Copied out so the caller can't mutate this store's backing slice
	// through what looks like a read.
	return append(json.RawMessage(nil), raw...), true
}

// Put replaces userID's whole record with prefs, which must already be
// a validated JSON object -- this package stores it opaquely and does
// not re-check its shape.
//
// The error is the persistence failure, if any. On failure the previous
// value (or its absence) is restored before returning, so a caller told
// "this failed" never has a stale in-memory copy suggesting it half
// worked -- the same restore-on-error contract internal/droplist's Add
// and Remove follow.
func (s *Store) Put(userID string, prefsDoc json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, existed := s.records[userID]
	s.records[userID] = append(json.RawMessage(nil), prefsDoc...)
	if err := s.tryPersistLocked(); err != nil {
		if existed {
			s.records[userID] = prev
		} else {
			delete(s.records, userID)
		}
		return fmt.Errorf("saving preferences: %w", err)
	}
	return nil
}

// Delete removes userID's record entirely, if one exists. Called when
// the account itself is deleted (handleAuthDeleteUser, #1283's own
// "cleared when the user is deleted" ruling) -- deleting an id with no
// record is not an error, since the end state either way is "nothing
// stored for this id".
func (s *Store) Delete(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, existed := s.records[userID]
	if !existed {
		return nil
	}
	delete(s.records, userID)
	if err := s.tryPersistLocked(); err != nil {
		s.records[userID] = prev
		return fmt.Errorf("saving preferences: %w", err)
	}
	return nil
}

// tryPersistLocked re-encodes the whole document and writes it through
// persist.SaveWithRetry. Whole-document rather than per-user, matching
// every sibling store: one canonical encoding is what makes "did this
// change" answerable, and this store is small (one small JSON blob per
// account, not per event).
func (s *Store) tryPersistLocked() error {
	if s.backend == nil {
		return nil
	}
	data, err := json.Marshal(document{Records: s.records})
	if err != nil {
		return fmt.Errorf("encoding the preferences document: %w", err)
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing to %s: %w", s.backend.Describe(), err)
	}
	if conflicted {
		prefsLog.Warn(fmt.Sprintf("the preferences document was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
	return nil
}
