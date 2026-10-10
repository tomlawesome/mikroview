// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1202 review fixes (note_34034 and rulings 14), written from the fix
// list before the fixes exist. Each test carries its item number
// (// #1202 review R3). R6 (the newer stored password list) and R15
// (wording) have no test here: see the commit message.
//
// Wording the fix must provide, chosen here:
//   - R13: the refusal says "history section" when that section of the
//     config file cannot be read.
//   - R10: the Warn for a re-check the limiter refused contains "re-check"
//     (or "recheck"), as gauntlet's own line does.
//   - R5: the refusal names the password-list directory's path.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// ---- helpers --------------------------------------------------------------

const reviewCIDR = "203.0.114.0/24"

// stampSchema writes the data directory's schema stamp, as a build that
// had reached schema n would have left it.
func stampSchema(t *testing.T, dataDir string, n int) {
	t.Helper()
	doc := fmt.Sprintf("{\n  \"schema\": %d,\n  \"version\": \"v0.0.0-review\",\n  \"stampedAt\": %q\n}\n", n, time.Now().UTC().Format(time.RFC3339))
	must(t, os.WriteFile(filepath.Join(dataDir, persist.SchemaDocumentName), []byte(doc), 0o600))
	if got := storedSchema(t, dataDir); got != n {
		t.Fatalf("test setup: wrote schema %d but it reads back as %d", n, got)
	}
}

func storedSchema(t *testing.T, dataDir string) int {
	t.Helper()
	n, _, err := persist.ReadFileSchema(dataDir)
	if err != nil {
		t.Fatalf("reading the stored schema: %v", err)
	}
	return int(n)
}

// richInstall is a real install that has used every sealed store: an
// account with a second factor, a token, recovery keys, sign-in history
// and a drop-list entry. It is stopped, its schema is 4 and every file is
// sealed.
type richInstall struct {
	w        *world
	tokenRaw string
	key      *retention.Key
	files    []string
	sealed   map[string][]byte
	plain    map[string][]byte
}

var richFiles = []string{"users.json", "tokens.json", "recovery.json", "signins.json", "droplist.json"}

func newRichInstall(t *testing.T) *richInstall {
	t.Helper()
	w := bootstrap(t, Opts{})
	m, r := w.mint(t, map[string]any{"name": "grafana", "kind": "api"})
	wantStatus(t, "minting a token", r, 201)
	wantStatus(t, "adding a drop-list entry", w.c.post("/api/droplist", map[string]string{"cidr": reviewCIDR, "reason": "review"}), 201)
	if code, out := w.cliOn(t, "saved\n", "-generate-recovery-keys"); code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}
	w.a.client().post("/api/auth/login", map[string]string{"username": w.admin.name, "password": "not-the-password"})
	time.Sleep(6 * time.Second) // sign-in rows are saved a few seconds after they arrive
	w.a.stop()

	key, err := retention.LoadKey(w.a.key)
	if err != nil {
		t.Fatal(err)
	}
	ri := &richInstall{w: w, tokenRaw: m.Value, key: key, files: richFiles, sealed: map[string][]byte{}, plain: map[string][]byte{}}
	for _, name := range ri.files {
		path := w.a.file(name)
		ri.sealed[name] = readBytes(t, path)
		if json.Valid(ri.sealed[name]) {
			t.Fatalf("test setup: %s is already plain JSON after use", name)
		}
		snap, err := persist.NewEncryptedFileBackend(path, key).Load(context.Background())
		if err != nil || !json.Valid(snap.Payload) {
			t.Fatalf("test setup: unsealing %s: %v", name, err)
		}
		ri.plain[name] = snap.Payload
	}
	if got := storedSchema(t, w.a.data); got != 4 {
		t.Fatalf("test setup: schema is %d after a normal start, want 4", got)
	}
	return ri
}

func (ri *richInstall) write(t *testing.T, name string, body []byte) {
	t.Helper()
	must(t, os.WriteFile(ri.w.a.file(name), body, 0o600))
}

func (ri *richInstall) opts() Opts { return Opts{Root: ri.w.a.root} }

