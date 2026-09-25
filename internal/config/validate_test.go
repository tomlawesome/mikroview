// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"strings"
	"testing"
	"time"
)

// validCfg is a configuration a real deployment could run.
func validCfg() Config {
	c := defaults()
	c.Auth.SessionTTL = 24 * time.Hour
	c.Auth.SecureCookie = true
	c.TLS.Enabled = true
	return c
}

func codes(ps []Problem) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

func has(ps []Problem, code string) *Problem {
	for i := range ps {
		if ps[i].Code == code {
			return &ps[i]
		}
	}
	return nil
}

// TestValidConfigProducesNothing is the guard against over-eager rules.
// A validator that flags a working deployment trains operators to ignore
// it, which costs more than the rules gain.
func TestValidConfigProducesNothing(t *testing.T) {
	c := validCfg()
	r := c.Validate()
	if r.HasProblems() {
		t.Errorf("a valid config produced fatal=%v warnings=%v", codes(r.Fatal), codes(r.Warnings))
	}
	if r.Err() != nil {
		t.Errorf("Err() = %v, want nil", r.Err())
	}
}

func TestFatalRules(t *testing.T) {
	tests := []struct {
		name string
		code string
		key  string
		mut  func(*Config)
	}{
		{"empty listen address", "CFG-0001", "listen.http", func(c *Config) { c.Listen.HTTP = "" }},
		{"unparseable listen address", "CFG-0002", "listen.http", func(c *Config) { c.Listen.HTTP = "not-an-address" }},
		{"unparseable redirect address", "CFG-0002", "listen.httpRedirect", func(c *Config) { c.Listen.HTTPRedirect = "nope" }},
		{"bad trusted proxy", "CFG-0003", "listen.trustedProxies", func(c *Config) { c.Listen.TrustedProxies = []string{"example.com"} }},
		{"session never expires", "CFG-0020", "auth.sessionTTL", func(c *Config) { c.Auth.SessionTTL = 0 }},
		{"insecure cookie under TLS", "CFG-0021", "auth.secureCookie", func(c *Config) { c.TLS.Enabled = true; c.Auth.SecureCookie = false }},
		{"device sourceIp not an IP", "CFG-0030", "devices[0].sourceIp", func(c *Config) {
			c.Devices = []Device{{ID: "r1", SourceIP: "router.local"}}
		}},
		{"duplicate device sourceIp", "CFG-0031", "devices[1].sourceIp", func(c *Config) {
			c.Devices = []Device{{ID: "a", Name: "A", SourceIP: "192.168.1.1"}, {ID: "b", Name: "B", SourceIP: "192.168.1.1"}}
		}},

		{"duplicate device id", "CFG-0032", "devices[1].id", func(c *Config) {
			c.Devices = []Device{
				{ID: "shared", Name: "a", SourceIP: "192.168.1.1"},
				{ID: "shared", Name: "b", SourceIP: "192.168.2.1"},
			}
		}},
		{"device id is another router's address", "CFG-0033", "devices[0].id", func(c *Config) {
			c.Devices = []Device{{ID: "10.0.0.9", Name: "a", SourceIP: "192.168.1.1"}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validCfg()
			tt.mut(&c)
			r := c.Validate()

			p := has(r.Fatal, tt.code)
			if p == nil {
				t.Fatalf("expected fatal %s, got fatal=%v warnings=%v", tt.code, codes(r.Fatal), codes(r.Warnings))
			}
			if p.Key != tt.key {
				t.Errorf("Key = %q, want %q -- the operator has to be able to find the setting", p.Key, tt.key)
			}
			if p.Remediation == "" {
				t.Error("no remediation: a fatal error that doesn't say what to do is a support ticket")
			}
			if r.Err() == nil {
				t.Error("Err() returned nil despite a fatal problem")
			}
		})
	}
}

// A duplicate written differently must still be caught -- otherwise the
// shadowing bug survives by being spelled unusually.
func TestDuplicateDeviceIPIsCanonicalised(t *testing.T) {
	c := validCfg()
	c.Devices = []Device{
		{ID: "a", Name: "A", SourceIP: "192.168.1.1"},
		{ID: "b", Name: "B", SourceIP: " ::ffff:192.168.1.1 "},
	}
	if has(c.Validate().Fatal, "CFG-0031") == nil {
		t.Error("an IPv4-mapped duplicate was not detected")
	}
}

// TestWarningsClampRatherThanRefuse pins the tier split: these must not
// stop startup, and must leave a usable value behind.
func TestWarningsClampRatherThanRefuse(t *testing.T) {
	for _, tt := range []struct {
		name string
		code string
		mut  func(*Config)
		want func(*Config) bool
	}{
		{"negative retention", "CFG-0010",
			func(c *Config) { c.Store.Retention = -1 },
			func(c *Config) bool { return c.Store.Retention == defaults().Store.Retention }},
		{"zero retention", "CFG-0010",
			func(c *Config) { c.Store.Retention = 0 },
			func(c *Config) bool { return c.Store.Retention > 0 }},
		{"zero maxMemory", "CFG-0011",
			func(c *Config) { c.Store.MaxMemory = 0 },
			func(c *Config) bool { return c.Store.MaxMemory == defaults().Store.MaxMemory }},
		{"empty matchLogPath", "CFG-0040",
			func(c *Config) { c.Watchlist.MatchLogPath = "" },
			func(c *Config) bool { return c.Watchlist.MatchLogPath == defaults().Watchlist.MatchLogPath }},
		{"zero matchLogCapacity", "CFG-0041",
			func(c *Config) { c.Watchlist.MatchLogCapacity = 0 },
			func(c *Config) bool {
				return c.Watchlist.MatchLogCapacity == defaults().Watchlist.MatchLogCapacity
			}},
		{"zero matchLogRetention", "CFG-0042",
			func(c *Config) { c.Watchlist.MatchLogRetention = 0 },
			func(c *Config) bool {
				return c.Watchlist.MatchLogRetention == defaults().Watchlist.MatchLogRetention
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := validCfg()
			tt.mut(&c)
			r := c.Validate()

			if len(r.Fatal) != 0 {
				t.Errorf("refused to start over a clampable value: %v", codes(r.Fatal))
			}
			p := has(r.Warnings, tt.code)
			if p == nil {
				t.Fatalf("expected warning %s, got %v", tt.code, codes(r.Warnings))
			}
			if p.Applied == "" {
				t.Error("no Applied value: clamping is only defensible if the substitution is reported")
			}
			if !tt.want(&c) {
				t.Error("the safe default was not actually applied to the config")
			}
		})
	}
}

// TestHighMaxMemoryWarnsWithoutClamping pins the other tier CFG-0012
// introduces: unlike CFG-0010/CFG-0011 above, a deliberately large
// store.maxMemory is a legitimate operator choice on a machine that has
// the memory to spare, so it must warn -- surfacing the real cost -- and
// then leave the configured value completely alone, not silently
// substitute something smaller.
func TestHighMaxMemoryWarnsWithoutClamping(t *testing.T) {
	c := validCfg()
	const big ByteSize = 2 << 30 // 2GiB, above highMaxMemoryWarnThreshold
	c.Store.MaxMemory = big

	r := c.Validate()

	if len(r.Fatal) != 0 {
		t.Errorf("a large maxMemory must not be fatal: %v", codes(r.Fatal))
	}
	p := has(r.Warnings, "CFG-0012")
	if p == nil {
		t.Fatalf("expected warning CFG-0012, got %v", codes(r.Warnings))
	}
	if p.Applied != "" {
		t.Errorf("CFG-0012 must not report a substitution, got Applied=%q", p.Applied)
	}
	if c.Store.MaxMemory != big {
		t.Errorf("the configured value was clamped to %s, want it left at %s", c.Store.MaxMemory, big)
	}
}

// A merely generous store.maxMemory (comfortably under the threshold)
// must not trip CFG-0012 -- otherwise every operator who reads the
// warning's own advice ("confirm this machine has it to spare") on a
// smaller box and dials it up a bit lands right back in a warning.
func TestModeratelyLargeMaxMemoryDoesNotWarn(t *testing.T) {
	c := validCfg()
	c.Store.MaxMemory = 500 * 1024 * 1024 // 500MiB, below the 1GiB threshold

	r := c.Validate()

	if p := has(r.Warnings, "CFG-0012"); p != nil {
		t.Errorf("500MiB should not trip the high-maxMemory warning")
	}
}

// TestValidationNeverEchoesSecrets: a -validate-config run is exactly
// the thing an operator pastes into an issue or a forum post.
func TestValidationNeverEchoesSecrets(t *testing.T) {
	const canary = "CANARY-d34db33f"

	c := validCfg()
	c.OIDC.ClientSecret = canary
	c.Notify.SMTP.Password = canary
	c.Reputation.AbuseIPDBKey = canary
	c.Notify.Pushover.Token = canary
	// Break things so every rule fires and has something to say.
	c.Listen.HTTP = ""
	c.Auth.SessionTTL = 0
	c.Store.Retention = -1
	c.Devices = []Device{{ID: "a", SourceIP: "nope"}}

	r := c.Validate()
	var sb strings.Builder
	for _, p := range append(append([]Problem{}, r.Fatal...), r.Warnings...) {
		sb.WriteString(p.String())
		sb.WriteString("\n")
	}
	if r.Err() != nil {
		sb.WriteString(r.Err().Error())
	}

	if strings.Contains(sb.String(), canary) {
		t.Errorf("a secret value reached validation output:\n%s", sb.String())
	}
}

// The example config is what operators copy. If it doesn't validate
// clean, the documentation has drifted from the code.
func TestExampleConfigValidatesClean(t *testing.T) {
	c, err := Load("../../deploy/config.example.yaml", nil)
	if err != nil {
		t.Skipf("example config not loadable here: %v", err)
	}
	r := c.Validate()
	if len(r.Fatal) > 0 {
		t.Errorf("deploy/config.example.yaml has fatal problems: %v", codes(r.Fatal))
	}
}

// An id-less device takes its sourceIp as its identity, so two of them
// do not collapse into one shared "" identity -- and declaring a router
// that was already being discovered keeps the id its events, pushed
// state and ingest tokens already use.
func TestDeviceIDDefaultsToSourceIP(t *testing.T) {
	c := &Config{Devices: []Device{
		{Name: "edge", SourceIP: "192.168.1.1"},
		{Name: "branch", SourceIP: "::ffff:192.168.2.1"},
		{ID: "explicit", Name: "third", SourceIP: "192.168.3.1"},
	}}
	c.normaliseDevices()

	want := []string{"192.168.1.1", "192.168.2.1", "explicit"}
	for i, w := range want {
		if c.Devices[i].ID != w {
			t.Errorf("Devices[%d].ID = %q, want %q", i, c.Devices[i].ID, w)
		}
	}
	if p := has(c.Validate().Fatal, "CFG-0032"); p != nil {
		t.Errorf("id-less devices with distinct sourceIps collided: %+v", p)
	}
}

// TestPublicURLAbsent is the baseline: unset is the default, and an
// install reached only by IP keeps working exactly as it does today --
// no warning, nothing fatal, publicUrl stays empty.
func TestPublicURLAbsent(t *testing.T) {
	c := validCfg()
	r := c.Validate()

	for _, code := range []string{"CFG-0100", "CFG-0101", "CFG-0102", "CFG-0103", "CFG-0104"} {
		if p := has(r.Warnings, code); p != nil {
			t.Errorf("unset publicUrl must not warn, got %s: %+v", code, p)
		}
	}
	if c.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty", c.PublicURL)
	}
}

