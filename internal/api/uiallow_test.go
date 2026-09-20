// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/config"
)

// uiAllowServer is a server with ui.allow set to entries and, when
// proxies is non-empty, those proxies declared as trusted -- the two
// settings issue #1287 ties together.
func uiAllowServer(t *testing.T, entries []string, proxies ...string) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	allow, err := config.ParseUIAllow(entries)
	if err != nil {
		t.Fatalf("ParseUIAllow(%v): %v", entries, err)
	}
	s.UIAllow = allow
	if len(proxies) > 0 {
		prefixes, err := config.ParseTrustedProxies(proxies)
		if err != nil {
			t.Fatalf("ParseTrustedProxies(%v): %v", proxies, err)
		}
		s.TrustedProxies = prefixes
	}
	return s
}

// uiAllowReached wraps a handler that records having been reached, so a
// test can tell "the gate let this through" from "the gate refused it"
// without depending on what any real handler would have answered.
func uiAllowReached(s *Server) (http.Handler, *bool) {
	reached := new(bool)
	return s.RestrictToAllowList(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("behind the wall"))
	})), reached
}

func uiAllowRequest(path, peer string, header string, values ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = peer
	for _, v := range values {
		r.Header.Add(header, v)
	}
	return r
}

func auditEntries(t *testing.T, s *Server) []audit.Entry {
	t.Helper()
	return s.Audit.Query(audit.Query{}).Entries
}

// TestUIAllowEmptyListAdmitsEveryAddress is the upgrade property: a
// deployment that never sets ui.allow behaves exactly as it did before
// the key existed. Absent this, adding the key would lock out every
// existing install on upgrade.
func TestUIAllowEmptyListAdmitsEveryAddress(t *testing.T) {
	s, _ := newTestServer(t)
	h, reached := uiAllowReached(s)

	for _, peer := range []string{"198.51.100.5:4001", "203.0.113.9:4002", "[2001:db8::1]:4003"} {
		*reached = false
		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest("/", peer, ""))
		if w.Code != http.StatusOK || !*reached {
			t.Errorf("peer %s got %d with ui.allow unset -- an empty list must admit everybody", peer, w.Code)
		}
	}
	if got := auditEntries(t, s); len(got) != 0 {
		t.Errorf("an unset ui.allow recorded %d audit entries, want none", len(got))
	}
}

// TestUIAllowRefusesAnAddressOutsideTheList covers the refusal itself:
// a plain 403, no login page, no HTML, and one audit entry naming the
// address that was turned away.
func TestUIAllowRefusesAnAddressOutsideTheList(t *testing.T) {
	s := uiAllowServer(t, []string{"192.168.1.0/24"})
	h, reached := uiAllowReached(s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, uiAllowRequest("/", "203.0.113.9:4001", ""))

	if *reached {
		t.Error("a request from outside ui.allow reached the handler behind the gate")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	body := w.Body.String()
	if strings.Contains(strings.ToLower(body), "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("the refusal served HTML, not a plain 403 -- an address that may not see the app must not be offered its login screen: %q", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}

	entries := auditEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("recorded %d audit entries, want exactly 1: %+v", len(entries), entries)
	}
	if entries[0].Action != "ui.address_refused" {
		t.Errorf("audit action = %q, want %q", entries[0].Action, "ui.address_refused")
	}
	if entries[0].Target != "203.0.113.9" {
		t.Errorf("audit target = %q, want the refused address %q", entries[0].Target, "203.0.113.9")
	}
}

// TestUIAllowAdmitsAnAddressInsideTheList is the other half of the
// previous test: a list that refuses everything proves nothing.
func TestUIAllowAdmitsAnAddressInsideTheList(t *testing.T) {
	s := uiAllowServer(t, []string{"192.168.1.0/24", "10.0.0.7"})
	h, reached := uiAllowReached(s)

	for _, peer := range []string{"192.168.1.44:5001", "10.0.0.7:5002"} {
		*reached = false
		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest("/", peer, ""))
		if w.Code != http.StatusOK || !*reached {
			t.Errorf("peer %s got %d, want 200 -- it is inside ui.allow", peer, w.Code)
		}
	}
}

