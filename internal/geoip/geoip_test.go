// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/oschwald/maxminddb-golang/v2"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/settings"
)

// Fixtures are built with MaxMind's own writer (already a test
// dependency, #1110): mikroview bundles no database by design. The
// networks are the RFC 5737 / RFC 3849 documentation ranges -- no real
// address space and nobody's data -- and the countries are invented.

type layout int

const (
	nested layout = iota // MaxMind / DB-IP: country.iso_code
	flat                 // IPinfo Lite: country_code, country (a string), asn, as_name
)

// buildDB returns an MMDB mapping every documentation range to iso.
func buildDB(t *testing.T, l layout, iso string) []byte {
	t.Helper()
	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "mikroview-test",
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}
	for _, cidr := range []string{"203.0.113.0/24", "198.51.100.0/24", "2001:db8::/32"} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		var rec mmdbtype.Map
		switch l {
		case flat:
			rec = mmdbtype.Map{
				"country_code":   mmdbtype.String(iso),
				"country":        mmdbtype.String("Somewhere"),
				"continent_code": mmdbtype.String("OC"),
				"asn":            mmdbtype.String("AS64500"),
				"as_name":        mmdbtype.String("Example Networks"),
			}
		default:
			rec = mmdbtype.Map{
				"country": mmdbtype.Map{
					"iso_code": mmdbtype.String(iso),
					"names":    mmdbtype.Map{"en": mmdbtype.String("Somewhere")},
				},
			}
		}
		if err := w.Insert(network, rec); err != nil {
			t.Fatalf("Insert(%s): %v", cidr, err)
		}
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarMember struct {
	name string
	body []byte
	typ  byte
}

