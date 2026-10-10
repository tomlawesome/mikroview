// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1202 review R18 (ruling 17, replacing withdrawn ruling 15): with
// auth.adminPasskey: required and a publicUrl that is an IP address, the
// rule cannot be met (passkeys need a domain name), and config validation
// refuses it (CFG-0023). That refusal is what starts setup-only mode, so
// setup-only mode treats the rule as optional only when no admin holds a
// passkey (an IP address can never have had one), and says so in its log.
// When an admin does hold a passkey it refuses, as in ruling 16.

import (
	"strings"
	"testing"
)

// r18Config is a config file that parses but asks for the passkey rule
// with an IP-address publicUrl (192.0.2.0/24 is the documentation range).
const r18Config = "publicUrl: https://192.0.2.10\nauth:\n  adminPasskey: required\n"

// #1202 review R18 (a): no admin holds a passkey, so setup-only mode
// starts, logs that the passkey rule is relaxed for setup-only mode, and
// the admin signs in with the password and a second factor.
func TestR18SetupOnlyWithUnmeetablePasskeyRuleAndNoPasskeyStartsAndLogsTheRelaxation(t *testing.T) {
	t.Parallel()
	f := newLegacyInstallSealed(t)
	a := mustStart(t, Opts{Root: f.root, RawConfig: r18Config, KeyFromEnv: true, KeyFile: f.keyPath})

	hz := a.client().get("/api/healthz")
	wantStatus(t, "healthz", hz, 200)
	if !strings.Contains(string(hz.Body), "setup-only") {
		t.Fatalf("healthz does not say setup-only: %s\n%s", hz.Body, a.log())
	}

	// One log line says the passkey rule is relaxed for setup-only mode.
	var lines []string
	for _, l := range strings.Split(a.log(), "\n") {
		low := strings.ToLower(l)
		if strings.Contains(low, "passkey") && strings.Contains(low, "setup-only") &&
			(strings.Contains(low, "relax") || strings.Contains(low, "optional")) {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		t.Errorf("want exactly one log line saying the passkey rule is relaxed for setup-only mode, got %d:\n%s", len(lines), a.log())
	}

	// A real sign-in: password, then an authenticator code.
	w := &world{a: a, admin: f.admin}
	c, r := w.signIn(t, f.admin, f.admin.code(t, a, 0))
	wantStatus(t, "admin sign-in (password and authenticator code) in setup-only mode", r, 200)
	if s := c.session(t); !s.Authenticated || s.Username != f.admin.name {
		t.Errorf("session = %+v, want the admin signed in", s)
	}
}

// #1202 review R18 (b): an admin holds a passkey, so the unmeetable rule
// is not relaxed: setup-only mode refuses to start and the message says
// to fix the file on disk.
func TestR18SetupOnlyWithUnmeetablePasskeyRuleAndAnAdminPasskeyRefusesAndSaysToFixTheFile(t *testing.T) {
	t.Parallel()
	f := newPasskeyInstall(t)
	code, out, _ := refused(t, Opts{Root: f.root, RawConfig: r18Config, KeyFromEnv: true, KeyFile: f.keyPath})
	if code == 0 {
		t.Errorf("exit status 0\n%s", out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "fix") || !strings.Contains(low, "on disk") {
		t.Errorf("the refusal does not say to fix the file on disk:\n%s", out)
	}
	if setupCodeRe.MatchString(out) {
		t.Errorf("a setup code was logged although start-up was refused:\n%s", out)
	}
}

// #1202 review R18 (c): the relaxation is for setup-only mode only. A
// normal start (here -validate-config, which runs the same validation)
// with the same setting is still refused with CFG-0023.
func TestR18UnmeetablePasskeyRuleIsStillRefusedWithCFG0023AtANormalStart(t *testing.T) {
	t.Parallel()
	code, out, _ := cli(t, Opts{AdminPasskey: "required", PublicURL: "https://192.0.2.10"}, "", "-validate-config")
	if code != 1 {
		t.Errorf("-validate-config exit %d, want 1\n%s", code, out)
	}
	if got := cfgCodeRe.FindString(out); got != "CFG-0023" {
		t.Errorf("CFG code = %q, want CFG-0023:\n%s", got, out)
	}
	if !strings.Contains(out, "adminPasskey") {
		t.Errorf("the problem does not name auth.adminPasskey:\n%s", out)
	}
}
