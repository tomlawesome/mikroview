// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// update regenerates every golden file this test compares against,
// instead of comparing against it -- the standard shape for this
// repository's first golden-file test (see this package's own AGENTS.md
// note that no prior convention existed to follow): run
//
//	go test ./internal/engine/ -run TestDefinitionEnvelopeJSONShape -update
//
// after a deliberate, reviewed change to Definition's JSON shape, then
// diff testdata/definition_golden.json in the resulting commit so the
// shape change is visible in review rather than silently baked in.
var update = flag.Bool("update", false, "update golden files in testdata/ instead of comparing against them")

// goldenDefinition is a fully-populated Definition -- every field set to
// a non-zero-ish value where the field is optional, so an accidentally
// dropped `omitempty` or a field silently never getting marshaled would
// actually change the golden output rather than passing by coincidence.
// ID is a fixed literal, not NewDefinition's generated one, so this test
// is reproducible.
func goldenDefinition() Definition {
	return Definition{
		ID:          "0123456789abcdef0123456789abcdef",
		Name:        "Port scan",
		Description: "Many distinct destination ports from one source in a short window.",
		Intent:      IntentDetection,
		Kind:        KindDeclarative,
		Enabled:     false,
		Scope: Scope{
			Hosts:          []string{"10.0.0.0/24"},
			HostsMode:      ListModeDeny,
			Ports:          []int{22, 443},
			PortsMode:      ListModeAllow,
			Classification: store.ScopeExternal,
			Rules:          []string{"lan-in"},
			RulesMode:      ListModeAllow,
		},
		Params: Params{
			"threshold": float64(15),
			"window":    "60s",
		},
		ParamSchema: []ParamSchema{
			{
				Name:        "threshold",
				Type:        ParamTypeInt,
				Description: "Distinct destination ports from one source within the window that counts as a port scan.",
				Unit:        "distinct ports",
				Required:    true,
				Min:         floatBound(1),
			},
			{
				Name:        "window",
				Type:        ParamTypeDuration,
				Description: "Rolling window the distinct-port count is measured over.",
				Required:    true,
				Min:         durationBound(time.Second),
			},
		},
		Provenance: Provenance{
			Origin: ProvenanceShipped,
			ShippedParams: Params{
				"threshold": float64(15),
				"window":    "60s",
			},
		},
		Suppressions: []Suppression{
			{ID: "supp-1", Target: "198.51.100.9", Reason: "known monitoring host"},
		},
	}
}

// TestDefinitionEnvelopeJSONShape pins Definition's JSON shape
// byte-for-byte against testdata/definition_golden.json -- issue #401's
// requirement that the envelope's wire shape is fixed and deliberate,
// not whatever `json.Marshal` happens to currently produce. #404's store
// and #407's API consume this shape as-is; a change here is a review
// signal, not something that should be able to happen silently as a
// side effect of reordering or renaming a Go field.
func TestDefinitionEnvelopeJSONShape(t *testing.T) {
	got, err := json.MarshalIndent(goldenDefinition(), "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "definition_golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("MkdirAll testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v (run with -update to create it)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("Definition JSON shape does not match %s -- if this is a deliberate, reviewed shape change, rerun with -update:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

// TestDefinitionEnvelopeJSONRoundTrips proves the golden shape is not
// merely stable but actually decodes back into an equal value -- a
// golden-file comparison alone would not catch an asymmetric field (one
// that marshals but never unmarshals correctly, e.g. a type mismatch
// hidden by `any`).
func TestDefinitionEnvelopeJSONRoundTrips(t *testing.T) {
	orig := goldenDefinition()
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Definition
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Params/ShippedParams round-trip through JSON as map[string]any,
	// whose values decode as float64/string/[]interface{} regardless of
	// what Go-native type was marshaled -- re-validate against the
	// schema (the boundary this package actually cares about) rather
	// than asserting Go-level deep equality against pre-decode values.
	if _, err := ValidateParams(decoded.ParamSchema, decoded.Params); err != nil {
		t.Errorf("decoded.Params failed re-validation against decoded.ParamSchema: %v", err)
	}
	if decoded.ID != orig.ID || decoded.Name != orig.Name || decoded.Intent != orig.Intent || decoded.Kind != orig.Kind {
		t.Errorf("decoded envelope fields do not match original: got %+v", decoded)
	}
	if len(decoded.Suppressions) != len(orig.Suppressions) {
		t.Errorf("decoded.Suppressions = %v, want %d entries", decoded.Suppressions, len(orig.Suppressions))
	}
}
