// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1202 second review, Go pass (notes 34148, 34149, 34208 and 34209): the
// items R20 to R26 and rulings 19 and 20, written before the fixes exist.
// Each test carries its item (// #1202 review R21).
//
// Names and wording the fix must provide, chosen here:
//   - R26 (ruling 19): every plaintext refusal -- start-up, setup-only mode
//     and the command-line modes alike -- says the data was written
//     "without a key before v0.7.0", and to "restore a sealed copy" or
//     "move it aside" (case does not matter), and no longer says "first
//     start of v0.7.0", "a normal start" or that a start "seals it".
//   - R20: a start whose schema stamp cannot be written exits non-zero and
//     its output mentions the schema.
//   - R25: the four commands refuse a file data directory whose schema.json
//     is newer than the build with the SchemaTooNewError text: it names the
//     build that wrote it ("v42.0.0"), the data directory, "that build or a
//     newer one" and "Refusing to start" -- the text persist's own tests
//     already hold it to.
//   - Opts.FileSizeLimit (harness_test.go) is how R20 makes the stamp write
//     fail: the process may not write a file bigger than 64 bytes.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// ---- shared ---------------------------------------------------------------

// wantRuling19Refusal checks the text an operator reads when a plaintext
// auth document or drop list refuses start-up (ruling 19).
func wantRuling19Refusal(t *testing.T, what, out string) {
	t.Helper()
	low := strings.ToLower(out)
	for _, w := range []string{"without a key", "v0.7.0", "restore a sealed copy", "move it aside"} {
		if !strings.Contains(low, w) {
			t.Errorf("%s: the refusal does not say %q:\n%s", what, w, out)
		}
	}
	for _, w := range []string{"first start of v0.7.0", "a normal start", "seals it", "will seal"} {
		if strings.Contains(low, w) {
			t.Errorf("%s: the refusal still says %q:\n%s", what, w, out)
		}
	}
}

// dataDigest is a hash of every regular file under dir (names and bytes),
// to show that a refused run wrote nothing.
func dataDigest(t *testing.T, dir string) string {
	t.Helper()
	var names []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, n)
		fmt.Fprintf(h, "%s %d\n", rel, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// blob reads a store_blob row: its payload and version column.
func (p *pgEnv) blob(t *testing.T, name string) (payload string, version int64, ok bool) {
	t.Helper()
	err := p.conn.QueryRow(context.Background(), `SELECT payload, version FROM store_blob WHERE name = $1`, name).Scan(&payload, &version)
	return payload, version, err == nil
}

// waitForSealedRow polls for a row to exist and read {"sealed": ...}.
func (p *pgEnv) waitForSealedRow(t *testing.T, name string) (string, int64) {
	t.Helper()
	var payload string
	var version int64
	var ok bool
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if payload, version, ok = p.blob(t, name); ok {
			break
		}
	}
	if !ok {
		t.Fatalf("no %q row", name)
	}
	if !isSealedRow(payload) {
		t.Fatalf("the %q row does not read {\"sealed\": ...}: %.80s", name, payload)
	}
	return payload, version
}

// ---- R20: the schema stamp -------------------------------------------------

