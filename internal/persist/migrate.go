// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// MigrateResult describes what AdoptFile did, so the caller can tell the
// operator something specific rather than "ok".
type MigrateResult struct {
	// Migrated is true only when a file's contents were copied into a
	// store that had none.
	Migrated bool
	// FilePath is the file that was read, for the log line telling the
	// operator it is no longer used.
	FilePath string
	// Bytes copied, purely for the log.
	Bytes int
}

// AdoptFile copies an existing JSON file into a backend that has no
// document yet -- the automatic JSON-to-Postgres migration issue #131
// asks for, so there is no separate import step to run.
//
// Three rules, each load-bearing:
//
//   - It only ever writes into an *empty* store. If the backend already
//     has a document, the file is ignored entirely. Without this, a
//     stale JSON file left on disk would roll live data back on every
//     subsequent restart.
//
//   - It does not delete or rename the file afterwards. Reverting is
//     then "remove the Postgres config and restart", which comes back on
//     the last file state -- stale, but present. Deleting would make a
//     config change irreversible, which is the wrong shape for something
//     an operator may be trying out.
//
//   - It copies bytes verbatim, without parsing. Whatever the store
//     could read from the file, it can read from the backend, because
//     they are the same bytes. Parsing here would mean this function had
//     to understand six different document shapes and stay correct as
//     they change.
func AdoptFile(ctx context.Context, filePath string, b Backend) (MigrateResult, error) {
	res := MigrateResult{FilePath: filePath}
	if filePath == "" || b == nil {
		return res, nil
	}

	snap, err := b.Load(ctx)
	if err != nil {
		return res, fmt.Errorf("persist: checking %s before migrating: %w", b.Describe(), err)
	}
	if snap.Exists {
		return res, nil // already has data; the file is history
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil // nothing to migrate: a genuinely fresh install
		}
		return res, fmt.Errorf("persist: reading %s to migrate: %w", filePath, err)
	}

	if _, err := b.Save(ctx, data, 0); err != nil {
		if errors.Is(err, ErrConflict) {
			// Another instance migrated the same store first. Its copy
			// stands; this is a success, not a collision to report.
			return res, nil
		}
		return res, fmt.Errorf("persist: migrating %s into %s: %w", filePath, b.Describe(), err)
	}

	res.Migrated = true
	res.Bytes = len(data)
	return res, nil
}
