// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// --- internal/detect.SettingsStore -> shipped definitions --------------

// ShippedDefaults is every value the shipped catalogue seeds a
// definition's default params from -- what an operator's shipped
// definition starts at before they change anything.
//
// It embeds DetectorDefaults -- internal/detect.Config, moved here when
// that package was deleted (see shipped_defaults.go) -- because that
// struct is exactly "every shipped detector's tunable thresholds,
// sourced from internal/config". What this type adds are the values for
// shipped definitions internal/detect never kept in Config at all,
// because they were constructor arguments to a bespoke type rather than
// entries in the shared threshold struct (stale_rule's two).
type ShippedDefaults struct {
	DetectorDefaults

	// StaleRuleMaxAge/StaleRuleCheckInterval are
	// config.Flags.StaleRuleDays (as a duration) and
	// config.Flags.StaleRuleCheckInterval -- main.go passed both straight
	// into internal/detect's stale-rule detector and its own ticker
	// respectively, so neither ever reached DetectorDefaults. As
	// definition params they become tunable the same way every other
	// shipped threshold is.
	StaleRuleMaxAge        time.Duration
	StaleRuleCheckInterval time.Duration
}

// DefaultShippedDefaults is the shipped catalogue's own starting point --
// DefaultDetectorDefaults() plus the two values that were never in it. The
// two match internal/config's own defaults (staleRuleDays: 30,
// staleRuleCheckInterval: 1h) exactly, which is what makes seeding them
// here a no-behaviour-change move.
func DefaultShippedDefaults() ShippedDefaults {
	return ShippedDefaults{
		DetectorDefaults:       DefaultDetectorDefaults(),
		StaleRuleMaxAge:        30 * 24 * time.Hour,
		StaleRuleCheckInterval: time.Hour,
	}
}

// shippedDetector pairs one of internal/detect's 12 settings-toggleable
// detectors with the ParamSchema issue #401 already declared for it
// (shipped_params.go) and a function building that detector's default
// Params from detect.DefaultConfig() -- what every operator's shipped
// definition starts from, per docs/decisions/evaluation-engine.md's
// Migration section ("shipped defaults seeded as provenance=shipped ...
// so every operator gets the same baseline set with their settings as
// overrides"). "Their settings" is Enabled/Scope, read from the
// migration source document below -- detect.SettingsStore's document
// carries only {enabled, scope} per detector, never tunable params (those
// live in config.yaml today, which this migration deliberately does not
// read: only the *persisted* document is a migration source, per the
// ADR's own wording), so Params is the same default for every operator
// at migration time.
//
// Kind was KindProgrammatic for all twelve, uniformly, as #404 shipped
// it -- that issue's own report noted this was seeded programmatic
// "pending #405," since Definition had no structured-condition
// representation yet at that point. #405 is what starts correcting the
// mapping, one ported detector at a time (see shippedDetector.kind's own
// doc comment): port_scan is the first to flip to KindDeclarative, built
// on shipped_declarative.go's buildPortScanDefinition.
type shippedDetector struct {
	// id is the definition id this catalogue entry becomes -- and, for a
	// detection-intent definition, therefore its flags.Type too (see
	// routeToFlag). A plain string rather than detect.DetectorName
	// because the catalogue is no longer a copy of that enum: #405's
	// final block adds shipped definitions internal/detect never had a
	// DetectorName for at all (mail_sender, known_bad_ip, netclass,
	// stale_rule), because they were always-on passes rather than
	// settings-toggleable detectors there. The twelve that do have a
	// DetectorName still read their enabled/scope from the migration
	// source document by exactly that string.
	id     string
	schema []ParamSchema
	params func(d ShippedDefaults) Params
	// kind is the migrated Definition's Kind -- KindProgrammatic for
	// every detector until issue #405 ports it onto a declarative or
	// programmatic definition built on this chassis, at which point this
	// field flips to match (docs/decisions/evaluation-engine.md section
	// 2's "current detectors whose logic already is threshold-over-window
	// ... become shipped declarative definitions"). #404's own report
	// noted every detector was seeded programmatic "pending this issue" --
	// this field, and shippedDeclarativeBuilders (shipped_declarative.go),
	// are #405's fix to that mapping, one detector at a time as each is
	// actually ported (see AGENTS.md's "removals are wholesale" applied
	// in reverse: nothing here claims a detector is declarative before
	// its evaluation logic actually exists as one).
	kind Kind
}

