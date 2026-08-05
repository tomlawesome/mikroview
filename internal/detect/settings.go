package detect

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/tomlawesome/mikroview/internal/store"
)

// DetectorName identifies one of the 9 behavioral detectors for settings
// purposes. Deliberately a separate enum from flags.Type (not reused
// directly) even though the string values match 1:1 today -- settings
// are keyed by detector, and "detector" and "flag type" are only the
// same thing by coincidence today; keeping them distinct means a
// detector that raises zero or multiple flag types never has to
// renegotiate this file's shape.
type DetectorName string

const (
	DetectorPortScan              DetectorName = "port_scan"
	DetectorActivitySpike         DetectorName = "activity_spike"
	DetectorCriticalPort          DetectorName = "critical_port"
	DetectorGlobalSpike           DetectorName = "global_spike"
	DetectorDistributedBruteForce DetectorName = "distributed_brute_force"
	DetectorOutboundAnomaly       DetectorName = "outbound_anomaly"
	DetectorInternalRecon         DetectorName = "internal_recon"
	DetectorRuleSpike             DetectorName = "rule_spike"
	DetectorRepeatedDrops         DetectorName = "repeated_drops"
	DetectorLowSlowScan           DetectorName = "low_slow_scan"
)

// AllDetectorNames is the canonical, stable-ordered list of all 10 --
// used to seed defaults and so the API always reports every detector
// even if only some have been customized.
var AllDetectorNames = []DetectorName{
	DetectorPortScan, DetectorActivitySpike, DetectorCriticalPort,
	DetectorGlobalSpike, DetectorDistributedBruteForce, DetectorOutboundAnomaly,
	DetectorInternalRecon, DetectorRuleSpike, DetectorRepeatedDrops,
	DetectorLowSlowScan,
}

// IsValidDetectorName reports whether n is one of AllDetectorNames.
func IsValidDetectorName(n DetectorName) bool {
	for _, name := range AllDetectorNames {
		if name == n {
			return true
		}
	}
	return false
}

// ListMode selects allow-vs-deny semantics for one Scope list axis.
// The zero value ("") behaves like ListModeAllow on an empty list --
// mode is irrelevant when the list itself is empty, since
// allow-of-nothing would suppress everything.
type ListMode string

const (
	ListModeAllow ListMode = "allow"
	ListModeDeny  ListMode = "deny"
)

func isValidListMode(m ListMode) bool {
	return m == "" || m == ListModeAllow || m == ListModeDeny
}

// Scope restricts which events a detector reacts to, beyond its own
// threshold logic. One deliberate superset struct covering every
// detector's possible restriction axis (hosts, ports, source
// classification, rule labels) rather than nine bespoke structs -- each
// detector's own evaluation code consults only the fields meaningful to
// its own signature and ignores the rest, the same way store.Query
// carries many optional fields not every query path uses. Multiple
// active axes on one detector combine with AND. Within one axis, Mode ==
// ListModeDeny excludes a match; ListModeAllow (or unset, on a non-empty
// list) means only listed entries are admitted.
//
// Per-detector field usage (see each detector's own evaluation code for
// the exact enforcement -- also documented in docs/configuration.md):
//   - PortScan: Hosts/HostsMode + Classification restrict which SOURCE
//     IPs are tracked at all. Ports/PortsMode restricts which distinct
//     destination ports count toward the port-scan threshold, not which
//     events are tracked at all (see observeScanAndSpike). Rules ignored.
//   - ActivitySpike: Hosts/HostsMode + Classification restrict source,
//     independently of PortScan even though the two share per-source
//     window state. Ports, Rules ignored.
//   - CriticalPort: Hosts/HostsMode + Classification restrict source.
//     Ports/PortsMode restricts the *effective* subset of the global
//     Config.CriticalPorts list this instance reacts to (layered on top
//     of, not instead of, Config.CriticalPorts). Rules ignored.
//   - DistributedBruteForce: same as CriticalPort -- Hosts/Classification
//     restrict which source IPs count toward a port's distinct-source
//     total; Ports/PortsMode restricts the effective critical-port
//     subset. Rules ignored.
//   - OutboundAnomaly, InternalRecon: Hosts/HostsMode restricts which
//     SOURCE IPs (always LAN hosts by design) are watched.
//     Classification, Ports, Rules ignored -- the source is already
//     always-internal.
//   - RuleSpike: Rules/RulesMode restricts which rule labels this
//     detector reacts to. Hosts, Ports, Classification ignored -- not
//     keyed by any host.
//   - RepeatedDrops: Hosts/HostsMode restricts source IP, Ports/PortsMode
//     restricts destination port -- both meaningful, AND'd.
//     Classification, Rules ignored.
//   - LowSlowScan: same as PortScan -- Hosts/HostsMode + Classification
//     restrict which SOURCE IPs are tracked; Ports/PortsMode restricts
//     which distinct destination ports count toward its own breadth
//     threshold. Rules ignored.
//   - GlobalSpike: every Scope field ignored; only Settings.Enabled
//     applies (network-wide aggregate, not keyed by anything
//     per-source). Scope is still present for structural uniformity
//     (one type, one JSON/YAML shape across all 9), not because it does
//     anything.
//
// Hosts entries accept a bare IP or a CIDR, mirroring store.Query.IP's
// existing convention.
type Scope struct {
	Hosts          []string    `json:"hosts,omitempty"`
	HostsMode      ListMode    `json:"hostsMode,omitempty"`
	Ports          []int       `json:"ports,omitempty"`
	PortsMode      ListMode    `json:"portsMode,omitempty"`
	Classification store.Scope `json:"classification,omitempty"`
	Rules          []string    `json:"rules,omitempty"`
	RulesMode      ListMode    `json:"rulesMode,omitempty"`
}

