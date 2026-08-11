// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tomlawesome/mikroview/internal/logging"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// schemaLog is deliberately its own component rather than folded into
// the caller's logging.
//
// A schema change is one of the few things that happens during a
// container upgrade, without anyone asking for it, that can leave the
// database in a state the previous image doesn't understand. When
// something breaks after an upgrade, "what changed in the database, and
// when" is the first question -- so it gets its own clearly labelled
// lines rather than being inferred from a version number nobody printed.
var schemaLog = logging.New("schema")

// Every statement in this package is a compile-time constant with $n
// bound parameters. Nothing is concatenated, formatted, or built from
// caller input -- see docs/decisions/postgres-backend.md §7, and the
// scoped exemption this package carries in the repo-root injection-sink
// guard, which exists so this property has to be re-argued rather than
// silently eroded.
const (
	sqlLoadBlob = `SELECT payload, version FROM store_blob WHERE name = $1`

	// Deliberately not `SELECT payload, version` -- see
	// PostgresBackend.Version. The whole point is not to move the
	// document over the wire just to find out whether it changed.
	sqlLoadBlobVersion = `SELECT version FROM store_blob WHERE name = $1`

	sqlInsertBlob = `INSERT INTO store_blob (name, payload, version, updated_at)
	                 VALUES ($1, $2, 1, now())
	                 ON CONFLICT (name) DO NOTHING`

	sqlUpdateBlob = `UPDATE store_blob
	                 SET payload = $2, version = version + 1, updated_at = now()
	                 WHERE name = $1 AND version = $3`

	sqlEnsureSchemaTable = `CREATE TABLE IF NOT EXISTS schema_version (
	                            version     bigint PRIMARY KEY,
	                            applied_at  timestamptz NOT NULL DEFAULT now()
	                        )`

	sqlAppliedVersions = `SELECT version FROM schema_version ORDER BY version`

	sqlRecordVersion = `INSERT INTO schema_version (version) VALUES ($1)`

	// Serializes concurrent migration runs across processes. The constant
	// is arbitrary but must never change: it is the shared name two
	// mikroview instances starting simultaneously agree on.
	sqlAdvisoryLock   = `SELECT pg_advisory_lock($1)`
	sqlAdvisoryUnlock = `SELECT pg_advisory_unlock($1)`

	migrationLockID = 8_291_131 // 8291 = Winbox, 131 = this issue
)

// Pool is a connection pool plus the schema-migration entry point. One
// per process; the per-store backends share it.
type Pool struct {
	pool *pgxpool.Pool
	// safeDesc is the DSN with any password removed, for logs. The DSN
	// itself is never retained beyond connecting.
	safeDesc string
}

// ErrInsecureSSLMode is returned by OpenPool for a DSN that would send
// credentials and account data over an unencrypted connection.
var ErrInsecureSSLMode = errors.New(
	"persist: postgres sslmode must be require, verify-ca or verify-full -- " +
		"this connection carries password hashes and a database credential across a network")

// OpenPool connects and verifies the connection.
//
// TLS is required and *enforced*, not requested: a DSN asking for
// sslmode=disable/allow/prefer is refused rather than quietly upgraded,
// because silently doing something other than what the operator wrote
// hides a misconfiguration they need to see. Note that `require`
// encrypts without verifying the server's certificate -- `verify-full`
// is what actually stops an active attacker on the path, and is what
// the documentation recommends.
func OpenPool(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// Deliberately not wrapping err: pgx includes the DSN in some
		// parse errors, and the DSN carries the password.
		return nil, errors.New("persist: postgres DSN is not valid")
	}
	if err := requireTLS(cfg); err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("persist: connecting to postgres: %w", redact(err, dsn))
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("persist: postgres did not respond: %w", redact(err, dsn))
	}

	return &Pool{
		pool:     pool,
		safeDesc: fmt.Sprintf("postgres %s/%s", cfg.ConnConfig.Host, cfg.ConnConfig.Database),
	}, nil
}

