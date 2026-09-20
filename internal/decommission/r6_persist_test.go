// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// withFastSaveTimeout shrinks persist.SaveTimeout for the duration of a
// test -- tryPersistLocked's Flush escape hatch waits out this bound
// before giving up on a failing backend, and the real 5s default would
// make every test below slow for no reason.
func withFastSaveTimeout(t *testing.T) {
	t.Helper()
	orig := persist.SaveTimeout
	persist.SaveTimeout = 30 * time.Millisecond
	t.Cleanup(func() { persist.SaveTimeout = orig })
}

// switchableFailBackend succeeds every Save until fail() is called, so a
// test can complete whatever ordinary setup it needs (which may itself
// trigger a background write-behind save on its own timing) before
// deterministically putting the backend into the failing state the
// assertion under test needs -- see internal/flags' identical fixture
// for the fuller reasoning on why a fixed save-count budget is racy
// against a write-behind-backed store.
type switchableFailBackend struct {
	mu      sync.Mutex
	version int64
	failing bool
}

func (b *switchableFailBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *switchableFailBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing {
		return 0, errors.New("backend unavailable")
	}
	b.version++
	return b.version, nil
}

func (b *switchableFailBackend) Close() error     { return nil }
func (b *switchableFailBackend) Describe() string { return "switchable test backend" }

func (b *switchableFailBackend) fail() {
	b.mu.Lock()
	b.failing = true
	b.mu.Unlock()
}

// This file is the v0.6.0 audit's R6 fix for this store: Add,
// ForceRemove, Restore and Delete all change a watch an operator
// explicitly created, force-removed, restored or deleted, so a failed
// save must not be reported as kept -- see each method's own
// restore-on-error comment. Every test here proves both halves: a
// non-nil error, and the store reading back unchanged afterward. Before
// this fix, each of these methods called the swallow-and-log
// persistLocked and reported success regardless of whether the write
// actually landed.

func TestAddFailedSaveLeavesNoWatch(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}

	b.fail()
	if _, err := s.Add(Watch{CIDR: "192.0.2.0/24", CreatedAt: created, CleanWindow: 6 * time.Hour, Covered: true}); err == nil {
		t.Fatal("expected Add to report the save failure")
	} else if !errors.Is(err, ErrSaveFailed) {
		t.Errorf("expected the error to wrap ErrSaveFailed, got %v", err)
	}

	if len(s.List()) != 0 {
		t.Errorf("List() = %+v after a failed Add, want no watch left behind", s.List())
	}
}

func TestForceRemoveFailedSaveLeavesWatchUntouched(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	w := addWatch(t, s, "192.0.2.0/24")
	before, ok := s.Get(w.ID)
	if !ok {
		t.Fatal("setup: expected the watch to exist")
	}

	b.fail()
	if _, err := s.ForceRemove(w.ID, "alice", "decommissioning early", created.Add(time.Hour)); err == nil {
		t.Fatal("expected ForceRemove to report the save failure")
	} else if !errors.Is(err, ErrSaveFailed) {
		t.Errorf("expected the error to wrap ErrSaveFailed, got %v", err)
	}

	after, ok := s.Get(w.ID)
	if !ok {
		t.Fatal("watch disappeared after a failed save")
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("ForceRemove changed the watch despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

func TestRestoreFailedSaveLeavesWatchRetired(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	w := addWatch(t, s, "192.0.2.0/24")
	if retired := s.Sweep(created.Add(7 * time.Hour)); len(retired) != 1 {
		t.Fatalf("setup: sweep retired %d watches, want 1", len(retired))
	}
	before, ok := s.Get(w.ID)
	if !ok || before.RetiredAt.IsZero() {
		t.Fatal("setup: expected the watch to be retired")
	}

	b.fail()
	if _, err := s.Restore(w.ID, created.Add(6*time.Hour+time.Minute)); err == nil {
		t.Fatal("expected Restore to report the save failure")
	} else if !errors.Is(err, ErrSaveFailed) {
		t.Errorf("expected the error to wrap ErrSaveFailed, got %v", err)
	}

	after, ok := s.Get(w.ID)
	if !ok {
		t.Fatal("watch disappeared after a failed save")
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("Restore changed the watch despite the failed save:\nbefore = %+v\nafter  = %+v", before, after)
	}
}

func TestDeleteFailedSaveLeavesWatchInPlace(t *testing.T) {
	withFastSaveTimeout(t)
	b := &switchableFailBackend{}
	s, err := OpenWithBackend(b)
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	w := addWatch(t, s, "192.0.2.0/24")

	b.fail()
	if err := s.Delete(w.ID); err == nil {
		t.Fatal("expected Delete to report the save failure")
	} else if !errors.Is(err, ErrSaveFailed) {
		t.Errorf("expected the error to wrap ErrSaveFailed, got %v", err)
	}

	if _, ok := s.Get(w.ID); !ok {
		t.Error("watch is gone after a failed save -- Delete must have put it back")
	}
}
