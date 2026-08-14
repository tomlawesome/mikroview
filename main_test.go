// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/syslog"
)

// newIngestTestDeps builds the minimal set of dependencies
// ingestOneRecovered needs, all unconfigured/in-memory (no GeoIP DB, no
// flags/MAC-registry persistence) -- enough to exercise the new-device
// wiring itself (issue #103 phase 1) without touching disk.
func newIngestTestDeps(t *testing.T) (*store.Store, *device.Registry, *device.MACRegistry, *flags.Store, *hub.Hub, *geoip.Lookup, *detect.Detector, *rules.Store) {
	t.Helper()
	st := store.New(1000, time.Hour)
	devices := device.NewRegistry(nil)
	macRegistry, err := device.OpenMACRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	h := hub.New()
	geo, err := geoip.Open("")
	if err != nil {
		t.Fatal(err)
	}
	detector := detect.New(detect.DefaultConfig(), fs)
	ru, err := rules.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return st, devices, macRegistry, fs, h, geo, detector, ru
}

const firewallLineWithMAC = "A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60"

// TestIngestRaisesNewDeviceFlagOnceForFirstSighting is the phase-1 wiring
// contract from issue #103: the first event carrying a given SrcMAC
// raises exactly one TypeNewDevice flag, targeted at that MAC.
func TestIngestRaisesNewDeviceFlagOnceForFirstSighting(t *testing.T) {
	st, devices, macRegistry, fs, h, geo, detector, ru := newIngestTestDeps(t)
	logger := slog.Default()

	rm := syslog.RawMessage{SourceIP: "192.168.1.1", Data: []byte(firewallLineWithMAC), RecvTime: time.Now()}
	ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{}, nil, nil)

	list := fs.List()
	var found *flags.Flag
	for i := range list {
		if list[i].Type == flags.TypeNewDevice {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a TypeNewDevice flag to be raised, got flags: %+v", list)
	}
	if found.Target != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("TypeNewDevice flag Target = %q, want the MAC address", found.Target)
	}
	if found.Count != 1 {
		t.Errorf("TypeNewDevice flag Count = %d, want 1", found.Count)
	}
}

// TestIngestDoesNotReRaiseNewDeviceFlagOnSubsequentEvents proves the
// "fires once, not on every subsequent event from the same MAC" behavior
// the phase-1 spec requires: a second (and third) event carrying the
// same SrcMAC must not create a second TypeNewDevice episode or bump the
// existing one's Count.
func TestIngestDoesNotReRaiseNewDeviceFlagOnSubsequentEvents(t *testing.T) {
	st, devices, macRegistry, fs, h, geo, detector, ru := newIngestTestDeps(t)
	logger := slog.Default()
	now := time.Now()

	for i := 0; i < 3; i++ {
		rm := syslog.RawMessage{SourceIP: "192.168.1.1", Data: []byte(firewallLineWithMAC), RecvTime: now.Add(time.Duration(i) * time.Minute)}
		ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{}, nil, nil)
	}

	var newDeviceFlags []flags.Flag
	for _, f := range fs.List() {
		if f.Type == flags.TypeNewDevice {
			newDeviceFlags = append(newDeviceFlags, f)
		}
	}
	if len(newDeviceFlags) != 1 {
		t.Fatalf("expected exactly 1 TypeNewDevice flag across 3 events from the same MAC, got %d: %+v", len(newDeviceFlags), newDeviceFlags)
	}
	if newDeviceFlags[0].Count != 1 {
		t.Errorf("TypeNewDevice flag Count = %d, want 1 (Add() must never be called again for an already-known MAC)", newDeviceFlags[0].Count)
	}
}

// TestIngestSkipsNewDeviceFlagForEmptySrcMAC covers the documented
// coverage caveat: a WAN-side rule set typically carries no src-mac at
// all, and that must never be treated as a "new device."
func TestIngestSkipsNewDeviceFlagForEmptySrcMAC(t *testing.T) {
	st, devices, macRegistry, fs, h, geo, detector, ru := newIngestTestDeps(t)
	logger := slog.Default()

	const lineWithoutMAC = "A|wan-in|forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 203.0.113.5:51234->192.168.1.10:443, len 60"
	rm := syslog.RawMessage{SourceIP: "192.168.1.1", Data: []byte(lineWithoutMAC), RecvTime: time.Now()}
	ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{}, nil, nil)

	for _, f := range fs.List() {
		if f.Type == flags.TypeNewDevice {
			t.Errorf("expected no TypeNewDevice flag for an event with an empty SrcMAC, got %+v", f)
		}
	}
}

func TestVersionBootMessageFreshInstallNoUpgradeAlert(t *testing.T) {
	got := versionBootMessage("", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage(%q, %q) = %q, want %q", "", "abc1234", got, want)
	}
}

