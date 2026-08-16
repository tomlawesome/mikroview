// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"
)

// ParamType is one param's data type -- what ParamSchema declares and
// ValidateParams checks a Params value against. The base set is exactly
// what issue #401 asked for (int, duration, float, port list, host
// list, enum, bool); ParamTypeStringList was added during the
// detect.Config field-by-field walk (see shipped_params.go) for
// detect.Config.VPNInterfaces, whose values are interface-name glob
// patterns ("wireguard1", "wireguard*") -- not IP/CIDR entries, so
// ParamTypeHostList's validation would reject every real value of it.
type ParamType string

const (
	ParamTypeInt        ParamType = "int"
	ParamTypeDuration   ParamType = "duration"
	ParamTypeFloat      ParamType = "float"
	ParamTypePortList   ParamType = "portList"
	ParamTypeHostList   ParamType = "hostList"
	ParamTypeStringList ParamType = "stringList"
	ParamTypeEnum       ParamType = "enum"
	ParamTypeBool       ParamType = "bool"
)

// ParamSchema declares one param a definition accepts: its name, type,
// bounds, unit and a one-line description -- what the builder/settings
// UI renders a field from and what auto-tune is allowed to move within
// (docs/decisions/evaluation-engine.md section 4). A Definition carries
// its own []ParamSchema (see Definition.ParamSchema) -- "per-definition
// schema" per the ADR's own phrase.
//
// Bounds (Min/Max) are interpreted per Type:
//   - int, float: the raw numeric bound.
//   - duration: the bound in nanoseconds (float64(time.Duration)) --
//     e.g. Min=float64(time.Second) means "at least one second."
//   - portList, hostList, stringList: Max bounds the number of entries;
//     Min is not meaningful and is ignored.
//   - enum, bool: neither bound is meaningful; use EnumValues for enum.
//
// Pointers (not a zero value) so "no bound" is distinguishable from "the
// bound is exactly zero" (e.g. LowSlowScanDropRatio's Min really is 0).
type ParamSchema struct {
	Name        string    `json:"name"`
	Type        ParamType `json:"type"`
	Description string    `json:"description"`
	Unit        string    `json:"unit,omitempty"`
	Required    bool      `json:"required,omitempty"`
	Min         *float64  `json:"min,omitempty"`
	Max         *float64  `json:"max,omitempty"`
	// EnumValues is the closed set of accepted values for
	// ParamTypeEnum -- unused (and unchecked) for every other Type.
	EnumValues []string `json:"enumValues,omitempty"`
}

// Params is a definition's typed param values, keyed by ParamSchema
// name. Deliberately map[string]any rather than a wrapped/discriminated
// value type: the JSON shape this produces is a plain object
// (`{"threshold": 15, "window": "60s"}`), which is what #404's store and
// #407's API consume as-is (see the golden-file test) -- a discriminated
// wrapper would leak an internal type-tag into a shape a UI has to
// render directly. ValidateParams is what keeps a Params value honest
// against a []ParamSchema; nothing in this package trusts an
// unvalidated Params map.
type Params map[string]any

// Min/Max helpers -- construct a *float64 bound inline without a
// caller-visible temporary variable, since a composite literal cannot
// take the address of a literal directly.
func floatBound(v float64) *float64 { return &v }

// durationBound is floatBound for a duration bound expressed as a
// time.Duration for readability at the call site (see shipped_params.go)
// -- converted to the nanosecond float64 ParamSchema.Min/Max actually
// store.
func durationBound(d time.Duration) *float64 { return floatBound(float64(d)) }

// ValidateParams checks values against schema and returns a normalized
// copy: every present param is type-checked, range-checked and coerced
// to its canonical Go representation (e.g. a JSON-decoded float64 for a
// ParamTypeInt entry becomes an int) -- validation happens at this one
// boundary so a malformed value is rejected here, never stored to be
// silently read back as a zero value later.
//
// An unknown param name (present in values but not declared in schema)
// is a hard error, not silently ignored -- the same reasoning
// RenderEmission's unaccumulated-value check uses (emission.go): a typo
// or a stale param from a since-changed schema must fail loudly. A
// missing Required param is also a hard error. A missing, non-required
// param is simply absent from the returned map.
func ValidateParams(schema []ParamSchema, values Params) (Params, error) {
	bySchema := make(map[string]ParamSchema, len(schema))
	for _, s := range schema {
		bySchema[s.Name] = s
	}

	out := make(Params, len(values))
	for name, raw := range values {
		s, ok := bySchema[name]
		if !ok {
			return nil, fmt.Errorf("param %q is not declared in this definition's schema", name)
		}
		v, err := validateParamValue(s, raw)
		if err != nil {
			return nil, fmt.Errorf("param %q: %w", name, err)
		}
		out[name] = v
	}

	for _, s := range schema {
		if s.Required {
			if _, ok := out[s.Name]; !ok {
				return nil, fmt.Errorf("param %q is required", s.Name)
			}
		}
	}
	return out, nil
}