// shippedDetectorDisplayNames gives each of the 12 settings-toggleable
// detectors an operator-facing Name -- detect.DetectorName's own values
// (e.g. "low_slow_scan") are machine keys, not display text, mirroring
// why Definition.ID is never the display name (see that field's own doc
// comment).
var shippedDetectorDisplayNames = map[string]string{
	string(flags.TypePortScan):              "Port scan",
	string(flags.TypeActivitySpike):         "Activity spike",
	string(flags.TypeCriticalPort):          "Critical port",
	string(flags.TypeGlobalSpike):           "Global spike",
	string(flags.TypeDistributedBruteForce): "Distributed brute force",
	string(flags.TypeOutboundAnomaly):       "Outbound anomaly",
	string(flags.TypeInternalRecon):         "Internal recon",
	string(flags.TypeRuleSpike):             "Rule spike",
	string(flags.TypeRepeatedDrops):         "Repeated drops",
	string(flags.TypeLowSlowScan):           "Low & slow scan",
	string(flags.TypeOffHoursActivity):      "Off-hours activity",
	string(flags.TypeDeviceSilence):         "Device silence",

	// The shipped definitions with no DetectorName -- see shippedDetectors.
	string(flags.TypeUnexpectedMailSender): "Unexpected mail sender",
	string(flags.TypeStaleRule):            "Stale rule",
	string(flags.TypeKnownBadIP):           "Known bad IP",
	"netclass":                             "Network class reinforcement",
	"reputation":                           "Reputation enrichment",
}

// zeroDuration is time.Duration(0).String() ("0s") -- the default value
// this migration seeds for the baselineFloorDuration param on the three
// detectors shipped_params.go added it for (activity_spike, global_spike,
// rule_spike): "no additional wall-clock floor beyond warmupSamples,"
// which is today's actual, pre-#399 behavior for these three -- #399/
// BaselineFloor did not exist before this port, so there is nothing to
// carry over except "off."
var zeroDuration = time.Duration(0).String()

