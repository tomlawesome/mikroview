// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// schema.go is the one numbered migration list for both backends (#1238,
// docs/decisions/upgrade-framework.md).
//
// A migration is a number, a description, and up to two steps: what it
// does to the JSON files under the data directory (File) and what it
// does to the database (Postgres, the .sql file that has always carried
// it). A number with only one of the two is normal -- the match log is a
// Postgres table with no file equivalent -- but the *number* is shared,
// so "this deployment is at schema 3" means the same thing whichever
// backend it runs on.
//
// Two rules, both load-bearing:
//
//  1. **Numbers and released steps are frozen.** A migration that has
//     shipped is never renumbered, reordered or edited; a change is a new
//     entry with the next number. Deployments that ran the old text will
//     never run it again, so editing it silently diverges them from a
//     fresh install. The Postgres half is pinned by hash in
//     migrations_test.go; the file half is pinned by this comment and by
//     review.
//
//  2. **A migration that reads an old document shape carries its own
//     frozen copy of that shape.** Never the store's current struct: the
//     store's shape is free to change with the next release, and a
//     migration written against it would then read a document by rules
//     that did not exist when it was written -- which is the exact
//     failure the whole framework exists to stop. Copy the fields the
//     migration needs into an unexported struct next to it, name the
//     schema version it describes, and leave it alone forever after.
//
// The file backend's own schema lives in a small document in the data
// directory (SchemaDocumentName). It holds a number and the build
// version that stamped it -- no operator data -- so it is written in the
// clear even where the stores themselves are encrypted (#853): a build
// has to be able to read it before it knows whether it may open anything
// else.

// SchemaDocumentName is the file backend's schema document, inside the
// data directory. A missing document reads as schema 0, which is every
// install that predates #1238.
const SchemaDocumentName = "schema.json"

// FileStep is one migration's work on the file backend. dir is the data
// directory. A step must be atomic per migration: write the new document
// beside the old with WriteFileAtomic (or FileBackend.Save) and let the
// rename publish it, so a step that is interrupted leaves the old
// document exactly as it was and the next start runs it again from the
// beginning.
//
// A nil step means this migration changes nothing on the file backend --
// stamping the new number is the whole of it.
type FileStep func(ctx context.Context, dir string) error

// SchemaMigration is one numbered entry in the shared list.
type SchemaMigration struct {
	// Number is the schema version this migration produces. Ascending,
	// gapless, and never reused -- see the rules above.
	Number int64
	// Description is what it does, in operator-facing words: it is what
	// the log line names while it runs.
	Description string
	// File is the file backend's step, or nil when there is nothing to
	// change there.
	File FileStep
	// Postgres names the embedded migrations/*.sql file that carries
	// this migration on the database, or "" when there is none. Its
	// numeric prefix must match Number.
	Postgres string
}

// schemaMigrations is the list. Append only.
var schemaMigrations = []SchemaMigration{
	{
		Number: 1,
		// The file backend's half is the stamp itself: every store's
		// document as written before #1238 is already the shape this
		// build reads, so schema 1 records that fact rather than
		// changing anything. The database's half is the store_blob
		// table it has always been.
		Description: "stamp the schema version (files); create store_blob (postgres)",
		Postgres:    "0001_store_blob.sql",
	},
	{
		Number:      2,
		Description: "create the match log table (postgres only)",
		Postgres:    "0002_match_log.sql",
	},
	{
		Number:      3,
		Description: "record provisional match-log rows (postgres only)",
		Postgres:    "0003_match_log_provisional.sql",
	},
}

// CurrentSchema is the highest schema version this build knows how to
// produce. Data stamped above it is refused -- see SchemaTooNewError.
func CurrentSchema() int64 { return highestSchema(schemaMigrations) }

func highestSchema(list []SchemaMigration) int64 {
	var highest int64
	for _, m := range list {
		if m.Number > highest {
			highest = m.Number
		}
	}
	return highest
}

// validateSchemaList catches an editing mistake in the list above at the
// first call rather than at the migration that trips over it: numbers
// have to ascend and be unique, because "pending" means "numbered above
// what is stored" and an out-of-order entry would be skipped forever on
// the deployments that already passed its number.
func validateSchemaList(list []SchemaMigration) error {
	var previous int64
	for _, m := range list {
		if m.Number <= 0 {
			return fmt.Errorf("persist: schema migration %q has number %d -- numbers start at 1", m.Description, m.Number)
		}
		if m.Number <= previous {
			return fmt.Errorf("persist: schema migration %d (%s) is not above the one before it (%d) -- the list is append-only and ascending",
				m.Number, m.Description, previous)
		}
		previous = m.Number
	}
	return nil
}

