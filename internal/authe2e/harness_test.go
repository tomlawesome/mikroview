// SPDX-License-Identifier: AGPL-3.0-only

// Package authe2e holds #1202's start-up, sign-in and account tests: the
// behaviour of mikroview with its authentication on gauntlet, driven
// through the real binary over HTTP and its command line, so they depend
// on as few internal names as possible.
//
// Written from the specification (docs/decisions/gauntlet-adoption.md and
// the build notes' sections B and B9) before the code exists. Every test
// carries its specification number in a comment (// #1202 B2.9).
//
// How it works. TestMain builds the module root's main package once into
// a scratch directory (go build, with an overlay supplying the empty
// web/dist placeholder when no UI is built, so the tree is never written
// to). Each test then writes a config and a key into its own temp
// directory, starts that binary on a free loopback port, reads the setup
// code out of its log as an operator would, and talks to it like a
// browser or a script would.
//
// Seams the build must provide (also listed in the commit message):
//
//   - MV_TEST_HOOKS=1: the existing POST /api/test/clock {"advance":"5m"}
//     must move the clock that gauntlet sees -- sessions, lockouts,
//     pending logins, held enrolments, token expiry -- not only the
//     definitions read path. It needs an admin session, as today.
//   - MV_TEST_HOOKS=1 and MV_TEST_PASSWORDLIST_URL: the password-list
//     refresher fetches from that https URL instead of blocklist.DefaultURL
//     (the wiring keeps the URL injectable; this is the way a process
//     test reaches it). Its HTTP client uses the system roots, so the
//     tests trust their server through SSL_CERT_FILE.
//   - the first token sweep runs shortly after start-up (within ten
//     seconds) and then daily, so a process test can see it.
//   - config keys auth.adminPasskey and auth.signInsStorePath (build notes
//     A4), the latter with the environment variable
//     MIKROVIEW_AUTH_SIGN_INS_STORE_PATH like the other store paths; the
//     password list is kept in "password-list" beside the accounts file.
//   - MV_TEST_APP_FOLDER (honoured only with MV_TEST_HOOKS=1) replaces
//     /etc/mikroview as the app folder, so a process test can put
//     config.yaml and keys/history.key where the binary looks by default.
package authe2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 authenticator codes are HMAC-SHA1
	"crypto/tls"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---- building the binary ----------------------------------------------

var (
	buildOnce sync.Once
	binDir    string
	binPath   string
	buildErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

func moduleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func buildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root := moduleRoot()
		dir, err := os.MkdirTemp("", "authe2e-bin-")
		if err != nil {
			buildErr = err
			return
		}
		binDir = dir
		binPath = filepath.Join(dir, "mikroview")

		args := []string{"build", "-buildvcs=false", "-ldflags", "-X main.version=0.0.0-authe2e", "-o", binPath}
		if matches, _ := filepath.Glob(filepath.Join(root, "web", "dist", "*")); len(matches) == 0 {
			// go:embed all:dist needs one file; supply it virtually.
			empty := filepath.Join(dir, "empty")
			if err := os.WriteFile(empty, nil, 0o600); err != nil {
				buildErr = err
				return
			}
			overlay, _ := json.Marshal(map[string]any{"Replace": map[string]string{
				filepath.Join(root, "web", "dist", ".gitkeep"): empty,
			}})
			ofile := filepath.Join(dir, "overlay.json")
			if err := os.WriteFile(ofile, overlay, 0o600); err != nil {
				buildErr = err
				return
			}
			args = append(args, "-overlay="+ofile)
		}
		args = append(args, ".")
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building the mikroview binary: %v", buildErr)
	}
	return binPath
}