// requireTLS refuses any configuration that could end up sending the
// database password and mikroview's accounts document in the clear.
//
// It inspects what pgx will actually *do* rather than parsing sslmode
// out of the DSN, because the mode can also arrive via PGSSLMODE or a
// service file, and because the interesting case isn't the string.
//
// Measured, per mode, on pgx v5:
//
//	disable      primaryTLS=false  fallbacks=0  plaintext fallback=no
//	allow        primaryTLS=false  fallbacks=1  plaintext fallback=no
//	prefer       primaryTLS=true   fallbacks=1  plaintext fallback=YES
//	require      primaryTLS=true   fallbacks=0  plaintext fallback=no
//	verify-full  primaryTLS=true   fallbacks=0  plaintext fallback=no
//
// `prefer` is the case that makes the fallback check necessary rather
// than belt-and-braces: it presents a TLS config, so "does the primary
// have TLS" says yes, and then it quietly reconnects in plaintext if the
// server declines. A check that only looked at the primary would pass
// the one mode that silently downgrades.
func requireTLS(cfg *pgxpool.Config) error {
	if cfg.ConnConfig.TLSConfig == nil {
		return ErrInsecureSSLMode
	}
	for _, fb := range cfg.ConnConfig.Fallbacks {
		if fb.TLSConfig == nil {
			return ErrInsecureSSLMode
		}
	}
	return nil
}

// redact strips the DSN out of an error, in case the driver embedded it.
// A password reaching a log file is a credential leak into whatever
// collects those logs.
func redact(err error, dsn string) error {
	msg := err.Error()
	if dsn != "" && strings.Contains(msg, dsn) {
		msg = strings.ReplaceAll(msg, dsn, "<dsn>")
	}
	return errors.New(msg)
}

// Raw returns the underlying connection pool, for a backend whose data
// doesn't fit store_blob's document shape (see docs/decisions/
// postgres-backend.md §1a) -- currently internal/matchlog's Postgres
// backend, which needs a genuinely indexed, queryable table of its own.
// Such a backend still shares this Pool -- one connection pool per
// process, one DSN, one TLS/migration story -- it just runs its own SQL
// against its own table instead of going through PostgresBackend's blob
// contract.
func (p *Pool) Raw() *pgxpool.Pool { return p.pool }

func (p *Pool) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func (p *Pool) Describe() string { return p.safeDesc }

// Migrate applies any embedded migrations this database hasn't seen, and
// says so in the log.
//
// Each runs inside its own transaction together with the row recording
// it, so a migration that fails part-way leaves no trace and no
// half-applied schema. An advisory lock serializes the whole run, so two
// instances starting at once don't both try.
//
// Every path logs. An up-to-date database says which version it is on;
// an upgrade names each migration *before* it runs and again with its
// duration afterwards, so a migration that hangs or crashes the process
// is identifiable from the last line written rather than by elimination.
func (p *Pool) Migrate(ctx context.Context) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("persist: acquiring migration connection: %w", err)
	}
	defer conn.Release()

	// Explicitly released. A session-level advisory lock is NOT dropped
	// by returning the connection to the pool -- pgxpool reuses the
	// session as-is, so the lock would outlive this call and every
	// subsequent Migrate in the process would block on it forever. That
	// is not hypothetical: it deadlocked the concurrency test on the
	// first run, which is why the unlock is here and this comment says
	// so rather than repeating the assumption that caused it.
	//
	// pg_advisory_xact_lock would self-release, but the lock has to span
	// several transactions (one per migration), so it can't live inside
	// any one of them.
	if _, err := conn.Exec(ctx, sqlAdvisoryLock, int64(migrationLockID)); err != nil {
		return fmt.Errorf("persist: taking migration lock: %w", err)
	}
	defer func() {
		// Best-effort: a failure here means the connection is already
		// broken, and a broken connection's session locks are released
		// by the server when it notices.
		_, _ = conn.Exec(context.WithoutCancel(ctx), sqlAdvisoryUnlock, int64(migrationLockID))
	}()
	if _, err := conn.Exec(ctx, sqlEnsureSchemaTable); err != nil {
		return fmt.Errorf("persist: creating schema_version: %w", err)
	}

	applied := map[int64]bool{}
	rows, err := conn.Query(ctx, sqlAppliedVersions)
	if err != nil {
		return fmt.Errorf("persist: reading schema_version: %w", err)
	}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("persist: reading schema_version: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("persist: reading schema_version: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	startVersion := highestVersion(applied)
	pending := make([]migration, 0, len(migrations))
	for _, m := range migrations {
		if !applied[m.version] {
			pending = append(pending, m)
		}
	}

	if len(pending) == 0 {
		schemaLog.Info(fmt.Sprintf("database schema is up to date at version %d (%s)",
			startVersion, p.Describe()))
		return nil
	}

	// Announced before the first one runs, so an upgrade that stalls is
	// visibly an upgrade rather than a mystery hang at boot.
	schemaLog.Info(fmt.Sprintf("database schema is at version %d, %d migration(s) to apply -- updating %s",
		startVersion, len(pending), p.Describe()))

	for _, m := range pending {
		schemaLog.Info(fmt.Sprintf("applying %s (version %d)", m.name, m.version))
		began := time.Now()

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("persist: starting migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			schemaLog.Error(fmt.Sprintf("migration %s (version %d) failed and was rolled back -- "+
				"the schema is unchanged, still at version %d", m.name, m.version, startVersion))
			return fmt.Errorf("persist: applying migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(ctx, sqlRecordVersion, m.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("persist: recording migration %d: %w", m.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("persist: committing migration %d: %w", m.version, err)
		}

		schemaLog.Info(fmt.Sprintf("applied %s in %s", m.name, time.Since(began).Round(time.Millisecond)))
	}

	endVersion := pending[len(pending)-1].version
	schemaLog.Warn(fmt.Sprintf("database schema updated from version %d to version %d -- "+
		"an older mikroview image may no longer read this database correctly",
		startVersion, endVersion))
	return nil
}

