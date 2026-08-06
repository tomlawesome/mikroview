package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionBootMessageFreshInstallNoUpgradeAlert(t *testing.T) {
	got := versionBootMessage("", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage(%q, %q) = %q, want %q", "", "abc1234", got, want)
	}
}

func TestVersionBootMessageSameVersionNoUpgradeAlert(t *testing.T) {
	got := versionBootMessage("abc1234", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage with an unchanged version = %q, want %q (no upgrade alert on a routine restart)", got, want)
	}
}

func TestVersionBootMessageUpgradeDetected(t *testing.T) {
	got := versionBootMessage("abc1234", "def5678")
	want := "upgraded from abc1234 to def5678"
	if got != want {
		t.Errorf("versionBootMessage across a version change = %q, want %q", got, want)
	}
}

func TestVersionBootMessageTrimsWhitespaceFromPersistedMarker(t *testing.T) {
	// The marker file is read back with os.ReadFile -- a trailing
	// newline (from an editor, or just how it happens to have been
	// written) shouldn't itself look like a version change.
	got := versionBootMessage("abc1234\n", "abc1234")
	want := "version abc1234"
	if got != want {
		t.Errorf("versionBootMessage with a trailing newline in the persisted marker = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetStripsPortAndAssumes443(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview.local:8081/api/events?x=1", nil)
	r.Host = "mikroview.local:8081"
	got := httpsRedirectTarget(r, []string{"mikroview.local"})
	want := "https://mikroview.local/api/events?x=1"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetHostWithNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://192.168.1.50/", nil)
	r.Host = "192.168.1.50"
	got := httpsRedirectTarget(r, []string{"192.168.1.50"})
	want := "https://192.168.1.50/"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetPreservesPathAndQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview:80/ca.crt", nil)
	r.Host = "mikroview:80"
	got := httpsRedirectTarget(r, []string{"mikroview"})
	want := "https://mikroview/ca.crt"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

// TestHTTPSRedirectTargetRejectsUnlistedHost proves the actual fix: a
// client connecting directly (not through a real browser navigation)
// fully controls the Host header, and previously that value was
// echoed straight into the Location header unvalidated -- a
// straightforward open redirect for anyone able to reach this
// listener directly.
func TestHTTPSRedirectTargetRejectsUnlistedHost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview:80/some/path", nil)
	r.Host = "evil.example.com"
	got := httpsRedirectTarget(r, []string{"mikroview", "192.168.1.50"})
	want := "https://mikroview/some/path"
	if got != want {
		t.Errorf("httpsRedirectTarget with an unlisted Host = %q, want a fall back to the first allowed host: %q", got, want)
	}
}

// TestHTTPSRedirectTargetEmptyAllowlistFallsBackToPriorBehavior covers
// the case TLS.Hosts is left unconfigured (auto-detected SANs instead
// -- see internal/servertls) -- with no explicit ground truth to
// validate against, this keeps the original echo-Host behavior rather
// than guessing.
func TestHTTPSRedirectTargetEmptyAllowlistFallsBackToPriorBehavior(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview.local/", nil)
	r.Host = "mikroview.local"
	got := httpsRedirectTarget(r, nil)
	want := "https://mikroview.local/"
	if got != want {
		t.Errorf("httpsRedirectTarget with an empty allowlist = %q, want %q", got, want)
	}
}

func TestSecurityHeadersSetOnEveryResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	rr := httptest.NewRecorder()
	securityHeaders(inner, false).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	h := rr.Header()
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", h.Get("X-Content-Type-Options"))
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", h.Get("X-Frame-Options"))
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("expected a Content-Security-Policy header to be set")
	}
	if h.Get("Strict-Transport-Security") != "" {
		t.Errorf("expected no HSTS header when hsts=false, got %q", h.Get("Strict-Transport-Security"))
	}

	rr2 := httptest.NewRecorder()
	securityHeaders(inner, true).ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr2.Header().Get("Strict-Transport-Security") == "" {
		t.Error("expected an HSTS header when hsts=true")
	}
}