// TestUIAllowJudgesForwardedAddressesOnlyFromATrustedProxy is why the
// check runs on Server.ClientIP rather than on the peer address. Behind
// a declared proxy the forwarded address is the one judged, so the list
// names real clients; from anywhere else the header is just text the
// caller chose, so the peer address is judged and a stranger cannot
// admit themselves by naming an allowed address in a header.
func TestUIAllowJudgesForwardedAddressesOnlyFromATrustedProxy(t *testing.T) {
	t.Run("untrusted peer: the header is ignored", func(t *testing.T) {
		s := uiAllowServer(t, []string{"192.168.1.0/24"})
		h, reached := uiAllowReached(s)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest("/", "203.0.113.9:4001", "X-Forwarded-For", "192.168.1.44"))

		if *reached || w.Code != http.StatusForbidden {
			t.Errorf("status = %d -- a caller who is not a declared proxy talked their way in with X-Forwarded-For", w.Code)
		}
	})

	t.Run("trusted proxy: the forwarded address is judged", func(t *testing.T) {
		s := uiAllowServer(t, []string{"192.168.1.0/24"}, "10.9.0.0/16")
		h, _ := uiAllowReached(s)

		allowed := httptest.NewRecorder()
		h.ServeHTTP(allowed, uiAllowRequest("/", "10.9.0.2:4001", "X-Forwarded-For", "192.168.1.44"))
		if allowed.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 -- the real client is inside ui.allow and the proxy is declared", allowed.Code)
		}

		refused := httptest.NewRecorder()
		h.ServeHTTP(refused, uiAllowRequest("/", "10.9.0.2:4002", "X-Forwarded-For", "203.0.113.9"))
		if refused.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403 -- the real client behind the declared proxy is outside ui.allow", refused.Code)
		}
	})

	t.Run("trusted proxy's own address is not what is judged", func(t *testing.T) {
		// The proxy itself sits inside ui.allow here. Without ClientIP
		// the gate would admit everybody who reached it through that
		// proxy, which is the "admits everybody or nobody" failure
		// #1287 names.
		s := uiAllowServer(t, []string{"10.9.0.0/16"}, "10.9.0.0/16")
		h, _ := uiAllowReached(s)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest("/", "10.9.0.2:4001", "X-Forwarded-For", "203.0.113.9"))
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403 -- the proxy's own address being allowed must not admit everyone behind it", w.Code)
		}
	})
}

// TestUIAllowExemptPathsStayReachableFromOutsideTheList: a router is
// not a browser, and it cannot be listed in a file it never reads. Each
// of these paths has its own, narrower gate -- see uiAllowExemptPaths.
func TestUIAllowExemptPathsStayReachableFromOutsideTheList(t *testing.T) {
	s := uiAllowServer(t, []string{"192.168.1.0/24"})
	h, reached := uiAllowReached(s)

	// Written out rather than ranged over uiAllowExemptPaths: a test
	// that iterates the list it is checking passes just as happily when
	// the list is empty.
	for _, path := range []string{
		"/ca.crt",
		"/api/ingest/routeros",
		"/api/ingest/router-backup",
		"/api/droplist.rsc",
	} {
		*reached = false
		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest(path, "203.0.113.9:4001", ""))
		if w.Code != http.StatusOK || !*reached {
			t.Errorf("%s got %d from an address outside ui.allow, want 200 -- routers do not read config.yaml", path, w.Code)
		}
	}
	if got := auditEntries(t, s); len(got) != 0 {
		t.Errorf("an exempt path recorded %d audit entries, want none", len(got))
	}
}

// TestUIAllowAuditsOncePerAddressPerInterval: the 403 is the
// enforcement and the audit row is only the record, so a scanner
// retrying must not roll the admin trail (the failure #285 fixed for
// ingest pushes). Driven through the handler, so the throttle is
// exercised where it actually runs.
func TestUIAllowAuditsOncePerAddressPerInterval(t *testing.T) {
	s := uiAllowServer(t, []string{"192.168.1.0/24"})
	h, _ := uiAllowReached(s)

	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, uiAllowRequest("/", "203.0.113.9:4001", ""))
		if w.Code != http.StatusForbidden {
			t.Fatalf("request %d: status = %d, want 403 -- the throttle must silence the audit row, never the refusal", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, uiAllowRequest("/", "198.51.100.5:4002", ""))
	if w.Code != http.StatusForbidden {
		t.Fatalf("second address: status = %d, want 403", w.Code)
	}

	entries := auditEntries(t, s)
	if len(entries) != 2 {
		t.Fatalf("recorded %d audit entries, want one per address: %+v", len(entries), entries)
	}
	targets := []string{entries[0].Target, entries[1].Target}
	sort.Strings(targets)
	if targets[0] != "198.51.100.5" || targets[1] != "203.0.113.9" {
		t.Errorf("audited targets = %v, want one entry for each refused address", targets)
	}
}

