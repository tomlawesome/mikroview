// SPDX-License-Identifier: AGPL-3.0-only

package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !s.Delete("ether1|bridge1") {
		t.Error("expected Delete to report true for a known key")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected the declaration to be gone, got %d", len(s.List()))
	}
}

func TestDeleteUnknownKeyIsNoop(t *testing.T) {
	s, _ := Open("")
	if s.Delete("nonexistent") {
		t.Error("expected Delete to report false for an unknown key")
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