// TestPublicURLHostname is a working value: no warning of any kind, and
// the value passes through Validate untouched.
func TestPublicURLHostname(t *testing.T) {
	c := validCfg()
	c.PublicURL = "https://mikroview.home.lan:8443"
	r := c.Validate()

	if len(r.Fatal) != 0 {
		t.Errorf("a valid publicUrl must never be fatal: %v", codes(r.Fatal))
	}
	for _, code := range []string{"CFG-0100", "CFG-0101", "CFG-0102", "CFG-0103"} {
		if p := has(r.Warnings, code); p != nil {
			t.Errorf("a working hostname URL must not trip %s: %+v", code, p)
		}
	}
	if c.PublicURL != "https://mikroview.home.lan:8443" {
		t.Errorf("PublicURL was rewritten to %q, want it left alone", c.PublicURL)
	}
}

// TestPublicURLIPAddress: WebAuthn refuses outright to bind a passkey to
// an IP literal (that is a browser rule, not MikroView's), so this warns
// and degrades -- CFG-0101 -- rather than refusing to start, and the
// configured value is left in place for internal/api's RP construction
// to read the same way CFG-0060 leaves oidc.publicBaseUrl alone.
func TestPublicURLIPAddress(t *testing.T) {
	c := validCfg()
	c.PublicURL = "https://192.168.1.10:8443"
	r := c.Validate()

	if len(r.Fatal) != 0 {
		t.Errorf("an IP-literal publicUrl must never be fatal: %v", codes(r.Fatal))
	}
	p := has(r.Warnings, "CFG-0101")
	if p == nil {
		t.Fatalf("expected warning CFG-0101, got %v", codes(r.Warnings))
	}
	if p.Applied == "" {
		t.Error("no Applied value: the operator needs to know passkeys are unavailable")
	}
	if c.PublicURL != "https://192.168.1.10:8443" {
		t.Errorf("PublicURL was changed to %q, want it left alone -- nothing is substituted for CFG-0101", c.PublicURL)
	}
}

