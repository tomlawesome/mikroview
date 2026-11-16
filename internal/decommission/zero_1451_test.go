// SPDX-License-Identifier: AGPL-3.0-only

package decommission

import (
	"context"
	"testing"
)

// #1451: Flush forces the pending write now, so a reopen sees the watch.
func TestFlushWritesThePendingDocument(t *testing.T) {
	path := t.TempDir() + "/decommission.json"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := addWatch(t, s, "192.0.2.0/24")
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close(context.Background())
	var found bool
	for _, got := range again.List() {
		found = found || got.ID == w.ID
	}
	if !found {
		t.Errorf("watch %s not on disk after Flush", w.ID)
	}
}
