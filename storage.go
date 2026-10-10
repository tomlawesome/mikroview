// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// storage decides where each persisted store lives, and is the one place
// that knows whether this deployment is on JSON files or Postgres
// (issue #131).
//
// The stores themselves don't: they take a persist.Backend and behave
// identically either way, which is what makes the choice a deployment
// decision rather than a code path per store.
type storage struct {
	// pool is nil when Postgres isn't configured -- the default,
	// zero-infrastructure path.
	pool *persist.Pool
	log  *slog.Logger
	cfg  config.Config
	// adoptedBefore records whether this deployment had already run on
	// Postgres when this process started. Only a first move is allowed
	// to adopt the JSON files; see backendFor.
	adoptedBefore bool
	// key is the same master key internal/retention derives the event
	// history's per-file keys from, loaded once from history.keyFile
	// (#853: one key, not two -- see docs/decisions/event-retention.md's
	// amendment). nil means no key is configured: backendFor then
	// persists no JSON-file store at all rather than writing it in the
	// clear, and authBackendFor refuses (the server never gets that far
	// without one, #1202). It has nothing to do with history.enabled,
	// which only switches the *event log* on top of this same key.
	key *retention.Key
	// keyErr is set when history.keyFile names a path but the key
	// couldn't be loaded from it (unreadable, too short) -- nil in both
	// of the other two states, "no key configured" and "key loaded".
	// Existing only so a caller can tell those two apart (#1211): key
	// alone is nil in both, which is what let the warm-restart snapshot
	// startup line claim "no history.keyFile configured" for a key that
	// was configured but broken. See snapshotKeyState in snapshot.go.
	keyErr error
	// pastFirstStart is whether the stored schema -- the database's on
	// Postgres, the data directory's on files -- had already reached
	// persist.SchemaAuthSealed when the storage opened (#1202, ruling
	// 14). Until it has, the first start of v0.7.0, the auth stores and
	// the drop list seal a plaintext document they find; from then on
	// they refuse it. openStorage always sets it, to true when the
	// schema cannot be read (the fail-closed answer); see
	// finishSchemaUpgrade for when it moves.
	pastFirstStart bool
	// filesPastFirstStart is the same question asked of the data
	// directory alone, for the files a first move onto Postgres adopts:
	// a plaintext file is carried over only while it could still be one
	// an earlier keyless release left (see adopt).
	filesPastFirstStart bool
}

