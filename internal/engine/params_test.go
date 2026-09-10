// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"reflect"
	"testing"
)

// configFieldMapping ties one DetectorDefaults field to the
// shipped ParamSchema (shipped_params.go) and param name that expresses
// it. A field consulted by more than one detector (VPNInterfaces,
// VPNConfidenceMultiplier -- see shipped_params.go's own doc comment)
// appears more than once.
type configFieldMapping struct {
	configField string
	schema      []ParamSchema
	paramName   string
}

var configFieldMappings = []configFieldMapping{
	{"PortScanThreshold", PortScanParamSchema, "threshold"},
	{"PortScanWindow", PortScanParamSchema, "window"},

	{"ActivitySpikeThreshold", ActivitySpikeParamSchema, "threshold"},
	{"ActivitySpikeWindow", ActivitySpikeParamSchema, "window"},
	{"HostActivityMultiplier", ActivitySpikeParamSchema, "baselineMultiplier"},
	{"HostActivityWarmupSamples", ActivitySpikeParamSchema, "warmupSamples"},

	{"CriticalPorts", CriticalPortParamSchema, "ports"},
	{"CriticalPortThreshold", CriticalPortParamSchema, "threshold"},
	{"CriticalPortWindow", CriticalPortParamSchema, "window"},

	{"GlobalSpikeMultiplier", GlobalSpikeParamSchema, "multiplier"},
	{"GlobalSpikeMinEPS", GlobalSpikeParamSchema, "minEPS"},
	{"GlobalSpikeWarmupSamples", GlobalSpikeParamSchema, "warmupSamples"},

	{"DistributedBruteForceThreshold", DistributedBruteForceParamSchema, "threshold"},
	{"DistributedBruteForceWindow", DistributedBruteForceParamSchema, "window"},

	{"OutboundAnomalyThreshold", OutboundAnomalyParamSchema, "threshold"},
	{"OutboundAnomalyWindow", OutboundAnomalyParamSchema, "window"},

	{"InternalReconThreshold", InternalReconParamSchema, "threshold"},
	{"InternalReconWindow", InternalReconParamSchema, "window"},

	{"RuleSpikeMultiplier", RuleSpikeParamSchema, "multiplier"},
	{"RuleSpikeMinRate", RuleSpikeParamSchema, "minRate"},
	{"RuleSpikeWindow", RuleSpikeParamSchema, "window"},
	{"RuleSpikeWarmupSamples", RuleSpikeParamSchema, "warmupSamples"},

	{"RepeatedDropsThreshold", RepeatedDropsParamSchema, "threshold"},
	{"RepeatedDropsWindow", RepeatedDropsParamSchema, "window"},

	{"LowSlowScanWindow", LowSlowScanParamSchema, "window"},
	{"LowSlowScanPortThreshold", LowSlowScanParamSchema, "portThreshold"},
	{"LowSlowScanHostThreshold", LowSlowScanParamSchema, "hostThreshold"},
	{"LowSlowScanMinObservation", LowSlowScanParamSchema, "minObservation"},
	{"LowSlowScanDropRatio", LowSlowScanParamSchema, "dropRatio"},
	{"LowSlowScanBaselineMultiplier", LowSlowScanParamSchema, "baselineMultiplier"},

	{"OffHoursStartHour", OffHoursActivityParamSchema, "startHour"},
	{"OffHoursEndHour", OffHoursActivityParamSchema, "endHour"},
	{"OffHoursMinSampleDays", OffHoursActivityParamSchema, "minSampleDays"},
	{"OffHoursMinCount", OffHoursActivityParamSchema, "minCount"},

	{"DeviceStaleAfter", DeviceSilenceParamSchema, "staleAfter"},

	// VPNInterfaces/VPNConfidenceMultiplier: consulted by three
	// detectors, see shipped_params.go's doc comment.
	{"VPNInterfaces", ActivitySpikeParamSchema, "vpnInterfaces"},
	{"VPNConfidenceMultiplier", ActivitySpikeParamSchema, "vpnConfidenceMultiplier"},
	{"VPNInterfaces", OutboundAnomalyParamSchema, "vpnInterfaces"},
	{"VPNConfidenceMultiplier", OutboundAnomalyParamSchema, "vpnConfidenceMultiplier"},
	{"VPNInterfaces", InternalReconParamSchema, "vpnInterfaces"},
	{"VPNConfidenceMultiplier", InternalReconParamSchema, "vpnConfidenceMultiplier"},
}

// TestShippedParamSchemaCoversEveryConfigField is issue #401's required
// field-by-field walk, proven rather than asserted: it reflects over
// DetectorDefaults' actual fields and fails if any of them has no entry in
// configFieldMappings above, and separately fails if a mapping claims a
// param name that doesn't actually exist in the schema it points at (a
// stale mapping would otherwise pass silently). shipped_params.go has
// exactly one []ParamSchema per threshold detector -- see
// TestShippedParamSchemaHasOneSliceForEveryDetector below.
func TestShippedParamSchemaCoversEveryConfigField(t *testing.T) {
	covered := map[string]bool{}
	for _, m := range configFieldMappings {
		covered[m.configField] = true
		found := false
		for _, entry := range m.schema {
			if entry.Name == m.paramName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mapping for DetectorDefaults.%s claims param %q, but no such entry exists in the schema it points at", m.configField, m.paramName)
		}
	}

	cfgType := reflect.TypeOf(DetectorDefaults{})
	for i := 0; i < cfgType.NumField(); i++ {
		name := cfgType.Field(i).Name
		if !covered[name] {
			t.Errorf("DetectorDefaults.%s has no ParamSchema mapping -- every shipped detector param must be expressed as a schema entry, see docs/decisions/evaluation-engine.md section 4", name)
		}
	}
}

