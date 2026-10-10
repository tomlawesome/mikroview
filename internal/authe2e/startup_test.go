// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// Build notes B1 (start-up and configuration), B8 35/36 (sealing, file
// backend), B9 40/41/44.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// #1202 B1.1: with no accounts store document, start-up logs one Warn line
// matching the setup-code wording, and GET /api/auth/session answers
// setupRequired: true. Paired: an install that already has an account
// logs none.
func TestNoAccountsLogsOneSetupCodeWarnAndSessionSaysSetupRequired(t *testing.T) {
	t.Parallel()
	a := mustStart(t, Opts{})
	code := a.setupCode(t)
	if !regexp.MustCompile(`^[A-Za-z0-9-]+$`).MatchString(code) {
		t.Errorf("setup code %q is not the display form", code)
	}
	var lines []string
	for _, l := range strings.Split(a.log(), "\n") {
		if regexp.MustCompile(`create the first admin with setup code [A-Z0-9-]+`).MatchString(l) {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("%d setup-code lines in the log, want exactly 1:\n%s", len(lines), a.log())
	}
	if !strings.Contains(lines[0], "WARN") {
		t.Errorf("the setup code is not logged at Warn: %s", lines[0])
	}
	// scripts/setup-code.sh, live-setup-code.mjs and docs/configuration.md
	// match this wording; the handler keeps it (design record, decision 3).
	if !strings.Contains(lines[0], "(valid until an account exists or this process restarts)") {
		t.Errorf("the setup-code line has lost its wording: %s", lines[0])
	}
	if s := a.client().session(t); !s.SetupRequired || s.Authenticated {
		t.Errorf("GET /api/auth/session = %+v, want setupRequired true", s)
	}

	// Paired: after the admin exists a restart announces nothing.
	_, r := register(t, a, "mv-admin", adminPassword)
	wantStatus(t, "register", r, 201)
	b := a.restart(t, nil)
	time.Sleep(500 * time.Millisecond)
	if setupCodeRe.MatchString(b.log()) {
		t.Errorf("an install with an account announced a setup code:\n%s", b.log())
	}
	if s := b.client().session(t); s.SetupRequired {
		t.Error("setupRequired is true on an install that has an account")
	}
}

// #1202 B1.2: register with the logged code creates an admin (201); a wrong
// code is 401 invalid-credentials; once an account exists it is 409.
func TestRegisterWithTheLoggedCodeCreatesAnAdminAndWrongCodeIsRefused(t *testing.T) {
	t.Parallel()
	a := mustStart(t, Opts{})
	good := a.setupCode(t)
	c := a.client()

	wrong := c.post("/api/auth/register", map[string]string{"username": "mv-admin", "password": adminPassword, "setupCode": "AAAA-BBBB-CCCC-DDDD"})
	wantProblem(t, "register with a wrong setup code", wrong, 401, "invalid-credentials")
	if s := c.session(t); !s.SetupRequired {
		t.Fatal("a wrong setup code created an account")
	}

	ok := c.post("/api/auth/register", map[string]string{"username": "mv-admin", "password": adminPassword, "setupCode": good})
	wantStatus(t, "register with the right setup code", ok, 201)
	var acc struct{ Username, Role string }
	ok.json(t, &acc)
	if acc.Username != "mv-admin" || acc.Role != "admin" {
		t.Errorf("register answered %+v, want {mv-admin admin}", acc)
	}
	if ok.cookie("mikroview_session") == nil && ok.cookie("__Host-mikroview_session") == nil {
		t.Error("register set no session cookie")
	}

	again := a.client().post("/api/auth/register", map[string]string{"username": "second", "password": adminPassword, "setupCode": good})
	wantProblem(t, "register once an account exists", again, 409, "conflict")
}

// #1202 B1.3 (file backend): no history.keyFile means start-up is refused
// with a CFG code and a message that says how to mount a key; nothing is
// written to the data directory and no setup code is logged. Paired with
// the same configuration plus a key starting normally.
func TestNoKeyRefusesStartUpWithACFGCodeAndHowToMountOne(t *testing.T) {
	t.Parallel()
	code, out, a := refused(t, Opts{NoKey: true})
	if code == 0 {
		t.Errorf("exit status 0, want non-zero\n%s", out)
	}
	found := cfgCodeRe.FindString(out)
	if found == "" {
		t.Fatalf("the refusal carries no CFG code:\n%s", out)
	}
	if !documentedCFGCodes(t)[found] {
		t.Errorf("%s is not registered in docs/configuration.md's problem codes", found)
	}
	for _, want := range []string{"history.keyFile", "history.key"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not mention %q, so it does not say how to mount a key:\n%s", want, out)
		}
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged although start-up was refused:\n%s", out)
	}
	if files := dataFiles(t, a); len(files) != 0 {
		t.Errorf("start-up was refused but wrote to the data directory: %v", files)
	}

	// Paired: with a key it starts, and announces its setup code.
	ok := mustStart(t, Opts{})
	ok.setupCode(t)
}