func TestVersionBootMessageSameVersionNoUpgradeAlert(t *testing.T) {
	got := versionBootMessage("abc1234", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage with an unchanged version = %q, want %q (no upgrade alert on a routine restart)", got, want)
	}
}

func TestVersionBootMessageUpgradeDetected(t *testing.T) {
	got := versionBootMessage("abc1234", "def5678")
	want := "upgraded from abc1234 to def5678"
	if got != want {
		t.Errorf("versionBootMessage across a version change = %q, want %q", got, want)
	}
}

func TestVersionBootMessageTrimsWhitespaceFromPersistedMarker(t *testing.T) {
	// The marker file is read back with os.ReadFile -- a trailing
	// newline (from an editor, or just how it happens to have been
	// written) shouldn't itself look like a version change.
	got := versionBootMessage("abc1234\n", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage with a trailing newline in the persisted marker = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetStripsPortAndAssumes443(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview.local:8081/api/events?x=1", nil)
	r.Host = "mikroview.local:8081"
	got := httpsRedirectTarget(r, []string{"mikroview.local"})
	want := "https://mikroview.local/api/events?x=1"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetHostWithNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://192.168.1.50/", nil)
	r.Host = "192.168.1.50"
	got := httpsRedirectTarget(r, []string{"192.168.1.50"})
	want := "https://192.168.1.50/"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetPreservesPathAndQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview:80/ca.crt", nil)
	r.Host = "mikroview:80"
	got := httpsRedirectTarget(r, []string{"mikroview"})
	want := "https://mikroview/ca.crt"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

// TestHTTPSRedirectTargetRejectsUnlistedHost proves the actual fix: a
// client connecting directly (not through a real browser navigation)
// fully controls the Host header, and previously that value was
// echoed straight into the Location header unvalidated -- a
// straightforward open redirect for anyone able to reach this
// listener directly.
func TestHTTPSRedirectTargetRejectsUnlistedHost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview:80/some/path", nil)
	r.Host = "evil.example.com"
	got := httpsRedirectTarget(r, []string{"mikroview", "192.168.1.50"})
	want := "https://mikroview/some/path"
	if got != want {
		t.Errorf("httpsRedirectTarget with an unlisted Host = %q, want a fall back to the first allowed host: %q", got, want)
	}
}

// An unset TLS.Hosts is the *shipped default* -- defaults() sets
// TLS.Enabled and Listen.HTTPRedirect but never TLS.Hosts, and
// deploy/docker-compose.yml maps host port 80 straight at this listener.
// So the allowlist was empty out of the box and this function fell back
// to echoing r.Host into a 308 Location: an unauthenticated Host-header
// reflection in the default configuration, in a function whose own doc
// comment claims it closes "a known vulnerability class".
//
// The guard now derives its own known-good set from the machine rather
// than giving up, so it works with nothing configured. See
// localRedirectHosts, and #283 finding 2.
func TestHTTPSRedirectTargetWithNoAllowlistRefusesAnArbitraryHost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview.local/some/path?x=1", nil)
	r.Host = "attacker.example"

	got := httpsRedirectTarget(r, nil)
	if strings.Contains(got, "attacker.example") {
		t.Errorf("httpsRedirectTarget with no configured allowlist = %q -- an arbitrary Host still reaches the Location header", got)
	}
	// The path and query still have to survive, or the redirect is
	// useless for its actual purpose.
	if !strings.HasSuffix(got, "/some/path?x=1") {
		t.Errorf("httpsRedirectTarget = %q, want the original path and query preserved", got)
	}
}

// The derived set has to contain something usable, or every redirect
// would point somewhere that does not answer.
func TestLocalRedirectHostsPrefersARealAddressOverLoopback(t *testing.T) {
	hosts := localRedirectHosts()
	if len(hosts) == 0 {
		t.Fatal("localRedirectHosts returned nothing -- httpsRedirectTarget would fall back to echoing Host")
	}
	if hosts[0] == "127.0.0.1" || hosts[0] == "localhost" {
		// Only legitimate on a machine with no non-loopback address at
		// all, which a CI container does have.
		t.Errorf("first host is %q -- an unrecognised Host is rewritten to hosts[0], and redirecting a LAN client to loopback is useless", hosts[0])
	}
	if !slices.Contains(hosts, "127.0.0.1") {
		t.Error("loopback missing -- a request that legitimately arrives as 127.0.0.1 would be rewritten away from it")
	}
}

// A Host mikroview genuinely answers to must still be honoured, or
// reaching it by its own name or address would bounce somewhere else.
func TestHTTPSRedirectTargetKeepsAHostTheMachineActuallyHas(t *testing.T) {
	hosts := localRedirectHosts()
	if len(hosts) == 0 {
		t.Skip("no local hosts enumerable in this environment")
	}
	for _, h := range hosts {
		r := httptest.NewRequest(http.MethodGet, "http://example/", nil)
		r.Host = h
		want := "https://" + h + "/"
		if got := httpsRedirectTarget(r, nil); got != want {
			t.Errorf("httpsRedirectTarget for own host %q = %q, want %q", h, got, want)
		}
	}
}

func TestSecurityHeadersSetOnEveryResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	rr := httptest.NewRecorder()
	securityHeaders(inner, false).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	h := rr.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", h.Get("X-Content-Type-Options"))
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", h.Get("X-Frame-Options"))
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("expected a Content-Security-Policy header to be set")
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Errorf("expected no HSTS header when hsts=false, got %q", h.Get("Strict-Transport-Security"))
	}

	rr2 := httptest.NewRecorder()
	securityHeaders(inner, true).ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr2.Header().Get("Strict-Transport-Security") == "" {
		t.Error("expected an HSTS header when hsts=true")
	}
}

