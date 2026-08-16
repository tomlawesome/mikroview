// SPDX-License-Identifier: AGPL-3.0-only

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
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
)

var logger = logging.New("detect")

// observeQueueDropLogInterval bounds how often a full observeQueue
// actually logs -- sustained overload is exactly the scenario this
// guards against, so logging every single drop would itself add load
// at the worst possible moment. A periodic summary is enough to make
// an otherwise-invisible "detection silently fell behind" condition
// observable without that cost.
const observeQueueDropLogInterval = 30 * time.Second

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
	// GlobalSpikeWarmupSamples: how many Check() calls the network-wide
	// EMA baseline needs before a flag can reach full confidence -- same
	// role as HostActivityWarmupSamples, see Flag.Confidence.
	GlobalSpikeWarmupSamples int

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
	// RuleSpikeWarmupSamples: how many observations a given rule's own
	// EMA baseline needs before a flag can reach full confidence -- same
	// role as HostActivityWarmupSamples, see Flag.Confidence.
	RuleSpikeWarmupSamples int

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
	HostActivityMultiplier    float64
	HostActivityWarmupSamples int

	// LowSlowScanWindow+... (issue #20): a port scan deliberately paced to
	// stay under PortScanWindow's short-burst threshold -- one new
	// port/host every few minutes rather than fifteen in a minute. Judged
	// over hours, gated by several independent signals rather than one
	// count, so an equally slow but perfectly ordinary pattern (a browser
	// or health check slowly accumulating distinct destinations) doesn't
	// trip it -- see each field's own doc comment.
	LowSlowScanWindow time.Duration
	// LowSlowScanPortThreshold+HostThreshold: destination *breadth*, not
	// just port breadth -- both must independently cross their own
	// threshold before this axis is satisfied.
	LowSlowScanPortThreshold int
	LowSlowScanHostThreshold int
	// LowSlowScanMinObservation: a source must have been under observation
	// (first sample to now) for at least this long before it's eligible to
	// fire at all -- the "no flag from too little history" floor, scaled
	// to this detector's much longer window since a low-and-slow source
	// may generate very few events overall (a sample-count floor like
	// activity-spike's wouldn't mean the same thing here).
	LowSlowScanMinObservation time.Duration
	// LowSlowScanDropRatio: the minimum fraction of this source's tracked
	// attempts (within the window) that were drop/reject rather than
	// accept -- paced scan traffic mostly gets refused; legitimate
	// low-rate access to real services mostly gets accepted.
	LowSlowScanDropRatio float64
	// LowSlowScanBaselineMultiplier: this source's own destination-breadth
	// rate must also clear a multiple of its own EMA baseline (same
	// per-host-baseline technique as activity-spike/#38 -- see
	// host_baseline.go), not just the absolute thresholds above.
	LowSlowScanBaselineMultiplier float64

	// OffHoursStartHour/EndHour (issue #104): the clock window (0-23,
	// server-local time, start inclusive/end exclusive) this detector is
	// willing to fire in at all -- wraps past midnight when Start > End
	// (e.g. 23, 6 means 23:00-06:00). A fixed, operator-set window rather
	// than a per-host-learned "quiet period": see off_hours.go's package
	// doc comment for why. Every hour's baseline is still tracked
	// continuously regardless of this window (see sourceWindow.hourly),
	// so narrowing or widening it later doesn't lose history that was
	// already being collected.
	OffHoursStartHour int
	OffHoursEndHour   int
	// OffHoursMinSampleDays: the hard floor on off_hours.go's per-hour
	// baseline -- that specific hour must have been observed on at least
	// this many distinct prior days before a flag can fire for it at
	// all, no matter how extreme the count. One busy night isn't a
	// baseline; this is what makes that structurally impossible rather
	// than just unlikely. Also doubles as emaConfidence's warmupSamples
	// for this detector: once a hour bucket clears this floor it's
	// already trusted, so confidence beyond that point is driven purely
	// by the z-score.
	OffHoursMinSampleDays int
	// OffHoursMinCount: an absolute floor on top of the z-score/baseline
	// check -- a host that's never been seen at some hour has a
	// near-zero baseline, so even a handful of events there would read
	// as a huge deviation by z-score alone. Mirrors
	// ActivitySpikeThreshold's role alongside HostActivityMultiplier in
	// checkHostActivityBaseline.
	OffHoursMinCount int
	// DeviceStaleAfter (issue #98): how long a configured device's
	// LastSeen may go without updating before DeviceSilenceDetector
	// raises TypeDeviceSilence for it. Needs to sit comfortably above
	// normal syslog gaps (RouterOS doesn't emit a steady heartbeat, just
	// events as they happen) so an ordinarily quiet stretch never false-
	// positives. Zero disables the detector entirely -- see
	// DeviceSilenceDetector.Check.
	DeviceStaleAfter time.Duration

	// VPNInterfaces (issue #105): interface-name patterns -- each
	// matched against store.Event.InInterface with glob syntax (see
	// isVPNInterface in vpn.go, e.g. "wireguard1" for an exact match or
	// "wireguard*" for a prefix match) -- identifying which interfaces
	// correspond to a configured VPN tunnel. RouterOS firewall log lines
	// see a WireGuard tunnel's *inner*, already-decrypted traffic (the
	// peer's tunnel IP as SrcIP, arriving on whatever interface name
	// RouterOS assigns the WireGuard interface), which is exactly what
	// InInterface already captures -- enough to say "this traffic came
	// from an already-authenticated remote peer" without needing
	// anything RouterOS's own API would provide.
	//
	// Consulted by checkHostActivityBaseline (host_baseline.go, feeding
	// TypeActivitySpike) and observeDestSpread (dest_spread.go, feeding
	// TypeOutboundAnomaly/TypeInternalRecon): an anomaly whose triggering
	// event arrived via a VPN-tagged interface is scored more
	// confidently than the identical anomaly arriving via an ordinary
	// LAN interface, since a remote peer that already had to pass
	// WireGuard's own key-based auth to reach the network at all
	// behaving anomalously is a stronger signal than an ordinary LAN
	// device doing the same. See VPNConfidenceMultiplier for exactly how
	// that boost is applied.
	//
	// Empty (the default) matches no interface, so every existing
	// deployment's confidence scoring is completely unchanged until this
	// is explicitly configured -- a safe, backward-compatible default.
	//
	// Deliberately out of scope, both here and everywhere else in
	// mikroview: tracking the WireGuard peer's *outer* UDP endpoint or
	// handshake state (/interface/wireguard/peers). Firewall logs never
	// see that data -- only the router's own API would -- so mikroview
	// can't yet tell "peer roamed to a new IP" (normal for a mobile
	// peer) from "peer's private key was stolen and is now being used
	// from elsewhere" (a real compromise signal). That's blocked on
	// mikroview issue #21 deciding whether/how mikroview talks to the
	// RouterOS API at all.
	VPNInterfaces []string
	// VPNConfidenceMultiplier scales a flag's already-computed
	// confidence score (from emaConfidence or overshootConfidence) when
	// the triggering event's InInterface matches VPNInterfaces -- see
	// vpn.go's vpnBoostConfidence for the full mechanism and the design
	// reasoning for why a post-hoc multiplier (rather than a lowered
	// firing threshold/z-score bar) was chosen. Applied identically at
	// both VPNInterfaces call sites. <= 0 is treated as 1 (no boost),
	// never as "suppress or invert" -- a misconfigured value should
	// never make a VPN-sourced anomaly read as less alarming than an
	// identical LAN one.
	VPNConfidenceMultiplier float64
}