// #1202 B1.4: with a key the accounts file is sealed (not valid JSON) and
// re-opens with the same key; a different key fails start-up and never
// reads as an empty store (no setup code, file untouched).
func TestAccountsFileIsSealedReopensWithTheSameKeyAndNotWithAnother(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	w.a.stop()

	raw := readBytes(t, w.a.file("users.json"))
	if json.Valid(raw) {
		t.Fatalf("the accounts file is valid JSON; it was not sealed:\n%.200s", raw)
	}
	if bytes.Contains(raw, []byte("mv-admin")) {
		t.Error("the accounts file holds the username in the clear")
	}

	// Same key: the account is there and signs in.
	b := mustStart(t, Opts{Root: w.a.root})
	if s := b.client().session(t); s.SetupRequired {
		t.Fatal("re-opening with the same key reads as an empty store")
	}
	wb := &world{a: b, admin: w.admin}
	_, r := wb.signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "sign-in after a re-open", r, 200)
	b.stop()

	// Another key: refused, nothing changed.
	before := readBytes(t, b.file("users.json"))
	other := filepath.Join(b.root, "keys", "other.key")
	code, out, _ := refused(t, Opts{Root: b.root, KeyFile: other})
	if code == 0 {
		t.Errorf("exit status 0 under a different key\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a different key announced a setup code, as if the store were empty:\n%s", out)
	}
	if after := readBytes(t, b.file("users.json")); !bytes.Equal(before, after) {
		t.Error("a start-up under a different key changed the accounts file")
	}
}