func tarGz(t *testing.T, members ...tarMember) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, m := range members {
		typ := m.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body)), Typeflag: typ}
		if typ != tar.TypeReg {
			hdr.Size = 0
			hdr.Linkname = "elsewhere"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write(m.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func maxmindArchive(t *testing.T, db []byte) []byte {
	return tarGz(t,
		tarMember{name: "GeoLite2-Country_20260926/COPYRIGHT.txt", body: []byte("test")},
		tarMember{name: "GeoLite2-Country_20260926/GeoLite2-Country.mmdb", body: db},
	)
}

// logBuf captures everything a Manager logs, for the redaction tests.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func testSealer(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeProvider serves all three sources from one httptest server.
type fakeProvider struct {
	t  *testing.T
	mu sync.Mutex
	// files by path; a missing path is a 404.
	files map[string][]byte
	etags map[string]string
	// status overrides the response for a path.
	status map[string]int
	hits   map[string]int
	// gotAuth is the last basic-auth pair the MaxMind path saw.
	gotUser, gotPass string
	gotToken         string
}

func newFakeProvider(t *testing.T) (*fakeProvider, *httptest.Server) {
	fp := &fakeProvider{t: t, files: map[string][]byte{}, etags: map[string]string{}, status: map[string]int{}, hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp.mu.Lock()
		defer fp.mu.Unlock()
		fp.hits[r.URL.Path]++
		if r.URL.Path == "/maxmind" {
			fp.gotUser, fp.gotPass, _ = r.BasicAuth()
		}
		if r.URL.Path == "/ipinfo" {
			fp.gotToken = r.URL.Query().Get("token")
		}
		if code, ok := fp.status[r.URL.Path]; ok {
			// A provider's error page echoing the request back is
			// exactly how a token ends up somewhere it should not.
			http.Error(w, "error for "+r.URL.String(), code)
			return
		}
		body, ok := fp.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if et := fp.etags[r.URL.Path]; et != "" {
			if r.Header.Get("If-None-Match") == et {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", et)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return fp, srv
}

func (fp *fakeProvider) set(path string, body []byte) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.files[path] = body
	delete(fp.status, path)
}

func (fp *fakeProvider) fail(path string, code int) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.status[path] = code
}

func (fp *fakeProvider) hitCount(path string) int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return fp.hits[path]
}

// fixedNow is a date whose month and previous month are known.
var fixedNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type testEnv struct {
	m     *Manager
	fp    *fakeProvider
	keys  *settings.Store
	logs  *logBuf
	cache string
	srv   *httptest.Server
}

func newEnv(t *testing.T, cacheDir string, keys *settings.Store, sealer *retention.Key) *testEnv {
	t.Helper()
	fp, srv := newFakeProvider(t)
	logs := &logBuf{}
	opts := Options{CacheDir: cacheDir, Sealer: sealer, Log: slog.New(slog.NewTextHandler(logs, nil))}
	if keys != nil {
		opts.Keys = keys
	}
	m := New(opts)
	t.Cleanup(m.Close)
	pointAt(m, srv)
	return &testEnv{m: m, fp: fp, keys: keys, logs: logs, cache: cacheDir, srv: srv}
}

// pointAt swaps the SSRF-guarded client and the real URLs for the
// httptest server's: the guard would refuse 127.0.0.1, which is the
// point of it (TestSSRFGuard covers that).
func pointAt(m *Manager, srv *httptest.Server) {
	m.client = srv.Client()
	m.eps = endpoints{
		dbip:    func(month string) string { return srv.URL + "/dbip-" + month },
		ipinfo:  srv.URL + "/ipinfo",
		maxmind: srv.URL + "/maxmind",
	}
	m.now = func() time.Time { return fixedNow }
}

func openSettings(t *testing.T, dir string) *settings.Store {
	t.Helper()
	s, err := settings.Open(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDecodesBothRecordLayouts(t *testing.T) {
	for _, c := range []struct {
		name      string
		layout    layout
		wantASN   uint
		wantOwner string
	}{
		{"nested (MaxMind, DB-IP)", nested, 0, ""},
		{"flat (IPinfo Lite)", flat, 64500, "Example Networks"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := maxminddb.OpenBytes(buildDB(t, c.layout, "nz"))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			res := r.Lookup(netip.MustParseAddr("203.0.113.5"))
			if got := countryFrom(res); got != "NZ" {
				t.Errorf("countryFrom = %q, want NZ", got)
			}
			asn, name := ownerFrom(res)
			if asn != c.wantASN || name != c.wantOwner {
				t.Errorf("ownerFrom = %d %q, want %d %q", asn, name, c.wantASN, c.wantOwner)
			}
			miss := r.Lookup(netip.MustParseAddr("192.0.2.7"))
			if got := countryFrom(miss); got != "" {
				t.Errorf("countryFrom on a miss = %q, want empty", got)
			}
		})
	}
}

func TestParseASN(t *testing.T) {
	for in, want := range map[any]uint{
		"AS13335": 13335, "as15169": 15169, "13335": 13335, uint64(64500): 64500,
		uint32(7): 7, "": 0, "ASX": 0, "AS": 0, nil: 0, int(-1): 0, "AS99999999999": 0,
	} {
		if got := parseASN(in); got != want {
			t.Errorf("parseASN(%#v) = %d, want %d", in, got, want)
		}
	}
}

func TestNoSourceMeansNoAnswers(t *testing.T) {
	env := newEnv(t, "", nil, nil)
	if env.m.Available() || env.m.Source() != "" {
		t.Fatal("a fresh manager reported a source before anything was fetched")
	}
	if _, ok := env.m.Country("203.0.113.5"); ok {
		t.Error("Country answered with nothing loaded")
	}
	var nilManager *Manager
	if _, ok := nilManager.Country("203.0.113.5"); ok || nilManager.Available() {
		t.Error("a nil manager must answer 'unknown'")
	}
	if st := nilManager.Status(); st.Source != nil {
		t.Error("a nil manager's status names a source")
	}
}

// TestPrecedence is the Fable call: IPinfo beats MaxMind beats DB-IP,
// setting a key switches once its file lands, removing one falls back.
func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	keys := openSettings(t, dir)
	env := newEnv(t, filepath.Join(dir, "geoip"), keys, testSealer(t))
	env.fp.set("/dbip-2026-09", gz(t, buildDB(t, nested, "NZ")))
	env.fp.set("/maxmind", maxmindArchive(t, buildDB(t, nested, "JP")))
	env.fp.set("/ipinfo", buildDB(t, flat, "IS"))
	ctx := context.Background()

	env.m.RefreshDue(ctx)
	if got := env.m.Source(); got != "dbip" {
		t.Fatalf("source = %q, want dbip with no keys set", got)
	}
	if c, _ := env.m.Country("203.0.113.5"); c != "NZ" {
		t.Errorf("country = %q, want DB-IP's NZ", c)
	}
	if _, _, ok := env.m.Owner("203.0.113.5"); ok {
		t.Error("Owner answered while DB-IP (no owner data) is in use")
	}

	if err := env.m.SetMaxMind("123456", "lic_ABCdef", "admin"); err != nil {
		t.Fatal(err)
	}
	env.m.RefreshDue(ctx)
	if got := env.m.Source(); got != "maxmind" {
		t.Fatalf("source = %q, want maxmind once its key is set", got)
	}
	if env.fp.gotUser != "123456" || env.fp.gotPass != "lic_ABCdef" {
		t.Errorf("MaxMind saw basic auth %q:%q, want the stored account ID and licence key", env.fp.gotUser, env.fp.gotPass)
	}
	if c, _ := env.m.Country("203.0.113.5"); c != "JP" {
		t.Errorf("country = %q, want MaxMind's JP", c)
	}

	if err := env.m.SetIPinfoToken("tok123abc", "admin"); err != nil {
		t.Fatal(err)
	}
	env.m.RefreshDue(ctx)
	if got := env.m.Source(); got != "ipinfo" {
		t.Fatalf("source = %q, want ipinfo, which beats both", got)
	}
	if c, _ := env.m.Country("203.0.113.5"); c != "IS" {
		t.Errorf("country = %q, want IPinfo's IS", c)
	}
	if asn, name, ok := env.m.Owner("203.0.113.5"); !ok || asn != 64500 || name != "Example Networks" {
		t.Errorf("Owner = %d %q %v, want 64500 Example Networks", asn, name, ok)
	}
	if env.fp.gotToken != "tok123abc" {
		t.Errorf("IPinfo saw token %q", env.fp.gotToken)
	}

	if err := env.m.RemoveKey(IPinfo); err != nil {
		t.Fatal(err)
	}
	if got := env.m.Source(); got != "maxmind" {
		t.Errorf("source = %q after removing IPinfo's key, want an immediate fall back to maxmind", got)
	}
	if _, err := os.Stat(filepath.Join(env.cache, "ipinfo.mmdb")); !os.IsNotExist(err) {
		t.Errorf("IPinfo's cached file survived its key's removal (stat err %v)", err)
	}
	if err := env.m.RemoveKey(MaxMind); err != nil {
		t.Fatal(err)
	}
	if got := env.m.Source(); got != "dbip" {
		t.Errorf("source = %q after removing both keys, want dbip", got)
	}

	st := env.m.Status()
	if st.Source == nil || *st.Source != "dbip" || !st.Sources.DBIP.Loaded || st.Sources.IPinfo.KeySet || st.Sources.MaxMind.Loaded {
		t.Errorf("status after removals = %+v", st)
	}
}

func TestDBIPFallsBackToLastMonthOn404(t *testing.T) {
	env := newEnv(t, "", nil, nil)
	env.fp.set("/dbip-2026-08", gz(t, buildDB(t, nested, "NZ")))
	env.m.RefreshDue(context.Background())
	if env.m.Source() != "dbip" {
		t.Fatal("last month's file was not used while this month's 404s")
	}
	st := env.m.Status().Sources.DBIP
	if st.NextRefresh == nil || st.NextRefresh.Sub(fixedNow) != 24*time.Hour {
		t.Errorf("nextRefresh = %v, want a retry for this month's file a day later", st.NextRefresh)
	}
	// This month's file appears: the next pass takes it, and then waits
	// for the month after.
	env.fp.set("/dbip-2026-09", gz(t, buildDB(t, nested, "JP")))
	env.m.now = func() time.Time { return fixedNow.Add(25 * time.Hour) }
	env.m.RefreshDue(context.Background())
	if c, _ := env.m.Country("203.0.113.5"); c != "JP" {
		t.Errorf("country = %q, want this month's file once it is published", c)
	}
	st = env.m.Status().Sources.DBIP
	if st.NextRefresh == nil || !st.NextRefresh.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("nextRefresh = %v, want the first of next month", st.NextRefresh)
	}
	// Due again before then (forced): nothing is downloaded.
	before := env.fp.hitCount("/dbip-2026-09")
	env.m.mu.Lock()
	env.m.sources[DBIP].force = true
	env.m.mu.Unlock()
	env.m.RefreshDue(context.Background())
	if env.fp.hitCount("/dbip-2026-09") != before {
		t.Error("the current month's file was downloaded again although it is final")
	}
}

func TestFailureKeepsLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	keys := openSettings(t, dir)
	env := newEnv(t, filepath.Join(dir, "geoip"), keys, testSealer(t))
	env.fp.set("/ipinfo", buildDB(t, flat, "IS"))
	if err := env.m.SetIPinfoToken("tok123abc", "admin"); err != nil {
		t.Fatal(err)
	}
	env.m.RefreshDue(context.Background())
	if env.m.Source() != "ipinfo" {
		t.Fatal("setup: ipinfo did not load")
	}

	for name, apply := range map[string]func(){
		"an HTML portal page": func() { env.fp.set("/ipinfo", []byte("<html>log in to the hotel wifi</html>")) },
		"a server error":      func() { env.fp.fail("/ipinfo", http.StatusBadGateway) },
		"a truncated gzip":    func() { env.fp.set("/ipinfo", gz(t, buildDB(t, flat, "JP"))[:40]) },
	} {
		t.Run(name, func(t *testing.T) {
			apply()
			env.m.mu.Lock()
			env.m.sources[IPinfo].force = true
			env.m.mu.Unlock()
			env.m.RefreshDue(context.Background())
			if c, _ := env.m.Country("203.0.113.5"); c != "IS" {
				t.Errorf("country = %q after a failed refresh, want the last good IS", c)
			}
			st := env.m.Status().Sources.IPinfo
			if st.LastError == nil || !st.Loaded {
				t.Errorf("status = %+v, want loaded with lastError set", st)
			}
			if st.NextRefresh == nil || st.NextRefresh.Sub(fixedNow) != retryAfter {
				t.Errorf("nextRefresh = %v, want a retry in %s", st.NextRefresh, retryAfter)
			}
		})
	}
}

func TestConditionalGETKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	keys := openSettings(t, dir)
	env := newEnv(t, filepath.Join(dir, "geoip"), keys, testSealer(t))
	env.fp.set("/ipinfo", buildDB(t, flat, "IS"))
	env.fp.etags["/ipinfo"] = `"v1"`
	if err := env.m.SetIPinfoToken("tok123abc", "admin"); err != nil {
		t.Fatal(err)
	}
	env.m.RefreshDue(context.Background())
	later := fixedNow.Add(25 * time.Hour)
	env.m.now = func() time.Time { return later }
	env.m.RefreshDue(context.Background())
	st := env.m.Status().Sources.IPinfo
	if !st.Loaded || st.FetchedAt == nil || !st.FetchedAt.Equal(later) {
		t.Errorf("after a 304: %+v, want still loaded with fetchedAt moved to %v", st, later)
	}
}

// TestIPinfoTokenNeverLeaks: the token rides in the download URL, so
// every error string, status field and log line from that path must
// strip it. Covers a provider error page that echoes the request back,
// and a transport error (the real SSRF guard refusing the httptest
// server's loopback address), whose *url.Error prints the whole URL.
func TestIPinfoTokenNeverLeaks(t *testing.T) {
	const token = "sekritTOKEN42"
	for _, c := range []struct {
		name  string
		setup func(env *testEnv)
	}{
		{"provider error page echoing the URL", func(env *testEnv) { env.fp.fail("/ipinfo", http.StatusInternalServerError) }},
		{"provider refusing the key", func(env *testEnv) { env.fp.fail("/ipinfo", http.StatusForbidden) }},
		{"transport error", func(env *testEnv) { env.m.client = newFetchClient() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			env := newEnv(t, filepath.Join(dir, "geoip"), openSettings(t, dir), testSealer(t))
			c.setup(env)
			if err := env.m.SetIPinfoToken(token, "admin"); err != nil {
				t.Fatal(err)
			}
			env.m.RefreshDue(context.Background())
			st := env.m.Status().Sources.IPinfo
			if st.LastError == nil {
				t.Fatal("no lastError recorded -- this test is not exercising a failure")
			}
			if strings.Contains(*st.LastError, token) {
				t.Errorf("lastError leaks the token: %s", *st.LastError)
			}
			if strings.Contains(env.logs.String(), token) {
				t.Errorf("a log line leaks the token:\n%s", env.logs.String())
			}
			settingsDoc, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(settingsDoc, []byte(token)) {
				t.Error("the settings document holds the token in the clear")
			}
		})
	}
}