// #1202 review R20: when the schema-4 stamp cannot be written, start-up
// refuses with the error, instead of logging a Warn and carrying on at
// schema 3. The write is made to fail with a file-size limit on the
// process (Opts.FileSizeLimit), so only the writes that are bigger than 64
// bytes fail -- the stamp is the first of them. The data is left as it
// was and the schema stays 3. Paired: the same install without the limit
// starts and stamps 4, and with the schema already at 4 the limit alone
// does not stop a start (nothing needs the stamp).
//
// Not parallel: Opts.FileSizeLimit lowers a limit on this process for the
// instant of the fork.
func TestAFailureToWriteTheSchemaStampRefusesStartUp(t *testing.T) {
	f := newLegacyInstallSealed(t)
	data := filepath.Join(f.root, "data")
	stampSchema(t, data, 3)
	usersBefore := readBytes(t, filepath.Join(data, "users.json"))
	tokensBefore := readBytes(t, filepath.Join(data, "tokens.json"))

	code, out, a := refused(t, Opts{Root: f.root, KeyFile: f.keyPath, FileSizeLimit: 64})
	if code == 0 {
		t.Errorf("exit status 0 although the schema stamp could not be written\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "schema") {
		t.Errorf("the refusal does not mention the schema:\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged although start-up was refused:\n%s", out)
	}
	if got := storedSchema(t, data); got != 3 {
		t.Errorf("schema after the failed stamp = %d, want 3", got)
	}
	if !bytes.Equal(readBytes(t, a.file("users.json")), usersBefore) || !bytes.Equal(readBytes(t, a.file("tokens.json")), tokensBefore) {
		t.Error("an auth file was changed by the refused start")
	}

	// Paired: without the limit the same install starts, signs in and is
	// stamped 4.
	b := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if got := storedSchema(t, data); got != 4 {
		t.Errorf("schema after a start with no limit = %d, want 4", got)
	}
	_, r := (&world{a: b, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in after the stamp was written", r, 200)
	b.stop()

	// Paired: at schema 4 there is no stamp to write, and the limit alone
	// does not stop the start.
	c := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath, FileSizeLimit: 64})
	c.stop()
}

// ---- R21 (ruling 20): the sealed tokens row ---------------------------------

// #1202 review R21 (ruling 20): the first v0.7 start on Postgres writes the
// sealed `tokens` row even though no token was ever minted, so that a
// v0.6.x binary, which cannot parse it, refuses to start rather than
// reading the sealed accounts row as an empty store. The row reads
// {"sealed": ...}. Paired: a later start leaves it exactly as it is (same
// payload, same version column).
func TestTheFirstStartWritesTheSealedTokensRowWithNoTokensEverMinted(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn}) // an admin, no token
	payload, version := p.waitForSealedRow(t, "tokens")
	if strings.Contains(payload, "mv-admin") {
		t.Error("the tokens row holds the username in the clear")
	}
	if auth, ok := p.row(t, "auth"); !ok || !isSealedRow(auth) {
		t.Fatalf("test premise: no sealed auth row: %.60s", auth)
	}
	w.a.stop()

	// A later start (and a sign-in on it) leaves the row alone.
	b := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	_, r := (&world{a: b, admin: w.admin}).signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "sign-in on the later start", r, 200)
	time.Sleep(2 * time.Second) // time for anything the start might write
	b.stop()
	after, afterVersion, ok := p.blob(t, "tokens")
	if !ok || after != payload || afterVersion != version {
		t.Errorf("a later start changed the tokens row (version %d -> %d)", version, afterVersion)
	}
}

// #1202 review R21 (ruling 20), a command first: with the database as it
// is before the first v0.7 start -- the sealed accounts row, the schema at
// 3 and no tokens row -- a command-line mode runs first (it moves the
// schema to 4), and the first start after it still ends with the sealed
// tokens row in place. Paired: yet another start leaves it as it is.
// (Round 3, S5: and the command alone, before any start, has written it.)
func TestTheSealedTokensRowIsWrittenWhenACommandRunsBeforeTheFirstStart(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})
	p.waitForSealedRow(t, "tokens")
	w.a.stop()
	ctx := context.Background()
	if _, err := p.conn.Exec(ctx, `DELETE FROM store_blob WHERE name = 'tokens'`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.conn.Exec(ctx, `DELETE FROM schema_version WHERE version = 4`); err != nil {
		t.Fatal(err)
	}

	code, out, _ := cli(t, Opts{Root: w.a.root, PostgresDSN: p.dsn}, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}
	// #1202 review round 3, S5: the command alone wrote the row. No server
	// has been started since, so it is there the moment the command
	// returns (no waiting), sealed, and opens under the install's key to
	// an empty token list.
	if row, _, ok := p.blob(t, "tokens"); !ok {
		t.Fatal("the command wrote no tokens row before any server start")
	} else if !isSealedRow(row) {
		t.Fatalf("the tokens row the command wrote is not {\"sealed\": ...}: %.80s", row)
	} else if toks := openSealedTokensRow(t, row, w.a.key); len(toks) != 0 {
		t.Errorf("the tokens row opens with %d token(s), want an empty list", len(toks))
	}
	a := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	payload, version := p.waitForSealedRow(t, "tokens")
	if s := a.client().session(t); s.SetupRequired {
		t.Error("the install reads as empty after the command and the start")
	}
	a.stop()

	b := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	time.Sleep(2 * time.Second)
	b.stop()
	after, afterVersion, ok := p.blob(t, "tokens")
	if !ok || after != payload || afterVersion != version {
		t.Errorf("a later start changed the tokens row (version %d -> %d)", version, afterVersion)
	}
}

