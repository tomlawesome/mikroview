// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/tomlawesome/mikroview/internal/oidc"
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
		r.Fatal = append(r.Fatal, Problem{Code: code, Severity: SeverityFatal, Key: key, Message: msg,
			Remediation: fix, Example: examplesByCode[code], Docs: docsAnchor(code)})
	}
	warn := func(code, key, msg, applied, fix string) {
		r.Warnings = append(r.Warnings, Problem{Code: code, Severity: SeverityWarn, Key: key, Message: msg,
			Applied: applied, Remediation: fix, Example: examplesByCode[code], Docs: docsAnchor(code)})
	}

	c.validateListen(fatal)
	c.validateStore(fatal, warn)
	c.validateWatchlist(warn)
	c.validateAuth(fatal)
	c.validateDevices(fatal)
	c.validateNotify(warn)
	c.validateOIDC(warn)

	return r
}

type problemFunc func(code, key, msg, fix string)
type warnFunc func(code, key, msg, applied, fix string)

// docsAnchor deep-links a code into the configuration reference. The
// anchor is the lower-cased code, which is what GitHub generates for a
// heading of the form "### CFG-0021: ...".
func docsAnchor(code string) string {
	return DocsURL + "#" + strings.ToLower(code)
}

// examplesByCode holds a ready-to-paste snippet per problem code.
//
// Kept here, in one block, rather than passed at each call site. Two
// reasons: the call sites already carry four arguments and a fifth
// multi-line string would bury them, and having every snippet in one
// place is what makes it practical to keep them in step with the
// matching section of docs/configuration.md.
//
// Each shows the *corrected* setting with its full nesting, because the
// question an operator actually has is "where does this key go", and
// prose like "set a positive duration" does not answer it.
var examplesByCode = map[string]string{
	"CFG-0001": `listen:
  http: ":8080"`,

	"CFG-0002": `listen:
  http: ":8080"              # every interface, port 8080
  # http: "192.168.1.10:8080"  # one interface only
  httpRedirect: ""           # "" disables the redirect listener`,

	"CFG-0003": `listen:
  # each proxy as an IP or CIDR ...
  trustedProxies: ["192.168.1.5", "10.0.0.0/8"]
  # ... or the shorthand for a proxy on your LAN or docker network
  # trustedProxies: ["private"]`,

	"CFG-0010": `store:
  retention: 24h`,

	"CFG-0011": `store:
  maxMemory: 120MiB`,

	"CFG-0012": `store:
  maxMemory: 120MiB  # lower this, or confirm the machine has the larger amount to spare`,

	"CFG-0040": `watchlist:
  matchLogPath: /var/lib/mikroview/matchlog.jsonl`,

	"CFG-0041": `watchlist:
  matchLogCapacity: 200000`,

	"CFG-0042": `watchlist:
  matchLogRetention: 168h  # 7 days`,

	"CFG-0020": `auth:
  sessionTTL: 24h`,

	"CFG-0021": `# Serving TLS yourself -- the usual case:
tls:
  enabled: true
auth:
  secureCookie: true

# Or, only if a reverse proxy terminates TLS and mikroview's own
# listener is never reachable from the LAN:
# tls:
#   enabled: false
# auth:
#   secureCookie: false`,

	"CFG-0030": `devices:
  - sourceIp: "192.168.1.1"   # the address the router sends syslog from
    name: "edge-router"`,

	"CFG-0031": `devices:
  - sourceIp: "192.168.1.1"
    name: "edge-router"
  - sourceIp: "192.168.2.1"   # must differ from every other sourceIp
    name: "branch-router"`,

	"CFG-0032": `devices:
  - sourceIp: "192.168.1.1"
    id: "edge-router"       # the device's identity: events, pushed
    name: "Edge"            # router state, and ingest-token scope
  - sourceIp: "192.168.2.1"
    id: "branch-router"     # must differ from every other id
    name: "Branch"`,

	"CFG-0033": `devices:
  - sourceIp: "192.168.1.1"
    id: "edge-router"       # a name, or this device's own sourceIp --
    name: "Edge"            # another router's address would merge the two`,

	"CFG-0050": `notify:
  webhook:
    url: "https://ntfy.example.com/mikroview"   # https, so the header below is not sent in the clear
    headers:
      Authorization: "Bearer <token>"`,

	"CFG-0051": `notify:
  webhook:
    url: "https://ntfy.example.com/mikroview"`,
	"CFG-0060": `oidc:
  issuerUrl: "https://id.example.com"
  publicBaseUrl: "https://mikroview.example.com"`,
	"CFG-0061": `oidc:
  clientId: "mikroview"
  clientSecret: "<from your provider>"`,
	"CFG-0062": `oidc:
  # a self-hosted provider, not a multi-tenant one
  issuerUrl: "https://id.example.com"`,
}

