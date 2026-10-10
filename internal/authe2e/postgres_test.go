// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// The Postgres halves of build notes B1.3, B1.5, B8.35 and B9.42, through
// the real binary. They skip without MIKROVIEW_TEST_POSTGRES, as the other
// Postgres tests do. Each test works in a schema of its own (the DSN's
// search_path), dropped afterwards.

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type pgEnv struct {
	dsn  string // with this test's search_path
	conn *pgx.Conn
}

func newPG(t *testing.T) *pgEnv {
	t.Helper()
	base := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if base == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping Postgres integration tests")
	}
	ctx := context.Background()
	// setup creates and drops the schema; it stays on the default
	// search_path. The connection the test reads and writes rows through
	// is a second one, opened with the schema baked into its DSN -- the
	// same search_path the binary under test gets -- so store_blob is the
	// binary's table and not one in the public schema (as internal/persist's
	// newTestPool does for its pool).
	setup, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	schema := "authe2e_" + strings.ToLower(randHex(t, 6))
	if _, err := setup.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating the schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = setup.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		_ = setup.Close(context.Background())
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("MIKROVIEW_TEST_POSTGRES is not a URL-form DSN: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatalf("connecting with the test schema's search_path: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return &pgEnv{dsn: u.String(), conn: conn}
}

func (p *pgEnv) row(t *testing.T, name string) (string, bool) {
	t.Helper()
	var payload string
	err := p.conn.QueryRow(context.Background(), `SELECT payload FROM store_blob WHERE name = $1`, name).Scan(&payload)
	if err != nil {
		return "", false
	}
	return payload, true
}

func isSealedRow(payload string) bool {
	return bytes.HasPrefix(bytes.TrimSpace([]byte(payload)), []byte(`{"sealed"`))
}

// #1202 B1.3 and B1.5 (Postgres): with a key the `auth` row holds
// {"sealed": ...} after the first open, a second open reads it back and
// sign-in works; without a key start-up is refused as on files and the row
// is untouched.
func TestPostgresAuthRowsAreSealedAndStartUpWithoutAKeyIsRefused(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})

	wantStatus(t, "mint a token", w.c.post("/api/tokens", map[string]any{"name": "grafana", "password": w.admin.pass}), 201)
	code, out, _ := cli(t, Opts{Root: w.a.root, PostgresDSN: p.dsn}, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Fatalf("-generate-recovery-keys on Postgres: exit %d\n%s", code, out)
	}
	_, r := w.signIn(t, w.admin, w.admin.code(t, w.a, 1))
	wantStatus(t, "sign-in", r, 200)
	time.Sleep(6 * time.Second) // sign-in history rows are saved a few seconds after they arrive
	w.a.stop()

	rows := map[string]string{}
	for _, name := range []string{"auth", "tokens", "recovery_keys", "signins"} {
		payload, ok := p.row(t, name)
		if !ok {
			t.Errorf("no %q row", name)
			continue
		}
		rows[name] = payload
		if !isSealedRow(payload) {
			t.Errorf("the %q row is not {\"sealed\": ...}: %.80s", name, payload)
		}
		if strings.Contains(payload, "mv-admin") {
			t.Errorf("the %q row holds the username in the clear", name)
		}
	}
	if rows["auth"] == rows["tokens"] {
		t.Error("the auth and tokens rows are identical")
	}

	// A second open reads it back; sign-in works.
	b := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	if s := b.client().session(t); s.SetupRequired {
		t.Fatal("the second open read the sealed auth row as an empty store")
	}
	_, r = (&world{a: b, admin: w.admin}).signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "sign-in after a re-open", r, 200)
	b.stop()

	// No key: refused, row untouched, no setup code.
	before, _ := p.row(t, "auth")
	code2, out2, _ := refused(t, Opts{Root: w.a.root, PostgresDSN: p.dsn, NoKey: true})
	if code2 == 0 {
		t.Errorf("exit status 0 with no key on Postgres\n%s", out2)
	}
	if cfgCodeRe.FindString(out2) == "" || !strings.Contains(out2, "history.keyFile") {
		t.Errorf("the Postgres refusal is not the file refusal (a CFG code and how to mount a key):\n%s", out2)
	}
	if setupCodeRe.MatchString(out2) {
		t.Errorf("a setup code was logged although start-up was refused:\n%s", out2)
	}
	if after, _ := p.row(t, "auth"); after != before {
		t.Error("a refused start-up changed the auth row")
	}
}

