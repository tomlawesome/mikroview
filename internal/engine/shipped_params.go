// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "time"

// This file is the field-by-field walk issue #401 asks for: one
// []ParamSchema per one of internal/detect's twelve shipped detectors
// (internal/detect.AllDetectorNames), expressing every field
// internal/detect.Config carries for that detector today. See
// params_test.go's TestShippedParamSchemaCoversEveryConfigField, which
// reflects over detect.Config and fails if any field is left
// unmapped -- this file is what that test is checking, not merely
// asserting.
//
// Two things beyond a literal 1:1 field copy:
//
//   - detect.Config.VPNInterfaces/VPNConfidenceMultiplier are consulted
//     by three detectors (activity_spike via checkHostActivityBaseline,
//     outbound_anomaly and internal_recon via observeDestSpread -- see
//     detect.Config.VPNInterfaces's own doc comment), so they appear on
//     all three schemas below, not just one.
//   - Every detector whose logic is an EMA baseline (activity_spike,
//     global_spike, rule_spike, low_slow_scan, off_hours_activity) also
//     declares updateCadence, and the three that don't already have an
//     equivalent duration-shaped field declare baselineFloorDuration
//     too -- internal/engine.BaselineFloor/UpdateCadence (#399) params
//     that don't exist in detect.Config at all today because
//     internal/detect's baselines don't have this vocabulary yet, but a
//     ported EMA definition (#405) will need to declare them. Where an
//     existing Config field already serves one of the two
//     BaselineFloor dimensions (low_slow_scan's LowSlowScanMinObservation,
//     off_hours_activity's OffHoursMinSampleDays -- see each field's own
//     doc comment in detect.go), that field is the floor and no second,
//     redundant field is added for it.

// portScanThresholdMin/etc. name the lower bound every count-style param
// below shares: a threshold of 0 would mean "fire on literally anything
// ever," which is never a meaningful configuration for a threshold-over-
// window detector -- 1 is the true floor.
const countParamMin = 1

// PortScanParamSchema expresses detect.Config.PortScanThreshold/
// PortScanWindow.
var PortScanParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "distinct ports", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct destination ports from one source within the window that counts as a port scan."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the distinct-port count is measured over."},
}

// ActivitySpikeParamSchema expresses detect.Config.ActivitySpikeThreshold/
// ActivitySpikeWindow/HostActivityMultiplier/HostActivityWarmupSamples,
// plus VPNInterfaces/VPNConfidenceMultiplier (shared, see this file's
// doc comment) and the #399 baseline params.
var ActivitySpikeParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "events", Min: floatBound(countParamMin), Required: true,
		Description: "Absolute floor on events from one source within the window, alongside the per-host baseline multiplier."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the per-source event rate is measured over."},
	{Name: "baselineMultiplier", Type: ParamTypeFloat, Unit: "x baseline", Min: floatBound(0),
		Description: "How many times a source's own EMA baseline rate its current rate must clear, alongside the absolute threshold."},
	{Name: "warmupSamples", Type: ParamTypeInt, Min: floatBound(countParamMin),
		Description: "Observations a source's own baseline needs before a flag from it can reach full confidence."},
	{Name: "vpnInterfaces", Type: ParamTypeStringList,
		Description: "Interface-name glob patterns (e.g. \"wireguard*\") identifying VPN tunnels, for confidence boosting."},
	{Name: "vpnConfidenceMultiplier", Type: ParamTypeFloat, Unit: "multiplier", Min: floatBound(0),
		Description: "Confidence multiplier applied when the triggering event arrived via a vpnInterfaces-matched interface."},
	{Name: "updateCadence", Type: ParamTypeEnum, EnumValues: []string{"perEvent", "perWindow"},
		Description: "How often this definition's per-host baseline folds in a new reading -- see internal/engine.UpdateCadence."},
	{Name: "baselineFloorDuration", Type: ParamTypeDuration, Min: durationBound(0),
		Description: "Minimum wall-clock history (in addition to warmupSamples) before the per-host baseline is trusted -- see internal/engine.BaselineFloor."},
}

