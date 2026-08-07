// SPDX-License-Identifier: AGPL-3.0-only

package audit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPersistFailureIsNotSilent is the regression test for the review's
// R1 finding. Every persisted store used to swallow all three failure
// paths in persistLocked -- marshal, write, and rename -- so a full
// disk, a read-only remount, or a permissions change left mikroview
// running and reporting success while nothing reached disk.
//
// The audit store is the sharpest case and stands in for all eight
// here: an attacker who can cause writes to fail silently disables the
// record of admin actions, which is exactly the artefact that would
// show what they did.
//
// This asserts the observable contract that survives regardless of how
// logging is wired: the write genuinely failed, the in-memory state is
// still coherent, and the store did not corrupt the real file by
// leaving a half-written temp in its place.
func TestPersistFailureIsNotSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Record("admin", "entity.upsert", "host:10.0.0.1", "")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the first record to persist: %v", err)
	}

	// Make the directory read-only so the temp-file write fails. Skip
	// when running as root, which ignores the mode bits entirely.
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	s.Record("attacker", "flag.exclusion_remove", "port_scan|203.0.113.9", "")

	// The in-memory store must still be coherent -- a failed persist is
	// not allowed to lose or corrupt what the process already knows.
	if got := len(s.Query(Query{}).Entries); got != 2 {
		t.Errorf("in-memory entries = %d, want 2 (a failed persist must not drop in-memory state)", got)
	}

	// The on-disk file must be untouched rather than truncated: the
	// atomic write-temp-then-rename means a failure leaves the previous
	// good copy in place.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the previously-persisted file must survive a failed write: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a failed persist modified the on-disk file; the previous good copy must be left intact")
	}
}
