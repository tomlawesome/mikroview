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

type Device struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	SourceIP string `yaml:"sourceIp"`
}

type Listen struct {
	SyslogUDP string `yaml:"syslogUdp"`
	SyslogTCP string `yaml:"syslogTcp"`
	HTTP      string `yaml:"http"`
}

type Store struct {
	Retention time.Duration `yaml:"retention"`
	MaxEvents int           `yaml:"maxEvents"`
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

// Notify is entirely optional -- see internal/notify. Left with an
// empty SMTP.Host, notifications are simply never dispatched; no
// default relay is assumed.
type Notify struct {
	// BatchWindow: how often pending flags are flushed to every
	// configured channel -- a fixed interval, not a quiet-period
	// debounce, so a sustained flood of flags during a real incident
	// still gets a bounded max delay before alerting rather than the
	// window continuously resetting. See internal/notify.Dispatcher.
	BatchWindow time.Duration `yaml:"batchWindow"`
	SMTP        SMTP          `yaml:"smtp"`
}

// Auth configures internal/auth's local authentication. Unlike Flags'
// StorePath, StorePath here is not truly optional -- mikroview stays
// fully open (today's behavior) as long as no user account exists, but
// the moment one is created it's required for that account to survive a
// restart, so registration refuses to proceed without it configured.
// See docs/configuration.md's "Authentication" section.
type Auth struct {
	StorePath string `yaml:"storePath"`
	// SecureCookie sets the session cookie's Secure flag. Off by default
	// because mikroview is very commonly deployed over plain HTTP on a
	// trusted LAN -- forcing Secure would silently break login on any
	// non-TLS deployment. Turn this on once you have TLS terminated
	// somewhere in front of mikroview.
	SecureCookie bool `yaml:"secureCookie"`
	// SessionTTL is the idle timeout: a session's expiry slides forward
	// on each authenticated request, so this is "how long you can go
	// without activity before needing to log in again," not a fixed
	// session lifetime.
	SessionTTL time.Duration `yaml:"sessionTTL"`
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
	StorePath              string        `yaml:"storePath"`
	PortScanThreshold      int           `yaml:"portScanThreshold"`
	PortScanWindow         time.Duration `yaml:"portScanWindow"`
	ActivitySpikeThreshold int           `yaml:"activitySpikeThreshold"`
	ActivitySpikeWindow    time.Duration `yaml:"activitySpikeWindow"`
	CriticalPorts          []int         `yaml:"criticalPorts"`
	CriticalPortThreshold  int           `yaml:"criticalPortThreshold"`
	CriticalPortWindow     time.Duration `yaml:"criticalPortWindow"`
	GlobalSpikeMultiplier  float64       `yaml:"globalSpikeMultiplier"`
	GlobalSpikeMinEPS      float64       `yaml:"globalSpikeMinEPS"`

	DistributedBruteForceThreshold int           `yaml:"distributedBruteForceThreshold"`
	DistributedBruteForceWindow    time.Duration `yaml:"distributedBruteForceWindow"`

	OutboundAnomalyThreshold int           `yaml:"outboundAnomalyThreshold"`
	OutboundAnomalyWindow    time.Duration `yaml:"outboundAnomalyWindow"`

	InternalReconThreshold int           `yaml:"internalReconThreshold"`
	InternalReconWindow    time.Duration `yaml:"internalReconWindow"`

	RuleSpikeMultiplier float64       `yaml:"ruleSpikeMultiplier"`
	RuleSpikeMinRate    float64       `yaml:"ruleSpikeMinRate"`
	RuleSpikeWindow     time.Duration `yaml:"ruleSpikeWindow"`

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

type Config struct {
	Listen     Listen     `yaml:"listen"`
	Store      Store      `yaml:"store"`
	GeoIP      GeoIP      `yaml:"geoip"`
	Reputation Reputation `yaml:"reputation"`
	Flags      Flags      `yaml:"flags"`
	Auth       Auth       `yaml:"auth"`
	Notify     Notify     `yaml:"notify"`
	Devices    []Device   `yaml:"devices"`

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
			SyslogUDP: ":1514",
			SyslogTCP: ":1514",
			HTTP:      ":8080",
		},
		Store: Store{
			Retention: 24 * time.Hour,
			MaxEvents: 200_000,
		},
		// Mirrors internal/detect.DefaultConfig() -- kept as separate
		// literal values (rather than importing internal/detect here) so
		// this package stays a dependency-free leaf that every feature
		// package can build on, not the other way around.
		Flags: Flags{
			PortScanThreshold:      15,
			PortScanWindow:         60 * time.Second,
			ActivitySpikeThreshold: 200,
			ActivitySpikeWindow:    60 * time.Second,
			CriticalPorts:          []int{21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729},
			CriticalPortThreshold:  5,
			CriticalPortWindow:     5 * time.Minute,
			GlobalSpikeMultiplier:  4,
			GlobalSpikeMinEPS:      5,

			DistributedBruteForceThreshold: 10,
			DistributedBruteForceWindow:    5 * time.Minute,

			OutboundAnomalyThreshold: 25,
			OutboundAnomalyWindow:    5 * time.Minute,

			InternalReconThreshold: 10,
			InternalReconWindow:    60 * time.Second,

			RuleSpikeMultiplier: 5,
			RuleSpikeMinRate:    0.2,
			RuleSpikeWindow:     60 * time.Second,

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
		},
		Auth: Auth{
			SessionTTL: 24 * time.Hour,
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
	retention := fs.Duration("retention", cfg.Store.Retention, "event retention window")
	maxEvents := fs.Int("max-events", cfg.Store.MaxEvents, "max events held in the ring buffer")
	geoipDB := fs.String("geoip-db", cfg.GeoIP.DBPath, "path to a MaxMind GeoLite2/GeoIP2 Country or City .mmdb file (optional; omit to disable country flags)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Listen.SyslogUDP = *syslogUDP
	cfg.Listen.SyslogTCP = *syslogTCP
	cfg.Listen.HTTP = *httpAddr
	cfg.Store.Retention = *retention
	cfg.Store.MaxEvents = *maxEvents
	cfg.GeoIP.DBPath = *geoipDB
	return nil
}
