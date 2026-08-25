// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/store"
)

var pgLog = logging.New("matchlog")

// Every statement here is a compile-time constant with $n bound
// parameters -- see docs/decisions/postgres-backend.md §7 and the
// scoped injection-sink exemption this package carries for the same
// reasoning internal/persist's own Postgres backend does.
// persistTimeout bounds the write/count calls below (Append,
// purgeOnceRecovered, Stats) -- not Query, which serves an
// admin-initiated HTTP request rather than running on a producer
// goroutine. Append runs on the watchlist evaluator's own goroutine
// (internal/watchlist/evaluator.go), not the ingest goroutine itself,
// so an unresponsive Postgres connection here has a smaller blast
// radius than the same failure in internal/rules, internal/flags or
// internal/device -- the evaluator has its own bounded queue and drop
// log -- but it would still block that goroutine indefinitely under
// context.Background(), and Stats/purgeOnceRecovered have no such
// isolation at all. Same 5s reasoning as persist.SaveWithRetry's
// ingest-path callers: generous for a write this small, short enough
// that a genuinely stuck backend degrades to a logged failure instead
// of an indefinite hang.
const persistTimeout = 5 * time.Second

const (
	sqlCollapseUpdate = `UPDATE match_log SET last_seen = $2, count = count + 1 WHERE tuple_key = $1`

	sqlInsertRecord = `INSERT INTO match_log
	                    (id, entry_id, tuple_key, source_identity_key, source_mac, source_ip, dest_ip, port, event, first_seen, last_seen, count, provisional)
	                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, 1, $11)`

	sqlCount = `SELECT count(*) FROM match_log`

	sqlSelectMatches = `SELECT id, entry_id, source_mac, source_ip, dest_ip, port, event, first_seen, last_seen, count, provisional
	                     FROM match_log
	                     WHERE source_identity_key = $1
	                       AND last_seen >= $2
	                       AND ($3::timestamptz IS NULL OR first_seen < $3)
	                     ORDER BY last_seen DESC
	                     LIMIT $4`

	// Serves Recent: the same projection and the same window, with the
	// source_identity_key predicate dropped. Deliberately *not* served
	// by match_log_source_last_seen -- that index is keyed on the
	// identity first, so it cannot answer an unfiltered query at all.
	// The ordering and the LIMIT ride the existing plain
	// match_log_last_seen index (migration 0002, added there for the
	// retention purge's WHERE last_seen < $1): a btree scanned
	// backwards answers ORDER BY last_seen DESC LIMIT n by reading n
	// entries, so no new index is needed for this and none was added.
	// The optional first_seen bound is a filter on top of that scan,
	// same as it is on sqlSelectMatches.
	//
	// Verified rather than assumed, against a real postgres:18-alpine
	// holding 200,000 matches: EXPLAIN ANALYZE reports "Index Scan
	// Backward using match_log_last_seen", 6 shared buffers hit and
	// 0.17ms to return 100 rows. It reads the rows it returns and stops,
	// which is what makes this mode's cost a function of the limit
	// rather than of how much history the table holds.
	sqlSelectRecent = `SELECT id, entry_id, source_mac, source_ip, dest_ip, port, event, first_seen, last_seen, count, provisional
	                    FROM match_log
	                    WHERE last_seen >= $1
	                      AND ($2::timestamptz IS NULL OR first_seen < $2)
	                    ORDER BY last_seen DESC
	                    LIMIT $3`

	sqlPurgeOlderThan = `DELETE FROM match_log WHERE last_seen < $1`
)

// hashTupleKey turns Tuple.key's Go-internal collapsing key into
// something Postgres's text type can actually hold. Tuple.key joins its
// fields with a literal NUL byte -- fine as a Go map key (the file
// backend's own use) and in a JSON string, but Postgres's text type
// refuses to store 0x00 outright ("invalid byte sequence for encoding
// UTF8"), caught by this package's own Postgres integration tests
// against a real server, not by anything a synthetic payload would have
// exercised. Hashing sidesteps it without changing Tuple.key itself,
// which the file backend already depends on unchanged -- this key is
// never read back for display, only matched against, so losing
// human-readability costs nothing.
func hashTupleKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// PostgresStore is the match log's Postgres backend (#243 slice 6) -- a
// dedicated, indexed table rather than a row in store_blob. See
// docs/decisions/postgres-backend.md §1a for why that doesn't reopen the
// blob-table decision the rest of the Postgres backend follows.
//
// Unlike FileStore, this has no record-count ceiling: Postgres is meant
// to be "pragmatically unlimited" here (#243 section 3), bounded instead
// by age via RunPeriodicPurge. Stats().Capacity is always 0 -- see that
// type's own doc comment.
type PostgresStore struct {
	pool      *pgxpool.Pool
	retention time.Duration
}

