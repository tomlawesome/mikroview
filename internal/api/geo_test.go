// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/geoip"
)

// fakeGeo stands in for internal/geoip's Manager: these tests are about
// the endpoints -- address filtering, error mapping, what is audited and
// what is never echoed -- and the manager's own fetch and sealing are
// tested against real databases in internal/geoip.
type fakeGeo struct {
	mu      sync.Mutex
	source  string
	setErr  error
	lookups []string
	token   string
	acct    string
	lic     string
	removed []geoip.SourceID
}

func (f *fakeGeo) Country(ip string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups = append(f.lookups, ip)
	return "GB", true
}

func (f *fakeGeo) Owner(ip string) (uint, string, bool) { return 13335, "Cloudflare, Inc.", true }
func (f *fakeGeo) Source() string                       { return f.source }

func (f *fakeGeo) Status() geoip.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	var st geoip.Status
	if f.source != "" {
		src := f.source
		st.Source = &src
	}
	st.Sources.IPinfo.KeySet = f.token != ""
	st.Sources.MaxMind.KeySet = f.lic != ""
	return st
}

func (f *fakeGeo) SetIPinfoToken(token, actor string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.token = token
	return nil
}

func (f *fakeGeo) SetMaxMind(accountID, licenseKey, actor string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.acct, f.lic = accountID, licenseKey
	return nil
}

func (f *fakeGeo) RemoveKey(id geoip.SourceID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	if id == geoip.IPinfo {
		f.token = ""
	}
	return nil
}

