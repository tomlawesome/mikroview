// SPDX-License-Identifier: AGPL-3.0-only

// Package config loads mikroview's configuration with precedence
// defaults < YAML file < environment variables < CLI flags.
//
// The list of monitored RouterOS devices lives only in the YAML file: it's
// a structured list of records, which doesn't map cleanly onto env vars
// without an awkward numbered-key scheme. Scalar runtime knobs (ports,
// retention, log level) stay overridable via env vars for simple
// single-container deployments that don't want to mount a file at all.
package config

import (
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultDataDir is where every optional persistence path defaults to
// living, out of the box -- flags, detector settings, accounts, and the
// self-generated TLS certificate. The Dockerfile creates this directory
// owned by the nonroot user, so all of it is writable (and persists
// across simple process restarts) with zero configuration; surviving a
// full container *recreation* additionally needs a volume mounted over
// this same path, documented in deploy/docker-compose.yml rather than
// forced -- an operator who doesn't want any of this persisted can
// still point any of these at "" to opt back out per field.
const DefaultDataDir = "/var/lib/mikroview"

type Device struct {
	// ID is the device's identity everywhere downstream: the deviceId on
	// its events, the key its pushed router state is stored under, and
	// the scope of an ingest token (internal/auth.Token.Device). Optional
	// in the file -- normaliseDevices fills it from SourceIP when unset,
	// which is what a *discovered* router already gets
	// (internal/device.Registry.Resolve), so declaring a router that has
	// been sending syslog keeps its existing identity rather than
	// silently orphaning the state and tokens already keyed to it.
	//
	// Left empty it used to stay empty: every configured device without
	// an id shared the identity "", including for token scoping. See
	// validateDevices for the collision rules.
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	SourceIP string `yaml:"sourceIp"`
}

type Listen struct {
	// SyslogTLS (issue #188) accepts RouterOS's remote-protocol=tls
	// syslog, using the same certificate the HTTPS listener presents --
	// the router already imports mikroview's generated CA to verify
	// HTTPS ingest, so this is that same trust step, not a second one.
	// Started whenever this is non-empty -- NOT gated on tls.enabled,
	// unlike HTTPRedirect. A certificate is loaded when either the HTTPS
	// listener or this one needs it, so a deployment that turns off
	// mikroview's own HTTP TLS because a reverse proxy terminates TLS
	// would otherwise lose syslog ingest entirely once #189 removed the
	// plaintext listeners: the router connects here directly, never
	// through that proxy. Set to "" to disable it entirely, same
	// optional-empty-string contract as HTTPRedirect. Defaults to
	// ":6514", RFC 5425's port.
	//
	// This buys confidentiality and mikroview authenticating itself to
	// the router -- it does not authenticate the sender. RouterOS's
	// logging action has no client-certificate option, so anything able
	// to reach the port can still connect and inject log lines.
	SyslogTLS string `yaml:"syslogTls"`
	HTTP      string `yaml:"http"`
	// HTTPRedirect: a second, plain-HTTP-only listener whose sole job is
	// redirecting to the HTTPS listener above -- lets a browser/client
	// that guesses port 80 get bounced to the real thing instead of a
	// connection reset. Only started when tls.enabled is true (nothing
	// to redirect to otherwise). Same optional-empty-string contract as
	// the storage paths: set to "" to disable it entirely. The redirect
	// target is built by stripping any port off the request's Host
	// header and assuming HTTPS is reachable on the browser-default 443
	// -- true for docker-compose.yml's own default mapping (host 443 ->
	// this container's HTTPS port). If you've mapped HTTPS to a
	// different external port, either disable this (set to "") and
	// handle the redirect at your reverse proxy instead, or accept that
	// the Location header will point at :443 regardless.
	HTTPRedirect string `yaml:"httpRedirect"`
	// TrustedProxies lists the addresses mikroview will believe a
	// forwarding header from, as bare IPs, CIDRs, or the shorthand
	// "private" (loopback, RFC1918, link-local and IPv6 ULA -- which is
	// what a reverse proxy on a LAN or a docker network resolves to).
	//
	// Empty by default, and empty means forwarding headers are ignored
	// entirely. That default is deliberate: a header is just
	// client-supplied text, so honouring one unconditionally would let
	// anyone mint a fresh login rate-limit bucket per request by varying
	// it. Set this only for proxies you actually operate. See
	// internal/api's clientIP for how the chain is then walked.
	//
	// Leaving it unset behind a reverse proxy is safe but blunt: every
	// request then carries the proxy's own address, so all users share one
	// rate-limit bucket and one attacker's failures lock everyone out.
	TrustedProxies []string `yaml:"trustedProxies"`
	// ClientIPHeader is which forwarding header to read, honoured only for
	// peers matching TrustedProxies. Defaults to X-Forwarded-For, which
	// nginx, Caddy, Traefik, HAProxy, Cloudflare and the cloud load
	// balancers all set. Point it elsewhere (X-Real-IP, CF-Connecting-IP)
	// for a proxy that doesn't -- single-value headers work unchanged,
	// they're just a one-element chain.
	ClientIPHeader string `yaml:"clientIpHeader"`
}

// privateProxyRanges backs the "private" shorthand in
// Listen.TrustedProxies: loopback, RFC1918, CGNAT, link-local, and the
// IPv6 equivalents. This is the range a reverse proxy sharing a LAN or a
// container network with mikroview lands in.
var privateProxyRanges = []string{
	"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"100.64.0.0/10", "169.254.0.0/16",
	"::1/128", "fc00::/7", "fe80::/10",
}

// ParseTrustedProxies turns Listen.TrustedProxies into prefixes for
// internal/api. A bare address becomes a single-host prefix, so
// "10.0.0.5" and "10.0.0.5/32" mean the same thing.
func ParseTrustedProxies(entries []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, "private") {
			for _, r := range privateProxyRanges {
				p, err := netip.ParsePrefix(r)
				if err != nil {
					return nil, fmt.Errorf("trusted proxy range %q: %w", r, err)
				}
				out = append(out, p)
			}
			continue
		}
		if p, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither an IP address nor a CIDR", entry)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

type Store struct {
	Retention time.Duration `yaml:"retention"`
	// MaxMemory bounds the in-memory event ring by its memory cost
	// rather than by an event count -- see #244. An event count means
	// something different by up to four orders of magnitude between
	// deployments (it depends entirely on which RouterOS rules the
	// operator set log=yes on), so no single default event count could
	// be documented as meaning anything in particular; a memory budget
	// is the thing an operator actually controls (it is what they set on
	// a container) and mikroview can derive the rest.
	MaxMemory ByteSize `yaml:"maxMemory"`
}

// assumedBytesPerEvent is what a typical retained event costs: the fixed
// struct (464 bytes, internal/store.Event) plus one heap allocation for
// its raw syslog line, rounded to the allocator's size class. Measured in
// internal/store/memory_test.go's TestRetainedBytesPerEvent against a
// representative RouterOS forward-chain line -- re-run that test rather
// than trusting this constant if store.Event's fields change.
//
// This is a typical cost, and now also a bounded one. internal/routeros.
// Parse clamps every extracted field to 256 bytes and deliberately
// leaves Raw verbatim, so a long line still costs more than this
// constant assumes -- but Raw is itself capped at store.MaxRawBytes
// (2 KiB), which puts a ceiling on how much more.
//
// Before that cap existed the gap was not a rounding error: the syslog
// listener accepts a 64 KiB message, so a deployment fed adversarial
// lines retained ~66 KB per event against the ~616 assumed here, and the
// default 120 MiB budget could hold 12.55 GiB -- a 107x overrun from
// unauthenticated input, with validate.go's own 1.47x resident-memory
// warning compounding it rather than catching it. The worst case is now
// roughly 2 KiB + the struct, about 3.5x this constant rather than 107x.
// See #285 finding 5.
const assumedBytesPerEvent = 624

// Capacity derives the event ring's element count from the configured
// memory budget. Always at least 1 -- store.New already treats a
// non-positive capacity as 1, so a budget too small to hold even one
// event's assumed cost should fail the same way rather than silently
// holding zero.
func (s Store) Capacity() int {
	n := int64(s.MaxMemory) / assumedBytesPerEvent
	if n < 1 {
		n = 1
	}
	return int(n)
}

// Log controls mikroview's own server log output -- see
// internal/logging. Doesn't apply to the CLI recovery commands
// (-list-users, -recover-admin-account's prompts, etc.), which print directly
// to stdout/stderr for scripting/piping, not through this leveled path.
type Log struct {
	// Level is one of debug/info/warn/error (case-insensitive); anything
	// else falls back to info silently, same as every other malformed
	// value in this package (see internal/logging.SetLevel).
	Level string `yaml:"level"`
}

// GeoIP is entirely optional -- see internal/geoip. Left empty, the
// country-flag feature just doesn't show anything; there is no default
// database bundled or fetched, since MaxMind requires a free account to
// obtain one.
type GeoIP struct {
	// DBPath is kept out of the backup set (#372) on purpose: it names an
	// external MaxMind database file the operator downloads themselves,
	// not a store mikroview writes -- there is nothing here for a
	// restore to reproduce that a fresh download would not already give
	// back. See backup_cli.go's excludedFromBackup.
	DBPath string `yaml:"dbPath"`
}

// Reputation is entirely optional -- see internal/reputation. Shodan's
// InternetDB source needs no key and is always used; AbuseIPDB is only
// queried if a key is configured here.
type Reputation struct {
	AbuseIPDBKey string `yaml:"abuseIPDBKey"`
}

// SMTP configures send-only email alerting on newly-raised flags (issue
// #30) -- no inbound mailbox, no auth flows beyond these client creds,
// relayed through the operator's own external mail server. See
// internal/notify.SMTPConfig for the runtime shape this maps onto.
type SMTP struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	// Username left empty means no AUTH is attempted, for an open local
	// relay (e.g. a Postfix instance on the same host/network).
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// TLSMode is "" (plaintext, local relay only), "starttls" (upgrade
	// after connecting, typically port 587), or "implicit" (TLS from the
	// first byte, typically port 465).
	TLSMode string   `yaml:"tlsMode"`
	From    string   `yaml:"from"`
	To      []string `yaml:"to"`
}