// #1202 B1.6: auth.sessionTTL: 24h and sessionMaxLifetime: 168h start with
// no Warn about them and the effective limits 1h idle and 24h total. Values
// at or below the limits are kept as set; unset gives 1h and 24h.
func TestSessionLimitsAreClampedSilentlyAndLowerValuesKept(t *testing.T) {
	cases := []struct {
		name       string
		ttl, max   string
		idle       time.Duration // the idle limit the session must obey
		checkCeil  bool
		silentWarn bool
	}{
		{"configured above the limits", "24h", "168h", time.Hour, true, true},
		{"unset", "", "", time.Hour, true, false},
		{"shorter than the limit is kept", "10m", "", 10 * time.Minute, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := bootstrap(t, Opts{SessionTTL: tc.ttl, SessionMax: tc.max})
			if tc.silentWarn {
				// Only a warning about the session LIMITS is forbidden. A
				// warning that merely mentions the session (the documented
				// "session cookie" one for secureCookie: false) is not one.
				for _, l := range w.a.logWarns() {
					low := strings.ToLower(l)
					for _, word := range []string{"sessionttl", "sessionmaxlifetime", "idle", "lifetime", "limit", "clamp"} {
						if strings.Contains(low, word) {
							t.Errorf("a warning about the session limits was logged (mentions %q): %s", word, l)
							break
						}
					}
				}
			}
			alive := func(c *client) bool { return c.session(t).Authenticated }

			// Three sessions of ordinary accounts, all signed in at t=0.
			mk := func(name string) *client {
				u := w.newUser(t, name, "user")
				c, r := w.signIn(t, u, "")
				wantStatus(t, "sign-in of "+name, r, 200)
				if !alive(c) {
					t.Fatalf("%s's session is not authenticated straight after sign-in", name)
				}
				return c
			}
			unused, nearly, busy := mk("unused-user"), mk("nearly-user"), mk("busy-user")

			// Idle: "nearly" is last used (idle-1m) before the check, "unused"
			// not at all, "busy" every 10 minutes (or 2 for the short TTL).
			tick := 10 * time.Minute
			if tc.idle <= 10*time.Minute {
				tick = 2 * time.Minute
			}
			elapsed := time.Duration(0)
			target := tc.idle + time.Minute // 61m (or 11m): unused is past the limit
			nearlyTouched := false
			for elapsed < target {
				step := tick
				if elapsed == 0 {
					step = 2 * time.Minute
				}
				if elapsed+step > target {
					step = target - elapsed
				}
				w.advance(t, step)
				elapsed += step
				if !nearlyTouched {
					alive(nearly) // used 2 minutes in, so idle for (limit - 1m) at the check
					nearlyTouched = true
				}
				if elapsed < target {
					alive(busy)
				}
			}
			if alive(unused) {
				t.Errorf("a session unused for %s is still signed in; the idle limit is %s", target, tc.idle)
			}
			if !alive(nearly) {
				t.Errorf("a session last used %s ago (inside the %s idle limit) was ended", target-2*time.Minute, tc.idle)
			}
			if !alive(busy) {
				t.Errorf("a session used every %s was ended before the ceiling", tick)
			}

			if !tc.checkCeil {
				return
			}
			// Ceiling: keep "busy" in use until just under 24 hours, then past it.
			for elapsed+tick <= 23*time.Hour+50*time.Minute {
				w.advance(t, tick)
				elapsed += tick
				if !alive(busy) {
					t.Fatalf("a busy session ended at %s, before the 24 hour ceiling", elapsed)
				}
			}
			w.advance(t, 24*time.Hour+time.Minute-elapsed)
			if alive(busy) {
				t.Error("a session that was used constantly is still signed in after 24 hours; the total limit is 24h")
			}
		})
	}
}

// #1202 B1.7: auth.adminPasskey: required with no publicUrl is refused at
// config validation with a CFG code; optional always starts. Paired with
// required plus an https domain URL passing.
func TestAdminPasskeyRequiredWithoutAPublicURLIsRefusedAtValidation(t *testing.T) {
	t.Parallel()
	code, out, _ := cli(t, Opts{AdminPasskey: "required"}, "", "-validate-config")
	if code != 1 {
		t.Errorf("-validate-config with adminPasskey required and no publicUrl: exit %d, want 1\n%s", code, out)
	}
	found := cfgCodeRe.FindString(out)
	if found == "" {
		t.Fatalf("no CFG code in the validation output:\n%s", out)
	}
	if !documentedCFGCodes(t)[found] {
		t.Errorf("%s is not registered in docs/configuration.md's problem codes", found)
	}
	if !strings.Contains(out, "adminPasskey") {
		t.Errorf("the problem does not name auth.adminPasskey:\n%s", out)
	}

	if code, out, _ := cli(t, Opts{AdminPasskey: "optional"}, "", "-validate-config"); code != 0 {
		t.Errorf("-validate-config with adminPasskey optional: exit %d, want 0\n%s", code, out)
	}
	if code, out, _ := cli(t, Opts{AdminPasskey: "required", PublicURL: "https://mikroview.example.test"}, "", "-validate-config"); code != 0 {
		t.Errorf("-validate-config with adminPasskey required and an https domain publicUrl: exit %d, want 0\n%s", code, out)
	}
}