// TestPublicURLMalformed: a value that does not even parse as an
// absolute URL cannot yield a host to reason about at all, so it is
// treated as though publicUrl were never set (CFG-0100) rather than left
// for a later check to trip over.
func TestPublicURLMalformed(t *testing.T) {
	for _, bad := range []string{"not-a-url", "mikroview.home.lan", "https://"} {
		t.Run(bad, func(t *testing.T) {
			c := validCfg()
			c.PublicURL = bad
			r := c.Validate()

			if len(r.Fatal) != 0 {
				t.Errorf("a malformed publicUrl must never be fatal: %v", codes(r.Fatal))
			}
			p := has(r.Warnings, "CFG-0100")
			if p == nil {
				t.Fatalf("expected warning CFG-0100 for %q, got %v", bad, codes(r.Warnings))
			}
			if c.PublicURL != "" {
				t.Errorf("PublicURL = %q, want cleared to empty", c.PublicURL)
			}
		})
	}
}

// TestPublicURLInsecureScheme: browsers only offer passkeys over https,
// so a plain http:// value degrades the same way an IP literal does --
// except for localhost, which is exempt because a browser's own
// same-origin secure-context rules already treat it as secure.
func TestPublicURLInsecureScheme(t *testing.T) {
	c := validCfg()
	c.PublicURL = "http://mikroview.home.lan:8080"
	r := c.Validate()

	p := has(r.Warnings, "CFG-0102")
	if p == nil {
		t.Fatalf("expected warning CFG-0102, got %v", codes(r.Warnings))
	}
	if c.PublicURL != "http://mikroview.home.lan:8080" {
		t.Errorf("PublicURL was changed to %q, want it left alone", c.PublicURL)
	}
}

