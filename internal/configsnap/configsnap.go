// SPDX-License-Identifier: AGPL-3.0-only

// Package configsnap keeps the config editor's snapshots (#1347): copies
// of a config text an admin can come back to, download, or load into the
// editor again -- the last Keep of them, one taken automatically before
// every Carry forward so the file as it was is never lost.
//
// A snapshot holds a whole config.yaml, secrets included, so the store
// only ever persists sealed: main.go gives it an encrypted file backend
// under the retention key, the same key and cipher router backups are
// sealed with, and gives it none at all when there is no key. There is
// no plain-text mode, the same "no key, no storage" rule as #853's.
package configsnap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// Keep is how many snapshots are kept (owner, #1347 answer 5).
const Keep = 5

// Why a snapshot was taken.
const (
	ReasonManual             = "manual"
	ReasonBeforeCarryForward = "before-carry-forward"
)

// MaxTextBytes caps one snapshot's text. The example config, every
// option in it, is about 30KB; a megabyte is room for any real file and
// still keeps five of them small.
const MaxTextBytes = 1 << 20

// MaxNoteBytes caps the admin's note.
const MaxNoteBytes = 1024

var (
	// ErrNotFound is returned for an id the store does not hold.
	ErrNotFound = errors.New("configsnap: no such snapshot")
	// ErrTooLarge is returned for a text or note over its cap.
	ErrTooLarge = errors.New("configsnap: snapshot text or note is too large")
)

// Meta is everything about a snapshot but its text: what the list shows.
type Meta struct {
	ID      string    `json:"id"`
	TakenAt time.Time `json:"takenAt"`
	// By is the username of the admin who took it.
	By string `json:"by"`
	// Schema is the key set the text was written for: its header's
	// schema, or the guess from its keys when it has none.
	Schema int `json:"schema"`
	// Version is the release its header says wrote it ("v0.6.1"), empty
	// for a text with no header.
	Version string `json:"version"`
	Reason  string `json:"reason"`
	Note    string `json:"note"`
}

// Snapshot is one kept config text.
type Snapshot struct {
	Meta
	Text string `json:"text"`
}

type document struct {
	Snapshots []Snapshot `json:"snapshots"`
}

// Store holds the snapshots, oldest first.
type Store struct {
	mu        sync.Mutex
	backend   persist.Backend
	version   int64
	snapshots []Snapshot
	now       func() time.Time
}

// Open loads the store from b. A nil backend gives a working store that
// keeps nothing across a restart -- for tests; main.go never builds one
// without a sealed backend.
func Open(ctx context.Context, b persist.Backend) (*Store, error) {
	s := &Store{backend: b, now: time.Now}
	version, existed, err := persist.Open(ctx, b, "the config snapshots store", func(data []byte) error {
		var doc document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		for _, snap := range doc.Snapshots {
			if snap.ID == "" {
				continue
			}
			s.snapshots = append(s.snapshots, snap)
		}
		sort.SliceStable(s.snapshots, func(i, j int) bool { return s.snapshots[i].TakenAt.Before(s.snapshots[j].TakenAt) })
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

// List returns every snapshot's metadata, newest first.
func (s *Store) List() []Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Meta, 0, len(s.snapshots))
	for i := len(s.snapshots) - 1; i >= 0; i-- {
		out = append(out, s.snapshots[i].Meta)
	}
	return out
}

// Count is how many snapshots are held.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
}

// Get returns one snapshot, text included.
func (s *Store) Get(id string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snap := range s.snapshots {
		if snap.ID == id {
			return snap, nil
		}
	}
	return Snapshot{}, ErrNotFound
}

// Newest returns the most recent snapshot, false when there is none.
func (s *Store) Newest() (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snapshots) == 0 {
		return Snapshot{}, false
	}
	return s.snapshots[len(s.snapshots)-1], true
}

// Add keeps a new snapshot and evicts down to Keep. TakenAt and ID are
// filled in here. The snapshot is only in the store once it has been
// written: a failed write leaves the store as it was and returns the
// error.
func (s *Store) Add(snap Snapshot) (Meta, error) {
	if len(snap.Text) > MaxTextBytes || len(snap.Note) > MaxNoteBytes {
		return Meta{}, ErrTooLarge
	}
	if snap.Reason != ReasonBeforeCarryForward {
		snap.Reason = ReasonManual
	}
	snap.ID = newID()
	snap.TakenAt = s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	next := evict(append(append([]Snapshot(nil), s.snapshots...), snap))
	if err := s.persistLocked(next); err != nil {
		return Meta{}, err
	}
	s.snapshots = next
	return snap.Meta, nil
}

// Delete removes one snapshot.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make([]Snapshot, 0, len(s.snapshots))
	found := false
	for _, snap := range s.snapshots {
		if snap.ID == id {
			found = true
			continue
		}
		next = append(next, snap)
	}
	if !found {
		return ErrNotFound
	}
	if err := s.persistLocked(next); err != nil {
		return err
	}
	s.snapshots = next
	return nil
}

// evict trims snaps (oldest first) to Keep, oldest out first, except
// that the newest before-carry-forward snapshot is never the one to go:
// it is the copy of the file as it was before the last rewrite, the one
// a rollback needs, and five manual snapshots taken afterwards must not
// push it out.
func evict(snaps []Snapshot) []Snapshot {
	for len(snaps) > Keep {
		protected := -1
		for i := len(snaps) - 1; i >= 0; i-- {
			if snaps[i].Reason == ReasonBeforeCarryForward {
				protected = i
				break
			}
		}
		victim := 0
		if victim == protected {
			victim = 1
		}
		snaps = append(snaps[:victim], snaps[victim+1:]...)
	}
	return snaps
}

func (s *Store) persistLocked(snaps []Snapshot) error {
	if s.backend == nil {
		return nil
	}
	data, err := json.Marshal(document{Snapshots: snaps})
	if err != nil {
		return fmt.Errorf("encoding the config snapshots: %w", err)
	}
	version, _, err := persist.SaveWithRetry(context.Background(), s.backend, data, s.version)
	if err != nil {
		return fmt.Errorf("writing the config snapshots to %s: %w", s.backend.Describe(), err)
	}
	s.version = version
	return nil
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("configsnap: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