// checkWorks signs the admin in, uses the token and reads the drop list.
func (ri *richInstall) checkWorks(t *testing.T, a *app) {
	t.Helper()
	if setupCodeRe.MatchString(a.log()) {
		t.Errorf("an install with accounts announced a setup code:\n%s", a.log())
	}
	w := &world{a: a, admin: ri.w.admin}
	c, r := w.signIn(t, ri.w.admin, ri.w.admin.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	wantStatus(t, "api token", a.client().bearer(ri.tokenRaw, "GET", "/api/stats"), 200)
	if l := c.get("/api/droplist"); l.Status != 200 || !strings.Contains(string(l.Body), reviewCIDR) {
		t.Errorf("the drop list lost its entry: %s", l)
	}
}

func (ri *richInstall) checkAllSealed(t *testing.T) {
	t.Helper()
	for _, name := range ri.files {
		raw := readBytes(t, ri.w.a.file(name))
		if json.Valid(raw) {
			t.Errorf("%s is still plain JSON", name)
		}
		if bytes.Contains(raw, []byte(reviewCIDR)) || bytes.Contains(raw, []byte("mv-admin")) {
			t.Errorf("%s holds readable content", name)
		}
	}
}

// ---- R0: sealing in place only on the first start of v0.7.0 -------------

// #1202 review R0a (ruling 14): with the stored schema at 3, plaintext
// auth files and a plaintext drop list are sealed on start-up and the
// schema becomes 4.
func TestPlaintextFilesAreSealedOnTheFirstStartAtSchema3(t *testing.T) {
	t.Parallel()
	ri := newRichInstall(t)
	for _, name := range ri.files {
		ri.write(t, name, ri.plain[name])
	}
	stampSchema(t, ri.w.a.data, 3)

	a := mustStart(t, ri.opts())
	ri.checkAllSealed(t)
	if got := storedSchema(t, a.data); got != 4 {
		t.Errorf("schema after the first start = %d, want 4", got)
	}
	ri.checkWorks(t, a)
}

// #1202 review R0b (ruling 14): with the schema at 4, a plaintext file
// refuses start-up -- whichever of the five it is. The file is left byte
// for byte, no setup code is logged and the schema stays 4. Paired with
// the sealed originals starting normally.
func TestPlaintextFileAtSchema4RefusesStartUpAndIsLeftAlone(t *testing.T) {
	t.Parallel()
	ri := newRichInstall(t)
	for _, name := range ri.files {
		ri.write(t, name, ri.plain[name])
		code, out, a := refused(t, ri.opts())
		if code == 0 {
			t.Errorf("%s plaintext at schema 4: exit status 0\n%s", name, out)
		}
		if setupCodeRe.MatchString(out) {
			t.Errorf("%s plaintext at schema 4: a setup code was logged:\n%s", name, out)
		}
		if got := readBytes(t, a.file(name)); !bytes.Equal(got, ri.plain[name]) {
			t.Errorf("%s plaintext at schema 4: the file was changed (sealed or rewritten)", name)
		}
		if got := storedSchema(t, a.data); got != 4 {
			t.Errorf("%s plaintext at schema 4: schema is now %d, want 4", name, got)
		}
		ri.write(t, name, ri.sealed[name]) // restore for the next case
	}

	a := mustStart(t, ri.opts())
	ri.checkWorks(t, a)
}

// #1202 review R0c (ruling 14): sealing fails partway when one auth file
// is damaged. The schema stays 3 and the damaged file is untouched. After
// it is restored the next start seals the rest and stamps 4.
func TestSealingThatFailsPartwayLeavesSchema3AndTheNextStartFinishes(t *testing.T) {
	t.Parallel()
	ri := newRichInstall(t)
	for _, name := range ri.files {
		ri.write(t, name, ri.plain[name])
	}
	stampSchema(t, ri.w.a.data, 3)
	damaged := []byte(`{"version":3,"seq":4,"tokens":[{"id":"a1b2`)
	ri.write(t, "tokens.json", damaged)

	code, out, a := refused(t, ri.opts())
	if code == 0 {
		t.Errorf("exit status 0 with a damaged tokens file\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged over a damaged file:\n%s", out)
	}
	if got := storedSchema(t, a.data); got != 3 {
		t.Errorf("schema after a start that failed partway = %d, want 3", got)
	}
	if got := readBytes(t, a.file("tokens.json")); !bytes.Equal(got, damaged) {
		t.Error("the damaged file was changed")
	}

	ri.write(t, "tokens.json", ri.plain["tokens.json"])
	b := mustStart(t, ri.opts())
	ri.checkAllSealed(t)
	if got := storedSchema(t, b.data); got != 4 {
		t.Errorf("schema after the restored start = %d, want 4", got)
	}
	ri.checkWorks(t, b)
}

// #1202 review R0b (Postgres): a plaintext `auth` row at schema 4 refuses
// start-up and is left as it was.
func TestPostgresPlaintextAuthRowAtSchema4RefusesStartUp(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{PostgresDSN: p.dsn})
	w.a.stop()
	before, ok := p.row(t, "auth")
	if !ok || !isSealedRow(before) {
		t.Fatalf("test setup: no sealed auth row: %.60s", before)
	}
	plain := string(newLegacyInstall(t).accounts)
	if _, err := p.conn.Exec(context.Background(), `UPDATE store_blob SET payload = $1 WHERE name = 'auth'`, plain); err != nil {
		t.Fatal(err)
	}

	code, out, _ := refused(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	if code == 0 {
		t.Errorf("exit status 0 with a plaintext auth row at schema 4\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged:\n%s", out)
	}
	if after, _ := p.row(t, "auth"); after != plain {
		t.Error("the plaintext row was sealed or rewritten")
	}
}

// ---- R1: a keyless install's plain drop list -----------------------------

// #1202 review R1: a keyless v0.6 install's plain droplist.json opens once
// a key is mounted and is sealed in place, with its entry intact. Paired
// with a damaged drop list, which fails start-up and is left alone.
func TestPlainDropListIsSealedInPlaceWhenAKeyIsMounted(t *testing.T) {
	t.Parallel()
	f := newLegacyInstall(t)
	plain := `{"entries":[{"cidr":"` + reviewCIDR + `","addedBy":"mv-admin","addedAt":"2026-01-02T03:04:05Z","reason":"keyless era"}]}`
	path := filepath.Join(f.root, "data", "droplist.json")
	must(t, os.WriteFile(path, []byte(plain), 0o600))

	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if json.Valid(readBytes(t, path)) {
		t.Error("droplist.json is still plain JSON after start-up with a key")
	}
	w := &world{a: a, admin: f.admin}
	c, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	if l := c.get("/api/droplist"); l.Status != 200 || !strings.Contains(string(l.Body), reviewCIDR) {
		t.Errorf("the drop list lost its entry: %s", l)
	}
}

func TestDamagedDropListFailsStartUpAndIsLeftAlone(t *testing.T) {
	t.Parallel()
	for name, body := range map[string][]byte{
		"truncated json": []byte(`{"entries":[{"cidr":"203.0.114.0/24"`),
		"garbage":        {0x00, 0x13, 0x37, 0xff, 0xfe},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLegacyInstall(t)
			path := filepath.Join(f.root, "data", "droplist.json")
			must(t, os.WriteFile(path, body, 0o600))
			code, out, _ := refused(t, Opts{Root: f.root, KeyFile: f.keyPath})
			if code == 0 {
				t.Errorf("exit status 0 with a damaged drop list\n%s", out)
			}
			if got := readBytes(t, path); !bytes.Equal(got, body) {
				t.Errorf("the damaged file was changed: %q", got)
			}
		})
	}
}