// openStorage connects to Postgres if configured, and applies the schema.
//
// Failure to reach a *configured* database is fatal, deliberately. The
// alternative is falling back to the JSON files, which would mean an
// operator who moved their accounts to Postgres for host-compromise
// separation silently gets a deployment reading a stale local file
// instead -- with different accounts, possibly a different admin, and no
// indication anything is wrong. Refusing to start is the honest failure.
func openStorage(ctx context.Context, cfg config.Config) (*storage, error) {
	log := logging.New("storage")
	s := &storage{log: log}

	// #853: the same key event history is encrypted under also covers the
	// file-backed state store and (see main.go) the warm-restart
	// snapshots. Loaded here, once, rather than by each caller, since
	// openStorage is the one chokepoint every persisted store and every
	// storage-touching CLI command goes through (run(), -backup/-restore,
	// -recover-admin-account and friends).
	//
	// The server, setup-only mode and the four commands that open the
	// account stores refuse to go on without a key before they get here
	// (requireKey, #1202 ruling 2), so the no-key branch below is reached
	// only by the commands that never touch those stores: -backup,
	// -restore, -migrate-data.
	key, keyErr := retention.LoadKey(cfg.History.KeyFile)
	switch {
	case keyErr == retention.ErrNoKey:
		log.Info("no history.keyFile configured -- every JSON-file-backed store and the warm-restart snapshots are memory-only; there is no unencrypted mode (#853)")
	case keyErr != nil:
		s.keyErr = keyErr
		log.Warn(fmt.Sprintf("history.keyFile is set but could not be used (%v) -- the state store and warm-restart snapshots run exactly as if no key were configured (memory-only)", keyErr), "keyFile", cfg.History.KeyFile, "err", keyErr)
	default:
		s.key = key
		if key.GroupOrWorldReadable {
			log.Warn("the retention key file is readable by more than its owner -- tighten it to 0600 if you can", "keyFile", cfg.History.KeyFile)
		}
		log.Info("history.keyFile is configured -- the file-backed state store and warm-restart snapshots are encrypted under it (#853)")
	}

	// Unreadable reads as "already sealed": a plaintext document is
	// then refused rather than sealed, the fail-closed answer. The
	// server never gets here with an unreadable schema document --
	// upgradeDataDirSchema has refused it.
	stored, _, err := persist.ReadFileSchema(dataDir(cfg))
	s.filesPastFirstStart = err != nil || stored >= persist.SchemaAuthSealed

	if cfg.Postgres.DSNFile == "" {
		if err := refuseIfPostgresAdopted(cfg); err != nil {
			return nil, err
		}
		s.pastFirstStart = s.filesPastFirstStart
		log.Info("persisting to JSON files under the configured store paths")
		return s, nil
	}

	dsn, err := readDSNFile(cfg.Postgres.DSNFile)
	if err != nil {
		return nil, err
	}

	pool, err := persist.OpenPool(ctx, dsn)
	if err != nil {
		// persist.OpenPool already strips the DSN out of its errors --
		// the string carries a password.
		return nil, fmt.Errorf("postgres: %w", err)
	}
	// Up to the schema below SchemaAuthSealed only: that one is stamped
	// once the stores have opened sealed (finishSchemaUpgrade, ruling
	// 14), so what is stored now says whether this is the first start
	// of v0.7.0.
	dbSchema, err := pool.MigrateTo(ctx, persist.SchemaAuthSealed-1)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: applying schema: %w", err)
	}
	s.pastFirstStart = dbSchema >= persist.SchemaAuthSealed

	s.pool = pool
	s.cfg = cfg
	// Whether this deployment had already run on Postgres *before* this
	// boot. It is what stops the one-time JSON adoption running a second
	// time -- see backendFor, and #294 item 1 for the failure that
	// allowed.
	//
	// Read before marking, obviously, since marking makes it true.
	s.adoptedBefore = postgresAlreadyAdopted(cfg)

	// Recorded on every Postgres boot, not only the migrating one: a
	// deployment that started on Postgres has no JSON files, so removing
	// the DSN later would silently present a first-run setup screen and
	// hand admin to whoever loaded it. Same fail-open, different route.
	if err := markPostgresAdopted(cfg); err != nil {
		pool.Close()
		return nil, err
	}
	log.Info(fmt.Sprintf("persisting to %s", pool.Describe()))
	log.Warn("this only separates your data from this host if Postgres is on a DIFFERENT machine, " +
		"reachable over a restricted network path -- a database on this same host exposes its credential " +
		"to exactly the compromise it is meant to survive")
	return s, nil
}

// readDSNFile reads and validates the connection string.
//
// From a file rather than config.yaml or a flag: a DSN carries a
// password, and a password in config.yaml ends up in whatever backs that
// file up, while a password in argv is visible to every process on the
// host and in `docker inspect`.
func readDSNFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("postgres: reading DSN file %s: %w", path, err)
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return "", fmt.Errorf("postgres: DSN file %s is empty", path)
	}
	return dsn, nil
}

