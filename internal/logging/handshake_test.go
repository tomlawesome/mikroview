// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"strings"
	"testing"
	"time"
)

func TestParseHandshakeErrorDropsTheSourcePort(t *testing.T) {
	peer, raw, ok := parseHandshakeError(
		"http: TLS handshake error from 192.168.254.123:61400: remote error: tls: unknown certificate")
	if !ok {
		t.Fatal("did not recognise Go's own handshake-error format")
	}
	// The port changes on every retry, which is what made a phone's
	// reconnect loop read as a port scan.
	if peer != "192.168.254.123" {
		t.Errorf("peer = %q, want the address without its source port", peer)
	}
	if raw != "remote error: tls: unknown certificate" {
		t.Errorf("raw = %q, want the original error preserved", raw)
	}
}

func TestParseHandshakeErrorIgnoresOtherServerErrors(t *testing.T) {
	for _, line := range []string{
		"http: superfluous response.WriteHeader call from foo",
		"http: panic serving 10.0.0.1:5555: boom",
		"",
	} {
		if _, _, ok := parseHandshakeError(line); ok {
			t.Errorf("%q was treated as a handshake error", line)
		}
	}
}

func TestExplainHandshakeSaysWhoRejectedWhom(t *testing.T) {
	cases := []struct {
		raw       string
		wantParts []string
		cause     string
	}{
		{"remote error: tls: unknown certificate",
			[]string{"1.2.3.4 refused our certificate", "re-import /ca.crt"}, "refused-cert"},
		{"remote error: tls: bad certificate",
			[]string{"1.2.3.4 refused our certificate"}, "refused-cert"},
		{"tls: first record does not look like a TLS handshake",
			[]string{"spoke plain HTTP", "use https://"}, "plaintext"},
		{"tls: client offered only unsupported versions [301]",
			[]string{"we turned 1.2.3.4 away", "older than 1.2"}, "old-tls"},
		{"local error: tls: bad record MAC",
			[]string{"hung up during the handshake", "did not trust our certificate"}, "hung-up"},
		{"read tcp 1.2.3.4:1: read: connection reset by peer",
			[]string{"went away before finishing"}, "went-away"},
	}
	for _, c := range cases {
		got, cause := explainHandshake("1.2.3.4", c.raw)
		for _, want := range c.wantParts {
			if !strings.Contains(got, want) {
				t.Errorf("explainHandshake(%q) = %q, missing %q", c.raw, got, want)
			}
		}
		if cause != c.cause {
			t.Errorf("explainHandshake(%q) cause = %q, want %q", c.raw, cause, c.cause)
		}
	}
}

// An error nobody anticipated must not be dressed up as a diagnosis --
// the caller still logs the raw text, so the sentence above it has to
// claim only what is certain.
func TestExplainHandshakeDoesNotGuess(t *testing.T) {
	got, cause := explainHandshake("1.2.3.4", "tls: some brand new failure mode")
	if got != "TLS handshake with 1.2.3.4 failed" {
		t.Errorf("explanation = %q, want a claim-nothing fallback", got)
	}
	if cause != "other" {
		t.Errorf("cause = %q, want %q", cause, "other")
	}
}

func TestKeyedLimiterCollapsesARetryLoop(t *testing.T) {
	l := newKeyedLimiter(time.Hour)

	if n, ok := l.allow("phone|refused-cert"); !ok || n != 1 {
		t.Fatalf("first occurrence = (%d, %v), want (1, true)", n, ok)
	}
	for i := 0; i < 27; i++ {
		if _, ok := l.allow("phone|refused-cert"); ok {
			t.Fatal("a repeat inside the window was written")
		}
	}
	// A different peer, and the same peer failing differently, are both
	// genuinely new information.
	if _, ok := l.allow("laptop|refused-cert"); !ok {
		t.Error("a different peer was suppressed by the first peer's line")
	}
	if _, ok := l.allow("phone|old-tls"); !ok {
		t.Error("a different cause from the same peer was suppressed")
	}
}

func TestKeyedLimiterReportsWhatItSuppressed(t *testing.T) {
	l := newKeyedLimiter(time.Millisecond)
	l.allow("peer|cause")
	for i := 0; i < 5; i++ {
		l.allow("peer|cause")
	}
	time.Sleep(2 * time.Millisecond)
	n, ok := l.allow("peer|cause")
	if !ok {
		t.Fatal("suppressed past the window")
	}
	if n != 7 {
		t.Errorf("count = %d, want 7 -- the written line must account for the quiet ones", n)
	}
}

// The key holds a peer address, so it is chosen by whoever connects.
func TestKeyedLimiterKeySetIsBounded(t *testing.T) {
	l := newKeyedLimiter(time.Hour)
	for i := 0; i < maxHandshakeKeys*3; i++ {
		l.allow(string(rune(i%1114111)) + "|refused-cert")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) > maxHandshakeKeys {
		t.Errorf("key set grew to %d, cap is %d -- an unauthenticated client controls this", len(l.seen), maxHandshakeKeys)
	}
}
