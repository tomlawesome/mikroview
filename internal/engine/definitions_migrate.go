// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// migrateWatchlistFile mirrors watchlist's own unexported storeFile
// shape (internal/watchlist/watchlist.go) -- duplicated here rather than
// exported from that package solely for this one-time reader, the same
// "each package keeps its own small copy" precedent
// internal/watchlist/characterization_test.go's own doc comment already
// sets for this codebase (pgTestDSN/pgNewTestPool). watchlist.Entry
// itself is exported and decodes directly.
type migrateWatchlistFile struct {
	Entries []*watchlist.Entry `json:"entries"`
}

// MigrateDefinitions seeds a not-yet-existing definitions document from
// internal/detect's settings store and internal/watchlist's entries
// store -- issue #404's one-way migration. Call this once, before
// OpenDefinitionsStoreWithBackend is ever called against
// definitionsBackend, in a deployment's boot sequence.
//
// # Non-destructive
//
// This reads the two source documents and writes the new one; it never
// deletes or modifies either source's bytes. Both old stores keep
// working in production exactly as before this lands: internal/detect
// and internal/watchlist still read and write their own documents until
// #405/#406 port their evaluation logic onto this chassis and retire
// them. Running this again once a definitions document exists is a
// deliberate no-op -- see the existence check below -- which is what
// makes a second boot idempotent rather than re-migrating (and silently
// overwriting whatever an operator has since changed through this
// store).
//
// # Fail-closed, all the way through
//
// persist.Open already guarantees an unreadable or unparseable *source*
// document refuses to start rather than being treated as empty (#378) --
// this function uses it for both sources, unchanged. What persist.Open
// alone does not cover is a failure *during conversion*, after both
// sources loaded and parsed cleanly: without an extra guarantee, a
// converter that got halfway through building the new document before
// hitting a bad value could still call Save with a partial result. This
// function structurally cannot do that: conversion (convertToDefinitions)
// runs entirely against local, in-memory values, and
// definitionsBackend.Save is called exactly once, at the very end, only
// after every prior step -- both loads, both parses, and the full
// conversion -- has returned no error. Any failure anywhere before that
// point returns immediately, before Save is ever reached, leaving
// definitionsBackend exactly as it was (no document) and both sources
// completely untouched. See
// TestMigrateDefinitionsRefusesOnConversionFailure, which reproduces
// this with a real value (an out-of-range port number a pre-migration
// watchlist entry could legitimately contain) rather than a synthetic
// hook.
//
// # One failure this function does NOT refuse to start over
//
// Everything above is about protecting existing data: an unreadable
// source, or a conversion that cannot be trusted to be complete, must
// never result in a partial write. A failure to perform the *final*
// Save -- the destination directory does not exist and cannot be
// created, a permission problem, Postgres being briefly unreachable --
// is a different kind of failure with no data to protect: neither
// source was ever touched, and the definitions document still does not
// exist either way, exactly as before this function ran. That failure
// is wrapped in ErrMigrationWriteFailed rather than left
// indistinguishable from a source/conversion failure, so a caller (see
// main.go) can do what every other store in this codebase already does
// when it cannot currently reach its backend: log it and keep running
// with an unmigrated definitions store, not refuse to start the whole
// process. Migration is safely retried on the next boot, since the
// document still does not exist.
func MigrateDefinitions(ctx context.Context, definitionsBackend, detectSettingsBackend, watchlistBackend persist.Backend) (migrated bool, err error) {
	if definitionsBackend == nil {
		// Definitions persistence isn't configured for this deployment --
		// same "empty path disables persistence, not the feature"
		// contract every store in this codebase follows. There is
		// nothing to migrate into, and no backend for
		// OpenDefinitionsStoreWithBackend to seed later either.
		return false, nil
	}

	existing, _, err := persist.LoadDocument(ctx, definitionsBackend)
	if err != nil {
		return false, &persist.StartupError{Store: "the definitions store", Location: definitionsBackend.Describe(), Err: err}
	}
	if existing != nil {
		// Already migrated (or already holds a document of its own) --
		// idempotent no-op, per this function's own doc comment.
		return false, nil
	}

	var settingsDoc map[detect.DetectorName]detect.Settings
	if _, _, err := persist.Open(ctx, detectSettingsBackend, "the detector settings store (definitions migration source)", func(data []byte) error {
		return json.Unmarshal(data, &settingsDoc)
	}); err != nil {
		return false, err
	}

	var wlFile migrateWatchlistFile
	if _, _, err := persist.Open(ctx, watchlistBackend, "the watchlist (definitions migration source)", func(data []byte) error {
		return json.Unmarshal(data, &wlFile)
	}); err != nil {
		return false, err
	}

	defs, err := convertToDefinitions(settingsDoc, wlFile.Entries, detect.DefaultConfig())
	if err != nil {
		return false, fmt.Errorf("engine: converting detector settings/watchlist into definitions: %w", err)
	}

	raw := make(map[string]json.RawMessage, len(defs))
	for id, d := range defs {
		b, err := json.Marshal(d)
		if err != nil {
			return false, fmt.Errorf("engine: encoding migrated definition %q: %w", id, err)
		}
		raw[id] = b
	}
	payload, err := json.MarshalIndent(definitionsDocument{Version: definitionsDocumentVersion, Definitions: raw}, "", "  ")
	if err != nil {
		return false, fmt.Errorf("engine: encoding the migrated definitions document: %w", err)
	}

	if _, err := definitionsBackend.Save(ctx, payload, 0); err != nil {
		if errors.Is(err, persist.ErrConflict) {
			// Another process migrated first (a concurrent boot against
			// the same backend) -- its copy stands; this is a success,
			// not a collision to report, same reasoning
			// persist.AdoptFile gives for the identical race.
			return false, nil
		}
		return false, fmt.Errorf("%w: %v", ErrMigrationWriteFailed, err)
	}
	return true, nil
}

