// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
