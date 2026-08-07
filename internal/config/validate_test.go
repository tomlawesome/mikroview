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
		{"unparseable listen address", "CFG-0002", "listen.syslogUdp", func(c *Config) { c.Listen.SyslogUDP = "not-an-address" }},
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
		{"zero maxEvents", "CFG-0011",
			func(c *Config) { c.Store.MaxEvents = 0 },
			func(c *Config) bool { return c.Store.MaxEvents == defaults().Store.MaxEvents }},
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