// ---- running it ---------------------------------------------------------

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Opts says how to start one instance. The zero value is a normal
// start: plain http on loopback, a key mounted, nothing else special.
type Opts struct {
	// Root reuses a previous instance's directory (a restart). Empty
	// makes a new one.
	Root string
	// NoKey mounts no history.keyFile (and writes none).
	NoKey bool
	// KeyFile names the key file to configure; empty means <root>/keys/history.key,
	// written with 32 random bytes if absent.
	KeyFile string
	// KeyFromEnv passes the key through MIKROVIEW_HISTORY_KEY_FILE
	// instead of the config file.
	KeyFromEnv bool
	// AdminPasskey is auth.adminPasskey: "", "required" or "optional".
	AdminPasskey string
	// PublicURL is publicUrl.
	PublicURL string
	// TLS serves https with auth.secureCookie true.
	TLS bool
	// SessionTTL and SessionMax are auth.sessionTTL and sessionMaxLifetime.
	SessionTTL, SessionMax string
	// Extra is appended to the generated config (top-level YAML).
	Extra string
	// AppFolder puts the config (RawConfig) and the key in a folder the
	// process finds by itself -- the app folder's config.yaml and
	// keys/history.key -- instead of through MIKROVIEW_CONFIG and a
	// configured keyFile. The folder is /etc/mikroview in a container;
	// MV_TEST_APP_FOLDER (with MV_TEST_HOOKS=1) moves it for a test.
	AppFolder bool
	// RawConfig replaces the generated config entirely (a broken file,
	// for setup-only mode). The port is then given through
	// MIKROVIEW_LISTEN_HTTP.
	RawConfig string
	// Env adds environment variables.
	Env map[string]string
	// PostgresDSN, if set, is written to a DSN file and configured.
	PostgresDSN string
	// NoTestHooks leaves MV_TEST_HOOKS unset.
	NoTestHooks bool
	// LogLevel overrides log.level.
	LogLevel string
}

