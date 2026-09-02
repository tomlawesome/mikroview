// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/settings"
	"github.com/tomlawesome/mikroview/internal/store"
)

// withMemoryControl gives a test server a settings store and a range to
// move within, the way main.go does at boot.
func withMemoryControl(t *testing.T, s *Server, current config.ByteSize, max config.ByteSize) {
	t.Helper()
	set, err := settings.Open(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = set
	s.InitMemory(current, config.MemoryBounds{Min: config.MinMaxMemory, Max: max})
}

func putMaxMemory(t *testing.T, url string, h http.Handler, bytes int64) (*http.Response, string) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	req, err := http.NewRequest(http.MethodPut, ts.URL+url,
		strings.NewReader(`{"maxMemory":`+itoa(bytes)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

func TestStoreSettingsGrowResizesTheRing(t *testing.T) {
	s, st := newTestServer(t)
	withMemoryControl(t, s, 120<<20, 4<<30)

	resp, body := putMaxMemory(t, "/api/settings/store", asAdmin(s.mux()), 480<<20)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}

	var got StoreSettings
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxMemory != 480<<20 {
		t.Errorf("response says maxMemory %d, want %d", got.MaxMemory, int64(480<<20))
	}
	want := config.Store{MaxMemory: 480 << 20}.Capacity()
	if st.Capacity() != want {
		t.Errorf("ring capacity %d, want %d -- the figure was stored but the buffer was not resized",
			st.Capacity(), want)
	}
	// The stored figure is what a restart would read back.
	if stored, ok := s.Settings.MaxMemory(); !ok || stored != 480<<20 {
		t.Errorf("settings store holds (%d, %v), want (%d, true)", stored, ok, int64(480<<20))
	}
}

func TestStoreSettingsShrinkLetsTheOldestGo(t *testing.T) {
	s, st := newTestServer(t)
	// A ring already holding more than the shrink will allow, so the
	// eviction has something to evict -- an assertion about what falls
	// away proves nothing against a buffer that was never full.
	for i := 0; i < 1000; i++ {
		st.Insert(store.Event{RuleLabel: "settings-test"})
	}
	before := st.Stats()
	if before.Count != 1000 {
		t.Fatalf("fixture holds %d events, want 1000", before.Count)
	}
	withMemoryControl(t, s, 120<<20, 4<<30)

	// 32 MiB is the floor, and its capacity (53,760) is far above 1000,
	// so shrink the *ring* directly to something below what is held and
	// then confirm the endpoint's own resize does the same thing at the
	// sizes it deals in. Here: a budget whose capacity is 500 events.
	small := config.ByteSize(500 * config.AssumedBytesPerEvent)
	s.InitMemory(120<<20, config.MemoryBounds{Min: small, Max: 4 << 30})

	resp, body := putMaxMemory(t, "/api/settings/store", asAdmin(s.mux()), int64(small))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}

	after := st.Stats()
	if after.Capacity != 500 {
		t.Errorf("capacity %d, want 500", after.Capacity)
	}
	if after.Count != 500 {
		t.Errorf("holding %d events, want 500 -- the shrink did not let anything go", after.Count)
	}
	// Oldest-first: the survivors are the newest 500 IDs.
	res := st.Query(store.Query{Limit: 1000})
	if len(res.Events) == 0 || res.Events[0].ID != 501 {
		t.Errorf("oldest surviving ID is %v, want 501 -- the wrong end was evicted", res.Events[0].ID)
	}
}

func TestStoreSettingsRefusesOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int64
	}{
		{"below the floor", int64(config.MinMaxMemory) - 1},
		{"above the ceiling", (1 << 30) + 1},
		{"zero", 0},
		{"negative", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st := newTestServer(t)
			withMemoryControl(t, s, 120<<20, 1<<30)
			capacityBefore := st.Capacity()

			resp, body := putMaxMemory(t, "/api/settings/store", asAdmin(s.mux()), tc.bytes)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("PUT %d returned %d, want 400: %s", tc.bytes, resp.StatusCode, body)
			}
			if st.Capacity() != capacityBefore {
				t.Errorf("a refused figure still resized the ring (%d -> %d)", capacityBefore, st.Capacity())
			}
			if _, ok := s.Settings.MaxMemory(); ok {
				t.Error("a refused figure was stored")
			}
		})
	}
}

