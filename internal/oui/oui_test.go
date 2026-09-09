// SPDX-License-Identifier: AGPL-3.0-only

package oui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// registryFixture is six rows copied verbatim from the real
// standards-oui.ieee.org/oui/oui.csv download (fetched 2026-09-09),
// chosen for the four shapes the parser has to survive: a quoted
// organisation name containing a comma, an unquoted one, a block IEEE
// holds in its own name (the MA-M/MA-S parent case), and a private
// listing with an empty address field.
const registryFixture = `Registry,Assignment,Organization Name,Organization Address
MA-L,286FB9,"Nokia Shanghai Bell Co., Ltd.","No.388 Ning Qiao Road,Jin Qiao Pudong Shanghai Shanghai   CN 201206 "
MA-L,D48AFC,Espressif Inc.,"Room 204, Building 2, 690 Bibo Rd, Pudong New Area Shanghai Shanghai CN 201203 "
MA-L,DCA632,Raspberry Pi Trading Ltd,"Maurice Wilkes Building, Cowley Road Cambridge  GB CB4 0DS "
MA-L,E80AB9,"Cisco Systems, Inc",80 West Tasman Drive San Jose CA US 94568
MA-L,B84C87,IEEE Registration Authority,445 Hoes Lane Piscataway NJ US 08554
MA-L,E4F14C,Private,
`

func TestParseCSVReadsRealRegistryRows(t *testing.T) {
	entries, err := parseCSV([]byte(registryFixture))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("got %d entries, want 6: %v", len(entries), entries)
	}
	want := map[string]string{
		// The comma inside the quoted name is the reason this uses
		// encoding/csv: a naive split reports "Nokia Shanghai Bell Co."
		// and nothing notices.
		"286FB9": "Nokia Shanghai Bell Co., Ltd.",
		"D48AFC": "Espressif Inc.",
		"DCA632": "Raspberry Pi Trading Ltd",
		"E80AB9": "Cisco Systems, Inc",
		"B84C87": "IEEE Registration Authority",
		"E4F14C": "Private",
	}
	for oui, name := range want {
		if got := entries[oui]; got != name {
			t.Errorf("entries[%s] = %q, want %q", oui, got, name)
		}
	}
	if _, ok := entries["ASSIGN"]; ok {
		t.Error("the header row was parsed as an assignment")
	}
}

func TestParseCSVRejectsAnEmptyOrForeignBody(t *testing.T) {
	for name, body := range map[string]string{
		"empty":        "",
		"header only":  "Registry,Assignment,Organization Name,Organization Address\n",
		"a login page": "<html><body>Sign in to continue</body></html>",
	} {
		if _, err := parseCSV([]byte(body)); err == nil {
			t.Errorf("%s: parseCSV accepted it, want an error so the last good table is kept", name)
		}
	}
}

func TestParseCSVSkipsOneBadRowNotTheFile(t *testing.T) {
	body := registryFixture + "MA-L,NOTHEX,Nonsense Ltd,\nMA-M,0055DA000,Sub Block Ltd,\n"
	entries, err := parseCSV([]byte(body))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("got %d entries, want the 6 good rows only", len(entries))
	}
}

func TestParseMAC(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantOK  bool
		address string
		oui     string
		local   bool
		group   bool
	}{
		{name: "RouterOS colon form", in: "DC:A6:32:11:22:33", wantOK: true,
			address: "DC:A6:32:11:22:33", oui: "DCA632"},
		{name: "lowercase is normalised up", in: "d4:8a:fc:aa:bb:cc", wantOK: true,
			address: "D4:8A:FC:AA:BB:CC", oui: "D48AFC"},
		{name: "dashes", in: "E8-0A-B9-01-02-03", wantOK: true,
			address: "E8:0A:B9:01:02:03", oui: "E80AB9"},
		{name: "cisco dots", in: "286f.b900.1234", wantOK: true,
			address: "28:6F:B9:00:12:34", oui: "286FB9"},
		{name: "no separators", in: "E4F14C000001", wantOK: true,
			address: "E4:F1:4C:00:00:01", oui: "E4F14C"},

		// The locally-administered bit is the second-least-significant
		// bit of the first octet (0x02). 0x02, 0x06, 0x0A and 0x0E all
		// carry it -- Docker's default 02:42:.. bridge MACs, KVM's
		// 52:54:00 and a randomised phone address all land here, and
		// each means the same thing: no vendor was ever assigned this.
		{name: "docker container LAA", in: "02:42:AC:11:00:02", wantOK: true,
			address: "02:42:AC:11:00:02", oui: "0242AC", local: true},
		{name: "kvm LAA", in: "52:54:00:12:34:56", wantOK: true,
			address: "52:54:00:12:34:56", oui: "525400", local: true},
		{name: "randomised wifi LAA", in: "8A:1F:0C:DE:AD:BE", wantOK: true,
			address: "8A:1F:0C:DE:AD:BE", oui: "8A1F0C", local: true},
		{name: "universally administered is not LAA", in: "DC:A6:32:00:00:01", wantOK: true,
			address: "DC:A6:32:00:00:01", oui: "DCA632", local: false},

		// The group bit (0x01) is a different bit with a different
		// meaning; a source address should never carry it.
		{name: "group bit", in: "01:00:5E:00:00:FB", wantOK: true,
			address: "01:00:5E:00:00:FB", oui: "01005E", group: true},

		{name: "too short", in: "DC:A6:32", wantOK: false},
		{name: "too long", in: "DC:A6:32:11:22:33:44", wantOK: false},
		{name: "not hex", in: "ZZ:A6:32:11:22:33", wantOK: false},
		{name: "empty", in: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Parse(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Address != tc.address || got.OUI != tc.oui {
				t.Errorf("Parse(%q) = %+v, want address %s oui %s", tc.in, got, tc.address, tc.oui)
			}
			if got.LocallyAdministered != tc.local {
				t.Errorf("Parse(%q) LocallyAdministered = %v, want %v", tc.in, got.LocallyAdministered, tc.local)
			}
			if got.Group != tc.group {
				t.Errorf("Parse(%q) Group = %v, want %v", tc.in, got.Group, tc.group)
			}
		})
	}
}