// Pushover configures push notifications on newly-raised flags (issue
// #31) via Pushover (https://pushover.net) -- the simpler of the two
// push targets scoped in that issue: no VAPID keys, no service worker,
// no per-browser subscription management, just an application token and
// a user/group key.
type Pushover struct {
	Token string `yaml:"token"`
	User  string `yaml:"user"`
}

// Webhook configures a generic JSON-POST notification channel on
// newly-raised flags (issue #96) -- covers ntfy, Discord, Slack, Home
// Assistant, n8n, and anything else without a bespoke integration of
// its own. Headers is a plain map rather than a single bearer-token
// field since those receivers each expect auth in a different header
// (Authorization: Bearer ..., a custom X-... header, etc) -- set
// whichever header(s) your receiver needs. See
// internal/notify.WebhookConfig for the runtime shape this maps onto.
type Webhook struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

// Notify is entirely optional -- see internal/notify. Each channel
// (SMTP, Pushover, Webhook) is independently enabled by whether its own
// identifying field is set (SMTP.Host, Pushover.Token, Webhook.URL) --
// any combination may be configured at once, and every enabled channel
// shares the same BatchWindow/Dispatcher.
type Notify struct {
	// BatchWindow: how often pending flags are flushed to every
	// configured channel -- a fixed interval, not a quiet-period
	// debounce, so a sustained flood of flags during a real incident
	// still gets a bounded max delay before alerting rather than the
	// window continuously resetting. See internal/notify.Dispatcher.
	BatchWindow time.Duration `yaml:"batchWindow"`
	SMTP        SMTP          `yaml:"smtp"`
	Pushover    Pushover      `yaml:"pushover"`
	Webhook     Webhook       `yaml:"webhook"`
}

// Auth configures internal/auth's local authentication. Unlike Flags'
// StorePath, StorePath here is not truly optional -- mikroview stays
// fully open (today's behavior) as long as no user account exists, but
// the moment one is created it's required for that account to survive a
// restart, so registration refuses to proceed without it configured.
// See docs/configuration.md's "Authentication" section.
type Auth struct {
	StorePath string `yaml:"storePath"`
	// SecureCookie sets the session cookie's Secure flag. Defaults to
	// true, matching TLS.Enabled's own default -- mikroview serves TLS
	// out of the box now, so there's no other kind of connection to have
	// a session on. Only turn this off if you've also set
	// tls.enabled: false (see that field's doc comment for the one
	// supported reason to), or sessions won't work at all: a Secure
	// cookie is never sent back over a plain connection.
	SecureCookie bool `yaml:"secureCookie"`
	// SessionTTL is the idle timeout: a session's expiry slides forward
	// on each authenticated request, so this is "how long you can go
	// without activity before needing to log in again," not a fixed
	// session lifetime.
	SessionTTL time.Duration `yaml:"sessionTTL"`
	// SessionMaxLifetime is the ceiling SessionTTL does not have: the
	// longest a session can live from the moment it was issued, however
	// often it is used.
	//
	// The two answer different questions, which is why both exist.
	// SessionTTL asks "has this been abandoned"; a session used once a
	// day satisfies it forever, so without this a browser left signed in
	// on a shared machine -- or a cookie taken months ago -- stays valid
	// indefinitely (#294 item 3).
	//
	// Seven days by default: long enough that an operator checking their
	// firewall most days is not re-authenticating constantly, short
	// enough that a forgotten session is not a permanent one. Set to 0
	// to remove the ceiling and keep the old behaviour, which is a
	// deliberate choice rather than an oversight if you make it.
	SessionMaxLifetime time.Duration `yaml:"sessionMaxLifetime"`
	// TokensStorePath: where read-only API bearer tokens (issue #101)
	// persist across restarts, as a small JSON file (names + SHA-256
	// hashes, never the raw bearer values). Unlike StorePath above, this
	// one really is optional the way Flags.StorePath is -- an operator
	// who never creates a token doesn't need it, and a missing/unwritable
	// path just means token creation refuses (ErrTokenNotPersisted)
	// rather than mikroview failing to start.
	TokensStorePath string `yaml:"tokensStorePath"`
	// RecoveryKeysPath holds the hashed recovery keys that gate the CLI
	// commands changing authentication state (#134).
	//
	// Deliberately a different file from StorePath: if the digests lived
	// in the accounts file, a corrupted accounts file would also destroy
	// the only thing able to validate a recovery key -- exactly the
	// situation those commands exist to recover from.
	RecoveryKeysPath string `yaml:"recoveryKeysPath"`
	// RecoveryPepperPath holds the server-side secret mixed into every
	// recovery-key digest.
	//
	// Kept out of the backup set (#97) on purpose: someone holding a
	// stolen backup then has the digests and nothing to verify them
	// against. Generated once on first use and never rewritten.
	// MIKROVIEW_RECOVERY_PEPPER_FILE overrides it, for operators who
	// want it off the data volume entirely.
	RecoveryPepperPath string `yaml:"recoveryPepperPath"`
}