// ValidateScope rejects an unrecognized mode or classification value --
// the API layer's guard against a malformed request being silently
// stored as "no restriction" (every field's zero value).
func ValidateScope(sc Scope) error {
	if !isValidListMode(sc.HostsMode) {
		return fmt.Errorf("invalid hostsMode %q", sc.HostsMode)
	}
	if !isValidListMode(sc.PortsMode) {
		return fmt.Errorf("invalid portsMode %q", sc.PortsMode)
	}
	if !isValidListMode(sc.RulesMode) {
		return fmt.Errorf("invalid rulesMode %q", sc.RulesMode)
	}
	switch sc.Classification {
	case store.ScopeAny, store.ScopeInternal, store.ScopeExternal:
	default:
		return fmt.Errorf("invalid classification %q", sc.Classification)
	}
	return nil
}

// Settings is one detector's live on/off + scope configuration -- the
// unit both config.yaml's startup defaults and the live JSON-persisted
// override store deal in.
type Settings struct {
	Enabled bool  `json:"enabled"`
	Scope   Scope `json:"scope"`
}

// scopeMatchesHost reports whether ip satisfies both sc's host list and
// its source classification restriction (AND'd, per issue #44's
// decision that multiple active axes combine with AND).
func scopeMatchesHost(sc Scope, ip string) bool {
	return matchesHostList(sc.Hosts, sc.HostsMode, ip) && classificationMatches(sc.Classification, ip)
}

func scopeMatchesPort(sc Scope, port int) bool {
	return matchesIntList(sc.Ports, sc.PortsMode, port)
}

func scopeMatchesRule(sc Scope, rule string) bool {
	return matchesStringList(sc.Rules, sc.RulesMode, rule)
}

func matchesHostList(list []string, mode ListMode, ip string) bool {
	if len(list) == 0 {
		return true
	}
	hit := false
	for _, entry := range list {
		if hostEntryMatches(entry, ip) {
			hit = true
			break
		}
	}
	if mode == ListModeDeny {
		return !hit
	}
	return hit
}

// hostEntryMatches parses entry fresh per call rather than caching a
// compiled form -- consistent with observeScanAndSpike's existing
// "O(window) scan per event... plenty fast at the traffic volumes this
// tool is scoped for" precedent; Hosts lists are expected to be small
// (tens of entries at most).
func hostEntryMatches(entry, ip string) bool {
	if _, ipNet, err := net.ParseCIDR(entry); err == nil {
		parsed := net.ParseIP(ip)
		return parsed != nil && ipNet.Contains(parsed)
	}
	return entry == ip
}

func matchesIntList(list []int, mode ListMode, v int) bool {
	if len(list) == 0 {
		return true
	}
	hit := false
	for _, entry := range list {
		if entry == v {
			hit = true
			break
		}
	}
	if mode == ListModeDeny {
		return !hit
	}
	return hit
}