// loadedRegistry is a Registry serving the fixture with no cache path
// and no network client, for the lookup tests.
func loadedRegistry(t *testing.T) *Registry {
	t.Helper()
	r := New("", testLog())
	entries, err := parseCSV([]byte(registryFixture))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	r.entries = entries
	r.fetchedAt = time.Now()
	return r
}

func TestLookup(t *testing.T) {
	r := loadedRegistry(t)

	t.Run("known vendor", func(t *testing.T) {
		_, v := r.LookupMAC("dc:a6:32:11:22:33")
		if !v.Known || v.Name != "Raspberry Pi Trading Ltd" || v.Registry != "MA-L" {
			t.Fatalf("got %+v, want the Raspberry Pi assignment", v)
		}
	})

	// The headline case: a locally-administered address is not a failed
	// lookup, it is the answer that there is nothing to look up.
	t.Run("locally administered has no vendor", func(t *testing.T) {
		m, v := r.LookupMAC("02:42:AC:11:00:02")
		if !m.LocallyAdministered {
			t.Fatal("02:42:.. should parse as locally administered")
		}
		if v.Known {
			t.Fatalf("got %+v, want no vendor for a locally-administered address", v)
		}
		if !strings.Contains(v.Reason, "no vendor exists") {
			t.Errorf("Reason = %q, want it to say no vendor exists", v.Reason)
		}
	})

	t.Run("sub-delegated block names IEEE, not a vendor", func(t *testing.T) {
		_, v := r.LookupMAC("B8:4C:87:00:00:01")
		if v.Known || !v.SubDelegated {
			t.Fatalf("got %+v, want the sub-delegated state rather than a vendor named IEEE", v)
		}
		if v.Name != "" {
			t.Errorf("Name = %q, want it empty rather than %q", v.Name, ieeeItself)
		}
	})

	t.Run("private listing", func(t *testing.T) {
		_, v := r.LookupMAC("E4:F1:4C:00:00:01")
		if v.Known || !v.Private {
			t.Fatalf("got %+v, want the private-listing state", v)
		}
	})

	t.Run("prefix not in the registry", func(t *testing.T) {
		_, v := r.LookupMAC("00:11:22:33:44:55")
		if v.Known || !strings.Contains(v.Reason, "not in the IEEE MA-L registry") {
			t.Fatalf("got %+v, want an explicit not-assigned answer", v)
		}
	})

	t.Run("unreadable address", func(t *testing.T) {
		_, v := r.LookupMAC("not-a-mac")
		if v.Known || !strings.Contains(v.Reason, "not a hardware address") {
			t.Fatalf("got %+v, want an unreadable-address answer", v)
		}
	})
}

// TestLookupBeforeAnyFetch is the "no vendor data yet" contract: an
// empty registry answers with why it is empty, never with a wrong or
// blank vendor.
func TestLookupBeforeAnyFetch(t *testing.T) {
	r := New("", testLog())
	_, v := r.LookupMAC("DC:A6:32:11:22:33")
	if v.Known {
		t.Fatalf("got %+v, want no vendor before the first fetch", v)
	}
	if !strings.Contains(v.Reason, "no vendor data yet") {
		t.Errorf("Reason = %q, want the no-data-yet wording", v.Reason)
	}
	st := r.Status()
	if st.Loaded || !strings.Contains(st.Note, "no vendor data yet") {
		t.Errorf("Status = %+v, want an explicitly unloaded status", st)
	}
}

func TestStatusStatesStalenessRatherThanHidingIt(t *testing.T) {
	r := loadedRegistry(t)
	r.fetchedAt = time.Now().Add(-2 * StaleAfter)
	st := r.Status()
	if !st.Loaded {
		t.Fatal("a stale registry must keep serving")
	}
	if !st.Stale || !strings.Contains(st.Note, "older than") {
		t.Errorf("Status = %+v, want staleness stated", st)
	}
}