// fileSchemaDocument is what SchemaDocumentName holds.
//
// Version is the build that last migrated this data, not the build that
// last ran: it is the answer the downgrade guard needs, "which MikroView
// wrote the shapes in here". A later build that changes nothing leaves
// it alone, because that older build can still read the data correctly.
type fileSchemaDocument struct {
	Schema  int64  `json:"schema"`
	Version string `json:"version"`
	// StampedAt is for the operator reading the file, nothing else reads
	// it back.
	StampedAt time.Time `json:"stampedAt"`
}

// SchemaTooNewError is the downgrade guard: the data directory was
// written by a build that knows more schema versions than this one.
//
// It is refused rather than warned about, and nothing is written on the
// way out. The alternative -- starting anyway -- has this build rewrite
// every document in its own older shape, over fields it does not know
// about, which is silent data loss dressed up as a successful start.
// There is no way back from that, so there is no downgrade; see
// docs/upgrades.md, "Going back".
type SchemaTooNewError struct {
	// DataDir is the directory whose schema document was read.
	DataDir string
	// Stored is the schema version found on disk.
	Stored int64
	// Known is the highest this build can produce (CurrentSchema).
	Known int64
	// WrittenBy is the build version recorded alongside Stored, empty
	// if the document did not name one.
	WrittenBy string
}

func (e *SchemaTooNewError) Error() string {
	// The build version is what the operator acts on, so the sentence is
	// built around whether the data names one rather than dropping an
	// empty string into a fixed phrase.
	wrote := "it does not record which build wrote it, so you need a newer build of MikroView than this one"
	if e.WrittenBy != "" {
		wrote = fmt.Sprintf("it was last written by MikroView %s, so you need that build or a newer one", e.WrittenBy)
	}
	return fmt.Sprintf(
		"the data in %s is at schema version %d, but this build of MikroView only knows schema version %d -- "+
			"%s. Refusing to start rather than writing: an older build would overwrite newer data in shapes "+
			"it does not understand, and there is no way back from that",
		e.DataDir, e.Stored, e.Known, wrote)
}

// ErrSchemaNotStamped reports that the data directory is already in the
// shape this build expects, but the new schema version could not be
// recorded -- an unwritable data directory, in practice.
//
// It is wrapped only when no migration step ran in this pass, so nothing
// on disk changed and the next start simply tries again. A write failure
// *after* a step has run is not this: the data would then be migrated
// with the old number still stamped, and the same step would run a
// second time against data it has already changed.
var ErrSchemaNotStamped = errors.New("persist: the data directory's schema version could not be recorded")

// FileSchemaResult says what MigrateFileSchema did.
type FileSchemaResult struct {
	// From is the schema version found on disk, 0 for an install that
	// has never been stamped.
	From int64
	// To is the schema version now stamped -- equal to From when there
	// was nothing to do, and to the last migration that landed when a
	// run was cut short by an error.
	To int64
	// Applied lists the migration numbers that landed in this pass.
	Applied []int64
}

// ReadFileSchema reports the schema version the data directory is
// stamped at and the build version that stamped it.
//
// A missing document is schema 0 with no version: every install from
// before #1238, and every fresh one. A document that exists but cannot
// be read or parsed is a *StartupError, on the same fail-closed
// reasoning as Open -- guessing at "probably fresh" here would hand the
// downgrade guard a 0 for data that could be from any version.
func ReadFileSchema(dir string) (schema int64, writtenBy string, err error) {
	path := filepath.Join(dir, SchemaDocumentName)
	data, err := os.ReadFile(path) // #nosec G304 -- mikroview's own data directory, from config
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", nil
		}
		return 0, "", &StartupError{Store: "the schema document", Location: "file " + path, Err: err}
	}
	var doc fileSchemaDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return 0, "", &StartupError{Store: "the schema document", Location: "file " + path, Err: err}
	}
	return doc.Schema, doc.Version, nil
}

// CheckFileSchema is the downgrade guard on its own: it reads the data
// directory's schema version and returns a *SchemaTooNewError if it is
// above what this build knows. It never writes.
//
// MigrateFileSchema performs the same check before anything else, so
// callers that migrate do not need this one; it exists for the callers
// that only want to know whether they may touch the data at all.
func CheckFileSchema(dir string) error {
	stored, writtenBy, err := ReadFileSchema(dir)
	if err != nil {
		return err
	}
	return guardSchema(dir, stored, writtenBy, CurrentSchema())
}

func guardSchema(dir string, stored int64, writtenBy string, known int64) error {
	if stored <= known {
		return nil
	}
	return &SchemaTooNewError{DataDir: dir, Stored: stored, Known: known, WrittenBy: writtenBy}
}