// TestPublicURLNonHTTPSchemeAlsoWarns pins the agreement between this
// check and NewRelyingParty in internal/api, which is where the two
// were briefly out of step: that side treats every non-https scheme as
// insecure and switches passkeys off, so a scheme this check stayed
// silent about would leave the operator with no warning and no
// passkeys -- the silent failure the design rules out. CFG-0102's
// wording names http because that is the realistic typo; the condition
// deliberately covers more than its wording.
func TestPublicURLNonHTTPSchemeAlsoWarns(t *testing.T) {
	for _, raw := range []string{
		"ftp://mikroview.home.lan:8080",
		"ws://mikroview.home.lan:8080",
	} {
		c := validCfg()
		c.PublicURL = raw
		r := c.Validate()

		if p := has(r.Warnings, "CFG-0102"); p == nil {
			t.Errorf("%s did not trip CFG-0102, so passkeys would switch off with nothing said: got %v",
				raw, codes(r.Warnings))
		}
	}
}

func TestPublicURLHTTPLocalhostExempt(t *testing.T) {
	c := validCfg()
	c.PublicURL = "http://localhost:8080"
	r := c.Validate()

	if p := has(r.Warnings, "CFG-0102"); p != nil {
		t.Errorf("http://localhost must not trip CFG-0102: %+v", p)
	}
}

