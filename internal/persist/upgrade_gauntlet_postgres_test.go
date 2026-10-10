// SPDX-License-Identifier: AGPL-3.0-only

// upgrade_gauntlet_postgres_test.go is #1202's migration test, database
// half (build notes section C, "Postgres recordings"): every recorded
// pg_dump restored into a database of its own, then the auth rows opened
// through persist.NewPostgresBackend wrapped in gauntlet's persist.Encrypt
// with MigratePlaintext and a test key. There is no unwrapped variant.
//
// Skips without MIKROVIEW_TEST_POSTGRES and without recorded dumps, as
// upgrade_postgres_test.go does.
package persist_test

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// #1202 C (Postgres): schema 4 in the migrations table, auth rows
// sealed on first open under their own labels, sign-in works after a
// re-open, version-10 and version-3 documents round-trip.
func TestUpgradeFixturesPostgresOpenOnGauntlet(t *testing.T) {
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres integration tests")
	}
	root := repoRoot(t)
	dumps, err := filepath.Glob(filepath.Join(root, ".upgrade-fixtures", "upgrade-fixture-*-postgres.sql.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) == 0 {
		t.Skip("no recorded Postgres dumps under .upgrade-fixtures/ -- run scripts/fetch-upgrade-fixtures.sh first")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Fatalf("psql is not on PATH, so %d recorded dump(s) cannot be restored: %v", len(dumps), err)
	}

	for _, dump := range dumps {
		version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(dump), "upgrade-fixture-"), "-postgres.sql.gz")
		t.Run(version, func(t *testing.T) {
			ctx := t.Context()
			m := loadGauntletManifest(t, root, version)
			database := restoreDump(t, dsn, version, dump)
			pool, err := persist.OpenPool(ctx, database)
			if err != nil {
				t.Fatalf("opening the restored database: %v", err)
			}
			defer pool.Close()

			// C.1: the migrations table shows 4.
			if err := pool.Migrate(ctx); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if got := maxSchemaVersion(t, pool); got != 4 {
				t.Errorf("schema after migration = %d, want 4", got)
			}

			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				t.Fatal(err)
			}
			sealed := func(name string) gpersist.Backend {
				b, err := gpersist.Encrypt(persist.NewPostgresBackend(pool, name), key,
					gpersist.EncryptOptions{Label: name, MigratePlaintext: true})
				if err != nil {
					t.Fatalf("gpersist.Encrypt(%s): %v", name, err)
				}
				return b
			}
			raw := func(name string) func() []byte {
				return func() []byte {
					var payload string
					if err := pool.Raw().QueryRow(ctx, `SELECT payload FROM store_blob WHERE name = $1`, name).Scan(&payload); err != nil {
						t.Fatalf("reading the %s row: %v", name, err)
					}
					return []byte(payload)
				}
			}
			runGauntletChecks(t, gauntletCase{
				accounts:         func() gpersist.Backend { return sealed("auth") },
				tokens:           func() gpersist.Backend { return sealed("tokens") },
				rawAccounts:      raw("auth"),
				rawTokens:        raw("tokens"),
				admin:            m.AdminUsername,
				viewer:           m.ViewerUsername,
				apiTokenName:     m.APITokenName,
				apiTokenKindless: version == "v0.1.0", // #1202 review R12
				totp:             m.SecondFactorEnrolled,
				plaintext:        true, // whatever the file recording says, the dumps hold plaintext rows
				postgres:         true,
			})
		})
	}
}