// Entities configures internal/entities' persisted, admin-manageable
// (type, key) -> label/tags store (issue #107) -- the shared foundation
// a future mail-sender allowlist and UI-managed IP/port/rule aliasing
// both build on. StorePath left empty is a fully supported, deliberate
// choice, same optional-persistence contract as Flags.StorePath: the
// store still works, entities just don't survive a restart.
type Entities struct {
	StorePath string `yaml:"storePath"`
}

// Coverage configures internal/coverage's persisted, admin-manageable
// coverage-gap declaration store (issue #630/#392): an admin's on-record
// statement that a given boundary-direction pair is intentionally, not
// accidentally, quiet. StorePath left empty is a fully supported,
// deliberate choice, same optional-persistence contract as
// Entities.StorePath: the store still works, declarations just don't
// survive a restart.
type Coverage struct {
	StorePath string `yaml:"storePath"`
}

// Audit configures internal/audit's persisted admin-action accountability
// log (issue #112) -- who created a user, changed a detector setting,
// upserted/deleted an entity, created or revoked an API token, or removed
// a permanent flag exclusion. StorePath left empty is a fully supported,
// deliberate choice, same optional-persistence contract as
// Entities.StorePath: the log still works, entries just don't survive a
// restart.
type Audit struct {
	StorePath string `yaml:"storePath"`
}

// Setup configures internal/setup's persisted setup-wizard ledger
// (#487) -- the steps an operator skipped or forced past, each with who
// decided it and when. Only those decisions are stored: what the wizard
// has *observed* (a CA fetch, a syslog connection, decoded log-prefixes,
// pushed tables) is re-made from arriving traffic every run.
//
// They are persisted because the design record makes the record the
// feature: a forced-past line has to keep explaining the silence it
// accounts for -- in the wizard's step list and in the empty states
// elsewhere -- and a restart is most likely at upgrade, exactly when
// somebody is looking for that explanation. StorePath left empty is a
// fully supported, deliberate choice, same optional-persistence
// contract as Audit.StorePath: the wizard still works, the decisions
// just don't survive a restart.
type Setup struct {
	StorePath string `yaml:"storePath"`
}

// Watchlist configures internal/watchlist's entry store and its
// internal/matchlog match log (#243) -- the persisted replacement for
// Control Ports' single flat criticalPorts port list. StorePath (the
// entries themselves) follows the same optional-persistence contract as
// every other small store here: left empty, entries still work, just
// don't survive a restart.
//
// MatchLogPath does not share that contract -- it has no in-memory-only
// mode, unlike every other store in this file. Durability is the entire
// reason this store exists (#243 section 3's "a match must survive a
// restart" requirement); an in-memory match log would be a second
// volatile event ring with extra steps, not a lesser version of this
// feature. So MatchLogPath must be non-empty (see CFG-0040) and
// MatchLogCapacity must be positive (CFG-0041) -- both a good default
// out of the box, not settings an operator has to supply.
type Watchlist struct {
	StorePath string `yaml:"storePath"`
	// MatchLogPath is where internal/matchlog's append-only JSON-lines
	// file lives.
	MatchLogPath string `yaml:"matchLogPath"`
	// MatchLogCapacity is the match log's hard ceiling on distinct
	// records -- #243 section 3 puts the file backend's realistic range
	// at ~100k-500k matches; see internal/matchlog.ErrCapacityReached
	// for what happens once it's reached (refused, not silently
	// overwritten). File backend only -- the Postgres backend ignores
	// this and uses MatchLogRetention instead.
	MatchLogCapacity int `yaml:"matchLogCapacity"`
	// MatchLogRetention is how long a match is kept, on the Postgres
	// backend only, once its last activity ages past it -- #243 section
	// 3's "pragmatically unlimited" record count there, bounded by age
	// rather than by count the way the file backend is. Enforced by
	// internal/matchlog.PostgresStore.RunPeriodicPurge, not at write
	// time. Ignored on the file backend, which has no ageing policy of
	// its own -- it stops accepting new records at MatchLogCapacity
	// instead.
	MatchLogRetention time.Duration `yaml:"matchLogRetention"`
	// SuggestionsStorePath is where internal/suggest's candidate pool
	// (#243 slice 5 -- watchlist entries suggested from data RouterOS has
	// already pushed) persists. Same optional-persistence contract as
	// StorePath above: left empty, suggestions still work, they just
	// regenerate from scratch (at Off, nothing lost that matters -- see
	// internal/suggest's package doc comment) on every restart instead of
	// remembering what was already accepted or hidden.
	SuggestionsStorePath string `yaml:"suggestionsStorePath"`
}

// TLS configures mikroview's own listener -- on by default: a browser
// secure-context requirement was only ever a symptom of the real
// problem, which is that an app serving real login credentials and
// session cookies has no business doing so over cleartext, LAN or not
// (see docs/configuration.md's "TLS" section for the full reasoning).
// See internal/servertls for exactly how a certificate is obtained.
type TLS struct {
	// Enabled defaults to true. The one documented reason to set this
	// false is a deployment where mikroview's listener is provably only
	// reachable from your own reverse proxy over an isolated docker
	// network -- never published to the LAN/host at all -- so there's no
	// bypass surface for TLS to protect against on that hop, and the RP
	// already owns TLS termination for real clients. Never set this
	// false if mikroview's port is reachable from a LAN or the internet
	// in any other way.
	Enabled bool `yaml:"enabled"`
	// CertFile/KeyFile: your own cert (a real domain + ACME, a corporate
	// CA, etc) -- takes priority over generation if both are set.
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
	// Hosts: hostnames/IPs a generated certificate should cover (SANs)
	// -- whatever you'll actually use to reach mikroview (a LAN IP, the
	// docker-compose service name a reverse proxy uses as its upstream,
	// a .local hostname). Defaults to localhost/127.0.0.1 if unset --
	// still fully encrypted either way, just only strictly verifiable
	// under those names unless you add your own.
	Hosts []string `yaml:"hosts"`
	// StorePath: where a generated CA + certificate persist across
	// restarts, so the trust step (importing the CA into your browser or
	// reverse proxy) is a one-time cost rather than a per-restart one.
	// Optional, same contract as flags.storePath: left unset, TLS still
	// works, it just regenerates (and needs re-trusting) every restart.
	//
	// Kept out of the backup set (#372) on purpose: this is a directory
	// of generated CA + certificate key material, not a single JSON
	// document like every other *Path field backedUpStores carries, and
	// restoring it blind onto a new host is more likely to be wrong than
	// right -- different hostname/IP SANs, a CA nothing there has
	// trusted yet. Regenerating it is one restart away, so there is
	// nothing here a restore is actually saving. See
	// backup_cli.go's excludedFromBackup.
	StorePath string `yaml:"storePath"`
}

