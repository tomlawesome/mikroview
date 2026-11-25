// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/dossier"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/hosts"
	"github.com/tomlawesome/mikroview/internal/store"
)

// installHomeScope is what main calls at boot: engine, hosts and dossier
// must each see a networks.home address as home.
func TestInstallHomeScopeReachesNewDefinitions(t *testing.T) {
	t.Cleanup(func() {
		engine.SetHomeScope(nil)
		hosts.SetHomeScope(nil)
		dossier.SetHomeScope(nil)
	})
	if _, err := installHomeScope([]string{"not-a-cidr"}); err == nil {
		t.Fatal("a bad networks.home entry must be refused")
	}
	if _, err := installHomeScope([]string{"198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	def := engine.NewDefinition("home-wiring", engine.IntentDetection, engine.KindDeclarative)
	def.Enabled = true
	def.Provenance = engine.Provenance{Origin: engine.ProvenanceCustom}
	dd, err := engine.NewDeclarativeDefinition(def, engine.DeclarativeSpec{
		Conditions: []engine.Condition{{Field: engine.FieldSourceAddress, Operator: engine.OpMatchesClassification, Values: []string{"internal"}}},
		Key:        engine.KeyGlobal, Window: 60e9, Threshold: 1, CountingMode: engine.CountingTotal,
		DetailTemplate: "{PortCount} hits",
		Evidence:       []engine.EvidenceField{engine.EvidencePorts},
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := engine.BuildDispatchIndex([]*engine.DeclarativeDefinition{dd})
	if got := idx.Candidates(store.Event{SrcIP: "198.51.100.66"}); len(got) != 1 {
		t.Errorf("home address: %d candidates, want 1", len(got))
	}
	if got := idx.Candidates(store.Event{SrcIP: "203.0.113.44"}); len(got) != 0 {
		t.Errorf("outside address: %d candidates, want 0", len(got))
	}
	if !hosts.Registers("vlan10", "198.51.100.66") {
		t.Error("hosts: a networks.home address must register as a host")
	}
	if hosts.Registers("ether1", "203.0.113.44") {
		t.Error("hosts: an outside address must stay public")
	}
	peerScope := func(ip string) string {
		d := dossier.Assemble(dossier.Input{
			IP:     "192.0.2.5",
			Events: []store.Event{{SrcIP: "192.0.2.5", DstIP: ip}},
		})
		if len(d.Traffic.Destinations) != 1 {
			t.Fatalf("dossier: %d destinations for %s, want 1", len(d.Traffic.Destinations), ip)
		}
		return d.Traffic.Destinations[0].Scope
	}
	if got := peerScope("198.51.100.66"); got != "local" {
		t.Errorf("dossier: home peer scope = %q, want local", got)
	}
	if got := peerScope("203.0.113.44"); got != "internet" {
		t.Errorf("dossier: outside peer scope = %q, want internet", got)
	}
}