// DefaultConfig returns sensible defaults for a home/small-office
// RouterOS deployment. CriticalPorts covers the services most commonly
// targeted by internet-wide scanning: SSH, Telnet, FTP, SMB, RDP, VNC,
// and RouterOS's own Winbox/API ports (8291, 8728, 8729) -- worth
// watching precisely because they're MikroTik-specific and a common
// target once a scanner has fingerprinted a device as RouterOS.
func DefaultConfig() Config {
	return Config{
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
		RuleSpikeMinRate:       0.2, // events/sec -- ~12/min, below which "5x" isn't meaningful
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

		// 23:00-06:00: a conservative, common-denominator quiet period
		// for a home/small-office network -- see off_hours.go's doc
		// comment for why a fixed window was chosen over a per-host-
		// learned one.
		OffHoursStartHour:     23,
		OffHoursEndHour:       6,
		OffHoursMinSampleDays: 14,
		OffHoursMinCount:      5,

		DeviceStaleAfter: 15 * time.Minute,

		// VPNInterfaces is empty by default -- see its doc comment for
		// why that's the deliberate, backward-compatible no-op starting
		// point. VPNConfidenceMultiplier still gets a sensible default
		// so simply setting VPNInterfaces is enough to opt in without
		// also having to pick a multiplier: 1.5x is a modest boost, not
		// an instant jump to full confidence -- consistent with every
		// other confidence signal in this package being additive
		// evidence, not an override.
		VPNConfidenceMultiplier: 1.5,
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

// observeQueueSize bounds Detector's async detection queue (see Enqueue/
// Run) -- sized to the same tier as main.go's raw syslog channel (4096),
// since Enqueue is offered once per stored event, the same rate as
// ingestion itself (unlike internal/notify's much smaller queue, which
// only receives newly-raised flags, a far rarer event).
const observeQueueSize = 4096

type sourceWindow struct {
	// spikes is a plain event count over ActivitySpikeWindow (see
	// window.go). port_scan's own distinct-destination-port ring used to
	// live here too (a distinctRing[int] named "ports") -- ported onto
	// internal/engine as a shipped declarative definition (issue #405,
	// see internal/engine/shipped_declarative.go); this struct only
	// tracks what activity_spike/off_hours still need.
	spikes *countRing

	lastActivity time.Time

	// Per-host activity baseline (see host_baseline.go): an EMA mean and
	// variance of this source's own event rate, primed on first sight
	// rather than compared against anything until there's a prior value
	// to compare to.
	baseline    float64
	variance    float64
	primed      bool
	sampleCount int

	// hourly is off_hours.go's per-hour-of-day counterpart to
	// baseline/variance above: 24 independent EMA baselines, one per
	// clock hour, each tracking how many events this source typically
	// produces during that specific hour. Unlike baseline/variance
	// (which judge every observation against one rolling-window rate),
	// each entry here only advances once per calendar day -- see
	// hourlyDay/hourlyCount below and checkOffHoursActivity's own doc
	// comment for why daily granularity is what "distinct prior days of
	// history" (sampleDays) actually requires.
	hourly [24]struct {
		baseline   float64
		variance   float64
		sampleDays int
	}
	// hourlyDay is the calendar day (server-local "2006-01-02") each
	// hour bucket is currently accumulating hourlyCount for -- "" if
	// that hour has never been observed. checkOffHoursActivity folds the
	// previous day's hourlyCount into hourly[h]'s EMA (and advances
	// sampleDays) the moment a later day is first seen at that hour,
	// then starts today's count fresh.
	hourlyDay [24]string
	// hourlyCount is today's (hourlyDay[h]'s) running event count for
	// hour h, not yet folded into hourly[h]'s baseline -- this is the
	// "current count" checkOffHoursActivity compares against that
	// baseline, live, as events arrive.
	hourlyCount [24]int
}

// Detector tracks per-source rolling-window state for the port-scan,
// activity-spike, and critical-port detectors, raising flags into fs
// when a threshold is crossed. Observe itself is intended to be called
// only from a single detection-worker goroutine (see Run) -- the same
// single-writer assumption internal/store and internal/device make, so
// like them it takes no lock of its own. Enqueue, in contrast, is safe
// to call from any goroutine (mikroview's ingest goroutine calls it) --
// it only ever hands an event off across a channel to that worker.
type Detector struct {
	cfg      Config
	fs       *flags.Store
	settings *SettingsStore

	// reputation and lookupSlots back the async, best-effort
	// confidence-floor lookups in reputation.go -- see WithReputation.
	// reputation is nil unless WithReputation is called explicitly
	// (never by New/NewWithSettings themselves, so tests never make
	// real network calls by default).
	reputation  reputationLookup
	lookupSlots chan struct{}

	// entities backs observeMailSender's trusted-mail-sender allowlist
	// check (issue #108) -- see WithEntities. nil unless WithEntities is
	// called explicitly (never by New/NewWithSettings themselves, so
	// tests that don't care about the allowlist just see every untagged
	// source flagged, the same "nil is a valid no-op" contract
	// reputation above uses).
	entities *entities.Store
	// knownBad backs the synchronous local-blocklist check in
	// known_bad_ip.go -- see WithKnownBadIPs. nil (the default) is a
	// valid, explicit "not configured" no-op, same convention as
	// reputation above.
	knownBad knownBadIPLookup
	// netclass backs the synchronous, direction-aware confidence
	// reinforcement in netclass.go (issue #114) -- see WithNetClass. nil
	// (the default) is a valid, explicit "not configured" no-op, same
	// convention as knownBad above.
	netclass netClassLookup

	perSource map[string]*sourceWindow
	// criticalHits (critical_port's own per-source attempt-count map)
	// moved to internal/engine as a shipped declarative definition (issue
	// #405) -- criticalPortIPs below is distributed_brute_force's
	// separate, per-port state, not affected by that port.
	criticalPortIPs map[int]*portSources
	destWindows     map[string]*destWindow
	ruleWindows     map[string]*ruleWindow
	lowSlowWindows  map[string]*lowSlowWindow

	// observeQueue backs Enqueue/Run -- see observeQueueSize's doc
	// comment for the sizing rationale.
	observeQueue chan store.Event

	// droppedEvents backs Enqueue's rate-limited overload logging --
	// the gate itself is dropLogGate (see recordDroppedEvent).
	droppedEvents atomic.Uint64
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
		lookupSlots:     make(chan struct{}, reputationLookupConcurrency),
		perSource:       make(map[string]*sourceWindow),
		criticalPortIPs: make(map[int]*portSources),
		destWindows:     make(map[string]*destWindow),
		ruleWindows:     make(map[string]*ruleWindow),
		lowSlowWindows:  make(map[string]*lowSlowWindow),
		observeQueue:    make(chan store.Event, observeQueueSize),
	}
}

// Enqueue hands e off to the detection-worker goroutine (see Run)
// without ever blocking the caller -- a non-blocking select/default
// send, dropping e if the queue is full. mikroview's ingest goroutine
// calls this so a slow or backed-up detection pass never delays event
// storage or WebSocket broadcast, only detection itself -- a dropped
// event is still stored/broadcast normally, it just isn't fed through
// the detectors.
func (d *Detector) Enqueue(e store.Event) {
	select {
	case d.observeQueue <- e:
	default:
		d.recordDroppedEvent()
	}
}

// recordDroppedEvent tracks an Enqueue drop and logs a rate-limited
// summary -- logging every single drop would itself add load during
// exactly the sustained-overload condition this is meant to surface
// (same reasoning internal/syslog/tcp_listener.go's handleTCPConn uses
// to justify never logging a drop at all), but staying silent forever --
// this function's predecessor -- left a real failure mode invisible:
// detection can silently fall behind with zero operator-facing signal,
// unlike a backed-up ingest goroutine, which at least shows up as
// dropped-syslog-packet symptoms elsewhere.
func (d *Detector) recordDroppedEvent() {
	total := d.droppedEvents.Add(1)
	if _, ok := dropLogGate.Allow(); ok {
		logger.Warn(fmt.Sprintf("detection queue full -- %d event(s) dropped from detection so far (still stored/broadcast normally)", total))
	}
}

// dropLogGate implements observeQueueDropLogInterval -- package-level
// rather than per-Detector because it only gates log noise, and there
// is one Detector per process outside tests.
var dropLogGate = logging.NewLimiter(observeQueueDropLogInterval)

// Run drains observeQueue, calling Observe for each event in order,
// until ctx is done. Meant to run in its own goroutine, separate from
// whatever goroutine calls Enqueue -- see Detector's doc comment.
func (d *Detector) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-d.observeQueue:
			d.observeRecovered(e)
		}
	}
}

