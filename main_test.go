package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{})

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
		ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{})
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
	ingestOneRecovered(logger, rm, st, devices, macRegistry, fs, h, geo, detector, ru, naming.Resolver{})

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

// TestHTTPSRedirectTargetEmptyAllowlistFallsBackToPriorBehavior covers
// the case TLS.Hosts is left unconfigured (auto-detected SANs instead
// -- see internal/servertls) -- with no explicit ground truth to
// validate against, this keeps the original echo-Host behavior rather
// than guessing.
func TestHTTPSRedirectTargetEmptyAllowlistFallsBackToPriorBehavior(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview.local/", nil)
	r.Host = "mikroview.local"
	got := httpsRedirectTarget(r, nil)
	want := "https://mikroview.local/"
	if got != want {
		t.Errorf("httpsRedirectTarget with an empty allowlist = %q, want %q", got, want)
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

// TestAuthShouldFailClosed pins the exact predicate main() boots against:
// a non-nil auth.Open error with a configured store path is the only case
// that must refuse to start. Both "no persistence configured" (storePath
// == "", err always nil per auth.Open) and "file genuinely doesn't exist"
// (a real fresh install, err == nil) must keep booting normally.
func TestAuthShouldFailClosed(t *testing.T) {
	someErr := errors.New("boom")

	cases := []struct {
		name      string
		err       error
		storePath string
		want      bool
	}{
		{"corrupt file with configured path fails closed", someErr, "/var/lib/mikroview/accounts.json", true},
		{"nil error never fails closed regardless of path", nil, "/var/lib/mikroview/accounts.json", false},
		{"nil error with unconfigured path never fails closed", nil, "", false},
		{"error with unconfigured path never fails closed", someErr, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authShouldFailClosed(tc.err, tc.storePath); got != tc.want {
				t.Errorf("authShouldFailClosed(%v, %q) = %v, want %v", tc.err, tc.storePath, got, tc.want)
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
	if authShouldFailClosed(err, freshPath) {
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
	if !authShouldFailClosed(err, corruptPath) {
		t.Error("authShouldFailClosed reported false for a corrupt existing accounts file")
	}
}

// TestRunResetAuthRefusesWorkingFile is the safety rail on the recovery
// tool itself: it must never touch a file that actually loads without
// error, since silently wiping a working deployment's accounts would turn
// a recovery command into the very footgun it exists to prevent.
func TestRunResetAuthRefusesWorkingFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "accounts.json")

	st, err := auth.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Register("admin", "correct horse battery staple", time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", storePath)

	if code := runResetAuth(); code != 1 {
		t.Errorf("runResetAuth() on a working file = %d, want 1 (refuse)", code)
	}
	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("runResetAuth modified a working accounts file, want it untouched")
	}
}

// TestRunResetAuthNoOpsOnMissingFile covers an operator running
// -reset-auth against a deployment that was never configured with
// persisted accounts in the first place -- nothing to reset, so it must
// report success without creating anything.
func TestRunResetAuthNoOpsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "accounts.json")

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", storePath)

	if code := runResetAuth(); code != 0 {
		t.Errorf("runResetAuth() on a never-created path = %d, want 0", code)
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Errorf("runResetAuth created %q, want it left absent", storePath)
	}
}

// TestRunResetAuthRenamesCorruptFile is the actual recovery path this
// segment's security fix depends on: given a genuinely broken accounts
// file, -reset-auth must move it aside (never delete it -- it's the only
// forensic evidence of what happened) so the next restart reaches the
// legitimate first-run setup screen instead of refusing to boot forever.
func TestRunResetAuthRenamesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(storePath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", storePath)

	if code := runResetAuth(); code != 0 {
		t.Errorf("runResetAuth() on a corrupt file = %d, want 0", code)
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Errorf("runResetAuth left %q in place, want it moved aside", storePath)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "accounts.json.broken-") {
			found = true
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "{not valid json" {
				t.Errorf("renamed backup content = %q, want the original corrupt content preserved", data)
			}
		}
	}
	if !found {
		t.Errorf("expected a %s.broken-<unix> sibling in %s, entries: %v", "accounts.json", dir, entries)
	}

	// The corrupt file is gone, so a subsequent boot must see this exactly
	// like a fresh install: auth.Open returns a nil error, and
	// authShouldFailClosed agrees it's safe to continue.
	_, err = auth.Open(storePath)
	if err != nil {
		t.Fatalf("auth.Open after runResetAuth returned an error, want the fresh-install nil: %v", err)
	}
	if authShouldFailClosed(err, storePath) {
		t.Error("authShouldFailClosed reported true after a successful reset, want the boot to proceed")
	}
}
