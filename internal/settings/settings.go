// SPDX-License-Identifier: AGPL-3.0-only

// Package settings holds the small set of configuration an admin may
// change from inside the running app, as opposed to in config.yaml.
//
// It exists because those two are not the same kind of setting even when
// they name the same field. config.yaml is how a deployment is built;
// this is how it is tuned by whoever is looking at it, and a change made
// here has to survive the restart that a change made in a browser would
// otherwise not survive. Where both name a value, the stored one wins --
// it is the more recent, more deliberate statement, made with the actual
// consequence on screen -- and mikroview says so at startup rather than
// leaving an operator to wonder why the file they edited had no effect
// (see main.go's "event buffer" line).
//
// Deliberately narrow. This is not a second configuration system: only
// settings whose whole point is being adjusted against live evidence
// belong here, and today that is store.maxMemory alone (#796). Anything
// that changes what mikroview *is* -- listen addresses, TLS, storage
// backends, credentials -- stays in the file, where it is reviewable,
// version-controllable and outside the blast radius of a session.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("settings")

// storeFile is the on-disk shape.
//
// MaxMemoryBytes is a plain byte count rather than config's "120MiB"
// string form, because this document is written by the app and read by
// the app: a unit suffix here would only add a parse that can fail on a
// value nothing but mikroview ever wrote. Zero, or the field absent,
// means "nothing stored" -- the config file's figure then applies, which
// is the state every instance starts in.
type storeFile struct {
	Store storeSection `json:"store"`
}

type storeSection struct {
	MaxMemoryBytes int64 `json:"maxMemoryBytes"`
}

// Store holds the admin-adjustable settings. The zero value is not
// usable; construct with Open or OpenWithBackend.
type Store struct {
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version        int64
	maxMemoryBytes int64
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists to it from then
// on. An empty path is the expected "persistence not configured" case: a
// fully usable, in-memory-only Store is returned, in which a change
// applies to the running instance and is simply not there after a
// restart. A document that exists but cannot be read or parsed is a hard
// error, same contract as every sibling store -- see persist.Open and
// #378 for why a store is never built around a backend whose load
// failed.
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

	version, existed, err := persist.Open(context.Background(), b, "the settings store", func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		// A negative figure is not a smaller buffer, it is a corrupt
		// document, and treating it as "nothing stored" quietly hands
		// the operator the config file's value instead of refusing.
		// Refuse: this store is small enough that a bad document is
		// always worth a look, and startup is where someone is looking.
		if file.Store.MaxMemoryBytes < 0 {
			return fmt.Errorf("store.maxMemoryBytes is %d, which is not a size", file.Store.MaxMemoryBytes)
		}
		s.maxMemoryBytes = file.Store.MaxMemoryBytes
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

// MaxMemory returns the stored event-buffer budget in bytes, and whether
// one is stored at all. False means the config file's figure applies.
func (s *Store) MaxMemory() (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxMemoryBytes, s.maxMemoryBytes > 0
}

// SetMaxMemory records a new event-buffer budget, in bytes.
//
// Bounds are the caller's business, not this store's: internal/config
// owns what an acceptable store.maxMemory is (see MemoryBounds), and
// duplicating that judgement here is how two answers to the same
// question come to disagree. This refuses only what it cannot store at
// all.
//
// The error is the persistence failure, if any. The in-memory value is
// updated either way, so a disk problem degrades to "this will not
// survive a restart" rather than silently refusing a change the operator
// can see took effect -- the same contract every sibling store follows.
// The caller is expected to say so; unlike those siblings this one
// returns the error rather than only logging it, because the operator is
// standing in front of the control that caused it.
func (s *Store) SetMaxMemory(bytes int64) error {
	if bytes <= 0 {
		return fmt.Errorf("%d is not a usable event-buffer budget", bytes)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxMemoryBytes = bytes
	return s.persistLocked()
}

// persistLocked writes the document if persistence is configured.
func (s *Store) persistLocked() error {
	if s.backend == nil {
		return nil
	}
	data, err := json.MarshalIndent(storeFile{Store: storeSection{MaxMemoryBytes: s.maxMemoryBytes}}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the settings document: %w", err)
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing the settings document to %s: %w", s.backend.Describe(), err)
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("the settings document was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
	return nil
}
