// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros/export"
)

// smallExport is a minimal but realistic RouterOS 7.24.1
// `/export hide-sensitive` used by most tests below: three filter
// rules -- one forward rule crossing bridge1->ether1, its reverse
// (which must NOT be read as crossing the same boundary), and one
// non-forward rule that never crosses anything regardless of
// interfaces.
const smallExport = `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/ip firewall filter
add action=accept chain=forward comment="lan to wan" in-interface=bridge1 out-interface=ether1
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
add action=accept chain=input comment="allow established"
`

func postTuneLogging(t *testing.T, base, path string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// setDeviceFirstSeen makes newTestServer's pre-configured "core" device
// (source IP 192.168.1.1) report FirstSeen as ago before now -- the
// source deviceObservedSince reads (#435 contract: the registry's
// FirstSeen, since routerstate keeps no earlier timestamp).
func setDeviceFirstSeen(s *Server, ago time.Duration) {
	s.Devices.Resolve("192.168.1.1", time.Now().Add(-ago))
}

// TestTuneLoggingAnalyseUnder24HoursReturnsNoRules covers #435 decision
// 5: before a full day of observation, analyse says how long mikroview
// has been watching and derives nothing -- ready:false, rules:[].
func TestTuneLoggingAnalyseUnder24HoursReturnsNoRules(t *testing.T) {
	s, _ := newTestServer(t)
	setDeviceFirstSeen(s, 3*time.Hour)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	body, _ := json.Marshal(tuneLoggingAnalyseRequest{
		Device: "core", Export: smallExport, DarkBoundaries: []string{"bridge1|ether1"},
	})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/analyse", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out tuneLoggingAnalyseResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Ready {
		t.Error("Ready = true, want false at 3 hours observed")
	}
	if len(out.Rules) != 0 {
		t.Errorf("Rules has %d entries, want 0 (nothing derived before 24h)", len(out.Rules))
	}
	if out.Observing.Hours != 3 {
		t.Errorf("Observing.Hours = %d, want 3", out.Observing.Hours)
	}
	if out.Observing.Since == "" {
		t.Error("Observing.Since is empty, want the device's FirstSeen")
	}
}

// TestTuneLoggingAnalyseCrossesDarkAndCounters is the ready path's main
// case: which rules cross the caller's dark boundary, and how their
// packet/byte counters are (or are not) matched to the latest push.
func TestTuneLoggingAnalyseCrossesDarkAndCounters(t *testing.T) {
	s, _ := newTestServer(t)
	setDeviceFirstSeen(s, 25*time.Hour)

	// Ordinal 0 matches rule 0 exactly (chain+action agree) -> counters
	// known. Ordinal 1's pushed action ("accept") disagrees with rule
	// 1's real action ("drop") -> counters must NOT be attributed.
	// Ordinal 2 (rule 2, chain=input) has no matching push at all.
	push := `{"kind":"filter-rule","page":1,"pages":1,"records":[` +
		`{"ordinal":0,"chain":"forward","action":"accept","packets":100,"bytes":2000},` +
		`{"ordinal":1,"chain":"forward","action":"accept","packets":50,"bytes":900}` +
		`]}`
	pushPayload, err := ingest.DecodePayload(strings.NewReader(push))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RouterState.Apply("core", pushPayload, time.Now()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	body, _ := json.Marshal(tuneLoggingAnalyseRequest{
		Device: "core", Export: smallExport, DarkBoundaries: []string{"bridge1|ether1"},
	})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/analyse", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out tuneLoggingAnalyseResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Ready {
		t.Fatal("Ready = false, want true at 25 hours observed")
	}
	if len(out.Rules) != 3 {
		t.Fatalf("Rules has %d entries, want 3: %+v", len(out.Rules), out.Rules)
	}

	r0, r1, r2 := out.Rules[0], out.Rules[1], out.Rules[2]

	if r0.Boundary != "bridge1|ether1" || !r0.CrossesDark {
		t.Errorf("rule 0: boundary=%q crossesDark=%v, want bridge1|ether1 / true", r0.Boundary, r0.CrossesDark)
	}
	if !r0.CountersKnown || r0.Packets != 100 || r0.Bytes != 2000 {
		t.Errorf("rule 0 counters = known=%v packets=%d bytes=%d, want known=true 100/2000", r0.CountersKnown, r0.Packets, r0.Bytes)
	}

	if r1.Boundary != "ether1|bridge1" || r1.CrossesDark {
		t.Errorf("rule 1: boundary=%q crossesDark=%v, want ether1|bridge1 / false (reverse direction is not the dark one)", r1.Boundary, r1.CrossesDark)
	}
	if r1.CountersKnown {
		t.Errorf("rule 1 counters known = true, want false (pushed action disagreed with the rule's own action)")
	}

	if r2.Boundary != "" || r2.CrossesDark {
		t.Errorf("rule 2 (chain=input): boundary=%q crossesDark=%v, want empty/false regardless of interfaces", r2.Boundary, r2.CrossesDark)
	}
	if r2.CountersKnown {
		t.Error("rule 2 counters known = true, want false (no push at that ordinal)")
	}
}