// OIDC configures optional SSO login via an external OIDC provider
// (issue #43, Authentik-targeted but any standard OIDC provider works)
// on top of, never instead of, local password auth -- see
// internal/oidc for the protocol implementation and
// internal/auth.Store.FindOrCreateOIDCUser for identity storage/JIT
// provisioning. Empty IssuerURL means OIDC is not configured, the same
// "empty means opt-out, no separate enabled bool" contract
// Reputation.AbuseIPDBKey/GeoIP.DBPath already use -- there's no
// scenario where a fully-populated OIDC block should be silently
// inert, unlike Notify's SMTP/Pushover (each independently optional
// *within* one shared block), so a bare bool would be redundant here.
type OIDC struct {
	IssuerURL    string `yaml:"issuerUrl"`
	ClientID     string `yaml:"clientId"`
	ClientSecret string `yaml:"clientSecret"`
	// PublicBaseURL is the externally-reachable origin used to build the
	// redirect_uri registered at the provider (PublicBaseURL +
	// "/api/auth/oidc/callback") -- required whenever IssuerURL is set.
	// Deliberately never inferred from a request's Host/X-Forwarded-Host
	// header: doing so is a known redirect_uri-confusion vulnerability
	// class, since the exact-match registration at the provider is the
	// actual defense, and only holds if mikroview never constructs it
	// from client-influenced input. Covers both deployment modes this
	// app already supports: mikroview's own self-signed TLS on a LAN
	// IP/hostname (e.g. "https://192.168.1.10:8443"), or fronted by a
	// reverse proxy terminating a real domain (e.g.
	// "https://mikroview.example.com").
	PublicBaseURL string `yaml:"publicBaseUrl"`
	// Scopes defaults to {openid, profile, email} if empty -- see
	// internal/oidc.New.
	Scopes []string `yaml:"scopes"`

	// AllowedGroups/GroupsClaim/AllowedEmails/AllowedEmailDomains/
	// RequiredClaims restrict *which* accounts at the issuer may sign in.
	// See internal/oidc.Policy for the full semantics; in short, leaving
	// all of them empty permits anyone the issuer vouches for, and each
	// one that is set adds a condition that must hold.
	//
	// For a self-hosted issuer (Authentik, Keycloak, Zitadel) empty is
	// the right answer: the issuer URL already restricts login to
	// accounts in a directory you run, and delegating that decision to
	// the IdP's own ACLs is the point of SSO. Set these when you want to
	// scope access more tightly than "everyone in my directory".
	//
	// For a multi-tenant issuer they are mandatory, and mikroview refuses
	// to enable SSO without them -- see oidc.IsMultiTenantIssuer.
	// Every Google account on earth validates against
	// accounts.google.com, so with no restriction the first stranger to
	// reach the login page becomes the admin.
	//
	// Google Workspace, one organisation:
	//   requiredClaims: {hd: ["example.com"]}
	// Microsoft Entra, one tenant:
	//   requiredClaims: {tid: ["00000000-0000-0000-0000-000000000000"]}
	// Authentik/Keycloak group:
	//   allowedGroups: ["mikroview-admins"]
	AllowedGroups []string `yaml:"allowedGroups"`
	// GroupsClaim defaults to "groups". Azure commonly needs "roles".
	GroupsClaim         string              `yaml:"groupsClaim"`
	AllowedEmails       []string            `yaml:"allowedEmails"`
	AllowedEmailDomains []string            `yaml:"allowedEmailDomains"`
	RequiredClaims      map[string][]string `yaml:"requiredClaims"`
}

// DetectorScope is DetectorSettings' host/port/rule/classification
// restriction, as plain yaml-tagged fields rather than importing
// internal/detect -- same reasoning Flags already gives for duplicating
// detect.Config's thresholds: this package stays a dependency-free
// leaf. See internal/detect.Scope's doc comment for exactly which
// fields each detector consults and what "" for a Mode/Classification
// field means (no restriction).
type DetectorScope struct {
	Hosts          []string `yaml:"hosts"`
	HostsMode      string   `yaml:"hostsMode"`
	Ports          []int    `yaml:"ports"`
	PortsMode      string   `yaml:"portsMode"`
	Classification string   `yaml:"classification"`
	Rules          []string `yaml:"rules"`
	RulesMode      string   `yaml:"rulesMode"`
}

// DetectorSettings is one detector's config.yaml-configurable starting
// point -- enabled by default, unscoped. A live admin-only UI toggle
// (see docs/configuration.md's "Per-detector toggles" section) can
// override this at runtime without a restart, persisted separately to
// DetectorSettingsStorePath; these YAML values are only ever the seed
// for the first run, not re-read afterward.
type DetectorSettings struct {
	Enabled bool          `yaml:"enabled"`
	Scope   DetectorScope `yaml:"scope"`
}

// Flags configures internal/detect's behavioral detectors and
// internal/flags' persistence -- see both packages' docs for what each
// threshold means and why the defaults are what they are. StorePath left
// empty is a fully supported, deliberate choice: detection still runs
// and flags still work, they just don't survive a restart, consistent
// with the rest of mikroview (see SECURITY.md).
type Flags struct {
	StorePath                string        `yaml:"storePath"`
	PortScanThreshold        int           `yaml:"portScanThreshold"`
	PortScanWindow           time.Duration `yaml:"portScanWindow"`
	ActivitySpikeThreshold   int           `yaml:"activitySpikeThreshold"`
	ActivitySpikeWindow      time.Duration `yaml:"activitySpikeWindow"`
	CriticalPorts            []int         `yaml:"criticalPorts"`
	CriticalPortThreshold    int           `yaml:"criticalPortThreshold"`
	CriticalPortWindow       time.Duration `yaml:"criticalPortWindow"`
	GlobalSpikeMultiplier    float64       `yaml:"globalSpikeMultiplier"`
	GlobalSpikeMinEPS        float64       `yaml:"globalSpikeMinEPS"`
	GlobalSpikeWarmupSamples int           `yaml:"globalSpikeWarmupSamples"`

	DistributedBruteForceThreshold int           `yaml:"distributedBruteForceThreshold"`
	DistributedBruteForceWindow    time.Duration `yaml:"distributedBruteForceWindow"`

	OutboundAnomalyThreshold int           `yaml:"outboundAnomalyThreshold"`
	OutboundAnomalyWindow    time.Duration `yaml:"outboundAnomalyWindow"`

	InternalReconThreshold int           `yaml:"internalReconThreshold"`
	InternalReconWindow    time.Duration `yaml:"internalReconWindow"`

	RuleSpikeMultiplier    float64       `yaml:"ruleSpikeMultiplier"`
	RuleSpikeMinRate       float64       `yaml:"ruleSpikeMinRate"`
	RuleSpikeWindow        time.Duration `yaml:"ruleSpikeWindow"`
	RuleSpikeWarmupSamples int           `yaml:"ruleSpikeWarmupSamples"`

	RepeatedDropsThreshold int           `yaml:"repeatedDropsThreshold"`
	RepeatedDropsWindow    time.Duration `yaml:"repeatedDropsWindow"`

	HostActivityMultiplier    float64 `yaml:"hostActivityMultiplier"`
	HostActivityWarmupSamples int     `yaml:"hostActivityWarmupSamples"`

	// LowSlowScan* (issue #20): see internal/detect.Config's matching
	// fields for what each one means -- duplicated here rather than
	// imported, same as every other Flags field.
	LowSlowScanWindow             time.Duration `yaml:"lowSlowScanWindow"`
	LowSlowScanPortThreshold      int           `yaml:"lowSlowScanPortThreshold"`
	LowSlowScanHostThreshold      int           `yaml:"lowSlowScanHostThreshold"`
	LowSlowScanMinObservation     time.Duration `yaml:"lowSlowScanMinObservation"`
	LowSlowScanDropRatio          float64       `yaml:"lowSlowScanDropRatio"`
	LowSlowScanBaselineMultiplier float64       `yaml:"lowSlowScanBaselineMultiplier"`

	// OffHours* (issue #104): see internal/detect.Config's matching
	// fields for what each one means -- duplicated here rather than
	// imported, same as every other Flags field. OffHoursStartHour/
	// EndHour (0-23, server-local time) is the fixed clock window this
	// detector is willing to fire in; OffHoursMinSampleDays/MinCount are
	// the false-positive guard's two independent floors.
	OffHoursStartHour     int `yaml:"offHoursStartHour"`
	OffHoursEndHour       int `yaml:"offHoursEndHour"`
	OffHoursMinSampleDays int `yaml:"offHoursMinSampleDays"`
	OffHoursMinCount      int `yaml:"offHoursMinCount"`

	// DeviceStaleAfter (issue #98): see internal/detect.Config's matching
	// field for what this means and why the default sits where it does.
	// 0 disables the device-silence detector entirely.
	DeviceStaleAfter time.Duration `yaml:"deviceStaleAfter"`

	// StaleRule* (issue #102): flags a firewall rule that fired at some
	// point but hasn't fired again in a long time -- either dead weight
	// or an unnecessary hole, worth a human's attention either way. See
	// internal/rules (the long-lived per-rule usage record this reads
	// from) and internal/engine's stale_rule definition (the sweep
	// itself).
	//
	// RuleUsageStorePath persists that usage record so "hasn't fired in
	// 30 days" survives a restart -- same optional-persistence contract
	// as StorePath above (empty disables persistence, not the feature).
	// StaleRuleDays is how long a rule must go quiet before it's
	// considered stale. StaleRuleCheckInterval is how often the sweep
	// re-checks -- coarse by design, since staleness is judged in days,
	// not seconds. Both seed the stale_rule definition's own maxAge and
	// checkInterval params (issue #405), which is what the engine's tick
	// driver honours; see internal/engine.ShippedDefaults.
	RuleUsageStorePath     string        `yaml:"ruleUsageStorePath"`
	StaleRuleDays          int           `yaml:"staleRuleDays"`
	StaleRuleCheckInterval time.Duration `yaml:"staleRuleCheckInterval"`

	// DetectorSettingsStorePath persists live UI on/off+scope toggles
	// (see internal/detect.SettingsStore) so they survive a restart --
	// same optional-persistence contract as StorePath above. Detectors
	// map is YAML-only (no env var), same rationale as RuleNames/
	// HostNames/Devices below: a structured per-detector record doesn't
	// map cleanly onto env vars. Keyed by detector name (e.g.
	// "port_scan", "rule_spike" -- see internal/detect.DetectorName).
	DetectorSettingsStorePath string                      `yaml:"detectorSettingsStorePath"`
	Detectors                 map[string]DetectorSettings `yaml:"detectors"`

	// VPNInterfaces/VPNConfidenceMultiplier (issue #105): see
	// internal/detect.Config's matching fields for what each one means
	// and why the default (empty VPNInterfaces, so this whole feature is
	// inert until configured) is what it is -- duplicated here rather
	// than imported, same as every other Flags field. VPNInterfaces
	// entries are glob patterns (path.Match syntax) matched against
	// store.Event.InInterface, e.g. "wireguard1" (exact) or "wireguard*"
	// (prefix) for whatever name RouterOS assigns your WireGuard
	// interface.
	VPNInterfaces           []string `yaml:"vpnInterfaces"`
	VPNConfidenceMultiplier float64  `yaml:"vpnConfidenceMultiplier"`
}

