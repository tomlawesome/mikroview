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
	got := httpsRedirectTarget(r)
	want := "https://mikroview.local/api/events?x=1"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetHostWithNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://192.168.1.50/", nil)
	r.Host = "192.168.1.50"
	got := httpsRedirectTarget(r)
	want := "https://192.168.1.50/"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}

func TestHTTPSRedirectTargetPreservesPathAndQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://mikroview:80/ca.crt", nil)
	r.Host = "mikroview:80"
	got := httpsRedirectTarget(r)
	want := "https://mikroview/ca.crt"
	if got != want {
		t.Errorf("httpsRedirectTarget = %q, want %q", got, want)
	}
}