// ---- R22: a failed first adoption onto Postgres is retried -----------------

// #1202 review R22: a sealed file install moved onto Postgres, with one
// damaged sealed file (tokens.json), refuses to start; the adoption is not
// recorded as done, so after the file is put right the next start adopts
// everything: the accounts are there, no setup code is logged, and the API
// token still works. No start in between comes up empty or memory-only.
func TestAFailedFirstMoveOntoPostgresIsRetriedAtTheNextStart(t *testing.T) {
	p := newPG(t)
	f := newLegacyInstallSealed(t)
	tokens := filepath.Join(f.root, "data", "tokens.json")
	good := readBytes(t, tokens)
	damaged := good[:len(good)/2]
	must(t, os.WriteFile(tokens, damaged, 0o600))
	o := Opts{Root: f.root, KeyFile: f.keyPath, PostgresDSN: p.dsn}

	code, out, a := refused(t, o)
	if code == 0 {
		t.Errorf("exit status 0 with a damaged sealed tokens file\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged over a damaged file:\n%s", out)
	}
	if got := readBytes(t, tokens); !bytes.Equal(got, damaged) {
		t.Error("the damaged file was changed")
	}
	for _, name := range dataFiles(t, a) {
		low := strings.ToLower(name)
		if strings.Contains(low, "postgres") || strings.Contains(low, "adopt") {
			t.Errorf("%s was written although the move onto Postgres failed: the adoption is marked done", name)
		}
	}

	// The file is put right; the next start finishes the move.
	must(t, os.WriteFile(tokens, good, 0o600))
	b := mustStart(t, o)
	if setupCodeRe.MatchString(b.log()) {
		t.Errorf("the retried start announced a setup code:\n%s", b.log())
	}
	if b.client().session(t).SetupRequired {
		t.Fatal("the retried start came up with an empty accounts store")
	}
	w := &world{a: b, admin: f.admin}
	_, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in after the retried move", r, 200)
	wantStatus(t, "api token after the retried move", b.client().bearer(f.tokens["api"], "GET", "/api/stats"), 200)
	for _, name := range []string{"auth", "tokens"} {
		if row, ok := p.row(t, name); !ok || !isSealedRow(row) {
			t.Errorf("the %q row is not {\"sealed\": ...} after the retried move: %.80s", name, row)
		}
	}
}

// ---- R23: setup-only mode ---------------------------------------------------

// #1202 review R23 (ruling 19): setup-only mode's refusal of a plaintext
// accounts file uses the ruling-19 wording and no longer tells the
// operator that a normal start will seal the file, at either schema. The
// file is left byte for byte and no setup code is logged. Paired: with the
// sealed original back, setup-only mode starts and the admin signs in.
func TestSetupOnlyModeRefusesAPlaintextAccountsFileWithTheRuling19Wording(t *testing.T) {
	t.Parallel()
	for _, schema := range []int{3, 4} {
		t.Run(fmt.Sprintf("schema %d", schema), func(t *testing.T) {
			f := newLegacyInstallSealed(t)
			data := filepath.Join(f.root, "data")
			stampSchema(t, data, schema)
			sealed := readBytes(t, filepath.Join(data, "users.json"))
			must(t, os.WriteFile(filepath.Join(data, "users.json"), f.accounts, 0o600))

			code, out, a := refused(t, Opts{Root: f.root, RawConfig: invalidConfig, KeyFromEnv: true, KeyFile: f.keyPath})
			if code == 0 {
				t.Errorf("exit status 0\n%s", out)
			}
			wantRuling19Refusal(t, "setup-only mode", out)
			if setupCodeRe.MatchString(out) {
				t.Errorf("a setup code was logged:\n%s", out)
			}
			if got := readBytes(t, a.file("users.json")); !bytes.Equal(got, f.accounts) {
				t.Error("the plaintext accounts file was changed (sealed or rewritten)")
			}
			if got := storedSchema(t, data); got != schema {
				t.Errorf("schema is now %d, want %d", got, schema)
			}

			must(t, os.WriteFile(filepath.Join(data, "users.json"), sealed, 0o600))
			b := mustStart(t, Opts{Root: f.root, RawConfig: invalidConfig, KeyFromEnv: true, KeyFile: f.keyPath})
			setupOnlyAdminCanSignIn(t, b, f)
		})
	}
}