func TestCleanErrRedactsURLErrors(t *testing.T) {
	_, err := http.Get("http://127.0.0.1:1/x?token=abc123secret")
	if err == nil {
		t.Skip("something is listening on port 1")
	}
	msg := cleanErr(err, nil)
	if strings.Contains(msg, "abc123secret") || strings.Contains(msg, "token=") {
		t.Errorf("cleanErr kept the query string: %s", msg)
	}
	if got := redact("x sek%2Fret y", []string{"sek/ret"}); strings.Contains(got, "sek") {
		t.Errorf("redact missed the URL-escaped form: %s", got)
	}
}

func TestMaxMindArchiveChecks(t *testing.T) {
	db := buildDB(t, nested, "JP")
	for _, c := range []struct {
		name    string
		archive []byte
		wantErr string
	}{
		{"two databases", tarGz(t, tarMember{name: "a/one.mmdb", body: db}, tarMember{name: "a/two.mmdb", body: db}), "more than one"},
		{"no database", tarGz(t, tarMember{name: "a/LICENSE.txt", body: []byte("x")}), "no .mmdb"},
		{"path climbing out", tarGz(t, tarMember{name: "../../etc/GeoLite2-Country.mmdb", body: db}), "unsafe path"},
		{"absolute path", tarGz(t, tarMember{name: "/tmp/GeoLite2-Country.mmdb", body: db}), "unsafe path"},
		{"symlink member", tarGz(t, tarMember{name: "a/GeoLite2-Country.mmdb", typ: tar.TypeSymlink}), "not a regular file"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := extractTarGz(bytes.NewReader(c.archive), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("extractTarGz = %v, want an error containing %q", err, c.wantErr)
			}
		})
	}
	var out bytes.Buffer
	if err := extractTarGz(bytes.NewReader(maxmindArchive(t, db)), &out); err != nil || !bytes.Equal(out.Bytes(), db) {
		t.Errorf("a well-formed archive: err %v, %d bytes (want %d)", err, out.Len(), len(db))
	}
}