// ---- R3: -recover-admin-account lifts a disabled sign-in ------------------

// newSealedInstallWith is newLegacyInstallSealed with the admin's record
// changed by edit (a field of the stored account document).
func newSealedInstallWith(t *testing.T, edit func(admin map[string]any)) *legacyInstall {
	t.Helper()
	l := buildLegacy(t)
	var doc struct {
		Users []map[string]any `json:"users"`
	}
	must(t, json.Unmarshal(l.accounts, &doc))
	for _, u := range doc.Users {
		if u["username"] == l.admin.name {
			edit(u)
		}
	}
	l.accounts, _ = json.Marshal(doc)
	data := filepath.Join(l.root, "data")
	must(t, os.MkdirAll(data, 0o700))
	key, err := retention.LoadKey(l.keyPath)
	must(t, err)
	for name, body := range map[string][]byte{"users.json": l.accounts, "tokens.json": l.tokenDoc} {
		_, err := persist.NewEncryptedFileBackend(filepath.Join(data, name), key).Save(context.Background(), body, 0)
		must(t, err)
	}
	return l
}

// newDisabledInstall: the admin's sign-in is disabled (too many failed
// attempts, LoginDisableDuration not yet over).
func newDisabledInstall(t *testing.T) *legacyInstall {
	return newSealedInstallWith(t, func(u map[string]any) {
		u["loginDisabledAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		u["loginLockoutCount"] = 10
	})
}

// newPasskeyInstall: the admin holds a passkey (made-up bytes, not a
// working credential).
func newPasskeyInstall(t *testing.T) *legacyInstall {
	return newSealedInstallWith(t, func(u map[string]any) {
		u["passkeys"] = []map[string]any{{
			"id": "AQIDBAUGBwg=", "publicKey": "CQoLDA0ODxA=", "signCount": 0,
			"flags": map[string]bool{"userPresent": true, "userVerified": true},
			"rpId":  "mikroview.example.test", "name": "review key",
			"createdAt": time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
		}}
	})
}

// #1202 review R3: -recover-admin-account on an admin whose sign-in is
// disabled lifts the disable, so the admin signs in straight afterwards.
// Premise, checked first: before the command the correct password is
// refused.
func TestRecoverAdminAccountLiftsADisabledSignIn(t *testing.T) {
	t.Parallel()
	f := newDisabledInstall(t)
	o := Opts{Root: f.root, KeyFile: f.keyPath}

	a := mustStart(t, o)
	w := &world{a: a, admin: f.admin}
	if _, r := w.signIn(t, f.admin, f.admin.recovery(t)); r.Status != 429 {
		t.Fatalf("test premise: the disabled admin's correct password = %s, want 429", r)
	}
	a.stop()

	code, out, _ := cli(t, o, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}
	keys := recoveryKeys(t, out)
	const newPass = "recovered-passphrase-3Wd!"
	ra, env := prepare(t, o)
	code, out = ptyRun(t, ra, env, []ptyStep{
		{"ecovery key", 1, keys[0]},
		{"assword", 1, newPass},
		{"assword", 2, newPass},
		{"never again", 1, "saved"},
	}, "-recover-admin-account")
	if code != 0 {
		t.Fatalf("-recover-admin-account exit %d:\n%s", code, out)
	}

	b := mustStart(t, o)
	f.admin.pass = newPass
	_, r := (&world{a: b, admin: f.admin}).signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in with the recovered password straight after the command", r, 200)
}

// ---- R4: the login limiter has the auth log -------------------------------

var limiterSaveRe = regexp.MustCompile(`login limiter: (recording|updating) the login lockout`)

func waitForLimiterLine(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if limiterSaveRe.MatchString(a.log()) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("a lockout was begun while the accounts file could not be written, and the log never said so:\n%s", a.log())
}

func readOnlyData(t *testing.T, a *app) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	must(t, os.Chmod(a.data, 0o500))
	t.Cleanup(func() { _ = os.Chmod(a.data, 0o700) })
}