// backendFor returns where the named store should persist, and adopts
// any existing JSON file into an empty Postgres store on the way.
//
// filePath is still passed when Postgres is configured, because it is
// the migration source -- and because turning Postgres off again has to
// come back on that file (see persist.AdoptFile, which never deletes it).
//
// #853: on the JSON-file path, whether name gets a working backend at all
// also depends on s.key -- "every file the file backend writes" is the
// rule the issue settled on. With no key every store returns (nil, nil),
// the same "persistence not configured" signal already used for
// memory-only stores (an empty filePath does the same). The exceptions
// #853 rule 6 once made for the hashed stores and the drop list are gone
// (#1202, rulings 2 and 13): no install runs without a key now, so with
// one every store here is sealed.
//
// The auth stores do not come through here directly: see authBackendFor.
func (s *storage) backendFor(ctx context.Context, name, filePath string) (persist.Backend, error) {
	if s.pool == nil {
		if filePath == "" {
			return nil, nil // this store's persistence is switched off
		}
		if s.key == nil {
			return nil, nil // #853: no key, no storage -- this store is memory-only
		}
		b := persist.NewEncryptedFileBackend(filePath, s.key)
		// The drop list is the one store besides the auth stores that a
		// keyless install kept, in the clear (#853's 2026-09-18
		// addendum, now ended): it comes across the same way, on the
		// same first start only (#1202 review R1, ruling 14).
		if name == dropListStore && !s.pastFirstStart {
			b = b.WithSealInPlace()
		}
		return b, nil
	}

	b := persist.NewPostgresBackend(s.pool, name)
	if err := s.adopt(ctx, b, filePath); err != nil {
		return nil, err
	}
	return b, nil
}

// dropListStore is the drop list's store name (main.go's backendFor
// call), named here for backendFor's one exception.
const dropListStore = "droplist"

// adopt performs the one-time JSON adoption into dst, a Postgres store
// that is empty unless this deployment has run on Postgres before. dst
// is what the store reads and writes through -- the bare row, or for an
// auth store gauntlet's sealed wrapper -- so the document arrives as the
// store reads it (#1202 review R2).
func (s *storage) adopt(ctx context.Context, dst persist.Backend, filePath string) error {

	// Adoption is a one-time migration, and only the boot that first
	// moves this deployment onto Postgres is allowed to perform it.
	//
	// Without this it ran on every boot, guarded only by "this store has
	// no document yet" -- so restoring the database to a snapshot from
	// before a store was populated, with the original JSON still on the
	// data volume, copied it straight back in. Deleted accounts and
	// their password hashes returned, with an Info log line as the only
	// signal (#294 item 1).
	//
	// The marker lives beside the JSON files rather than in the
	// database, and that placement is what makes it work here. #294
	// suggested recording the adoption *in* the database so a rollback
	// would take the marker with it -- but that is the wrong way round:
	// a rollback deep enough to lose the data is deep enough to lose a
	// marker stored alongside it, and mikroview would re-adopt exactly
	// as before. The guard has to survive the rollback it is guarding
	// against, so it has to sit on the other side of it.
	if s.adoptedBefore {
		s.reportUnadoptedFile(ctx, dst, filePath)
		return nil
	}

	// The files are sealed under the key, path bound, so each is opened
	// through its own file backend and its document handed to dst
	// (#1202 review R2) -- not copied verbatim, which would leave the
	// row holding an envelope nothing reads. A plaintext file is carried
	// over only while the data directory is below schema 4, as opening
	// it in place would be (ruling 14). With no key the file stores
	// were never written, so there is nothing sealed to open.
	adopt := persist.AdoptSealedFileOnly
	switch {
	case s.key == nil:
		adopt = func(ctx context.Context, src string, _ *retention.Key, dst persist.Backend) (persist.MigrateResult, error) {
			return persist.AdoptFile(ctx, src, dst)
		}
	case !s.filesPastFirstStart:
		adopt = persist.AdoptSealedFile
	}
	res, err := adopt(ctx, filePath, s.key, dst)
	if err != nil {
		return err
	}
	if res.Migrated {
		s.log.Info(fmt.Sprintf("migrated %s (%d bytes) into %s -- that file is no longer read, "+
			"and can be deleted once you are satisfied the move worked",
			res.FilePath, res.Bytes, dst.Describe()))
	}
	return nil
}