// #1202 B8.35 (Postgres): plaintext auth rows left by an earlier release
// are sealed on first open; every account and token still works.
func TestPostgresPlaintextAuthRowsAreSealedOnFirstStart(t *testing.T) {
	p := newPG(t)
	f := newLegacyInstall(t)

	// One start to create the tables, then plant the old plaintext rows.
	first := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
	first.stop()
	for name, doc := range map[string][]byte{"auth": f.accounts, "tokens": f.tokenDoc} {
		if _, err := p.conn.Exec(context.Background(),
			`INSERT INTO store_blob (name, payload, version) VALUES ($1, $2, 1)
			 ON CONFLICT (name) DO UPDATE SET payload = EXCLUDED.payload, version = store_blob.version + 1`, name, string(doc)); err != nil {
			t.Fatalf("planting the %s row: %v", name, err)
		}
	}

	// #1202 review R0 (ruling 14): sealing in place applies only on the
	// first start of v0.7.0. The first start above stamped schema 4, so
	// put the database back at 3, as the earlier release left it.
	if _, err := p.conn.Exec(context.Background(), `DELETE FROM schema_version WHERE version = 4`); err != nil {
		t.Fatalf("putting the stored schema back to 3: %v", err)
	}

	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
	if setupCodeRe.MatchString(a.log()) {
		t.Errorf("an install with accounts announced a setup code:\n%s", a.log())
	}
	for _, name := range []string{"auth", "tokens"} {
		payload, _ := p.row(t, name)
		if !isSealedRow(payload) {
			t.Errorf("the %q row is still plaintext after first open: %.80s", name, payload)
		}
	}
	_, r := (&world{a: a, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in of a pre-existing account", r, 200)
	wantStatus(t, "api token on /api/stats", a.client().bearer(f.tokens["api"], "GET", "/api/stats"), 200)
}

// #1202 B9.42: the sealed `tokens` row copied into the `auth` row's place
// fails to open. It is never read as an accounts document or an empty
// store, and the row is left as it is. Paired with the genuine row opening.
func TestPostgresTokensRowCopiedIntoTheAuthRowFailsToOpen(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})
	if r := w.c.post("/api/tokens", map[string]any{"name": "grafana", "password": w.admin.pass}); r.Status != 201 {
		t.Fatalf("mint a token: %s", r)
	}
	w.a.stop()

	genuineAuth, _ := p.row(t, "auth")
	tokens, _ := p.row(t, "tokens")
	if !isSealedRow(genuineAuth) || !isSealedRow(tokens) || genuineAuth == tokens {
		t.Fatalf("test setup: want two distinct sealed rows; auth=%.40s tokens=%.40s", genuineAuth, tokens)
	}

	// Paired: the genuine rows open.
	ok := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	if ok.client().session(t).SetupRequired {
		t.Fatal("the genuine auth row read as empty")
	}
	ok.stop()

	if _, err := p.conn.Exec(context.Background(), `UPDATE store_blob SET payload = $1 WHERE name = 'auth'`, tokens); err != nil {
		t.Fatal(err)
	}
	code, out, _ := refused(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	if code == 0 {
		t.Errorf("exit status 0 with the tokens row in the auth row's place\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("the swapped row was treated as an empty store (a setup code was logged):\n%s", out)
	}
	if after, _ := p.row(t, "auth"); after != tokens {
		t.Error("a refused open changed the swapped row")
	}
}
