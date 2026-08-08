// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/store"
)

// newTestServer's Auth defaults to the "disabled" state (see
// auth.Store.Disable) -- zero users, but a deliberate, permanent
// opt-out, not the tightened "undecided" bootstrap state (see
// requireAuth). Callers mount s.mux() rather than s.Routes(), which is
// what "a fully open API" means now that authentication cannot be turned
// off -- auth_test.go and the authzMatrix guard mount Routes and cover
// the gate itself.
func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st := store.New(1000, time.Hour)
	fs, _ := flags.Open("")
	es, _ := entities.Open("")
	authStore, err := auth.Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	tokenStore, err := auth.OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	ru, _ := rules.Open("")
	as, _ := audit.Open("")
	s := &Server{
		Store:            st,
		Devices:          device.NewRegistry([]config.Device{{ID: "core", Name: "Core", SourceIP: "192.168.1.1"}}),
		Hub:              hub.New(),
		Reputation:       reputation.New(""),
		Flags:            fs,
		DetectorSettings: detect.AllEnabledSettingsStore(),
		Entities:         es,
		Rules:            ru,
		Audit:            as,
		Auth:             authStore,
		Sessions:         auth.NewSessionStore(time.Hour),
		LoginLimiter:     auth.NewLoginLimiter(10, time.Minute),
		Tokens:           tokenStore,
		IngestLimiter:    auth.NewLoginLimiter(ingestLimiterThreshold, ingestLimiterWindow),
		StartTime:        time.Now(),
		Version:          "test-version",
	}
	return s, st
}

func TestHandleHealthz(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %v, want ok", body["status"])
	}
	if body["version"] != "test-version" {
		t.Errorf("version field = %v, want test-version", body["version"])
	}
}

func TestHandleEventsFiltering(t *testing.T) {
	s, st := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	st.Insert(store.Event{Time: now, DeviceID: "core", Action: store.ActionAccept, Protocol: "TCP", SrcIP: "10.0.0.1", DstIP: "8.8.8.8", DstPort: 443})
	st.Insert(store.Event{Time: now, DeviceID: "core", Action: store.ActionDrop, Protocol: "UDP", SrcIP: "10.0.0.2", DstIP: "1.1.1.1", DstPort: 53})

	resp, err := http.Get(ts.URL + "/api/events?action=drop")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var res store.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].Action != store.ActionDrop {
		t.Errorf("unexpected result: %+v", res.Events)
	}
}

func TestHandleEventsScopeFiltering(t *testing.T) {
	s, st := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	st.Insert(store.Event{Time: now, DeviceID: "core", Action: store.ActionAccept, SrcIP: "10.0.0.1", DstIP: "8.8.8.8"})
	st.Insert(store.Event{Time: now, DeviceID: "core", Action: store.ActionDrop, SrcIP: "203.0.113.9", DstIP: "10.0.0.5"})

	resp, err := http.Get(ts.URL + "/api/events?srcScope=internal")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var res store.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].Action != store.ActionAccept {
		t.Errorf("expected only the event with a private SrcIP, got %+v", res.Events)
	}

	// a malformed scope value falls back to "any" rather than erroring or
	// matching nothing
	resp2, err := http.Get(ts.URL + "/api/events?srcScope=not-a-real-scope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var res2 store.Result
	if err := json.NewDecoder(resp2.Body).Decode(&res2); err != nil {
		t.Fatal(err)
	}
	if len(res2.Events) != 2 {
		t.Errorf("expected a malformed scope value to be ignored (both events returned), got %+v", res2.Events)
	}
}