func highestVersion(applied map[int64]bool) int64 {
	var highest int64
	for v := range applied {
		if v > highest {
			highest = v
		}
	}
	return highest
}

type migration struct {
	version int64
	name    string
	sql     string
}

// loadMigrations reads the embedded .sql files, ordered by the numeric
// prefix rather than by filename string, so 0010 sorts after 0009
// instead of between 0001 and 0002.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("persist: reading embedded migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var version int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil || version <= 0 {
			return nil, fmt.Errorf("persist: migration %q must start with a positive version number", e.Name())
		}
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("persist: reading migration %q: %w", e.Name(), err)
		}
		out = append(out, migration{version: version, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })

	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("persist: two migrations share version %d (%s, %s)",
				out[i].version, out[i-1].name, out[i].name)
		}
	}
	return out, nil
}

// PostgresBackend is one store's document in the store_blob table.
type PostgresBackend struct {
	pool *Pool
	name string
}

// NewPostgresBackend returns a Backend for the named store. name is a
// fixed identifier chosen in code (see the migration's comment for the
// set), never anything a user supplies -- it is a parameter to every
// statement regardless.
func NewPostgresBackend(pool *Pool, name string) *PostgresBackend {
	return &PostgresBackend{pool: pool, name: name}
}

func (b *PostgresBackend) Describe() string {
	return fmt.Sprintf("%s store %q", b.pool.Describe(), b.name)
}

// Close is a no-op: the pool is shared and owned by the caller that
// opened it, not by each store's backend.
func (b *PostgresBackend) Close() error { return nil }

func (b *PostgresBackend) Load(ctx context.Context) (Snapshot, error) {
	var payload string
	var version int64
	err := b.pool.pool.QueryRow(ctx, sqlLoadBlob, b.name).Scan(&payload, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, nil
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("persist: loading %q: %w", b.name, err)
	}
	return Snapshot{Payload: []byte(payload), Version: version, Exists: true}, nil
}

// Version implements VersionReader: it answers "has this document
// changed?" without transferring the document.
//
// This matters because internal/auth checks staleness on every
// authenticated request, and the accounts payload grows with the number
// of accounts. Loading it whole to compare one integer meant every
// request moved the entire accounts document -- password hashes
// included -- across the network, per request.
func (b *PostgresBackend) Version(ctx context.Context) (int64, bool, error) {
	var version int64
	err := b.pool.pool.QueryRow(ctx, sqlLoadBlobVersion, b.name).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("persist: reading version of %q: %w", b.name, err)
	}
	return version, true, nil
}

func (b *PostgresBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	if expect == 0 {
		// Create. ON CONFLICT DO NOTHING makes the race explicit: zero
		// rows affected means somebody else created it first, which is a
		// conflict, not a success.
		tag, err := b.pool.pool.Exec(ctx, sqlInsertBlob, b.name, string(payload))
		if err != nil {
			return 0, fmt.Errorf("persist: creating %q: %w", b.name, err)
		}
		if tag.RowsAffected() == 0 {
			return 0, ErrConflict
		}
		return 1, nil
	}

	tag, err := b.pool.pool.Exec(ctx, sqlUpdateBlob, b.name, string(payload), expect)
	if err != nil {
		return 0, fmt.Errorf("persist: saving %q: %w", b.name, err)
	}
	if tag.RowsAffected() == 0 {
		// Either the row is gone or its version moved on. Both mean the
		// caller's copy is stale.
		return 0, ErrConflict
	}
	return expect + 1, nil
}