// CheckSchemaDocument is CheckFileSchema's guard applied to a schema
// document's raw bytes rather than to one already sitting in dir --
// what -restore needs before it writes a bundled schema.json into a data
// directory (#1244). A bundle stamped newer than this build knows is
// refused the same way opening it directly would be, before anything on
// disk is touched; a bundle with no schema.json never reaches here,
// since that restores as schema 0 without needing a check at all.
//
// dir names the data directory the bundle would land in, for the error
// message only -- CheckSchemaDocument never reads or writes it.
func CheckSchemaDocument(dir string, data []byte) error {
	var doc fileSchemaDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return &StartupError{Store: "the schema document", Location: "the backup bundle", Err: err}
	}
	return guardSchema(dir, doc.Schema, doc.Version, CurrentSchema())
}

// MigrateFileSchema brings the data directory up to CurrentSchema and
// records it, running each pending migration's file step in order.
//
// buildVersion is this build's version string, stamped alongside the
// schema number so a later, older build can name it when it refuses the
// data.
//
// Guarantees, in the order they matter:
//
//   - Data newer than this build is refused before anything is read or
//     written (*SchemaTooNewError).
//   - Each migration is atomic on its own: its step publishes by rename,
//     so it either landed whole or not at all.
//   - The schema document is stamped after each migration lands, not
//     once at the end. A process killed part-way through an upgrade
//     comes back stamped at the last migration that finished, and
//     resumes at the first that did not.
func MigrateFileSchema(ctx context.Context, dir, buildVersion string) (FileSchemaResult, error) {
	return migrateFileSchema(ctx, dir, buildVersion, schemaMigrations)
}

func migrateFileSchema(ctx context.Context, dir, buildVersion string, list []SchemaMigration) (FileSchemaResult, error) {
	if err := validateSchemaList(list); err != nil {
		return FileSchemaResult{}, err
	}

	stored, writtenBy, err := ReadFileSchema(dir)
	if err != nil {
		return FileSchemaResult{}, err
	}
	res := FileSchemaResult{From: stored, To: stored}

	known := highestSchema(list)
	if err := guardSchema(dir, stored, writtenBy, known); err != nil {
		return res, err
	}

	pending := make([]SchemaMigration, 0, len(list))
	for _, m := range list {
		if m.Number > stored {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		schemaLog.Info(fmt.Sprintf("data directory schema is up to date at version %d (%s)", stored, dir))
		return res, nil
	}

	// Announced before the first one runs, for the same reason the
	// Postgres runner does it: an upgrade that stalls should be visibly
	// an upgrade rather than a mystery hang at boot.
	schemaLog.Info(fmt.Sprintf("data directory schema is at version %d, %d migration(s) to apply -- updating %s",
		stored, len(pending), dir))

	ran := false
	for _, m := range pending {
		if m.File != nil {
			schemaLog.Info(fmt.Sprintf("applying schema version %d: %s", m.Number, m.Description))
			began := time.Now()
			ran = true
			if err := m.File(ctx, dir); err != nil {
				schemaLog.Error(fmt.Sprintf("schema version %d (%s) failed -- the data directory is unchanged and "+
					"still at version %d; the next start resumes here", m.Number, m.Description, res.To))
				return res, fmt.Errorf("persist: applying schema version %d (%s) to %s: %w",
					m.Number, m.Description, dir, err)
			}
			schemaLog.Info(fmt.Sprintf("applied schema version %d in %s", m.Number, time.Since(began).Round(time.Millisecond)))
		}

		// After the step, never before: the stamp is what makes the
		// migration finished, so a crash between the two costs a repeat
		// of one migration, not a skipped one.
		if err := stampFileSchema(dir, m.Number, buildVersion); err != nil {
			if !ran {
				// Nothing has been changed on disk in this pass, so the
				// only casualty is the record. The caller can carry on
				// and try again next start.
				return res, fmt.Errorf("%w: %s: %w", ErrSchemaNotStamped, dir, err)
			}
			return res, fmt.Errorf("persist: recording schema version %d in %s after it was applied: %w",
				m.Number, dir, err)
		}
		res.To = m.Number
		res.Applied = append(res.Applied, m.Number)
	}

	schemaLog.Info(fmt.Sprintf("data directory schema updated from version %d to version %d -- "+
		"an older MikroView build will refuse this data from now on", res.From, res.To))
	return res, nil
}

// stampFileSchema records the schema version and the build that produced
// it, published by rename like every other document this package writes
// -- a half-written schema document would be read as an unparseable one
// and refuse the next start.
func stampFileSchema(dir string, number int64, buildVersion string) error {
	body, err := json.MarshalIndent(fileSchemaDocument{
		Schema:    number,
		Version:   buildVersion,
		StampedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	// 0600 and not 0644, matching every other document in the data
	// directory: nothing here is secret, but the directory's mode is one
	// rule rather than a per-file judgement.
	return WriteFileAtomic(filepath.Join(dir, SchemaDocumentName), append(body, '\n'), 0o600)
}
