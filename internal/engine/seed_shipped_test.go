// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
)

// TestSeedShippedDefinitionsPopulatesAnUnpersistedStore is the
// regression this function exists for, found by #405's live-check run:
// with no definitions backend configured -- which nothing in the config
// file requires, and which the live-check environment genuinely has --
// the definitions store opens empty, MigrateDefinitions correctly no-ops
// (there is nothing to migrate into), and every detector already ported
// onto this chassis silently stops running. No error, no warning, just
// flags that never appear.
func TestSeedShippedDefinitionsPopulatesAnUnpersistedStore(t *testing.T) {
	s, err := OpenDefinitionsStoreWithBackend(nil) // no persistence at all
	if err != nil {
		t.Fatalf("OpenDefinitionsStoreWithBackend(nil): %v", err)
	}
	if got := len(s.List()); got != 0 {
		t.Fatalf("expected an unpersisted store to open empty, got %d", got)
	}

	if err := SeedShippedDefinitions(s, DefaultDetectorSettings(), DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}

	for _, name := range ShippedDefinitionIDs() {
		sd, ok := s.Get(name)
		if !ok {
			t.Errorf("%q missing after seeding", name)
			continue
		}
		if !sd.Available {
			t.Errorf("%q seeded unavailable", name)
		}
		if sd.Definition.Provenance.Origin != ProvenanceShipped {
			t.Errorf("%q seeded with provenance %q, want shipped", name, sd.Definition.Provenance.Origin)
		}
	}

	// Every declarative-kind one must actually build, since a definition
	// that exists but cannot be built is the same coverage hole wearing
	// a different symptom (main.go logs and skips it).
	built := 0
	for _, sd := range s.List() {
		if sd.Definition.Kind != KindDeclarative || sd.Definition.Provenance.Origin != ProvenanceShipped {
			continue
		}
		if _, err := BuildShippedDeclarativeDefinition(sd.Definition); err != nil {
			t.Errorf("%q: BuildShippedDeclarativeDefinition after seeding: %v", sd.Definition.ID, err)
			continue
		}
		built++
	}
	if built == 0 {
		t.Fatal("no shipped declarative definition was buildable after seeding -- the ported detectors would evaluate nothing")
	}
}

// TestSeedShippedDefinitionsNeverOverwrites pins the other half:
// seeding runs on every boot, so it must be incapable of undoing a
// migration's output or an operator's edit. It is incapable of it twice
// over -- it skips any id already present, and the store itself refuses
// a wholesale replace of a shipped definition (see Upsert's own refusal,
// which is Definition's "shipped definitions are never deleted, only
// disabled" invariant enforced at the store). The second is what makes
// the first a guarantee rather than a matter of care, so it is what this
// pins.
func TestSeedShippedDefinitionsNeverOverwrites(t *testing.T) {
	s, err := OpenDefinitionsStoreWithBackend(nil)
	if err != nil {
		t.Fatalf("OpenDefinitionsStoreWithBackend(nil): %v", err)
	}
	if err := SeedShippedDefinitions(s, DefaultDetectorSettings(), DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	first, ok := s.Get("port_scan")
	if !ok {
		t.Fatal("port_scan missing after the first seed")
	}

	// A second seed against settings that would produce a *different*
	// definition must change nothing.
	settings := DefaultDetectorSettings()
	settings["port_scan"] = DetectorSettings{Enabled: false}
	if err := SeedShippedDefinitions(s, settings, DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions (second run): %v", err)
	}
	after, ok := s.Get("port_scan")
	if !ok {
		t.Fatal("port_scan missing after the second seed")
	}
	if after.Definition.Enabled != first.Definition.Enabled {
		t.Errorf("a second seed changed Enabled: %v -> %v", first.Definition.Enabled, after.Definition.Enabled)
	}
	if !paramValueEqual(after.Definition.Params["threshold"], first.Definition.Params["threshold"]) {
		t.Errorf("a second seed changed threshold: %v -> %v", first.Definition.Params["threshold"], after.Definition.Params["threshold"])
	}
}

// TestSeedShippedDefinitionsCarriesLiveDetectorSettings pins that a
// detector switched off before the port stays off after it -- the
// enabled/scope an operator set on internal/detect's settings store is
// what seeds the definition, not an unconditional "enabled".
func TestSeedShippedDefinitionsCarriesLiveDetectorSettings(t *testing.T) {
	s, err := OpenDefinitionsStoreWithBackend(nil)
	if err != nil {
		t.Fatalf("OpenDefinitionsStoreWithBackend(nil): %v", err)
	}
	settings := DefaultDetectorSettings()
	settings["port_scan"] = DetectorSettings{
		Enabled: false,
		Scope:   Scope{Hosts: []string{"198.51.100.4"}, HostsMode: ListModeDeny},
	}
	if err := SeedShippedDefinitions(s, settings, DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}

	sd, ok := s.Get("port_scan")
	if !ok {
		t.Fatal("port_scan missing after seeding")
	}
	if sd.Definition.Enabled {
		t.Error("port_scan seeded enabled, but this deployment had it switched off")
	}
	if len(sd.Definition.Scope.Hosts) != 1 || sd.Definition.Scope.HostsMode != ListModeDeny {
		t.Errorf("scope did not carry over: %+v", sd.Definition.Scope)
	}
}