// #1202 B1.7: unset, the rule is required when publicUrl is https on a
// domain name and optional otherwise -- seen in GET /api/auth/session.
func TestAdminPasskeyDefaultFollowsThePublicURL(t *testing.T) {
	cases := []struct {
		name, url string
		want      bool
	}{
		{"https on a domain name", "https://mikroview.example.test", true},
		{"plain http on a domain name", "http://mikroview.example.test", false},
		{"an IP address", "https://192.0.2.10", false},
		{"no public URL", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := bootstrap(t, Opts{PublicURL: tc.url})
			s := w.c.session(t)
			if !s.Authenticated {
				t.Fatalf("session = %+v, want authenticated", s)
			}
			if s.AdminPasskeyRequired != tc.want {
				t.Errorf("adminPasskeyRequired = %v, want %v", s.AdminPasskeyRequired, tc.want)
			}
		})
	}

	// An explicit setting overrides the default either way.
	w := bootstrap(t, Opts{PublicURL: "https://mikroview.example.test", AdminPasskey: "optional"})
	if s := w.c.session(t); s.AdminPasskeyRequired {
		t.Error("auth.adminPasskey: optional did not override the https default")
	}
}

// #1202 B8.35 (file backend): plaintext accounts, tokens and recovery-key
// documents left by an earlier release are sealed on first open; every
// account and token still works; a second start-up reads them back; a wrong
// key fails start-up and never reads as an empty store.
func TestPlaintextAuthFilesAreSealedOnFirstStartAndKeepWorking(t *testing.T) {
	t.Parallel()
	// Make a real install, then unseal its documents to what a previous
	// release wrote: this needs the plaintext, so take it from a sealed
	// run by driving the file backend directly.
	f := newLegacyInstall(t)

	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if setupCodeRe.MatchString(a.log()) {
		t.Errorf("an install with accounts announced a setup code:\n%s", a.log())
	}
	for _, name := range []string{"users.json", "tokens.json"} {
		raw := readBytes(t, a.file(name))
		if json.Valid(raw) {
			t.Errorf("%s is still valid JSON after first start-up; it was not sealed", name)
		}
	}
	if raw, err := os.ReadFile(a.file("recovery.json")); err == nil && json.Valid(raw) {
		t.Error("recovery.json is still valid JSON after first start-up; it was not sealed")
	}

	w := &world{a: a, admin: f.admin}
	_, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in of a pre-existing account", r, 200)
	wantStatus(t, "api token on /api/stats", a.client().bearer(f.tokens["api"], "GET", "/api/stats"), 200)
	if r := a.client().bearer(f.tokens["droplist-pull"], "GET", "/api/droplist.rsc"); r.Status == 401 || r.Status == 403 || r.Status == 404 {
		t.Errorf("the droplist-pull token was refused after sealing: %s", r)
	}
	a.stop()

	// Second start-up reads the sealed files back.
	b := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if s := b.client().session(t); s.SetupRequired {
		t.Fatal("the second start-up read the sealed accounts as empty")
	}
	b.stop()

	// Wrong key: refused, never an empty store.
	other := filepath.Join(f.root, "keys", "other.key")
	code, out, _ := refused(t, Opts{Root: f.root, KeyFile: other})
	if code == 0 || setupCodeRe.MatchString(out) {
		t.Errorf("a wrong key was not cleanly refused (exit %d):\n%s", code, out)
	}
}

