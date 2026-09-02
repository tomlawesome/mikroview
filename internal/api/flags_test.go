// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/flags"
)

func TestHandleFlagsList(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "20 distinct ports in 60s", time.Now())

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/flags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Flags      []flags.Flag           `json:"flags"`
		TimeSeries []flags.FlagTimeBucket `json:"timeSeries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Flags) != 1 || body.Flags[0].Target != "203.0.113.9" || body.Flags[0].Type != flags.TypePortScan {
		t.Errorf("unexpected flags: %+v", body.Flags)
	}

	// Same 60-entry fixed-width-window shape as GET /api/stats's
	// timeSeries (internal/store/ring.go's Stats.TimeSeries), and the one
	// new episode raised above should land in the current minute's
	// bucket.
	if len(body.TimeSeries) != 60 {
		t.Fatalf("expected 60 time series buckets, got %d", len(body.TimeSeries))
	}
	last := body.TimeSeries[len(body.TimeSeries)-1]
	if last.ByType[flags.TypePortScan] != 1 {
		t.Errorf("expected the just-raised port_scan episode in the latest bucket, got %+v", last.ByType)
	}
}

// -- #640: POST /api/flags/{id}/verdict -----------------------------

// postVerdict is postFlagsAction's shape for the one flags.go handler
// that actually reads a body -- a bare {"verdict": "..."} object, no
// CSRF header needed since these tests go through s.mux() (asAdmin),
// not the real session-authenticated Routes().
func postVerdict(t *testing.T, url, verdict string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"verdict": verdict})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestHandleFlagsVerdictExpectedClearsFlag and
// TestHandleFlagsVerdictCheckedClearsFlag cover #640's contract that
// every verdict but investigate clears the flag, and that the response
// is the updated flag itself (200 with verdict/verdictBy/verdictAt set)
// rather than a {"cleared": bool} envelope.
func TestHandleFlagsVerdictExpectedClearsFlag(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "20 distinct ports in 60s", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var f flags.Flag
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if f.Verdict != flags.VerdictExpected {
		t.Errorf("response Verdict = %q, want %q", f.Verdict, flags.VerdictExpected)
	}
	if f.VerdictBy != "admin" {
		t.Errorf("response VerdictBy = %q, want admin", f.VerdictBy)
	}
	if f.VerdictAt.IsZero() {
		t.Error("response VerdictAt should be set")
	}
	if !f.Cleared {
		t.Error("expected verdict should clear the flag")
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared || list[0].Verdict != flags.VerdictExpected {
		t.Errorf("expected the flag to be cleared and verdict-marked in the store, got %+v", list)
	}
}

func TestHandleFlagsVerdictCheckedClearsFlag(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypeActivitySpike, "198.51.100.4", "500 events in 60s", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "checked")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared || list[0].Verdict != flags.VerdictChecked {
		t.Errorf("expected the flag to be cleared and verdict-marked checked, got %+v", list)
	}
	// Checked learns nothing that suppresses -- that is what separates it
	// from expected, and it has to hold at the HTTP layer too.
	if len(s.Flags.ListExclusions()) != 0 {
		t.Errorf("a checked verdict must record no expectation, got %+v", s.Flags.ListExclusions())
	}
}

// TestHandleFlagsVerdictResolvedClearsWithoutSuppressing is resolved's
// own half of the same contract: it clears, and deliberately does not
// suppress, so the same circumstances recurring bring the flag back.
func TestHandleFlagsVerdictResolvedClearsWithoutSuppressing(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypeCriticalPort, "198.51.100.5", "6 attempts on port 22", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "resolved")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared || list[0].Verdict != flags.VerdictResolved {
		t.Errorf("expected the flag to be cleared and verdict-marked resolved, got %+v", list)
	}
	if len(s.Flags.ListExclusions()) != 0 {
		t.Errorf("a resolved verdict must record no expectation, got %+v", s.Flags.ListExclusions())
	}

	// It's back: the same circumstances recur, and the returning flag
	// carries the memory the card reads.
	s.Flags.Add(flags.TypeCriticalPort, "198.51.100.5", "6 attempts on port 22", time.Now().Add(time.Hour))
	back := s.Flags.List()[0]
	if back.Cleared {
		t.Error("a resolved verdict must not suppress the recurrence")
	}
	if back.PriorVerdict != flags.VerdictResolved {
		t.Errorf("the returning flag should remember it was resolved, got %q", back.PriorVerdict)
	}
}

// TestHandleFlagsVerdictInvestigateLeavesFlagOpen, through the HTTP
// layer: investigate is the one verdict that leaves the flag open, so
// the row can switch to expected/resolved while someone works on it.
func TestHandleFlagsVerdictInvestigateLeavesFlagOpen(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypeCriticalPort, "203.0.113.11", "d", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "investigate")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := s.Flags.List()
	if len(list) != 1 || list[0].Cleared {
		t.Errorf("an investigate verdict must not clear the flag, got %+v", list)
	}
	if list[0].Verdict != flags.VerdictInvestigate {
		t.Errorf("expected the flag's Verdict to be recorded as investigate, got %+v", list)
	}
}

// TestHandleFlagsVerdictInvalidVerdictReturns400 covers the contract's
// 400 case: a verdict outside the four recognised labels. "noise" is
// used deliberately -- it was a real verdict before #640 removed it, so
// this also pins that the removal is wholesale rather than an alias.
func TestHandleFlagsVerdictInvalidVerdictReturns400(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.12", "d", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "bogus")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unrecognised verdict", resp.StatusCode)
	}

	list := s.Flags.List()
	if list[0].Verdict != "" {
		t.Errorf("an invalid verdict must not mutate the flag, got %+v", list)
	}
}

// TestHandleFlagsVerdictUnknownIDReturns404 covers the contract's 404
// case: an unknown flag id is an error here, not the silent no-op the
// removed plain clear treated it as.
func TestHandleFlagsVerdictUnknownIDReturns404(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/does-not-exist/verdict", "investigate")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown flag id", resp.StatusCode)
	}
}

// -- DELETE /api/flags/verdict/{id} (undo) --------------------------

// deleteVerdict issues the undo call -- no body, same shape as
// postFlagsAction but with the DELETE verb the endpoint actually uses.
func deleteVerdict(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestHandleFlagsVerdictUndoReopensFlag covers the ordinary undo case
// through the HTTP layer: judging an open flag clears it, and undoing
// re-opens it with the verdict fields reset.
func TestHandleFlagsVerdictUndoReopensFlag(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.20", "d", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "checked")
	resp.Body.Close()
	if !s.Flags.List()[0].Cleared {
		t.Fatal("setup: expected the checked verdict to clear the flag")
	}

	resp = deleteVerdict(t, ts.URL+"/api/flags/verdict/"+id)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var f flags.Flag
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if f.Cleared {
		t.Error("undo should re-open a flag the verdict itself cleared")
	}
	if f.Verdict != "" {
		t.Errorf("response Verdict = %q, want empty after undo", f.Verdict)
	}

	list := s.Flags.List()
	if len(list) != 1 || list[0].Cleared || list[0].Verdict != "" {
		t.Errorf("expected the flag to be re-opened and un-judged in the store, got %+v", list)
	}
}

// TestHandleFlagsVerdictUndoLeavesAlreadyClearedFlagCleared is #638's
// central subtlety, exercised through the HTTP layer: judging a flag
// that was already cleared before the verdict must not let undo re-open
// it.
func TestHandleFlagsVerdictUndoLeavesAlreadyClearedFlagCleared(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.21", "d", time.Now())
	id := s.Flags.List()[0].ID
	if _, ok := s.Flags.SetVerdict(id, flags.VerdictChecked, "someone", time.Now()); !ok {
		t.Fatal("setup: expected the first, clearing verdict to succeed")
	}

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected")
	resp.Body.Close()

	resp = deleteVerdict(t, ts.URL+"/api/flags/verdict/"+id)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	list := s.Flags.List()
	if len(list) != 1 || !list[0].Cleared {
		t.Errorf("undo must not re-open a flag that was already cleared before it was judged, got %+v", list)
	}
	if list[0].Verdict != "" {
		t.Errorf("expected the verdict to still be cleared out, got %+v", list)
	}
}

// TestHandleFlagsVerdictUndoUnknownIDReturns404 covers the contract's
// 404 case, same as the POST side.
func TestHandleFlagsVerdictUndoUnknownIDReturns404(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := deleteVerdict(t, ts.URL+"/api/flags/verdict/does-not-exist")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown flag id", resp.StatusCode)
	}
}

// TestHandleFlagsClearAll covers issue #198's bulk clear: every active
// flag ends up cleared in one request, an already-cleared flag is left
// alone and not recounted, and the response reports how many were
// actually cleared.
func TestHandleFlagsClearAll(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.1", "d1", time.Now())
	s.Flags.Add(flags.TypeActivitySpike, "203.0.113.2", "d2", time.Now())
	preClearedID := s.Flags.List()[0].ID
	s.Flags.SetVerdict(preClearedID, flags.VerdictChecked, "someone", time.Now())

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/clear-all", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Cleared int `json:"cleared"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Cleared != 1 {
		t.Errorf("cleared = %d, want 1 (the pre-cleared flag must not be recounted)", body.Cleared)
	}

	for _, f := range s.Flags.List() {
		if !f.Cleared {
			t.Errorf("flag %+v still active after clear-all", f)
		}
	}
}