// OpenPostgres wraps an already-migrated pool (see persist.Pool.Migrate,
// applying this package's own migration alongside every other store's).
// retention is how long a match is kept once its last activity ages
// past it (#243 section 3's "config knob, default 7 days") --
// RunPeriodicPurge is what actually enforces it; OpenPostgres itself
// never deletes anything.
func OpenPostgres(pool *persist.Pool, retention time.Duration) *PostgresStore {
	return &PostgresStore{pool: pool.Raw(), retention: retention}
}

// Append implements Store.
func (s *PostgresStore) Append(entryID string, tuple Tuple, event store.Event, t time.Time) error {
	return s.appendProvisional(entryID, tuple, event, t, false)
}

// AppendProvisional implements Store.
func (s *PostgresStore) AppendProvisional(entryID string, tuple Tuple, event store.Event, t time.Time, provisional bool) error {
	return s.appendProvisional(entryID, tuple, event, t, provisional)
}

func (s *PostgresStore) appendProvisional(entryID string, tuple Tuple, event store.Event, t time.Time, provisional bool) error {
	if tuple.Source.Empty() {
		return ErrEmptyIdentity
	}
	key := hashTupleKey(tuple.key(entryID))
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()

	// Collapse first: a repeat of an already-open tuple is the common
	// case once a watchlist entry has been live for a while, and this
	// never needs the capacity/insert path below. provisional is
	// ignored on this path -- it is fixed at creation, like first_seen.
	tag, err := s.pool.Exec(ctx, sqlCollapseUpdate, key, t)
	if err != nil {
		return fmt.Errorf("matchlog: collapsing a match: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("matchlog: encoding an event: %w", err)
	}
	_, err = s.pool.Exec(ctx, sqlInsertRecord,
		newID(), entryID, key, tuple.Source.identityKey(), tuple.Source.MAC, tuple.Source.IP,
		tuple.DestIP, tuple.Port, string(eventJSON), t, provisional)
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		// Lost a create race against another process between the update
		// above (0 rows) and this insert -- the row now certainly
		// exists, so finish as a collapse instead of surfacing an error
		// for what is, from the caller's point of view, a normal match.
		if _, err := s.pool.Exec(ctx, sqlCollapseUpdate, key, t); err != nil {
			return fmt.Errorf("matchlog: collapsing a match after a create race: %w", err)
		}
		return nil
	}
	return fmt.Errorf("matchlog: inserting a match: %w", err)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Query implements Store. Unlike the file backend, this never holds the
// whole log in memory or in a scan -- the range query and the sort/limit
// are all pushed down to the index Query's own doc comment on Store
// describes, which is the entire reason this table exists.
func (s *PostgresStore) Query(ctx context.Context, q Query, yield func(Record) bool) error {
	if q.Source.Empty() {
		return ErrEmptyIdentity
	}
	rows, err := s.pool.Query(ctx, sqlSelectMatches,
		q.Source.identityKey(), q.Since, nilIfZero(q.Until), clampLimit(q.Limit))
	if err != nil {
		return fmt.Errorf("matchlog: querying: %w", err)
	}
	defer rows.Close()
	return s.yieldRows(rows, yield)
}

// Recent implements Store. Bounded the same way Query is and by the same
// clamp -- the LIMIT reaches the server, so what Postgres materialises
// is bounded too, not just what this process retains. That is the
// difference between the two backends here: the file store has to read
// its whole log to know the ordering (see FileStore.Recent), while this
// one never reads past the rows it returns.
//
// No identity, and so no identity-key index -- see sqlSelectRecent for
// which index actually serves it, and RecentQuery for why this is a
// separate entry point rather than Query with an empty Source.
func (s *PostgresStore) Recent(ctx context.Context, q RecentQuery, yield func(Record) bool) error {
	rows, err := s.pool.Query(ctx, sqlSelectRecent,
		q.Since, nilIfZero(q.Until), clampLimit(q.Limit))
	if err != nil {
		return fmt.Errorf("matchlog: querying recent matches: %w", err)
	}
	defer rows.Close()
	return s.yieldRows(rows, yield)
}

// nilIfZero renders an optional upper bound the way both statements
// above expect it: a real NULL, so the `$n::timestamptz IS NULL OR ...`
// arm short-circuits, rather than the zero time.Time, which Postgres
// would happily compare against and which would exclude everything.
func nilIfZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// yieldRows decodes and delivers a result set from either statement
// above -- both project the same columns in the same order, on purpose,
// so the scanning and the timestamp normalisation below exist once.
func (s *PostgresStore) yieldRows(rows pgx.Rows, yield func(Record) bool) error {
	for rows.Next() {
		var r Record
		var eventJSON string
		if err := rows.Scan(&r.ID, &r.EntryID, &r.Tuple.Source.MAC, &r.Tuple.Source.IP, &r.Tuple.DestIP, &r.Tuple.Port,
			&eventJSON, &r.FirstSeen, &r.LastSeen, &r.Count, &r.Provisional); err != nil {
			return fmt.Errorf("matchlog: reading a match: %w", err)
		}
		// pgx v5's default TimestamptzCodec scans a timestamptz into
		// time.Local (no ScanLocation is set anywhere in this package,
		// unlike search_path's explicit pin in
		// internal/persist/postgres.go's pinSearchPath) -- so without
		// this, FirstSeen/LastSeen come back in the server process's
		// local zone while the embedded event.Event.Time (decoded from
		// eventJSON just below, via encoding/json) stays UTC, and the
		// file backend returns UTC for both. .UTC() here normalizes to
		// the "every other timestamp mikroview emits is UTC" invariant
		// without touching the connection's session state (issue #380
		// item 7).
		r.FirstSeen = r.FirstSeen.UTC()
		r.LastSeen = r.LastSeen.UTC()
		if err := json.Unmarshal([]byte(eventJSON), &r.Event); err != nil {
			return fmt.Errorf("matchlog: decoding a match's event: %w", err)
		}
		if !yield(r) {
			break
		}
	}
	return rows.Err()
}

// Stats implements Store. Capacity is always 0 and Full is always
// false -- this backend has no ceiling, see this type's own doc
// comment.
func (s *PostgresStore) Stats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	var count int
	if err := s.pool.QueryRow(ctx, sqlCount).Scan(&count); err != nil {
		pgLog.Warn(fmt.Sprintf("counting matches failed: %v", err))
		return Stats{}
	}
	return Stats{Count: count}
}

