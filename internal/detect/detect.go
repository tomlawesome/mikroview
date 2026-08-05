// Package detect watches the ingested event stream for a small set of
// behavioral patterns worth a human's attention -- a source scanning many
// ports, a source generating an unusual volume of traffic, repeated
// attempts against a critical service port from outside the LAN, and a
// sudden spike in overall traffic. It's an "interrogation helper," not an
// intrusion prevention system: every detector only ever raises a flag
// (see internal/flags) for a human to look at and clear. Nothing here
// blocks, drops, or otherwise acts on traffic.
package detect

import (
	"fmt"
	"net"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// Config holds every detector's tunable thresholds, sourced from
// internal/config so an operator can adjust them (or the critical port
// list) for their own network without a code change.
type Config struct {
	PortScanThreshold      int
	PortScanWindow         time.Duration
	ActivitySpikeThreshold int
	ActivitySpikeWindow    time.Duration
	CriticalPorts          []int
	CriticalPortThreshold  int
	CriticalPortWindow     time.Duration
	GlobalSpikeMultiplier  float64
	GlobalSpikeMinEPS      float64

	// DistributedBruteForceThreshold+Window: the inverse of the
	// critical-port detector -- many distinct external sources hitting
	// the *same* critical port, rather than one source hitting it
	// repeatedly. The signature of a botnet/credential-stuffing campaign
	// against one service.
	DistributedBruteForceThreshold int
	DistributedBruteForceWindow    time.Duration

	// OutboundAnomalyThreshold+Window: a LAN source contacting an
	// unusual number of distinct external destinations -- one of the
	// strongest signals of a compromised/malware-infected device
	// (C2 beaconing, botnet participation).
	OutboundAnomalyThreshold int
	OutboundAnomalyWindow    time.Duration

	// InternalReconThreshold+Window: a LAN source contacting an unusual
	// number of distinct *internal* destinations -- a network sweep,
	// the classic lateral-movement signature of an attacker who already
	// has a foothold on the LAN.
	InternalReconThreshold int
	InternalReconWindow    time.Duration

	// RuleSpikeMultiplier+MinRate+Window: a firewall rule firing at a
	// large multiple of its own historical rate -- same EMA-baseline
	// technique as the global spike detector, applied per rule instead
	// of network-wide, so a normally-quiet rule suddenly lighting up is
	// visible even if it doesn't move the network-wide total.
	RuleSpikeMultiplier float64
	RuleSpikeMinRate    float64
	RuleSpikeWindow     time.Duration

	// RepeatedDropsThreshold+Window: the same (source, destination port)
	// pair repeatedly getting dropped/rejected against a locally-hosted
	// service. Aimed at self-hosters: often this is a misconfigured
	// port-forward (the real client keeps retrying a port that's not
	// actually open the way they think), not necessarily an attack --
	// framed as "worth a look," not "critical," in the UI.
	RepeatedDropsThreshold int
	RepeatedDropsWindow    time.Duration

	// HostActivityMultiplier+WarmupSamples: per-host adaptive baseline for
	// activity-spike, replacing a single fixed threshold with each
	// source's own EMA baseline (same technique GlobalSpikeMultiplier
	// uses network-wide -- see host_baseline.go). ActivitySpikeThreshold
	// above still applies as an absolute floor: a host's rate must clear
	// both its own baseline by this multiple *and* the floor, so a nearly
	// idle host doesn't "spike" from one extra event. WarmupSamples is
	// how many observations a host needs before a flag can reach full
	// confidence -- see Flag.Confidence.
	HostActivityMultiplier   float64
	HostActivityWarmupSamples int
}

// DefaultConfig returns sensible defaults for a home/small-office
// RouterOS deployment. CriticalPorts covers the services most commonly
// targeted by internet-wide scanning: SSH, Telnet, FTP, SMB, RDP, VNC,
// and RouterOS's own Winbox/API ports (8291, 8728, 8729) -- worth
// watching precisely because they're MikroTik-specific and a common
// target once a scanner has fingerprinted a device as RouterOS.
func DefaultConfig() Config {
	return Config{
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
		RuleSpikeMinRate:    0.2, // events/sec -- ~12/min, below which "5x" isn't meaningful
		RuleSpikeWindow:     60 * time.Second,

		RepeatedDropsThreshold: 10,
		RepeatedDropsWindow:    15 * time.Minute,

		HostActivityMultiplier:    3,
		HostActivityWarmupSamples: 20,
	}
}

// maxTrackedSources bounds the per-source rolling-window state the same
// way every other buffer in mikroview has an explicit ceiling (see
// internal/store's ring buffer, internal/flags' maxFlags) -- without it,
// a scan using many spoofed or ephemeral source IPs could grow this
// state without bound. The least-recently-active source is evicted first
// once the cap is reached. A var rather than a const so tests can shrink
// it without needing thousands of distinct source IPs.
var maxTrackedSources = 4096

type sample struct {
	at   time.Time
	port int
}

type sourceWindow struct {
	samples      []sample
	lastActivity time.Time

	// Per-host activity baseline (see host_baseline.go): an EMA mean and
	// variance of this source's own event rate, primed on first sight
	// rather than compared against anything until there's a prior value
	// to compare to.
	baseline    float64
	variance    float64
	primed      bool
	sampleCount int
}

// Detector tracks per-source rolling-window state for the port-scan,
// activity-spike, and critical-port detectors, raising flags into fs
// when a threshold is crossed. It's intended to be called only from
// mikroview's single ingest goroutine (see main.go) -- the same
// single-writer assumption internal/store and internal/device make, so
// like them it takes no lock of its own.
type Detector struct {
	cfg      Config
	fs       *flags.Store
	settings *SettingsStore

	perSource       map[string]*sourceWindow
	criticalHits    map[string][]time.Time
	criticalPortIPs map[int]*portSources
	destWindows     map[string]*destWindow
	ruleWindows     map[string]*ruleWindow
	dropPairs       map[string][]time.Time
}

// New constructs a Detector with every detector enabled and unscoped --
// see NewWithSettings for per-detector on/off + scope control (issue
// #44). Kept for callers (and the ~30 existing tests) that don't need
// that control.
func New(cfg Config, fs *flags.Store) *Detector {
	return NewWithSettings(cfg, fs, AllEnabledSettingsStore())
}

// NewWithSettings is New, but backed by an explicit, live, mutable
// SettingsStore -- Observe consults it on every call, so toggling a
// detector on/off or narrowing its scope via settings.Set takes effect
// on the very next event, no restart needed.
func NewWithSettings(cfg Config, fs *flags.Store, settings *SettingsStore) *Detector {
	return &Detector{
		cfg:             cfg,
		fs:              fs,
		settings:        settings,
		perSource:       make(map[string]*sourceWindow),
		criticalHits:    make(map[string][]time.Time),
		criticalPortIPs: make(map[int]*portSources),
		destWindows:     make(map[string]*destWindow),
		ruleWindows:     make(map[string]*ruleWindow),
		dropPairs:       make(map[string][]time.Time),
	}
}

// Observe feeds one stored event through every per-event detector.
func (d *Detector) Observe(e store.Event) {
	if e.SrcIP == "" {
		return
	}
	now := e.ReceivedAt

	d.observeScanAndSpike(e, now)

	srcPublic := isPublic(e.SrcIP)
	if e.DstPort != 0 && isCriticalPort(d.cfg.CriticalPorts, e.DstPort) && srcPublic {
		if cp := d.settings.Get(DetectorCriticalPort); cp.Enabled &&
			scopeMatchesHost(cp.Scope, e.SrcIP) && scopeMatchesPort(cp.Scope, e.DstPort) {
			d.observeCriticalPort(e, now)
		}
		if dbf := d.settings.Get(DetectorDistributedBruteForce); dbf.Enabled &&
			scopeMatchesHost(dbf.Scope, e.SrcIP) && scopeMatchesPort(dbf.Scope, e.DstPort) {
			d.observeDistributedBruteForce(e, now)
		}
	}

	if !srcPublic && e.DstIP != "" {
		// source is on the LAN -- track where it's going, split by
		// whether the destination is also internal (recon/lateral
		// movement) or external (possible C2/exfiltration). Gated inside
		// observeDestSpread itself (outbound-anomaly and internal-recon
		// are independently toggleable but share window state).
		d.observeDestSpread(e, now)
	}

	if e.RuleLabel != "" {
		if rs := d.settings.Get(DetectorRuleSpike); rs.Enabled && scopeMatchesRule(rs.Scope, e.RuleLabel) {
			d.observeRuleRate(e, now)
		}
	}

	if e.DstIP != "" && e.DstPort != 0 && !isPublic(e.DstIP) && (e.Action == store.ActionDrop || e.Action == store.ActionReject) {
		// destination is a locally-hosted service, and this attempt was
		// refused -- track repeats regardless of whether the source is
		// internal or external, unlike the critical-port detector this
		// isn't restricted to a curated port list or to external sources.
		if rd := d.settings.Get(DetectorRepeatedDrops); rd.Enabled &&
			scopeMatchesHost(rd.Scope, e.SrcIP) && scopeMatchesPort(rd.Scope, e.DstPort) {
			d.observeRepeatedDrops(e, now)
		}
	}
}

func (d *Detector) observeScanAndSpike(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	// Independently toggleable even though they share sourceWindow/
	// w.samples below -- both consulted once up front so a detector
	// that's off contributes no work beyond this pair of settings
	// lookups, and short-circuits entirely if neither wants this source.
	ps := d.settings.Get(DetectorPortScan)
	as := d.settings.Get(DetectorActivitySpike)
	psActive := ps.Enabled && scopeMatchesHost(ps.Scope, e.SrcIP)
	asActive := as.Enabled && scopeMatchesHost(as.Scope, e.SrcIP)
	if !psActive && !asActive {
		return
	}

	w, ok := d.perSource[e.SrcIP]
	if !ok {
		if len(d.perSource) >= maxTrackedSources {
			d.evictOldestSource()
		}
		w = &sourceWindow{}
		d.perSource[e.SrcIP] = w
	}
	if !asActive {
		// Mark the baseline stale rather than leaving it be: w.primed
		// otherwise stays true from whenever activity-spike was last
		// active, so the *next* time it's active again,
		// checkHostActivityBaseline would instantly compare against a
		// baseline that's since gone stale instead of cleanly re-priming
		// (see that function's own w.primed handling).
		w.primed = false
	}
	w.lastActivity = now
	w.samples = append(w.samples, sample{at: now, port: e.DstPort})

	// Prune to the larger of the two windows this state feeds, then
	// compute both metrics from what's left -- a straightforward O(window
	// size) scan per event rather than incremental counters, which is
	// plenty fast at the traffic volumes this tool is scoped for (see
	// internal/store's package doc on its own default capacity).
	window := d.cfg.PortScanWindow
	if d.cfg.ActivitySpikeWindow > window {
		window = d.cfg.ActivitySpikeWindow
	}
	cutoff := now.Add(-window)
	i := 0
	for i < len(w.samples) && w.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		w.samples = w.samples[i:]
	}

	spikeCutoff := now.Add(-d.cfg.ActivitySpikeWindow)
	scanCutoff := now.Add(-d.cfg.PortScanWindow)
	spikeCount := 0
	distinctPorts := make(map[int]struct{})
	for _, s := range w.samples {
		if asActive && !s.at.Before(spikeCutoff) {
			spikeCount++
		}
		if psActive && !s.at.Before(scanCutoff) && s.port != 0 && scopeMatchesPort(ps.Scope, s.port) {
			distinctPorts[s.port] = struct{}{}
		}
	}

	// Skipped entirely while inactive, rather than kept warm: the EMA
	// baseline self-protects on re-prime (see checkHostActivityBaseline
	// -- the first call after w.primed resets only primes, it never
	// fires), so re-priming after a period of being off is the safer
	// behavior, not a gap.
	if asActive {
		d.checkHostActivityBaseline(w, e.SrcIP, spikeCount, now)
	}
	if psActive && len(distinctPorts) >= d.cfg.PortScanThreshold {
		d.fs.AddWithConfidence(flags.TypePortScan, e.SrcIP,
			fmt.Sprintf("%d distinct destination ports in %s", len(distinctPorts), d.cfg.PortScanWindow),
			overshootConfidence(len(distinctPorts), d.cfg.PortScanThreshold), now)
	}
}