// TestHandleFlagsClearAllAvailableToUserNotViewer pins #653's tightening
// of clear-all (and, by the same reasoning, the other three flag writes
// below it -- see TestHandleFlagsWritesRefuseViewer): a plain user may
// still call it, same as before viewer existed to exclude, but a viewer
// -- who must not change anything that affects the instance -- may not,
// even though the action is reversible.
func TestHandleFlagsClearAllAvailableToUserNotViewer(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "port scan", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "watcher", Password: "password789", Role: "viewer"}).Body.Close()

	userClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, userClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "watcher", Password: "password789"}).Body.Close()

	viewerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/flags/clear-all", nil)
	viewerReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	viewerResp, err := viewerClient.Do(viewerReq)
	if err != nil {
		t.Fatal(err)
	}
	defer viewerResp.Body.Close()
	if viewerResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a viewer clear-all to be forbidden (#653), got %d", viewerResp.StatusCode)
	}
	if got := s.Flags.List(); len(got) != 1 || got[0].Cleared {
		t.Errorf("a refused clear-all must have no effect, got %+v", got)
	}

	userReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/flags/clear-all", nil)
	userReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	userResp, err := userClient.Do(userReq)
	if err != nil {
		t.Fatal(err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		t.Errorf("expected a user clear-all to succeed, got %d", userResp.StatusCode)
	}
}