// testRegistry builds a Registry pointed at an httptest server. The
// SSRF guard would refuse the server's 127.0.0.1 address, so the client
// is swapped for the test server's own -- the guard has its own test
// below, and this way the fetch path is exercised without it.
func testRegistry(t *testing.T, srv *httptest.Server, cachePath string) *Registry {
	t.Helper()
	r := New(cachePath, testLog())
	// The source URL is a constant in production; a test points the
	// same machinery at its own server through the unexported field,
	// which is also why the SSRF guard has to be swapped out below --
	// it would refuse the server's 127.0.0.1 address, and it has its
	// own test.
	r.url = srv.URL
	r.client = &fetchClient{http: srv.Client()}
	r.loadCache()
	return r
}

func TestRefreshLoadsAndConditionalGETSavesTheDownload(t *testing.T) {
	const etag = `"6aa1123f-3a7fe6"`
	var requests, conditional int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == etag {
			conditional++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		io.WriteString(w, registryFixture)
	}))
	defer srv.Close()

	r := testRegistry(t, srv, "")
	r.Refresh(context.Background())
	if got := r.entryCount(); got != 6 {
		t.Fatalf("after the first refresh: %d entries, want 6", got)
	}

	// Second pass: unchanged upstream, so no body is downloaded and the
	// table survives.
	r.Refresh(context.Background())
	if conditional != 1 {
		t.Errorf("second refresh sent %d conditional requests, want 1", conditional)
	}
	if got := r.entryCount(); got != 6 {
		t.Fatalf("after a 304: %d entries, want the table kept", got)
	}
	if st := r.Status(); !st.Loaded || st.FromCache {
		t.Errorf("Status = %+v, want loaded from the network", st)
	}
}

func TestRefreshKeepsTheLastGoodTable(t *testing.T) {
	body := registryFixture
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	r := testRegistry(t, srv, "")
	r.Refresh(context.Background())
	if r.entryCount() != 6 {
		t.Fatal("setup: first refresh did not load the fixture")
	}

	for _, tc := range []struct {
		name   string
		body   string
		status int
	}{
		{"a 500", registryFixture, http.StatusInternalServerError},
		{"a captive portal page", "<html>Sign in to continue</html>", http.StatusOK},
		{"an empty body", "", http.StatusOK},
		{"a truncated file", "Registry,Assignment,Organization Name,Organization Address\nMA-L,286FB9,Nokia,\n", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, status = tc.body, tc.status
			r.Refresh(context.Background())
			if got := r.entryCount(); got != 6 {
				t.Errorf("%s left %d entries, want the previous 6 kept", tc.name, got)
			}
		})
	}
}

func TestCacheSurvivesARestart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, registryFixture)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "sub", "oui-registry.json")

	first := testRegistry(t, srv, path)
	first.Refresh(context.Background())
	if first.entryCount() != 6 {
		t.Fatal("setup: refresh did not load the fixture")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %o, want 600", perm)
	}

	// A fresh process: vendors must be answerable before any network
	// access happens at all.
	second := New(path, testLog())
	second.url = srv.URL
	second.loadCache()
	if got := second.entryCount(); got != 6 {
		t.Fatalf("restart read %d entries from the cache, want 6", got)
	}
	if st := second.Status(); !st.FromCache {
		t.Errorf("Status = %+v, want it to say the data came from the cache", st)
	}
	_, v := second.LookupMAC("DC:A6:32:11:22:33")
	if !v.Known {
		t.Errorf("got %+v, want a vendor from the cached registry", v)
	}
}

// TestCacheForAnotherSourceIsIgnored: repointing the feed at a mirror
// must not keep serving the previous source's data under the new
// source's name.
func TestCacheForAnotherSourceIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oui-registry.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, registryFixture)
	}))
	defer srv.Close()
	first := testRegistry(t, srv, path)
	first.Refresh(context.Background())

	other := New(path, testLog())
	other.url = "https://example.invalid/oui.csv"
	other.loadCache()
	if got := other.entryCount(); got != 0 {
		t.Fatalf("a cache written for another source served %d entries, want none", got)
	}
}

func TestSSRFGuardRefusesNonPublicAddresses(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1:80",       // loopback
		"10.0.0.1:80",        // RFC1918
		"169.254.169.254:80", // cloud metadata
		"100.64.0.1:80",      // CGNAT -- every net/netip Is* predicate says false
		"192.0.0.1:80",       // IETF protocol assignments
		"[::1]:80",
	} {
		if err := guardDial("tcp", host, nil); err == nil {
			t.Errorf("guardDial permitted %s, want refusal", host)
		}
	}
	for _, host := range []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"} {
		if err := guardDial("tcp", host, nil); err != nil {
			t.Errorf("guardDial refused public %s: %v", host, err)
		}
	}
}