// TestStaticCacheHeaders pins the two rules the upgrade path depends on
// (#347): content-hashed assets may be kept indefinitely, and everything
// with a stable filename must be revalidated.
//
// sw.js is the case worth stating out loud. Without an explicit header a
// browser may reuse a cached service worker for up to 24 hours, never
// notice the new one, and go on serving a precached app shell from the
// previous release -- an upgraded server showing a days-old UI, with
// every server-side signal correctly reporting the new version.
func TestStaticCacheHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	for _, tc := range []struct {
		path string
		want string
		why  string
	}{
		{"/assets/index-BShEGKey.js", "public, max-age=31536000, immutable", "content-hashed: the name changes when the bytes do"},
		{"/assets/index-CE5qYX4Y.css", "public, max-age=31536000, immutable", "content-hashed"},
		{"/sw.js", "no-cache", "stable name, and the file that decides whether an upgrade is noticed at all"},
		{"/registerSW.js", "no-cache", "stable name"},
		{"/", "no-cache", "index.html under a stable name"},
		{"/index.html", "no-cache", "stable name"},
		{"/manifest.webmanifest", "no-cache", "stable name"},
		{"/pwa-192x192.png", "no-cache", "stable name -- icons are not hashed"},
	} {
		rr := httptest.NewRecorder()
		staticCacheHeaders(inner).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if got := rr.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("Cache-Control for %s = %q, want %q (%s)", tc.path, got, tc.want, tc.why)
		}
	}
}

// TestRunVersionPrintsTheBareVersionString proves runVersion's whole
// contract: the printed line is exactly the version string, nothing
// else -- `docker exec <container> mikroview -version` output has to be
// directly usable in a script without any trimming.
func TestRunVersionPrintsTheBareVersionString(t *testing.T) {
	prevVersion := version
	version = "test-sha1234"
	defer func() { version = prevVersion }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prevStdout := os.Stdout
	os.Stdout = w
	code := runVersion()
	w.Close()
	os.Stdout = prevStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Errorf("runVersion() = %d, want 0", code)
	}
	if got := strings.TrimRight(string(out), "\n"); got != "test-sha1234" {
		t.Errorf("runVersion() printed %q, want %q", got, "test-sha1234")
	}
}

// TestAuthShouldFailClosed pins the exact predicate main() boots against:
// a non-nil auth.Open error with a configured store path is the only case
// that must refuse to start. Both "no persistence configured" (storePath
// == "", err always nil per auth.Open) and "file genuinely doesn't exist"
// (a real fresh install, err == nil) must keep booting normally.
func TestAuthShouldFailClosed(t *testing.T) {
	someErr := errors.New("boom")

	// The second argument is now the backend rather than a path -- a
	// nil backend is the "persistence not configured" case that used to
	// be an empty string. Same predicate, same three cases.
	configured := persist.NewFileBackend("/var/lib/mikroview/accounts.json")

	cases := []struct {
		name    string
		err     error
		backend persist.Backend
		want    bool
	}{
		{"corrupt document with a configured backend fails closed", someErr, configured, true},
		{"nil error never fails closed regardless of backend", nil, configured, false},
		{"nil error with no backend never fails closed", nil, nil, false},
		{"error with no backend never fails closed", someErr, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authShouldFailClosed(tc.err, tc.backend); got != tc.want {
				t.Errorf("authShouldFailClosed(%v, %v) = %v, want %v", tc.err, tc.backend != nil, got, tc.want)
			}
		})
	}
}