// TestHandleFlagsWritesRefuseViewer covers the two remaining flag writes
// #653 tightened from "any signed-in session" to user tier: the verdict
// and its undo (the plain clear it also covered is gone, #640). A viewer
// is refused both; a user succeeds at both.
// TestHandleFlagsClearAllAvailableToUserNotViewer above covers clear-all
// with the same shape.
func TestHandleFlagsWritesRefuseViewer(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "port scan", time.Now())
	flagID := s.Flags.List()[0].ID
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "watcher", Password: "password789", Role: "viewer"}).Body.Close()

	userClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, userClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "watcher", Password: "password789"}).Body.Close()

	// Viewer: refused both.
	viewerVerdictResp := postJSON(t, viewerClient, ts.URL+"/api/flags/"+flagID+"/verdict", verdictRequest{Verdict: flags.VerdictChecked})
	viewerVerdictResp.Body.Close()
	if viewerVerdictResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a viewer verdict to be forbidden, got %d", viewerVerdictResp.StatusCode)
	}

	viewerUndoReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/flags/verdict/"+flagID, nil)
	viewerUndoReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	viewerUndoResp, err := viewerClient.Do(viewerUndoReq)
	if err != nil {
		t.Fatal(err)
	}
	viewerUndoResp.Body.Close()
	if viewerUndoResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a viewer verdict-undo to be forbidden, got %d", viewerUndoResp.StatusCode)
	}

	if got := s.Flags.List(); len(got) != 1 || got[0].Cleared || got[0].Verdict != "" {
		t.Errorf("every refused viewer write must have no effect, got %+v", got)
	}

	// User: succeeds at both -- a verdict, then its own undo, each
	// checked against real store state, not just the status code.
	userVerdictResp := postJSON(t, userClient, ts.URL+"/api/flags/"+flagID+"/verdict", verdictRequest{Verdict: flags.VerdictChecked})
	userVerdictResp.Body.Close()
	if userVerdictResp.StatusCode != http.StatusOK {
		t.Errorf("expected a user verdict to succeed, got %d", userVerdictResp.StatusCode)
	}
	if got := s.Flags.List()[0]; got.Verdict != flags.VerdictChecked {
		t.Errorf("expected the verdict to be recorded, got %+v", got)
	}

	userUndoReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/flags/verdict/"+flagID, nil)
	userUndoReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	userUndoResp, err := userClient.Do(userUndoReq)
	if err != nil {
		t.Fatal(err)
	}
	userUndoResp.Body.Close()
	if userUndoResp.StatusCode != http.StatusOK {
		t.Errorf("expected a user verdict-undo to succeed, got %d", userUndoResp.StatusCode)
	}
	if got := s.Flags.List()[0]; got.Cleared || got.Verdict != "" {
		t.Errorf("expected the undo to re-open the flag it judged, got %+v", got)
	}
}

