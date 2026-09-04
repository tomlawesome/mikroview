// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// fakeHistoryControl stands in for main's historyRuntime.
//
// A stand-in rather than the real thing because what these tests are
// about is the endpoint: who may call it, what it refuses, and what it
// hands the control. The real lifecycle -- opening, backfilling,
// purging -- is tested against real encrypted files in the root
// package's history_runtime_test.go, which is where it belongs.
type fakeHistoryControl struct {
	mu      sync.Mutex
	state   HistorySettings
	applied []appliedHistory
	err     error
}

type appliedHistory struct {
	enabled  bool
	days     int
	maxBytes int64
}

func (f *fakeHistoryControl) HistorySettings() HistorySettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeHistoryControl) ApplyHistory(enabled bool, days int, maxBytes int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.applied = append(f.applied, appliedHistory{enabled, days, maxBytes})
	f.state.Enabled = enabled
	f.state.Days = days
	f.state.MaxBytes = maxBytes
	if !enabled {
		// Off purges, so nothing is held afterwards.
		f.state.Held = nil
		f.state.Capped = false
	}
	return nil
}

func (f *fakeHistoryControl) calls() []appliedHistory {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]appliedHistory(nil), f.applied...)
}

// keyedHistory is the state of an instance that has been retaining for
// a while: a key mounted, the switch on, and 27 of an allowed 30 days
// actually on disk.
func keyedHistory() *fakeHistoryControl {
	return &fakeHistoryControl{state: HistorySettings{
		Keyed:    true,
		Enabled:  true,
		Days:     30,
		MaxBytes: 1 << 30,
		Held: &HistoryHeld{
			Days:   27,
			Oldest: "2026-08-07",
			Newest: "2026-09-02",
			Bytes:  851443712,
		},
		BytesPerDay: 31457280,
	}}
}

func historyRequest(t *testing.T, method, body string, h http.Handler) (*http.Response, string) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+"/api/settings/history", reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(raw)
}

// The wire shape, byte for byte. Pinned rather than field-by-field
// because a control is being drawn against it in another repo-half: a
// renamed or reordered key is a broken screen, and the diff should say
// so here rather than in a browser.
func TestHistorySettingsWireShape(t *testing.T) {
	s, _ := newTestServer(t)
	s.HistoryControl = keyedHistory()

	resp, body := historyRequest(t, http.MethodGet, "", asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET returned %d: %s", resp.StatusCode, body)
	}
	want := `{"keyed":true,"enabled":true,"days":30,"maxBytes":1073741824,` +
		`"held":{"days":27,"oldest":"2026-08-07","newest":"2026-09-02","bytes":851443712},` +
		`"capped":false,"bytesPerDay":31457280}` + "\n"
	if body != want {
		t.Errorf("GET /api/settings/history returned\n  %s\nwant\n  %s", body, want)
	}
}

// Nothing on disk is null, not a zero-filled object: an empty window is
// a different fact from a window of no days, and a screen that renders
// "0 days · since —" has stated something about the network that is not
// true.
func TestHistorySettingsReportsNothingHeldAsNull(t *testing.T) {
	s, _ := newTestServer(t)
	s.HistoryControl = &fakeHistoryControl{state: HistorySettings{
		Keyed: true, Enabled: false, Days: 30, MaxBytes: 1 << 30,
	}}

	resp, body := historyRequest(t, http.MethodGet, "", asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET returned %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"held":null`) {
		t.Errorf("with nothing on disk the response was %s, want held null", body)
	}
}

func TestHistorySettingsTurnsItOn(t *testing.T) {
	s, _ := newTestServer(t)
	ctl := &fakeHistoryControl{state: HistorySettings{Keyed: true, Days: 30, MaxBytes: 1 << 30}}
	s.HistoryControl = ctl

	resp, body := historyRequest(t, http.MethodPut,
		`{"enabled":true,"days":30,"maxBytes":1073741824}`, asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}
	var got HistorySettings
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Error("the response says the history is still off after turning it on")
	}
	calls := ctl.calls()
	if len(calls) != 1 || calls[0] != (appliedHistory{true, 30, 1 << 30}) {
		t.Errorf("the control was asked for %+v, want one call of {true 30 1073741824}", calls)
	}
}

// Off is the destructive half: the purge has to have happened before
// the response is written, not been scheduled behind it.
func TestHistorySettingsTurnsItOffAndReportsNothingHeld(t *testing.T) {
	s, _ := newTestServer(t)
	ctl := keyedHistory()
	s.HistoryControl = ctl

	resp, body := historyRequest(t, http.MethodPut,
		`{"enabled":false,"days":30,"maxBytes":1073741824}`, asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}
	var got HistorySettings
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("the response says the history is still on after turning it off")
	}
	if got.Held != nil {
		t.Errorf("the response still reports %+v held after turning it off -- off has to mean the events are gone", *got.Held)
	}
	calls := ctl.calls()
	if len(calls) != 1 || calls[0].enabled {
		t.Errorf("the control was asked for %+v, want one call turning it off", calls)
	}
}

func TestHistorySettingsShrinksTheDayCount(t *testing.T) {
	s, _ := newTestServer(t)
	ctl := keyedHistory()
	s.HistoryControl = ctl

	resp, body := historyRequest(t, http.MethodPut,
		`{"enabled":true,"days":7,"maxBytes":1073741824}`, asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}
	calls := ctl.calls()
	if len(calls) != 1 || calls[0].days != 7 {
		t.Errorf("the control was asked for %+v, want one call at 7 days", calls)
	}
}

// No key mounted means the feature cannot run at all -- there is no
// unencrypted mode -- so this is refused with a sentence saying what to
// go and do, not a bare status.
func TestHistorySettingsRefusesWithoutAKey(t *testing.T) {
	s, _ := newTestServer(t)
	ctl := &fakeHistoryControl{state: HistorySettings{Keyed: false, Days: 30, MaxBytes: 1 << 30}}
	s.HistoryControl = ctl

	resp, body := historyRequest(t, http.MethodPut,
		`{"enabled":true,"days":30,"maxBytes":1073741824}`, asAdmin(s.mux()))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT returned %d, want 409: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "key file") {
		t.Errorf("the refusal reads %q, which does not tell the operator a key file is what is missing", strings.TrimSpace(body))
	}
	if len(ctl.calls()) != 0 {
		t.Error("a refused change still reached the control")
	}
}

// A keyless instance reports the switch off whatever is stored, because
// nothing is being written.
func TestHistorySettingsWithoutAKeyReportsOff(t *testing.T) {
	s, _ := newTestServer(t)
	s.HistoryControl = &fakeHistoryControl{state: HistorySettings{Keyed: false, Days: 30, MaxBytes: 1 << 30}}

	resp, body := historyRequest(t, http.MethodGet, "", asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET returned %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"keyed":false`) || !strings.Contains(body, `"enabled":false`) {
		t.Errorf("a keyless instance reported %s, want keyed false and enabled false", body)
	}
}