func (d *Detector) observeCriticalPort(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}
	hits, ok := d.criticalHits[e.SrcIP]
	if !ok && len(d.criticalHits) >= maxTrackedSources {
		d.evictOldestCriticalSource()
	}
	hits = append(hits, now)

	cutoff := now.Add(-d.cfg.CriticalPortWindow)
	i := 0
	for i < len(hits) && hits[i].Before(cutoff) {
		i++
	}
	hits = hits[i:]
	d.criticalHits[e.SrcIP] = hits

	if len(hits) >= d.cfg.CriticalPortThreshold {
		d.fs.AddWithConfidence(flags.TypeCriticalPort, e.SrcIP,
			fmt.Sprintf("%d attempts against port %d in %s", len(hits), e.DstPort, d.cfg.CriticalPortWindow),
			overshootConfidence(len(hits), d.cfg.CriticalPortThreshold), now)
	}
}

func (d *Detector) evictOldestSource() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, w := range d.perSource {
		if first || w.lastActivity.Before(oldest) {
			oldestKey, oldest, first = k, w.lastActivity, false
		}
	}
	if oldestKey != "" {
		delete(d.perSource, oldestKey)
	}
}

func (d *Detector) evictOldestCriticalSource() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, hits := range d.criticalHits {
		if len(hits) == 0 {
			continue
		}
		last := hits[len(hits)-1]
		if first || last.Before(oldest) {
			oldestKey, oldest, first = k, last, false
		}
	}
	if oldestKey != "" {
		delete(d.criticalHits, oldestKey)
	}
}

func isCriticalPort(ports []int, p int) bool {
	for _, cp := range ports {
		if cp == p {
			return true
		}
	}
	return false
}

// isTrackableConnState reports whether e should count toward the scan/
// spike/recon/critical-port/distributed-brute-force detectors below.
// RouterOS commonly logs both directions of an established connection on
// a single stateful accept rule -- without this filter, a busy server's
// ordinary *return* traffic (many distinct client ephemeral ports, many
// distinct clients) trivially crosses thresholds meant to catch new
// connection attempts, producing false positives on any host that's just
// legitimately busy (see the flag detail these detectors' Add calls
// write, and mikroview issue #35). Empty ConnState -- a log line without
// one, or one routeros.Parse couldn't recognize -- is treated as
// trackable rather than discarded, so setups that don't log connection
// state at all keep today's behavior.
func isTrackableConnState(e store.Event) bool {
	return e.ConnState == "" || e.ConnState == "new"
}

// isPublic mirrors the same small check internal/geoip and
// internal/reputation each keep their own private copy of, rather than
// sharing one -- consistent with how this codebase already does it.
func isPublic(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return !ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}