func TestHandleEventsUntilFiltering(t *testing.T) {
	s, st := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	old := st.Insert(store.Event{Time: now.Add(-time.Minute), ReceivedAt: now.Add(-time.Minute), DeviceID: "core", Action: store.ActionAccept, SrcIP: "10.0.0.1", DstIP: "8.8.8.8"})
	st.Insert(store.Event{Time: now, ReceivedAt: now, DeviceID: "core", Action: store.ActionAccept, SrcIP: "10.0.0.1", DstIP: "8.8.8.8"})

	resp, err := http.Get(ts.URL + "/api/events?until=" + url.QueryEscape(old.ReceivedAt.Add(30*time.Second).Format(time.RFC3339)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var res store.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].ID != old.ID {
		t.Errorf("expected only the event before Until, got %+v", res.Events)
	}
}

// TestHandleEventsAroundWindow covers issue #29's bounded before/after
// lookback: given a center timestamp and window, the endpoint should
// return only events within that window, matching the source IP.
func TestHandleEventsAroundWindow(t *testing.T) {
	s, st := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	now := time.Now()
	tooEarly := now.Add(-time.Hour)
	center := now
	tooLate := now.Add(time.Hour)
	st.Insert(store.Event{Time: tooEarly, ReceivedAt: tooEarly, DeviceID: "core", Action: store.ActionAccept, SrcIP: "203.0.113.9", DstIP: "8.8.8.8"})
	inWindow := st.Insert(store.Event{Time: center, ReceivedAt: center, DeviceID: "core", Action: store.ActionAccept, SrcIP: "203.0.113.9", DstIP: "8.8.8.8"})
	st.Insert(store.Event{Time: tooLate, ReceivedAt: tooLate, DeviceID: "core", Action: store.ActionAccept, SrcIP: "203.0.113.9", DstIP: "8.8.8.8"})

	reqURL := ts.URL + "/api/events?ip=203.0.113.9&around=" + url.QueryEscape(center.Format(time.RFC3339)) + "&window=5m"
	resp, err := http.Get(reqURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var res store.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].ID != inWindow.ID {
		t.Errorf("expected only the event within the 5m window around the center timestamp, got %+v", res.Events)
	}
}

func TestHandleDevices(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Devices []device.Info `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Devices) != 1 || body.Devices[0].ID != "core" {
		t.Errorf("unexpected devices: %+v", body.Devices)
	}
}

// TestHandleDevicesReportsStatus covers the issue #98 fleet-health status
// field end to end through the real HTTP handler: a device that's never
// sent anything is "never_seen", one that's sent something recently is
// "live", and one whose last event is older than DeviceStaleAfter is
// "stale".
func TestHandleDevicesReportsStatus(t *testing.T) {
	s, _ := newTestServer(t)
	s.DeviceStaleAfter = 10 * time.Minute

	// newTestServer's "core" device is configured but has never had
	// Resolve called for it -- exactly the "never_seen" case.
	s.Devices.Resolve("203.0.113.9", time.Now()) // an auto-discovered, currently-live device
	s.Devices.Resolve("198.51.100.1", time.Now().Add(-30*time.Minute))
	s.Devices.Resolve("198.51.100.1", time.Now().Add(-30*time.Minute)) // same source, stays stale either way

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Devices []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	byID := map[string]string{}
	for _, d := range body.Devices {
		byID[d.ID] = d.Status
	}
	if byID["core"] != "never_seen" {
		t.Errorf("expected core's status = never_seen (configured, zero events), got %q", byID["core"])
	}
	if byID["203.0.113.9"] != "live" {
		t.Errorf("expected 203.0.113.9's status = live (just resolved), got %q", byID["203.0.113.9"])
	}
	if byID["198.51.100.1"] != "stale" {
		t.Errorf("expected 198.51.100.1's status = stale (last seen 30m ago, threshold 10m), got %q", byID["198.51.100.1"])
	}
}

// TestDeviceStatus is a direct, table-driven unit test of deviceStatus's
// three-way classification -- the HTTP-level test above already covers
// it end to end, this pins the exact boundary/zero-threshold behavior
// deviceStatus's own doc comment promises.
func TestDeviceStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		info       device.Info
		staleAfter time.Duration
		want       string
	}{
		{
			name:       "never seen regardless of threshold",
			info:       device.Info{LastSeen: time.Time{}},
			staleAfter: 10 * time.Minute,
			want:       "never_seen",
		},
		{
			name:       "well within threshold",
			info:       device.Info{LastSeen: now.Add(-time.Minute)},
			staleAfter: 10 * time.Minute,
			want:       "live",
		},
		{
			name:       "exactly at threshold counts as stale",
			info:       device.Info{LastSeen: now.Add(-10 * time.Minute)},
			staleAfter: 10 * time.Minute,
			want:       "stale",
		},
		{
			name:       "one second under threshold is still live",
			info:       device.Info{LastSeen: now.Add(-10*time.Minute + time.Second)},
			staleAfter: 10 * time.Minute,
			want:       "live",
		},
		{
			name:       "well past threshold",
			info:       device.Info{LastSeen: now.Add(-time.Hour)},
			staleAfter: 10 * time.Minute,
			want:       "stale",
		},
		{
			name:       "zero threshold disables staleness entirely",
			info:       device.Info{LastSeen: now.Add(-30 * 24 * time.Hour)},
			staleAfter: 0,
			want:       "live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deviceStatus(tt.info, tt.staleAfter, now)
			if got != tt.want {
				t.Errorf("deviceStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHandleRules covers issue #109's "discovered but unnamed rules"
// source: GET /api/rules must serve every rule label internal/rules.Store
// has ever seen fire (via Touch), not just what's currently loaded --
// mirroring TestHandleDevices' shape for the analogous device endpoint.
func TestHandleRules(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Now()
	s.Rules.Touch("r13", now)
	s.Rules.Touch("r13", now)
	s.Rules.Touch("r99", now.Add(-time.Hour))

	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/rules")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Rules []rules.Usage `json:"rules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %+v", len(body.Rules), body.Rules)
	}
	byRule := map[string]rules.Usage{}
	for _, u := range body.Rules {
		byRule[u.Rule] = u
	}
	if byRule["r13"].Count != 2 {
		t.Errorf("expected r13's count = 2, got %d", byRule["r13"].Count)
	}
	if byRule["r99"].Count != 1 {
		t.Errorf("expected r99's count = 1, got %d", byRule["r99"].Count)
	}
}

func TestHandleCriticalPorts(t *testing.T) {
	s, _ := newTestServer(t)
	s.CriticalPorts = []int{22, 3389, 8291}
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/critical-ports")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Ports []int `json:"ports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Ports) != 3 || body.Ports[0] != 22 {
		t.Errorf("unexpected ports: %+v", body.Ports)
	}
}

func TestHandleStats(t *testing.T) {
	s, st := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	st.Insert(store.Event{Time: time.Now(), Action: store.ActionAccept})

	resp, err := http.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["total"].(float64) != 1 {
		t.Errorf("total = %v, want 1", body["total"])
	}
}

// asAdmin wraps the ungated mux with a stand-in admin identity -- what a
// handler sees once requireAuth has already done its job.
//
// The admin-gated handler tests used to pass because callerIsAdminOrOpen
// treated an anonymous caller as an admin while zero accounts existed.
// That bypass is gone (#164), so the identity has to come from somewhere.
// Injecting it keeps these tests about the handler; the gate itself is
// covered by auth_test.go and the authzMatrix guard, which both mount
// Routes and log in for real. Repeating a full login here would test the
// middleware nine more times and the handler once.
func asAdmin(h http.Handler) http.Handler {
	admin := &auth.User{ID: "test-admin", Username: "admin", Role: auth.RoleAdmin}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, admin)))
	})
}