// observeRecovered isolates Observe's panic-recovery to a single event
// rather than Run's whole lifetime -- recover only unwinds as far as
// the nearest deferring function, so if the defer lived in Run itself
// instead, one bad event would still end Run for good (silently
// stopping all future detection) rather than just being skipped. See
// logging.Recover's doc comment for why this is needed at all: nothing
// else in Go contains a panic in a goroutine like this one.
func (d *Detector) observeRecovered(e store.Event) {
	defer logging.Recover(logger)
	d.Observe(e)
}

// Observe feeds one stored event through every per-event detector.
func (d *Detector) Observe(e store.Event) {
	if e.SrcIP == "" {
		return
	}
	now := e.ReceivedAt

	d.observeScanAndSpike(e, now)
	d.observeLowSlowScan(e, now)
	d.observeOffHours(e, now)

	// critical_port itself moved to internal/engine as a shipped
	// declarative definition (issue #405, see
	// shipped_declarative.go's buildCriticalPortDefinition) -- this gate
	// now only guards distributed_brute_force, which shares the same
	// "critical port, external source" precondition but isn't ported yet.
	srcPublic := isPublic(e.SrcIP)
	if e.DstPort != 0 && isCriticalPort(d.cfg.CriticalPorts, e.DstPort) && srcPublic {
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

		// internal-source -> external-destination on an SMTP port (issue
		// #108) -- always on, unlike the DetectorName-backed checks
		// above/below, since this is deterministic (see
		// observeMailSender's doc comment) rather than a scoped,
		// tunable threshold.
		if isPublic(e.DstIP) && isMailPort(e.DstPort) {
			d.observeMailSender(e, now)
		}
	}

	if e.RuleLabel != "" {
		// Deliberately no "mark the baseline stale" reset here, unlike
		// GlobalSpikeDetector.Check and checkHostActivityBaseline. #267
		// finding 17 proposed adding one for consistency; measured, it
		// makes this detector worse -- see
		// TestRuleSpikeSurvivesADisableEnableCycleWithoutFalsePositives.
		//
		// The difference is where the rate comes from. GlobalSpike is
		// handed an accurate current EPS, so re-priming gives it a
		// correct baseline immediately. This detector derives its rate
		// from a time-windowed hits ring that only fills while it is
		// enabled, so re-priming on the first event after re-enabling
		// primes against a nearly empty ring -- and the ordinary refill
		// back to normal traffic then reads as a spike. low_slow_scan
		// derives its rate the same way and is left alone for the same
		// reason.
		if rs := d.settings.Get(DetectorRuleSpike); rs.Enabled && scopeMatchesRule(rs.Scope, e.RuleLabel) {
			d.observeRuleRate(e, now)
		}
	}

	// repeated_drops moved to internal/engine as a shipped declarative
	// definition (issue #405, see shipped_declarative.go's
	// buildRepeatedDropsDefinition) -- its "locally-hosted destination,
	// refused attempt" gate went with it, expressed as conditions.

	// Local blocklist match (issue #113 Part B) -- deliberately last:
	// see observeKnownBadIP's own doc comment for why its
	// RaiseConfidenceFloor reinforcement pass needs every other
	// detector above to have already run for this same event.
	d.observeKnownBadIP(e, now)

	// Network-class reinforcement (issue #114) -- deliberately after
	// observeKnownBadIP, for the identical reason: both are
	// RaiseConfidenceFloor-only reinforcement passes that need every
	// flag-raising detector above (including known-bad-IP) to have
	// already run for this same event.
	d.observeNetClass(e, now)
}