// DeviceMAC configures internal/device's MACRegistry (issue #103 phase
// 1) -- the new-MAC detector's persisted per-SrcMAC FirstSeen/LastSeen
// history. Optional persistence, same contract as Flags.StorePath: left
// empty, the detector still runs, it just starts with an empty registry
// on every restart instead of remembering every MAC mikroview has ever
// logged traffic from -- and "new" only means anything against history
// well beyond the 24h event-retention window.
type DeviceMAC struct {
	StorePath string `yaml:"storePath"`
}

// Engine configures internal/engine's evaluation chassis
// (docs/decisions/evaluation-engine.md): its persisted per-definition,
// per-key baseline state (#399/#400: engine.StateStore), what a Baseline
// needs to resume warm across a restart instead of being blind for its
// whole warm-up again, and (#404) the definitions store -- the one
// document holding every definition (shipped detectors, watchlist
// expectations, and eventually builder-authored custom ones), on both
// backends. Optional persistence, same contract as Flags.StorePath: left
// empty, the engine still runs, every Baseline just starts cold and the
// definitions store stays in-memory only.
type Engine struct {
	StorePath string `yaml:"storePath"`
	// DefinitionsStorePath persists the definitions store (#404). On
	// first boot against an empty document, it is seeded from
	// internal/detect's settings store and internal/watchlist's entries
	// store (see engine.MigrateDefinitions) -- non-destructively: both
	// old stores keep reading and writing their own documents until
	// #405/#406 port their evaluation logic onto this chassis and retire
	// them.
	DefinitionsStorePath string `yaml:"definitionsStorePath"`
}

// Blocklist configures internal/blocklist's local IP/CIDR "known-bad"
// matching against a small, vetted menu of free threat-intel feeds
// (issue #113 Part B) -- see that package's own doc comment for the
// full menu, why it's a fixed menu rather than an arbitrary URL field,
// and how the refresh cadence/entry-count cap were decided.
//
// On by default with Spamhaus's DROP list -- the issue's own
// recommended starting point: small, free, no registration, and
// curated specifically to only include netblocks Spamhaus is confident
// are entirely malicious-controlled, a safe "flag on sight" default
// unlike a larger, noisier aggregated list would be. Set sources to an
// empty list (`sources: []`) to disable local blocklist matching
// entirely. Refresh cadence is intentionally not configurable here --
// see internal/blocklist.RefreshInterval's doc comment.
type Blocklist struct {
	// Sources is a list of internal/blocklist.Source values (e.g.
	// "spamhaus_drop", "emerging_threats_compromised")
	// -- an unrecognized entry is logged and skipped at startup, not a
	// fatal error, same degrade-not-crash contract as every other
	// optional integration in this codebase.
	Sources []string `yaml:"sources"`
}

// NetClass configures internal/netclass's local IP attribution: labelling
// an address as a Tor exit, a commercial VPN, cloud/datacenter space, or
// a privacy relay (issue #114). It adds context to a manual IP lookup,
// and never raises a flag on its own for any category -- but a Tor or
// VPN match on an inbound source (see internal/detect/netclass.go) does
// reinforce an already-raised flag's confidence, direction-aware and
// weighted per category. See internal/netclass's doc comment for the
// menu and why it is a fixed menu rather than an arbitrary URL field.
//
// On by default with the high-precision lists (Tor exit nodes, Apple
// Private Relay, and the X4BNet VPN list) -- deliberately not the broad
// datacenter/cloud feeds, which cover >10% of routable IPv4 and would
// attach a label to ordinary traffic. An operator who wants full cloud
// attribution opts the rest in: sources like "x4b_datacenter", "aws",
// "gcp". Set sources to an empty list to disable attribution (and the
// confidence reinforcement) entirely. Refresh cadence is not
// configurable, same reasoning as Blocklist.
type NetClass struct {
	// Sources is a list of internal/netclass.Source values -- an
	// unrecognized entry is logged and skipped, degrade-not-crash like
	// every other optional integration here.
	Sources []string `yaml:"sources"`
}

// Postgres optionally moves mikroview's persisted state off this host
// and onto a database server (issue #131).
//
// The point is separation: compromising the mikroview host should not
// hand over the accounts store, because the attacker also has to reach
// and authenticate to a second, independently secured system.
//
// **That only holds if the database is genuinely off-box.** A Postgres
// on this same host -- including one in a container beside mikroview --
// exposes its credential to exactly the compromise it was meant to
// survive, and is strictly worse than the JSON files it replaces: same
// exposure, more moving parts. This is why deploy/docker-compose.yml
// deliberately ships no Postgres service to uncomment.
type Postgres struct {
	// DSNFile is a file containing the connection string, and is the
	// recommended way to supply it -- a Docker/Kubernetes secret, or a
	// 0600 file on the host.
	//
	// A DSN carries a password, which is why there is no `dsn:` field
	// here and no command-line flag: a password in config.yaml ends up
	// in whatever backs that file up, and a password in argv is visible
	// to every process on the box and to `docker inspect`. Same
	// reasoning -recover-admin-account uses for prompting rather than
	// taking an argument.
	// Empty means "not configured": mikroview uses the JSON files,
	// which stays the default, zero-infrastructure path.
	//
	// The DSN's sslmode must be require, verify-ca or verify-full.
	// Anything weaker is refused at startup rather than silently
	// upgraded -- see internal/persist.OpenPool. verify-full is the one
	// that actually stops an attacker on the network path; require only
	// encrypts.
	DSNFile string `yaml:"dsnFile"`
}