func matchesStringList(list []string, mode ListMode, v string) bool {
	if len(list) == 0 {
		return true
	}
	hit := false
	for _, entry := range list {
		if entry == v {
			hit = true
			break
		}
	}
	if mode == ListModeDeny {
		return !hit
	}
	return hit
}

func classificationMatches(scope store.Scope, ip string) bool {
	if scope == store.ScopeAny {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	public := isPublic(ip)
	if scope == store.ScopeInternal {
		return !public
	}
	return public // store.ScopeExternal
}

// DefaultSettingsMap returns every detector enabled and unscoped -- the
// seed used both by AllEnabledSettingsStore and, in main.go, as the
// starting point config.yaml's flags.detectors entries are layered onto.
func DefaultSettingsMap() map[DetectorName]Settings {
	m := make(map[DetectorName]Settings, len(AllDetectorNames))
	for _, n := range AllDetectorNames {
		m[n] = Settings{Enabled: true}
	}
	return m
}

// SettingsStore holds every detector's live, mutable on/off + scope
// settings, keyed by DetectorName. Structurally a clone of
// flags.Store's pattern (mutex + optional atomic-write JSON
// persistence) -- see internal/flags/store.go. The zero value is not
// usable; construct with OpenSettingsStore.
type SettingsStore struct {
	mu     sync.RWMutex
	path   string
	byName map[DetectorName]Settings
}

// OpenSettingsStore loads path if it exists (a missing file is the
// expected first-run case, not an error) and returns a Store that
// persists to it from then on. An empty path is the expected
// "persistence not configured" case: a fully usable, in-memory-only
// Store is returned. A malformed file is treated as empty rather than
// failing -- a corrupted settings file should never block mikroview
// from starting. Either way the returned Store is always safe to use
// unconditionally; a non-nil error is only ever informational, for the
// caller to log.
//
// seed supplies each detector's config.yaml-derived starting point. On
// a clean deployment (no file yet) seed is used verbatim. Any detector
// name found on disk overrides its seed entry; any name in seed but
// absent from disk (a detector added in a later mikroview version, or
// simply never toggled) is filled from seed -- so every one of
// AllDetectorNames always has an entry.
func OpenSettingsStore(path string, seed map[DetectorName]Settings) (*SettingsStore, error) {
	s := &SettingsStore{path: path, byName: make(map[DetectorName]Settings, len(seed))}
	for name, st := range seed {
		s.byName[name] = st
	}
	if path == "" {
		return s, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return s, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}

	var onDisk map[DetectorName]Settings
	if err := json.Unmarshal(data, &onDisk); err != nil {
		return s, err
	}
	for name, st := range onDisk {
		s.byName[name] = st
	}
	return s, nil
}

// AllEnabledSettingsStore backs New/NewGlobalSpikeDetector's original
// signatures -- every detector on, unscoped, unpersisted. Preserves
// today's behavior for the ~30 existing tests and any caller that
// doesn't need per-detector settings at all.
func AllEnabledSettingsStore() *SettingsStore {
	s, _ := OpenSettingsStore("", DefaultSettingsMap())
	return s
}

// Get returns name's current settings, or the zero value (disabled,
// unscoped) if name is unknown. The returned Settings is a value copy;
// its slice fields are never mutated in place by Set (which always
// replaces the whole entry), so a caller holding onto a Get result is
// always looking at one consistent, never-changes-underneath-it
// snapshot without needing its own locking.
func (s *SettingsStore) Get(name DetectorName) Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byName[name]
}

// Set replaces name's settings wholesale and persists if configured.
func (s *SettingsStore) Set(name DetectorName, settings Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byName[name] = settings
	s.persistLocked()
}

// List returns every detector's current settings.
func (s *SettingsStore) List() map[DetectorName]Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[DetectorName]Settings, len(s.byName))
	for name, st := range s.byName {
		out[name] = st
	}
	return out
}

// persistLocked writes the current state to disk if persistence is
// configured. Write failures are swallowed rather than surfaced to
// Set's caller: the in-memory state (which every read goes through)
// stays correct either way, so a transient disk issue degrades to
// "won't survive a restart right now" rather than breaking live use.
func (s *SettingsStore) persistLocked() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s.byName, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, s.path) // same filesystem, so this is atomic
}