// shippedDetectors is the field-by-field walk from detect.Config,
// through shipped_params.go's ParamSchema, into this migration's default
// Params -- one entry per internal/detect.AllDetectorNames, same order.
var shippedDetectors = []shippedDetector{
	// port_scan (issue #405): threshold-over-window, ported onto a
	// shipped DeclarativeDefinition -- see shipped_declarative.go's
	// buildPortScanDefinition. Every entry below still marked
	// KindProgrammatic is one #405 has not ported yet.
	{id: string(flags.TypePortScan), schema: PortScanParamSchema, kind: KindDeclarative, params: func(c ShippedDefaults) Params {
		return Params{"threshold": c.PortScanThreshold, "window": c.PortScanWindow.String()}
	}},
	{id: string(flags.TypeActivitySpike), schema: ActivitySpikeParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"threshold":               c.ActivitySpikeThreshold,
			"window":                  c.ActivitySpikeWindow.String(),
			"baselineMultiplier":      c.HostActivityMultiplier,
			"warmupSamples":           c.HostActivityWarmupSamples,
			"vpnInterfaces":           c.VPNInterfaces,
			"vpnConfidenceMultiplier": c.VPNConfidenceMultiplier,
			"updateCadence":           "perEvent",
			"baselineFloorDuration":   zeroDuration,
		}
	}},
	// critical_port (issue #405): threshold-over-window keyed per source,
	// ported onto a shipped DeclarativeDefinition -- see
	// shipped_declarative.go's buildCriticalPortDefinition.
	{id: string(flags.TypeCriticalPort), schema: CriticalPortParamSchema, kind: KindDeclarative, params: func(c ShippedDefaults) Params {
		return Params{"ports": c.CriticalPorts, "threshold": c.CriticalPortThreshold, "window": c.CriticalPortWindow.String()}
	}},
	{id: string(flags.TypeGlobalSpike), schema: GlobalSpikeParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"multiplier":            c.GlobalSpikeMultiplier,
			"minEPS":                c.GlobalSpikeMinEPS,
			"warmupSamples":         c.GlobalSpikeWarmupSamples,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": zeroDuration,
		}
	}},
	// distributed_brute_force (issue #405): distinct-source count over a
	// window keyed per destination port, ported onto a shipped
	// DeclarativeDefinition -- see shipped_declarative.go's
	// buildDistributedBruteForceDefinition. Seeded with the same
	// CriticalPorts list critical_port gets, which is what internal/detect
	// shared between the two.
	{id: string(flags.TypeDistributedBruteForce), schema: DistributedBruteForceParamSchema, kind: KindDeclarative, params: func(c ShippedDefaults) Params {
		return Params{"ports": c.CriticalPorts, "threshold": c.DistributedBruteForceThreshold, "window": c.DistributedBruteForceWindow.String()}
	}},
	{id: string(flags.TypeOutboundAnomaly), schema: OutboundAnomalyParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"threshold":               c.OutboundAnomalyThreshold,
			"window":                  c.OutboundAnomalyWindow.String(),
			"vpnInterfaces":           c.VPNInterfaces,
			"vpnConfidenceMultiplier": c.VPNConfidenceMultiplier,
		}
	}},
	{id: string(flags.TypeInternalRecon), schema: InternalReconParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"threshold":               c.InternalReconThreshold,
			"window":                  c.InternalReconWindow.String(),
			"vpnInterfaces":           c.VPNInterfaces,
			"vpnConfidenceMultiplier": c.VPNConfidenceMultiplier,
		}
	}},
	{id: string(flags.TypeRuleSpike), schema: RuleSpikeParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"multiplier":            c.RuleSpikeMultiplier,
			"minRate":               c.RuleSpikeMinRate,
			"window":                c.RuleSpikeWindow.String(),
			"warmupSamples":         c.RuleSpikeWarmupSamples,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": zeroDuration,
		}
	}},
	// repeated_drops (issue #405): threshold-over-window keyed per
	// (source, destination port), ported onto a shipped
	// DeclarativeDefinition -- see shipped_declarative.go's
	// buildRepeatedDropsDefinition.
	{id: string(flags.TypeRepeatedDrops), schema: RepeatedDropsParamSchema, kind: KindDeclarative, params: func(c ShippedDefaults) Params {
		return Params{"threshold": c.RepeatedDropsThreshold, "window": c.RepeatedDropsWindow.String()}
	}},
	{id: string(flags.TypeLowSlowScan), schema: LowSlowScanParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"window":             c.LowSlowScanWindow.String(),
			"portThreshold":      c.LowSlowScanPortThreshold,
			"hostThreshold":      c.LowSlowScanHostThreshold,
			"minObservation":     c.LowSlowScanMinObservation.String(),
			"dropRatio":          c.LowSlowScanDropRatio,
			"baselineMultiplier": c.LowSlowScanBaselineMultiplier,
			"updateCadence":      "perEvent",
		}
	}},
	{id: string(flags.TypeOffHoursActivity), schema: OffHoursActivityParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{
			"startHour":     c.OffHoursStartHour,
			"endHour":       c.OffHoursEndHour,
			"minSampleDays": c.OffHoursMinSampleDays,
			"minCount":      c.OffHoursMinCount,
			"updateCadence": "perEvent",
		}
	}},
	{id: string(flags.TypeDeviceSilence), schema: DeviceSilenceParamSchema, kind: KindProgrammatic, params: func(c ShippedDefaults) Params {
		return Params{"staleAfter": c.DeviceStaleAfter.String()}
	}},

	// Below this line: shipped definitions internal/detect had no
	// DetectorName for, because it ran them as always-on passes rather
	// than settings-toggleable detectors (issue #405's final block). Their
	// params take no argument from detect.Config -- there was nothing in
	// it for them -- and are seeded at exactly the values internal/detect
	// hard-coded, so the port changes no behaviour. See
	// shipped_params.go's own note on why they get an envelope at all.
	//
	// Each id is also its flags.Type (routeToFlag keys on the definition
	// id), which is why these read as flag names rather than as detector
	// names: "unexpected_mail_sender", not "mail_sender".
	{id: string(flags.TypeUnexpectedMailSender), schema: UnexpectedMailSenderParamSchema, kind: KindProgrammatic, params: func(ShippedDefaults) Params {
		return Params{
			"ports":      []int{25, 465, 587},
			"trustedTag": []string{"trusted-mail-sender"},
		}
	}},
	{id: string(flags.TypeStaleRule), schema: StaleRuleParamSchema, kind: KindProgrammatic, params: func(d ShippedDefaults) Params {
		return Params{
			"maxAge":        d.StaleRuleMaxAge.String(),
			"checkInterval": d.StaleRuleCheckInterval.String(),
		}
	}},
	{id: string(flags.TypeKnownBadIP), schema: KnownBadIPParamSchema, kind: KindProgrammatic, params: func(ShippedDefaults) Params {
		return Params{"confidence": knownBadIPConfidence}
	}},
	// netclass is the one shipped definition whose id is not also a
	// flags.Type, because it raises no flag of its own -- it only
	// reinforces flags other definitions raised. See
	// netClassDefinition's own doc comment.
	{id: "netclass", schema: NetClassParamSchema, kind: KindProgrammatic, params: func(ShippedDefaults) Params {
		return Params{"torFloor": reputation.TorExitNodeFloor, "vpnFloor": netclassVPNFloor}
	}},
	// reputation, like netclass, has no flags.Type: it raises nothing and
	// only enriches other definitions' episodes. See
	// reputationDefinition's own doc comment for why it is a definition
	// rather than a set of constants.
	{id: "reputation", schema: ReputationParamSchema, kind: KindProgrammatic, params: func(ShippedDefaults) Params {
		p := DefaultReputationPolicy()
		return Params{
			"lookupConcurrency":          p.Concurrency,
			"lookupTimeout":              p.Timeout.String(),
			"groupSampleSize":            p.GroupSampleSize,
			"groupMinSignificantSamples": p.GroupMinSignificantSamples,
		}
	}},
}