func wrongLogins(a *app, username string, n int) {
	c := a.client()
	for i := 0; i < n; i++ {
		c.post("/api/auth/login", map[string]string{"username": username, "password": fmt.Sprintf("wrong-password-%d", i)})
	}
}

// #1202 review R4, normal start: a failed save of a lockout is logged.
// Premise: with the data directory read-only an account cannot be created.
func TestFailedSaveOfALockoutIsLoggedAtNormalStartUp(t *testing.T) {
	w := bootstrap(t, Opts{})
	readOnlyData(t, w.a)
	if r := w.c.post("/api/auth/users", map[string]string{"username": "probe", "password": "pw-probe-9X7q!", "role": "user"}); r.Status < 500 {
		t.Fatalf("test premise: creating an account with a read-only data directory = %s, want a server error", r)
	}
	wrongLogins(w.a, w.admin.name, 12)
	waitForLimiterLine(t, w.a)
}

// #1202 review R4, setup-only mode: the same.
func TestFailedSaveOfALockoutIsLoggedInSetupOnlyMode(t *testing.T) {
	f := newLegacyInstallSealed(t)
	a := mustStart(t, Opts{Root: f.root, RawConfig: invalidConfig, KeyFromEnv: true, KeyFile: f.keyPath})
	readOnlyData(t, a)
	wrongLogins(a, f.admin.name, 12)
	waitForLimiterLine(t, a)
}