// The four auth stores, each sealed under the key on both backends
// (#1202, ruling 2). The name is the Postgres row and, there, the label
// the seal binds -- a row copied into another store's place fails to
// open rather than reading as that store (gauntlet ADR-0005).
const (
	authStoreAccounts     = "auth"
	authStoreTokens       = "tokens"
	authStoreRecoveryKeys = "recovery_keys"
	authStoreSignIns      = "signins"
)

// errAuthStoreNeedsKey is authBackendFor's refusal without a key. Not
// reached in practice: every caller has already been stopped by
// requireKey, which says how to mount one (CFG-0083).
var errAuthStoreNeedsKey = errors.New("the account stores are kept sealed under history.keyFile, and no key is loaded")

// authBackendFor is backendFor for the four auth stores: the same
// placement and the same one-time Postgres adoption, then sealed under
// the key, with whatever an install that ran without one left in the
// clear sealed in place on the first start of v0.7.0 (s.pastFirstStart,
// ruling 14) and refused after it.
//
//   - File backend: mikroview's own encrypted file backend, path bound as
//     every other store's, with WithSealInPlace while sealing in place
//     applies (ruling 11): a plaintext JSON file is sealed at the
//     version it was read at; anything else that will not open fails as
//     it always did.
//   - Postgres: gauntlet's persist.Encrypt around the row, labelled with
//     the store's name, MigratePlaintext while sealing in place applies.
//     The other stores stay plaintext on Postgres, as
//     docs/decisions/event-retention.md decided; these four are the
//     exception (ruling 11).
//
// nil, nil means the store's path is empty on the file backend -- its
// persistence is switched off, as for any other store.
func (s *storage) authBackendFor(ctx context.Context, name, filePath string) (gpersist.Backend, error) {
	if s.key == nil {
		return nil, errAuthStoreNeedsKey
	}
	if s.pool == nil {
		if filePath == "" {
			return nil, nil // this store's persistence is switched off
		}
		b := persist.NewEncryptedFileBackend(filePath, s.key)
		if !s.pastFirstStart {
			b = b.WithSealInPlace()
		}
		return b, nil
	}
	b, err := gpersist.Encrypt(persist.NewPostgresBackend(s.pool, name), s.key.Material(),
		gpersist.EncryptOptions{Label: name, MigratePlaintext: !s.pastFirstStart})
	if err != nil {
		return nil, err
	}
	// Adopted through the sealed wrapper, so the file's document is
	// sealed again under this store's label on the way in.
	if err := s.adopt(ctx, b, filePath); err != nil {
		return nil, err
	}
	return b, nil
}

// neverSealInPlace turns sealing in place off for this process,
// whatever the stored schema: setup-only mode's rule (configeditor.go).
func (s *storage) neverSealInPlace() {
	s.pastFirstStart = true
	s.filesPastFirstStart = true
}

// requireKey is the no-key refusal (#1202, rulings 2 and 12), made
// before anything touches the data directory: the server, setup-only
// mode and the four commands that open the account stores all call it
// first, and all say the same thing (config.KeyRequired, CFG-0083). ok is
// false when it refused; it has logged why on log.
func requireKey(log *slog.Logger, cfg config.Config, what string) bool {
	_, err := retention.LoadKey(cfg.History.KeyFile)
	if err == nil {
		return true
	}
	p := config.KeyRequired(cfg.History.KeyFile, errors.Is(err, retention.ErrNoKey), err)
	log.Error(fmt.Sprintf("refusing to %s:\n%s", what, config.Report([]config.Problem{p})))
	return false
}