// observeScanAndSpike is activity_spike's entry point from Observe.
// port_scan used to share this function and sourceWindow's per-source
// state (a distinct-destination-port ring, queried here) -- ported onto
// internal/engine as a shipped declarative definition (issue #405, see
// internal/engine/shipped_declarative.go's buildPortScanDefinition),
// which tracks its own state independently of d.perSource. Renamed doc
// comment aside, this function's remaining behavior (activity_spike's
// per-host EMA baseline check) is unchanged.
func (d *Detector) observeScanAndSpike(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	as := d.settings.Get(DetectorActivitySpike)
	if !as.Enabled || !scopeMatchesHost(as.Scope, e.SrcIP) {
		return
	}

	w, ok := d.perSource[e.SrcIP]
	if !ok {
		if len(d.perSource) >= maxTrackedSources {
			evictOldestByActivity(d.perSource)
		}
		w = &sourceWindow{
			spikes: newCountRing(d.cfg.ActivitySpikeWindow),
		}
		d.perSource[e.SrcIP] = w
	}
	w.lastActivity = now
	w.spikes.Add(now, true)

	spikeCount := w.spikes.Count(now, d.cfg.ActivitySpikeWindow)
	d.checkHostActivityBaseline(w, e.SrcIP, e.SrcCountry, e.InInterface, spikeCount, now)
}

