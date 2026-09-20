// SPDX-License-Identifier: AGPL-3.0-only

// upgrade_postgres_test.go is the database half of the upgrade proof
// (#1247, following #1239's file half in upgrade_test.go). For every
// pg_dump under .upgrade-fixtures/ -- one per *schema* version, taken
// from the first released image that carried it -- restore it into a
// freshly created database, open it with the current build so the
// pending migrations run, and check that what
// scripts/record-upgrade-fixture.sh wrote is still there and still means
// what it meant.
//
// Per schema version rather than per release, per docs/decisions/
// upgrade-framework.md: the Postgres migrations are numbered and
// checksummed, so two releases on the same schema version have the same
// database to restore. Today that is v0.1.0 (schema 1, store_blob),
// v0.2.0 (schema 2, match_log) and v0.3.0 (schema 3,
// match_log.provisional).
//
// A fresh *database* rather than a fresh schema, unlike the pool helper
// in postgres_test.go: pg_dump's plain output names public explicitly and
// carries psql meta-commands (\restrict, \.), so it restores faithfully
// only into a database of its own, through psql.
//
// Nothing here is encrypted, whatever the manifest says about the file
// recording: #853's key encrypts documents the *file* backend writes
// (storage.backendFor), and the Postgres path never consults it.
package persist_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/coverage"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hosts"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// TestUpgradeFixturesPostgres restores every recorded pg_dump and opens
// it with the current build.
//
// Skips without MIKROVIEW_TEST_POSTGRES, like every other Postgres test
// here; .gitlab-ci.yml's test:postgres job sets it and fetches the dumps
// first. It does *not* skip when psql is missing while a database is
// configured: a gate that quietly passes having restored nothing is the
// failure mode this whole framework exists to avoid.
func TestUpgradeFixturesPostgres(t *testing.T) {
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres integration tests")
	}

	root := repoRoot(t)
	fixtureDir := filepath.Join(root, ".upgrade-fixtures")
	dumps, err := filepath.Glob(filepath.Join(fixtureDir, "upgrade-fixture-*-postgres.sql.gz"))
	if err != nil {
		t.Fatalf("looking for recorded dumps: %v", err)
	}
	if len(dumps) == 0 {
		t.Skip("no recorded Postgres dumps under .upgrade-fixtures/ -- run scripts/fetch-upgrade-fixtures.sh first")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Fatalf("psql is not on PATH, so %d recorded dump(s) cannot be restored: %v "+
			"(install postgresql-client -- see .gitlab-ci.yml's test:postgres before_script)", len(dumps), err)
	}

	for _, dump := range dumps {
		base := filepath.Base(dump)
		version := strings.TrimSuffix(strings.TrimPrefix(base, "upgrade-fixture-"), "-postgres.sql.gz")
		t.Run(version, func(t *testing.T) {
			testOnePostgresFixture(t, root, version, dsn, dump)
		})
	}
}