// TestAuthOpenErrorShapeDrivesFailClosed proves the real-world trigger for
// authShouldFailClosed against the actual auth.Store.Open implementation
// (not just the predicate in isolation): a file that exists but fails to
// parse produces a non-nil error, while a path with no file at all (fresh
// install) does not -- the exact distinction main()'s boot sequence relies
// on to tell "was configured, now broken" apart from "never configured".
func TestAuthOpenErrorShapeDrivesFailClosed(t *testing.T) {
	dir := t.TempDir()

	freshPath := filepath.Join(dir, "fresh", "accounts.json")
	_, err := auth.Open(freshPath)
	if err != nil {
		t.Fatalf("auth.Open on a not-yet-created path returned an error, want nil (fresh install must still boot): %v", err)
	}
	if authShouldFailClosed(err, persist.NewFileBackend(freshPath)) {
		t.Error("authShouldFailClosed reported true for a genuine fresh install")
	}

	corruptPath := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(corruptPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = auth.Open(corruptPath)
	if err == nil {
		t.Fatal("auth.Open on a corrupt existing file returned nil error, want non-nil")
	}
	if !authShouldFailClosed(err, persist.NewFileBackend(corruptPath)) {
		t.Error("authShouldFailClosed reported false for a corrupt existing accounts file")
	}
}

// The version string carries its lane, not just a commit -- see the
// `version` var's doc comment. This pins the three shapes so a build
// script can't quietly start stamping a bare SHA again, which would
// make "is this the published candidate or someone's laptop?"
// unanswerable from the logs.
func TestVersionStringShape(t *testing.T) {
	valid := regexp.MustCompile(`^(dev:[0-9a-z]+|preview:[0-9a-f]{7,40}|v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)$`)

	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"dev:local", true},
		{"dev:a1b2c3d", true},
		{"preview:a1b2c3d", true},
		{"preview:0123456789abcdef0123456789abcdef01234567", true},
		{"v1.2.3", true},
		{"v1.2.3-rc.1", true},
		{"dev", false},     // the old bare form
		{"a1b2c3d", false}, // a bare SHA says nothing about the lane
		{"", false},
		{"preview", false},
	} {
		if got := valid.MatchString(tc.v); got != tc.want {
			t.Errorf("%q: matched=%v, want %v", tc.v, got, tc.want)
		}
	}

	// The compiled-in default must itself be one of the valid shapes.
	if !valid.MatchString(version) {
		t.Errorf("the built-in default %q is not a valid version shape", version)
	}
}

// The VERSION file is what the release lane stamps into the binary (see
// docs/decisions/release-versioning.md and .github/workflows/docker.yml).
// A malformed one produces an image tag and a binary version that don't
// match anything, discovered at release time.
func TestVersionFileIsReleasable(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("reading VERSION: %v -- the release lane reads this file", err)
	}
	v := strings.TrimSpace(string(raw))

	if v != string(raw) && strings.TrimRight(string(raw), "\n") != v {
		t.Errorf("VERSION contains unexpected whitespace: %q", string(raw))
	}
	// Plain semver, no leading "v" -- the workflow adds it for the image
	// and git tags, so having it here too would produce "vv0.1.0".
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(v) {
		t.Fatalf("VERSION = %q, want plain semver like 0.1.0 with no leading 'v'", v)
	}

	// And "v" + that must satisfy the same shape the binary reports, so
	// the release tag and the running version are the same string.
	tagged := "v" + v
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(tagged) {
		t.Errorf("v%s is not a valid version string for the binary to report", v)
	}
}

// -transfer-admin asks for the recovery key before naming or listing
// any account, so who holds an account isn't disclosed to someone
// without a key. That ordering is only safe because Redeem prepares a
// rotation without persisting it -- backing out at the list must leave
// the key usable. Verified end-to-end against the binary; this pins the
// invariant the ordering depends on.
func TestRedeemDoesNotConsumeAKeyUntilCommitted(t *testing.T) {
	dir := t.TempDir()
	rs, err := auth.OpenRecovery(filepath.Join(dir, "recovery.json"), filepath.Join(dir, "pepper"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := rs.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Commit(); err != nil {
		t.Fatal(err)
	}

	// Stand in for "operator ran transfer, saw the list, backed out":
	// the key is redeemed but the rotation is never committed.
	if _, err := rs.Redeem(keys[0]); err != nil {
		t.Fatalf("first redeem: %v", err)
	}

	// A fresh store, as the next invocation of the command would open.
	again, err := auth.OpenRecovery(filepath.Join(dir, "recovery.json"), filepath.Join(dir, "pepper"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Redeem(keys[0]); err != nil {
		t.Errorf("the key was consumed by an abandoned transfer: %v -- "+
			"backing out at the account list must cost nothing", err)
	}
}
