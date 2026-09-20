// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// failingSaveBackend lets Open/OpenWithBackend succeed (nothing stored
// yet) but fails every Save -- the v0.6.0 audit's R6 fix needs a backend
// that can never durably record the change a mutator is about to make.
// Copied from internal/auth/store_test.go, which carries the fuller
// doc comment; this package has no equivalent of its own.
type failingSaveBackend struct{}

func (failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("backend unavailable")
}
func (failingSaveBackend) Close() error     { return nil }
func (failingSaveBackend) Describe() string { return "failing test backend" }

func TestOpenEmptyPathIsUsable(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	if _, err := s.Put("ether1|bridge1", "internal management link, never expected to log", "admin"); err != nil {
		t.Fatalf("Put on an in-memory-only store returned an error: %v", err)
	}
	if len(s.List()) != 1 {
		t.Errorf("expected an in-memory-only store to still work, got %d declarations", len(s.List()))
	}
}

func TestOpenMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected an empty store, got %d declarations", len(s.List()))
	}
}

func TestOpenCorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected Open() on a corrupt file to return an error")
	}
}

// A JSON array containing null is syntactically valid, so it unmarshals
// without error into a slice with a nil *Declaration element -- without
// the nil guard in OpenWithBackend, the very next line (indexing
// d.Key) would panic, same class of bug internal/entities and
// internal/flags both guard against in their own Open.
func TestOpenSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	data := `{"declarations":[null, {"key":"ether1|bridge1","reason":"management link","declaredBy":"admin"}, null]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path) // must not panic
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected the one real entry to survive, got %d: %+v", len(list), list)
	}
	if list[0].Key != "ether1|bridge1" || list[0].Reason != "management link" {
		t.Errorf("expected the real declaration's data to be intact, got %+v", list[0])
	}
}

func TestPutUpsertsInPlace(t *testing.T) {
	s, _ := Open("")

	first, err := s.Put("ether1|bridge1", "first reason", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(s.List()))
	}

	second, err := s.Put("ether1|bridge1", "second reason", "admin2")
	if err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected the second Put to replace the first in place, got %d declarations", len(list))
	}
	if list[0].Reason != "second reason" || list[0].DeclaredBy != "admin2" {
		t.Errorf("expected the upsert to overwrite reason/declaredBy, got %+v", list[0])
	}
	if !second.DeclaredAt.After(first.DeclaredAt) && second.DeclaredAt != first.DeclaredAt {
		t.Errorf("expected DeclaredAt to be set on each Put")
	}
}

func TestPutSetsDeclaredAtServerSide(t *testing.T) {
	s, _ := Open("")
	d, err := s.Put("ether1|bridge1", "a reason", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if d.DeclaredAt.IsZero() {
		t.Error("expected DeclaredAt to be set server-side, got zero value")
	}
}

func TestPutRejectsEmptyKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("", "a reason", "admin"); err == nil {
		t.Error("expected an empty key to be rejected")
	}
}

func TestPutRejectsEmptyReason(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("ether1|bridge1", "", "admin"); err == nil {
		t.Error("expected an empty reason to be rejected")
	}
}

func TestPutRejectsOverlongKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put(strings.Repeat("a", maxKeyLength+1), "a reason", "admin"); err == nil {
		t.Error("expected a key over the length limit to be rejected")
	}
	if _, err := s.Put(strings.Repeat("a", maxKeyLength), "a reason", "admin"); err != nil {
		t.Errorf("expected a key exactly at the length limit to be accepted, got %v", err)
	}
}

func TestPutRejectsOverlongReason(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("ether1|bridge1", strings.Repeat("a", maxReasonLength+1), "admin"); err == nil {
		t.Error("expected a reason over the length limit to be rejected")
	}
	if _, err := s.Put("ether1|bridge1", strings.Repeat("a", maxReasonLength), "admin"); err != nil {
		t.Errorf("expected a reason exactly at the length limit to be accepted, got %v", err)
	}
}

func TestPutRejectsControlCharacters(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("ether1\x00bridge1", "a reason", "admin"); err == nil {
		t.Error("expected a key containing a control character to be rejected")
	}
	if _, err := s.Put("ether1|bridge1", "a reason\nwith a newline", "admin"); err == nil {
		t.Error("expected a reason containing a control character to be rejected")
	}
}

func TestDeleteRemovesKnownKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("ether1|bridge1", "a reason", "admin"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Delete("ether1|bridge1"); err != nil || !ok {
		t.Errorf("expected Delete to report (true, nil) for a known key, got (%v, %v)", ok, err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected the declaration to be gone, got %d", len(s.List()))
	}
}

func TestDeleteUnknownKeyIsNoop(t *testing.T) {
	s, _ := Open("")
	if ok, err := s.Delete("nonexistent"); err != nil || ok {
		t.Errorf("expected Delete to report (false, nil) for an unknown key, got (%v, %v)", ok, err)
	}
}

func TestListSortsByKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Put("z|boundary", "reason", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("a|boundary", "reason", "admin"); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 2 || list[0].Key != "a|boundary" || list[1].Key != "z|boundary" {
		t.Errorf("expected declarations sorted by key, got %+v", list)
	}
}

func TestPersistenceSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("ether1|bridge1", "a reason", "admin"); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 1 || list[0].Key != "ether1|bridge1" || list[0].Reason != "a reason" || list[0].DeclaredBy != "admin" {
		t.Errorf("expected the declaration to survive a reopen, got %+v", list)
	}
}

// TestPutLeavesTheStoreUnchangedWhenPersistFails is the v0.6.0 audit's
// R6 fix: a coverage-gap declaration that cannot be saved must not take
// effect in memory either, or a restart before the next good write
// would silently discard it while the caller was told it succeeded.
func TestPutLeavesTheStoreUnchangedWhenPersistFails(t *testing.T) {
	s, err := OpenWithBackend(failingSaveBackend{})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}

	if _, err := s.Put("ether1|bridge1", "internal management link", "admin"); err == nil {
		t.Fatal("Put against a backend that cannot save = nil error, want one")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected no declaration to exist in memory after a failed persist, got %+v", s.List())
	}
}

// TestPutRestoresThePreviousDeclarationWhenPersistFails proves the
// rollback restores the previous record, not just "nothing new
// appeared" -- a re-declaration that cannot be saved must leave the old
// reason in place.
func TestPutRestoresThePreviousDeclarationWhenPersistFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("ether1|bridge1", "original reason", "admin"); err != nil {
		t.Fatal(err)
	}

	s.backend = failingSaveBackend{}
	if _, err := s.Put("ether1|bridge1", "changed reason", "someone-else"); err == nil {
		t.Fatal("Put against a backend that cannot save = nil error, want one")
	}
	list := s.List()
	if len(list) != 1 || list[0].Reason != "original reason" || list[0].DeclaredBy != "admin" {
		t.Errorf("expected the previous declaration restored after a failed persist, got %+v", list)
	}
}

// TestDeleteLeavesTheDeclarationInPlaceWhenPersistFails is the v0.6.0
// audit's R6 fix: an undeclare that cannot be saved must not read as
// undeclared, or a restart before the next good write would resurrect a
// declaration an operator was told was already removed.
func TestDeleteLeavesTheDeclarationInPlaceWhenPersistFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("ether1|bridge1", "a reason", "admin"); err != nil {
		t.Fatal(err)
	}

	s.backend = failingSaveBackend{}
	deleted, err := s.Delete("ether1|bridge1")
	if err == nil {
		t.Fatal("Delete against a backend that cannot save = nil error, want one")
	}
	if deleted {
		t.Error("expected Delete to report false when the removal could not be saved")
	}
	if len(s.List()) != 1 {
		t.Errorf("expected the declaration to still exist after a failed persist, got %+v", s.List())
	}
}