// TestNoteUIRefusalInterval pins the clock-dependent half of the
// throttle directly, the way ingest_test.go pins noteIngest: an address
// is worth another row once the interval has passed, and the map of
// remembered addresses is capped, because behind a trusted proxy the
// address is caller-chosen text.
func TestNoteUIRefusalInterval(t *testing.T) {
	s := &Server{}
	now := time.Now()

	if !s.noteUIRefusal("203.0.113.9", now) {
		t.Fatal("the first refusal from an address must be audited")
	}
	if s.noteUIRefusal("203.0.113.9", now.Add(uiAllowAuditInterval-time.Second)) {
		t.Error("a second refusal inside the interval must not be audited")
	}
	if !s.noteUIRefusal("203.0.113.9", now.Add(uiAllowAuditInterval)) {
		t.Error("a refusal after the interval must be audited again")
	}
	if !s.noteUIRefusal("198.51.100.5", now) {
		t.Error("a different address must be audited on its own first refusal")
	}
}

func TestNoteUIRefusalCapsRememberedAddresses(t *testing.T) {
	s := &Server{}
	now := time.Now()

	for i := 0; i < uiAllowAuditMaxAddresses; i++ {
		addr := uiAllowCapTestAddr(i)
		if !s.noteUIRefusal(addr, now) {
			t.Fatalf("address %d (%s) was not audited while below the cap", i, addr)
		}
	}
	if s.noteUIRefusal("203.0.113.9", now) {
		t.Error("a fresh address at the cap must not be audited -- otherwise a caller varying a forwarding header rolls the whole trail")
	}
	// Once the remembered entries have aged out, recording resumes:
	// the cap bounds the rate, it does not switch auditing off.
	if !s.noteUIRefusal("203.0.113.9", now.Add(uiAllowAuditInterval)) {
		t.Error("auditing must resume once the remembered addresses have expired")
	}
}

// uiAllowCapTestAddr builds distinct addresses for the cap test
// without depending on any one documentation range being large enough
// to hold uiAllowAuditMaxAddresses of them.
func uiAllowCapTestAddr(i int) string {
	return "100.64." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256)
}

// TestUIAllowExemptsEveryRouterFacingRoute holds uiAllowExemptPaths
// against the bearer muxes it mirrors. A new router-facing route added
// to ingestRoutes or droplistPullRoutes without a decision here would
// otherwise start being refused for a deployment that set ui.allow --
// and the symptom would be a router that quietly stopped pushing, not
// an error anyone reads.
func TestUIAllowExemptsEveryRouterFacingRoute(t *testing.T) {
	source, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("reading auth.go: %v", err)
	}

	var registered []string
	for _, fn := range []string{"ingestRoutes", "droplistPullRoutes"} {
		body := regexp.MustCompile(`(?s)func \(s \*Server\) ` + fn + `\(\) http\.Handler \{(.*?)\n\}`).FindSubmatch(source)
		if body == nil {
			t.Fatalf("found no %s function in auth.go -- this test is not looking where it thinks it is", fn)
		}
		for _, m := range regexp.MustCompile(`mux\.HandleFunc\("[A-Z]+ (/[^"]+)"`).FindAllSubmatch(body[1], -1) {
			registered = append(registered, string(m[1]))
		}
	}
	if len(registered) == 0 {
		t.Fatal("found no routes on the router-facing bearer muxes -- this test is not looking where it thinks it is")
	}

	var missing []string
	for _, route := range registered {
		if !uiAllowExemptPaths[route] {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d router-facing route(s) are not in uiAllowExemptPaths, so ui.allow would refuse a router that cannot be listed in it: %s",
			len(missing), strings.Join(missing, ", "))
	}
}

// TestUIAllowRefusalBodySaysNothingAboutTheApp: the refusal is served
// to an address that is not allowed to know what is here.
func TestUIAllowRefusalBodySaysNothingAboutTheApp(t *testing.T) {
	s := uiAllowServer(t, []string{"192.168.1.0/24"})
	h, _ := uiAllowReached(s)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, uiAllowRequest("/api/events", "203.0.113.9:4001", ""))

	body, _ := io.ReadAll(w.Body)
	for _, leak := range []string{"MikroView", "mikroview", "ui.allow", "192.168.1"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("the 403 body mentions %q -- it should say nothing about the app or its configuration: %q", leak, body)
		}
	}
}
