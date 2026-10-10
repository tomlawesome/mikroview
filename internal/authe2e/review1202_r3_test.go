// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1202 review round 3 (rulings 16, 17, 19 and 20; "a refused start leaves
// the data openable by the release it came from"), written from the
// specification before the fixes exist. Each test carries its point
// (// S1 ...). All on Postgres except S8. Wording: the ruling-19 wording of
// wantRuling19Refusal; S8's refusal says, as ruling 16's does, to fix the
// file "on disk".

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/gauntlet"
	gpersist "github.com/tomlawesome/gauntlet/persist"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// plainDropList is a keyless install's drop-list document.
const plainDropList = `{"entries":[{"cidr":"` + reviewCIDR + `","addedBy":"mv-admin","addedAt":"2026-01-02T03:04:05Z","reason":"keyless era"}]}`

// schema4Rows counts the version-4 rows of the database's schema_version
// table; a table that does not exist yet counts as none.
func (p *pgEnv) schema4Rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := p.conn.QueryRow(context.Background(), `SELECT count(*) FROM schema_version WHERE version = 4`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// wantSchemaUnstamped: nothing recorded schema 4, in the database or in
// the data directory (stamped 3 by the test).
func wantSchemaUnstamped(t *testing.T, what string, p *pgEnv, dataDir string) {
	t.Helper()
	if n := p.schema4Rows(t); n != 0 {
		t.Errorf("%s: schema_version holds %d version-4 row(s) after a refused or failed start", what, n)
	}
	if got := storedSchema(t, dataDir); got != 3 {
		t.Errorf("%s: the data directory's schema record is %d, want 3", what, got)
	}
}

// moveWithADropList is a sealed file install about to move onto Postgres
// for the first time, schema 3, with droplist.json set to body.
func moveWithADropList(t *testing.T, body []byte) (p *pgEnv, f *legacyInstall, dropList string, o Opts) {
	t.Helper()
	p = newPG(t)
	f = newLegacyInstallSealed(t)
	data := filepath.Join(f.root, "data")
	stampSchema(t, data, 3)
	dropList = filepath.Join(data, "droplist.json")
	must(t, os.WriteFile(dropList, body, 0o600))
	return p, f, dropList, Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn}
}

// ---- S1: a plaintext drop list on the first move onto Postgres -------------