func TestHistorySettingsRefusesBadCaps(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"zero days", `{"enabled":true,"days":0,"maxBytes":1073741824}`},
		{"negative days", `{"enabled":true,"days":-1,"maxBytes":1073741824}`},
		{"a cap below a megabyte", `{"enabled":true,"days":30,"maxBytes":1024}`},
		{"a zero cap", `{"enabled":true,"days":30,"maxBytes":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			ctl := keyedHistory()
			s.HistoryControl = ctl

			resp, body := historyRequest(t, http.MethodPut, tc.body, asAdmin(s.mux()))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("PUT %s returned %d, want 400: %s", tc.body, resp.StatusCode, body)
			}
			if strings.TrimSpace(body) == "" {
				t.Error("a refusal with no sentence in it leaves the operator nothing to act on")
			}
			if len(ctl.calls()) != 0 {
				t.Error("a refused change still reached the control")
			}
		})
	}
}

// The same tier the memory slider uses, for a stronger version of the
// same reason: this one deletes retained evidence.
func TestHistorySettingsRefusesNonAdmins(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(http.Handler) http.Handler
	}{
		{"viewer", asViewer},
		{"user", asUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			ctl := keyedHistory()
			s.HistoryControl = ctl

			resp, body := historyRequest(t, http.MethodPut,
				`{"enabled":false,"days":30,"maxBytes":1073741824}`, tc.wrap(s.mux()))
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("a %s's PUT got %d, want 403: %s", tc.name, resp.StatusCode, body)
			}
			if len(ctl.calls()) != 0 {
				t.Errorf("a %s's refused PUT still reached the control", tc.name)
			}

			resp, body = historyRequest(t, http.MethodGet, "", tc.wrap(s.mux()))
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("a %s's GET got %d, want 403: %s", tc.name, resp.StatusCode, body)
			}
		})
	}
}

// An instance with no control refuses rather than pretending: the
// alternative is a screen reporting a history nobody is keeping.
func TestHistorySettingsWithoutAControl(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		s, _ := newTestServer(t)
		resp, body := historyRequest(t, method, `{"enabled":true,"days":30,"maxBytes":1073741824}`, asAdmin(s.mux()))
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s returned %d, want 503: %s", method, resp.StatusCode, body)
		}
	}
}

// A change that deletes a month of retained evidence has to be
// answerable for: who did it, from what to what.
func TestHistorySettingsIsAudited(t *testing.T) {
	s, _ := newTestServer(t)
	s.HistoryControl = keyedHistory()

	resp, body := historyRequest(t, http.MethodPut,
		`{"enabled":false,"days":30,"maxBytes":1073741824}`, asAdmin(s.mux()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}

	found := false
	for _, e := range s.Audit.Query(audit.Query{Limit: 100}).Entries {
		if e.Action != "settings.history" {
			continue
		}
		found = true
		if e.Target != "history" {
			t.Errorf("audit target %q, want history", e.Target)
		}
		if !strings.Contains(e.Detail, "on, 30 days") || !strings.Contains(e.Detail, "off") {
			t.Errorf("audit detail %q does not say what it went from and to", e.Detail)
		}
	}
	if !found {
		t.Error("no settings.history audit entry was written")
	}
}