func TestCapReaderRefusesOversizedBodies(t *testing.T) {
	c := &capReader{r: bytes.NewReader(make([]byte, 10)), left: 4}
	_, err := copyCappedTo(c)
	if !errors.Is(err, errTooLarge) {
		t.Errorf("err = %v, want errTooLarge", err)
	}
}

func copyCappedTo(r *capReader) (int, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Len(), err
}

func TestKeyEntry(t *testing.T) {
	t.Run("refused with no retention key", func(t *testing.T) {
		dir := t.TempDir()
		env := newEnv(t, "", openSettings(t, dir), nil)
		if err := env.m.SetIPinfoToken("tok123abc", "admin"); !errors.Is(err, ErrNoSealKey) {
			t.Errorf("err = %v, want ErrNoSealKey", err)
		}
		if _, ok := env.keys.GeoKey("ipinfo"); ok {
			t.Error("a key was stored with no retention key to seal it")
		}
	})
	t.Run("refused with no settings store", func(t *testing.T) {
		env := newEnv(t, "", nil, testSealer(t))
		if err := env.m.SetIPinfoToken("tok123abc", "admin"); !errors.Is(err, ErrNoKeyStore) {
			t.Errorf("err = %v, want ErrNoKeyStore", err)
		}
	})
	t.Run("validation", func(t *testing.T) {
		dir := t.TempDir()
		env := newEnv(t, "", openSettings(t, dir), testSealer(t))
		for _, bad := range []string{"", "has space", "tab\tin", strings.Repeat("a", 257), "nul\x00", "ünicode"} {
			var ve *ValidationError
			if err := env.m.SetIPinfoToken(bad, "admin"); !errors.As(err, &ve) {
				t.Errorf("SetIPinfoToken(%q) = %v, want a ValidationError", bad, err)
			} else if bad != "" && strings.Contains(ve.Error(), bad) {
				t.Errorf("the validation message repeats the value: %s", ve)
			}
		}
		if err := env.m.SetMaxMind("12:34", "lic", "admin"); err == nil {
			t.Error("an account ID with a colon was accepted")
		}
		if err := env.m.SetMaxMind("1234", "", "admin"); err == nil {
			t.Error("an empty licence key was accepted")
		}
	})
	t.Run("sealed at rest and reopened after a restart", func(t *testing.T) {
		dir := t.TempDir()
		sealer := testSealer(t)
		env := newEnv(t, filepath.Join(dir, "geoip"), openSettings(t, dir), sealer)
		env.fp.set("/maxmind", maxmindArchive(t, buildDB(t, nested, "JP")))
		if err := env.m.SetMaxMind("998877", "licSECRETvalue", "alice"); err != nil {
			t.Fatal(err)
		}
		env.m.RefreshDue(context.Background())
		doc, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		if bytes.Contains(doc, []byte("licSECRETvalue")) || bytes.Contains(doc, []byte("998877")) {
			t.Error("the settings document holds the MaxMind credentials in the clear")
		}
		env.m.Close()

		restarted := New(Options{CacheDir: filepath.Join(dir, "geoip"), Keys: openSettings(t, dir), Sealer: sealer, Log: slog.New(slog.NewTextHandler(&logBuf{}, nil))})
		defer restarted.Close()
		if restarted.Source() != "maxmind" {
			t.Errorf("source after a restart = %q, want maxmind served from the cache", restarted.Source())
		}
		st := restarted.Status().Sources.MaxMind
		if !st.KeySet || st.SetBy == nil || *st.SetBy != "alice" || st.SetAt == nil {
			t.Errorf("key status after a restart = %+v", st)
		}
		if restarted.sources[MaxMind].cred.LicenseKey != "licSECRETvalue" {
			t.Error("the stored key did not reopen under the same retention key")
		}

		// A different retention key cannot open it: reported, not used.
		other, _ := retention.NewKeyFromMaterial(bytes.Repeat([]byte{9}, 32))
		wrong := New(Options{CacheDir: filepath.Join(dir, "geoip"), Keys: openSettings(t, dir), Sealer: other, Log: slog.New(slog.NewTextHandler(&logBuf{}, nil))})
		defer wrong.Close()
		st = wrong.Status().Sources.MaxMind
		if !st.KeySet || st.LastError == nil || wrong.Source() == "maxmind" {
			t.Errorf("under the wrong retention key: status %+v, source %q", st, wrong.Source())
		}
	})
}