// S1 (#1202 review round 3, ruling 19): the first move onto Postgres with a
// plaintext droplist.json in the data directory is refused with the
// ruling-19 wording, the same refusal the file backend gives the same file.
// No setup code is logged and the file is left byte for byte. Paired: with
// the file moved aside the start succeeds and the admin signs in.
func TestAPlaintextDropListRefusesTheFirstMoveOntoPostgres(t *testing.T) {
	p, f, dropList, o := moveWithADropList(t, []byte(plainDropList))

	code, out, _ := refused(t, o)
	if code == 0 {
		t.Errorf("exit status 0 moving a plaintext drop list onto Postgres\n%s", out)
	}
	wantRuling19Refusal(t, "plaintext droplist.json onto Postgres", out)
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged:\n%s", out)
	}
	if got := readBytes(t, dropList); string(got) != plainDropList {
		t.Errorf("the plaintext drop list was changed (sealed or rewritten): %q", got)
	}

	must(t, os.Rename(dropList, dropList+".aside"))
	a := mustStart(t, o)
	if a.client().session(t).SetupRequired {
		t.Fatal("the start with the drop list moved aside came up with an empty accounts store")
	}
	_, r := (&world{a: a, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in with the drop list moved aside", r, 200)
	_ = p
}

// ---- S2: a drop list sealed under a different key ----------------------------

// S2 (#1202 review round 3): a droplist.json sealed under a DIFFERENT key
// refuses the first move onto Postgres -- it is not a warning followed by a
// start with an empty drop list. No setup code is logged and the file is
// left byte for byte.
func TestADropListSealedUnderAnotherKeyRefusesTheFirstMoveOntoPostgres(t *testing.T) {
	p, f, dropList, o := moveWithADropList(t, nil)
	otherKeyPath := filepath.Join(f.root, "other-keys", "other.key")
	writeKey(t, otherKeyPath)
	other, err := retention.LoadKey(otherKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.Remove(dropList))
	if _, err := persist.NewEncryptedFileBackend(dropList, other).Save(context.Background(), []byte(plainDropList), 0); err != nil {
		t.Fatalf("sealing the drop list under the other key: %v", err)
	}
	sealedBefore := readBytes(t, dropList)

	code, out, _ := refused(t, o)
	if code == 0 {
		t.Errorf("exit status 0 with a drop list sealed under another key\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged:\n%s", out)
	}
	if got := readBytes(t, dropList); !bytes.Equal(got, sealedBefore) {
		t.Error("the drop list sealed under another key was changed")
	}
	_ = p
}

// ---- S3: a refused or failed move does not record schema 4 -------------------

// S3 (#1202 review round 3), a refused start: the start refused over a
// plaintext drop list records no schema 4 -- no version-4 row in
// schema_version, and the data directory's record stays at 3. Paired: the
// next start that completes the move (the file moved aside) records 4.
func TestARefusedStartOnPostgresDoesNotRecordSchema4(t *testing.T) {
	p, _, dropList, o := moveWithADropList(t, []byte(plainDropList))
	data := filepath.Dir(dropList)

	if code, out, _ := refused(t, o); code == 0 {
		t.Errorf("exit status 0\n%s", out)
	}
	wantSchemaUnstamped(t, "refused over a plaintext drop list", p, data)

	must(t, os.Rename(dropList, dropList+".aside"))
	a := mustStart(t, o)
	a.stop()
	if n := p.schema4Rows(t); n != 1 {
		t.Errorf("after the start that completed the move, schema_version holds %d version-4 row(s), want 1", n)
	}
}

// S3 (#1202 review round 3), a failed move: a move of a JSON store onto
// Postgres that fails (a damaged sealed tokens.json) records no schema 4
// either. Paired: with the file put right the next start completes the move
// and records 4.
func TestAFailedMoveOntoPostgresDoesNotRecordSchema4(t *testing.T) {
	p, f, _, o := moveWithADropList(t, nil)
	_ = os.Remove(filepath.Join(f.root, "data", "droplist.json"))
	data := filepath.Join(f.root, "data")
	tokens := filepath.Join(data, "tokens.json")
	good := readBytes(t, tokens)
	must(t, os.WriteFile(tokens, good[:len(good)/2], 0o600))

	if code, out, _ := refused(t, o); code == 0 {
		t.Errorf("exit status 0 with a damaged sealed tokens file\n%s", out)
	}
	wantSchemaUnstamped(t, "failed move of tokens.json", p, data)

	must(t, os.WriteFile(tokens, good, 0o600))
	a := mustStart(t, o)
	a.stop()
	if n := p.schema4Rows(t); n != 1 {
		t.Errorf("after the start that completed the move, schema_version holds %d version-4 row(s), want 1", n)
	}
}

// ---- S4: when the tokens row is written (ruling 20) ---------------------------

// plaintextAccountsOnPostgres: an install that has run on Postgres, then
// put back as a keyless v0.6 left it -- a plaintext accounts row, no tokens
// row, schema 3.
func plaintextAccountsOnPostgres(t *testing.T) (p *pgEnv, root, plain string) {
	t.Helper()
	p = newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})
	w.a.stop()
	ctx := context.Background()
	plain = string(newLegacyInstall(t).accounts)
	for _, q := range []string{`DELETE FROM store_blob WHERE name = 'tokens'`, `DELETE FROM schema_version WHERE version = 4`} {
		if _, err := p.conn.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.conn.Exec(ctx, `UPDATE store_blob SET payload = $1 WHERE name = 'auth'`, plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.row(t, "tokens"); ok {
		t.Fatal("test setup: the tokens row is still there")
	}
	return p, w.a.root, plain
}

// S4a (#1202 review round 3, ruling 20 and "a refused start leaves the data
// openable by the release it came from"): a server start over a plaintext
// accounts row is refused AND writes no tokens row -- a sealed tokens row
// would stop the release the data came from from opening it. Same for a
// command-line mode run first.
func TestAPlaintextAccountsRowOnPostgresIsRefusedWithoutWritingATokensRow(t *testing.T) {
	t.Run("server start", func(t *testing.T) {
		p, root, plain := plaintextAccountsOnPostgres(t)
		code, out, _ := refused(t, Opts{Root: root, PostgresDSN: p.dsn})
		if code == 0 {
			t.Errorf("exit status 0 over a plaintext accounts row\n%s", out)
		}
		if setupCodeRe.MatchString(out) {
			t.Errorf("a setup code was logged:\n%s", out)
		}
		if row, ok := p.row(t, "tokens"); ok {
			t.Errorf("a refused start wrote the tokens row: %.80s", row)
		}
		if after, _ := p.row(t, "auth"); after != plain {
			t.Error("the plaintext accounts row was sealed or rewritten")
		}
	})
	t.Run("command first", func(t *testing.T) {
		p, root, plain := plaintextAccountsOnPostgres(t)
		code, out, _ := cli(t, Opts{Root: root, PostgresDSN: p.dsn}, "saved\n", "-generate-recovery-keys")
		if code == 0 {
			t.Errorf("-generate-recovery-keys exit status 0 over a plaintext accounts row\n%s", out)
		}
		if row, ok := p.row(t, "tokens"); ok {
			t.Errorf("a refused command wrote the tokens row: %.80s", row)
		}
		if after, _ := p.row(t, "auth"); after != plain {
			t.Error("the plaintext accounts row was sealed or rewritten")
		}
	})
}

// S4b (#1202 review round 3, ruling 20): a first move onto Postgres whose
// accounts file is sealed and valid, but where a store opened later refuses
// the start (a plaintext drop list), leaves the sealed tokens row in place:
// the accounts row is sealed by then, and the tokens row is what makes the
// release it came from refuse rather than read it as an empty store.
func TestTheSealedTokensRowIsPresentAfterAMoveRefusedByALaterStore(t *testing.T) {
	p, _, _, o := moveWithADropList(t, []byte(plainDropList))
	if code, out, _ := refused(t, o); code == 0 {
		t.Fatalf("test premise: exit status 0 with a plaintext drop list\n%s", out)
	}
	if auth, ok := p.row(t, "auth"); !ok || !isSealedRow(auth) {
		t.Fatalf("test premise: the accounts row is not sealed after the move: %.60s", auth)
	}
	row, ok := p.row(t, "tokens")
	if !ok {
		t.Fatal("no tokens row after a move refused by a later store")
	}
	if !isSealedRow(row) {
		t.Errorf("the tokens row is not {\"sealed\": ...}: %.80s", row)
	}
}

// ---- S5: the tokens row written by a command alone ----------------------------
// (the test is TestTheSealedTokensRowIsWrittenWhenACommandRunsBeforeTheFirstStart
// in review1202_go2_test.go, strengthened; it opens the row with this helper)

// openSealedTokensRow opens the tokens row as the server would, under the
// install's key and the `tokens` label, and returns its token list. The row
// is copied into an in-memory backend, so nothing here touches the database.
func openSealedTokensRow(t *testing.T, payload, keyPath string) []gauntlet.Token {
	t.Helper()
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	mem := gpersist.NewMemory()
	if _, err := mem.Save(context.Background(), []byte(payload), 0); err != nil {
		t.Fatal(err)
	}
	b, err := gpersist.Encrypt(mem, key.Material(), gpersist.EncryptOptions{Label: "tokens"})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := gauntlet.OpenTokenStore(b, gauntlet.TokenOptions{})
	if err != nil {
		t.Fatalf("the tokens row does not open under the install's key: %v", err)
	}
	return ts.List()
}

// ---- S8: setup-only mode and a config file that cannot be parsed ---------------

// S8 (#1202 review round 3, ruling 16 and 17): setup-only mode over a
// config file that cannot be parsed at all (a YAML syntax error, or a root
// that is not a mapping) and an admin who holds a passkey is refused and
// says to fix the file on disk, as when the auth section or publicUrl
// cannot be read. Paired: the same broken file with no admin holding a
// passkey starts setup-only mode.
func TestSetupOnlyRefusesAnUnparseableConfigWhenAnAdminHoldsAPasskey(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]string{
		"yaml syntax error":     "auth: [unclosed\n  x: : :\n",
		"root is not a mapping": "- a\n- b\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Run("an admin holds a passkey", func(t *testing.T) {
				f := newPasskeyInstall(t)
				code, out, _ := refused(t, Opts{Root: f.root, RawConfig: cfg, KeyFromEnv: true, KeyFile: f.keyPath})
				if code == 0 {
					t.Errorf("exit status 0\n%s", out)
				}
				low := strings.ToLower(out)
				if !strings.Contains(low, "on disk") || !strings.Contains(low, "fix") {
					t.Errorf("the refusal does not say to fix the file on disk:\n%s", out)
				}
				if setupCodeRe.MatchString(out) {
					t.Errorf("a setup code was logged although start-up was refused:\n%s", out)
				}
			})
			t.Run("no admin holds a passkey", func(t *testing.T) {
				f := newLegacyInstallSealed(t)
				a := mustStart(t, Opts{Root: f.root, RawConfig: cfg, KeyFromEnv: true, KeyFile: f.keyPath})
				setupOnlyAdminCanSignIn(t, a, f)
			})
		})
	}
}