// TestShippedParamSchemaHasOneSliceForEveryDetector confirms
// shipped_params.go's coverage extends to detector identity too, not
// just field count: each of the twelve threshold detectors the schemas
// below were written against is a real shipped definition id with a
// non-empty ParamSchema slice in this file.
func TestShippedParamSchemaHasOneSliceForEveryDetector(t *testing.T) {
	byDetector := map[string][]ParamSchema{
		"port_scan":               PortScanParamSchema,
		"activity_spike":          ActivitySpikeParamSchema,
		"critical_port":           CriticalPortParamSchema,
		"global_spike":            GlobalSpikeParamSchema,
		"distributed_brute_force": DistributedBruteForceParamSchema,
		"outbound_anomaly":        OutboundAnomalyParamSchema,
		"internal_recon":          InternalReconParamSchema,
		"rule_spike":              RuleSpikeParamSchema,
		"repeated_drops":          RepeatedDropsParamSchema,
		"low_slow_scan":           LowSlowScanParamSchema,
		"off_hours_activity":      OffHoursActivityParamSchema,
		"device_silence":          DeviceSilenceParamSchema,
	}
	for name, schema := range byDetector {
		if !IsShippedDefinitionID(name) {
			t.Errorf("detector %q has a shipped ParamSchema but is not in the shipped catalogue", name)
		}
		if len(schema) == 0 {
			t.Errorf("detector %q has an empty ParamSchema", name)
		}
	}
}

// --- ValidateParams ---

func TestValidateParamsNormalizesAndRoundTrips(t *testing.T) {
	schema := []ParamSchema{
		{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1), Required: true},
		{Name: "window", Type: ParamTypeDuration, Min: durationBound(0)},
		{Name: "multiplier", Type: ParamTypeFloat},
		{Name: "enabled", Type: ParamTypeBool},
		{Name: "mode", Type: ParamTypeEnum, EnumValues: []string{"a", "b"}},
		{Name: "ports", Type: ParamTypePortList},
		{Name: "hosts", Type: ParamTypeHostList},
		{Name: "patterns", Type: ParamTypeStringList},
	}
	values := Params{
		"threshold":  float64(15), // JSON-decoded shape
		"window":     "1m30s",
		"multiplier": 2.5,
		"enabled":    true,
		"mode":       "b",
		"ports":      []any{float64(22), float64(443)},
		"hosts":      []any{"10.0.0.1", "192.168.0.0/24"},
		"patterns":   []any{"wireguard*"},
	}

	out, err := ValidateParams(schema, values)
	if err != nil {
		t.Fatalf("ValidateParams: %v", err)
	}
	if out["threshold"] != 15 {
		t.Errorf("threshold = %#v, want normalized int 15", out["threshold"])
	}
	if out["window"] != "1m30s" {
		t.Errorf("window = %#v, want %q", out["window"], "1m30s")
	}
	ports, ok := out["ports"].([]int)
	if !ok || len(ports) != 2 || ports[0] != 22 || ports[1] != 443 {
		t.Errorf("ports = %#v, want []int{22, 443}", out["ports"])
	}
	hosts, ok := out["hosts"].([]string)
	if !ok || len(hosts) != 2 {
		t.Errorf("hosts = %#v, want 2 normalized entries", out["hosts"])
	}
}

func TestValidateParamsRejectsUnknownParam(t *testing.T) {
	_, err := ValidateParams(nil, Params{"nope": 1})
	if err == nil {
		t.Fatal("ValidateParams succeeded on a param not declared in the schema, want a hard failure")
	}
}

func TestValidateParamsRejectsMissingRequired(t *testing.T) {
	schema := []ParamSchema{{Name: "threshold", Type: ParamTypeInt, Required: true}}
	_, err := ValidateParams(schema, Params{})
	if err == nil {
		t.Fatal("ValidateParams succeeded with a required param missing, want a hard failure")
	}
}

func TestValidateParamsRejectsOutOfBounds(t *testing.T) {
	schema := []ParamSchema{{Name: "ratio", Type: ParamTypeFloat, Min: floatBound(0), Max: floatBound(1)}}
	_, err := ValidateParams(schema, Params{"ratio": 1.5})
	if err == nil {
		t.Fatal("ValidateParams succeeded on a value above Max, want a hard failure")
	}
}

func TestValidateParamsRejectsWrongType(t *testing.T) {
	schema := []ParamSchema{{Name: "enabled", Type: ParamTypeBool}}
	_, err := ValidateParams(schema, Params{"enabled": "yes"})
	if err == nil {
		t.Fatal("ValidateParams succeeded with a string where a bool was declared, want a hard failure -- malformed values must never be stored to be read back as a zero value")
	}
}

