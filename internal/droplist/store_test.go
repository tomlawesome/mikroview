// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustOpen(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddStoresCanonicalEntry(t *testing.T) {
	s := mustOpen(t)
	e, err := s.Add("admin", "203.0.114.7/24", "scanning our SSH port", "")
	must(t, err)
	if e.CIDR.String() != "203.0.114.0/24" {
		t.Errorf("Entry.CIDR = %v, want the canonical, masked range", e.CIDR)
	}
	if e.AddedBy != "admin" || e.Reason != "scanning our SSH port" {
		t.Errorf("Add did not carry actor/reason through: %+v", e)
	}

	got := s.List()
	if len(got) != 1 || got[0].CIDR != e.CIDR {
		t.Errorf("List() = %+v, want the one entry just added", got)
	}
}

func TestAddRefusesDuplicate(t *testing.T) {
	s := mustOpen(t)
	must(t, ignoreEntry(s.Add("admin", "203.0.114.0/24", "first", "")))
	// A different spelling of the same range must still collide.
	if _, err := s.Add("admin", "203.0.114.9/24", "second", ""); !errors.Is(err, ErrExists) {
		t.Errorf("Add(duplicate) error = %v, want ErrExists", err)
	}
	if len(s.List()) != 1 {
		t.Errorf("List() = %v, want the duplicate attempt to leave the store unchanged", s.List())
	}
}

func TestAddRejectsInvalidCIDR(t *testing.T) {
	s := mustOpen(t)
	if _, err := s.Add("admin", "10.0.0.0/24", "bad", ""); !errors.Is(err, ErrNotPublic) {
		t.Errorf("Add(private range) error = %v, want ErrNotPublic", err)
	}
	if len(s.List()) != 0 {
		t.Error("a rejected Add must not leave a partial entry behind")
	}
}

func TestRemoveDeletesEntry(t *testing.T) {
	s := mustOpen(t)
	must(t, ignoreEntry(s.Add("admin", "203.0.114.0/24", "reason", "")))
	must(t, s.Remove("admin", "203.0.114.0/24"))
	if len(s.List()) != 0 {
		t.Errorf("List() after Remove = %v, want empty", s.List())
	}
}

func TestRemoveRefusesUnknownRange(t *testing.T) {
	s := mustOpen(t)
	for _, cidr := range []string{"203.0.114.0/24", "not-a-cidr"} {
		if err := s.Remove("admin", cidr); !errors.Is(err, ErrNotFound) {
			t.Errorf("Remove(%q) error = %v, want ErrNotFound", cidr, err)
		}
	}
}

func TestListIsSortedNumericallyByAddress(t *testing.T) {
	s := mustOpen(t)
	must(t, ignoreEntry(s.Add("admin", "198.51.101.0/24", "b", "")))
	must(t, ignoreEntry(s.Add("admin", "9.0.0.0/24", "a", "")))
	got := s.List()
	if len(got) != 2 || got[0].CIDR.String() != "9.0.0.0/24" || got[1].CIDR.String() != "198.51.101.0/24" {
		t.Errorf("List() = %+v, want numeric order (9.x before 198.x), not lexical", got)
	}
}

func TestAddUsesInjectableClock(t *testing.T) {
	s := mustOpen(t)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return fixed }

	e, err := s.Add("admin", "203.0.114.0/24", "reason", "")
	must(t, err)
	if !e.AddedAt.Equal(fixed) {
		t.Errorf("Entry.AddedAt = %v, want the injected clock's %v", e.AddedAt, fixed)
	}
}

func TestAddAndRemoveAreAudited(t *testing.T) {
	as, err := audit.Open(filepath.Join(t.TempDir(), "audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t)
	s.SetAuditor(as)

	must(t, ignoreEntry(s.Add("admin", "203.0.114.0/24", "scanning our SSH port", "flag-42")))
	must(t, s.Remove("admin", "203.0.114.0/24"))

	entries := as.Query(audit.Query{}).Entries
	if len(entries) != 2 {
		t.Fatalf("audit log has %d entries, want 2: %+v", len(entries), entries)
	}
	add, remove := entries[0], entries[1]

	if add.Action != "droplist.add" || add.Target != "203.0.114.0/24" {
		t.Errorf("add entry = %+v, want action droplist.add target 203.0.114.0/24", add)
	}
	if add.Detail != "scanning our SSH port (from flag flag-42)" {
		t.Errorf("add entry Detail = %q, want reason plus the flag note", add.Detail)
	}

	if remove.Action != "droplist.remove" || remove.Target != "203.0.114.0/24" {
		t.Errorf("remove entry = %+v, want action droplist.remove target 203.0.114.0/24", remove)
	}
	if remove.Detail != "scanning our SSH port (from flag flag-42)" {
		t.Errorf("remove entry Detail = %q, want the removed entry's own reason carried through", remove.Detail)
	}
}

func TestNilAuditorRecordsNothing(t *testing.T) {
	s := mustOpen(t)
	// SetAuditor deliberately not called -- Add/Remove must still work.
	must(t, ignoreEntry(s.Add("admin", "203.0.114.0/24", "reason", "")))
	must(t, s.Remove("admin", "203.0.114.0/24"))
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "droplist.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	must(t, ignoreEntry(s1.Add("admin", "203.0.114.0/24", "scanning our SSH port", "")))

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	got := s2.List()
	if len(got) != 1 || got[0].CIDR.String() != "203.0.114.0/24" || got[0].Reason != "scanning our SSH port" {
		t.Errorf("List() after reopening = %+v, want the entry added before restart", got)
	}
}

func ignoreEntry(_ Entry, err error) error { return err }