// ---- R5: the password-list directory -------------------------------------

// #1202 review R5: a password-list folder that cannot be created stops
// start-up with a message naming it; a fresh data folder gets it created.
func TestPasswordListFolderThatCannotBeCreatedStopsStartUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "data"), 0o700))
	blocker := filepath.Join(root, "data", "password-list")
	must(t, os.WriteFile(blocker, []byte("a file where the folder should be"), 0o600))

	code, out, _ := refused(t, Opts{Root: root})
	if code == 0 {
		t.Errorf("exit status 0\n%s", out)
	}
	if !strings.Contains(out, blocker) {
		t.Errorf("the refusal does not name %s:\n%s", blocker, out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged although start-up was refused:\n%s", out)
	}

	// Paired: a fresh data folder gets the directory at start.
	fresh := mustStart(t, Opts{})
	st, err := os.Stat(filepath.Join(fresh.data, "password-list"))
	if err != nil || !st.IsDir() {
		t.Errorf("a fresh data folder has no password-list directory after start: %v", err)
	}
}

// ---- R9 and R10: the password re-check routes -----------------------------

const recheckPath = "/api/config/editor/open"

func (w *world) recheck(password string) resp {
	return w.c.post(recheckPath, map[string]string{"password": password})
}

// hitRecheckLimit makes wrong guesses until the route answers 429.
func (w *world) hitRecheckLimit(t *testing.T) int {
	t.Helper()
	for i := 1; i <= 40; i++ {
		switch r := w.recheck("not-the-password"); r.Status {
		case 429:
			return i
		case 401:
		default:
			t.Fatalf("a wrong password at %s = %s, want 401 until the limit", recheckPath, r)
		}
	}
	t.Fatalf("40 wrong passwords at %s were never rate-limited", recheckPath)
	return 0
}

// #1202 review R9: re-check attempts are stamped with the server's clock.
// After the limit is hit, moving the (test) clock past the window frees
// the account; stamped with the real clock instead, the attempts would
// never age. Paired: before the clock moves, the right password is
// refused too.
func TestRecheckAttemptsAgeOnTheServersClock(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	w.hitRecheckLimit(t)
	if r := w.recheck(w.admin.pass); r.Status != 429 {
		t.Fatalf("the right password inside the window = %s, want 429", r)
	}
	w.advance(t, 70*time.Minute)
	if r := w.recheck(w.admin.pass); r.Status != 200 {
		t.Errorf("the right password after the clock moved past the window = %s, want 200", r)
	}
}

func loginFailedRecheckEntries(t *testing.T, w *world) []map[string]any {
	t.Helper()
	r := w.c.get("/api/audit?limit=500")
	wantStatus(t, "GET /api/audit", r, 200)
	var res struct {
		Entries []map[string]any `json:"entries"`
	}
	r.json(t, &res)
	var out []map[string]any
	for _, e := range res.Entries {
		if e["action"] == "user.login_failed" && strings.Contains(fmt.Sprint(e["detail"]), "step=recheck") {
			out = append(out, e)
		}
	}
	return out
}

// #1202 review R10: a wrong password at a re-check route writes a
// user.login_failed audit record marked step=recheck; a right one writes
// none. Hitting the limit logs a Warn.
func TestWrongRecheckPasswordIsAuditedAndTheLimitLogsAWarn(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})

	wantStatus(t, "right password", w.recheck(w.admin.pass), 200)
	if n := len(loginFailedRecheckEntries(t, w)); n != 0 {
		t.Fatalf("a right password wrote %d user.login_failed record(s)", n)
	}
	warnsBefore := len(w.a.logWarns())

	// These routes keep the plain-text body the frontend matches on.
	if r := w.recheck("not-the-password"); r.Status != 401 || strings.TrimSpace(string(r.Body)) != "incorrect password" {
		t.Errorf("a wrong password at %s = %s, want 401 incorrect password", recheckPath, r)
	}
	entries := loginFailedRecheckEntries(t, w)
	if len(entries) != 1 {
		t.Fatalf("a wrong password at %s wrote %d user.login_failed step=recheck record(s), want 1: %v", recheckPath, len(entries), entries)
	}
	if entries[0]["target"] != w.admin.name {
		t.Errorf("the record's target = %v, want %q", entries[0]["target"], w.admin.name)
	}
	if strings.Contains(fmt.Sprint(entries[0]["detail"]), "not-the-password") {
		t.Error("the record holds the password that was tried")
	}

	w.hitRecheckLimit(t)
	rx := regexp.MustCompile(`(?i)re-?check`)
	var warns []string
	for _, l := range w.a.logWarns()[warnsBefore:] {
		if rx.MatchString(l) {
			warns = append(warns, l)
		}
	}
	if len(warns) == 0 {
		t.Errorf("hitting the re-check limit logged no Warn mentioning the re-check:\n%s", strings.Join(w.a.logWarns()[warnsBefore:], "\n"))
	}
}

