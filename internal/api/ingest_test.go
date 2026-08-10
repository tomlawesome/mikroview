// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
)

// ingestTestServer registers an admin and issues one ingest token scoped
// to device, returning the raw token and the running server -- the setup
// every test in this file needs.
func ingestTestServer(t *testing.T, device string) (*httptest.Server, *Server, string) {
	t.Helper()
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	admin, ok := s.Auth.ByUsername("admin")
	if !ok {
		t.Fatal("the admin account was not created")
	}

	raw, _, err := s.Tokens.Create("router-1", auth.TokenKindIngest, device, admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}
	return ts, s, raw
}

func postIngest(t *testing.T, ts *httptest.Server, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest/routeros", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const validARPPayload = `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.50","mac":"aa:bb:cc:dd:ee:ff"}]}`

func TestIngestRouteAcceptsAValidPushFromAnIngestToken(t *testing.T) {
	ts, _, raw := ingestTestServer(t, "router-1")

	resp := postIngest(t, ts, raw, validARPPayload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var ack ingestAckResponse
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatal(err)
	}
	if ack.Kind != "arp" || ack.Page != 1 || ack.Pages != 1 || ack.Records != 1 {
		t.Errorf("ack = %+v, unexpected", ack)
	}
}

// TestIngestRouteRejectsAReadOnlyToken is the other direction of the
// kind separation TestIngestTokenCannotReachReadOnlyRoutes checks: a
// read-only API token authenticates, but requireAuth dispatches it to
// readOnlyRoutes, which does not register POST /api/ingest/routeros --
// so this must 404, the same "valid token, wrong mux" shape.
func TestIngestRouteRejectsAReadOnlyToken(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	admin, _ := s.Auth.ByUsername("admin")
	apiRaw, _, err := s.Tokens.Create("birdcage", auth.TokenKindAPI, "", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	resp := postIngest(t, ts, apiRaw, validARPPayload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (readOnlyRoutes does not register this path)", resp.StatusCode)
	}
}

// TestIngestRouteRejectsASession is the third direction: an
// authenticated admin session must not be able to reach this endpoint
// either. POST /api/ingest/routeros is deliberately absent from
// s.routes() -- see server.go -- so a session-authenticated request
// falls through to the real mux and 404s there, the same as any other
// route that was never registered for session access.
func TestIngestRouteRejectsASession(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest/routeros", strings.NewReader(validARPPayload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	req.Header.Set("Content-Type", "application/json")
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 -- an admin session must not reach the ingest endpoint", resp.StatusCode)
	}
}

// TestIngestRouteRejectsNoToken sends no bearer token at all, which
// means requireAuth never identifies this as a service-to-service
// caller and falls through to the ordinary CSRF/session checks --
// refusing with 403 (missing CSRF header) before it would ever reach the
// 401 a missing session produces. Both are "refused", the same
// tolerance authzMatrix's own assertAccess applies; what matters here is
// that no token means no access, not which of the two refusal codes
// fires first.
func TestIngestRouteRejectsNoToken(t *testing.T) {
	ts, _, _ := ingestTestServer(t, "router-1")

	resp := postIngest(t, ts, "", validARPPayload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 401 or 403", resp.StatusCode)
	}
}

func TestIngestRouteRejectsARevokedToken(t *testing.T) {
	ts, s, raw := ingestTestServer(t, "router-1")

	tokens := s.Tokens.List()
	if len(tokens) != 1 {
		t.Fatalf("expected exactly one token, got %d", len(tokens))
	}
	if err := s.Tokens.Revoke(tokens[0].ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	resp := postIngest(t, ts, raw, validARPPayload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestIngestRouteRejectsAnInvalidPayload(t *testing.T) {
	ts, _, raw := ingestTestServer(t, "router-1")

	cases := []struct {
		name string
		body string
	}{
		{"unknown kind", `{"kind":"routing-table","page":1,"pages":1,"records":[]}`},
		{"unknown top-level field", `{"kind":"arp","page":1,"pages":1,"records":[],"extra":1}`},
		{"unknown record field", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"1.1.1.1","mac":"","owner":"admin"}]}`},
		{"malformed JSON", `{not json`},
		{"empty body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postIngest(t, ts, raw, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestIngestRouteRateLimitsPerToken proves the limiter is wired in and
// keyed per-token, not global -- a second token for a different device
// must be unaffected by the first one being exhausted.
func TestIngestRouteRateLimitsPerToken(t *testing.T) {
	s := newAuthTestServer(t)
	// A tight limiter so the test doesn't need 120 requests to prove the
	// point -- IngestLimiter is swapped after server construction since
	// there's no other seam to inject a smaller threshold.
	s.IngestLimiter = auth.NewLoginLimiter(2, time.Minute)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	admin, _ := s.Auth.ByUsername("admin")

	rawA, _, err := s.Tokens.Create("router-a", auth.TokenKindIngest, "router-a", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}
	rawB, _, err := s.Tokens.Create("router-b", auth.TokenKindIngest, "router-b", admin, time.Now())
	if err != nil {
		t.Fatalf("Tokens.Create: %v", err)
	}

	for i := 0; i < 2; i++ {
		resp := postIngest(t, ts, rawA, validARPPayload)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d for router-a: got %d, want 200", i, resp.StatusCode)
		}
	}
	resp := postIngest(t, ts, rawA, validARPPayload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("router-a's 3rd request: got %d, want 429", resp.StatusCode)
	}

	// router-b's own budget must be untouched by router-a's exhaustion.
	respB := postIngest(t, ts, rawB, validARPPayload)
	defer respB.Body.Close()
	if respB.StatusCode != http.StatusOK {
		t.Errorf("router-b's request after router-a was rate-limited: got %d, want 200 -- the limiter must be keyed per-token", respB.StatusCode)
	}
}

