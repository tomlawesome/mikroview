// SPDX-License-Identifier: AGPL-3.0-only

package persist

// #1202 review R19 (ruling 18): the Postgres backend refuses a database
// whose schema_version table holds a migration this build does not
// contain, the same way the file backend refuses a newer schema.json
// (*SchemaTooNewError). Before ruling 18 such rows were skipped and the
// older build ran on a schema it had never seen.
//
// Skipped without MIKROVIEW_TEST_POSTGRES, as the other Postgres tests are.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// futureVersion records that a build newer than this one has migrated the
// database: one schema_version row with a version this build lacks.
func futureVersion(t *testing.T, p *Pool, version int64) {
	t.Helper()
	if _, err := p.pool.Exec(t.Context(),
		`INSERT INTO schema_version (version, checksum) VALUES ($1, $2)`, version, "feedface"); err != nil {
		t.Fatalf("recording schema version %d: %v", version, err)
	}
}

// pgState is everything a refused start-up must leave alone: every
// schema_version row (including its applied_at) and every store_blob row.
func pgState(t *testing.T, p *Pool) string {
	t.Helper()
	ctx := t.Context()
	var out strings.Builder
	rows, err := p.pool.Query(ctx, `SELECT version, applied_at::text, coalesce(checksum, '') FROM schema_version ORDER BY version`)
	if err != nil {
		t.Fatalf("reading schema_version: %v", err)
	}
	for rows.Next() {
		var v int64
		var at, sum string
		if err := rows.Scan(&v, &at, &sum); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "schema_version %d %s %s\n", v, at, sum)
	}
	rows.Close()
	rows, err = p.pool.Query(ctx, `SELECT name, payload, version FROM store_blob ORDER BY name`)
	if err != nil {
		t.Fatalf("reading store_blob: %v", err)
	}
	for rows.Next() {
		var name, payload string
		var v int64
		if err := rows.Scan(&name, &payload, &v); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "store_blob %s %d %s\n", name, v, payload)
	}
	rows.Close()
	return out.String()
}

// #1202 review R19: a database that records a version above the build's
// highest is refused by Migrate with a *SchemaTooNewError naming both
// versions and the two ways out, and nothing in the database changes.
func TestPostgresMigrateRefusesASchemaNewerThanTheBuild(t *testing.T) {
	for _, ahead := range []int64{1, 100} {
		t.Run(fmt.Sprintf("%d ahead", ahead), func(t *testing.T) {
			p := newTestPool(t) // migrated to this build's CurrentSchema()
			ctx := t.Context()
			known := CurrentSchema()
			stored := known + ahead

			// A payload in the store, so "writes nothing" covers data too.
			if _, err := NewPostgresBackend(p, "auth").Save(ctx, []byte(`{"users":[]}`), 0); err != nil {
				t.Fatalf("seeding the auth row: %v", err)
			}
			futureVersion(t, p, stored)
			before := pgState(t, p)

			err := p.Migrate(ctx)
			var tooNew *SchemaTooNewError
			if !errors.As(err, &tooNew) {
				t.Fatalf("Migrate on a database at schema %d (build knows %d): err = %v, want a *SchemaTooNewError", stored, known, err)
			}
			if tooNew.Stored != stored || tooNew.Known != known {
				t.Errorf("stored/known = %d/%d, want %d/%d", tooNew.Stored, tooNew.Known, stored, known)
			}
			msg := tooNew.Error()
			for _, want := range []string{fmt.Sprint(stored), fmt.Sprint(known), "Refusing to start"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal does not mention %q:\n%s", want, msg)
				}
			}
			low := strings.ToLower(msg)
			if !strings.Contains(low, "newer") {
				t.Errorf("the refusal does not say to run the newer release:\n%s", msg)
			}
			if !strings.Contains(low, "restore") || !strings.Contains(low, "pre-upgrade") {
				t.Errorf("the refusal does not say to restore the pre-upgrade copy:\n%s", msg)
			}
			if after := pgState(t, p); after != before {
				t.Errorf("the refusal changed the database:\nbefore:\n%s\nafter:\n%s", before, after)
			}

			// Asking again is refused again: nothing was recorded that
			// would let the second start through.
			if err := p.Migrate(ctx); !errors.As(err, &tooNew) {
				t.Errorf("second Migrate: err = %v, want a *SchemaTooNewError again", err)
			}
		})
	}
}

// #1202 review R19: refusal comes before any migration is applied. A
// database that is both behind (a migration this build has not yet run)
// and holds a version above the build's highest is refused, and the
// pending migration is not applied on the way.
func TestPostgresMigrateRefusesBeforeApplyingAnythingPending(t *testing.T) {
	p := newTestPool(t)
	ctx := t.Context()
	known := CurrentSchema()
	if _, err := p.pool.Exec(ctx, `DELETE FROM schema_version WHERE version = $1`, known); err != nil {
		t.Fatalf("making the last migration pending: %v", err)
	}
	futureVersion(t, p, known+1)
	before := pgState(t, p)

	err := p.Migrate(ctx)
	var tooNew *SchemaTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("err = %v, want a *SchemaTooNewError", err)
	}
	if after := pgState(t, p); after != before {
		t.Errorf("the refusal applied or recorded something:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// #1202 review R19, the pairing: a database at exactly the build's
// version is accepted, repeatedly, and Migrate writes nothing to it.
func TestPostgresMigrateAcceptsASchemaExactlyAtTheBuild(t *testing.T) {
	p := newTestPool(t)
	ctx := context.Background()
	if _, err := NewPostgresBackend(p, "auth").Save(ctx, []byte(`{"users":[]}`), 0); err != nil {
		t.Fatalf("seeding the auth row: %v", err)
	}
	before := pgState(t, p)

	for i := 0; i < 2; i++ {
		if err := p.Migrate(ctx); err != nil {
			t.Fatalf("Migrate %d on a database at this build's schema (%d): %v", i+1, CurrentSchema(), err)
		}
	}
	if after := pgState(t, p); after != before {
		t.Errorf("Migrate on an up-to-date database wrote something:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
