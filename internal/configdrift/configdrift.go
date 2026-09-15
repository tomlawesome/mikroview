// SPDX-License-Identifier: AGPL-3.0-only

// Package configdrift records one thing: which config-version "N new
// settings are available" notice (#1218) an operator has already dealt
// with, so it stops resurfacing for that version once dismissed but
// comes back for the next one that actually adds something new.
//
// The notice itself -- which settings, and the YAML to paste -- is
// computed live from the running config and the binary's own
// deploy/config.example.yaml on every read (see internal/config's
// MissingSettings); there is nothing about it worth persisting, since it
// can only change across a restart anyway. Only the operator's own
// decision to dismiss it needs to survive one, which is exactly what
// this store is for.
//
// Modeled on internal/setup.Store -- same shape (an optional
// persist.Backend, an in-memory value that is the source of truth
// either way), same locking (one mutex, write under it, persist while
// still holding it), same comment voice -- kept as its own small package
// rather than folded into that one, since a version dismissal is not a
// wizard-step decision and has nothing to do with the wizard's
// observations.
package configdrift

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("configdrift")

// Store is safe for concurrent use.
type Store struct {
	mu sync.RWMutex
	// dismissedVersion is the last version an operator dismissed the
	// notice for. A single value, not a set: only the most recent
	// dismissal has ever mattered, since the notice for any earlier
	// version cannot be showing once a later one has run.
	dismissedVersion string
	// backend is where the dismissal is persisted, or nil when
	// persistence is switched off -- dismissing still works, it just
	// does not survive a restart, the same optional-persistence contract
	// every sibling store here follows.
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
}

// storeFile is the on-disk shape.
type storeFile struct {
	DismissedVersion string `json:"dismissedVersion,omitempty"`
}

// New returns an in-memory-only Store.
func New() *Store {
	return &Store{}
}

// Open loads path if it exists (a missing file is the expected first-run
// case) and returns a Store that persists dismissals there from then on.
// An empty path is the expected "persistence not configured" case. A
// document that exists but cannot be read or parsed is a hard error, the
// same contract every sibling store here follows -- see persist.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b}
	if b == nil {
		return s, nil
	}
	version, existed, err := persist.Open(context.Background(), b, "the config-drift dismissal", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		s.dismissedVersion = file.DismissedVersion
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

// Dismissed reports whether version's "new settings are available"
// notice has already been dismissed.
func (s *Store) Dismissed(version string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return version != "" && s.dismissedVersion == version
}

// Dismiss records that version's notice has been dealt with. Persists
// immediately, with no debounce -- a dismissal is a rare, operator-
// driven, interactive action, not a hot path, the same reasoning
// internal/setup.Store.NoteMark gives.
func (s *Store) Dismiss(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dismissedVersion = version
	s.persistLocked()
}

// persistLocked writes the dismissal to the backend if persistence is
// configured. Write failures are logged loudly and swallowed rather
// than surfaced to Dismiss's caller, matching every sibling store: the
// in-memory state (which Dismissed always reads) stays correct either
// way, so a transient disk problem degrades to "this will not survive a
// restart right now" rather than failing the click that made it.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	data, err := json.Marshal(storeFile{DismissedVersion: s.dismissedVersion})
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the config-drift dismissal for persistence failed: %v -- this will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing the config-drift dismissal to %s failed: %v -- this will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("the config-drift dismissal was modified by another process while this one was pending (%s); this dismissal was applied on top", s.backend.Describe()))
	}
	s.version = version
}
