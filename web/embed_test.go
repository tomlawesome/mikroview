// SPDX-License-Identifier: AGPL-3.0-only

package web

import (
	"io/fs"
	"testing"
)

// TestDistFSAlwaysCompilesAndOpens is the property .gitkeep exists for:
// the embed pattern must match something on a fresh clone where nothing
// has been built, or the whole module stops compiling. If this test is
// running at all the compile-time half already held, so what is left to
// check is that fs.Sub finds the directory rather than erroring.
func TestDistFSAlwaysCompilesAndOpens(t *testing.T) {
	if _, err := DistFS(); err != nil {
		t.Fatalf("DistFS() = %v, want no error even with an unbuilt dist/", err)
	}
}

// TestHasUIFollowsIndexHTML pins HasUI to the one thing it claims to
// report. Deliberately not asserting true or false outright: whether a
// frontend was built is a property of the checkout this test runs in,
// not of the code, and hardcoding either answer would make the test
// fail for the wrong reason depending on whether `make build` had been
// run first.
func TestHasUIFollowsIndexHTML(t *testing.T) {
	dist, err := DistFS()
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := fs.Stat(dist, "index.html")
	built := statErr == nil

	if got := HasUI(); got != built {
		t.Errorf("HasUI() = %v, but index.html present = %v -- these must agree, since the warning at the call site is the only thing that tells an operator which case they are in", got, built)
	}
}
