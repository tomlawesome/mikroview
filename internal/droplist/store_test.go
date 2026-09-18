// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"errors"
	"os"
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

// TestAddRejectsLineAndParagraphSeparators pins validText's Zl/Zp rule
// (security review): U+2028 LINE SEPARATOR and U+2029 PARAGRAPH
// SEPARATOR render as whitespace but are not a plain space, the same
// class of character Cf/control already existed to keep out of a reason
// that renders directly in the UI and lands in a persisted, admin-read
// file.
func TestAddRejectsLineAndParagraphSeparators(t *testing.T) {
	s := mustOpen(t)
	if _, err := s.Add("admin", "203.0.114.0/24", "line break", ""); !errors.Is(err, ErrBadText) {
		t.Errorf("Add(reason with U+2028) error = %v, want ErrBadText", err)
	}
	if _, err := s.Add("admin", "203.0.114.0/24", "para break", ""); !errors.Is(err, ErrBadText) {
		t.Errorf("Add(reason with U+2029) error = %v, want ErrBadText", err)
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

// TestOpenCanonicalisesAnUnmaskedEntryLoadedFromDisk is the v0.6.0
// pre-release audit's finding: OpenWithBackend's load loop indexed each
// loaded entry by e.CIDR.String() verbatim, never through Validate the
// way Add does -- so a file carrying an unmasked prefix (hand-edited,
// or written by a version predating this canonicalisation) loaded with
// that exact spelling as its map key. Remove always computes its key
// through parseCIDR().Masked(), the canonical form, so it could never
// match a key that never went through that same masking -- the entry
// became permanently unreachable by any client the moment it was
// loaded.
func TestOpenCanonicalisesAnUnmaskedEntryLoadedFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "droplist.json")
	raw := `{"entries":[{"cidr":"203.0.114.7/24","addedBy":"admin","addedAt":"2026-01-02T03:04:05Z","reason":"scanning our SSH port"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	got := s.List()
	if len(got) != 1 || got[0].CIDR.String() != "203.0.114.0/24" {
		t.Fatalf("List() = %+v, want the one entry masked to 203.0.114.0/24", got)
	}

	// The entry loaded from disk must be reachable by the same canonical
	// key Add/Remove always use -- not the unmasked spelling the file
	// happened to carry.
	if err := s.Remove("admin", "203.0.114.7/24"); err != nil {
		t.Errorf("Remove(unmasked spelling of a loaded entry) = %v, want success", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("List() after Remove = %v, want empty", s.List())
	}
}

// TestOpenDropsAnEntryThatNoLongerValidates is the other half of the
// same finding: a loaded entry must satisfy the same rules Add enforces
// on the way in, not be trusted just because it is already on disk --
// a file could predate a rule (or be hand-edited) and carry a range
// Validate would refuse today.
func TestOpenDropsAnEntryThatNoLongerValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "droplist.json")
	raw := `{"entries":[
		{"cidr":"10.0.0.0/24","addedBy":"admin","addedAt":"2026-01-02T03:04:05Z","reason":"private, never valid"},
		{"cidr":"203.0.114.0/24","addedBy":"admin","addedAt":"2026-01-02T03:04:05Z","reason":"still good"}
	]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	got := s.List()
	if len(got) != 1 || got[0].CIDR.String() != "203.0.114.0/24" {
		t.Errorf("List() = %+v, want only the entry that still validates", got)
	}
}