type app struct {
	t      *testing.T
	o      Opts
	root   string
	data   string
	key    string
	port   int
	base   string
	cmd    *exec.Cmd
	out    *syncBuf
	exited chan struct{}
	code   int
	// offset is how far the test clock has been moved.
	offsetMu sync.Mutex
	offset   time.Duration
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func writeKey(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(raw)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (a *app) config() string {
	d := a.data
	scheme := "false"
	tlsBlock := fmt.Sprintf("tls: {enabled: false, storePath: %s/tls}", d)
	if a.o.TLS {
		scheme = "true"
		tlsBlock = fmt.Sprintf(`tls: {enabled: true, hosts: ["127.0.0.1"], storePath: %s/tls}`, d)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "listen: {syslogTls: \"\", http: \"127.0.0.1:%d\", httpRedirect: \"\"}\n", a.port)
	b.WriteString(tlsBlock + "\n")
	fmt.Fprintf(&b, "auth:\n  storePath: %s/users.json\n  recoveryKeysPath: %s/recovery.json\n  recoveryPepperPath: %s/pepper\n  tokensStorePath: %s/tokens.json\n  secureCookie: %s\n",
		d, d, d, d, scheme)
	fmt.Fprintf(&b, "  signInsStorePath: %s/signins.json\n", d)
	if a.o.SessionTTL != "" {
		fmt.Fprintf(&b, "  sessionTTL: %s\n", a.o.SessionTTL)
	}
	if a.o.SessionMax != "" {
		fmt.Fprintf(&b, "  sessionMaxLifetime: %s\n", a.o.SessionMax)
	}
	if a.o.AdminPasskey != "" {
		fmt.Fprintf(&b, "  adminPasskey: %s\n", a.o.AdminPasskey)
	}
	fmt.Fprintf(&b, "flags:\n  storePath: %s/flags.json\n  ruleUsageStorePath: %s/rule-usage.json\n", d, d)
	fmt.Fprintf(&b, "store: {settingsStorePath: %s/settings.json}\n", d)
	for _, kv := range [][2]string{
		{"entities", "entities.json"}, {"coverage", "coverage.json"}, {"hosts", "hosts.json"},
		{"seen", "seen-values.json"}, {"baseline", "baseline.json"}, {"audit", "audit.json"},
		{"setup", "setup.json"}, {"droplist", "droplist.json"}, {"prefs", "preferences.json"},
		{"deviceMac", "mac-registry.json"}, {"deviceRegistry", "device-registry.json"},
	} {
		fmt.Fprintf(&b, "%s: {storePath: %s/%s}\n", kv[0], d, kv[1])
	}
	fmt.Fprintf(&b, "watchlist:\n  matchLogPath: %s/matchlog.jsonl\n  suggestionsStorePath: %s/suggestions.json\n", d, d)
	fmt.Fprintf(&b, "engine:\n  storePath: %s/engine-state.json\n  definitionsStorePath: %s/definitions.json\n  decommissionStorePath: %s/decommission.json\n", d, d, d)
	hist := fmt.Sprintf("history: {enabled: false, dir: %s/history", d)
	if !a.o.NoKey && !a.o.KeyFromEnv {
		hist += fmt.Sprintf(", keyFile: %q", a.key)
	}
	b.WriteString(hist + "}\n")
	fmt.Fprintf(&b, "snapshot: {dir: %s/snapshots}\n", d)
	fmt.Fprintf(&b, "geoip: {cachePath: %s/geoip}\n", d)
	fmt.Fprintf(&b, "backup: {vaultDir: %s/vault}\n", d)
	b.WriteString("oui: {enabled: false}\nblocklist: {sources: []}\n")
	if a.o.LogLevel != "" {
		fmt.Fprintf(&b, "log: {level: %s}\n", a.o.LogLevel)
	}
	if a.o.PublicURL != "" {
		fmt.Fprintf(&b, "publicUrl: %q\n", a.o.PublicURL)
	}
	if a.o.PostgresDSN != "" {
		dsnFile := filepath.Join(a.root, "postgres-dsn")
		_ = os.WriteFile(dsnFile, []byte(a.o.PostgresDSN+"\n"), 0o600)
		fmt.Fprintf(&b, "postgres: {dsnFile: %s}\n", dsnFile)
	}
	b.WriteString(a.o.Extra)
	return b.String()
}

// prepare makes the directory, key and config for one instance without
// starting it.
func prepare(t *testing.T, o Opts) (*app, []string) {
	t.Helper()
	a := &app{t: t, o: o, out: &syncBuf{}, exited: make(chan struct{})}
	a.root = o.Root
	if a.root == "" {
		a.root = t.TempDir()
	}
	a.data = filepath.Join(a.root, "data")
	if err := os.MkdirAll(a.data, 0o700); err != nil {
		t.Fatal(err)
	}
	a.key = o.KeyFile
	if a.key == "" {
		a.key = filepath.Join(a.root, "keys", "history.key")
		if o.AppFolder {
			a.key = filepath.Join(a.root, "appfolder", "keys", "history.key")
		}
	}
	if !o.NoKey {
		writeKey(t, a.key)
	}
	a.port = freePort(t)
	scheme := "http"
	if o.TLS {
		scheme = "https"
	}
	a.base = fmt.Sprintf("%s://127.0.0.1:%d", scheme, a.port)

	cfgPath := filepath.Join(a.root, "config.yaml")
	if o.AppFolder {
		cfgPath = filepath.Join(a.root, "appfolder", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := a.config()
	if o.RawConfig != "" {
		cfg = o.RawConfig
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + a.root}
	if o.AppFolder {
		env = append(env, "MV_TEST_APP_FOLDER="+filepath.Join(a.root, "appfolder"))
	} else {
		env = append(env, "MIKROVIEW_CONFIG="+cfgPath)
	}
	if !o.NoTestHooks {
		env = append(env, "MV_TEST_HOOKS=1")
	}
	if o.RawConfig != "" {
		env = append(env, fmt.Sprintf("MIKROVIEW_LISTEN_HTTP=127.0.0.1:%d", a.port), "MIKROVIEW_LISTEN_SYSLOG_TLS=", "MIKROVIEW_LISTEN_HTTP_REDIRECT=",
			"MIKROVIEW_TLS_ENABLED=false", "MIKROVIEW_AUTH_SECURE_COOKIE=false",
			"MIKROVIEW_AUTH_STORE_PATH="+a.data+"/users.json", "MIKROVIEW_TLS_STORE_PATH="+a.data+"/tls",
			"MIKROVIEW_AUTH_TOKENS_STORE_PATH="+a.data+"/tokens.json", "MIKROVIEW_AUTH_SIGN_INS_STORE_PATH="+a.data+"/signins.json")
	}
	if o.KeyFromEnv && !o.NoKey {
		env = append(env, "MIKROVIEW_HISTORY_KEY_FILE="+a.key)
	}
	for k, v := range o.Env {
		env = append(env, k+"="+v)
	}
	return a, env
}

// runOnce runs the binary with arguments to completion (a command-line
// mode: -validate-config, -recover-admin-account, ...) and returns its
// exit status and combined output. stdin is fed to it as given.
func (a *app) runOnce(t *testing.T, env []string, stdin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(buildBinary(t), args...)
	cmd.Dir = a.root
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var out syncBuf
	cmd.Stdout = &out
	cmd.Stderr = &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), out.String()
		} else if err != nil {
			return -1, out.String()
		}
		return 0, out.String()
	case <-time.After(120 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("%v did not finish in 120s:\n%s", args, out.String())
	}
	return -1, ""
}

// cli runs a command-line mode against an instance's configuration.
func cli(t *testing.T, o Opts, stdin string, args ...string) (int, string, *app) {
	t.Helper()
	a, env := prepare(t, o)
	code, out := a.runOnce(t, env, stdin, args...)
	return code, out, a
}

// start launches the binary and waits until it answers /api/healthz, or
// until it exits, in which case the error carries its output.
func start(t *testing.T, o Opts) (*app, error) {
	t.Helper()
	a, env := prepare(t, o)
	cmd := exec.Command(buildBinary(t))
	cmd.Dir = a.root
	cmd.Env = env
	cmd.Stdout = a.out
	cmd.Stderr = a.out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the binary: %v", err)
	}
	a.cmd = cmd
	go func() {
		err := cmd.Wait()
		a.code = 0
		if ee, ok := err.(*exec.ExitError); ok {
			a.code = ee.ExitCode()
		} else if err != nil {
			a.code = -1
		}
		close(a.exited)
	}()
	t.Cleanup(a.stop)

	hc := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- loopback test server
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-a.exited:
			return a, fmt.Errorf("exited with status %d before it was ready:\n%s", a.code, a.out.String())
		default:
		}
		if resp, err := hc.Get(a.base + "/api/healthz"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return a, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return a, fmt.Errorf("not ready after 60s:\n%s", a.out.String())
}

func mustStart(t *testing.T, o Opts) *app {
	t.Helper()
	a, err := start(t, o)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return a
}

// refused starts the binary expecting it to stop of its own accord, and
// returns its exit status and everything it printed. It fails if the
// process becomes ready instead.
func refused(t *testing.T, o Opts) (int, string, *app) {
	t.Helper()
	a, err := start(t, o)
	if err == nil {
		a.stop()
		t.Fatalf("the binary started, but was expected to refuse:\n%s", a.out.String())
	}
	select {
	case <-a.exited:
	case <-time.After(30 * time.Second):
		a.stop()
		t.Fatalf("the binary neither became ready nor exited:\n%s", a.out.String())
	}
	return a.code, a.out.String(), a
}

func (a *app) stop() {
	if a.cmd == nil || a.cmd.Process == nil {
		return
	}
	select {
	case <-a.exited:
		return
	default:
	}
	_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-a.exited:
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-a.cmd.Process.Pid, syscall.SIGKILL)
		<-a.exited
	}
}