// convertDetectSettings builds every shipped detector definition -- always
// all 12, regardless of what settingsDoc contains (see shippedDetector's
// own doc comment) -- reading each one's enabled/scope from settingsDoc
// when present and falling back to DefaultDetectorSettings()'s enabled,
// unscoped default otherwise.
//
// A settingsDoc entry keyed by a name outside that 12 (IsShippedDefinitionID)
// is not read at all: nothing here builds anything for it. Issue #887
// removed the placeholder definition this function used to build for that
// case -- SeedShippedDefinitions (its only caller) only ever reads out[sd.id]
// for the 12 known ids, so the placeholder was built into this function's
// local map, then discarded with it: never reached DefinitionsStore.Upsert,
// never persisted. Removing it changes no deployment's on-disk state.
func convertDetectSettings(settingsDoc map[string]DetectorSettings, cfg ShippedDefaults, out map[string]Definition) error {
	for _, sd := range shippedDetectors {
		settings, ok := settingsDoc[sd.id]
		if !ok {
			// Matches DefaultDetectorSettings()'s own default: enabled,
			// unscoped.
			settings = DetectorSettings{Enabled: true}
		}
		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			return fmt.Errorf("shipped detector %q: building default params: %w", sd.id, err)
		}
		out[sd.id] = Definition{
			ID:          sd.id,
			Name:        shippedDetectorDisplayNames[sd.id],
			Description: fmt.Sprintf("Migrated from internal/detect's %q detector settings (issue #404).", sd.id),
			Intent:      IntentDetection,
			Kind:        sd.kind,
			Enabled:     settings.Enabled,
			Scope:       settings.Scope,
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		}
	}
	return nil
}

// --- internal/watchlist.Store -> expectation definitions ----------------

// optionalStringList turns a single optional string field (Entry.DestIP,
// Entry.Source.MAC, ...) into the 0-or-1-element list shape
// ParamTypeStringList expects -- there is no single-optional-string
// ParamType (see params.go's own type menu), and inventing one for this
// one migration is more machinery than the problem needs.
func optionalStringList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// formatTime renders t as RFC 3339 for a Params value, or "" for a zero
// time -- optionalStringList then turns "" into an absent param, the
// same "zero means absent" convention every other optional field here
// follows.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// watchlistCommonParamSchema documents the fields both watchlist-derived
// schemas below share -- inlined into each rather than factored into a
// shared slice, so each schema's own field list is what a reader sees in
// one place.

