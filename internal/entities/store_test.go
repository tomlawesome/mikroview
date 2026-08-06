package entities

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenEmptyPathIsUsable(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") returned an error: %v", err)
	}
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4", Label: "test"}); err != nil {
		t.Fatalf("Upsert on an in-memory-only store returned an error: %v", err)
	}
	if len(s.List()) != 1 {
		t.Errorf("expected an in-memory-only store to still work, got %d entities", len(s.List()))
	}
}

func TestOpenMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entities.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on a missing file returned an error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected an empty store, got %d entities", len(s.List()))
	}
}

// A JSON array containing null is syntactically valid, so it unmarshals
// without error into a slice with a nil *Entity element -- without the
// nil guard in Open, the very next line (indexing e.Type/e.Key) would
// panic, contradicting the documented "malformed file never blocks
// startup" contract (same class of bug flags.Store's Open guards
// against).
func TestOpenSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entities.json")
	data := `[null, {"type":"host","key":"1.2.3.4","label":"router"}, null]`
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
	if list[0].Key != "1.2.3.4" || list[0].Label != "router" {
		t.Errorf("expected the real entity's data to be intact, got %+v", list[0])
	}
}

func TestOpenMalformedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entities.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Error("expected a non-nil informational error for a malformed file")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected a malformed file to start empty, got %d entities", len(s.List()))
	}
	// still usable despite the error
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4"}); err != nil {
		t.Fatalf("store returned from Open() with a malformed file should still be usable: %v", err)
	}
	if len(s.List()) != 1 {
		t.Error("store returned from Open() with a malformed file should still be usable")
	}
}

func TestUpsertRejectsEmptyTypeOrKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Upsert(Entity{Type: "", Key: "1.2.3.4"}); err != ErrInvalidEntity {
		t.Errorf("expected ErrInvalidEntity for an empty Type, got %v", err)
	}
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: ""}); err != ErrInvalidEntity {
		t.Errorf("expected ErrInvalidEntity for an empty Key, got %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected no entity to be stored from a rejected Upsert, got %d", len(s.List()))
	}
}

func TestUpsertCreatesNewEntity(t *testing.T) {
	s, _ := Open("")
	e, err := s.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4", Label: "core router", Tags: []string{"trusted"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != TypeHost || e.Key != "1.2.3.4" || e.Label != "core router" {
		t.Errorf("unexpected entity returned: %+v", e)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(list))
	}
	if len(list[0].Tags) != 1 || list[0].Tags[0] != "trusted" {
		t.Errorf("expected Tags to round-trip, got %+v", list[0].Tags)
	}
}

// TestUpsertReplacesInPlace proves the (Type, Key) pair is a true
// identity -- a second Upsert for the same pair must overwrite, not
// duplicate, so List() never grows past one entry per pair regardless of
// how many times it's edited.
func TestUpsertReplacesInPlace(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Upsert(Entity{Type: TypeRule, Key: "r13", Label: "first label"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(Entity{Type: TypeRule, Key: "r13", Label: "second label", Tags: []string{"noisy"}}); err != nil {
		t.Fatal(err)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected replacing an existing (Type, Key) pair to update in place, got %d entities", len(list))
	}
	if list[0].Label != "second label" {
		t.Errorf("expected the latest Upsert to win, got label %q", list[0].Label)
	}
}

// TestUpsertDistinguishesTypesWithTheSameKey proves Type is part of the
// identity, not just a label -- a "host" and a "rule" that happen to
// share the same Key string are two different entities.
func TestUpsertDistinguishesTypesWithTheSameKey(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: "shared", Label: "host label"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(Entity{Type: TypeRule, Key: "shared", Label: "rule label"}); err != nil {
		t.Fatal(err)
	}

	if len(s.List()) != 2 {
		t.Fatalf("expected the same Key under two different Types to be two entities, got %d", len(s.List()))
	}
}

func TestDeleteRemovesEntityAndReportsFound(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4"}); err != nil {
		t.Fatal(err)
	}

	if !s.Delete(TypeHost, "1.2.3.4") {
		t.Error("expected Delete on a known entity to return true")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected the entity to be removed, got %d remaining", len(s.List()))
	}
}

func TestDeleteUnknownIsNoOp(t *testing.T) {
	s, _ := Open("")
	if s.Delete(TypeHost, "nonexistent") {
		t.Error("expected Delete on an unknown pair to return false")
	}
}

func TestListOrdersByTypeThenKey(t *testing.T) {
	s, _ := Open("")
	s.Upsert(Entity{Type: TypeRule, Key: "r2"})
	s.Upsert(Entity{Type: TypeHost, Key: "9.9.9.9"})
	s.Upsert(Entity{Type: TypeHost, Key: "1.1.1.1"})
	s.Upsert(Entity{Type: TypeRule, Key: "r1"})

	list := s.List()
	if len(list) != 4 {
		t.Fatalf("expected 4 entities, got %d", len(list))
	}
	want := []struct{ typ, key string }{
		{TypeHost, "1.1.1.1"},
		{TypeHost, "9.9.9.9"},
		{TypeRule, "r1"},
		{TypeRule, "r2"},
	}
	for i, w := range want {
		if list[i].Type != w.typ || list[i].Key != w.key {
			t.Errorf("index %d: got (%s, %s), want (%s, %s)", i, list[i].Type, list[i].Key, w.typ, w.key)
		}
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "entities.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Upsert(Entity{Type: TypeHost, Key: "192.168.1.1", Label: "core", Tags: []string{"trusted-mail-sender"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Upsert(Entity{Type: TypeRule, Key: "r13", Label: "WAN input"}); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	list := s2.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 persisted entities after reopening, got %d: %+v", len(list), list)
	}
	var sawHost bool
	for _, e := range list {
		if e.Type == TypeHost && e.Key == "192.168.1.1" {
			sawHost = true
			if len(e.Tags) != 1 || e.Tags[0] != "trusted-mail-sender" {
				t.Errorf("expected Tags to survive persistence, got %+v", e.Tags)
			}
		}
	}
	if !sawHost {
		t.Errorf("expected the host entity to survive reopening, got %+v", list)
	}
}

func TestDeletePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entities.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4"}); err != nil {
		t.Fatal(err)
	}
	if !s1.Delete(TypeHost, "1.2.3.4") {
		t.Fatal("expected Delete to succeed")
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 0 {
		t.Errorf("expected the deletion to persist across reopening, got %+v", s2.List())
	}
}

// TestSeedImportsWhenEmpty is the migration path this store exists to
// support: an existing deployment's config.yaml RuleNames/HostNames
// become UI-editable Entity records the first time it boots against an
// empty (or freshly created) store.
func TestSeedImportsWhenEmpty(t *testing.T) {
	s, _ := Open("")
	n := s.Seed(
		map[string]string{"r13": "WAN input"},
		map[string]string{"192.168.1.1": "core router"},
	)
	if n != 2 {
		t.Fatalf("expected Seed to report importing 2 entities, got %d", n)
	}

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entities after seeding, got %d: %+v", len(list), list)
	}
	for _, e := range list {
		switch {
		case e.Type == TypeRule && e.Key == "r13":
			if e.Label != "WAN input" {
				t.Errorf("expected the rule's label to be imported, got %q", e.Label)
			}
		case e.Type == TypeHost && e.Key == "192.168.1.1":
			if e.Label != "core router" {
				t.Errorf("expected the host's label to be imported, got %q", e.Label)
			}
		default:
			t.Errorf("unexpected imported entity: %+v", e)
		}
	}
}

// TestSeedIsANoOpOnceStoreIsNonEmpty is what makes Seed safe to call
// unconditionally on every boot: once the store holds anything at all
// (whether from a prior Seed or a user's own edits, including deleting
// every imported entity), it must never re-import and silently undo a
// user's changes.
func TestSeedIsANoOpOnceStoreIsNonEmpty(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Upsert(Entity{Type: TypeHost, Key: "10.0.0.1", Label: "user-added"}); err != nil {
		t.Fatal(err)
	}

	n := s.Seed(map[string]string{"r13": "WAN input"}, nil)
	if n != 0 {
		t.Errorf("expected Seed to report 0 imports once the store is non-empty, got %d", n)
	}

	list := s.List()
	if len(list) != 1 || list[0].Key != "10.0.0.1" {
		t.Errorf("expected Seed to leave the existing entity untouched and import nothing, got %+v", list)
	}
}

func TestSeedWithNoConfiguredNamesIsANoOp(t *testing.T) {
	s, _ := Open("")
	if n := s.Seed(nil, nil); n != 0 {
		t.Errorf("expected Seed with empty maps to import nothing, got %d", n)
	}
	if len(s.List()) != 0 {
		t.Errorf("expected the store to remain empty, got %+v", s.List())
	}
}

func TestCountReflectsStoreSize(t *testing.T) {
	s, _ := Open("")
	if s.Count() != 0 {
		t.Errorf("expected Count()==0 for a fresh store, got %d", s.Count())
	}
	s.Upsert(Entity{Type: TypeHost, Key: "1.2.3.4"})
	s.Upsert(Entity{Type: TypeRule, Key: "r1"})
	if s.Count() != 2 {
		t.Errorf("expected Count()==2, got %d", s.Count())
	}
	s.Delete(TypeHost, "1.2.3.4")
	if s.Count() != 1 {
		t.Errorf("expected Count()==1 after a delete, got %d", s.Count())
	}
}