func TestValidateParamsRejectsInvalidDuration(t *testing.T) {
	schema := []ParamSchema{{Name: "window", Type: ParamTypeDuration}}
	_, err := ValidateParams(schema, Params{"window": "not-a-duration"})
	if err == nil {
		t.Fatal("ValidateParams succeeded on an unparseable duration string, want a hard failure")
	}
}

func TestValidateParamsRejectsInvalidEnum(t *testing.T) {
	schema := []ParamSchema{{Name: "mode", Type: ParamTypeEnum, EnumValues: []string{"a", "b"}}}
	_, err := ValidateParams(schema, Params{"mode": "c"})
	if err == nil {
		t.Fatal("ValidateParams succeeded on a value outside EnumValues, want a hard failure")
	}
}

func TestValidateParamsRejectsPortOutOfRange(t *testing.T) {
	schema := []ParamSchema{{Name: "ports", Type: ParamTypePortList}}
	_, err := ValidateParams(schema, Params{"ports": []any{float64(70000)}})
	if err == nil {
		t.Fatal("ValidateParams succeeded on a port above 65535, want a hard failure")
	}
}

func TestValidateParamsRejectsMalformedHost(t *testing.T) {
	schema := []ParamSchema{{Name: "hosts", Type: ParamTypeHostList}}
	_, err := ValidateParams(schema, Params{"hosts": []any{"not an ip or cidr"}})
	if err == nil {
		t.Fatal("ValidateParams succeeded on a hostList entry that is neither an IP nor a CIDR, want a hard failure")
	}
}

func TestValidateParamsAcceptsAnyStringInStringList(t *testing.T) {
	schema := []ParamSchema{{Name: "patterns", Type: ParamTypeStringList}}
	out, err := ValidateParams(schema, Params{"patterns": []any{"wireguard*", "not-an-ip-or-cidr-either"}})
	if err != nil {
		t.Fatalf("ValidateParams: %v -- stringList must accept glob patterns, unlike hostList", err)
	}
	if got := out["patterns"].([]string); len(got) != 2 {
		t.Errorf("patterns = %#v, want 2 entries", got)
	}
}

func TestValidateParamsEnforcesListMaxLength(t *testing.T) {
	schema := []ParamSchema{{Name: "ports", Type: ParamTypePortList, Max: floatBound(1)}}
	_, err := ValidateParams(schema, Params{"ports": []any{float64(22), float64(443)}})
	if err == nil {
		t.Fatal("ValidateParams succeeded with 2 entries against a Max of 1, want a hard failure")
	}
}

func TestValidateParamsRejectsNonIntegralInt(t *testing.T) {
	schema := []ParamSchema{{Name: "threshold", Type: ParamTypeInt}}
	_, err := ValidateParams(schema, Params{"threshold": 1.5})
	if err == nil {
		t.Fatal("ValidateParams succeeded with a non-integral value for an int param, want a hard failure")
	}
}

// TestValidateParamsEveryShippedSchemaValidatesItsOwnDefaults sanity-
// checks every shipped schema against a representative value for each
// entry (its Min bound, or a valid enum/example) -- catching a schema
// entry whose own bounds are internally inconsistent (e.g. Min > Max)
// or whose Type doesn't match what a real value for it would look like.
func TestValidateParamsEveryShippedSchemaValidatesItsOwnDefaults(t *testing.T) {
	all := [][]ParamSchema{
		PortScanParamSchema, ActivitySpikeParamSchema, CriticalPortParamSchema,
		GlobalSpikeParamSchema, DistributedBruteForceParamSchema, OutboundAnomalyParamSchema,
		InternalReconParamSchema, RuleSpikeParamSchema, RepeatedDropsParamSchema,
		LowSlowScanParamSchema, OffHoursActivityParamSchema, DeviceSilenceParamSchema,
	}
	for _, schema := range all {
		values := Params{}
		for _, entry := range schema {
			values[entry.Name] = exampleValueFor(t, entry)
		}
		if _, err := ValidateParams(schema, values); err != nil {
			t.Errorf("schema %v rejected its own representative values: %v", schema, err)
		}
	}
}

func exampleValueFor(t *testing.T, s ParamSchema) any {
	t.Helper()
	switch s.Type {
	case ParamTypeInt, ParamTypeFloat:
		if s.Min != nil {
			return *s.Min
		}
		return float64(1)
	case ParamTypeDuration:
		return "1m"
	case ParamTypeBool:
		return true
	case ParamTypeEnum:
		if len(s.EnumValues) == 0 {
			t.Fatalf("enum param %q has no EnumValues", s.Name)
		}
		return s.EnumValues[0]
	case ParamTypePortList:
		return []any{float64(443)}
	case ParamTypeHostList:
		return []any{"10.0.0.1"}
	case ParamTypeStringList:
		return []any{"wireguard*"}
	default:
		t.Fatalf("unhandled ParamType %q in test helper", s.Type)
		return nil
	}
}
