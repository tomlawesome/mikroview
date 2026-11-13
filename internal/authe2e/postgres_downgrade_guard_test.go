// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1202 review R19 (ruling 18), through the real binary: with the
// database's schema_version holding a migration this build does not
// contain, start-up on Postgres is refused with a message naming the
// stored and known versions and saying to run the newer release or restore
// the pre-upgrade copy. Nothing is migrated or written, no setup code is
// logged, and the command-line modes that open the store refuse the same
// way. Paired: a database at exactly the build's version starts normally.
//
// Skips without MIKROVIEW_TEST_POSTGRES, as the other Postgres tests do.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// r19State is what a refused start must leave alone: the schema_version
// rows (with applied_at) and the store rows.
func (p *pgEnv) r19State(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var out strings.Builder
	rows, err := p.conn.Query(ctx, `SELECT version, applied_at::text, coalesce(checksum, '') FROM schema_version ORDER BY version`)
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
	rows, err = p.conn.Query(ctx, `SELECT name, version, payload FROM store_blob ORDER BY name`)
	if err != nil {
		t.Fatalf("reading store_blob: %v", err)
	}
	for rows.Next() {
		var name, payload string
		var v int64
		if err := rows.Scan(&name, &v, &payload); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "store_blob %s %d %s\n", name, v, payload)
	}
	rows.Close()
	return out.String()
}

// r19WantRefusalMessage checks the text an operator reads.
func r19WantRefusalMessage(t *testing.T, what, out string, stored, known int64) {
	t.Helper()
	whole := func(n int64) bool { return regexp.MustCompile(fmt.Sprintf(`\b%d\b`, n)).MatchString(out) }
	if !whole(stored) || !whole(known) {
		t.Errorf("%s: the refusal does not name the stored version %d and the known version %d:\n%s", what, stored, known, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "newer") {
		t.Errorf("%s: the refusal does not say to run the newer release:\n%s", what, out)
	}
	if !strings.Contains(low, "restore") || !strings.Contains(low, "pre-upgrade") {
		t.Errorf("%s: the refusal does not say to restore the pre-upgrade copy:\n%s", what, out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("%s: a setup code was logged although start-up was refused:\n%s", what, out)
	}
}

// #1202 review R19: start-up and the four commands that open the store are
// refused on a database ahead of the build, and write nothing.
func TestPostgresStartUpAndCommandsRefuseASchemaNewerThanTheBuild(t *testing.T) {
	p := newPG(t)
	known := persist.CurrentSchema()
	stored := known + 35 // far enough ahead not to be mistaken for the known version

	// Paired, first: a database at exactly this build's version starts,
	// and starts again.
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})
	w.a.stop()
	mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn}).stop()

	// A newer build has been here.
	if _, err := p.conn.Exec(context.Background(),
		`INSERT INTO schema_version (version, checksum) VALUES ($1, 'feedface')`, stored); err != nil {
		t.Fatalf("recording a newer schema version: %v", err)
	}
	before := p.r19State(t)

	t.Run("start-up", func(t *testing.T) {
		code, out, _ := refused(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
		if code == 0 {
			t.Errorf("start-up exited 0 on a database at schema %d (build knows %d)\n%s", stored, known, out)
		}
		r19WantRefusalMessage(t, "start-up", out, stored, known)
		if after := p.r19State(t); after != before {
			t.Errorf("a refused start-up changed the database:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	for _, args := range [][]string{
		{"-recover-admin-account"},
		{"-transfer-admin", "heir"},
		{"-clear-second-factor", "someone"},
		{"-generate-recovery-keys"},
	} {
		t.Run(args[0], func(t *testing.T) {
			code, out, _ := cli(t, Opts{Root: w.a.root, PostgresDSN: p.dsn}, "ANYRECOVERYKEYANYRECOVERYKEY1234\nsaved\n", args...)
			if code == 0 {
				t.Errorf("%v exited 0 on a database at schema %d (build knows %d):\n%s", args, stored, known, out)
			}
			r19WantRefusalMessage(t, fmt.Sprint(args), out, stored, known)
			if after := p.r19State(t); after != before {
				t.Errorf("%v: a refused command changed the database:\nbefore:\n%s\nafter:\n%s", args, before, after)
			}
		})
	}

	// Taking the newer row away (the pre-upgrade copy restored, in effect)
	// lets the same install start again.
	if _, err := p.conn.Exec(context.Background(), `DELETE FROM schema_version WHERE version = $1`, stored); err != nil {
		t.Fatalf("removing the newer schema version: %v", err)
	}
	mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn}).stop()
}