// Close is a no-op: the pool is shared and owned by whatever opened it
// (see main.go), not by this store -- same convention
// persist.PostgresBackend.Close follows.
func (s *PostgresStore) Close() error { return nil }

// RunPeriodicPurge deletes matches whose last activity is older than
// retention, on a fixed schedule, until ctx is done. Runs once
// immediately on entry, matching every other periodic-background-task
// convention in this codebase (see e.g. suggest.Store.RunPeriodicSync),
// so a freshly started mikroview doesn't carry months of pre-retention
// data until the first interval elapses.
func (s *PostgresStore) RunPeriodicPurge(ctx context.Context, interval time.Duration) {
	s.purgeOnceRecovered()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeOnceRecovered()
		}
	}
}

// purgeOnceRecovered isolates panic recovery to a single purge pass
// rather than RunPeriodicPurge's whole lifetime -- recover only unwinds
// as far as the nearest deferring function, so a defer in
// RunPeriodicPurge itself would end background purging for good on the
// first bad pass. Same reasoning as suggest.Store.syncOnceRecovered.
func (s *PostgresStore) purgeOnceRecovered() {
	defer logging.Recover(pgLog)
	cutoff := time.Now().Add(-s.retention)
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	tag, err := s.pool.Exec(ctx, sqlPurgeOlderThan, cutoff)
	if err != nil {
		pgLog.Warn(fmt.Sprintf("purging matches older than %s failed: %v", s.retention, err))
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		pgLog.Info(fmt.Sprintf("purged %d match(es) older than %s (retention)", n, s.retention))
	}
}