// TestTuneLoggingAnalyseWildcardBoundaryMatching pins the matching
// choice crossesDarkBoundary documents: a rule that names no interface
// on one side is read as crossing every dark boundary that shares its
// named side, not just an exact string match on the whole pair.
func TestTuneLoggingAnalyseWildcardBoundaryMatching(t *testing.T) {
	s, _ := newTestServer(t)
	setDeviceFirstSeen(s, 48*time.Hour)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	text := "# 2026/09/01 10:00:00 by RouterOS 7.24.1\n" +
		"/ip firewall filter\n" +
		`add action=drop chain=forward comment="any in, to ether1" out-interface=ether1` + "\n" +
		`add action=drop chain=forward comment="from bridge1, any out" in-interface=bridge1` + "\n" +
		`add action=drop chain=forward comment="fully wildcard"` + "\n"

	body, _ := json.Marshal(tuneLoggingAnalyseRequest{
		Device: "core", Export: text, DarkBoundaries: []string{"bridge1|ether1"},
	})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/analyse", body)
	defer resp.Body.Close()
	var out tuneLoggingAnalyseResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rules) != 3 {
		t.Fatalf("Rules has %d entries, want 3", len(out.Rules))
	}
	for i, r := range out.Rules {
		if !r.CrossesDark {
			t.Errorf("rule %d (%s): crossesDark = false, want true (wildcard side should match)", i, r.Comment)
		}
	}
}