// #1202 B8.36: the sign-in history store is written sealed under the key.
// After use none of the auth files in the data directory (accounts, tokens,
// recovery keys, sign-ins) reads as plain JSON.
func TestNoAuthFileReadsAsPlainJSONAfterUse(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	if r := w.c.post("/api/tokens", map[string]string{"name": "grafana", "password": w.admin.pass}); r.Status != 201 {
		t.Fatalf("creating a token: %s", r)
	}
	// A sign-in and a failed one, so the history has rows.
	_, r := w.signIn(t, w.admin, w.admin.code(t, w.a, 1))
	wantStatus(t, "sign-in", r, 200)
	w.a.client().post("/api/auth/login", map[string]string{"username": w.admin.name, "password": "not-the-password"})
	// Rows are saved a few seconds after they arrive; closing the process
	// saves once more.
	time.Sleep(6 * time.Second)
	w.a.stop()
	if code, out, _ := cli(t, Opts{Root: w.a.root}, "saved\n", "-generate-recovery-keys"); code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}

	for _, name := range []string{"users.json", "tokens.json", "recovery.json", "signins.json"} {
		path := w.a.file(name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s was not written: %v", name, err)
			continue
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			t.Errorf("%s is empty", name)
			continue
		}
		if json.Valid(raw) {
			t.Errorf("%s reads as plain JSON:\n%.200s", name, raw)
		}
	}
}

// #1202 B9.40: an auth file sealed by the previous release opens unchanged,
// and its bytes do not change until the first save.
func TestAuthFileSealedByThePreviousReleaseOpensAndIsUntouchedUntilASave(t *testing.T) {
	t.Parallel()
	f := newLegacyInstallSealed(t)
	before := map[string][]byte{}
	for _, name := range []string{"users.json", "tokens.json"} {
		before[name] = readBytes(t, filepath.Join(f.root, "data", name))
	}

	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	if setupCodeRe.MatchString(a.log()) {
		t.Errorf("an install with accounts announced a setup code:\n%s", a.log())
	}
	if s := a.client().session(t); s.SetupRequired {
		t.Fatal("a file sealed by the previous release read as an empty store")
	}
	// Give start-up time to do anything it might (a sweep, a rewrite).
	time.Sleep(2 * time.Second)
	for name, want := range before {
		if got := readBytes(t, a.file(name)); !bytes.Equal(got, want) {
			t.Errorf("%s changed before any save: opening a file the previous release sealed must not rewrite it", name)
		}
	}

	// Paired: a sign-in is a save, and it changes the accounts file.
	w := &world{a: a, admin: f.admin}
	_, r := w.signIn(t, f.admin, f.admin.recovery(t))
	wantStatus(t, "sign-in", r, 200)
	if got := readBytes(t, a.file("users.json")); bytes.Equal(got, before["users.json"]) {
		t.Error("a sign-in did not save the accounts file (test premise)")
	}
}

// #1202 B9.41: an auth file that is neither sealed nor valid JSON fails
// start-up. The file is left byte for byte as it was, and no setup code is
// logged.
func TestAuthFileThatIsNeitherSealedNorJSONFailsStartUpAndIsLeftAlone(t *testing.T) {
	cases := map[string][]byte{
		"truncated json": []byte(`{"users":[{"id":"a1b2","username":"mv-admin"`),
		"garbage":        {0x00, 0x13, 0x37, 0xff, 0xfe, 0x01},
		"text":           []byte("restored from the wrong place"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newLegacyInstallSealed(t)
			path := filepath.Join(f.root, "data", "users.json")
			must(t, os.WriteFile(path, body, 0o600))

			code, out, _ := refused(t, Opts{Root: f.root, KeyFile: f.keyPath})
			if code == 0 {
				t.Errorf("exit status 0 with a damaged accounts file\n%s", out)
			}
			if setupCodeRe.MatchString(out) {
				t.Errorf("a setup code was logged over a damaged accounts file:\n%s", out)
			}
			if got := readBytes(t, path); !bytes.Equal(got, body) {
				t.Errorf("the damaged file was changed:\n got %q\nwant %q", got, body)
			}
		})
	}

	// Paired: a valid JSON file is sealed in place instead (ruling 11).
	t.Run("valid JSON is sealed instead", func(t *testing.T) {
		t.Parallel()
		f := newLegacyInstall(t)
		a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
		if json.Valid(readBytes(t, a.file("users.json"))) {
			t.Error("plaintext accounts were not sealed")
		}
	})
}