type Config struct {
	Listen     Listen     `yaml:"listen"`
	Store      Store      `yaml:"store"`
	Log        Log        `yaml:"log"`
	GeoIP      GeoIP      `yaml:"geoip"`
	Reputation Reputation `yaml:"reputation"`
	Flags      Flags      `yaml:"flags"`
	Auth       Auth       `yaml:"auth"`
	Entities   Entities   `yaml:"entities"`
	Coverage   Coverage   `yaml:"coverage"`
	Audit      Audit      `yaml:"audit"`
	Setup      Setup      `yaml:"setup"`
	Watchlist  Watchlist  `yaml:"watchlist"`
	Notify     Notify     `yaml:"notify"`
	TLS        TLS        `yaml:"tls"`
	OIDC       OIDC       `yaml:"oidc"`
	Postgres   Postgres   `yaml:"postgres"`
	Devices    []Device   `yaml:"devices"`
	DeviceMAC  DeviceMAC  `yaml:"deviceMac"`
	Blocklist  Blocklist  `yaml:"blocklist"`
	NetClass   NetClass   `yaml:"netClass"`
	Engine     Engine     `yaml:"engine"`

	// RuleNames/HostNames are optional friendly-display-name maps -- see
	// internal/naming. Keyed by the raw value RouterOS reports (a rule
	// label like "r13", a host IP), same lookup-table shape rather than
	// Devices' structured-record shape since there's nothing else to
	// store per entry.
	RuleNames map[string]string `yaml:"ruleNames"`
	HostNames map[string]string `yaml:"hostNames"`
}

// normaliseDevices fills in each device's effective id. An entry with no
// id is identified by its own source address -- the same identity the
// device would have had if it had simply been discovered from its
// syslog, so declaring a router is additive rather than a rename.
func (c *Config) normaliseDevices() {
	for i := range c.Devices {
		if strings.TrimSpace(c.Devices[i].ID) != "" {
			continue
		}
		ip := strings.TrimSpace(c.Devices[i].SourceIP)
		if addr, err := netip.ParseAddr(ip); err == nil {
			c.Devices[i].ID = addr.Unmap().String()
			continue
		}
		c.Devices[i].ID = ip
	}
}

func defaults() Config {
	return Config{
		Listen: Listen{
			SyslogTLS:    ":6514",
			HTTP:         ":8080",
			HTTPRedirect: ":8081",
		},
		Store: Store{
			Retention: 24 * time.Hour,
			// 120MiB / 624 bytes/event (assumedBytesPerEvent) derives to
			// ~201,649 events -- close to the old flat 200,000 default,
			// so a fresh install's memory footprint does not jump on
			// upgrade even though the unit did.
			MaxMemory: 120 * 1024 * 1024,
		},
		Log: Log{
			Level: "info",
		},
		// Mirrors internal/detect.DefaultConfig() -- kept as separate
		// literal values (rather than importing internal/detect here) so
		// this package stays a dependency-free leaf that every feature
		// package can build on, not the other way around.
		Flags: Flags{
			PortScanThreshold:        15,
			PortScanWindow:           60 * time.Second,
			ActivitySpikeThreshold:   200,
			ActivitySpikeWindow:      60 * time.Second,
			CriticalPorts:            []int{21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729},
			CriticalPortThreshold:    5,
			CriticalPortWindow:       5 * time.Minute,
			GlobalSpikeMultiplier:    4,
			GlobalSpikeMinEPS:        5,
			GlobalSpikeWarmupSamples: 20,

			DistributedBruteForceThreshold: 10,
			DistributedBruteForceWindow:    5 * time.Minute,

			OutboundAnomalyThreshold: 25,
			OutboundAnomalyWindow:    5 * time.Minute,

			InternalReconThreshold: 10,
			InternalReconWindow:    60 * time.Second,

			RuleSpikeMultiplier:    5,
			RuleSpikeMinRate:       0.2,
			RuleSpikeWindow:        60 * time.Second,
			RuleSpikeWarmupSamples: 20,

			RepeatedDropsThreshold: 10,
			RepeatedDropsWindow:    15 * time.Minute,

			HostActivityMultiplier:    3,
			HostActivityWarmupSamples: 20,

			LowSlowScanWindow:             3 * time.Hour,
			LowSlowScanPortThreshold:      8,
			LowSlowScanHostThreshold:      5,
			LowSlowScanMinObservation:     45 * time.Minute,
			LowSlowScanDropRatio:          0.8,
			LowSlowScanBaselineMultiplier: 3,

			// 23:00-06:00: a conservative, common-denominator quiet
			// period for a home/small-office network -- see
			// internal/detect/off_hours.go's doc comment for why a
			// fixed window was chosen over a per-host-learned one.
			OffHoursStartHour:     23,
			OffHoursEndHour:       6,
			OffHoursMinSampleDays: 14,
			OffHoursMinCount:      5,

			DeviceStaleAfter: 15 * time.Minute,

			RuleUsageStorePath:     DefaultDataDir + "/rule-usage.json",
			StaleRuleDays:          30,
			StaleRuleCheckInterval: time.Hour,

			StorePath:                 DefaultDataDir + "/flags.json",
			DetectorSettingsStorePath: DefaultDataDir + "/detector-settings.json",

			// VPNInterfaces is empty by default -- see its doc comment
			// for why that's the deliberate, backward-compatible no-op
			// starting point. VPNConfidenceMultiplier mirrors
			// internal/detect.DefaultConfig()'s own default so setting
			// only vpnInterfaces in config.yaml is enough to opt in.
			VPNConfidenceMultiplier: 1.5,
		},
		Auth: Auth{
			StorePath:          DefaultDataDir + "/users.json",
			SessionTTL:         24 * time.Hour,
			SessionMaxLifetime: 7 * 24 * time.Hour,
			SecureCookie:       true,
			TokensStorePath:    DefaultDataDir + "/tokens.json",
			RecoveryKeysPath:   DefaultDataDir + "/recovery-keys.json",
			RecoveryPepperPath: DefaultDataDir + "/recovery-pepper.key",
		},
		Entities: Entities{
			StorePath: DefaultDataDir + "/entities.json",
		},
		Coverage: Coverage{
			StorePath: DefaultDataDir + "/coverage.json",
		},
		Audit: Audit{
			StorePath: DefaultDataDir + "/audit.json",
		},
		Setup: Setup{
			StorePath: DefaultDataDir + "/setup.json",
		},
		Watchlist: Watchlist{
			StorePath:            DefaultDataDir + "/watchlist.json",
			MatchLogPath:         DefaultDataDir + "/matchlog.jsonl",
			MatchLogCapacity:     200_000,
			MatchLogRetention:    7 * 24 * time.Hour,
			SuggestionsStorePath: DefaultDataDir + "/suggestions.json",
		},
		TLS: TLS{
			Enabled:   true,
			StorePath: DefaultDataDir + "/tls",
		},
		DeviceMAC: DeviceMAC{
			StorePath: DefaultDataDir + "/mac-registry.json",
		},
		Engine: Engine{
			StorePath:            DefaultDataDir + "/engine-state.json",
			DefinitionsStorePath: DefaultDataDir + "/definitions.json",
		},
		Blocklist: Blocklist{
			// Mirrors internal/blocklist.DefaultSources -- kept as a
			// literal here (rather than importing internal/blocklist)
			// so this package stays a dependency-free leaf, same
			// reasoning Flags already gives for duplicating
			// internal/detect.Config's own defaults.
			// EDROP is deliberately absent: Spamhaus merged it into
			// DROP on 2024-04-10 and the endpoint now serves no ranges
			// at all.
			Sources: []string{"spamhaus_drop"},
		},
		NetClass: NetClass{
			// Mirrors internal/netclass.DefaultSources -- literal here
			// to keep this package a dependency-free leaf, same as
			// Blocklist above. TestNetClassDefaultMatchesNetclassPackage
			// pins the two together, because they drifted: this list was
			// missing apple_private_relay, and since main.go wires
			// netclass.New with *this* value, netclass.DefaultSources was
			// dead code and a fresh install shipped without it. That is
			// not a cosmetic difference -- x4b_vpn's upstream data covers
			// the same ranges, so leaving Apple's own list out is what
			// makes ordinary iPhone/iPad/Mac traffic read as a VPN exit.
			Sources: []string{"tor", "apple_private_relay", "x4b_vpn"},
		},
		Notify: Notify{
			BatchWindow: 60 * time.Second,
		},
	}
}

