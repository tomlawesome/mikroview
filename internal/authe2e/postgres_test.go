// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// The Postgres halves of build notes B1.3, B1.5, B8.35 and B9.42, through
// the real binary. They skip without MIKROVIEW_TEST_POSTGRES, as the other
// Postgres tests do. Each test works in a schema of its own (the DSN's
// search_path), dropped afterwards.

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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

// #1202 B8.35 (Postgres), as changed by R26 (ruling 19; it was "plaintext
// auth rows left by an earlier release are sealed on first open"): a
// plaintext `auth` or `tokens` row refuses start-up, at the schema the
// database was left at (3 or 4) alike. The row is left byte for byte, no
// setup code is logged and the refusal has the ruling-19 wording. Paired:
// with the sealed rows put back the install starts and signs in.
func TestPostgresPlaintextAuthRowsRefuseStartUpAndAreLeftAlone(t *testing.T) {
	for _, schema := range []int{4, 3} {
		t.Run(fmt.Sprintf("schema %d", schema), func(t *testing.T) {
			p := newPG(t)
			f := newLegacyInstallSealed(t)

			// A first start moves the sealed files onto Postgres and makes the
			// tables; keep the sealed rows to put back.
			first := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
			first.stop()
			sealedRows := map[string]string{}
			for _, name := range []string{"auth", "tokens"} {
				row, ok := p.row(t, name)
				if !ok || !isSealedRow(row) {
					t.Fatalf("test setup: no sealed %q row after the first start: %.60s", name, row)
				}
				sealedRows[name] = row
			}
			setRow := func(name, payload string) {
				if _, err := p.conn.Exec(context.Background(),
					`INSERT INTO store_blob (name, payload, version) VALUES ($1, $2, 1)
					 ON CONFLICT (name) DO UPDATE SET payload = EXCLUDED.payload, version = store_blob.version + 1`, name, payload); err != nil {
					t.Fatalf("planting the %s row: %v", name, err)
				}
			}
			if schema == 3 {
				if _, err := p.conn.Exec(context.Background(), `DELETE FROM schema_version WHERE version = 4`); err != nil {
					t.Fatalf("putting the stored schema back to 3: %v", err)
				}
			}

			for name, doc := range map[string][]byte{"auth": f.accounts, "tokens": f.tokenDoc} {
				setRow(name, string(doc))
				code, out, _ := refused(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
				if code == 0 {
					t.Errorf("%s row plaintext: exit status 0\n%s", name, out)
				}
				wantRuling19Refusal(t, name+" row plaintext", out)
				if setupCodeRe.MatchString(out) {
					t.Errorf("%s row plaintext: a setup code was logged:\n%s", name, out)
				}
				if after, _ := p.row(t, name); after != string(doc) {
					t.Errorf("%s row plaintext: the row was sealed or rewritten: %.80s", name, after)
				}
				setRow(name, sealedRows[name])
			}

			a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
			if setupCodeRe.MatchString(a.log()) {
				t.Errorf("an install with accounts announced a setup code:\n%s", a.log())
			}
			_, r := (&world{a: a, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
			wantStatus(t, "sign-in with the sealed rows back", r, 200)
			wantStatus(t, "api token with the sealed rows back", a.client().bearer(f.tokens["api"], "GET", "/api/stats"), 200)
		})
	}
}

// #1202 review R26 (ruling 19), plaintext sources in file-to-Postgres
// adoption: a file install whose accounts and tokens files are plaintext
// (a keyless v0.6 install, now given a key and a Postgres address at
// once) is not moved onto Postgres. Start-up is refused with the ruling-19
// wording, no setup code is logged, the files are byte for byte as they
// were, nothing readable is written to the database and the adoption is
// not marked done. Paired: TestSealedFileInstallMovesOntoPostgres moves
// the same install when its files are sealed.
func TestPlaintextFilesAreNotMovedOntoPostgres(t *testing.T) {
	p := newPG(t)
	f := newLegacyInstall(t)
	data := filepath.Join(f.root, "data")
	beforeUsers := readBytes(t, filepath.Join(data, "users.json"))
	beforeTokens := readBytes(t, filepath.Join(data, "tokens.json"))

	code, out, a := refused(t, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn})
	if code == 0 {
		t.Errorf("exit status 0 moving plaintext files onto Postgres\n%s", out)
	}
	wantRuling19Refusal(t, "plaintext files onto Postgres", out)
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged:\n%s", out)
	}
	if !bytes.Equal(readBytes(t, filepath.Join(data, "users.json")), beforeUsers) || !bytes.Equal(readBytes(t, filepath.Join(data, "tokens.json")), beforeTokens) {
		t.Error("a plaintext source file was changed (sealed or rewritten)")
	}
	for _, name := range []string{"auth", "tokens"} {
		if row, ok := p.row(t, name); ok && strings.Contains(row, f.admin.name) {
			t.Errorf("the %q row holds the account in the clear after a refused move: %.80s", name, row)
		}
	}
	for _, name := range dataFiles(t, a) {
		low := strings.ToLower(name)
		if strings.Contains(low, "postgres") || strings.Contains(low, "adopt") {
			t.Errorf("%s was written although the move onto Postgres was refused: the adoption is marked done", name)
		}
	}
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
