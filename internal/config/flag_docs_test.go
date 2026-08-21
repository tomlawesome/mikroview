// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The -syslog-tls flag's help text must not claim that tls.enabled gates
// the listener, because it does not.
//
// main.go starts the syslog TLS listener on cfg.Listen.SyslogTLS != ""
// alone -- there is no reference to cfg.TLS.Enabled in that condition,
// and the comment above it says the independence is deliberate (#188/
// #189: a deployment whose reverse proxy terminates HTTP TLS still needs
// this listener, because RouterOS connects to it directly and never
// through the proxy). The Listen.SyslogTLS doc comment in this file says
// the same thing, as does docs/configuration.md's TLS section.
//
// The help text said the opposite for long enough to reach a release
// (#382). Two ways that misleads, and the second is the one that matters:
// an operator who wants no syslog listener at all sets tls.enabled: false
// believing that closes it, and the port -- which authenticates no sender
// -- keeps listening.
//
// Pinned against the source rather than against a rendered -h, because
// the flag set writes its usage to stderr and Load owns it; the literal
// is the thing that goes stale either way.
func TestSyslogTLSFlagHelpDoesNotClaimTLSEnabledGatesIt(t *testing.T) {
	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("reading config.go: %v", err)
	}

	m := regexp.MustCompile(`fs\.String\("syslog-tls",[^,]+,\s*"([^"]*)"\)`).FindSubmatch(source)
	if m == nil {
		t.Fatal("found no -syslog-tls flag declaration in config.go -- this test is not looking where it thinks it is")
	}
	help := string(m[1])

	if strings.Contains(help, "tls.enabled is true") {
		t.Errorf("the -syslog-tls help says the listener is gated on tls.enabled, which main.go does not do: %q", help)
	}
	if !strings.Contains(help, "independently of tls.enabled") {
		t.Errorf("the -syslog-tls help should state that the listener runs independently of tls.enabled, so an operator turning tls.enabled off knows the port stays open: %q", help)
	}
}