// watchlistNonInvertedParamSchema is what a non-inverted watchlist entry
// ("record attempts against these ports") becomes: a declarative
// expectation definition, per issue #404's decision. There is no
// structured-condition representation in this package yet (see
// shippedDetector's own doc comment on why detect-derived definitions
// stay Programmatic for the same reason) -- these fields are Params for
// now, a pragmatic home issue #404 uses because Definition has nowhere
// else to carry them, expected to become real match conditions once
// #405/#406 give declarative definitions a condition schema to port
// onto.
var watchlistNonInvertedParamSchema = []ParamSchema{
	{Name: "ports", Type: ParamTypePortList,
		Description: "Destination ports this expectation watches."},
	{Name: "destIp", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Destination IP this expectation is scoped to, if any."},
	{Name: "sourceMac", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Source MAC this expectation is scoped to, if any."},
	{Name: "sourceIp", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Source IP this expectation is scoped to, if any (used when sourceMac is unset)."},
	{Name: "sourceListDevice", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Router device name this expectation's live address-list scoping refers to, if any."},
	{Name: "sourceListList", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Router address-list name this expectation's live address-list scoping refers to, if any."},
	{Name: "createdAt", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "When this expectation was originally created (RFC 3339), carried over from the watchlist entry it was migrated from."},
	{Name: "windowJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded watchlist.Window -- when this expectation is expected to see traffic (clock range, days, IANA zone). Absent means no window: watched at every hour."},
	{Name: "nightsJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded []watchlist.Night -- the last seven occurrences of the window and what happened in each (kept, empty, not observed). Recorded rather than derived: the match log keeps 48 hours, so a healthy watch would read as empty nights if this were rebuilt from it."},
	{Name: "ringJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded watchlist.Ring -- the recorded break in this expectation's run of kept nights, written at the moment it broke."},
	{Name: "silentJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded []time.Time -- the Open instant of every currently-open-or-recent occurrence found, at some tick, to have the device behind this expectation's pathway gone stale (issue #730). Sticky: written while the occurrence is still open, so FillNights can still close it as not-observed even if the device recovered before the window shut."},
}

// watchlistInvertedParamSchema is what an inverted watchlist entry ("this
// device should only ever reach X") becomes: a programmatic expectation
// definition, per issue #404's decision -- the observed/permitted/
// violation state machine with live SourceList resolution is built-in Go
// logic (invert.go), not something an operator authors through a builder
// UI, which is exactly Kind's own documented Programmatic/Custom
// boundary (definition.go). permittedJSON/observedJSON carry
// Entry.Permitted/Entry.Observed forward as JSON-encoded strings rather
// than a structured param type -- there is no ParamType shaped like
// "list of (destIP, port, timestamps, count)" (see params.go's type
// menu), and the mid-observation state (Observed while Observing) is
// exactly what issue #404 requires survive migration intact: an
// in-progress observation period is not reset.
var watchlistInvertedParamSchema = []ParamSchema{
	{Name: "sourceMac", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Source MAC this expectation's device is identified by, if any."},
	{Name: "sourceIp", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "Source IP this expectation's device is identified by, if any (used when sourceMac is unset)."},
	{Name: "includeStructuralNoise", Type: ParamTypeBool,
		Description: "Whether broadcast/multicast/link-local destinations are evaluated instead of exempted by default."},
	{Name: "observing", Type: ParamTypeBool,
		Description: "Whether this expectation is still in its observe period (recording candidates) rather than enforcing violations."},
	{Name: "permittedJSON", Type: ParamTypeStringList,
		Description: "JSON-encoded []watchlist.PermittedDest -- this device's promoted allow-list, carried over verbatim."},
	{Name: "observedJSON", Type: ParamTypeStringList,
		Description: "JSON-encoded []watchlist.ObservedDest -- destinations seen during this expectation's observe period, not yet promoted or dismissed, carried over verbatim. An in-progress observation period is not reset by migration."},
	{Name: "createdAt", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "When this expectation was originally created (RFC 3339), carried over from the watchlist entry it was migrated from."},
	{Name: "windowJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded watchlist.Window -- when this expectation is expected to see traffic (clock range, days, IANA zone). Absent means no window: watched at every hour."},
	{Name: "nightsJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded []watchlist.Night -- the last seven occurrences of the window and what happened in each (kept, empty, not observed). Recorded rather than derived: the match log keeps 48 hours, so a healthy watch would read as empty nights if this were rebuilt from it."},
	{Name: "ringJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded watchlist.Ring -- the recorded break in this expectation's run of kept nights, written at the moment it broke."},
	{Name: "silentJSON", Type: ParamTypeStringList, Max: floatBound(1),
		Description: "JSON-encoded []time.Time -- the Open instant of every currently-open-or-recent occurrence found, at some tick, to have the device behind this expectation's pathway gone stale (issue #730). Sticky: written while the occurrence is still open, so FillNights can still close it as not-observed even if the device recovered before the window shut."},
}

func convertWatchlistEntry(e *watchlist.Entry) (Definition, error) {
	name := e.Name
	if name == "" {
		name = "Watchlist entry " + e.ID
	}
	if e.Invert {
		return convertInvertedEntry(e, name)
	}
	return convertNonInvertedEntry(e, name)
}

// watchHistoryParams encodes the window and the nightly history an entry
// carries (#680) into the three JSON-in-a-string params both watchlist
// schemas above declare.
//
// JSON rather than structured param types for the same reason
// permittedJSON/observedJSON are: there is no ParamType shaped like "a
// clock range with a zone" or "a list of (instant, state, count)" (see
// params.go's type menu). Each is omitted entirely when it holds nothing,
// so an entry with no window adds no params at all and the stored
// definition for every existing entry is byte-identical to what it was.
//
// This is a field addition inside the definitions blob, not a schema
// change: there is no migration to run. It is not downgrade-safe, though
// -- #404's raw-JSON preservation covers unknown definition *types*, not
// unknown params on a known one, so an older binary that rewrote one of
// these definitions would drop the window and the nights silently.
// addWatchHistoryParams sets each of the three only when it holds
// something. An absent key rather than a null value, deliberately: an
// entry with no window then converts to exactly the definition it
// converted to before #680, byte for byte, so the field addition costs
// existing deployments nothing on disk and nothing in review.
func addWatchHistoryParams(params Params, windowJSON, nightsJSON, ringJSON, silentJSON string) {
	for name, value := range map[string]string{
		"windowJSON": windowJSON, "nightsJSON": nightsJSON, "ringJSON": ringJSON, "silentJSON": silentJSON,
	} {
		if value != "" {
			params[name] = []string{value}
		}
	}
}

func watchHistoryParams(e *watchlist.Entry) (windowJSON, nightsJSON, ringJSON, silentJSON string, err error) {
	if e.Window.Defined() {
		b, err := json.Marshal(e.Window)
		if err != nil {
			return "", "", "", "", fmt.Errorf("encoding the watch window: %w", err)
		}
		windowJSON = string(b)
	}
	if len(e.Nights) > 0 {
		b, err := json.Marshal(e.Nights)
		if err != nil {
			return "", "", "", "", fmt.Errorf("encoding the nightly history: %w", err)
		}
		nightsJSON = string(b)
	}
	if e.Ring.Broken {
		b, err := json.Marshal(e.Ring)
		if err != nil {
			return "", "", "", "", fmt.Errorf("encoding the ring state: %w", err)
		}
		ringJSON = string(b)
	}
	if len(e.SilentOccurrences) > 0 {
		b, err := json.Marshal(e.SilentOccurrences)
		if err != nil {
			return "", "", "", "", fmt.Errorf("encoding the silent-occurrence marks: %w", err)
		}
		silentJSON = string(b)
	}
	return windowJSON, nightsJSON, ringJSON, silentJSON, nil
}

func convertNonInvertedEntry(e *watchlist.Entry, name string) (Definition, error) {
	windowJSON, nightsJSON, ringJSON, silentJSON, err := watchHistoryParams(e)
	if err != nil {
		return Definition{}, err
	}
	params := Params{
		"ports":            e.Ports,
		"destIp":           optionalStringList(e.DestIP),
		"sourceMac":        optionalStringList(e.Source.MAC),
		"sourceIp":         optionalStringList(e.Source.IP),
		"sourceListDevice": optionalStringList(e.SourceList.Device),
		"sourceListList":   optionalStringList(e.SourceList.List),
		"createdAt":        optionalStringList(formatTime(e.CreatedAt)),
	}
	addWatchHistoryParams(params, windowJSON, nightsJSON, ringJSON, silentJSON)
	normalized, err := ValidateParams(watchlistNonInvertedParamSchema, params)
	if err != nil {
		return Definition{}, fmt.Errorf("converting to a declarative expectation definition: %w", err)
	}
	return Definition{
		ID:          e.ID,
		Name:        name,
		Description: "Migrated from a non-inverted watchlist entry (issue #404): records attempts against the listed ports.",
		Intent:      IntentExpectation,
		Kind:        KindDeclarative,
		Enabled:     true,
		Params:      normalized,
		ParamSchema: watchlistNonInvertedParamSchema,
		// ProvenanceCustom: an operator authored this expectation's own
		// matching data through the watchlist UI -- see Kind's own doc
		// comment on why that pairs with KindDeclarative (the only
		// combination Definition.Validate allows for provenance=custom).
		Provenance: Provenance{Origin: ProvenanceCustom},
	}, nil
}

func convertInvertedEntry(e *watchlist.Entry, name string) (Definition, error) {
	windowJSON, nightsJSON, ringJSON, silentJSON, err := watchHistoryParams(e)
	if err != nil {
		return Definition{}, err
	}
	permittedJSON, err := json.Marshal(e.Permitted)
	if err != nil {
		return Definition{}, fmt.Errorf("encoding permitted destinations: %w", err)
	}
	observedJSON, err := json.Marshal(e.Observed)
	if err != nil {
		return Definition{}, fmt.Errorf("encoding observed destinations: %w", err)
	}

	params := Params{
		"sourceMac":              optionalStringList(e.Source.MAC),
		"sourceIp":               optionalStringList(e.Source.IP),
		"includeStructuralNoise": e.IncludeStructuralNoise,
		"observing":              e.Observing,
		"permittedJSON":          []string{string(permittedJSON)},
		"observedJSON":           []string{string(observedJSON)},
		"createdAt":              optionalStringList(formatTime(e.CreatedAt)),
	}
	addWatchHistoryParams(params, windowJSON, nightsJSON, ringJSON, silentJSON)
	normalized, err := ValidateParams(watchlistInvertedParamSchema, params)
	if err != nil {
		return Definition{}, fmt.Errorf("converting to a programmatic expectation definition: %w", err)
	}
	return Definition{
		ID:          e.ID,
		Name:        name,
		Description: "Migrated from an inverted watchlist entry (issue #404): this device is expected to reach only its permitted destinations.",
		Intent:      IntentExpectation,
		Kind:        KindProgrammatic,
		Enabled:     true,
		Params:      normalized,
		ParamSchema: watchlistInvertedParamSchema,
		// ProvenanceShipped, not Custom, even though an operator created
		// this entry: the evaluating logic (the observed/permitted state
		// machine) is built-in Go, exactly like a shipped detector's
		// Programmatic logic -- see Kind's own doc comment and
		// watchlistInvertedParamSchema's. What the operator authored is
		// captured in Params (which device, which destinations), the
		// same way customizing a shipped detector's threshold doesn't
		// make that detector provenance=custom.
		Provenance: Provenance{Origin: ProvenanceShipped},
	}, nil
}

// SeedShippedDefinitions makes sure every shipped detector definition
// actually exists in s, adding any that are missing at their shipped
// defaults (with enabled/scope taken from settingsDoc). Definitions
// already present are left completely alone -- an operator's edits win
// over a default.
//
// It runs on every boot, answering "does the shipped catalogue this
// binary evaluates actually exist". Issue #405 is what made that
// question matter: before it, an absent or unwritable definitions
// document cost nothing, because internal/detect evaluated every
// detector from its own settings store regardless. Once a detector's
// evaluation logic lives here, a definition that does not exist is a
// detector that does not run -- and a deployment that simply never
// configured engine.definitionsStorePath (nothing in the config file
// requires it) would silently lose every ported detector, with no
// symptom beyond flags quietly not appearing. That is exactly the
// "absence of detection presented as absence of threat" failure #380's
// first item describes.
//
// Seeding is therefore not a convenience: it is what makes the shipped
// catalogue a property of the binary rather than of whether persistence
// happens to be configured. A deployment with no definitions backend at
// all still gets every shipped definition, in memory, for the life of
// the process -- the same "empty path disables persistence, not the
// feature" contract every other store in this codebase follows.
func SeedShippedDefinitions(s *DefinitionsStore, settingsDoc map[string]DetectorSettings, cfg ShippedDefaults) error {
	defs := make(map[string]Definition, len(shippedDetectors))
	if err := convertDetectSettings(settingsDoc, cfg, defs); err != nil {
		return fmt.Errorf("engine: seeding shipped definitions: %w", err)
	}
	for _, sd := range shippedDetectors {
		id := sd.id
		def, ok := defs[id]
		if !ok {
			continue
		}
		if _, exists := s.Get(id); exists {
			continue
		}
		if err := s.Upsert(def); err != nil {
			return fmt.Errorf("engine: seeding shipped definition %q: %w", id, err)
		}
	}
	return nil
}
