package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

func newTestServer() (*Server, *store.Store) {
	st := store.New(1000, time.Hour)
	fs, _ := flags.Open("")
	authStore, _ := auth.Open("") // unpersisted, zero users -- auth inactive, matches every existing test's assumption of a fully open API
	s := &Server{
		Store:            st,
		Devices:          device.NewRegistry([]config.Device{{ID: "core", Name: "Core", SourceIP: "192.168.1.1"}}),
		Hub:              hub.New(),
		Reputation:       reputation.New(""),
		Flags:            fs,
		DetectorSettings: detect.AllEnabledSettingsStore(),
		Auth:             authStore,
		Sessions:         auth.NewSessionStore(time.Hour),
		LoginLimiter:     auth.NewLoginLimiter(10, time.Minute),
		StartTime:        time.Now(),
	}
	return s, st
}

func TestHandleHealthz(t *testing.T) {
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
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
}

func TestHandleEventsFiltering(t *testing.T) {
	s, st := newTestServer()
	ts := httptest.NewServer(s.Routes())
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
	s, st := newTestServer()
	ts := httptest.NewServer(s.Routes())
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

func TestHandleDevices(t *testing.T) {
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
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

func TestHandleStats(t *testing.T) {
	s, st := newTestServer()
	ts := httptest.NewServer(s.Routes())
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