// TestHandleFlagsClearAllCreatesNoExpectations is the HTTP-level half of
// the invariant #198 states explicitly, in #640's vocabulary: a bulk
// clear must never record an expectation -- only a judged flag does.
func TestHandleFlagsClearAllCreatesNoExpectations(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.9", "port scan", time.Now())

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/flags/clear-all", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if n := len(s.Flags.ListExclusions()); n != 0 {
		t.Errorf("clear-all recorded %d expectations, want 0", n)
	}
}

// TestHandleFlagsClearAllIsAuditLoggedOnce pins "one audit entry for the
// whole call, not one per flag" -- handleFlagsClearAll's own doc
// comment states this is deliberate.
func TestHandleFlagsClearAllIsAuditLoggedOnce(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.1", "d1", time.Now())
	s.Flags.Add(flags.TypeActivitySpike, "203.0.113.2", "d2", time.Now())
	s.Flags.Add(flags.TypeCriticalPort, "203.0.113.3", "d3", time.Now())
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/flags/clear-all", nil)
	resp.Body.Close()

	var matches []audit.Entry
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "flag.clear_all" {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 flag.clear_all audit entry for 3 cleared flags, got %d: %+v", len(matches), matches)
	}
	if matches[0].Detail != "cleared 3 flags" {
		t.Errorf("audit entry detail = %q, want %q", matches[0].Detail, "cleared 3 flags")
	}
}

// TestHandleFlagsClearAllOnEmptyStoreSkipsAudit: clearing nothing is not
// a meaningful action, so it is not logged as one.
func TestHandleFlagsClearAllOnEmptyStoreSkipsAudit(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	resp := postJSON(t, adminClient, ts.URL+"/api/flags/clear-all", nil)
	resp.Body.Close()

	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "flag.clear_all" {
			t.Errorf("unexpected flag.clear_all audit entry on an empty store: %+v", e)
		}
	}
}

