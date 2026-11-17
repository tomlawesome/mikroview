// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Tests for #1500: a route that reads a JSON body refuses a field it does
// not know, with 400, and changes nothing. Bodies of known fields only
// behave as before.

// rawBody sends body verbatim (so invalid JSON can be sent) with the CSRF
// header the frontend always sends, and returns what the caller sees.
func rawBody(t *testing.T, client *http.Client, method, url, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

func strictHistoryServer(t *testing.T) (*httptest.Server, *fakeHistoryControl) {
	t.Helper()
	s, _ := newTestServer(t)
	ctl := &fakeHistoryControl{state: HistorySettings{Keyed: true, Days: 30, MaxBytes: 1 << 30}}
	s.HistoryControl = ctl
	ts := httptest.NewServer(asAdmin(s.mux()))
	t.Cleanup(ts.Close)
	return ts, ctl
}

const historyURLPath = "/api/settings/history"

func TestStrictJSONHistorySettingsRefusesUnknownField(t *testing.T) {
	ts, ctl := strictHistoryServer(t)
	code, _, body := rawBody(t, http.DefaultClient, http.MethodPut, ts.URL+historyURLPath,
		`{"enabled":true,"days":30,"maxBytes":1073741824,"retainForever":true}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	if n := len(ctl.calls()); n != 0 {
		t.Errorf("the control was applied %d times by a refused request, want 0", n)
	}
	if ctl.HistorySettings().Enabled {
		t.Error("history was switched on by a refused request")
	}
}

func TestStrictJSONHistorySettingsRefusesMisspeltField(t *testing.T) {
	ts, ctl := strictHistoryServer(t)
	// snake_case spelling of a real camelCase field, sent beside the right one.
	code, _, body := rawBody(t, http.DefaultClient, http.MethodPut, ts.URL+historyURLPath,
		`{"enabled":true,"days":30,"maxBytes":1073741824,"max_bytes":5}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	// And in place of it.
	code, _, body = rawBody(t, http.DefaultClient, http.MethodPut, ts.URL+historyURLPath,
		`{"enabled":true,"days":30,"max_bytes":1073741824}`)
	if code != http.StatusBadRequest {
		t.Errorf("status (misspelt instead of real) = %d, want 400; body %s", code, body)
	}
	if n := len(ctl.calls()); n != 0 {
		t.Errorf("the control was applied %d times by refused requests, want 0", n)
	}
}

func TestStrictJSONHistorySettingsKnownFieldsStillWork(t *testing.T) {
	ts, ctl := strictHistoryServer(t)
	code, _, body := rawBody(t, http.DefaultClient, http.MethodPut, ts.URL+historyURLPath,
		`{"enabled":true,"days":30,"maxBytes":1073741824}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", code, body)
	}
	if n := len(ctl.calls()); n != 1 {
		t.Errorf("the control was applied %d times, want 1", n)
	}
}

func TestStrictJSONDeviceCreateRefusesUnknownField(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	code, _, body := rawBody(t, admin, http.MethodPost, ts.URL+"/api/devices",
		`{"name":"hap-ax3","displayName":"Hall AP"}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	if got := s.Devices.List(); len(got) != 0 {
		t.Errorf("a refused create left %d devices, want 0: %+v", len(got), got)
	}
}

func TestStrictJSONDeviceCreateKnownFieldsStillWork(t *testing.T) {
	s, ts, admin := deviceTestServer(t)
	code, _, body := rawBody(t, admin, http.MethodPost, ts.URL+"/api/devices", `{"name":"hap-ax3"}`)
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", code, body)
	}
	found := s.Devices.List()
	if len(found) != 1 {
		t.Errorf("devices = %d, want 1", len(found))
	}
}

func TestStrictJSONPreferencesPatchRefusesUnknownField(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	code, _, body := rawBody(t, client, http.MethodPatch, ts.URL+"/api/me/preferences",
		`{"version":1,"prefs":{"uiTheme":"teal"},"pref":{"uiTheme":"red"}}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	code, _, body = rawBody(t, client, http.MethodGet, ts.URL+"/api/me/preferences", "")
	if code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	if strings.Contains(body, "uiTheme") {
		t.Errorf("a refused PATCH changed the stored preferences: %s", body)
	}
}

// prefs is an open object by design: keys inside it are the frontend's
// and must keep being accepted. Only the document's own fields are strict.
func TestStrictJSONPreferencesPatchKnownFieldsStillWork(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := registerAdmin(t, s, ts)

	code, _, body := rawBody(t, client, http.MethodPatch, ts.URL+"/api/me/preferences",
		`{"version":1,"prefs":{"uiTheme":"teal","anythingTheFrontendInvents":1}}`)
	if code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", code, body)
	}
	_, _, body = rawBody(t, client, http.MethodGet, ts.URL+"/api/me/preferences", "")
	if !strings.Contains(body, "uiTheme") || !strings.Contains(body, "anythingTheFrontendInvents") {
		t.Errorf("stored preferences = %s, want both keys", body)
	}
}

func TestStrictJSONDefinitionCreateRefusesNestedUnknownField(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	before := len(getDefinitions(t, ts).Definitions)

	code, _, body := rawBody(t, http.DefaultClient, http.MethodPost, ts.URL+"/api/definitions",
		`{"name":"SSH watch","expectation":{"ports":[22],"portz":[23]}}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	if after := len(getDefinitions(t, ts).Definitions); after != before {
		t.Errorf("definitions went from %d to %d after a refused create", before, after)
	}
}

func TestStrictJSONDefinitionCreateRefusesTopLevelUnknownField(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	before := len(getDefinitions(t, ts).Definitions)

	code, _, body := rawBody(t, http.DefaultClient, http.MethodPost, ts.URL+"/api/definitions",
		`{"name":"SSH watch","expectation":{"ports":[22]},"familly":"x"}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body %s", code, body)
	}
	if after := len(getDefinitions(t, ts).Definitions); after != before {
		t.Errorf("definitions went from %d to %d after a refused create", before, after)
	}
}

func TestStrictJSONDefinitionCreateKnownFieldsStillWork(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	before := len(getDefinitions(t, ts).Definitions)

	code, _, body := rawBody(t, http.DefaultClient, http.MethodPost, ts.URL+"/api/definitions",
		`{"name":"SSH watch","expectation":{"ports":[22]}}`)
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", code, body)
	}
	if after := len(getDefinitions(t, ts).Definitions); after != before+1 {
		t.Errorf("definitions went from %d to %d, want one more", before, after)
	}
}

// The refusal is the same kind of response the route already gives for a
// body it cannot parse: same status, content type and message.
func TestStrictJSONRefusalMatchesMalformedJSONResponse(t *testing.T) {
	hist, _ := strictHistoryServer(t)
	_, devTS, admin := deviceTestServer(t)
	s := newAuthTestServer(t)
	prefTS := httptest.NewServer(s.Routes())
	defer prefTS.Close()
	prefClient := registerAdmin(t, s, prefTS)

	cases := []struct {
		name, method, url, unknown string
		client                     *http.Client
	}{
		{"history", http.MethodPut, hist.URL + historyURLPath,
			`{"enabled":true,"days":30,"maxBytes":1073741824,"extra":1}`, http.DefaultClient},
		{"device create", http.MethodPost, devTS.URL + "/api/devices",
			`{"name":"hap-ax3","extra":1}`, admin},
		{"preferences", http.MethodPatch, prefTS.URL + "/api/me/preferences",
			`{"version":1,"prefs":{},"extra":1}`, prefClient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantCode, wantType, wantBody := rawBody(t, c.client, c.method, c.url, `{`)
			if wantCode != http.StatusBadRequest {
				t.Fatalf("malformed JSON gave %d, want 400", wantCode)
			}
			gotCode, gotType, gotBody := rawBody(t, c.client, c.method, c.url, c.unknown)
			if gotCode != wantCode || gotType != wantType || gotBody != wantBody {
				t.Errorf("unknown field gave %d %q %q; malformed JSON gives %d %q %q",
					gotCode, gotType, gotBody, wantCode, wantType, wantBody)
			}
		})
	}
}
