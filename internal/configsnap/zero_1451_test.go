// SPDX-License-Identifier: AGPL-3.0-only

package configsnap

import "testing"

// #1451: Count and Newest on an empty and a filled store.
func TestCountAndNewest(t *testing.T) {
	s := openMemory(t)
	if s.Count() != 0 {
		t.Errorf("Count() on an empty store = %d", s.Count())
	}
	if _, ok := s.Newest(); ok {
		t.Error("Newest() on an empty store reported a snapshot")
	}
	add(t, s, "manual", "first")
	last := add(t, s, "manual", "second")
	if s.Count() != 2 {
		t.Errorf("Count() = %d, want 2", s.Count())
	}
	newest, ok := s.Newest()
	if !ok || newest.ID != last.ID || newest.Note != "second" || newest.Text == "" {
		t.Errorf("Newest() = %+v, %v; want the second snapshot with its text", newest.Meta, ok)
	}
}