func validateParamValue(s ParamSchema, raw any) (any, error) {
	switch s.Type {
	case ParamTypeInt:
		return validateIntParam(s, raw)
	case ParamTypeFloat:
		return validateFloatParam(s, raw)
	case ParamTypeDuration:
		return validateDurationParam(s, raw)
	case ParamTypeBool:
		return validateBoolParam(raw)
	case ParamTypeEnum:
		return validateEnumParam(s, raw)
	case ParamTypePortList:
		return validateIntListParam(raw, s.Max, 1, 65535, "port")
	case ParamTypeHostList:
		return validateHostListParam(raw, s.Max)
	case ParamTypeStringList:
		return validateStringListParam(raw, s.Max)
	default:
		return nil, fmt.Errorf("unknown schema type %q", s.Type)
	}
}

func asFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func validateIntParam(s ParamSchema, raw any) (any, error) {
	f, ok := asFloat(raw)
	if !ok {
		return nil, fmt.Errorf("want an integer, got %T", raw)
	}
	if f != float64(int64(f)) {
		return nil, fmt.Errorf("want an integer, got non-integral value %v", f)
	}
	if err := checkBounds(s, f); err != nil {
		return nil, err
	}
	return int(f), nil
}

func validateFloatParam(s ParamSchema, raw any) (any, error) {
	f, ok := asFloat(raw)
	if !ok {
		return nil, fmt.Errorf("want a number, got %T", raw)
	}
	if err := checkBounds(s, f); err != nil {
		return nil, err
	}
	return f, nil
}

func validateDurationParam(s ParamSchema, raw any) (any, error) {
	str, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("want a duration string (e.g. \"60s\"), got %T", raw)
	}
	d, err := time.ParseDuration(str)
	if err != nil {
		return nil, fmt.Errorf("invalid duration %q: %w", str, err)
	}
	if err := checkBounds(s, float64(d)); err != nil {
		return nil, err
	}
	return d.String(), nil
}

func validateBoolParam(raw any) (any, error) {
	b, ok := raw.(bool)
	if !ok {
		return nil, fmt.Errorf("want a boolean, got %T", raw)
	}
	return b, nil
}

func validateEnumParam(s ParamSchema, raw any) (any, error) {
	str, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("want a string, got %T", raw)
	}
	for _, allowed := range s.EnumValues {
		if str == allowed {
			return str, nil
		}
	}
	return nil, fmt.Errorf("value %q is not one of %v", str, s.EnumValues)
}

func checkBounds(s ParamSchema, v float64) error {
	if s.Min != nil && v < *s.Min {
		return fmt.Errorf("value %v is below the minimum %v", v, *s.Min)
	}
	if s.Max != nil && v > *s.Max {
		return fmt.Errorf("value %v exceeds the maximum %v", v, *s.Max)
	}
	return nil
}

func rawList(raw any) ([]any, bool) {
	list, ok := raw.([]any)
	if ok {
		return list, true
	}
	// Also accept a caller-constructed native slice (int/string), the
	// shape a Go-side seeder (e.g. shipped_params.go's own defaults)
	// naturally produces, as opposed to values decoded off JSON.
	switch v := raw.(type) {
	case []int:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = x
		}
		return out, true
	case []string:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = x
		}
		return out, true
	default:
		return nil, false
	}
}

func validateIntListParam(raw any, max *float64, lo, hi int, noun string) (any, error) {
	list, ok := rawList(raw)
	if !ok {
		return nil, fmt.Errorf("want a list, got %T", raw)
	}
	if max != nil && float64(len(list)) > *max {
		return nil, fmt.Errorf("%d entries exceeds the maximum of %v", len(list), *max)
	}
	out := make([]int, 0, len(list))
	for _, item := range list {
		f, ok := asFloat(item)
		if !ok || f != float64(int64(f)) {
			return nil, fmt.Errorf("entry %v is not an integer", item)
		}
		n := int(f)
		if n < lo || n > hi {
			return nil, fmt.Errorf("%s %d is out of range [%d, %d]", noun, n, lo, hi)
		}
		out = append(out, n)
	}
	return out, nil
}

func validateHostListParam(raw any, max *float64) (any, error) {
	list, ok := rawList(raw)
	if !ok {
		return nil, fmt.Errorf("want a list, got %T", raw)
	}
	if max != nil && float64(len(list)) > *max {
		return nil, fmt.Errorf("%d entries exceeds the maximum of %v", len(list), *max)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entry %v is not a string", item)
		}
		if !hostEntryValid(str) {
			return nil, fmt.Errorf("entry %q is not a valid IP or CIDR", str)
		}
		out = append(out, str)
	}
	return out, nil
}

func validateStringListParam(raw any, max *float64) (any, error) {
	list, ok := rawList(raw)
	if !ok {
		return nil, fmt.Errorf("want a list, got %T", raw)
	}
	if max != nil && float64(len(list)) > *max {
		return nil, fmt.Errorf("%d entries exceeds the maximum of %v", len(list), *max)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("entry %v is not a string", item)
		}
		out = append(out, str)
	}
	return out, nil
}
