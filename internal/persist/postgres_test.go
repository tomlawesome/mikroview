// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setupRetryAttempts and setupRetryBackoff bound the retry this harness
// allows on the *first* connection of a test, the "setup" step in
// newTestPool below. A Postgres service container can still be finishing
// its own startup when that first attempt runs, surfacing as a reset or
// "the database system is starting up" (SQLSTATE 57P03) -- see #702,
// observed twice on CI. The bound keeps a genuinely dead database
// failing within a second rather than hanging the job.
const (
	setupRetryAttempts = 5
	setupRetryBackoff  = 200 * time.Millisecond
)

// openPoolWithRetry calls connect up to attempts times, sleeping backoff
// between failures, and returns the first success or the last error.
//
// This wraps only the connect-and-verify step. It must never wrap a
// test's assertions: retrying there would turn a real regression into an
// intermittent pass instead of absorbing a slow-starting dependency.
func openPoolWithRetry(attempts int, backoff time.Duration, connect func() (*Pool, error)) (*Pool, error) {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		var p *Pool
		if p, err = connect(); err == nil {
			return p, nil
		}
		if attempt < attempts {
			time.Sleep(backoff)
		}
	}
	return nil, err
}

// These run against a real Postgres, not a mock. The properties worth
// testing here -- that a compare-and-swap actually rejects a stale
// write, that a failed migration leaves no trace, that concurrent
// creators can't both win -- are properties of the database. A mock
// would only assert that this code calls the functions it calls.
//
// Skipped, not failed, when MIKROVIEW_TEST_POSTGRES is unset, so
// `go test ./...` still works without one. CI supplies it via a service
// container.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres integration tests")
	}
	return dsn
}

// newTestPool gives each test its own schema, so tests can run in any
// order without seeing each other's rows.
//
// The schema is created on a throwaway pool first, then baked into the
// DSN of the pool actually returned. An earlier version set it via
// `SET search_path` plus `pool.Config()` — which silently did nothing,
// because pgxpool.Config() hands back a *copy*. Every test then shared
// the public schema and tripped over the previous test's rows, which
// showed up as a create returning ErrConflict.
func newTestPool(t *testing.T) *Pool {
	t.Helper()
	ctx := t.Context()

	schema := "test_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	dsn := testDSN(t)
	setup, err := openPoolWithRetry(setupRetryAttempts, setupRetryBackoff, func() (*Pool, error) {
		return OpenPool(ctx, dsn)
	})
	if err != nil {
		t.Fatalf("OpenPool (setup): %v", err)
	}
	// Identifiers can't be bound parameters, and this one is built from
	// t.Name() -- source code, not input. It is still constrained to
	// [a-z0-9_] below so the shape of this can't drift into something
	// that takes outside text.
	for _, r := range schema {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("test name produces an unsafe schema identifier: %q", schema)
		}
	}
	if _, err := setup.pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatalf("dropping schema: %v", err)
	}
	if _, err := setup.pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	setup.Close()

	p, err := OpenPool(ctx, withSchema(testDSN(t), schema))
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	t.Cleanup(p.Close)

	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := OpenPool(context.Background(), testDSN(t))
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})
	return p
}

