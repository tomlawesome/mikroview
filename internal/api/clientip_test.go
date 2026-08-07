// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

func serverWithProxies(t *testing.T, header string, entries ...string) *Server {
	t.Helper()
	prefixes, err := config.ParseTrustedProxies(entries)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%v): %v", entries, err)
	}
	return &Server{TrustedProxies: prefixes, ClientIPHeader: header}
}

func requestFrom(peer string, header string, values ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.RemoteAddr = peer
	for _, v := range values {
		r.Header.Add(header, v)
	}
	return r
}

// TestClientIPIgnoresForwardingHeadersByDefault pins the property that
// makes the per-IP login limiter worth having at all. If a forwarding
// header were honoured without an operator opting in, an attacker would
// get a fresh, empty rate-limit bucket for every request just by varying
// one header -- the limiter would still appear to work while enforcing
// nothing.
func TestClientIPIgnoresForwardingHeadersByDefault(t *testing.T) {
	s := &Server{} // no TrustedProxies: the out-of-the-box configuration

	for _, spoof := range []string{"1.2.3.4", "9.9.9.9", "203.0.113.77"} {
		r := requestFrom("198.51.100.5:44321", "X-Forwarded-For", spoof)
		if got := s.clientIP(r); got != "198.51.100.5" {
			t.Errorf("X-Forwarded-For: %s was honoured -- clientIP = %q, want the real peer %q",
				spoof, got, "198.51.100.5")
		}
	}
}

// TestClientIPSeparatesUsersBehindATrustedProxy is the other half: with
// the proxy declared, two different people behind it must land in two
// different rate-limit buckets. Without this, one attacker's failed
// logins exhaust the single shared bucket and lock out the entire
// deployment.
func TestClientIPSeparatesUsersBehindATrustedProxy(t *testing.T) {
	s := serverWithProxies(t, "", "10.0.0.0/8")

	attacker := s.clientIP(requestFrom("10.0.0.2:5555", "X-Forwarded-For", "203.0.113.9"))
	victim := s.clientIP(requestFrom("10.0.0.2:5556", "X-Forwarded-For", "198.51.100.20"))

	if attacker == victim {
		t.Fatalf("both clients keyed as %q -- they share one rate-limit bucket", attacker)
	}
	if attacker != "203.0.113.9" {
		t.Errorf("attacker keyed as %q, want %q", attacker, "203.0.113.9")
	}
	if victim != "198.51.100.20" {
		t.Errorf("victim keyed as %q, want %q", victim, "198.51.100.20")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		trusted []string
		header  string
		peer    string
		values  []string
		want    string
	}{
		{
			name:    "untrusted peer's header is ignored even when proxies are configured",
			trusted: []string{"10.0.0.0/8"},
			peer:    "203.0.113.50:1234",
			values:  []string{"1.2.3.4"},
			want:    "203.0.113.50",
		},
		{
			// The client forged two entries before the request reached the
			// proxy; the proxy then appended the address it actually saw.
			// Only that last entry is evidence of anything.
			name:    "forged entries to the left of the real one are not returned",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"1.2.3.4, 5.6.7.8, 198.51.100.7"},
			want:    "198.51.100.7",
		},
		{
			name:    "walks back through a chain of trusted hops",
			trusted: []string{"10.0.0.0/8", "192.168.0.0/16"},
			peer:    "10.0.0.1:9999",
			values:  []string{"198.51.100.7, 192.168.1.5, 10.0.0.9"},
			want:    "198.51.100.7",
		},
		{
			name:    "multiple header instances are one chain",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"1.2.3.4", "198.51.100.7, 10.0.0.9"},
			want:    "198.51.100.7",
		},
		{
			// Falling back to the peer keeps a deployment where every hop is
			// internal from collapsing onto an attacker-chosen value.
			name:    "all-trusted chain falls back to the peer",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"10.0.0.5, 10.0.0.9"},
			want:    "10.0.0.1",
		},
		{
			name:    "malformed rightmost entry falls back to the peer",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"198.51.100.7, not-an-ip"},
			want:    "10.0.0.1",
		},
		{
			name:    "absent header falls back to the peer",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			want:    "10.0.0.1",
		},
		{
			name:    "entries carrying a source port are accepted",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"198.51.100.7:51234"},
			want:    "198.51.100.7",
		},
		{
			name:    "bracketed IPv6 with a port is accepted",
			trusted: []string{"10.0.0.0/8"},
			peer:    "10.0.0.1:9999",
			values:  []string{"[2001:db8::42]:51234"},
			want:    "2001:db8::42",
		},
		{
			name:    "a single-value header works as a one-element chain",
			trusted: []string{"10.0.0.0/8"},
			header:  "CF-Connecting-IP",
			peer:    "10.0.0.1:9999",
			values:  []string{"198.51.100.7"},
			want:    "198.51.100.7",
		},
		{
			name:    "a bare IP entry means that host exactly",
			trusted: []string{"172.20.0.3"},
			peer:    "172.20.0.3:8080",
			values:  []string{"198.51.100.7"},
			want:    "198.51.100.7",
		},
		{
			name:    "a bare IP entry does not trust its neighbours",
			trusted: []string{"172.20.0.3"},
			peer:    "172.20.0.4:8080",
			values:  []string{"198.51.100.7"},
			want:    "172.20.0.4",
		},
		{
			name:    "the private shorthand covers a docker-network proxy",
			trusted: []string{"private"},
			peer:    "172.18.0.2:8080",
			values:  []string{"198.51.100.7"},
			want:    "198.51.100.7",
		},
		{
			name:    "the private shorthand does not cover a public peer",
			trusted: []string{"private"},
			peer:    "203.0.113.50:8080",
			values:  []string{"198.51.100.7"},
			want:    "203.0.113.50",
		},
		{
			// Otherwise the same client keys differently depending on
			// whether it arrived over v4 or v4-mapped-into-v6.
			name: "an IPv4-mapped peer normalises to its IPv4 form",
			peer: "[::ffff:198.51.100.5]:44321",
			want: "198.51.100.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := tt.header
			if header == "" {
				header = "X-Forwarded-For"
			}
			s := serverWithProxies(t, tt.header, tt.trusted...)
			got := s.clientIP(requestFrom(tt.peer, header, tt.values...))
			if got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTrustedProxiesRejectsGarbage(t *testing.T) {
	for _, entry := range []string{"not-an-ip", "10.0.0.0/99", "10.0.0.0/", "example.com"} {
		if _, err := config.ParseTrustedProxies([]string{entry}); err == nil {
			t.Errorf("ParseTrustedProxies(%q) accepted an unparseable entry", entry)
		}
	}
}

func TestParseTrustedProxiesSkipsBlankEntries(t *testing.T) {
	got, err := config.ParseTrustedProxies([]string{"", "  ", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d prefixes, want 1", len(got))
	}
}