// ---- R24: a non-auth store's plaintext file ---------------------------------

// #1202 review R24: a plaintext file for a store that is not an auth store
// (flags.json here) refuses start-up and is left alone, and the refusal
// does not claim it is "the first start of v0.7.0". Paired: with the file
// moved aside the install starts.
func TestAPlaintextFileForAStoreThatIsNotAnAuthStoreRefusesWithoutClaimingTheFirstStart(t *testing.T) {
	t.Parallel()
	f := newLegacyInstallSealed(t)
	path := filepath.Join(f.root, "data", "flags.json")
	plain := []byte(`{"flags":[]}`)
	must(t, os.WriteFile(path, plain, 0o600))

	code, out, _ := refused(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if code == 0 {
		t.Errorf("exit status 0 with a plaintext flags file\n%s", out)
	}
	if !strings.Contains(out, "flags.json") {
		t.Errorf("the refusal does not name flags.json:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "first start of v0.7.0") {
		t.Errorf("the refusal of a non-auth store's file claims the first start of v0.7.0:\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged:\n%s", out)
	}
	if got := readBytes(t, path); !bytes.Equal(got, plain) {
		t.Error("the plaintext flags file was changed")
	}

	must(t, os.Rename(path, path+".aside"))
	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	wantStatus(t, "sign-in with the file moved aside", func() resp {
		_, r := (&world{a: a, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
		return r
	}(), 200)
}

// ---- R25: the four commands and a newer file schema -------------------------

// stampNewerSchema writes a schema.json from a build newer than this one.
func stampNewerSchema(t *testing.T, dataDir string) {
	t.Helper()
	doc := fmt.Sprintf("{\n  \"schema\": %d,\n  \"version\": \"v42.0.0\",\n  \"stampedAt\": %q\n}\n", persist.CurrentSchema()+1, time.Now().UTC().Format(time.RFC3339))
	must(t, os.WriteFile(filepath.Join(dataDir, persist.SchemaDocumentName), []byte(doc), 0o600))
}

// #1202 review R25: each of the four commands that open the store refuses
// a file data directory whose schema.json is newer than the build, with
// the SchemaTooNewError text (the build that wrote it, the directory,
// "that build or a newer one", "Refusing to start"), exits non-zero, logs
// no setup code and writes nothing -- not a file, not a byte. Paired: with
// the stamp put back to this build's schema the same install runs
// -generate-recovery-keys.
func TestTheFourCommandsRefuseAFileDataDirectoryWithANewerSchemaAndWriteNothing(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	w.a.stop()
	data := w.a.data
	stampNewerSchema(t, data)
	before := dataDigest(t, data)

	for _, args := range [][]string{
		{"-recover-admin-account"},
		{"-transfer-admin", "heir"},
		{"-clear-second-factor", "someone"},
		{"-generate-recovery-keys"},
	} {
		code, out, _ := cli(t, Opts{Root: w.a.root}, "ANYRECOVERYKEYANYRECOVERYKEY1234\nsaved\n", args...)
		if code == 0 {
			t.Errorf("%v exited 0 on a data directory from a newer build:\n%s", args, out)
		}
		for _, want := range []string{"v42.0.0", data, "that build or a newer one", "Refusing to start"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: the refusal does not mention %q:\n%s", args, want, out)
			}
		}
		if setupCodeRe.MatchString(out) {
			t.Errorf("%v: a setup code was printed:\n%s", args, out)
		}
		if after := dataDigest(t, data); after != before {
			t.Errorf("%v: a refused command changed the data directory", args)
		}
	}

	stampSchema(t, data, int(persist.CurrentSchema()))
	code, out, _ := cli(t, Opts{Root: w.a.root}, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Errorf("-generate-recovery-keys on a data directory at this build's schema: exit %d:\n%s", code, out)
	}
}