// TestPublicURLPathStripped: a path/query/fragment is not a reason to
// lose passkeys -- WebAuthn only cares about the origin -- so this warns
// and strips (CFG-0103) rather than degrading availability the way
// CFG-0101/CFG-0102 do.
func TestPublicURLPathStripped(t *testing.T) {
	c := validCfg()
	c.PublicURL = "https://mikroview.home.lan:8443/some/path?x=1#frag"
	r := c.Validate()

	p := has(r.Warnings, "CFG-0103")
	if p == nil {
		t.Fatalf("expected warning CFG-0103, got %v", codes(r.Warnings))
	}
	const want = "https://mikroview.home.lan:8443"
	if c.PublicURL != want {
		t.Errorf("PublicURL = %q, want stripped to %q", c.PublicURL, want)
	}
	if p.Applied != want {
		t.Errorf("Applied = %q, want %q", p.Applied, want)
	}
	if pIP := has(r.Warnings, "CFG-0101"); pIP != nil {
		t.Errorf("a hostname with a path must not also trip CFG-0101: %+v", pIP)
	}
}

// A bare trailing slash carries no real path, so it must not trip
// CFG-0103 -- otherwise the most natural way to type the setting
// ("https://host/") would warn on every single boot.
func TestPublicURLBareTrailingSlashNotStripped(t *testing.T) {
	c := validCfg()
	c.PublicURL = "https://mikroview.home.lan:8443/"
	r := c.Validate()

	if p := has(r.Warnings, "CFG-0103"); p != nil {
		t.Errorf("a bare trailing slash must not trip CFG-0103: %+v", p)
	}
}

// TestPublicURLSuggestedFromOIDC: publicUrl never falls back to
// oidc.publicBaseUrl (that is the point -- see PublicURL's doc comment),
// but leaving publicUrl unset while oidc.publicBaseUrl is set is worth a
// nudge, since the two usually name the same address.
func TestPublicURLSuggestedFromOIDC(t *testing.T) {
	c := validCfg()
	c.OIDC.IssuerURL = "https://id.example.com"
	c.OIDC.PublicBaseURL = "https://mikroview.example.com"
	c.OIDC.ClientID = "mikroview"
	c.OIDC.ClientSecret = "secret"
	r := c.Validate()

	if has(r.Warnings, "CFG-0104") == nil {
		t.Fatalf("expected warning CFG-0104, got %v", codes(r.Warnings))
	}
	if c.PublicURL != "" {
		t.Errorf("PublicURL was set to %q -- CFG-0104 must only nudge, never substitute (no fallback to oidc.publicBaseUrl)", c.PublicURL)
	}
}

// The other half of the no-fallback rule: publicUrl actually set to
// something different from oidc.publicBaseUrl must be left exactly as
// configured -- CFG-0104 only fires when publicUrl is unset.
func TestPublicURLNotOverriddenByOIDC(t *testing.T) {
	c := validCfg()
	c.OIDC.IssuerURL = "https://id.example.com"
	c.OIDC.PublicBaseURL = "https://mikroview.example.com"
	c.OIDC.ClientID = "mikroview"
	c.OIDC.ClientSecret = "secret"
	c.PublicURL = "https://mikroview.home.lan:8443"
	r := c.Validate()

	if p := has(r.Warnings, "CFG-0104"); p != nil {
		t.Errorf("CFG-0104 must not fire when publicUrl is set: %+v", p)
	}
	if c.PublicURL != "https://mikroview.home.lan:8443" {
		t.Errorf("PublicURL = %q, want left exactly as configured", c.PublicURL)
	}
}
