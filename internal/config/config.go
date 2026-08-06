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
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	SourceIP string `yaml:"sourceIp"`
}

type Listen struct {
	SyslogUDP string `yaml:"syslogUdp"`
	SyslogTCP string `yaml:"syslogTcp"`
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
}

type Store struct {
	Retention time.Duration `yaml:"retention"`
	MaxEvents int           `yaml:"maxEvents"`
}

// Log controls mikroview's own server log output -- see
// internal/logging. Doesn't apply to the CLI recovery commands
// (-list-users, -reset-password's prompts, etc.), which print directly
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
	DBPath string `yaml:"dbPath"`
}

// Reputation is entirely optional -- see internal/reputation. Shodan's
// InternetDB source needs no key and is always used; AbuseIPDB is only
// queried if a key is configured here.
type Reputation struct {
	AbuseIPDBKey string    `yaml:"abuseIPDBKey"`
	GreyNoise    GreyNoise `yaml:"greyNoise"`
}

// GreyNoise configures internal/reputation's second live source (issue
// #113 Part A) -- see internal/reputation.GreyNoiseClient's doc comment
// for why, unlike Shodan's InternetDB, this really does need a key
// despite GreyNoise's own "Community API" marketing suggesting
// otherwise. Empty APIKey means GreyNoise is never queried, same
// "empty means opt-out" convention as Reputation.AbuseIPDBKey --
// mikroview falls back to AbuseIPDB+Shodan alone (today's behavior)
// with nothing else to configure.
type GreyNoise struct {
	APIKey string `yaml:"apiKey"`
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
	// TokensStorePath: where read-only API bearer tokens (issue #101)
	// persist across restarts, as a small JSON file (names + SHA-256
	// hashes, never the raw bearer values). Unlike StorePath above, this
	// one really is optional the way Flags.StorePath is -- an operator
	// who never creates a token doesn't need it, and a missing/unwritable
	// path just means token creation refuses (ErrTokenNotPersisted)
	// rather than mikroview failing to start.
	TokensStorePath string `yaml:"tokensStorePath"`
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
	// from) and internal/detect.StaleRuleDetector (the sweep itself).
	//
	// RuleUsageStorePath persists that usage record so "hasn't fired in
	// 30 days" survives a restart -- same optional-persistence contract
	// as StorePath above (empty disables persistence, not the feature).
	// StaleRuleDays is how long a rule must go quiet before it's
	// considered stale. StaleRuleCheckInterval is how often the sweep
	// re-checks (see main.go's staleRuleCheckInterval-style ticker) --
	// coarse by design, since staleness is judged in days, not seconds.
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

// Blocklist configures internal/blocklist's local IP/CIDR "known-bad"
// matching against a small, vetted menu of free threat-intel feeds
// (issue #113 Part B) -- see that package's own doc comment for the
// full menu, why it's a fixed menu rather than an arbitrary URL field,
// and how the refresh cadence/entry-count cap were decided.
//
// On by default with Spamhaus's DROP+EDROP lists -- the issue's own
// recommended starting point: small, free, no registration, and
// curated specifically to only include netblocks Spamhaus is confident
// are entirely malicious-controlled, a safe "flag on sight" default
// unlike a larger, noisier aggregated list would be. Set sources to an
// empty list (`sources: []`) to disable local blocklist matching
// entirely. Refresh cadence is intentionally not configurable here --
// see internal/blocklist.RefreshInterval's doc comment.
type Blocklist struct {
	// Sources is a list of internal/blocklist.Source values (e.g.
	// "spamhaus_drop", "spamhaus_edrop", "emerging_threats_compromised")
	// -- an unrecognized entry is logged and skipped at startup, not a
	// fatal error, same degrade-not-crash contract as every other
	// optional integration in this codebase.
	Sources []string `yaml:"sources"`
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
	Notify     Notify     `yaml:"notify"`
	TLS        TLS        `yaml:"tls"`
	OIDC       OIDC       `yaml:"oidc"`
	Devices    []Device   `yaml:"devices"`
	DeviceMAC  DeviceMAC  `yaml:"deviceMac"`
	Blocklist  Blocklist  `yaml:"blocklist"`

	// RuleNames/HostNames are optional friendly-display-name maps -- see
	// internal/naming. Keyed by the raw value RouterOS reports (a rule
	// label like "r13", a host IP), same lookup-table shape rather than
	// Devices' structured-record shape since there's nothing else to
	// store per entry.
	RuleNames map[string]string `yaml:"ruleNames"`
	HostNames map[string]string `yaml:"hostNames"`
}

func defaults() Config {
	return Config{
		Listen: Listen{
			SyslogUDP:    ":1514",
			SyslogTCP:    ":1514",
			HTTP:         ":8080",
			HTTPRedirect: ":8081",
		},
		Store: Store{
			Retention: 24 * time.Hour,
			MaxEvents: 200_000,
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
		},
		Auth: Auth{
			StorePath:       DefaultDataDir + "/users.json",
			SessionTTL:      24 * time.Hour,
			SecureCookie:    true,
			TokensStorePath: DefaultDataDir + "/tokens.json",
		},
		Entities: Entities{
			StorePath: DefaultDataDir + "/entities.json",
		},
		TLS: TLS{
			Enabled:   true,
			StorePath: DefaultDataDir + "/tls",
		},
		DeviceMAC: DeviceMAC{
			StorePath: DefaultDataDir + "/mac-registry.json",
		},
		Blocklist: Blocklist{
			// Mirrors internal/blocklist.DefaultSources -- kept as a
			// literal here (rather than importing internal/blocklist)
			// so this package stays a dependency-free leaf, same
			// reasoning Flags already gives for duplicating
			// internal/detect.Config's own defaults.
			Sources: []string{"spamhaus_drop", "spamhaus_edrop"},
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
	cfg := defaults()

	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, fmt.Errorf("loading config file %s: %w", configPath, err)
		}
	}

	applyEnv(&cfg)

	if err := applyFlags(&cfg, args); err != nil {
		return Config{}, err
	}

	return cfg, nil
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
	if v := os.Getenv("MIKROVIEW_LISTEN_SYSLOG_UDP"); v != "" {
		cfg.Listen.SyslogUDP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_SYSLOG_TCP"); v != "" {
		cfg.Listen.SyslogTCP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_HTTP"); v != "" {
		cfg.Listen.HTTP = v
	}
	if v := os.Getenv("MIKROVIEW_LISTEN_HTTP_REDIRECT"); v != "" {
		cfg.Listen.HTTPRedirect = v
	}
	if v := os.Getenv("MIKROVIEW_STORE_RETENTION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Store.Retention = d
		}
	}
	if v := os.Getenv("MIKROVIEW_STORE_MAX_EVENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Store.MaxEvents = n
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
	// Secret-via-env, same precedent as MIKROVIEW_ABUSEIPDB_KEY --
	// a credential doesn't have to sit in config.yaml just because the
	// rest of the block does.
	if v := os.Getenv("MIKROVIEW_GREYNOISE_KEY"); v != "" {
		cfg.Reputation.GreyNoise.APIKey = v
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
	if v := os.Getenv("MIKROVIEW_ENTITIES_STORE_PATH"); v != "" {
		cfg.Entities.StorePath = v
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
	if v := os.Getenv("MIKROVIEW_DEVICE_MAC_STORE_PATH"); v != "" {
		cfg.DeviceMAC.StorePath = v
	}
	if v := os.Getenv("MIKROVIEW_BLOCKLIST_SOURCES"); v != "" {
		cfg.Blocklist.Sources = parseStringList(v)
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
	syslogUDP := fs.String("syslog-udp", cfg.Listen.SyslogUDP, "syslog UDP listen address")
	syslogTCP := fs.String("syslog-tcp", cfg.Listen.SyslogTCP, "syslog TCP listen address")
	httpAddr := fs.String("http", cfg.Listen.HTTP, "HTTP listen address")
	httpRedirectAddr := fs.String("http-redirect", cfg.Listen.HTTPRedirect, "HTTP listen address for the redirect-to-HTTPS-only listener (empty disables it)")
	retention := fs.Duration("retention", cfg.Store.Retention, "event retention window")
	maxEvents := fs.Int("max-events", cfg.Store.MaxEvents, "max events held in the ring buffer")
	geoipDB := fs.String("geoip-db", cfg.GeoIP.DBPath, "path to a MaxMind GeoLite2/GeoIP2 Country or City .mmdb file (optional; omit to disable country flags)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Listen.SyslogUDP = *syslogUDP
	cfg.Listen.SyslogTCP = *syslogTCP
	cfg.Listen.HTTP = *httpAddr
	cfg.Listen.HTTPRedirect = *httpRedirectAddr
	cfg.Store.Retention = *retention
	cfg.Store.MaxEvents = *maxEvents
	cfg.GeoIP.DBPath = *geoipDB
	return nil
}
