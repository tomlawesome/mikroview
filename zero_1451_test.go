// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// #1451: paths no other test reached.

func TestSamePortRedirectHostKeepsThePortAndPinsTheHost(t *testing.T) {
	allowed := []string{"mikroview.example.net", "192.0.2.10"}
	cases := []struct {
		name, requested, local, want string
	}{
		{"keeps an allowed host and its port", "192.0.2.10:8443", "0.0.0.0:8443", "192.0.2.10:8443"},
		{"never echoes an unlisted host", "evil.example.org:8443", "0.0.0.0:8443", "mikroview.example.net:8443"},
		{"takes the port from the local address when the request has none", "mikroview.example.net", "0.0.0.0:9443", "mikroview.example.net:9443"},
		{"no port anywhere gives the bare host", "mikroview.example.net", "", "mikroview.example.net"},
	}
	for _, c := range cases {
		if got := samePortRedirectHost(c.requested, allowed, c.local); got != c.want {
			t.Errorf("%s: samePortRedirectHost(%q, %q) = %q, want %q", c.name, c.requested, c.local, got, c.want)
		}
	}
	// IPv6 literals are bracketed again on the way out.
	if got := samePortRedirectHost("[2001:db8::1]:8443", []string{"2001:db8::1"}, ""); got != "[2001:db8::1]:8443" {
		t.Errorf("IPv6 host = %q, want [2001:db8::1]:8443", got)
	}
}

func TestSeveralAdminsAdviceNamesTheCountAndTheRecoveryRoute(t *testing.T) {
	got := severalAdminsAdvice(3, "alice")
	for _, want := range []string{"3 admin accounts", "-recover-admin-account", `"alice"`, "Settings"} {
		if !strings.Contains(got, want) {
			t.Errorf("advice %q lacks %q", got, want)
		}
	}
}

func TestConfirmYesOnlyAcceptsAnExplicitYes(t *testing.T) {
	cases := map[string]bool{
		"y\n": true, "Y\n": true, "yes\n": true, "YES\n": true,
		"n\n": false, "no\n": false, "\n": false, "yep\n": false, "": false, // closed stdin is never consent
	}
	for in, want := range cases {
		withStdin(t, in)
		if got := confirmYes("Proceed?"); got != want {
			t.Errorf("confirmYes with input %q = %v, want %v", in, got, want)
		}
	}
}

func healthcheckConfig(t *testing.T, yaml string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIKROVIEW_CONFIG", p)
}

func TestRunHealthcheckReportsTheHealthEndpoint(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	var path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()
	healthcheckConfig(t, "tls:\n  enabled: false\nlisten:\n  http: \""+strings.TrimPrefix(srv.URL, "http://")+"\"\n")

	if code := runHealthcheck(); code != 0 {
		t.Fatalf("healthy server: runHealthcheck() = %d, want 0", code)
	}
	if got, _ := path.Load().(string); got != "/api/healthz" {
		t.Errorf("probed %q, want /api/healthz", got)
	}
	status.Store(http.StatusServiceUnavailable)
	if code := runHealthcheck(); code != 1 {
		t.Errorf("503 from the server: runHealthcheck() = %d, want 1", code)
	}
}

func TestRunHealthcheckAcceptsItsOwnSelfSignedCertificate(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	healthcheckConfig(t, "tls:\n  enabled: true\nlisten:\n  http: \""+strings.TrimPrefix(srv.URL, "https://")+"\"\n")
	if code := runHealthcheck(); code != 0 {
		t.Errorf("runHealthcheck() over self-signed TLS = %d, want 0", code)
	}
}

func TestRunHealthcheckFailsWhenNothingListensOrConfigIsBad(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // the port is now closed
	healthcheckConfig(t, "tls:\n  enabled: false\nlisten:\n  http: \""+addr+"\"\n")
	if code := runHealthcheck(); code != 1 {
		t.Errorf("nothing listening: runHealthcheck() = %d, want 1", code)
	}
	t.Setenv("MIKROVIEW_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	if code := runHealthcheck(); code != 1 {
		t.Errorf("unreadable config: runHealthcheck() = %d, want 1", code)
	}
}

// The ShippedDeps adapters are handed to detectors before every store
// exists; each must answer "nothing" rather than panic on a nil store.
func TestShippedDepsAdaptersAreSafeOverMissingStores(t *testing.T) {
	if (entityTagLookup{}).HasTag("host", "h1", "tag") {
		t.Error("entityTagLookup over no store reported a tag")
	}
	if l, c, ok := (blocklistLookup{}).MatchIP("192.0.2.1"); ok || l != "" || c != "" {
		t.Errorf("blocklistLookup over no list = %q %q %v", l, c, ok)
	}
	if m, cat, l := (netClassLookup{}).LookupClass("192.0.2.1"); m || cat != "" || l != "" {
		t.Errorf("netClassLookup over no classifier = %v %q %q", m, cat, l)
	}
}