func (a *app) log() string { return a.out.String() }

// restart stops this instance and starts another on the same directory.
func (a *app) restart(t *testing.T, mutate func(*Opts)) *app {
	t.Helper()
	a.stop()
	o := a.o
	o.Root = a.root
	if mutate != nil {
		mutate(&o)
	}
	return mustStart(t, o)
}

var setupCodeRe = regexp.MustCompile(`create the first admin with setup code ([A-Za-z0-9][A-Za-z0-9-]*)`)

func (a *app) setupCode(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := setupCodeRe.FindAllStringSubmatch(a.log(), -1); len(m) > 0 {
			return m[len(m)-1][1]
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no setup code in the log:\n%s", a.log())
	return ""
}

func (a *app) serverNow() time.Time {
	a.offsetMu.Lock()
	defer a.offsetMu.Unlock()
	return time.Now().Add(a.offset)
}

// logWarns returns the Warn and Error lines of the log.
func (a *app) logWarns() []string {
	var out []string
	for _, l := range strings.Split(a.log(), "\n") {
		if strings.Contains(l, " WARN ") || strings.Contains(l, " ERROR ") {
			out = append(out, l)
		}
	}
	return out
}

// ---- talking to it -----------------------------------------------------

type client struct {
	a      *app
	hc     *http.Client
	jar    http.CookieJar
	noCSRF bool
}

func (a *app) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{a: a, jar: jar, hc: &http.Client{
		Jar:     jar,
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- loopback test server
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type resp struct {
	Status  int
	H       http.Header
	Body    []byte
	Cookies []*http.Cookie
}

func (r resp) String() string { return fmt.Sprintf("%d %s", r.Status, r.Body) }

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("response body is not the expected JSON (%v): %s", err, r.Body)
	}
}

func (r resp) cookie(name string) *http.Cookie {
	for _, c := range r.Cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// problemClass is the anchor of a Problem Details body's type
// ("invalid-credentials"), or "" when the body is not one.
func (r resp) problemClass() string {
	var p struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(r.Body, &p) != nil {
		return ""
	}
	if i := strings.LastIndex(p.Type, "#"); i >= 0 {
		return p.Type[i+1:]
	}
	return p.Type
}

// wantProblem asserts the status and the problem class's type anchor.
func wantProblem(t *testing.T, what string, r resp, status int, class string) {
	t.Helper()
	if r.Status != status {
		t.Errorf("%s: status = %d, want %d (%s)", what, r.Status, status, r.Body)
		return
	}
	if ct := r.H.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("%s: Content-Type = %q, want application/problem+json", what, ct)
	}
	if got := r.problemClass(); got != class {
		t.Errorf("%s: problem type anchor = %q, want %q (%s)", what, got, class, r.Body)
	}
}

func wantStatus(t *testing.T, what string, r resp, status int) {
	t.Helper()
	if r.Status != status {
		t.Errorf("%s: status = %d, want %d (%s)", what, r.Status, status, r.Body)
	}
}

func (c *client) do(method, path string, body any, hdr map[string]string) resp {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.a.base+path, rd)
	if err != nil {
		c.a.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead && !c.noCSRF {
		req.Header.Set("X-Requested-With", "mikroview")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, H: res.Header, Body: raw, Cookies: res.Cookies()}
}

func (c *client) get(path string) resp         { return c.do(http.MethodGet, path, nil, nil) }
func (c *client) post(path string, b any) resp { return c.do(http.MethodPost, path, b, nil) }
func (c *client) put(path string, b any) resp  { return c.do(http.MethodPut, path, b, nil) }
func (c *client) del(path string, b any) resp  { return c.do(http.MethodDelete, path, b, nil) }
func (c *client) bearer(raw, method, path string) resp {
	return c.do(method, path, nil, map[string]string{"Authorization": "Bearer " + raw})
}

type sessionBody struct {
	SetupRequired        bool   `json:"setupRequired"`
	SSOAvailable         bool   `json:"ssoAvailable"`
	Authenticated        bool   `json:"authenticated"`
	Username             string `json:"username"`
	Role                 string `json:"role"`
	HasLocalPassword     bool   `json:"hasLocalPassword"`
	SSOConnected         bool   `json:"ssoConnected"`
	MustChangePassword   bool   `json:"mustChangePassword"`
	MustEnrolSecond      bool   `json:"mustEnrolSecondFactor"`
	MustEnrolPasskey     bool   `json:"mustEnrolPasskey"`
	AdminPasskeyRequired bool   `json:"adminPasskeyRequired"`
	HasTOTP              bool   `json:"hasTOTP"`
}

func (c *client) session(t *testing.T) sessionBody {
	t.Helper()
	r := c.get("/api/auth/session")
	wantStatus(t, "GET /api/auth/session", r, 200)
	var s sessionBody
	r.json(t, &s)
	return s
}

// ---- authenticator codes ---------------------------------------------

// totpCode is the RFC 6238 code (SHA-1, 30 s, 6 digits) for secret at t.
func totpCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		t.Fatalf("authenticator secret %q is not base32: %v", secret, err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", n%1_000_000)
}

// ---- accounts ----------------------------------------------------------

const adminPassword = "correct-horse-battery-7"

type acct struct {
	name, pass string
	secret     string
	codes      []string
	used       int
}

// code is the authenticator code step steps from the server's now
// (-1, 0 or +1 are accepted by the server).
func (a *acct) code(t *testing.T, app *app, step int) string {
	return totpCode(t, a.secret, app.serverNow().Add(time.Duration(step)*30*time.Second))
}

// recovery returns the next unused recovery code.
func (a *acct) recovery(t *testing.T) string {
	t.Helper()
	if a.used >= len(a.codes) {
		t.Fatalf("%s has no recovery codes left", a.name)
	}
	c := a.codes[a.used]
	a.used++
	return c
}

type world struct {
	a     *app
	admin *acct
	// c is the admin's signed-in client from setup; it is the "clock
	// driver" in tests that move time.
	c *client
}

// register creates the first admin with the setup code from the log.
func register(t *testing.T, a *app, name, pass string) (*client, resp) {
	t.Helper()
	c := a.client()
	r := c.post("/api/auth/register", map[string]string{"username": name, "password": pass, "setupCode": a.setupCode(t)})
	return c, r
}

// enrol puts an authenticator app on c's account the way the screens do:
// enrol, confirm with a code, then confirm the recovery codes are saved.
// It returns the secret and the ten codes.
func enrol(t *testing.T, a *app, c *client, pass string) (string, []string) {
	t.Helper()
	r := c.post("/api/auth/totp/enrol", map[string]string{"password": pass})
	wantStatus(t, "totp/enrol", r, 200)
	var e struct {
		URI    string `json:"uri"`
		Secret string `json:"secret"`
	}
	r.json(t, &e)
	if e.Secret == "" {
		t.Fatalf("totp/enrol returned no secret: %s", r.Body)
	}
	r = c.post("/api/auth/totp/confirm", map[string]string{"code": totpCode(t, e.Secret, a.serverNow().Add(-30*time.Second))})
	wantStatus(t, "totp/confirm", r, 200)
	var cf struct {
		Enabled             bool     `json:"enabled"`
		RecoveryCodes       []string `json:"recoveryCodes"`
		PendingConfirmation bool     `json:"pendingConfirmation"`
	}
	r.json(t, &cf)
	if len(cf.RecoveryCodes) != 10 || !cf.PendingConfirmation || cf.Enabled {
		t.Fatalf("totp/confirm = %+v, want ten codes held with pendingConfirmation true and enabled false", cf)
	}
	r = c.post("/api/auth/recovery-codes/confirm", map[string]any{})
	wantStatus(t, "recovery-codes/confirm", r, 200)
	return e.Secret, cf.RecoveryCodes
}

// bootstrap starts an instance, creates an admin and gives it an
// authenticator app, ready for anything.
func bootstrap(t *testing.T, o Opts) *world {
	t.Helper()
	a := mustStart(t, o)
	return bootstrapOn(t, a)
}

func bootstrapOn(t *testing.T, a *app) *world {
	t.Helper()
	c, r := register(t, a, "mv-admin", adminPassword)
	wantStatus(t, "register", r, 201)
	secret, codes := enrol(t, a, c, adminPassword)
	return &world{a: a, admin: &acct{name: "mv-admin", pass: adminPassword, secret: secret, codes: codes}, c: c}
}

// signIn runs the sign-in for acc: password, then (if the server asks)
// one second-factor code. By default the code is an authenticator code
// one step ahead (the caller says which), or a recovery code.
func (w *world) signIn(t *testing.T, acc *acct, factor string) (*client, resp) {
	t.Helper()
	c := w.a.client()
	r := c.post("/api/auth/login", map[string]string{"username": acc.name, "password": acc.pass})
	if r.Status != 200 {
		return c, r
	}
	var ch struct {
		SecondFactor []string `json:"secondFactor"`
	}
	_ = json.Unmarshal(r.Body, &ch)
	if len(ch.SecondFactor) == 0 && factor == "" {
		return c, r
	}
	return c, c.post("/api/auth/login/factor", map[string]string{"code": factor})
}

// newUser has the admin create an account and returns it.
func (w *world) newUser(t *testing.T, name, role string) *acct {
	t.Helper()
	pass := "pw-" + name + "-9X7q!"
	r := w.c.post("/api/auth/users", map[string]string{"username": name, "password": pass, "role": role})
	wantStatus(t, "create user "+name, r, 201)
	return &acct{name: name, pass: pass}
}

// enrolled creates an account and gives it an authenticator app, so it
// is not held at a door.
func (w *world) enrolled(t *testing.T, name, role string) *acct {
	t.Helper()
	u := w.newUser(t, name, role)
	c, r := w.signIn(t, u, "")
	wantStatus(t, "first sign-in of "+name, r, 200)
	u.secret, u.codes = enrol(t, w.a, c, u.pass)
	return u
}

// advance moves the server's clock (needs the admin client, as the hook
// does) and keeps the harness's idea of server time in step.
func (w *world) advance(t *testing.T, d time.Duration) {
	t.Helper()
	// In steps the admin's own session survives (idle limit one hour).
	for d > 0 {
		step := d
		if step > 20*time.Minute {
			step = 20 * time.Minute
		}
		r := w.c.post("/api/test/clock", map[string]string{"advance": step.String()})
		if r.Status != 200 {
			t.Fatalf("POST /api/test/clock advance=%s = %s (is MV_TEST_HOOKS wired to gauntlet's clock?)", step, r)
		}
		w.a.offsetMu.Lock()
		w.a.offset += step
		w.a.offsetMu.Unlock()
		d -= step
	}
}

// rebind points the world at a restarted instance and signs the admin in
// again (sessions do not survive a restart) with a recovery code.
func (w *world) rebind(t *testing.T, a *app) {
	t.Helper()
	w.a = a
	c, r := w.signIn(t, w.admin, w.admin.recovery(t))
	wantStatus(t, "admin sign-in after restart", r, 200)
	w.c = c
}

// ---- files ---------------------------------------------------------------

func (a *app) file(name string) string { return filepath.Join(a.data, name) }

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// dataFiles lists the regular files under the data directory, relative.
func dataFiles(t *testing.T, a *app) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(a.data, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			rel, _ := filepath.Rel(a.data, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

var cfgCodeRe = regexp.MustCompile(`CFG-\d{4}`)

// documentedCFGCodes are the codes docs/configuration.md lists.
func documentedCFGCodes(t *testing.T) map[string]bool {
	t.Helper()
	raw := readBytes(t, filepath.Join(moduleRoot(), "docs", "configuration.md"))
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^#### (CFG-\d{4})`).FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = true
	}
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

var _ = context.Background
var _ = strconv.Itoa