func (c *Config) validateListen(fatal problemFunc) {
	// An unparseable listen address means the server cannot bind and
	// would fail moments later anyway -- catching it here turns a
	// confusing runtime bind error into a named config key.
	for key, addr := range map[string]string{
		"listen.http": c.Listen.HTTP,
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

// highMaxMemoryWarnThreshold is where store.maxMemory stops being a
// routine tuning choice and starts being worth a second look: the ring
// is reserved in full at startup (store.New's make([]Event, capacity)),
// so a value this large fails immediately on a machine that cannot
// spare it rather than degrading gradually. Not clamped -- a
// deliberately large budget on a machine that genuinely has the memory
// is a legitimate choice the operator gets to make, per the discussion
// on #244; this only makes sure they are making it with the actual cost
// in front of them, since the failure mode otherwise is silent right up
// until the process fails to start.
const highMaxMemoryWarnThreshold ByteSize = 1 << 30 // 1GiB

func (c *Config) validateStore(fatal problemFunc, warn warnFunc) {
	// Retention and maxMemory both being positive is what makes
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
	if c.Store.MaxMemory <= 0 {
		was := c.Store.MaxMemory
		c.Store.MaxMemory = defaultMaxMemory
		warn("CFG-0011", "store.maxMemory",
			fmt.Sprintf("%s is not a usable memory budget -- nothing would be kept", was),
			c.Store.MaxMemory.String(),
			"set a positive amount such as 120MiB")
	} else if c.Store.MaxMemory > highMaxMemoryWarnThreshold {
		// No Applied -- nothing is substituted, this only surfaces the
		// cost. See highMaxMemoryWarnThreshold's doc comment for why a
		// warning rather than a clamp.
		resident := ByteSize(float64(c.Store.MaxMemory) * ResidentPerRingByte) // measured ring-to-resident overhead, see #244
		warn("CFG-0012", "store.maxMemory",
			fmt.Sprintf("%s reserves up to %d events at startup (~%s resident once the Go runtime and process overhead are counted, not just the ring itself) -- confirm this machine has it to spare",
				c.Store.MaxMemory, c.Store.Capacity(), resident),
			"", "lower store.maxMemory if this wasn't a deliberate choice")
	}
	_ = fatal
}

func (c *Config) validateWatchlist(warn warnFunc) {
	// Unlike every other store's StorePath in this file,
	// watchlist.matchLogPath has no in-memory-only mode -- durability is
	// the entire reason internal/matchlog exists (#243 section 3's "a
	// match must survive a restart" requirement), so an empty path is
	// treated the same as an unusable value, not an opt-out.
	if c.Watchlist.MatchLogPath == "" {
		c.Watchlist.MatchLogPath = defaultMatchLogPath
		warn("CFG-0040", "watchlist.matchLogPath",
			"is empty, which internal/matchlog has no in-memory-only mode for -- matches would have nowhere to be recorded",
			c.Watchlist.MatchLogPath,
			"set a path, or leave this unset to use the default")
	}
	if c.Watchlist.MatchLogCapacity <= 0 {
		was := c.Watchlist.MatchLogCapacity
		c.Watchlist.MatchLogCapacity = defaultMatchLogCapacity
		warn("CFG-0041", "watchlist.matchLogCapacity",
			fmt.Sprintf("%d is not a usable match log capacity -- nothing would be kept", was),
			fmt.Sprintf("%d", c.Watchlist.MatchLogCapacity),
			"set a positive number of matches to hold, e.g. 200000")
	}
	// MatchLogRetention only takes effect on the Postgres backend (#243
	// section 3: "pragmatically unlimited" record count there, bounded by
	// age instead) -- validated unconditionally anyway, the same way
	// MatchLogCapacity above is validated even though only the file
	// backend enforces it, so a config that later adopts Postgres doesn't
	// discover a bad value for the first time at that point.
	if c.Watchlist.MatchLogRetention <= 0 {
		was := c.Watchlist.MatchLogRetention
		c.Watchlist.MatchLogRetention = defaultMatchLogRetention
		warn("CFG-0042", "watchlist.matchLogRetention",
			fmt.Sprintf("%s is not a usable retention window -- on Postgres, nothing would be kept", was),
			c.Watchlist.MatchLogRetention.String(),
			"set a positive duration such as 168h (7 days)")
	}
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

// validateNotify warns about a webhook that would ship this
// deployment's flag data, and whatever credential is configured to reach
// the receiver, in cleartext.
//
// A warning rather than a fatal: notify.webhook.url legitimately points
// at something on the operator's own LAN (a self-hosted ntfy, Home
// Assistant), where plain HTTP is a considered choice rather than a
// mistake. What it must not be is an unnoticed one -- notify.webhook.
// headers exists precisely to carry a credential, and config.go's own
// documentation steers operators towards putting one there. See #285.
func (c *Config) validateNotify(warn warnFunc) {
	url := strings.TrimSpace(c.Notify.Webhook.URL)
	if url == "" || !strings.HasPrefix(strings.ToLower(url), "http://") {
		return
	}
	if len(c.Notify.Webhook.Headers) > 0 {
		warn("CFG-0050", "notify.webhook.url",
			"is a plain http:// URL while notify.webhook.headers is set, so the credential in those headers and every flag's contents cross the network in cleartext",
			"sending anyway",
			"use an https:// URL, or accept this deliberately if the receiver is on a network you control end to end")
		return
	}
	warn("CFG-0051", "notify.webhook.url",
		"is a plain http:// URL, so every flag's contents -- source addresses, rule labels and detector detail -- cross the network in cleartext",
		"sending anyway",
		"use an https:// URL, or accept this deliberately if the receiver is on a network you control end to end")
}

// validateOIDC mirrors, at config-check time, the four conditions
// main.go checks at startup before wiring SSO.
//
// -validate-config is documented as deliberately stricter than the
// server, and it performed no OIDC validation at all (#267 finding 14):
// a block missing publicBaseUrl, or clientId/clientSecret, or pointed at
// a provider mikroview refuses outright, passed cleanly. The server then
// logs an error and leaves SSO off -- so the operator's first sign that
// their config is wrong is that the SSO button is not there.
//
// Warnings, not fatals, and that is the point of the split. The server
// deliberately fails soft here -- a half-configured SSO block leaves SSO
// off and local login working, because taking a running deployment down
// over an optional integration would be the worse outcome. Making these
// fatal would do exactly that. -validate-config exits non-zero on
// warnings too (see runValidateConfig), so a pipeline still fails, which
// is where "stricter than the server" actually lives.
//
// Applied says what the operator gets rather than naming a substituted
// value: nothing is filled in on their behalf, and pretending otherwise
// would be worse than saying so.
//
// The multi-tenant issuer check calls oidc.AllowIssuer rather than
// keeping its own copy of the list. This package is otherwise a
// dependency-free leaf, and the one exception is deliberate: the
// alternative is a second copy of a security allowlist that would drift
// from the first, which is worse than the import.
func (c *Config) validateOIDC(warn warnFunc) {
	if c.OIDC.IssuerURL == "" {
		// Not configured, which is the default and entirely fine.
		return
	}
	if c.OIDC.PublicBaseURL == "" {
		warn("CFG-0060", "oidc.publicBaseUrl",
			"is empty while oidc.issuerUrl is set, so mikroview cannot build the redirect URI",
			"SSO login disabled; local login unaffected",
			"set oidc.publicBaseUrl to the URL your users reach mikroview on, exactly as registered with the provider")
	}
	if c.OIDC.ClientID == "" || c.OIDC.ClientSecret == "" {
		warn("CFG-0061", "oidc.clientId",
			"oidc.clientId and/or oidc.clientSecret are empty while oidc.issuerUrl is set",
			"SSO login disabled; local login unaffected",
			"set both from the client your provider issued, or remove oidc.issuerUrl to turn SSO off deliberately")
	}
	if err := oidc.AllowIssuer(c.OIDC.IssuerURL); err != nil {
		warn("CFG-0062", "oidc.issuerUrl",
			"names a multi-tenant provider, which mikroview does not support",
			"SSO login disabled; local login unaffected",
			"use a self-hosted provider (Authentik, Keycloak, Zitadel) or a single-tenant Entra issuer URL, where the issuer itself restricts who can sign in")
	}
}

func (c *Config) validateDevices(fatal problemFunc) {
	seen := make(map[string]int, len(c.Devices))
	seenID := make(map[string]int, len(c.Devices))
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

		// id is the device's identity everywhere downstream -- it keys
		// pushed router state and scopes an ingest token (see
		// internal/auth.Token.Device), so two devices sharing one is two
		// routers wearing one identity: either can then supply host
		// names for the other's traffic, defeating the per-device
		// scoping issue #285 added. Left unset it defaults to sourceIp
		// (see Config.normaliseDevices), so a collision here is always
		// an explicit one.
		id := strings.TrimSpace(d.ID)
		if id == "" {
			id = canonical
		}
		if prev, dup := seenID[id]; dup {
			fatal("CFG-0032", fmt.Sprintf("devices[%d].id", i),
				fmt.Sprintf("%q is already used by devices[%d]", id, prev),
				"give each device a distinct id -- it is what pushed router state and ingest tokens are keyed by")
			continue
		}
		seenID[id] = i

		// An id that is some *other* device's address is the same
		// collision by a longer route: a router discovered from that
		// address takes it as its own id (internal/device.Registry.
		// Resolve), and the two merge.
		if idAddr, err := netip.ParseAddr(id); err == nil && idAddr.Unmap().String() != canonical {
			fatal("CFG-0033", fmt.Sprintf("devices[%d].id", i),
				fmt.Sprintf("%q is an IP address that is not this device's sourceIp (%s)", id, canonical),
				"use a name (e.g. \"edge-router\"), or this device's own sourceIp -- an address belonging to another router would merge the two")
		}
	}
}

// defaultRetention/defaultMaxMemory are read from Default() rather than
// restated, so a clamp can never substitute something different from
// what a fresh install would have used. Restating them would be exactly
// the kind of quiet drift this whole feature exists to catch.
var (
	defaultRetention         = defaults().Store.Retention
	defaultMaxMemory         = defaults().Store.MaxMemory
	defaultMatchLogPath      = defaults().Watchlist.MatchLogPath
	defaultMatchLogCapacity  = defaults().Watchlist.MatchLogCapacity
	defaultMatchLogRetention = defaults().Watchlist.MatchLogRetention
)