// CriticalPortParamSchema expresses detect.Config.CriticalPorts/
// CriticalPortThreshold/CriticalPortWindow.
var CriticalPortParamSchema = []ParamSchema{
	{Name: "ports", Type: ParamTypePortList, Required: true,
		Description: "Destination ports considered critical (e.g. SSH, RDP, RouterOS Winbox/API)."},
	{Name: "threshold", Type: ParamTypeInt, Unit: "attempts", Min: floatBound(countParamMin), Required: true,
		Description: "Attempts against a critical port from one source within the window before this fires."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the attempt count is measured over."},
}

// GlobalSpikeParamSchema expresses detect.Config.GlobalSpikeMultiplier/
// GlobalSpikeMinEPS/GlobalSpikeWarmupSamples, plus the #399 baseline
// params.
var GlobalSpikeParamSchema = []ParamSchema{
	{Name: "multiplier", Type: ParamTypeFloat, Unit: "x baseline", Min: floatBound(0), Required: true,
		Description: "How many times the network-wide EMA baseline rate the current rate must clear."},
	{Name: "minEPS", Type: ParamTypeFloat, Unit: "events/sec", Min: floatBound(0), Required: true,
		Description: "Absolute floor on network-wide events/sec, below which a multiplier crossing is not meaningful."},
	{Name: "warmupSamples", Type: ParamTypeInt, Min: floatBound(countParamMin),
		Description: "Check() calls the network-wide baseline needs before a flag can reach full confidence."},
	{Name: "updateCadence", Type: ParamTypeEnum, EnumValues: []string{"perEvent", "perWindow"},
		Description: "How often the network-wide baseline folds in a new reading -- see internal/engine.UpdateCadence."},
	{Name: "baselineFloorDuration", Type: ParamTypeDuration, Min: durationBound(0),
		Description: "Minimum wall-clock history (in addition to warmupSamples) before the network-wide baseline is trusted."},
}

// DistributedBruteForceParamSchema expresses
// detect.Config.DistributedBruteForceThreshold/Window, plus the port
// list.
//
// internal/detect read the port list off the shared
// Config.CriticalPorts, so critical_port and distributed_brute_force
// could never be pointed at different sets. Issue #405's port gives each
// definition its own copy, which is what
// docs/decisions/evaluation-engine.md means by per-definition params --
// and is not a behaviour change on migration, since both are seeded from
// the same DefaultConfig().CriticalPorts (see shippedDetectors,
// definitions_migrate.go). An operator who wants them to keep agreeing
// simply leaves both alone.
var DistributedBruteForceParamSchema = []ParamSchema{
	{Name: "ports", Type: ParamTypePortList, Required: true,
		Description: "Destination ports considered critical (e.g. SSH, RDP, RouterOS Winbox/API)."},
	{Name: "threshold", Type: ParamTypeInt, Unit: "distinct sources", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct source IPs hitting the same critical port within the window before this fires."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the distinct-source count is measured over."},
}

// OutboundAnomalyParamSchema expresses
// detect.Config.OutboundAnomalyThreshold/Window, plus VPNInterfaces/
// VPNConfidenceMultiplier (shared, see this file's doc comment).
var OutboundAnomalyParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "distinct destinations", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct external destinations one LAN source contacts within the window before this fires."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the distinct-destination count is measured over."},
	{Name: "vpnInterfaces", Type: ParamTypeStringList,
		Description: "Interface-name glob patterns (e.g. \"wireguard*\") identifying VPN tunnels, for confidence boosting."},
	{Name: "vpnConfidenceMultiplier", Type: ParamTypeFloat, Unit: "multiplier", Min: floatBound(0),
		Description: "Confidence multiplier applied when the triggering event arrived via a vpnInterfaces-matched interface."},
}

// InternalReconParamSchema expresses
// detect.Config.InternalReconThreshold/Window, plus VPNInterfaces/
// VPNConfidenceMultiplier (shared, see this file's doc comment).
var InternalReconParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "distinct destinations", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct internal destinations one LAN source contacts within the window before this fires."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the distinct-destination count is measured over."},
	{Name: "vpnInterfaces", Type: ParamTypeStringList,
		Description: "Interface-name glob patterns (e.g. \"wireguard*\") identifying VPN tunnels, for confidence boosting."},
	{Name: "vpnConfidenceMultiplier", Type: ParamTypeFloat, Unit: "multiplier", Min: floatBound(0),
		Description: "Confidence multiplier applied when the triggering event arrived via a vpnInterfaces-matched interface."},
}

