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
	cfg Config
	fs  *flags.Store

	perSource       map[string]*sourceWindow
	criticalHits    map[string][]time.Time
	criticalPortIPs map[int]*portSources
	destWindows     map[string]*destWindow
	ruleWindows     map[string]*ruleWindow
	dropPairs       map[string][]time.Time
}

func New(cfg Config, fs *flags.Store) *Detector {
	return &Detector{
		cfg:             cfg,
		fs:              fs,
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
		d.observeCriticalPort(e, now)
		d.observeDistributedBruteForce(e, now)
	}

	if !srcPublic && e.DstIP != "" {
		// source is on the LAN -- track where it's going, split by
		// whether the destination is also internal (recon/lateral
		// movement) or external (possible C2/exfiltration).
		d.observeDestSpread(e, now)
	}

	if e.RuleLabel != "" {
		d.observeRuleRate(e, now)
	}

	if e.DstIP != "" && e.DstPort != 0 && !isPublic(e.DstIP) && (e.Action == store.ActionDrop || e.Action == store.ActionReject) {
		// destination is a locally-hosted service, and this attempt was
		// refused -- track repeats regardless of whether the source is
		// internal or external, unlike the critical-port detector this
		// isn't restricted to a curated port list or to external sources.
		d.observeRepeatedDrops(e, now)
	}
}

func (d *Detector) observeScanAndSpike(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
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
		if !s.at.Before(spikeCutoff) {
			spikeCount++
		}
		if !s.at.Before(scanCutoff) && s.port != 0 {
			distinctPorts[s.port] = struct{}{}
		}
	}

	d.checkHostActivityBaseline(w, e.SrcIP, spikeCount, now)
	if len(distinctPorts) >= d.cfg.PortScanThreshold {
		d.fs.Add(flags.TypePortScan, e.SrcIP,
			fmt.Sprintf("%d distinct destination ports in %s", len(distinctPorts), d.cfg.PortScanWindow), now)
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
		d.fs.Add(flags.TypeCriticalPort, e.SrcIP,
			fmt.Sprintf("%d attempts against port %d in %s", len(hits), e.DstPort, d.cfg.CriticalPortWindow), now)
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
