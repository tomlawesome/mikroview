// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

func mustOpenStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "watchlist.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestUpsertAndGet(t *testing.T) {
	s := mustOpenStore(t)
	e := Entry{ID: "e1", Name: "SSH watch", Ports: []int{22}}
	if err := s.Upsert(e); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, ok := s.Get("e1")
	if !ok {
		t.Fatal("Get did not find the entry just upserted")
	}
	if got.Name != "SSH watch" || len(got.Ports) != 1 || got.Ports[0] != 22 {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set on first creation")
	}
}

func TestGetMissingReturnsFalse(t *testing.T) {
	s := mustOpenStore(t)
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(\"nope\") = true, want false for an entry that was never created")
	}
}

func TestUpsertRejectsEmptyID(t *testing.T) {
	s := mustOpenStore(t)
	err := s.Upsert(Entry{Ports: []int{22}})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("Upsert with no ID = %v, want ErrInvalidEntry", err)
	}
}

func TestUpsertRejectsNoPorts(t *testing.T) {
	s := mustOpenStore(t)
	err := s.Upsert(Entry{ID: "e1"})
	if !errors.Is(err, ErrNoPorts) {
		t.Errorf("Upsert with no ports = %v, want ErrNoPorts", err)
	}
}

func TestUpsertRejectsInvalidText(t *testing.T) {
	s := mustOpenStore(t)
	cases := []Entry{
		{ID: "e1", Ports: []int{22}, Name: "bad\x00null"},
		{ID: "e1", Ports: []int{22}, Name: "bad\x01control"},
		{ID: "e1", Ports: []int{22}, DestIP: "1.2.3.4\x00"},
	}
	for _, e := range cases {
		if err := s.Upsert(e); !errors.Is(err, ErrInvalidText) {
			t.Errorf("Upsert(%+v) = %v, want ErrInvalidText", e, err)
		}
	}
}

// An update must not reset CreatedAt to now -- how long an entry has
// existed is meaningful (e.g. for a future "how long has this been
// observing" display) and a routine edit isn't a new entry.
func TestUpsertUpdatePreservesCreatedAt(t *testing.T) {
	s := mustOpenStore(t)
	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Get("e1")

	time.Sleep(2 * time.Millisecond)
	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22, 23}, Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	second, _ := s.Get("e1")

	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt changed on update: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if second.Name != "renamed" || len(second.Ports) != 2 {
		t.Errorf("update did not apply: %+v", second)
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	s := mustOpenStore(t)
	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	s.Delete("e1")
	if _, ok := s.Get("e1"); ok {
		t.Error("entry still present after Delete")
	}
}

func TestDeleteMissingIsNoop(t *testing.T) {
	s := mustOpenStore(t)
	s.Delete("never-existed") // must not panic
}

func TestListIsSortedByID(t *testing.T) {
	s := mustOpenStore(t)
	for _, id := range []string{"c", "a", "b"} {
		if err := s.Upsert(Entry{ID: id, Ports: []int{22}}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("List() returned %d entries, want 3", len(got))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got[i].ID != want {
			t.Errorf("List()[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watchlist.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{ID: "e1", Name: "SSH watch", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, DestIP: "10.0.0.5", Ports: []int{22, 2222}}
	if err := s1.Upsert(entry); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopening after restart: %v", err)
	}
	got, ok := s2.Get("e1")
	if !ok {
		t.Fatal("entry not found after restart")
	}
	if got.Name != entry.Name || got.Source.MAC != entry.Source.MAC || got.DestIP != entry.DestIP || len(got.Ports) != 2 {
		t.Errorf("entry not intact after restart: %+v", got)
	}
}

func TestEmptyPathIsInMemoryOnly(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	if err := s.Upsert(Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("e1"); !ok {
		t.Error("in-memory-only store did not retain an upserted entry within the same process")
	}
}