// reportUnadoptedFile says something about a JSON file that will never
// be adopted, and how loudly depends on whether that is expected.
//
// Ordinarily it is: the file is what this deployment migrated from, kept
// because turning Postgres off again would need it, and saying so once
// per boot tells an operator it is safe to delete.
//
// The loud case is the one worth having this function for. The marker is
// written when Postgres opens, before any store is adopted, so a process
// that dies part-way through its first migration comes back with the
// marker set and the remaining stores unadopted. Those stores are then
// empty in Postgres while their file still holds the data, and nothing
// would ever move it -- which without this would be silent, and would
// look exactly like a store that was always empty.
//
// Not fixed by adopting anyway: this cannot distinguish that case from a
// rolled-back database, which is the whole point of the guard. Telling
// the operator precisely what happened and what to do is the honest
// answer where guessing is not.
func (s *storage) reportUnadoptedFile(ctx context.Context, b persist.Backend, filePath string) {
	if filePath == "" {
		return
	}
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		return
	}

	snap, err := b.Load(ctx)
	if err != nil || snap.Exists {
		// Either the store has data (the ordinary case -- this file is
		// the history it was migrated from) or the database could not be
		// read, which the caller reports on its own terms.
		s.log.Info(fmt.Sprintf("%s is left over from before this deployment moved to Postgres and is not read -- "+
			"delete it when you are ready; it will never be adopted again", filePath))
		return
	}

	s.log.Warn(fmt.Sprintf("%s holds data but %s is empty, and this deployment has already adopted Postgres so it will "+
		"not be migrated. This usually means a first migration was interrupted part-way. MikroView will not adopt it "+
		"automatically, because it cannot tell that apart from a database restored to an older snapshot -- adopting "+
		"the wrong one of those brings deleted accounts back. To migrate it deliberately: stop MikroView, remove %s, "+
		"start once to adopt, and the marker is rewritten",
		filePath, b.Describe(), markerPath(s.cfg)))
}

func (s *storage) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// upgradeDataDirSchema runs the data directory's pending migrations and
// records the schema version they left it at, before anything opens a
// store (#1238).
//
// Two failures, treated differently because only one of them means the
// data is not what this build thinks it is:
//
//   - Data written by a newer build (*persist.SchemaTooNewError) stops
//     startup. Nothing was written on the way to that answer, and
//     continuing would rewrite every document in this build's older
//     shapes -- see docs/upgrades.md, "Going back".
//   - Failing to *record* a schema version that needed no migration is a
//     warning, not a refusal: nothing on disk changed, and the next
//     start tries again. It means an unwritable data directory in
//     practice, which checkStoresUsable names properly a moment later
//     with the ownership and the command to fix it.
//
// Anything else -- a migration that failed, or one that landed and could
// not be recorded -- stops startup too. A migration whose number was
// never stamped would run a second time against data it has already
// changed.
//
// It stops below persist.SchemaAuthSealed: that number is stamped by
// finishSchemaUpgrade, once every auth store and the drop list have
// opened sealed (#1202, ruling 14).
func upgradeDataDirSchema(cfg config.Config) error {
	return migrateDataDirSchema(cfg, persist.SchemaAuthSealed-1)
}

func migrateDataDirSchema(cfg config.Config, to int64) error {
	dir := dataDir(cfg)
	_, err := persist.MigrateFileSchemaTo(context.Background(), dir, version, to)
	if err == nil {
		return nil
	}
	if errors.Is(err, persist.ErrSchemaNotStamped) {
		logging.New("schema").Warn(fmt.Sprintf("%v -- upgrades from this point will not be able to tell "+
			"which build wrote this data until %s is writable", err, dir))
		return nil
	}
	return err
}

// finishSchemaUpgrade stamps persist.SchemaAuthSealed, and anything this
// build knows above it, on the database and the data directory (#1202,
// ruling 14). The server calls it once every auth store and the drop
// list have opened -- sealed, or sealed in place on this start -- and
// never before: a start that stopped part way leaves the schema below
// it, so the next start still seals what is left. From the stamp on, a
// plaintext document is refused.
//
// Setup-only mode and the command-line tools never call it; only a
// normal start moves the schema.
func (s *storage) finishSchemaUpgrade(ctx context.Context, cfg config.Config) error {
	if s.pool != nil {
		if _, err := s.pool.MigrateTo(ctx, persist.CurrentSchema()); err != nil {
			return fmt.Errorf("postgres: applying schema: %w", err)
		}
	}
	return migrateDataDirSchema(cfg, persist.CurrentSchema())
}

