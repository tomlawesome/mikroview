// SPDX-License-Identifier: AGPL-3.0-only

package prefs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// failingSaveBackend lets Open succeed (nothing stored yet) but fails
// every Save -- for the R6-style case proving Merge/Delete return the
// persistence error rather than reporting a change as durably stored
// when it wasn't. Mirrors internal/settings' own test fixture of the
// same name.
type failingSaveBackend struct{}

func (failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("backend unavailable")
}
func (failingSaveBackend) Close() error     { return nil }
func (failingSaveBackend) Describe() string { return "failing test backend" }

func TestFirstRunHasNoRecord(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatalf("a missing document is the first-run case, not an error: %v", err)
	}
	if _, ok := s.Get("user-1"); ok {
		t.Error("a fresh store reported a stored record for a user who never had one")
	}
}

func TestMergeThenGetRoundTrips(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"colorway":"teal","altitudeStop":3}`)); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got, ok := s.Get("user-1")
	if !ok {
		t.Fatal("Get reported no record right after Merge")
	}
	// Compared as decoded values, not raw bytes -- Merge re-encodes the
	// record from a map, and Go's map iteration order (hence
	// json.Marshal's key order) is not the order the patch was written
	// in.
	var gotFields map[string]any
	if err := json.Unmarshal(got, &gotFields); err != nil {
		t.Fatal(err)
	}
	if gotFields["colorway"] != "teal" || gotFields["altitudeStop"] != float64(3) {
		t.Errorf("Get = %s, want colorway=teal, altitudeStop=3", got)
	}
}

func TestResetDropsEveryRecordAndPersistsThat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"user-1", "user-2"} {
		if err := s.Merge(id, json.RawMessage(`{"colorway":"teal"}`)); err != nil {
			t.Fatalf("Merge %s: %v", id, err)
		}
	}
	if err := s.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	for _, id := range []string{"user-1", "user-2"} {
		if _, ok := s.Get(id); ok {
			t.Errorf("%s still has a record after Reset", id)
		}
	}
	// Durable, not just in memory: a reopened store must not resurrect
	// what the reset was told had gone.
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get("user-1"); ok {
		t.Error("user-1's record came back after reopening the store")
	}
}

// TestMergeKeepsKeysNotInThePatch is the "two tabs" case Merge exists
// for: a second save naming a different key must not discard the first
// save's key, the way a whole-record replace would.
func TestMergeKeepsKeysNotInThePatch(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"a":1,"b":2}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"c":3}`)); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("user-1")
	if !ok {
		t.Fatal("Get reported no record after two Merges")
	}
	// Decoded, not compared as raw bytes -- see TestMergeThenGetRoundTrips
	// on why key order in the re-encoded record isn't stable.
	var gotFields map[string]any
	if err := json.Unmarshal(got, &gotFields); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": float64(1), "b": float64(2), "c": float64(3)}
	for k, v := range want {
		if gotFields[k] != v {
			t.Errorf("Get after a second Merge = %s, missing or wrong %q -- Merge keeps keys the second save never mentioned", got, k)
		}
	}
}

// TestMergeOverwritesAKeyItRepeats proves Merge is a merge, not a
// no-clobber union: naming the same key again still updates it.
func TestMergeOverwritesAKeyItRepeats(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"colorway":"teal"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"colorway":"mono"}`)); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("user-1")
	if !ok || string(got) != `{"colorway":"mono"}` {
		t.Errorf("Get after re-merging the same key = (%s, %v), want ({\"colorway\":\"mono\"}, true)", got, ok)
	}
}

func TestOneUsersRecordDoesNotLeakToAnother(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-a", json.RawMessage(`{"who":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-b", json.RawMessage(`{"who":"b"}`)); err != nil {
		t.Fatal(err)
	}
	gotA, _ := s.Get("user-a")
	gotB, _ := s.Get("user-b")
	if string(gotA) != `{"who":"a"}` {
		t.Errorf("user-a's record = %s, want {\"who\":\"a\"}", gotA)
	}
	if string(gotB) != `{"who":"b"}` {
		t.Errorf("user-b's record = %s, want {\"who\":\"b\"}", gotB)
	}
}

func TestDeleteRemovesTheRecord(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("user-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("user-1"); ok {
		t.Error("a record survived its own Delete")
	}
}

// Deleting a user with no record is not an error: the end state either
// way is "nothing stored for this id".
func TestDeleteWithNoRecordIsNotAnError(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("nobody"); err != nil {
		t.Errorf("Delete on a user with no record returned %v, want nil", err)
	}
}

func TestStoreSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"metricsView":"table"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-2", json.RawMessage(`{"groupRepeats":true}`)); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got1, ok1 := reopened.Get("user-1")
	if !ok1 || string(got1) != `{"metricsView":"table"}` {
		t.Errorf("user-1 after reopen = (%s, %v), want the stored record", got1, ok1)
	}
	got2, ok2 := reopened.Get("user-2")
	if !ok2 || string(got2) != `{"groupRepeats":true}` {
		t.Errorf("user-2 after reopen = (%s, %v), want the stored record", got2, ok2)
	}
}

func TestUnparseableDocumentRefusesToOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("an unparseable preferences document opened cleanly")
	}
}

func TestMergeReportsAPersistFailure(t *testing.T) {
	s, err := OpenWithBackend(failingSaveBackend{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"a":1}`)); err == nil {
		t.Fatal("Merge against a failing backend was reported as succeeding")
	}
	if _, ok := s.Get("user-1"); ok {
		t.Error("a refused Merge was stored anyway")
	}
}

func TestDeleteReportsAPersistFailure(t *testing.T) {
	// A record has to exist in memory before Delete has anything to
	// refuse to remove, so it is written through a real, working backend
	// first, then the backend is swapped for a failing one -- the store
	// has no exported way to seed a record with no backend attached at
	// all.
	path := filepath.Join(t.TempDir(), "preferences.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	s.backend = failingSaveBackend{}

	if err := s.Delete("user-1"); err == nil {
		t.Fatal("Delete against a failing backend was reported as succeeding")
	}
	if _, ok := s.Get("user-1"); !ok {
		t.Error("a refused Delete removed the record anyway")
	}
}

func TestNoBackendStillApplies(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Merge("user-1", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatalf("Merge with no backend: %v", err)
	}
	if got, ok := s.Get("user-1"); !ok || string(got) != `{"a":1}` {
		t.Errorf("Get = (%s, %v), want ({\"a\":1}, true)", got, ok)
	}
}
