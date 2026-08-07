package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Validate checks a loaded configuration and reports everything wrong
// with it, split into problems that stop startup and problems that don't.
//
// It is deliberately pure: no network calls, no filesystem writes, no
// directory creation, nothing that changes the thing being checked.
// -validate-config is expected to run in CI, and a checker that dials a
// production SMTP server or an OIDC discovery endpoint from a build
// agent is both a surprise and a finding in its own right. Anything
// needing the network belongs behind a separate explicit flag.
//
// For warnings it also applies the safe default in place, so the caller
// gets a configuration it can actually run with. Substituting a value
// the operator chose is only defensible because Problem.Applied reports
// exactly what was substituted and the admin UI surfaces it -- if that
// reporting is ever dropped, these rules must become fatal instead.
func (c *Config) Validate() Result {
	var r Result
	fatal := func(code, key, msg, fix string) {
		r.Fatal = append(r.Fatal, Problem{Code: code, Severity: SeverityFatal, Key: key, Message: msg, Remediation: fix})
	}
	warn := func(code, key, msg, applied, fix string) {
		r.Warnings = append(r.Warnings, Problem{Code: code, Severity: SeverityWarn, Key: key, Message: msg, Applied: applied, Remediation: fix})
	}

	c.validateListen(fatal)
	c.validateStore(fatal, warn)
	c.validateAuth(fatal)
	c.validateDevices(fatal)

	return r
}

type problemFunc func(code, key, msg, fix string)
type warnFunc func(code, key, msg, applied, fix string)

func (c *Config) validateListen(fatal problemFunc) {
	// An unparseable listen address means the server cannot bind and
	// would fail moments later anyway -- catching it here turns a
	// confusing runtime bind error into a named config key.
	for key, addr := range map[string]string{
		"listen.syslogUdp": c.Listen.SyslogUDP,
		"listen.syslogTcp": c.Listen.SyslogTCP,
		"listen.http":      c.Listen.HTTP,
	} {
		if addr == "" {
			fatal("CFG-0001", key, "is empty", "set an address such as \":8080\" or \"192.168.1.10:8080\"")
			continue
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			fatal("CFG-0002", key, fmt.Sprintf("%q is not a valid listen address", addr),
				"use host:port, or :port to listen on every interface")
		}
	}
	// httpRedirect is optional -- empty disables it entirely, which is a
	// documented, supported choice, not a mistake.
	if c.Listen.HTTPRedirect != "" {
		if _, _, err := net.SplitHostPort(c.Listen.HTTPRedirect); err != nil {
			fatal("CFG-0002", "listen.httpRedirect",
				fmt.Sprintf("%q is not a valid listen address", c.Listen.HTTPRedirect),
				"use host:port, or set it to \"\" to disable the redirect listener")
		}
	}

	// Already fatal at startup (see main.go), repeated here so
	// -validate-config catches it before a deploy rather than after.
	if _, err := ParseTrustedProxies(c.Listen.TrustedProxies); err != nil {
		fatal("CFG-0003", "listen.trustedProxies", err.Error(),
			"list each proxy as an IP or CIDR, or use \"private\" for a proxy on your LAN or docker network")
	}
}

func (c *Config) validateStore(fatal problemFunc, warn warnFunc) {
	// Retention and maxEvents both being positive is what makes
	// mikroview retain anything at all. Zero or negative isn't a tuning
	// choice, it's an empty dashboard the operator will read as "no
	// traffic" -- the exact silent failure this whole feature exists to
	// prevent. Clamped rather than refused so a typo doesn't cost the
	// operator their monitoring entirely.
	if c.Store.Retention <= 0 {
		was := c.Store.Retention
		c.Store.Retention = defaultRetention
		warn("CFG-0010", "store.retention",
			fmt.Sprintf("%s is not a usable retention window -- nothing would be kept", was),
			c.Store.Retention.String(),
			"set a positive duration such as 24h")
	}
	if c.Store.MaxEvents <= 0 {
		was := c.Store.MaxEvents
		c.Store.MaxEvents = defaultMaxEvents
		warn("CFG-0011", "store.maxEvents",
			fmt.Sprintf("%d is not a usable event limit -- nothing would be kept", was),
			fmt.Sprintf("%d", c.Store.MaxEvents),
			"set a positive number of events to hold in memory")
	}
	_ = fatal
}

func (c *Config) validateAuth(fatal problemFunc) {
	// A session that never idles out is a credential that never expires.
	if c.Auth.SessionTTL <= 0 {
		fatal("CFG-0020", "auth.sessionTTL",
			fmt.Sprintf("%s means sessions never expire", c.Auth.SessionTTL),
			"set a positive duration such as 24h")
	}
	// Serving TLS while issuing cookies without the Secure flag is a
	// contradiction, and a downgrade: the cookie becomes sendable over a
	// plain connection that this deployment has otherwise ruled out.
	if c.TLS.Enabled && !c.Auth.SecureCookie {
		fatal("CFG-0021", "auth.secureCookie",
			"is false while tls.enabled is true, so session cookies would be issued without the Secure flag",
			"set auth.secureCookie: true, or set tls.enabled: false if you really terminate TLS elsewhere")
	}
}

func (c *Config) validateDevices(fatal problemFunc) {
	seen := make(map[string]int, len(c.Devices))
	for i, d := range c.Devices {
		key := fmt.Sprintf("devices[%d].sourceIp", i)
		ip := strings.TrimSpace(d.SourceIP)
		if ip == "" {
			continue // unconfigured devices are discovered by their raw IP
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			fatal("CFG-0030", key, fmt.Sprintf("%q is not an IP address", d.SourceIP),
				"use the address the router sends syslog from, e.g. 192.168.1.1")
			continue
		}
		canonical := addr.Unmap().String()
		if prev, dup := seen[canonical]; dup {
			// A duplicate silently shadows the earlier device: events
			// land under whichever entry wins, and the other never
			// appears. Worse than a typo because everything looks fine.
			fatal("CFG-0031", key,
				fmt.Sprintf("%s is already used by devices[%d] (%q)", canonical, prev, c.Devices[prev].Name),
				"give each device a distinct sourceIp, or remove the duplicate entry")
			continue
		}
		seen[canonical] = i
	}
}

// defaultRetention/defaultMaxEvents are read from Default() rather than
// restated, so a clamp can never substitute something different from
// what a fresh install would have used. Restating them would be exactly
// the kind of quiet drift this whole feature exists to catch.
var (
	defaultRetention = defaults().Store.Retention
	defaultMaxEvents = defaults().Store.MaxEvents
)