// ---- R11: the CSRF header on mikroview's own routes -------------------------

// #1202 review R11: a mutating request to a mikroview-owned route with a
// valid session but no CSRF header is 403 csrf-required and changes
// nothing. Paired with the same request carrying the header.
func TestMutatingRequestWithoutTheCSRFHeaderIsRefusedOnMikroviewRoutes(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	bare := &client{a: w.a, hc: w.c.hc, jar: w.c.jar, noCSRF: true}
	if s := bare.session(t); !s.Authenticated {
		t.Fatal("test premise: the cookie-only client is not signed in")
	}

	wantProblem(t, "POST /api/droplist without the header", bare.post("/api/droplist", map[string]string{"cidr": reviewCIDR, "reason": "x"}), 403, "csrf-required")
	wantProblem(t, "POST /api/flags/clear-all without the header", bare.post("/api/flags/clear-all", map[string]any{}), 403, "csrf-required")
	wantProblem(t, "DELETE /api/settings/history/files without the header", bare.del("/api/settings/history/files", map[string]string{"password": w.admin.pass}), 403, "csrf-required")
	if l := w.c.get("/api/droplist"); strings.Contains(string(l.Body), reviewCIDR) {
		t.Error("a refused request still created the drop-list entry")
	}
	wantStatus(t, "GET without the header", bare.get("/api/droplist"), 200)

	wantStatus(t, "POST /api/droplist with the header", w.c.post("/api/droplist", map[string]string{"cidr": reviewCIDR, "reason": "x"}), 201)
}

// ---- R13 and R14: setup-only mode and -validate-config -----------------------

// #1202 review R13: setup-only mode with a history section it cannot read
// says so and names the environment variable and the app folder's key
// path, rather than "no key is mounted". Paired: a readable section with
// no key keeps the original message.
func TestSetupOnlyWithAnUnreadableHistorySectionSaysSo(t *testing.T) {
	t.Parallel()
	code, out, a := refused(t, Opts{AppFolder: true, NoKey: true, RawConfig: "history: notamap\n"})
	if code == 0 {
		t.Errorf("exit status 0\n%s", out)
	}
	i := strings.Index(out, "refusing to start the config editor")
	if i < 0 {
		t.Fatalf("setup-only mode never reached its refusal:\n%s", out)
	}
	tail := out[i:]
	if strings.Contains(tail, "no key is mounted") {
		t.Errorf("the refusal says no key is mounted although the history section could not be read:\n%s", tail)
	}
	if !strings.Contains(strings.ToLower(tail), "history section") {
		t.Errorf("the refusal does not say the history section could not be read:\n%s", tail)
	}
	if !strings.Contains(tail, "MIKROVIEW_HISTORY_KEY_FILE") {
		t.Errorf("the refusal does not name MIKROVIEW_HISTORY_KEY_FILE:\n%s", tail)
	}
	if want := filepath.Join(a.root, "appfolder", "keys", "history.key"); !strings.Contains(tail, want) {
		t.Errorf("the refusal does not name the app folder path %s:\n%s", want, tail)
	}

	_, plain, _ := refused(t, Opts{AppFolder: true, NoKey: true, RawConfig: invalidConfig})
	if !strings.Contains(plain, "no key is mounted") {
		t.Errorf("a readable config with no key lost the \"no key is mounted\" message:\n%s", plain)
	}
}

