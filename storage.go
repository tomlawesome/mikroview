// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	// amendment). nil means no key is configured, which is the default
	// install: backendFor then refuses to persist most JSON-file stores
	// at all rather than writing them in the clear. It has nothing to do
	// with history.enabled, which only switches the *event log* on top
	// of this same key.
	key *retention.Key
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
	key, keyErr := retention.LoadKey(cfg.History.KeyFile)
	switch {
	case keyErr == retention.ErrNoKey:
		log.Info("no history.keyFile configured -- every JSON-file-backed store except accounts, tokens and recovery keys (flags, entities, watchlist, definitions and the rest), and the warm-restart snapshots, are memory-only and are lost on every restart; there is no unencrypted mode to fall back to for those (#853). Accounts, tokens and recovery keys keep persisting in plain JSON because they hold only one-way hashes (#853 rule 6)")
	case keyErr != nil:
		log.Warn("history.keyFile is set but could not be used -- the state store and warm-restart snapshots run exactly as if no key were configured (memory-only)", "keyFile", cfg.History.KeyFile, "err", keyErr)
	default:
		s.key = key
		if key.GroupOrWorldReadable {
			log.Warn("the retention key file is readable by more than its owner -- tighten it to 0600 if you can", "keyFile", cfg.History.KeyFile)
		}
		log.Info("history.keyFile is configured -- the file-backed state store and warm-restart snapshots are encrypted under it (#853)")
	}

	if cfg.Postgres.DSNFile == "" {
		if err := refuseIfPostgresAdopted(cfg); err != nil {
			return nil, err
		}
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
	if err := pool.Migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: applying schema: %w", err)
	}

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

// hashedStores lists the JSON-file stores exempt from "no key, no
// storage" (see backendFor): they hold only one-way hashes -- argon2id
// password hashes (auth), hashed API/ingest tokens (tokens) and hashed
// recovery keys (recovery_keys) -- so nothing in them can be decrypted
// even if the plain file leaked. Owner decision, #853 rule 6,
// 2026-09-05: keep persisting these without a key, exactly as before
// this issue's change, accepting that the plain file still discloses
// usernames and roles.
var hashedStores = map[string]bool{
	"auth":          true,
	"tokens":        true,
	"recovery_keys": true,
}

// backendFor returns where the named store should persist, and adopts
// any existing JSON file into an empty Postgres store on the way.
//
// filePath is still passed when Postgres is configured, because it is
// the migration source -- and because turning Postgres off again has to
// come back on that file (see persist.AdoptFile, which never deletes it).
//
// #853: on the JSON-file path, whether name gets a working backend at all
// now also depends on s.key -- "every file the file backend writes" is
// the rule the issue settled on, with one exception decided afterwards
// (rule 6, 2026-09-05): hashedStores above keep persisting in the clear
// with no key, because a one-way hash gains nothing from encryption.
// Every other store returns (nil, nil) with no key, the same
// "persistence not configured" signal already used for memory-only
// stores (an empty filePath does the same today).
//
// openAuthStoreForCLI and openRecoveryStoreForCLI (main.go) both check
// for a nil backend here -- now only possible for auth and recovery_keys
// when filePath itself is empty -- and refuse loudly rather than
// silently handing a recovery command an empty in-memory store.
func (s *storage) backendFor(ctx context.Context, name, filePath string) (persist.Backend, error) {
	if s.pool == nil {
		if filePath == "" {
			return nil, nil // this store's persistence is switched off
		}
		if s.key == nil {
			if hashedStores[name] {
				return persist.NewFileBackend(filePath), nil // #853 rule 6: one-way hashes, no key needed
			}
			return nil, nil // #853: no key, no storage -- this store is memory-only
		}
		return persist.NewEncryptedFileBackend(filePath, s.key), nil
	}

	b := persist.NewPostgresBackend(s.pool, name)

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
		s.reportUnadoptedFile(ctx, b, filePath)
		return b, nil
	}

	res, err := persist.AdoptFile(ctx, filePath, b)
	if err != nil {
		return nil, err
	}
	if res.Migrated {
		s.log.Info(fmt.Sprintf("migrated %s (%d bytes) into %s -- that file is no longer read, "+
			"and can be deleted once you are satisfied the move worked",
			res.FilePath, res.Bytes, b.Describe()))
	}
	return b, nil
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
		"not be migrated. This usually means a first migration was interrupted part-way. mikroview will not adopt it "+
		"automatically, because it cannot tell that apart from a database restored to an older snapshot -- adopting "+
		"the wrong one of those brings deleted accounts back. To migrate it deliberately: stop mikroview, remove %s, "+
		"start once to adopt, and the marker is rewritten",
		filePath, b.Describe(), markerPath(s.cfg)))
}

func (s *storage) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
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
		"Moving to Postgres is one-way. mikroview will refuse to start on the JSON\n" +
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
