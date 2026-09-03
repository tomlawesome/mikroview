// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"time"
)

// This file is where internal/detect.Config landed when that package's
// engine machinery was deleted (issue #405). It is the same struct,
// moved rather than reworked: every field, its default and its doc
// comment are unchanged, because they are the values every shipped
// definition's params are seeded from and changing any of them would be
// a retuning riding along with a deletion.
//
// What did change is what it is FOR. In internal/detect it was live
// configuration, read on every event by whichever detector consulted it.
// Here it is a seed: SeedShippedDefinitions turns it into each shipped
// definition's Params once, and from then on the definition's own params
// are what evaluation reads. The struct is a migration boundary, not a
// runtime one.

// DetectorDefaults holds every shipped detector's tunable thresholds, sourced from
// internal/config so an operator can adjust them (or the critical port
// list) for their own network without a code change.
type DetectorDefaults struct {
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

// DefaultDetectorDefaults returns sensible defaults for a home/small-office
// RouterOS deployment. CriticalPorts covers the services most commonly
// targeted by internet-wide scanning: SSH, Telnet, FTP, SMB, RDP, VNC,
// and RouterOS's own Winbox/API ports (8291, 8728, 8729) -- worth
// watching precisely because they're MikroTik-specific and a common
// target once a scanner has fingerprinted a device as RouterOS.
func DefaultDetectorDefaults() DetectorDefaults {
	return DetectorDefaults{
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

// DetectorSettings is one shipped detector's enabled + scope pair --
// internal/detect.Settings, moved here when that package was deleted.
//
// It is deliberately NOT the shape anything evaluates. A definition's
// enabled/scope live on the Definition itself (definition.go); this type
// exists for one caller, SeedShippedDefinitions, which layers
// config.yaml's own per-detector toggles over the shipped defaults on
// every boot. That is a seeding path, which is why an "operator
// settings" type survives the deletion of the store that used to own it.
type DetectorSettings struct {
	Enabled bool  `json:"enabled"`
	Scope   Scope `json:"scope"`
}

// DefaultDetectorSettings returns every shipped definition enabled and
// unscoped -- internal/detect.DefaultSettingsMap's replacement, and the
// starting point config.yaml's flags.detectors entries are layered onto.
func DefaultDetectorSettings() map[string]DetectorSettings {
	m := make(map[string]DetectorSettings, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		m[sd.id] = DetectorSettings{Enabled: true}
	}
	return m
}

// ShippedDefinitionIDs is every id the shipped catalogue defines, in a
// stable order -- what internal/detect.AllDetectorNames was for, grown to
// cover the five definitions that package had no name for at all.
func ShippedDefinitionIDs() []string {
	out := make([]string, 0, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		out = append(out, sd.id)
	}
	return out
}

// IsShippedDefinitionID reports whether id names a definition in this
// binary's shipped catalogue.
func IsShippedDefinitionID(id string) bool {
	for _, sd := range shippedDetectors {
		if sd.id == id {
			return true
		}
	}
	return false
}

// LegacyDetectorIDs is internal/detect.AllDetectorNames, frozen: the
// twelve definitions that package exposed as settings-toggleable, in its
// own order.
//
// It is deliberately NOT ShippedDefinitionIDs(). The shipped catalogue
// grew five definitions with issue #405's final block
// (unexpected_mail_sender, stale_rule, known_bad_ip, netclass,
// reputation), each of which internal/detect ran as an always-on pass
// with no name, no toggle and no scope. Giving them an envelope makes
// them toggleable for the first time -- but the detector-settings page
// carries hand-written label, explanation, scope-note and example copy
// per detector for exactly these twelve, so listing five more through
// that endpoint would render rows with no name and no explanation.
//
// That is a product surface with product copy to write, not a deletion's
// business: #405's own rule is that no behaviour change rides along with
// the port. The five are fully present in the definitions store, fully
// evaluated, and fully toggleable through the definitions API when #407
// builds it with the UI to match. Until then this endpoint answers the
// same twelve it always did.
func LegacyDetectorIDs() []string {
	return []string{
		"port_scan", "activity_spike", "critical_port",
		"global_spike", "distributed_brute_force", "outbound_anomaly",
		"internal_recon", "rule_spike", "repeated_drops",
		"low_slow_scan", "off_hours_activity", "device_silence",
	}
}

// IsLegacyDetectorID reports whether id is one of LegacyDetectorIDs.
func IsLegacyDetectorID(id string) bool {
	for _, name := range LegacyDetectorIDs() {
		if name == id {
			return true
		}
	}
	return false
}