// TestTuneLoggingAnalyseRejectsSecrets covers #435's parser-level
// safety gate end to end: an export that is not actually
// hide-sensitive output is refused with 400, and the reason names the
// offending key.
func TestTuneLoggingAnalyseRejectsSecrets(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	text := "/interface wireless security-profiles\nadd wpa2-pre-shared-key=\"hunter2\"\n"
	body, _ := json.Marshal(tuneLoggingAnalyseRequest{Device: "core", Export: text})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/analyse", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var out struct {
		Rejected *tuneLoggingRejectReason `json:"rejected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Rejected == nil || !strings.Contains(out.Rejected.Reason, "wpa2-pre-shared-key") {
		t.Errorf("Rejected = %+v, want a reason naming wpa2-pre-shared-key", out.Rejected)
	}
}

// TestTuneLoggingRenderRejectsSecrets is the same gate on the render
// endpoint, which parses its own uploaded export independently.
func TestTuneLoggingRenderRejectsSecrets(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	text := "/ppp secret\nadd name=vpn-user password=\"hunter2\"\n"
	body, _ := json.Marshal(tuneLoggingRenderRequest{Device: "core", Export: text, Selected: []int{0}})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/render", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestTuneLoggingBodyOver2MiBIsRefused covers the endpoint's own body
// cap (#435's contract: 2 MiB, its own constant beside the shared
// 64 KiB JSON cap -- too small for a whole router config).
func TestTuneLoggingBodyOver2MiBIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	oversized := strings.Repeat("x", tuneLoggingMaxBodyBytes+1)
	body, _ := json.Marshal(tuneLoggingAnalyseRequest{Device: "core", Export: oversized})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/analyse", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a body over the 2 MiB cap", resp.StatusCode)
	}
}

// TestTuneLoggingRenderProducesAnnotatedAndCommands is the render
// endpoint's normal path: the annotated export and a matching `set`
// command per selected rule, using a comment-based matcher (rule 0's
// comment is unique) and confirming logging actually landed.
func TestTuneLoggingRenderProducesAnnotatedAndCommands(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	body, _ := json.Marshal(tuneLoggingRenderRequest{Device: "core", Export: smallExport, Selected: []int{0}})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/render", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out tuneLoggingRenderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Changed != 1 {
		t.Errorf("Changed = %d, want 1", out.Changed)
	}
	if !strings.Contains(out.Commands, `[find comment="lan to wan"]`) {
		t.Errorf("Commands = %q, want a comment-based matcher for rule 0's unique comment", out.Commands)
	}
	if !strings.Contains(out.Commands, "log=yes") {
		t.Errorf("Commands = %q, want log=yes", out.Commands)
	}

	ex, err := export.Parse(out.Annotated)
	if err != nil {
		t.Fatalf("re-parsing Annotated failed: %v", err)
	}
	if !ex.FilterRules[0].Log || ex.FilterRules[0].LogPrefix != "A|accept|" {
		t.Errorf("rule 0 in Annotated = log=%v prefix=%q, want log=true prefix=A|accept|", ex.FilterRules[0].Log, ex.FilterRules[0].LogPrefix)
	}
	if ex.FilterRules[1].Log {
		t.Error("rule 1 (not selected) logs in Annotated, want it untouched")
	}
}

// TestTuneLoggingRenderMatcherFallsBackToNumbers covers the "or else
// numbers=" half of the matcher choice: two rules sharing a comment
// cannot be addressed by it uniquely.
func TestTuneLoggingRenderMatcherFallsBackToNumbers(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	text := "# 2026/09/01 10:00:00 by RouterOS 7.24.1\n" +
		"/ip firewall filter\n" +
		`add action=drop chain=forward comment="duplicate"` + "\n" +
		`add action=drop chain=forward comment="duplicate"` + "\n"
	body, _ := json.Marshal(tuneLoggingRenderRequest{Device: "core", Export: text, Selected: []int{1}})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/render", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out tuneLoggingRenderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Commands, "[find numbers=1]") {
		t.Errorf("Commands = %q, want a numbers=1 matcher (comment is not unique)", out.Commands)
	}
}

// TestTuneLoggingRenderLoggingOnlyEnforcementBites is the load-bearing
// negative test for #435's central invariant: it substitutes the
// package-level renderExport hook with a version that corrupts a
// non-logging attribute in the rendered text (chain=forward ->
// chain=input, wherever it first appears), and asserts the mechanical
// check catches it -- 500, the fixed error body, and nothing else. If
// this test is deleted or renderExport reverted to always trust
// itself, a rendering bug that touched more than logging would ship
// silently.
func TestTuneLoggingRenderLoggingOnlyEnforcementBites(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asUser(s.mux()))
	defer ts.Close()

	original := renderExport
	renderExport = func(ex *export.Export, selected []int, prefixFor export.LogPrefixFunc) (string, int) {
		annotated, changed := original(ex, selected, prefixFor)
		tampered := strings.Replace(annotated, "chain=forward", "chain=input", 1)
		return tampered, changed
	}
	defer func() { renderExport = original }()

	body, _ := json.Marshal(tuneLoggingRenderRequest{Device: "core", Export: smallExport, Selected: []int{0}})
	resp := postTuneLogging(t, ts.URL, "/api/tune-logging/render", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the render step is tampered with", resp.StatusCode)
	}

	var out map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("body has %d top-level keys, want exactly 1 (\"error\") -- no annotated/commands output on a failed check: %v", len(out), out)
	}
	var errMsg string
	if err := json.Unmarshal(out["error"], &errMsg); err != nil || errMsg != "logging-only check failed" {
		t.Errorf(`body["error"] = %q (err %v), want "logging-only check failed"`, errMsg, err)
	}
}

// TestTuneLoggingHandlersRequireUserRole is a narrow spot-check that
// the callerIsUser gate is actually wired in (the full matrix is
// authz_matrix_test.go's job); mux() has no session at all here, which
// callerIsUser must read as not-a-user.
func TestTuneLoggingHandlersRequireUserRole(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	for _, path := range []string{"/api/tune-logging/analyse", "/api/tune-logging/render"} {
		resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader([]byte("{}")))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s with no session = %d, want 403 (mux() carries no auth middleware, so callerIsUser itself must refuse)", path, resp.StatusCode)
		}
	}
}