// criticalWindow/observeCriticalPort (critical_port's own per-source
// attempt-count state and its reputation-lookup call site) moved to
// internal/engine as a shipped declarative definition (issue #405, see
// shipped_declarative.go's buildCriticalPortDefinition and main.go's
// engine.ReputationSink wiring for the reputation-lookup counterpart).

// activeWindow is implemented by every per-key detector state struct
// (sourceWindow, destWindow, ruleWindow, lowSlowWindow)
// purely so evictOldestByActivity can be generic over all of them --
// they otherwise share no behavior, just this one field.
type activeWindow interface {
	lastActivityTime() time.Time
}

func (w *sourceWindow) lastActivityTime() time.Time { return w.lastActivity }

// evictOldestByActivity sheds the least-recently-active entries once a
// per-source map is full, shared by every per-key detector state map
// once it hits maxTrackedSources.
//
// The batch-shed reasoning that used to live here now lives in
// internal/evict, alongside the measurements that motivated it -- #285
// found the same evict-back-to-exactly-the-cap pattern still live in
// internal/device and internal/rules, so the remedy is one
// implementation those packages share rather than a third and fourth
// hand-copy. This function stays because it is what adapts the
// activeWindow interface to that helper.
func evictOldestByActivity[V activeWindow](m map[string]V) {
	evict.DownTo(m, len(m)-evict.Batch(len(m)), func(w V) time.Time {
		return w.lastActivityTime()
	})
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