// #1202 B9.44: with a key, the drop-list file is sealed (not valid JSON),
// and no start-up log line lists stores kept unencrypted.
func TestDropListFileIsSealedAndNoLogLineListsUnencryptedStores(t *testing.T) {
	t.Parallel()
	w := bootstrap(t, Opts{})
	// An entry, so the store has content to persist.
	ra := w.c.post("/api/droplist", map[string]string{"cidr": "203.0.114.0/24", "reason": "test entry"})
	wantStatus(t, "adding a drop-list entry", ra, 201)
	time.Sleep(3 * time.Second)
	w.a.stop()

	raw, err := os.ReadFile(w.a.file("droplist.json"))
	if err != nil {
		t.Fatalf("the drop-list file was not written: %v", err)
	}
	if json.Valid(raw) {
		t.Errorf("droplist.json reads as plain JSON although a key is mounted:\n%.200s", raw)
	}
	for _, l := range strings.Split(w.a.log(), "\n") {
		low := strings.ToLower(strings.ReplaceAll(l, w.a.root, ""))
		if strings.Contains(low, "unencrypted") || strings.Contains(low, "in the clear") || strings.Contains(low, "plaintext") {
			t.Errorf("a start-up log line talks about unencrypted stores: %s", l)
		}
	}
}

// #1202 B1.8: a tokens document that is a bare JSON list holding an api
// token, an ingest token with a device and a droplist-pull token opens; all
// three raw values (32 hex, no prefix) authenticate on their routes
// afterwards, and only on those.
func TestBareTokenListOpensAndAllThreeKindsAuthenticateOnTheirRoutes(t *testing.T) {
	t.Parallel()
	f := newLegacyInstall(t)
	a := mustStart(t, Opts{Root: f.root, KeyFile: f.keyPath})
	c := a.client()

	// /api/matches answers 400 by design without a mac, so it gets one.
	for _, p := range []string{"/api/events", "/api/flags", "/api/stats", "/api/devices", "/api/matches?mac=aa:bb:cc:dd:ee:ff"} {
		wantStatus(t, "api token on GET "+p, c.bearer(f.tokens["api"], "GET", p), 200)
	}
	if r := c.bearer(f.tokens["droplist-pull"], "GET", "/api/droplist.rsc"); r.Status == 401 || r.Status == 403 || r.Status == 404 {
		t.Errorf("the legacy droplist-pull token was refused on GET /api/droplist.rsc: %s", r)
	}
	// The ingest token is accepted by its route (whatever the empty body
	// is answered with, it is not an authentication failure).
	r := c.do("POST", "/api/ingest/routeros", map[string]any{}, map[string]string{"Authorization": "Bearer " + f.tokens["ingest"]})
	if r.Status == 401 {
		t.Errorf("the legacy ingest token was refused as a credential on POST /api/ingest/routeros: %s", r)
	}

	// Paired: a value that was never issued is refused on the same routes.
	wantStatus(t, "unknown token on GET /api/stats", c.bearer(randHex(t, 16), "GET", "/api/stats"), 401)
	wantStatus(t, "api token on the droplist route", c.bearer(f.tokens["api"], "GET", "/api/droplist.rsc"), 404)
}

// #1202 B8.36 (recovery keys): -generate-recovery-keys writes a sealed
// recovery-keys document, which like the other auth files does not read
// as plain JSON.
func TestRecoveryKeysFileIsSealed(t *testing.T) {
	t.Parallel()
	code, out, a := cli(t, Opts{}, "saved\n", "-generate-recovery-keys")
	if code != 0 {
		t.Fatalf("-generate-recovery-keys exit %d:\n%s", code, out)
	}
	raw, err := os.ReadFile(a.file("recovery.json"))
	if err != nil {
		t.Fatalf("no recovery-keys document written: %v\n%s", err, out)
	}
	if json.Valid(raw) {
		t.Errorf("recovery.json reads as plain JSON:\n%.200s", raw)
	}
}