// RuleSpikeParamSchema expresses detect.Config.RuleSpikeMultiplier/
// RuleSpikeMinRate/RuleSpikeWindow/RuleSpikeWarmupSamples, plus the
// #399 baseline params.
var RuleSpikeParamSchema = []ParamSchema{
	{Name: "multiplier", Type: ParamTypeFloat, Unit: "x baseline", Min: floatBound(0), Required: true,
		Description: "How many times a rule's own EMA baseline rate its current rate must clear."},
	{Name: "minRate", Type: ParamTypeFloat, Unit: "events/sec", Min: floatBound(0), Required: true,
		Description: "Absolute floor on a rule's events/sec, below which a multiplier crossing is not meaningful."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window a rule's event rate is measured over."},
	{Name: "warmupSamples", Type: ParamTypeInt, Min: floatBound(countParamMin),
		Description: "Observations a rule's own baseline needs before a flag from it can reach full confidence."},
	{Name: "updateCadence", Type: ParamTypeEnum, EnumValues: []string{"perEvent", "perWindow"},
		Description: "How often this rule's baseline folds in a new reading -- see internal/engine.UpdateCadence."},
	{Name: "baselineFloorDuration", Type: ParamTypeDuration, Min: durationBound(0),
		Description: "Minimum wall-clock history (in addition to warmupSamples) before this rule's baseline is trusted."},
}

// RepeatedDropsParamSchema expresses
// detect.Config.RepeatedDropsThreshold/Window.
var RepeatedDropsParamSchema = []ParamSchema{
	{Name: "threshold", Type: ParamTypeInt, Unit: "drops", Min: floatBound(countParamMin), Required: true,
		Description: "Drop/reject events for the same (source, destination port) pair within the window before this fires."},
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Rolling window the drop count is measured over."},
}

// LowSlowScanParamSchema expresses detect.Config.LowSlowScanWindow/
// PortThreshold/HostThreshold/MinObservation/DropRatio/
// BaselineMultiplier, plus the #399 updateCadence param
// (LowSlowScanMinObservation already serves as the BaselineFloor
// duration dimension -- see this file's doc comment).
var LowSlowScanParamSchema = []ParamSchema{
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "Long window this detector's destination-breadth signals are measured over."},
	{Name: "portThreshold", Type: ParamTypeInt, Unit: "distinct ports", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct destination ports within window before the port-breadth axis is satisfied."},
	{Name: "hostThreshold", Type: ParamTypeInt, Unit: "distinct hosts", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct destination hosts within window before the host-breadth axis is satisfied."},
	{Name: "minObservation", Type: ParamTypeDuration, Min: durationBound(0), Required: true,
		Description: "Minimum time a source must have been observed (first sample to now) before it's eligible to fire -- this definition's BaselineFloor duration."},
	{Name: "dropRatio", Type: ParamTypeFloat, Unit: "ratio", Min: floatBound(0), Max: floatBound(1), Required: true,
		Description: "Minimum fraction of tracked attempts that must be drop/reject rather than accept."},
	{Name: "baselineMultiplier", Type: ParamTypeFloat, Unit: "x baseline", Min: floatBound(0), Required: true,
		Description: "How many times a source's own destination-breadth EMA baseline its current rate must clear."},
	{Name: "updateCadence", Type: ParamTypeEnum, EnumValues: []string{"perEvent", "perWindow"},
		Description: "How often this source's destination-breadth baseline folds in a new reading -- see internal/engine.UpdateCadence."},
}

// OffHoursActivityParamSchema expresses detect.Config.OffHoursStartHour/
// EndHour/MinSampleDays/MinCount, plus the #399 updateCadence param
// (OffHoursMinSampleDays already serves as both BaselineFloor dimensions
// -- see this file's doc comment, and OffHoursMinSampleDays's own doc
// comment in detect.go).
var OffHoursActivityParamSchema = []ParamSchema{
	{Name: "startHour", Type: ParamTypeInt, Unit: "hour of day (0-23)", Min: floatBound(0), Max: floatBound(23), Required: true,
		Description: "Clock hour (server-local, inclusive) this detector is willing to fire from -- wraps past midnight when after endHour."},
	{Name: "endHour", Type: ParamTypeInt, Unit: "hour of day (0-23)", Min: floatBound(0), Max: floatBound(23), Required: true,
		Description: "Clock hour (server-local, exclusive) this detector stops being willing to fire at."},
	{Name: "minSampleDays", Type: ParamTypeInt, Unit: "distinct days", Min: floatBound(countParamMin), Required: true,
		Description: "Distinct prior days that hour must have been observed before a flag can fire for it -- this definition's BaselineFloor (both dimensions)."},
	{Name: "minCount", Type: ParamTypeInt, Min: floatBound(countParamMin), Required: true,
		Description: "Absolute floor on events in that hour, alongside the per-hour baseline z-score."},
	{Name: "updateCadence", Type: ParamTypeEnum, EnumValues: []string{"perEvent", "perWindow"},
		Description: "How often this hour's baseline folds in a new reading -- see internal/engine.UpdateCadence."},
}

// DeviceSilenceParamSchema expresses detect.Config.DeviceStaleAfter.
var DeviceSilenceParamSchema = []ParamSchema{
	{Name: "staleAfter", Type: ParamTypeDuration, Min: durationBound(0), Required: true,
		Description: "How long a configured device's LastSeen may go without updating before this fires. Zero disables the detector."},
}

// --- schemas for the shipped definitions internal/detect had no
// DetectorName for -----------------------------------------------------
//
// The four below (plus reputation's, see ReputationParamSchema) are new
// with #405's final block. internal/detect ran each of them as an
// always-on pass rather than a settings-toggleable detector -- there was
// no DetectorName, no Settings entry and no scope for any of them (see
// e.g. observeMailSender's and observeKnownBadIP's own doc comments on
// why). That was never a statement that they should be untunable; it was
// a consequence of internal/detect's settings store being a fixed
// twelve-entry enum. On the chassis every definition wears the same
// envelope, so these get one too, and the constants each of them hard-
// coded in Go become params at exactly the values they were compiled
// with -- which is what makes the port a no-behaviour-change move rather
// than a retuning.

// UnexpectedMailSenderParamSchema expresses internal/detect's mailPorts
// and tagTrustedMailSender (mail_sender.go), both package constants
// there.
var UnexpectedMailSenderParamSchema = []ParamSchema{
	{Name: "ports", Type: ParamTypePortList, Required: true,
		Description: "Destination ports treated as outbound SMTP -- the unencrypted, implicit-TLS and STARTTLS submission ports."},
	{Name: "trustedTag", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Entity tag marking a host as a known, legitimate outbound mail sender; a host carrying it is never flagged."},
}

// StaleRuleParamSchema expresses the two values internal/detect took as
// constructor arguments to StaleRuleDetector rather than as Config
// fields: config.Flags.StaleRuleDays (as a duration) and
// config.Flags.StaleRuleCheckInterval. See ShippedDefaults for why they
// were never in detect.Config.
//
// checkInterval is a param rather than a constant because it always was
// operator-configurable -- main.go read cfg.Flags.StaleRuleCheckInterval
// straight into its own ticker. Ticked.TickInterval makes a definition
// declare its cadence, so the declaration reads the param.
var StaleRuleParamSchema = []ParamSchema{
	{Name: "maxAge", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "How long a firewall rule must go without firing before it is reported as stale."},
	{Name: "checkInterval", Type: ParamTypeDuration, Min: durationBound(time.Second), Required: true,
		Description: "How often the stale-rule sweep runs. Coarse by design: staleness is judged in days."},
}

// KnownBadIPParamSchema expresses internal/detect's knownBadIPConfidence
// (known_bad_ip.go), a package constant there.
//
// The reinforced flag-type set is deliberately NOT a param: it is every
// definition whose Target is a plain source address (see
// reinforcedFlagTypes), which is a structural fact about those
// definitions rather than a preference -- pointing a reinforcement pass
// at a definition whose target is a port label or a device ID would not
// tune anything, it would simply never match.
var KnownBadIPParamSchema = []ParamSchema{
	{Name: "confidence", Type: ParamTypeInt, Min: floatBound(0), Max: floatBound(100), Required: true,
		Description: "Confidence a blocklist match is raised at, and the floor it applies to any other active source-keyed flag for the same address."},
}