func TestSSRFGuard(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "203.0.113.5", "::ffff:127.0.0.1"} {
		if err := guardDial("tcp", net.JoinHostPort(a, "443"), nil); err == nil {
			t.Errorf("guardDial let %s through", a)
		}
	}
	for _, a := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if err := guardDial("tcp", net.JoinHostPort(a, "443"), nil); err != nil {
			t.Errorf("guardDial refused public %s: %v", a, err)
		}
	}
}

func TestLookupFiltersNonPublicAddresses(t *testing.T) {
	env := newEnv(t, "", nil, nil)
	env.fp.set("/dbip-2026-09", gz(t, buildDB(t, nested, "NZ")))
	env.m.RefreshDue(context.Background())
	for _, c := range []struct {
		ip     string
		want   string
		wantOK bool
	}{
		{"203.0.113.5", "NZ", true},
		{"::ffff:203.0.113.5", "NZ", true},
		{"2001:db8::1", "NZ", true},
		{"192.0.2.7", "", false},
		{"192.168.1.1", "", false},
		{"127.0.0.1", "", false},
		{"not-an-ip", "", false},
		{"", "", false},
	} {
		if got, ok := env.m.Country(c.ip); got != c.want || ok != c.wantOK {
			t.Errorf("Country(%q) = %q %v, want %q %v", c.ip, got, ok, c.want, c.wantOK)
		}
	}
}
