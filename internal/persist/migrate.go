// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/tomlawesome/mikroview/internal/retention"
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

// AdoptSealedFile is AdoptFile for a file the encrypted file backend
// wrote (#1202 review R2): the document is opened under key, with the
// store's name bound as EncryptedFileBackend binds it, and handed to dst
// readable. Copying the bytes verbatim, as AdoptFile does, would put the
// file backend's envelope -- under a binding the database row does not use --
// where the store expects its document, and nothing could read it.
//
// Whether the document is then stored sealed is dst's business: an auth
// store's dst is gauntlet's persist.Encrypt under the store's label,
// which seals it again; any other store's dst is the plain Postgres
// backend, as before the auth stores were sealed.
//
// A source that is not sealed but parses as JSON -- the plaintext a
// release that ran without a key left -- is refused with
// ErrPlaintextDocument, as opening it in place is (#1202, ruling 19):
// nothing is written and the file is left as it is.
//
// AdoptFile's three rules hold here too: only an empty dst is written,
// the source file is never touched, and a file that does not exist is
// a fresh install. A file that will not open -- the wrong key, damaged,
// sealed as another store -- fails and writes nothing. storeName is the
// store's name, which the file is sealed bound to (#1450); a file sealed
// before #1450, bound to src's path, opens too, and is not re-saved:
// once adopted it is no longer read.
func AdoptSealedFile(ctx context.Context, src, storeName string, key *retention.Key, dst Backend) (MigrateResult, error) {
	res := MigrateResult{FilePath: src}
	if src == "" || dst == nil {
		return res, nil
	}
	if key == nil {
		return res, fmt.Errorf("persist: migrating %s: no key to open it with", src)
	}

	existing, err := dst.Load(ctx)
	if err != nil {
		return res, fmt.Errorf("persist: checking %s before migrating: %w", dst.Describe(), err)
	}
	if existing.Exists {
		return res, nil // already has data; the file is history
	}

	// One read, then one decision on those bytes: a second read for the
	// plaintext case could see a different file.
	file := NewEncryptedFileBackend(src, storeName, key)
	raw, err := file.inner.Load(ctx)
	if err != nil {
		return res, fmt.Errorf("persist: reading %s to migrate: %w", src, err)
	}
	if !raw.Exists {
		return res, nil // nothing to migrate: a genuinely fresh install
	}
	if !retention.HasSealMagic(raw.Payload) && json.Valid(raw.Payload) {
		return res, fmt.Errorf("persist: migrating %s: %w", src, ErrPlaintextDocument)
	}
	doc, _, err := file.unseal(raw)
	if err != nil {
		return res, fmt.Errorf("persist: migrating %s: %w", src, err)
	}

	if _, err := dst.Save(ctx, doc.Payload, 0); err != nil {
		if errors.Is(err, ErrConflict) {
			// Another instance migrated the same store first. Its copy
			// stands; this is a success, not a collision to report.
			return res, nil
		}
		return res, fmt.Errorf("persist: migrating %s into %s: %w", src, dst.Describe(), err)
	}

	res.Migrated = true
	res.Bytes = len(doc.Payload)
	return res, nil
}