// ErrMigrationWriteFailed marks a failure in MigrateDefinitions' final
// write specifically -- see that function's own doc comment, "One
// failure this function does NOT refuse to start over," for what this
// is and is not: every other error MigrateDefinitions can return (an
// unreadable/unparseable source, wrapped as *persist.StartupError by
// persist.Open; a conversion failure) is deliberately NOT wrapped in
// this, so a caller can tell them apart with errors.Is.
var ErrMigrationWriteFailed = errors.New("engine: writing the migrated definitions document failed")

// convertToDefinitions is MigrateDefinitions's whole in-memory
// conversion step, split out so MigrateDefinitions's own fail-closed
// doc comment can point at one function as "everything that must
// succeed before Save is ever reached."
func convertToDefinitions(settingsDoc map[detect.DetectorName]detect.Settings, entries []*watchlist.Entry, cfg detect.Config) (map[string]Definition, error) {
	out := make(map[string]Definition, len(shippedDetectors)+len(entries))
	if err := convertDetectSettings(settingsDoc, cfg, out); err != nil {
		return nil, err
	}
	if err := convertWatchlistEntries(entries, out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- internal/detect.SettingsStore -> shipped definitions --------------

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
// Kind is KindProgrammatic for all twelve, uniformly -- a decision this
// migration makes on its own, recorded here since docs/decisions/
// evaluation-engine.md's illustrative list names some of these as future
// "shipped declarative definitions." That is #405's job, not this one:
// Definition has no structured-condition representation yet (no package
// in this repository defines one as of #404), so marking any of these
// Declarative now would claim a data-driven condition set that does not
// exist. Programmatic is the honest classification for "built-in Go,
// wearing the envelope" today; #405 is free to migrate specific
// detectors to Declarative once real condition data exists for them --
// that is an Upsert against this store, not a reason to block on it here.
type shippedDetector struct {
	name   detect.DetectorName
	schema []ParamSchema
	params func(cfg detect.Config) Params
}

// shippedDetectorDisplayNames gives each of the 12 settings-toggleable
// detectors an operator-facing Name -- detect.DetectorName's own values
// (e.g. "low_slow_scan") are machine keys, not display text, mirroring
// why Definition.ID is never the display name (see that field's own doc
// comment).
var shippedDetectorDisplayNames = map[detect.DetectorName]string{
	detect.DetectorPortScan:              "Port scan",
	detect.DetectorActivitySpike:         "Activity spike",
	detect.DetectorCriticalPort:          "Critical port",
	detect.DetectorGlobalSpike:           "Global spike",
	detect.DetectorDistributedBruteForce: "Distributed brute force",
	detect.DetectorOutboundAnomaly:       "Outbound anomaly",
	detect.DetectorInternalRecon:         "Internal recon",
	detect.DetectorRuleSpike:             "Rule spike",
	detect.DetectorRepeatedDrops:         "Repeated drops",
	detect.DetectorLowSlowScan:           "Low & slow scan",
	detect.DetectorOffHoursActivity:      "Off-hours activity",
	detect.DetectorDeviceSilence:         "Device silence",
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
	{detect.DetectorPortScan, PortScanParamSchema, func(c detect.Config) Params {
		return Params{"threshold": c.PortScanThreshold, "window": c.PortScanWindow.String()}
	}},
	{detect.DetectorActivitySpike, ActivitySpikeParamSchema, func(c detect.Config) Params {
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
	{detect.DetectorCriticalPort, CriticalPortParamSchema, func(c detect.Config) Params {
		return Params{"ports": c.CriticalPorts, "threshold": c.CriticalPortThreshold, "window": c.CriticalPortWindow.String()}
	}},
	{detect.DetectorGlobalSpike, GlobalSpikeParamSchema, func(c detect.Config) Params {
		return Params{
			"multiplier":            c.GlobalSpikeMultiplier,
			"minEPS":                c.GlobalSpikeMinEPS,
			"warmupSamples":         c.GlobalSpikeWarmupSamples,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": zeroDuration,
		}
	}},
	{detect.DetectorDistributedBruteForce, DistributedBruteForceParamSchema, func(c detect.Config) Params {
		return Params{"threshold": c.DistributedBruteForceThreshold, "window": c.DistributedBruteForceWindow.String()}
	}},
	{detect.DetectorOutboundAnomaly, OutboundAnomalyParamSchema, func(c detect.Config) Params {
		return Params{
			"threshold":               c.OutboundAnomalyThreshold,
			"window":                  c.OutboundAnomalyWindow.String(),
			"vpnInterfaces":           c.VPNInterfaces,
			"vpnConfidenceMultiplier": c.VPNConfidenceMultiplier,
		}
	}},
	{detect.DetectorInternalRecon, InternalReconParamSchema, func(c detect.Config) Params {
		return Params{
			"threshold":               c.InternalReconThreshold,
			"window":                  c.InternalReconWindow.String(),
			"vpnInterfaces":           c.VPNInterfaces,
			"vpnConfidenceMultiplier": c.VPNConfidenceMultiplier,
		}
	}},
	{detect.DetectorRuleSpike, RuleSpikeParamSchema, func(c detect.Config) Params {
		return Params{
			"multiplier":            c.RuleSpikeMultiplier,
			"minRate":               c.RuleSpikeMinRate,
			"window":                c.RuleSpikeWindow.String(),
			"warmupSamples":         c.RuleSpikeWarmupSamples,
			"updateCadence":         "perEvent",
			"baselineFloorDuration": zeroDuration,
		}
	}},
	{detect.DetectorRepeatedDrops, RepeatedDropsParamSchema, func(c detect.Config) Params {
		return Params{"threshold": c.RepeatedDropsThreshold, "window": c.RepeatedDropsWindow.String()}
	}},
	{detect.DetectorLowSlowScan, LowSlowScanParamSchema, func(c detect.Config) Params {
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
	{detect.DetectorOffHoursActivity, OffHoursActivityParamSchema, func(c detect.Config) Params {
		return Params{
			"startHour":     c.OffHoursStartHour,
			"endHour":       c.OffHoursEndHour,
			"minSampleDays": c.OffHoursMinSampleDays,
			"minCount":      c.OffHoursMinCount,
			"updateCadence": "perEvent",
		}
	}},
	{detect.DetectorDeviceSilence, DeviceSilenceParamSchema, func(c detect.Config) Params {
		return Params{"staleAfter": c.DeviceStaleAfter.String()}
	}},
}

// convertDetectScope maps detect.Scope onto engine.Scope -- the two are
// structurally identical (issue #401 copied one from the other verbatim;
// see Scope's own doc comment in definition.go), so this is a field-by-
// field type conversion, not a semantic one.
func convertDetectScope(s detect.Scope) Scope {
	return Scope{
		Hosts:          s.Hosts,
		HostsMode:      ListMode(s.HostsMode),
		Ports:          s.Ports,
		PortsMode:      ListMode(s.PortsMode),
		Classification: s.Classification,
		Rules:          s.Rules,
		RulesMode:      ListMode(s.RulesMode),
	}
}

// convertDetectSettings builds every shipped detector definition (always
// all 12, regardless of what settingsDoc contains -- see shippedDetector's
// own doc comment) plus a preserved-but-unavailable placeholder for any
// settingsDoc entry keyed by a name this binary's detect.AllDetectorNames
// does not include. That second case is deliberately not an error: a
// detector name settingsDoc doesn't recognize is well-formed data, not
// corruption, and nothing operator-authored is dropped for it either --
// its enabled/scope survives inside the placeholder's Params, and
// decodeStored's availability check (definitions_store.go) marks the
// placeholder unavailable via an empty Kind, so it is preserved
// byte-for-byte on every future write without ever being evaluated. See
// TestMigrateDefinitionsUnrecognizedDetectorNameIsPreservedUnavailable.
func convertDetectSettings(settingsDoc map[detect.DetectorName]detect.Settings, cfg detect.Config, out map[string]Definition) error {
	for _, sd := range shippedDetectors {
		settings, ok := settingsDoc[sd.name]
		if !ok {
			// Matches detect.DefaultSettingsMap()'s own default: enabled,
			// unscoped.
			settings = detect.Settings{Enabled: true}
		}
		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			return fmt.Errorf("shipped detector %q: building default params: %w", sd.name, err)
		}
		out[string(sd.name)] = Definition{
			ID:          string(sd.name),
			Name:        shippedDetectorDisplayNames[sd.name],
			Description: fmt.Sprintf("Migrated from internal/detect's %q detector settings (issue #404).", sd.name),
			Intent:      IntentDetection,
			Kind:        KindProgrammatic,
			Enabled:     settings.Enabled,
			Scope:       convertDetectScope(settings.Scope),
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		}
	}

	for name, settings := range settingsDoc {
		if detect.IsValidDetectorName(name) {
			continue // handled above, with its real shipped schema/defaults
		}
		id := "legacy-detector:" + string(name)
		scopeJSON, err := json.Marshal(settings.Scope)
		if err != nil {
			return fmt.Errorf("unrecognized detector %q: encoding its scope: %w", name, err)
		}
		out[id] = Definition{
			ID:          id,
			Name:        string(name) + " (unrecognized detector)",
			Description: "Preserved from a detector settings entry this binary's shipped catalogue does not recognize -- see StoredDefinition.Available. Not evaluated, never dropped.",
			Intent:      IntentDetection,
			// Kind is deliberately left as the zero value (not
			// KindProgrammatic/KindDeclarative): decodeStored
			// (definitions_store.go) treats an unrecognized Kind as
			// Available == false, which is exactly what an entry this
			// binary cannot identify at all should be.
			Enabled: settings.Enabled,
			Params: Params{
				"legacyDetectorName": string(name),
				"legacyEnabled":      settings.Enabled,
				"legacyScopeJSON":    string(scopeJSON),
			},
			Provenance: Provenance{Origin: ProvenanceShipped},
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
}

// convertWatchlistEntries converts every watchlist entry into an
// expectation definition, keyed by the entry's own ID -- preserving
// identity across the migration rather than generating a fresh one,
// since a stable, predictable ID is what makes this migration
// idempotent and lets a future direct reference (a UI link, a saved
// filter) keep working across the move.
func convertWatchlistEntries(entries []*watchlist.Entry, out map[string]Definition) error {
	for _, e := range entries {
		// A JSON array containing `null` unmarshals to a nil *Entry --
		// same guard watchlist.OpenWithBackend's own decode closure
		// applies.
		if e == nil || e.ID == "" {
			continue
		}
		d, err := convertWatchlistEntry(e)
		if err != nil {
			return fmt.Errorf("watchlist entry %q: %w", e.ID, err)
		}
		out[d.ID] = d
	}
	return nil
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

func convertNonInvertedEntry(e *watchlist.Entry, name string) (Definition, error) {
	params := Params{
		"ports":            e.Ports,
		"destIp":           optionalStringList(e.DestIP),
		"sourceMac":        optionalStringList(e.Source.MAC),
		"sourceIp":         optionalStringList(e.Source.IP),
		"sourceListDevice": optionalStringList(e.SourceList.Device),
		"sourceListList":   optionalStringList(e.SourceList.List),
		"createdAt":        optionalStringList(formatTime(e.CreatedAt)),
	}
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