// withSchema pins search_path for every connection the pool opens, by
// putting it in the DSN rather than issuing SET on one connection.
func withSchema(dsn, schema string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestPostgresRoundTrip(t *testing.T) {
	p := newTestPool(t)
	b := NewPostgresBackend(p, "auth")
	ctx := t.Context()

	snap, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("Load on empty: %v", err)
	}
	if snap.Exists {
		t.Fatal("a store that was never written reports Exists")
	}

	v, err := b.Save(ctx, []byte(`{"users":[]}`), 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v != 1 {
		t.Errorf("first version = %d, want 1", v)
	}

	snap, err = b.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(snap.Payload) != `{"users":[]}` || snap.Version != 1 || !snap.Exists {
		t.Errorf("round-trip mismatch: %+v", snap)
	}
}

// The bytes have to come back exactly as they went in -- that is why the
// column is text rather than jsonb. jsonb would reorder keys and rewrite
// numbers, which would make a JSON->Postgres migration merely equivalent
// instead of identical.
func TestPostgresPayloadIsByteIdentical(t *testing.T) {
	p := newTestPool(t)
	b := NewPostgresBackend(p, "auth")
	ctx := t.Context()

	// Deliberately awkward: key order that jsonb would sort, a number
	// jsonb would normalise, and a duplicate-looking unicode escape.
	original := []byte(`{"zebra":1,"alpha":2,"n":1.50,"s":"café","nested":{"b":1,"a":2}}`)
	if _, err := b.Save(ctx, original, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	snap, err := b.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(snap.Payload) != string(original) {
		t.Errorf("payload changed in storage:\n got %s\nwant %s", snap.Payload, original)
	}
}

// The lost-update window the file backend has: the server holds state in
// memory while a CLI command rewrites it, then the server persists over
// the top. Here that write must be refused.
func TestPostgresRefusesAStaleWrite(t *testing.T) {
	p := newTestPool(t)
	b := NewPostgresBackend(p, "auth")
	ctx := t.Context()

	v1, err := b.Save(ctx, []byte(`{"n":1}`), 0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Somebody else writes.
	if _, err := b.Save(ctx, []byte(`{"n":2}`), v1); err != nil {
		t.Fatalf("second write: %v", err)
	}
	// Our stale copy tries to write over it.
	if _, err := b.Save(ctx, []byte(`{"n":3}`), v1); err != ErrConflict {
		t.Fatalf("expected ErrConflict for a stale write, got %v", err)
	}

	snap, _ := b.Load(ctx)
	if string(snap.Payload) != `{"n":2}` {
		t.Errorf("the stale write landed anyway: %s", snap.Payload)
	}
}

func TestPostgresCreateRacesHaveOneWinner(t *testing.T) {
	p := newTestPool(t)
	b := NewPostgresBackend(p, "auth")
	ctx := t.Context()

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			_, results[i] = b.Save(ctx, []byte(fmt.Sprintf(`{"who":%d}`, i)), 0)
		}(i)
	}
	wg.Wait()

	won := 0
	for i, err := range results {
		switch err {
		case nil:
			won++
		case ErrConflict:
		default:
			t.Errorf("racer %d: unexpected error %v", i, err)
		}
	}
	if won != 1 {
		t.Errorf("%d creators succeeded, want exactly 1", won)
	}
}

// Save with expect != 0 against a store that doesn't exist is a
// conflict, not a create -- the caller believed there was something
// there, and there wasn't.
func TestPostgresUpdateOfAMissingStoreIsAConflict(t *testing.T) {
	p := newTestPool(t)
	b := NewPostgresBackend(p, "flags")
	if _, err := b.Save(t.Context(), []byte(`{}`), 7); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestPostgresStoresAreIndependent(t *testing.T) {
	p := newTestPool(t)
	ctx := t.Context()
	authB := NewPostgresBackend(p, "auth")
	flagsB := NewPostgresBackend(p, "flags")

	if _, err := authB.Save(ctx, []byte(`{"which":"auth"}`), 0); err != nil {
		t.Fatalf("auth save: %v", err)
	}
	if _, err := flagsB.Save(ctx, []byte(`{"which":"flags"}`), 0); err != nil {
		t.Fatalf("flags save: %v", err)
	}

	a, _ := authB.Load(ctx)
	f, _ := flagsB.Load(ctx)
	if string(a.Payload) != `{"which":"auth"}` || string(f.Payload) != `{"which":"flags"}` {
		t.Errorf("stores bled into each other: auth=%s flags=%s", a.Payload, f.Payload)
	}
}

// Migrate has to be safe to run on every boot, and safe to run twice at
// once (two instances starting together).
func TestMigrateIsIdempotentAndConcurrencySafe(t *testing.T) {
	p := newTestPool(t) // already migrated once
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	wg.Add(4)
	for i := 0; i < 4; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = p.Migrate(ctx)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Migrate %d: %v", i, err)
		}
	}

	// One row per embedded migration, not a hardcoded 1 -- the point
	// under test is that four concurrent Migrate calls against an
	// already-migrated database don't re-insert anything, which holds
	// regardless of how many migrations exist by the time this runs.
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}

	var count int
	if err := p.pool.QueryRow(ctx, "SELECT count(*) FROM schema_version").Scan(&count); err != nil {
		t.Fatalf("counting schema_version: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("schema_version has %d rows after repeated migration, want %d (one per migration)", count, len(migrations))
	}
}

// A DSN that would send the database password and the accounts document
// over plaintext is refused, not silently upgraded.
func TestOpenPoolRefusesPlaintextConnections(t *testing.T) {
	dsn := testDSN(t)
	for _, mode := range []string{"disable", "allow", "prefer"} {
		t.Run(mode, func(t *testing.T) {
			_, err := OpenPool(t.Context(), forceSSLMode(dsn, mode))
			if err != ErrInsecureSSLMode {
				t.Errorf("sslmode=%s: got %v, want ErrInsecureSSLMode", mode, err)
			}
		})
	}
}

// forceSSLMode replaces the mode rather than appending another one.
// Appending produced a DSN with two sslmode parameters, of which pgx
// honoured the first -- so the test appeared to exercise sslmode=disable
// while actually still connecting with require, and reported a
// connection failure instead of the refusal it was checking for.
func forceSSLMode(dsn, mode string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("sslmode", mode)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestLoadMigrationsOrdersNumericallyAndRejectsDuplicates(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].version <= ms[i-1].version {
			t.Errorf("migrations out of order: %s before %s", ms[i-1].name, ms[i].name)
		}
	}
}