func geoRequest(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	ts := httptest.NewServer(h)
	defer ts.Close()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestGeoLookupFiltersAddresses(t *testing.T) {
	s, _ := newTestServer(t)
	fg := &fakeGeo{source: "ipinfo"}
	s.Geo = fg
	h := asViewer(s.mux())

	for _, bad := range []string{"", "not-an-ip", "1.2.3.4/24", "fe80::1%25eth0"} {
		if code, _ := geoRequest(t, h, http.MethodGet, "/api/geo/lookup?ip="+bad, ""); code != http.StatusBadRequest {
			t.Errorf("ip=%q: status %d, want 400", bad, code)
		}
	}

	allNull := `{"country":null,"asn":null,"asName":null}` + "\n"
	for _, private := range []string{"192.168.1.10", "10.0.0.1", "127.0.0.1", "100.64.1.1", "169.254.169.254", "fd00::1", "203.0.113.9", "::ffff:10.0.0.1"} {
		code, body := geoRequest(t, h, http.MethodGet, "/api/geo/lookup?ip="+private, "")
		if code != http.StatusOK || body != allNull {
			t.Errorf("ip=%s: %d %s, want 200 with every field null", private, code, body)
		}
	}
	if len(fg.lookups) != 0 {
		t.Errorf("non-public addresses reached the lookup: %v", fg.lookups)
	}

	code, body := geoRequest(t, h, http.MethodGet, "/api/geo/lookup?ip=::ffff:1.1.1.1", "")
	want := `{"country":"GB","asn":13335,"asName":"Cloudflare, Inc."}` + "\n"
	if code != http.StatusOK || body != want {
		t.Errorf("public address: %d %s, want %s", code, body, want)
	}
	if len(fg.lookups) != 1 || fg.lookups[0] != "1.1.1.1" {
		t.Errorf("looked up %v, want the unmapped 1.1.1.1", fg.lookups)
	}

	s.Geo = nil
	if code, body := geoRequest(t, h, http.MethodGet, "/api/geo/lookup?ip=1.1.1.1", ""); code != http.StatusOK || body != allNull {
		t.Errorf("no Geo configured: %d %s, want all nulls", code, body)
	}
}

func TestGeoSettingsKeysAreWriteOnly(t *testing.T) {
	s, _ := newTestServer(t)
	fg := &fakeGeo{source: "dbip"}
	s.Geo = fg
	h := asAdmin(s.mux())

	const token = "tok9secretVALUE"
	code, body := geoRequest(t, h, http.MethodPut, "/api/settings/geo/ipinfo", `{"token":"`+token+`"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT ipinfo: %d %s", code, body)
	}
	if fg.token != token {
		t.Errorf("the manager received %q", fg.token)
	}
	var st geoip.Status
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("PUT did not answer with the GET shape: %v (%s)", err, body)
	}
	if !st.Sources.IPinfo.KeySet {
		t.Error("PUT's answer does not say the key is set")
	}

	const lic = "licSECRET_abc"
	if code, body := geoRequest(t, h, http.MethodPut, "/api/settings/geo/maxmind", `{"accountId":"424242","licenseKey":"`+lic+`"}`); code != http.StatusOK {
		t.Fatalf("PUT maxmind: %d %s", code, body)
	}
	if fg.acct != "424242" || fg.lic != lic {
		t.Errorf("the manager received %q / %q", fg.acct, fg.lic)
	}

	_, get := geoRequest(t, h, http.MethodGet, "/api/settings/geo", "")
	for _, secret := range []string{token, lic} {
		if strings.Contains(get, secret) || strings.Contains(body, secret) {
			t.Errorf("a response carries the key %q", secret)
		}
	}
	for _, want := range []string{`"source":"dbip"`, `"dbip":{`, `"ipinfo":{"keySet":true`, `"maxmind":{"keySet":true`, `"fetchedAt":null`, `"nextRefresh":null`, `"lastError":null`, `"setAt":null`, `"setBy":null`, `"loaded":false`} {
		if !strings.Contains(get, want) {
			t.Errorf("GET /api/settings/geo lacks %s:\n%s", want, get)
		}
	}

	if code, body := geoRequest(t, h, http.MethodDelete, "/api/settings/geo/ipinfo", ""); code != http.StatusOK || !strings.Contains(body, `"ipinfo":{"keySet":false`) {
		t.Errorf("DELETE ipinfo: %d %s", code, body)
	}
	if code, _ := geoRequest(t, h, http.MethodDelete, "/api/settings/geo/maxmind", ""); code != http.StatusOK {
		t.Errorf("DELETE maxmind: %d", code)
	}
	if len(fg.removed) != 2 || fg.removed[0] != geoip.IPinfo || fg.removed[1] != geoip.MaxMind {
		t.Errorf("removed %v", fg.removed)
	}

	entries := s.Audit.Query(audit.Query{}).Entries
	var got []string
	for _, e := range entries {
		if e.Action != "settings.geo" {
			continue
		}
		got = append(got, e.Target+":"+e.Detail)
		for _, secret := range []string{token, lic, "424242"} {
			if strings.Contains(e.Detail, secret) || strings.Contains(e.Target, secret) {
				t.Errorf("an audit entry carries a credential: %+v", e)
			}
		}
		if e.Actor != "admin" {
			t.Errorf("audit actor = %q, want admin", e.Actor)
		}
	}
	want := []string{"ipinfo:key set", "maxmind:key set", "ipinfo:key removed", "maxmind:key removed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("audit entries %v, want %v", got, want)
	}
}

func TestGeoSettingsErrorMapping(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"no retention key", geoip.ErrNoSealKey, http.StatusConflict},
		{"no settings store", geoip.ErrNoKeyStore, http.StatusConflict},
		{"persist failure", errors.New("disk full"), http.StatusInternalServerError},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			s.Geo = &fakeGeo{setErr: c.err}
			code, body := geoRequest(t, asAdmin(s.mux()), http.MethodPut, "/api/settings/geo/ipinfo", `{"token":"abc"}`)
			if code != c.want {
				t.Errorf("status %d (%s), want %d", code, body, c.want)
			}
			if c.err == geoip.ErrNoSealKey && !strings.Contains(body, "history.keyFile") {
				t.Errorf("the refusal does not say what to do: %s", body)
			}
		})
	}

	// Real validation, through the real manager's rules.
	s, _ := newTestServer(t)
	s.Geo = geoip.New(geoip.Options{})
	for _, body := range []string{`{"token":""}`, `{"token":"has space"}`, `{"token":"` + strings.Repeat("x", 300) + `"}`, `not json`} {
		if code, _ := geoRequest(t, asAdmin(s.mux()), http.MethodPut, "/api/settings/geo/ipinfo", body); code != http.StatusBadRequest {
			t.Errorf("PUT %s: status %d, want 400", body, code)
		}
	}
	if code, body := geoRequest(t, asAdmin(s.mux()), http.MethodPut, "/api/settings/geo/maxmind", `{"accountId":"1","licenseKey":"ok"}`); code != http.StatusConflict {
		t.Errorf("a valid key on a manager with no store: %d %s, want 409", code, body)
	}
}

func TestGeoSettingsRefusesNonAdmins(t *testing.T) {
	s, _ := newTestServer(t)
	s.Geo = &fakeGeo{}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/settings/geo", ""},
		{http.MethodPut, "/api/settings/geo/ipinfo", `{"token":"abc"}`},
		{http.MethodDelete, "/api/settings/geo/maxmind", ""},
	} {
		if code, _ := geoRequest(t, asUser(s.mux()), c.method, c.path, c.body); code != http.StatusForbidden {
			t.Errorf("user %s %s: %d, want 403", c.method, c.path, code)
		}
	}
}