// Load builds the effective Config from defaults, an optional YAML file,
// environment variables, and CLI flags (in that order of increasing
// precedence). configPath and args are taken as parameters (rather than
// read from os.Args/env directly) so tests can call this deterministically.
func Load(configPath string, args []string) (Config, error) {
	cfg, _, err := load(configPath, args)
	return cfg, err
}

func load(configPath string, args []string) (Config, Result, error) {
	cfg := defaults()

	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, Result{}, fmt.Errorf("loading config file %s: %w", configPath, err)
		}
	}

	applyEnv(&cfg)

	if err := applyFlags(&cfg, args); err != nil {
		return Config{}, Result{}, err
	}

	// Devices get their identity filled in before validation, so
	// validation and every downstream consumer agree on what a device's
	// id actually is.
	cfg.normaliseDevices()

	// Validate last, after every source has been merged -- an env var or
	// a flag can fix a bad yaml value, so checking earlier would reject
	// configurations that are actually fine.
	result := cfg.Validate()
	if err := result.Err(); err != nil {
		// The result goes back even on failure. Returning Result{} here
		// discarded every Problem the caller needed -- the code, the
		// key, the remediation, the example snippet -- leaving the
		// server and -validate-config with nothing but Err()'s single
		// flattened line. The config is still not usable, so Config{}
		// stays empty; the diagnosis is the part worth keeping.
		return Config{}, result, err
	}

	return cfg, result, nil
}

// LoadWithProblems is Load plus the non-fatal problems found. Callers
// that can surface warnings (the server, -validate-config) use this;
// Load stays the simple form for callers that only need a usable config.
//
// The result has to come from the *same* Validate call that did the
// clamping. Validating a second time would report nothing, because the
// first pass already replaced the bad value with a good one -- the
// operator would get a silently substituted default and no warning,
// which is precisely the failure this feature exists to prevent.
func LoadWithProblems(configPath string, args []string) (Config, Result, error) {
	return load(configPath, args)
}

