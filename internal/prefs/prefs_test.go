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
// every Save -- for the R6-style case proving Put/Delete return the
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

func TestPutThenGetRoundTrips(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := json.RawMessage(`{"colorway":"teal","altitudeStop":3}`)
	if err := s.Put("user-1", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := s.Get("user-1")
	if !ok {
		t.Fatal("Get reported no record right after Put")
	}
	if string(got) != string(want) {
		t.Errorf("Get = %s, want %s", got, want)
	}
}

func TestResetDropsEveryRecordAndPersistsThat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"user-1", "user-2"} {
		if err := s.Put(id, json.RawMessage(`{"colorway":"teal"}`)); err != nil {
			t.Fatalf("Put %s: %v", id, err)
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

func TestPutReplacesTheWholeRecord(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-1", json.RawMessage(`{"a":1,"b":2}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-1", json.RawMessage(`{"c":3}`)); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("user-1")
	if !ok || string(got) != `{"c":3}` {
		t.Errorf("Get after a second Put = (%s, %v), want (\"{\\\"c\\\":3}\", true) -- a PUT replaces, it doesn't merge", got, ok)
	}
}

func TestOneUsersRecordDoesNotLeakToAnother(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-a", json.RawMessage(`{"who":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-b", json.RawMessage(`{"who":"b"}`)); err != nil {
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
	if err := s.Put("user-1", json.RawMessage(`{"a":1}`)); err != nil {
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
	if err := s.Put("user-1", json.RawMessage(`{"metricsView":"table"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-2", json.RawMessage(`{"groupRepeats":true}`)); err != nil {
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

func TestPutReportsAPersistFailure(t *testing.T) {
	s, err := OpenWithBackend(failingSaveBackend{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("user-1", json.RawMessage(`{"a":1}`)); err == nil {
		t.Fatal("Put against a failing backend was reported as succeeding")
	}
	if _, ok := s.Get("user-1"); ok {
		t.Error("a refused Put was stored anyway")
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
	if err := s.Put("user-1", json.RawMessage(`{"a":1}`)); err != nil {
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
	if err := s.Put("user-1", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatalf("Put with no backend: %v", err)
	}
	if got, ok := s.Get("user-1"); !ok || string(got) != `{"a":1}` {
		t.Errorf("Get = (%s, %v), want ({\"a\":1}, true)", got, ok)
	}
}