// Not a t.Helper(): this is the body of each subtest, and marking it one
// would attribute every assertion below to the single t.Run line above.
func testOnePostgresFixture(t *testing.T, root, version, adminDSN, dumpPath string) {
	ctx := t.Context()

	manifest := loadUpgradeManifest(t, root, version)
	database := restoreDump(t, adminDSN, version, dumpPath)

	pool, err := persist.OpenPool(ctx, database)
	if err != nil {
		t.Fatalf("opening the restored database: %v", err)
	}
	defer pool.Close()

	// 1. What the released image left behind, before this build touches
	// it. Not an equality assertion against a number written here: the
	// recording is the authority on what that release wrote, and
	// hard-coding it would mean editing this test every time a dump is
	// re-recorded. What matters is that it is a real, older-or-equal
	// schema -- anything above CurrentSchema() would be data this build
	// must refuse rather than migrate.
	before := maxSchemaVersion(t, pool)
	if before <= 0 {
		t.Fatalf("the restored database records schema version %d -- the dump carried no applied migrations", before)
	}
	if before > persist.CurrentSchema() {
		t.Fatalf("the restored database is at schema %d, above this build's CurrentSchema() = %d", before, persist.CurrentSchema())
	}
	t.Logf("%s recorded schema version %d; migrating to %d", version, before, persist.CurrentSchema())

	// 2. The migrations run, and the checksums the released image
	// recorded still verify against this build's migration text --
	// Migrate refuses outright if they do not (persist.verifyApplied).
	if err := pool.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if after := maxSchemaVersion(t, pool); after != persist.CurrentSchema() {
		t.Errorf("schema after migration = %d, want CurrentSchema() = %d", after, persist.CurrentSchema())
	}

	// 3. The admin and viewer accounts exist and their passwords verify.
	authStore, err := auth.OpenWithBackend(persist.NewPostgresBackend(pool, "auth"))
	if err != nil {
		t.Fatalf("opening the accounts store: %v", err)
	}
	if _, err := authStore.Authenticate(manifest.AdminUsername, fixtureAdminPassword, time.Now()); err != nil {
		t.Errorf("admin login (%s): %v", manifest.AdminUsername, err)
	}
	if _, err := authStore.Authenticate(manifest.ViewerUsername, fixtureAdminPassword, time.Now()); err != nil {
		t.Errorf("viewer login (%s): %v", manifest.ViewerUsername, err)
	}

	// 4. The named entity.
	entityStore, err := entities.OpenWithBackend(persist.NewPostgresBackend(pool, "entities"))
	if err != nil {
		t.Fatalf("opening the entities store: %v", err)
	}
	if !entityHasLabel(entityStore.List(), manifest.Entity.Type, manifest.Entity.Key, manifest.Entity.Label) {
		t.Errorf("entity %+v not found (or label mismatch) in the entities store", manifest.Entity)
	}

	// 5. The flag (present on every version: new_device is deterministic
	// since v0.1.0).
	flagsStore, err := flags.OpenWithBackend(persist.NewPostgresBackend(pool, "flags"))
	if err != nil {
		t.Fatalf("opening the flags store: %v", err)
	}
	defer func() { _ = flagsStore.Close(ctx) }()
	if !hasFlag(flagsStore.List(), manifest.Flag.Type, manifest.Flag.Target) {
		t.Errorf("flag %+v not found in the flags store", manifest.Flag)
	}

	// 6. The watchlist entry, from v0.3.0 on.
	if manifest.Watchlist != nil {
		defsStore, err := engine.OpenDefinitionsStoreWithBackend(persist.NewPostgresBackend(pool, "definitions"))
		if err != nil {
			t.Fatalf("opening the definitions store: %v", err)
		}
		defer func() { _ = defsStore.Close(ctx) }()
		if err := checkWatchlist(defsStore.List(), manifest.Watchlist.Name, manifest.Watchlist.Ports); err != nil {
			t.Error(err)
		}
	}

	// 7. The coverage declaration and the host mark, from v0.5.0 on.
	if manifest.Coverage != nil {
		covStore, err := coverage.OpenWithBackend(persist.NewPostgresBackend(pool, "coverage"))
		if err != nil {
			t.Fatalf("opening the coverage store: %v", err)
		}
		if !hasDeclaration(covStore.List(), manifest.Coverage.Key, manifest.Coverage.Reason) {
			t.Errorf("coverage declaration %+v not found in the coverage store", manifest.Coverage)
		}
	}
	if manifest.HostMark != nil {
		hostRegister, err := hosts.OpenWithBackend(persist.NewPostgresBackend(pool, "hosts"))
		if err != nil {
			t.Fatalf("opening the hosts store: %v", err)
		}
		defer func() { _ = hostRegister.Close(ctx) }()
		h, ok := hostRegister.Get(manifest.HostMark.Key)
		if !ok {
			t.Errorf("host %q not found in the hosts store", manifest.HostMark.Key)
		} else if h.Mark == nil {
			t.Errorf("host %q has no mark", manifest.HostMark.Key)
		} else if string(h.Mark.Kind) != manifest.HostMark.Kind || h.Mark.Reason != manifest.HostMark.Reason {
			t.Errorf("host %q mark = %+v, want kind=%s reason=%s", manifest.HostMark.Key, h.Mark, manifest.HostMark.Kind, manifest.HostMark.Reason)
		}
	}
}