func loadYAML(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	return dec.Decode(cfg)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("MIKROVIEW_LISTEN_SYSLOG_TLS"); v != "" {
		cfg.Listen.SyslogTLS = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_HTTP"); v != "" {
		cfg.Listen.HTTP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_HTTP_REDIRECT"); v != "" {
		cfg.Listen.HTTPRedirect = v
	}
	if v := os.Getenv("MIKROVIEW_RECOVERY_PEPPER_FILE"); v != "" {
		cfg.Auth.RecoveryPepperPath = v
	}
	if v := os.Getenv("MIKROVIEW_TRUSTED_PROXIES"); v != "" {
		cfg.Listen.TrustedProxies = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_CLIENT_IP_HEADER"); v != "" {
		cfg.Listen.ClientIPHeader = v
	}
	if v := os.Getenv("MIKROVIEW_STORE_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Store.Retention = d
		}
	}
	if v := os.Getenv("MIKROVIEW_STORE_MAX_MEMORY"); v != "" {
		if b, err := ParseByteSize(v); err == nil {
			cfg.Store.MaxMemory = b
		}
	}
	if v := os.Getenv("MIKROVIEW_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("MIKROVIEW_GEOIP_DB_PATH"); v != "" {
		cfg.GeoIP.DBPath = v
	}
	if v := os.Getenv("MIKROVIEW_ABUSEIPDB_KEY"); v != "" {
		cfg.Reputation.AbuseIPDBKey = v
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_STORE_PATH"); v != "" {
		cfg.Flags.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_PORT_SCAN_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.PortScanThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_PORT_SCAN_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.PortScanWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_ACTIVITY_SPIKE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.ActivitySpikeThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_ACTIVITY_SPIKE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.ActivitySpikeWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_CRITICAL_PORTS"); v != "" {
		if ports, ok := parseIntList(v); ok {
			cfg.Flags.CriticalPorts = ports
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_CRITICAL_PORT_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.CriticalPortThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_CRITICAL_PORT_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.CriticalPortWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.GlobalSpikeMultiplier = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_MIN_EPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.GlobalSpikeMinEPS = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_GLOBAL_SPIKE_WARMUP_SAMPLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.GlobalSpikeWarmupSamples = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.DistributedBruteForceThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.DistributedBruteForceWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.OutboundAnomalyThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.OutboundAnomalyWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_INTERNAL_RECON_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.InternalReconThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_INTERNAL_RECON_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.InternalReconWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_RULE_SPIKE_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.RuleSpikeMultiplier = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_RULE_SPIKE_MIN_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.RuleSpikeMinRate = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_RULE_SPIKE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.RuleSpikeWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_RULE_SPIKE_WARMUP_SAMPLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.RuleSpikeWarmupSamples = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_REPEATED_DROPS_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.RepeatedDropsThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_REPEATED_DROPS_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.RepeatedDropsWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_HOST_ACTIVITY_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.HostActivityMultiplier = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_HOST_ACTIVITY_WARMUP_SAMPLES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.HostActivityWarmupSamples = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.LowSlowScanWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_PORT_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.LowSlowScanPortThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_HOST_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.LowSlowScanHostThreshold = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_MIN_OBSERVATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.LowSlowScanMinObservation = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_DROP_RATIO"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.LowSlowScanDropRatio = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_LOW_SLOW_SCAN_BASELINE_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.LowSlowScanBaselineMultiplier = f
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OFF_HOURS_START_HOUR"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.OffHoursStartHour = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OFF_HOURS_END_HOUR"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.OffHoursEndHour = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OFF_HOURS_MIN_SAMPLE_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.OffHoursMinSampleDays = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_OFF_HOURS_MIN_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.OffHoursMinCount = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_DEVICE_STALE_AFTER"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.DeviceStaleAfter = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_RULE_USAGE_STORE_PATH"); v != "" {
		cfg.Flags.RuleUsageStorePath = v
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_STALE_RULE_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Flags.StaleRuleDays = n
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_STALE_RULE_CHECK_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Flags.StaleRuleCheckInterval = d
		}
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH"); v != "" {
		cfg.Flags.DetectorSettingsStorePath = v
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_VPN_INTERFACES"); v != "" {
		cfg.Flags.VPNInterfaces = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_FLAGS_VPN_CONFIDENCE_MULTIPLIER"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Flags.VPNConfidenceMultiplier = f
		}
	}
	if v := os.Getenv("MIKROVIEW_AUTH_STORE_PATH"); v != "" {
		cfg.Auth.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_AUTH_SECURE_COOKIE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Auth.SecureCookie = b
		}
	}
	if v := os.Getenv("MIKROVIEW_AUTH_SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Auth.SessionTTL = d
		}
	}
	if v := os.Getenv("MIKROVIEW_AUTH_SESSION_MAX_LIFETIME"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Auth.SessionMaxLifetime = d
		}
	}
	if v := os.Getenv("MIKROVIEW_ENTITIES_STORE_PATH"); v != "" {
		cfg.Entities.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_COVERAGE_STORE_PATH"); v != "" {
		cfg.Coverage.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_AUDIT_STORE_PATH"); v != "" {
		cfg.Audit.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_SETUP_STORE_PATH"); v != "" {
		cfg.Setup.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_WATCHLIST_STORE_PATH"); v != "" {
		cfg.Watchlist.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_WATCHLIST_MATCH_LOG_PATH"); v != "" {
		cfg.Watchlist.MatchLogPath = v
	}
	if v := os.Getenv("MIKROVIEW_WATCHLIST_MATCH_LOG_CAPACITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Watchlist.MatchLogCapacity = n
		}
	}
	if v := os.Getenv("MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH"); v != "" {
		cfg.Watchlist.SuggestionsStorePath = v
	}
	if v := os.Getenv("MIKROVIEW_WATCHLIST_MATCH_LOG_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Watchlist.MatchLogRetention = d
		}
	}
	if v := os.Getenv("MIKROVIEW_AUTH_TOKENS_STORE_PATH"); v != "" {
		cfg.Auth.TokensStorePath = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_BATCH_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Notify.BatchWindow = d
		}
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_HOST"); v != "" {
		cfg.Notify.SMTP.Host = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Notify.SMTP.Port = n
		}
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_USERNAME"); v != "" {
		cfg.Notify.SMTP.Username = v
	}
	// Password is the one field here worth a secret-via-env path even
	// with a config file in play, same reasoning MIKROVIEW_ABUSEIPDB_KEY
	// already establishes -- a credential doesn't have to sit in
	// config.yaml just because the rest of the block does.
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_PASSWORD"); v != "" {
		cfg.Notify.SMTP.Password = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_TLS_MODE"); v != "" {
		cfg.Notify.SMTP.TLSMode = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_FROM"); v != "" {
		cfg.Notify.SMTP.From = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_SMTP_TO"); v != "" {
		cfg.Notify.SMTP.To = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_PUSHOVER_TOKEN"); v != "" {
		cfg.Notify.Pushover.Token = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_PUSHOVER_USER"); v != "" {
		cfg.Notify.Pushover.User = v
	}
	if v := os.Getenv("MIKROVIEW_NOTIFY_WEBHOOK_URL"); v != "" {
		cfg.Notify.Webhook.URL = v
	}
	// Headers is deliberately not env-configurable: it's a map, not a
	// scalar, same "structured value doesn't map cleanly onto one env
	// var" reasoning Flags.Detectors/Devices/RuleNames/HostNames already
	// give for staying YAML-only.
	if v := os.Getenv("MIKROVIEW_TLS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.TLS.Enabled = b
		}
	}
	if v := os.Getenv("MIKROVIEW_POSTGRES_DSN_FILE"); v != "" {
		cfg.Postgres.DSNFile = v
	}
	if v := os.Getenv("MIKROVIEW_TLS_CERT_FILE"); v != "" {
		cfg.TLS.CertFile = v
	}
	if v := os.Getenv("MIKROVIEW_TLS_KEY_FILE"); v != "" {
		cfg.TLS.KeyFile = v
	}
	if v := os.Getenv("MIKROVIEW_TLS_HOSTS"); v != "" {
		cfg.TLS.Hosts = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_TLS_STORE_PATH"); v != "" {
		cfg.TLS.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_OIDC_ISSUER_URL"); v != "" {
		cfg.OIDC.IssuerURL = v
	}
	if v := os.Getenv("MIKROVIEW_OIDC_CLIENT_ID"); v != "" {
		cfg.OIDC.ClientID = v
	}
	// Secret-via-env, same precedent as MIKROVIEW_NOTIFY_SMTP_PASSWORD/
	// MIKROVIEW_ABUSEIPDB_KEY -- a credential doesn't have to sit in
	// config.yaml just because the rest of the block does.
	if v := os.Getenv("MIKROVIEW_OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDC.ClientSecret = v
	}
	if v := os.Getenv("MIKROVIEW_OIDC_PUBLIC_BASE_URL"); v != "" {
		cfg.OIDC.PublicBaseURL = v
	}
	if v := os.Getenv("MIKROVIEW_OIDC_SCOPES"); v != "" {
		cfg.OIDC.Scopes = parseStringList(v)
	}
	// The access policy. Same list-via-env shape as Scopes above and as
	// TLS.Hosts/Blocklist.Sources -- these four had no override at all,
	// which meant a deployment keeping its whole OIDC block in the
	// environment could set who its provider is but not who is allowed
	// in (#267 finding 21).
	if v := os.Getenv("MIKROVIEW_OIDC_ALLOWED_GROUPS"); v != "" {
		cfg.OIDC.AllowedGroups = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_OIDC_GROUPS_CLAIM"); v != "" {
		cfg.OIDC.GroupsClaim = v
	}
	if v := os.Getenv("MIKROVIEW_OIDC_ALLOWED_EMAILS"); v != "" {
		cfg.OIDC.AllowedEmails = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_OIDC_ALLOWED_EMAIL_DOMAINS"); v != "" {
		cfg.OIDC.AllowedEmailDomains = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_DEVICE_MAC_STORE_PATH"); v != "" {
		cfg.DeviceMAC.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_BLOCKLIST_SOURCES"); v != "" {
		cfg.Blocklist.Sources = parseStringList(v)
	}
	if v := os.Getenv("MIKROVIEW_ENGINE_STORE_PATH"); v != "" {
		cfg.Engine.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_ENGINE_DEFINITIONS_STORE_PATH"); v != "" {
		cfg.Engine.DefinitionsStorePath = v
	}
}

// parseIntList parses a comma-separated list of integers (e.g. a port
// list from an env var). Any single malformed entry invalidates the
// whole value -- like every other env var here, a bad value is ignored
// in favor of whatever was already set, rather than partially applied.
// parseStringList parses a comma-separated list of plain strings (e.g.
// notify.smtp.to's recipient addresses from an env var). Unlike
// parseIntList, there's no format to validate here -- any entry is a
// valid recipient as far as this package is concerned -- so it never
// fails, just splits and trims.
func parseStringList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseIntList(v string) ([]int, bool) {
	parts := strings.Split(v, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func applyFlags(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("mikroview", flag.ContinueOnError)
	syslogTLS := fs.String("syslog-tls", cfg.Listen.SyslogTLS, "syslog TLS listen address, RouterOS remote-protocol=tls (started whenever non-empty, independently of tls.enabled; empty disables it)")
	httpAddr := fs.String("http", cfg.Listen.HTTP, "HTTP listen address")
	httpRedirectAddr := fs.String("http-redirect", cfg.Listen.HTTPRedirect, "HTTP listen address for the redirect-to-HTTPS-only listener (empty disables it)")
	retention := fs.Duration("retention", cfg.Store.Retention, "event retention window")
	maxMemory := cfg.Store.MaxMemory
	fs.Var(&maxMemory, "max-memory", "memory budget for the event ring buffer, e.g. 120MiB (see docs/configuration.md)")
	geoipDB := fs.String("geoip-db", cfg.GeoIP.DBPath, "path to a MaxMind GeoLite2/GeoIP2 Country or City .mmdb file (optional; omit to disable country flags)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Listen.SyslogTLS = *syslogTLS
	cfg.Listen.HTTP = *httpAddr
	cfg.Listen.HTTPRedirect = *httpRedirectAddr
	cfg.Store.Retention = *retention
	cfg.Store.MaxMemory = maxMemory
	cfg.GeoIP.DBPath = *geoipDB
	return nil
}