// The negative half of #796's viewer line, at the handler rather than
// only in the authz matrix: a non-admin's PUT changes nothing.
func TestStoreSettingsRefusesNonAdmins(t *testing.T) {
	for _, tc := range []struct {
		name string
		wrap func(http.Handler) http.Handler
	}{
		{"viewer", asViewer},
		{"user", asUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st := newTestServer(t)
			withMemoryControl(t, s, 120<<20, 4<<30)
			capacityBefore := st.Capacity()

			resp, body := putMaxMemory(t, "/api/settings/store", tc.wrap(s.mux()), 480<<20)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("a %s got %d, want 403: %s", tc.name, resp.StatusCode, body)
			}
			if st.Capacity() != capacityBefore {
				t.Errorf("a %s's refused PUT still resized the ring", tc.name)
			}
			if _, ok := s.Settings.MaxMemory(); ok {
				t.Errorf("a %s's refused PUT still stored a figure", tc.name)
			}
		})
	}
}

// The read half a viewer does get: the bar and the figure ride
// /api/stats, so a viewer can see the trade-off without being able to
// change it.
func TestStatsCarriesTheMemoryFigureForAViewer(t *testing.T) {
	s, _ := newTestServer(t)
	withMemoryControl(t, s, 120<<20, 3584<<20)

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a viewer got %d from /api/stats, want 200", resp.StatusCode)
	}

	var payload struct {
		Memory StoreSettings `json:"memory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Memory.MaxMemory != 120<<20 {
		t.Errorf("maxMemory = %d, want %d", payload.Memory.MaxMemory, int64(120<<20))
	}
	if payload.Memory.Min != int64(config.MinMaxMemory) || payload.Memory.Max != 3584<<20 {
		t.Errorf("bounds = (%d, %d), want (%d, %d)",
			payload.Memory.Min, payload.Memory.Max, int64(config.MinMaxMemory), int64(3584<<20))
	}
	if payload.Memory.BytesPerEvent != config.AssumedBytesPerEvent {
		t.Errorf("bytesPerEvent = %d, want %d", payload.Memory.BytesPerEvent, int64(config.AssumedBytesPerEvent))
	}
	if payload.Memory.Resident <= 0 {
		t.Errorf("resident = %d, want a positive figure -- the trade-off's other half is missing",
			payload.Memory.Resident)
	}
}

// A change has to be answerable for: who set it, from what to what.
func TestStoreSettingsIsAudited(t *testing.T) {
	s, _ := newTestServer(t)
	withMemoryControl(t, s, 120<<20, 4<<30)

	resp, body := putMaxMemory(t, "/api/settings/store", asAdmin(s.mux()), 480<<20)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", resp.StatusCode, body)
	}

	entries := s.Audit.Query(audit.Query{Limit: 100}).Entries
	found := false
	for _, e := range entries {
		if e.Action == "settings.store_max_memory" {
			found = true
			if !strings.Contains(e.Detail, "480.0MiB") {
				t.Errorf("audit detail %q does not say what it was set to", e.Detail)
			}
			if !strings.Contains(e.Detail, "120.0MiB") {
				t.Errorf("audit detail %q does not say what it was set from", e.Detail)
			}
			if e.Target != "store.maxMemory" {
				t.Errorf("audit target %q, want store.maxMemory", e.Target)
			}
		}
	}
	if !found {
		t.Errorf("no settings.store_max_memory audit entry among %d rows", len(entries))
	}
}

// A Server with no settings store refuses rather than pretending: the
// buffer would resize and the figure would vanish at the next restart.
func TestStoreSettingsRefusesWithoutASettingsStore(t *testing.T) {
	s, st := newTestServer(t)
	s.InitMemory(120<<20, config.MemoryBounds{Min: config.MinMaxMemory, Max: 4 << 30})
	capacityBefore := st.Capacity()

	resp, body := putMaxMemory(t, "/api/settings/store", asAdmin(s.mux()), 480<<20)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT returned %d, want 503: %s", resp.StatusCode, body)
	}
	if st.Capacity() != capacityBefore {
		t.Error("the ring was resized even though the figure could not be stored")
	}
}