// TestHandleFlagsVerdictExpectedRecordsASizedExpectation is #640 part
// B's central API contract, end to end through the real flags.Store: the
// expected verdict is what records the expectation now that the admin-
// only clear-permanent endpoint is gone, it takes its size from the
// flag the operator looked at, and a later firing within tolerance is
// absorbed rather than raised.
func TestHandleFlagsVerdictExpectedRecordsASizedExpectation(t *testing.T) {
	s, _ := newTestServer(t)
	size := 30
	s.Flags.AddEmission(flags.TypePortScan, "203.0.113.40", "30 distinct ports in 60s", nil, flags.Evidence{}, "", false, &size, time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected")
	resp.Body.Close()

	ex, ok := s.Flags.Expectation(flags.TypePortScan, "203.0.113.40")
	if !ok {
		t.Fatal("expected the expected verdict to record an expectation")
	}
	if ex.Size == nil || *ex.Size != 30 {
		t.Fatalf("expected the recorded size to be the flag's own 30, got %v", ex.Size)
	}

	// Within 1.5x: absorbed, so nothing returns to the inbox.
	within := 40
	s.Flags.AddEmission(flags.TypePortScan, "203.0.113.40", "40 ports", nil, flags.Evidence{}, "", false, &within, time.Now())
	if got := s.Flags.List()[0]; !got.Cleared {
		t.Errorf("a firing within tolerance must stay absorbed, got %+v", got)
	}

	// Above it: back, carrying both numbers for the card.
	above := 120
	s.Flags.AddEmission(flags.TypePortScan, "203.0.113.40", "120 ports", nil, flags.Evidence{}, "", false, &above, time.Now())
	back := s.Flags.List()[0]
	if back.Cleared {
		t.Fatalf("a firing above tolerance must raise the flag again, got %+v", back)
	}
	if back.ExpectedSize == nil || *back.ExpectedSize != 30 || back.Size == nil || *back.Size != 120 {
		t.Errorf("expected the returning flag to carry expected 30 / saw 120, got %v / %v", back.ExpectedSize, back.Size)
	}
}

// TestHandleFlagsVerdictUndoWithdrawsTheExpectation: undo is offered on
// an expected verdict, so it has to reverse the suppression as well as
// the clear -- an undo that reopened the flag but left the expectation
// standing would silently absorb every later firing of it.
func TestHandleFlagsVerdictUndoWithdrawsTheExpectation(t *testing.T) {
	s, _ := newTestServer(t)
	size := 30
	s.Flags.AddEmission(flags.TypePortScan, "203.0.113.41", "30 distinct ports in 60s", nil, flags.Evidence{}, "", false, &size, time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected").Body.Close()
	deleteVerdict(t, ts.URL+"/api/flags/verdict/"+id).Body.Close()

	if _, ok := s.Flags.Expectation(flags.TypePortScan, "203.0.113.41"); ok {
		t.Error("undoing an expected verdict must withdraw the expectation it recorded")
	}
	if got := s.Flags.List()[0]; got.Cleared || got.Verdict != "" {
		t.Errorf("expected the undo to re-open the flag, got %+v", got)
	}
}

// TestHandleFlagsVerdictIsAuditLogged: an expected verdict suppresses
// future detection for a (detector, target) pair at user tier, where the
// exclude-forever it replaces was admin-only. "Who decided this stopped
// being flagged" therefore has to stay answerable from the audit log.
func TestHandleFlagsVerdictIsAuditLogged(t *testing.T) {
	s := newAuthTestServer(t)
	s.Flags.Add(flags.TypePortScan, "203.0.113.42", "20 distinct ports in 60s", time.Now())
	flagID := s.Flags.List()[0].ID
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "tom", Password: "password123"}).Body.Close()

	resp := postJSON(t, client, ts.URL+"/api/flags/"+flagID+"/verdict", verdictRequest{Verdict: flags.VerdictExpected})
	resp.Body.Close()

	var found bool
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "flag.verdict" && e.Target == flagID {
			found = true
			if e.Actor != "tom" {
				t.Errorf("audit entry actor = %q, want tom", e.Actor)
			}
			if e.Detail != "expected" {
				t.Errorf("audit entry detail = %q, want the verdict itself", e.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected a flag.verdict audit entry for %s, got: %+v", flagID, s.Audit.Query(audit.Query{}).Entries)
	}

	undoReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/flags/verdict/"+flagID, nil)
	undoReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	undoResp, err := client.Do(undoReq)
	if err != nil {
		t.Fatal(err)
	}
	undoResp.Body.Close()

	var undoFound bool
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "flag.verdict_undo" && e.Target == flagID {
			undoFound = true
		}
	}
	if !undoFound {
		t.Errorf("expected a flag.verdict_undo audit entry for %s, got: %+v", flagID, s.Audit.Query(audit.Query{}).Entries)
	}
}