// postgresAdoptedMarker records that this deployment has run on
// Postgres. Moving to Postgres is a one-way, deliberate choice; this is
// what makes that real rather than advisory.
const postgresAdoptedMarker = "postgres-adopted"

// markerPath puts the marker beside the JSON stores, because that is
// where it has to be readable from: the check runs when Postgres is NOT
// configured, so it cannot live in the database it is describing.
// dataDir is where mikroview's own files live: the account store, the
// Postgres adoption marker, and the recovery-key hand-over file. Derived
// from the account store rather than configured separately, so those
// files cannot drift onto different volumes with different protections.
func dataDir(cfg config.Config) string {
	if cfg.Auth.StorePath != "" {
		return filepath.Dir(cfg.Auth.StorePath)
	}
	return config.DefaultDataDir
}

func markerPath(cfg config.Config) string {
	return filepath.Join(dataDir(cfg), postgresAdoptedMarker)
}

// postgresAlreadyAdopted reports whether this deployment has run on
// Postgres before now. Distinct from markPostgresAdopted's own
// idempotence check because the answer is needed *before* marking, and
// the two questions are different: "has this happened before" versus
// "make sure it is recorded".
func postgresAlreadyAdopted(cfg config.Config) bool {
	_, err := os.Stat(markerPath(cfg))
	return err == nil
}

// markPostgresAdopted records the choice, idempotently.
//
// A failure to write is fatal rather than a warning. The marker is the
// only thing standing between "someone removed the DSN" and "mikroview
// silently came up on stale or empty local state" -- a guard that
// quietly failed to arm is worse than no guard, because the operator
// believes it is there.
func markPostgresAdopted(cfg config.Config) error {
	path := markerPath(cfg)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("postgres: recording the storage choice at %s: %w", path, err)
	}
	body := "This deployment stores its state in Postgres.\n\n" +
		"Moving to Postgres is one-way. MikroView will refuse to start on the JSON\n" +
		"files while this file exists, because coming back up on months-old local\n" +
		"accounts -- with a different admin, or none -- is worse than not starting.\n\n" +
		"Back up the database, not these files. See docs/configuration.md, \"Postgres\".\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("postgres: recording the storage choice at %s: %w", path, err)
	}
	return nil
}

// refuseIfPostgresAdopted stops a deployment that has used Postgres from
// starting on the JSON files.
//
// The realistic trigger is not someone deciding to switch back. It is a
// mounted secret that failed to appear, a typo in an env var, or a
// compose file edited in a hurry -- and the failure it prevents is the
// worst kind: mikroview starts, looks healthy, and serves stale
// accounts, or an empty store that presents the first-run setup screen
// to whoever reaches it first.
//
// Consistent with a configured-but-unreachable database being fatal.
// Same failure, reached by a different route, so it gets the same
// answer.
func refuseIfPostgresAdopted(cfg config.Config) error {
	path := markerPath(cfg)
	if _, err := os.Stat(path); err != nil {
		return nil // never adopted; JSON is this deployment's real backend
	}
	return fmt.Errorf(
		"this deployment stores its state in Postgres, but no postgres.dsnFile is configured -- "+
			"refusing to start on the JSON files, which are stale or absent. "+
			"Moving to Postgres is one-way and this is not a supported way back. "+
			"Restore the database connection (check the DSN file is mounted and readable). "+
			"If you genuinely intend to abandon the database and its data, remove %s and restart, "+
			"understanding that accounts, flags and settings all come from whatever is left on local disk",
		path)
}
