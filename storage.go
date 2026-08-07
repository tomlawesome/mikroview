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
func (s *storage) backendFor(ctx context.Context, name, filePath string) (persist.Backend, error) {
	if s.pool == nil {
		if filePath == "" {
			return nil, nil // this store's persistence is switched off
		}
		return persist.NewFileBackend(filePath), nil
	}

	b := persist.NewPostgresBackend(s.pool, name)
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