// maxSchemaVersion reads the highest applied migration the database
// records. Raw SQL rather than an exported helper because
// persist.Pool.Migrate deliberately reports this only to the log.
func maxSchemaVersion(t *testing.T, pool *persist.Pool) int64 {
	t.Helper()
	var version int64
	err := pool.Raw().QueryRow(t.Context(),
		`SELECT coalesce(max(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	return version
}

// restoreDump creates a database of its own for one recording, restores
// the gzip'd plain-SQL dump into it with psql, and returns a DSN
// pointing at it. The database is dropped when the test ends.
func restoreDump(t *testing.T, adminDSN, version, dumpPath string) string {
	t.Helper()
	ctx := t.Context()

	database := "upgrade_fixture_" + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToLower(version))
	// Identifiers cannot be bound parameters. This one is derived from a
	// filename in .upgrade-fixtures/ rather than from source, so it is
	// mapped to [a-z0-9_] above and checked again here -- a dump named
	// anything else is refused rather than concatenated.
	for _, r := range database {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			t.Fatalf("recorded version %q does not map to a safe database identifier (%q)", version, database)
		}
	}

	admin, err := persist.OpenPool(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connecting to %s: %v", redactDSN(adminDSN), err)
	}
	defer admin.Close()

	// WITH (FORCE) so a connection left over from a previous failed run
	// cannot make this test fail for a reason that has nothing to do
	// with the recording.
	if _, err := admin.Raw().Exec(ctx, `DROP DATABASE IF EXISTS `+database+` WITH (FORCE)`); err != nil {
		t.Fatalf("dropping %s: %v", database, err)
	}
	if _, err := admin.Raw().Exec(ctx, `CREATE DATABASE `+database); err != nil {
		t.Fatalf("creating %s: %v", database, err)
	}
	t.Cleanup(func() {
		cleanup, err := persist.OpenPool(context.Background(), adminDSN)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Raw().Exec(context.Background(), `DROP DATABASE IF EXISTS `+database+` WITH (FORCE)`)
	})

	target := withDatabase(t, adminDSN, database)

	f, err := os.Open(dumpPath)
	if err != nil {
		t.Fatalf("opening %s: %v", dumpPath, err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader for %s: %v", dumpPath, err)
	}
	defer gz.Close()

	// The password goes to psql in the environment, never in argv: a
	// process listing is readable by anyone on the box, and on CI this
	// DSN is a job variable.
	safe, password := splitDSNPassword(t, target)
	cmd := exec.CommandContext(ctx, "psql", "--quiet", "--no-psqlrc", "--set=ON_ERROR_STOP=1",
		"--dbname", safe, "--file", "-")
	cmd.Stdin = gz
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("restoring %s into %s: %v\n%s", filepath.Base(dumpPath), database, err, out.String())
	}

	return target
}

// withDatabase returns dsn pointing at a different database on the same
// server, with everything else (sslmode, search_path, credentials) as it
// was.
func withDatabase(t *testing.T, dsn, database string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("MIKROVIEW_TEST_POSTGRES is not a URL-form DSN: %v", err)
	}
	u.Path = "/" + database
	return u.String()
}

// splitDSNPassword returns the DSN with its password removed, and the
// password separately, so the two can travel by different routes.
func splitDSNPassword(t *testing.T, dsn string) (safe, password string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("MIKROVIEW_TEST_POSTGRES is not a URL-form DSN: %v", err)
	}
	if u.User != nil {
		password, _ = u.User.Password()
		u.User = url.User(u.User.Username())
	}
	return u.String(), password
}

// redactDSN is for the one error message raised before the pool exists,
// where persist's own redaction has not had a chance to run.
func redactDSN(dsn string) string {
	safe, _ := url.Parse(dsn)
	if safe == nil {
		return "the configured database"
	}
	if safe.User != nil {
		safe.User = url.User(safe.User.Username())
	}
	return safe.String()
}

// loadUpgradeManifest reads the manifest describing one recording:
// the committed testdata/upgrade/<version>/manifest.json where there is
// one, and otherwise the copy scripts/fetch-upgrade-fixtures.sh pulled
// out of the package registry beside the recording itself.
//
// The fallback is #1247's decision (2026-09-16). The tag job that makes
// a recording authenticates with a job token, which can upload a package
// but cannot push or open a merge request, so it cannot commit the
// manifest. Uploading it next to the recording means dev's next run
// tests the new release straight away, and committing the manifest stays
// a review step rather than something the gate waits for.
func loadUpgradeManifest(t *testing.T, root, version string) upgradeManifest {
	t.Helper()
	candidates := []string{
		filepath.Join(root, "testdata", "upgrade", version, "manifest.json"),
		filepath.Join(root, ".upgrade-fixtures", version, "manifest.json"),
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path) // #nosec G304 -- both paths are built from this repo's own layout
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var manifest upgradeManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		return manifest
	}
	t.Fatalf("no manifest for %s: looked in %s. A recorded fixture with no manifest cannot be checked -- "+
		"re-run scripts/fetch-upgrade-fixtures.sh, or commit testdata/upgrade/%s/manifest.json",
		version, strings.Join(candidates, " and "), version)
	return upgradeManifest{}
}
