// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// appliedMigrations pins the SHA-256 of every migration that has ever
// been released.
//
// **This is not tamper protection, and adding a hash here does not make
// the schema harder to attack.** The migrations are compiled into the
// binary by go:embed -- there is no file on a deployed system to edit --
// and this table lives in the same repository, so anyone who can change
// a migration can change its hash on the same commit. A program cannot
// meaningfully verify its own integrity against a value it also carries.
// Verifying what you are about to run is a signing problem, handled at
// release time; see .github/workflows/docker.yml and SECURITY.md.
//
// What this catches is a mistake that is easy to make and expensive to
// find: **editing a migration that has already been applied somewhere.**
// Deployed databases have recorded that version and will never run it
// again, so they keep the old schema; a fresh install runs the new text
// and gets a different one. The two silently diverge, and the symptom
// turns up much later as a column that exists on some deployments and
// not others.
//
// So: adding a new migration is normal, and adds a line here. Changing
// an existing one fails this test, which is the prompt to write a new
// migration instead.
var appliedMigrations = map[string]string{
	"0001_store_blob.sql": "a2dda218a690e4bd0d5d41be7cc9f8c9562d860dc183019304027b0119fd91e5",
}

func TestAppliedMigrationsAreImmutable(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	seen := make(map[string]bool, len(ms))
	for _, m := range ms {
		seen[m.name] = true
		want, pinned := appliedMigrations[m.name]
		if !pinned {
			t.Errorf("migration %s has no pinned hash.\n"+
				"Add it to appliedMigrations:\n    %q: %q,",
				m.name, m.name, hashOf(m.sql))
			continue
		}
		if got := hashOf(m.sql); got != want {
			t.Errorf("migration %s has changed.\n  got  %s\n  want %s\n\n"+
				"A migration that has already been applied must never be edited: deployments that "+
				"ran the old text will not run it again, so their schema stops matching a fresh "+
				"install's. Add a NEW migration with the change instead. If this one has genuinely "+
				"never been released, update the pinned hash.",
				m.name, got, want)
		}
	}

	for name := range appliedMigrations {
		if !seen[name] {
			t.Errorf("migration %s is pinned but no longer exists -- deleting a released migration "+
				"leaves existing deployments on a schema nothing can reproduce", name)
		}
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