// TestPinSearchPath guards the schema pinning without needing a
// database: pgxpool.ParseConfig only parses, so this holds on a machine
// with no Postgres to hand as well as in the Postgres job.
//
// The forced-to-public version of pinSearchPath broke every Postgres
// test's per-schema isolation and silently ignored an operator's own
// `?search_path=`. It went 41 commits unnoticed because the job running
// those tests was skipped on dev; that gate is gone now, so this is a
// second line rather than the only one.
func TestPinSearchPath(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			// Nothing asked for: pinned to public rather than left to
			// the role's or database's default, which is the shadowing
			// #285 was about.
			name: "no search_path in the DSN defaults to public",
			dsn:  "postgres://u:p@h:5432/db?sslmode=require",
			want: "public",
		},
		{
			name: "an explicit search_path is honoured",
			dsn:  "postgres://u:p@h:5432/db?sslmode=require&search_path=mikroview",
			want: "mikroview",
		},
		{
			// What the test helpers depend on for isolation.
			name: "a per-test schema is honoured",
			dsn:  "postgres://u:p@h:5432/db?sslmode=require&search_path=test_matchlog_x",
			want: "test_matchlog_x",
		},
		{
			name: "an empty search_path falls back to public rather than to nothing",
			dsn:  "postgres://u:p@h:5432/db?sslmode=require&search_path=",
			want: "public",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := pgxpool.ParseConfig(tt.dsn)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			pinSearchPath(cfg)
			if got := cfg.ConnConfig.RuntimeParams["search_path"]; got != tt.want {
				t.Errorf("search_path = %q, want %q", got, tt.want)
			}
		})
	}
}

// A migration's checksum identifies its SQL, so a database can be asked
// whether it ran what this binary contains (#294 item 2).
func TestMigrationChecksumTracksTheSQLNotTheName(t *testing.T) {
	a := migration{version: 1, name: "0001_first.sql", sql: "CREATE TABLE x (id int);"}
	renamed := migration{version: 1, name: "0001_renamed.sql", sql: a.sql}
	altered := migration{version: 1, name: a.name, sql: "CREATE TABLE x (id bigint);"}

	if a.checksum() != renamed.checksum() {
		t.Error("renaming a migration changed its checksum -- a rename is cosmetic and would look like tampering")
	}
	if a.checksum() == altered.checksum() {
		t.Error("changing a migration's SQL did not change its checksum, so the whole check is inert")
	}
	if a.checksum() == "" {
		t.Error("empty checksum")
	}
}

// verifyApplied is what stands between "schema_version says version 3"
// and "the database actually ran version 3". A role with write access to
// that table could claim a version, and mikroview would log "up to date"
// and query a schema it has never seen.
func TestVerifyAppliedRefusesAMismatchedChecksum(t *testing.T) {
	m := migration{version: 1, name: "0001_first.sql", sql: "CREATE TABLE x (id int);"}
	migrations := []migration{m}

	if err := verifyApplied(map[int64]string{1: m.checksum()}, migrations, "test"); err != nil {
		t.Errorf("a matching checksum was refused: %v", err)
	}

	err := verifyApplied(map[int64]string{1: "deadbeef"}, migrations, "test")
	if err == nil {
		t.Fatal("a mismatched checksum was accepted -- the database could be running any schema at all")
	}
	// The operator has to be able to act on this without reading source.
	for _, want := range []string{"0001_first.sql", "schema_version", "Refusing to start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, err)
		}
	}
}

// Rows written before the column existed have nothing to compare.
// Refusing them would be a false alarm on every existing deployment's
// first upgrade, which is worse than leaving them unverifiable.
func TestVerifyAppliedAcceptsRowsWithNoRecordedChecksum(t *testing.T) {
	m := migration{version: 1, name: "0001_first.sql", sql: "CREATE TABLE x (id int);"}
	if err := verifyApplied(map[int64]string{1: ""}, []migration{m}, "test"); err != nil {
		t.Errorf("a pre-checksum row was refused: %v", err)
	}
}

// A database that has run a migration this binary does not contain is an
// older image against a newer database. Real, documented elsewhere, and
// deliberately not this check's job -- refusing here would duplicate
// that guidance badly.
func TestVerifyAppliedIgnoresVersionsThisBuildDoesNotHave(t *testing.T) {
	m := migration{version: 1, name: "0001_first.sql", sql: "CREATE TABLE x (id int);"}
	if err := verifyApplied(map[int64]string{1: m.checksum(), 99: "whatever"}, []migration{m}, "test"); err != nil {
		t.Errorf("an unknown future version was refused by the checksum check: %v", err)
	}
}
