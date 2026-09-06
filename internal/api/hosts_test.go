// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/hosts"
)

func TestHandleHostsList(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "printer", now)
	s.Hosts.Observe("bridge-lan", "10.0.10.9", "", now)

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- reading the register is a viewer-tier read, same as reading coverage declarations", resp.StatusCode)
	}

	var body struct {
		Hosts []hosts.Host `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hosts) != 2 {
		t.Fatalf("hosts = %+v, want both registered hosts", body.Hosts)
	}
	if body.Hosts[0].Key != "bridge-lan|10.0.10.5" || body.Hosts[0].Label != "printer" {
		t.Errorf("first host = %+v, want the key and last-seen label as the register holds them", body.Hosts[0])
	}
	if !body.Hosts[0].LastSeen.Equal(now) {
		t.Errorf("lastSeen = %v, want %v -- the whole point is that the view can tell how long a host has been quiet", body.Hosts[0].LastSeen, now)
	}
	if body.Hosts[0].Mark != nil {
		t.Errorf("mark = %+v, want none on a host nobody has said anything about", body.Hosts[0].Mark)
	}
}

func TestHandleHostMarkPutRecordsAndAudits(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "nas", time.Now())

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	resp := putHostMark(t, ts.URL, "bridge-lan|10.0.10.5", `{"kind":"intended","reason":"the lab NAS, powered on twice a month"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var h hosts.Host
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.Mark == nil || h.Mark.Kind != hosts.MarkIntended {
		t.Fatalf("mark = %+v, want intended", h.Mark)
	}
	if h.Mark.Reason != "the lab NAS, powered on twice a month" {
		t.Errorf("reason = %q, want what the request sent", h.Mark.Reason)
	}
	if h.Mark.By != "user" {
		t.Errorf("by = %q, want the session's own username -- the body must never be able to sign for somebody else", h.Mark.By)
	}
	if h.Mark.At.IsZero() {
		t.Error("at is zero, want it stamped server-side")
	}

	stored, ok := s.Hosts.Get("bridge-lan|10.0.10.5")
	if !ok || stored.Mark == nil {
		t.Fatalf("the register holds %+v, want the mark stored", stored)
	}

	assertAudit(t, s, "hosts.mark", "bridge-lan|10.0.10.5", "user", "intended")
}

func TestHandleHostMarkPutDismiss(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", time.Now())

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	// A dismissal carries no reason: "take this off my map" is
	// actionable without a justification.
	resp := putHostMark(t, ts.URL, "bridge-lan|10.0.10.5", `{"kind":"dismissed"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a dismissal with no reason", resp.StatusCode)
	}
	h, _ := s.Hosts.Get("bridge-lan|10.0.10.5")
	if h.Mark == nil || h.Mark.Kind != hosts.MarkDismissed {
		t.Errorf("mark = %+v, want dismissed", h.Mark)
	}
}

func TestHandleHostMarkPutRejectsBadRequests(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	key := "bridge-lan|10.0.10.5"

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	cases := []struct {
		name string
		key  string
		body string
		want int
	}{
		{"unknown kind", key, `{"kind":"hidden","reason":"x"}`, http.StatusBadRequest},
		{"missing kind", key, `{"reason":"x"}`, http.StatusBadRequest},
		{"intended with no reason", key, `{"kind":"intended"}`, http.StatusBadRequest},
		{"over-long reason", key, `{"kind":"intended","reason":"` + strings.Repeat("r", 401) + `"}`, http.StatusBadRequest},
		{"bidi override in reason", key, `{"kind":"intended","reason":"safe\u202egnorw"}`, http.StatusBadRequest},
		{"malformed body", key, `not json`, http.StatusBadRequest},
		{"control character in key", "bridge-lan|10.0.10.5\n", `{"kind":"dismissed"}`, http.StatusBadRequest},
		{"unknown host", "bridge-lan|10.0.10.99", `{"kind":"dismissed"}`, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := putHostMark(t, ts.URL, c.key, c.body)
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}

	if h, _ := s.Hosts.Get(key); h.Mark != nil {
		t.Errorf("mark = %+v, want nothing stored by any rejected request", h.Mark)
	}
	if n := len(s.Audit.Query(audit.Query{}).Entries); n != 0 {
		t.Errorf("%d audit entries, want none -- a refused mark is not an action taken", n)
	}
}

func TestHandleHostMarkRefusesBelowUserTier(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	if _, err := s.Hosts.Mark("bridge-lan|10.0.10.5", hosts.MarkIntended, "on purpose", "admin"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asViewer(s.mux()))
	defer ts.Close()

	resp := putHostMark(t, ts.URL, "bridge-lan|10.0.10.5", `{"kind":"dismissed"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("PUT status = %d, want 403 for a viewer -- authoring a statement about a silence is the user tier", resp.StatusCode)
	}

	del := deleteHostMark(t, ts.URL, "bridge-lan|10.0.10.5")
	del.Body.Close()
	if del.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE status = %d, want 403 for a viewer", del.StatusCode)
	}

	h, _ := s.Hosts.Get("bridge-lan|10.0.10.5")
	if h.Mark == nil || h.Mark.Kind != hosts.MarkIntended {
		t.Errorf("mark = %+v, want the admin's mark untouched by a refused call", h.Mark)
	}
}

func TestHandleHostMarkDelete(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	if _, err := s.Hosts.Mark("bridge-lan|10.0.10.5", hosts.MarkIntended, "on purpose", "admin"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	resp := deleteHostMark(t, ts.URL, "bridge-lan|10.0.10.5")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	h, _ := s.Hosts.Get("bridge-lan|10.0.10.5")
	if h.Mark != nil {
		t.Errorf("mark = %+v, want it gone", h.Mark)
	}

	assertAudit(t, s, "hosts.unmark", "bridge-lan|10.0.10.5", "user", "")
}

func TestHandleHostMarkDeleteUnmarkedIs404(t *testing.T) {
	s, _ := newTestServer(t)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", time.Now())

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	// An unmarked host and an unknown key both mean the caller is
	// working from a stale list, which is worth saying.
	for _, key := range []string{"bridge-lan|10.0.10.5", "bridge-lan|10.0.10.99"} {
		resp := deleteHostMark(t, ts.URL, key)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("DELETE %s status = %d, want 404", key, resp.StatusCode)
		}
	}
	if n := len(s.Audit.Query(audit.Query{}).Entries); n != 0 {
		t.Errorf("%d audit entries, want none -- nothing was removed", n)
	}
}

// TestHostMarkEndpointsRoundTripThroughTheRegister is the slice's own
// end-to-end shape: mark, read it back off the list, then prove a
// further event clears a dismissal (issue #1016's "if it reappears in
// the feed it comes back by itself").
func TestHostMarkEndpointsRoundTripThroughTheRegister(t *testing.T) {
	s, _ := newTestServer(t)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", now)

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	resp := putHostMark(t, ts.URL, "bridge-lan|10.0.10.5", `{"kind":"dismissed","reason":"decommissioned"}`)
	resp.Body.Close()

	listed := listHosts(t, ts.URL)
	if len(listed) != 1 || listed[0].Mark == nil || listed[0].Mark.Kind != hosts.MarkDismissed {
		t.Fatalf("list = %+v, want the dismissal readable straight back off GET /api/hosts", listed)
	}

	s.Hosts.Observe("bridge-lan", "10.0.10.5", "", now.Add(time.Hour))

	listed = listHosts(t, ts.URL)
	if len(listed) != 1 || listed[0].Mark != nil {
		t.Errorf("list = %+v, want the dismissal cleared by the host speaking again", listed)
	}
}

func listHosts(t *testing.T, base string) []hosts.Host {
	t.Helper()
	resp, err := http.Get(base + "/api/hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Hosts []hosts.Host `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Hosts
}

// putHostMark/deleteHostMark issue the two writes with a raw JSON body,
// mirroring putCoverage in coverage_test.go (no CSRF header needed --
// that check lives in requireAuth, which s.mux() deliberately bypasses;
// see newTestServer's own doc comment).
func putHostMark(t *testing.T, base, key, jsonBody string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/api/hosts/"+url.PathEscape(key)+"/mark", bytes.NewReader([]byte(jsonBody)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func deleteHostMark(t *testing.T, base, key string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, base+"/api/hosts/"+url.PathEscape(key)+"/mark", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// assertAudit fails unless exactly the expected accountability entry is
// on the log -- the same shape setup_test.go's own audit assertion
// uses.
func assertAudit(t *testing.T, s *Server, action, target, actor, detailContains string) {
	t.Helper()
	entries := s.Audit.Query(audit.Query{}).Entries
	for _, e := range entries {
		if e.Action != action || e.Target != target || e.Actor != actor {
			continue
		}
		if detailContains != "" && !strings.Contains(e.Detail, detailContains) {
			continue
		}
		return
	}
	t.Errorf("no %s audit entry for %q by %q in %+v -- a decision about a silence is an authored action and must be on the record",
		action, target, actor, entries)
}