// #1202 review R14: -validate-config reports a missing key (CFG-0083) and
// exits non-zero; with a key it exits 0.
func TestValidateConfigReportsAMissingKey(t *testing.T) {
	t.Parallel()
	code, out, _ := cli(t, Opts{NoKey: true}, "", "-validate-config")
	if code == 0 {
		t.Errorf("-validate-config with no key exited 0:\n%s", out)
	}
	if !strings.Contains(out, "CFG-0083") {
		t.Errorf("-validate-config does not report CFG-0083:\n%s", out)
	}
	code, out, _ = cli(t, Opts{}, "", "-validate-config")
	if code != 0 || strings.Contains(out, "CFG-0083") {
		t.Errorf("-validate-config with a key: exit %d, output:\n%s", code, out)
	}
}

// ---- R2 end to end: moving a sealed file install onto Postgres ---------------

// #1202 review R2: start a sealed file install against an empty Postgres.
// Each document arrives readable to the stores: the auth rows are sealed
// under their labels, the drop list is stored as before (plain JSON), and
// the account signs in.
func TestSealedFileInstallMovesOntoPostgres(t *testing.T) {
	p := newPG(t)
	w := bootstrap(t, Opts{})
	m, r := w.mint(t, map[string]any{"name": "grafana", "kind": "api"})
	wantStatus(t, "minting a token", r, 201)
	wantStatus(t, "adding a drop-list entry", w.c.post("/api/droplist", map[string]string{"cidr": reviewCIDR, "reason": "review"}), 201)
	time.Sleep(3 * time.Second)
	w.a.stop()

	a := mustStart(t, Opts{Root: w.a.root, PostgresDSN: p.dsn})
	if a.client().session(t).SetupRequired {
		t.Fatal("the moved install reads as empty")
	}
	_, r = (&world{a: a, admin: w.admin}).signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "sign-in after the move", r, 200)
	wantStatus(t, "api token after the move", a.client().bearer(m.Value, "GET", "/api/stats"), 200)

	for _, name := range []string{"auth", "tokens"} {
		payload, ok := p.row(t, name)
		if !ok || !isSealedRow(payload) {
			t.Errorf("the %q row is not {\"sealed\": ...} after the move: %.80s", name, payload)
		}
	}
	rows, err := p.conn.Query(context.Background(), `SELECT name, payload FROM store_blob`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var name, payload string
		if err := rows.Scan(&name, &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload, reviewCIDR) {
			found = true
			if !json.Valid([]byte(payload)) || isSealedRow(payload) {
				t.Errorf("the drop-list row %q does not hold the plain document: %.80s", name, payload)
			}
		}
	}
	if !found {
		t.Error("no row holds the drop-list entry after the move")
	}
}

// ---- R17: setup-only mode and the admin passkey rule -------------------------

// #1202 review R17 (ruling 16), with an admin holding a passkey and
// auth.adminPasskey not set: setup-only mode cannot work out the rule if
// publicUrl (or the auth section) cannot be read, so it refuses to start
// and says to fix the file on disk. Paired (c): the same broken publicUrl
// with no admin holding a passkey starts setup-only mode as before.
func TestSetupOnlyRefusesWhenItCannotReadThePasskeyRuleAndAnAdminHoldsAPasskey(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]struct{ cfg, field string }{
		"publicUrl unreadable":    {"publicUrl:\n  not: a-string\n", "publicUrl"},
		"auth section unreadable": {"auth: not-a-map\n", "auth"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPasskeyInstall(t)
			code, out, _ := refused(t, Opts{Root: f.root, RawConfig: raw.cfg, KeyFromEnv: true, KeyFile: f.keyPath})
			if code == 0 {
				t.Errorf("exit status 0\n%s", out)
			}
			low := strings.ToLower(out)
			if !strings.Contains(low, "on disk") || !strings.Contains(low, "fix") {
				t.Errorf("the refusal does not say to fix the file on disk:\n%s", out)
			}
			if !strings.Contains(out, raw.field) {
				t.Errorf("the refusal does not name %s:\n%s", raw.field, out)
			}
		})
	}

	t.Run("no admin holds a passkey", func(t *testing.T) {
		f := newLegacyInstallSealed(t)
		a := mustStart(t, Opts{Root: f.root, RawConfig: "publicUrl:\n  not: a-string\n", KeyFromEnv: true, KeyFile: f.keyPath})
		setupOnlyAdminCanSignIn(t, a, f)
	})
}
