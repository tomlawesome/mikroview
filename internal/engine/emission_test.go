// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

// TestRenderEmissionFailsOnUnaccumulatedValue pins the structural rule
// this package's evidence design exists for: a Detail template naming a
// value its EvidenceSet never accumulated (here, {Hosts} -- AddHost is
// never called) must fail RenderEmission, not silently render an empty
// value. This is #379's wrong-naming class made a hard, testable
// failure instead of a code-review discipline.
func TestRenderEmissionFailsOnUnaccumulatedValue(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(22)
	evidence.AddPort(443)
	// Never AddHost -- this is the mistake: naming {Hosts} anyway.

	_, err := RenderEmission(evidence, 0, "{PortCount} ports; hosts seen: {Hosts}", false)
	if err == nil {
		t.Fatal("RenderEmission succeeded referencing {Hosts}, which nothing ever accumulated -- want a hard failure")
	}
}

// TestRenderEmissionFailsOnMisspelledField pins that this is real
// name-set enforcement, not string interpolation that happens to work
// for the fields a definition remembers to spell correctly -- a
// template referencing a name that was never even a category (a typo)
// fails the same way as naming a genuinely un-accumulated one.
func TestRenderEmissionFailsOnMisspelledField(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(22)

	_, err := RenderEmission(evidence, 0, "{PortCuont} ports", false)
	if err == nil {
		t.Fatal("RenderEmission succeeded on a misspelled field name, want a hard failure")
	}
}

func TestRenderEmissionSucceedsOnAccumulatedValues(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(22)
	evidence.AddPort(80)
	evidence.AddHost("203.0.113.5")

	em, err := RenderEmission(evidence, 0, "{PortCount} distinct ports, {HostCount} host(s)", false)
	if err != nil {
		t.Fatalf("RenderEmission: %v", err)
	}
	if em.Detail != "2 distinct ports, 1 host(s)" {
		t.Errorf("Detail = %q, want %q", em.Detail, "2 distinct ports, 1 host(s)")
	}
	if len(em.Ports) != 2 {
		t.Errorf("Emission.Ports = %v, want 2 entries", em.Ports)
	}
	if len(em.Hosts) != 1 {
		t.Errorf("Emission.Hosts = %v, want 1 entry", em.Hosts)
	}
	if em.Labels != nil {
		t.Errorf("Emission.Labels = %v, want nil -- Labels was never accumulated", em.Labels)
	}
}

func TestRenderEmissionInlinesTheActualPortsAndHostsList(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(443)
	evidence.AddPort(22)
	evidence.AddHost("203.0.113.5")

	em, err := RenderEmission(evidence, 0, "ports: {Ports}; hosts: {Hosts}", false)
	if err != nil {
		t.Fatalf("RenderEmission: %v", err)
	}
	if want := "ports: 22, 443; hosts: 203.0.113.5"; em.Detail != want {
		t.Errorf("Detail = %q, want %q", em.Detail, want)
	}
}

func TestRenderEmissionCarriesProvisionalThrough(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(22)

	em, err := RenderEmission(evidence, 0, "{PortCount} ports", true)
	if err != nil {
		t.Fatalf("RenderEmission: %v", err)
	}
	if !em.Provisional {
		t.Error("Emission.Provisional = false, want true")
	}
}

// TestRenderEmissionCountIsIndependentOfEvidenceCounts pins #379's
// critical_port fix: {Count} is the threshold-crossing tally the caller
// supplies, not derived from evidence at all -- it can legitimately
// differ from {PortCount} (e.g. 5 attempts against a set of 2 distinct
// critical ports), and {Count} needs no corresponding Add call, unlike
// Ports/Hosts/Labels.
func TestRenderEmissionCountIsIndependentOfEvidenceCounts(t *testing.T) {
	evidence := NewEvidenceSet()
	evidence.AddPort(22)
	evidence.AddPort(23)

	em, err := RenderEmission(evidence, 5, "{Count} attempts against critical ports {Ports}", false)
	if err != nil {
		t.Fatalf("RenderEmission: %v", err)
	}
	if want := "5 attempts against critical ports 22, 23"; em.Detail != want {
		t.Errorf("Detail = %q, want %q", em.Detail, want)
	}
}

func TestRenderEmissionWithNoTemplateReferencesSucceedsEvenWithNoEvidence(t *testing.T) {
	em, err := RenderEmission(NewEvidenceSet(), 0, "device went silent", false)
	if err != nil {
		t.Fatalf("RenderEmission: %v", err)
	}
	if em.Detail != "device went silent" {
		t.Errorf("Detail = %q, want the literal template text unchanged", em.Detail)
	}
}
