// SPDX-License-Identifier: AGPL-3.0-only

// Package persist is the storage boundary the six persisted stores sit
// on: accounts/tokens (internal/auth), flags, entities, the MAC
// registry, rule usage, and detector settings.
//
// Each store keeps its whole dataset in memory and serves reads from
// there; persistence is a whole-document snapshot, written after each
// change. That shape predates this package -- it is what every store
// already did against a JSON file -- and is deliberately preserved, so
// moving a store onto Postgres changes where its bytes live and nothing
// about how it behaves. See docs/decisions/postgres-backend.md for why
// a relational schema was rejected.
//
// Deliberately not covered: the live event stream (internal/store),
// which is in-memory-only by design, and the TLS material
// (internal/servertls), which mikroview needs before it could reach a
// database at all.
//
// One exception to the blob-per-store shape: internal/matchlog's
// Postgres backend, which needs a genuine indexed table rather than a
// document -- see docs/decisions/postgres-backend.md §1a. It shares
// this package's Pool (Pool.Raw) and migration runner, but not the
// Backend interface below.
//
// EncryptedFileBackend (#853) wraps FileBackend so every document is
// sealed under the operator's retention key -- the same key and cipher
// internal/retention uses for the on-disk event history. See its own doc
// comment, and docs/decisions/event-retention.md's amendment, for what it
// does and does not cover.
package persist

import (
	"context"

	gpersist "github.com/tomlawesome/gauntlet/persist"
)

// ErrConflict is returned by Save when the stored version is not the one
// the caller expected -- someone else wrote in between.
//
// This is not an error condition to bubble up to a user. It means
// "reload and try again": the caller's in-memory copy is stale, which is
// exactly the situation that used to cause a silent lost update against
// the file backend (the server persisting over a change a CLI command
// had just made). Callers must not treat it as fatal.
//
// It is gauntlet's own sentinel (#1202): gauntlet's stores compare
// against gpersist.ErrConflict, and every store here compares against
// this one, so the two must be the same value.
var ErrConflict = gpersist.ErrConflict

// Snapshot is one store's persisted document plus the token needed to
// write it back safely. It is gauntlet's type (#1202), so this package's
// backends satisfy gauntlet's persist.Backend with no adapter. The fields
// are Payload, Version and Exists, as documented on the gauntlet type;
// "Exists" matters here because auto-migration must not overwrite an
// existing empty document from a stale JSON file.
type Snapshot = gpersist.Snapshot

// Every backend here satisfies gauntlet's persist.Backend as it is, and
// the encrypted file backend its AtRest (#1202). A compile-time check, so
// a signature drift fails the build rather than a gauntlet store at start.
var (
	_ gpersist.Backend = (*FileBackend)(nil)
	_ gpersist.Backend = (*EncryptedFileBackend)(nil)
	_ gpersist.Backend = (*PostgresBackend)(nil)
	_ gpersist.AtRest  = (*EncryptedFileBackend)(nil)
)

// Backend is where one store's document lives.
//
// Implementations must be safe for concurrent use: the CLI commands and
// the running server are separate processes against the same backend,
// which is the whole reason Save takes a version rather than just
// writing.
type Backend interface {
	// Load reads the current document. A store that has never been
	// written returns a zero-value Snapshot and a nil error -- a missing
	// document is the normal first-run case, not a failure.
	Load(ctx context.Context) (Snapshot, error)

	// Save writes payload if the stored version still matches expect,
	// returning the new version. It returns ErrConflict if it does not.
	//
	// expect == 0 means "create": it succeeds only if nothing is stored,
	// so two processes racing to create the same store can't both win.
	Save(ctx context.Context, payload []byte, expect int64) (int64, error)

	// Close releases whatever the backend holds. Safe to call on a
	// backend that was never used.
	Close() error

	// Describe returns a short, credential-free description for logs and
	// the config-problem surface, e.g. "file /var/lib/mikroview/users.json"
	// or "postgres store 'auth'". Never includes a DSN or password.
	Describe() string
}

// VersionReader is an optional Backend capability: reporting the stored
// version without transferring the document.
//
// It exists for callers that poll for staleness far more often than the
// document actually changes -- internal/auth does this on every
// authenticated request, so a live server would otherwise pull the whole
// accounts document over the wire per request just to compare an
// integer. Callers must treat its absence as normal and fall back to
// Load: FileBackend deliberately does not implement it, because its
// version *is* a hash of the file's bytes, so there is nothing cheaper
// than reading them.
//
// exists is false when nothing has been stored yet, matching
// Snapshot.Exists.
type VersionReader interface {
	Version(ctx context.Context) (version int64, exists bool, err error)
}