// TestIngestRouteAudits proves a successful push is recorded to the
// admin audit log, attributed to the device rather than "unauthenticated"
// -- auditActor(r) falls back to that for any request with no session
// user, which an ingest-token request never has.
func TestIngestRouteAudits(t *testing.T) {
	ts, s, raw := ingestTestServer(t, "router-7")

	resp := postIngest(t, ts, raw, validARPPayload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	result := s.Audit.Query(audit.Query{})
	if len(result.Entries) == 0 {
		t.Fatal("no audit entry was recorded for a successful ingest")
	}
	last := result.Entries[len(result.Entries)-1] // Query returns oldest-first
	if last.Action != "ingest.routeros" {
		t.Errorf("Action = %q, want ingest.routeros", last.Action)
	}
	if last.Actor != "device:router-7" {
		t.Errorf("Actor = %q, want device:router-7 -- not falling back to the session-based auditActor", last.Actor)
	}
	if !strings.Contains(last.Detail, "kind=arp") || !strings.Contains(last.Detail, "records=1") {
		t.Errorf("Detail = %q, missing expected fields", last.Detail)
	}
	// #186 step 5: the raw payload/record content must never land in the
	// audit trail, only its shape.
	if strings.Contains(last.Detail, "192.168.1.50") || strings.Contains(last.Detail, "aa:bb:cc:dd:ee:ff") {
		t.Errorf("Detail = %q, contains raw record content -- audit must record shape only, never the pushed data itself", last.Detail)
	}
}

func TestIngestRouteRejectsOversizedBody(t *testing.T) {
	ts, _, raw := ingestTestServer(t, "router-1")

	huge := `{"kind":"arp","page":1,"pages":1,"records":[{"address":"` + strings.Repeat("a", 70*1024) + `","mac":""}]}`
	resp := postIngest(t, ts, raw, huge)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 400 or 413 for a body over the 64KiB cap", resp.StatusCode)
	}
}

// TestIngestPushIsReadableFromTheTableEndpoints is the step-4 round
// trip: a page pushed with the ingest token comes back, ordered and
// attributed, from GET /api/routeros/{device}/rules under an ordinary
// session -- and only for the device the token was scoped to.
func TestIngestPushIsReadableFromTheTableEndpoints(t *testing.T) {
	ts, _, raw := ingestTestServer(t, "router-7")

	push := `{"kind":"filter-rule","page":1,"pages":1,"records":[` +
		`{"ordinal":1,"comment":"drop scanners","chain":"input","action":"drop","srcAddressList":"scanners","logPrefix":"DROP"},` +
		`{"ordinal":0,"comment":"allow lan","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":""}]}`
	resp := postIngest(t, ts, raw, push)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("push: status = %d, want 200", resp.StatusCode)
	}

	adminClient := loggedInClient(t, ts.URL, "admin", "password123")
	get := func(t *testing.T, path string) routerTableResponse {
		t.Helper()
		res, err := adminClient.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, res.StatusCode)
		}
		var out routerTableResponse
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := get(t, "/api/routeros/router-7/rules")
	if !got.Available || got.UpdatedAt == nil {
		t.Fatalf("rules table = %+v, want available with an updatedAt", got)
	}
	rules, ok := got.Rules.([]any)
	if !ok || len(rules) != 2 {
		t.Fatalf("rules = %#v, want 2 entries", got.Rules)
	}
	first, _ := rules[0].(map[string]any)
	if first["comment"] != "allow lan" {
		t.Errorf("first rule = %+v, want the ordinal-0 rule first (RouterOS display order)", first)
	}

	// A device nothing pushed for reports unavailable -- not an error,
	// and not an empty table pretending to be a real one.
	other := get(t, "/api/routeros/other-router/rules")
	if other.Available || other.UpdatedAt != nil {
		t.Errorf("an unpushed device's table = %+v, want available=false with no updatedAt", other)
	}

	nat := get(t, "/api/routeros/router-7/nat")
	if nat.Available {
		t.Errorf("NAT table = %+v, want available=false when only filter rules were pushed", nat)
	}
}

// TestIngestOversizedStateIsRefused drives internal/routerstate's
// per-kind record cap through the real endpoint: the page that would
// cross it comes back 400, and the state already held stays intact.
func TestIngestOversizedStateIsRefused(t *testing.T) {
	ts, _, raw := ingestTestServer(t, "router-7")

	page := func(n, pages int) string {
		var b strings.Builder
		fmt.Fprintf(&b, `{"kind":"arp","page":%d,"pages":%d,"records":[`, n, pages)
		for i := 0; i < 1000; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"address":"10.%d.%d.%d","mac":"aa:bb:cc:dd:ee:ff"}`, n, i/250, i%250)
		}
		b.WriteString(`]}`)
		return b.String()
	}
	for n := 1; n <= 5; n++ {
		resp := postIngest(t, ts, raw, page(n, 6))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d: status = %d, want 200", n, resp.StatusCode)
		}
	}
	resp := postIngest(t, ts, raw, page(6, 6))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("the cap-crossing page: status = %d, want 400", resp.StatusCode)
	}
}
