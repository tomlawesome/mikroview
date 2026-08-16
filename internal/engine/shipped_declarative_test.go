// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// This file carries forward four of internal/detect/characterization_test.go's
// pins for port_scan, moved here by issue #405 once port_scan's own
// evaluation logic left internal/detect for this package's shipped
// declarative definition (see shipped_declarative.go's
// buildPortScanDefinition). Every pinned value (thresholds, Detail
// strings, Confidence numbers, Evidence contents) is unchanged from the
// internal/detect original -- only the construction/entry point differs,
// per this issue's own instruction to "adapt imports/construction
// minimally, preserving every pin."

// newShippedPortScanDefinition builds port_scan's live
// DeclarativeDefinition at DefaultConfig's real threshold/window
// (15/60s, see internal/detect.DefaultConfig), wired to raise into fs the
// same way main.go wires every detection-intent definition (FlagsSink).
func newShippedPortScanDefinition(t *testing.T, fs *flags.Store, scope Scope) *DeclarativeDefinition {
	t.Helper()
	def := Definition{
		ID:          "port_scan",
		Name:        "Port scan",
		Intent:      IntentDetection,
		Kind:        KindDeclarative,
		Enabled:     true,
		Scope:       scope,
		Params:      Params{"threshold": 15, "window": (60 * time.Second).String()},
		ParamSchema: PortScanParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	dd, err := BuildShippedDeclarativeDefinition(def)
	if err != nil {
		t.Fatalf("BuildShippedDeclarativeDefinition(port_scan): %v", err)
	}
	dd.OnRoutedEmission = FlagsSink(fs)
	return dd
}

func newTestFlagsStore(t *testing.T) *flags.Store {
	t.Helper()
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// psEvt/psEvtCountry mirror internal/detect/detect_test.go's evt/evtCountry
// helpers -- same fixed DstIP ("192.168.1.1"), same field shape.
func psEvt(srcIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: "192.168.1.1", DstPort: dstPort, ReceivedAt: at}
}

func psEvtCountry(srcIP, country string, dstPort int, at time.Time) store.Event {
	e := psEvt(srcIP, dstPort, at)
	e.SrcCountry = country
	return e
}

func psFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypePortScan {
			return &f
		}
	}
	return nil
}

// TestShippedPortScanFiresAtDefaultConfigScale is
// internal/detect/characterization_test.go's
// TestCharacterizationPortScanFiresAtDefaultConfigScale, moved: spreads
// events across the full 60s window (bucketSpanFor(60s) == 1s) so the
// shipped declarative definition exercises most of CountRing/DistinctRing's
// 60 buckets, the shape a real slow-burst scan produces.
func TestShippedPortScanFiresAtDefaultConfigScale(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{})

	now := time.Now()
	for port := 1; port < 15; port++ {
		dd.Evaluate(psEvt("203.0.113.9", port, now.Add(time.Duration(port)*3*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag below threshold, got %+v", fs.List())
	}

	dd.Evaluate(psEvt("203.0.113.9", 15, now.Add(15*3*time.Second)))
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypePortScan || list[0].Target != "203.0.113.9" {
		t.Fatalf("expected a port_scan flag at threshold, got %+v", list)
	}
}

// TestShippedPortScan_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationPortScan_FieldsRefireClearRevive, moved: pins
// port_scan's firing boundary, the exact shape of the flag it raises,
// and its re-fire/clear/revive behaviour across a second crossing --
// every value below is unchanged from the internal/detect original.
func TestShippedPortScan_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{})
	ip := "203.0.113.9"
	t0 := time.Now()

	// 14 distinct ports: must not fire.
	for port := 1; port <= 14; port++ {
		dd.Evaluate(psEvtCountry(ip, "DE", port, t0.Add(time.Duration(port-1)*time.Second)))
	}
	if got := psFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag at 14 distinct ports, got %+v", got)
	}

	// The 15th distinct port crosses the threshold.
	dd.Evaluate(psEvtCountry(ip, "DE", 15, t0.Add(14*time.Second)))
	f := psFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a port_scan flag at exactly 15 distinct ports")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	if want := "15 distinct destination ports in 1m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "DE" {
		t.Errorf("Country = %q, want DE", f.Country)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}
	wantPorts := make([]int, 15)
	for i := range wantPorts {
		wantPorts[i] = i + 1
	}
	if fmt.Sprint(f.Evidence.Ports) != fmt.Sprint(wantPorts) {
		t.Errorf("Evidence.Ports = %v, want %v", f.Evidence.Ports, wantPorts)
	}
	if f.Count != 1 {
		t.Errorf("Count = %d, want 1", f.Count)
	}

	// Re-fire: a 16th distinct port within the same window updates the
	// flag in place.
	dd.Evaluate(psEvtCountry(ip, "DE", 16, t0.Add(15*time.Second)))
	f2 := psFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 3 {
		t.Errorf("Confidence after re-fire = %v, want 3 (overshootConfidence(16,15))", f2.Confidence)
	}

	// Clear it, then feed a 17th distinct port still inside the same
	// window: the flag revives.
	if !fs.Clear(f2.ID, t0.Add(15500*time.Millisecond)) {
		t.Fatal("expected Clear to succeed on the active flag")
	}
	reviveAt := t0.Add(16 * time.Second)
	dd.Evaluate(psEvtCountry(ip, "DE", 17, reviveAt))
	f3 := psFlagOfType(fs)
	if f3 == nil {
		t.Fatal("expected the flag to revive")
	}
	if f3.Cleared {
		t.Error("expected the revived flag to no longer be Cleared")
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1 (revival resets Count)", f3.Count)
	}
	if !f3.FirstSeen.Equal(reviveAt) {
		t.Errorf("FirstSeen after revival = %v, want %v (revival resets FirstSeen)", f3.FirstSeen, reviveAt)
	}
	if want := "17 distinct destination ports in 1m0s"; f3.Detail != want {
		t.Errorf("Detail after revival = %q, want %q", f3.Detail, want)
	}
	if f3.Confidence == nil || *f3.Confidence != 7 {
		t.Errorf("Confidence after revival = %v, want 7 (overshootConfidence(17,15))", f3.Confidence)
	}
}

// TestShippedPortScanScope_HostsModeDeny is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_HostsModeDeny, moved: pins the HostsMode axis
// under ListModeDeny, at port_scan's real DefaultConfig scale.
func TestShippedPortScanScope_HostsModeDeny(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{Hosts: []string{"203.0.113.9"}, HostsMode: ListModeDeny})
	now := time.Now()

	for port := 1; port <= 20; port++ {
		dd.Evaluate(psEvt("203.0.113.9", port, now.Add(time.Duration(port)*time.Second))) // denied
	}
	if got := psFlagOfType(fs); got != nil {
		t.Fatalf("expected the denylisted host to never flag even at 20 distinct ports, got %+v", got)
	}

	for port := 1; port <= 15; port++ {
		dd.Evaluate(psEvt("203.0.113.10", port, now.Add(time.Duration(port)*time.Second))) // not denied
	}
	if got := psFlagOfType(fs); got == nil {
		t.Fatal("expected a non-denylisted host to still flag at threshold")
	}
}

// TestShippedPortScanScope_Classification is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_Classification, moved: pins the
// Classification axis, per settings.go's per-detector field usage table
// (PortScan's Hosts/HostsMode + Classification restrict which source IPs
// are tracked at all).
func TestShippedPortScanScope_Classification(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{Classification: store.ScopeInternal})
	now := time.Now()

	// An external source: never tracked at all under Classification=Internal,
	// no matter how many distinct ports it touches.
	for port := 1; port <= 20; port++ {
		dd.Evaluate(psEvt("203.0.113.9", port, now.Add(time.Duration(port)*time.Second)))
	}
	if got := psFlagOfType(fs); got != nil {
		t.Fatalf("expected an external source to never flag under Classification=Internal, got %+v", got)
	}

	// An internal (LAN) source still flags normally at threshold.
	for port := 1; port <= 15; port++ {
		dd.Evaluate(psEvt("192.168.1.77", port, now.Add(time.Duration(port)*time.Second)))
	}
	if got := psFlagOfType(fs); got == nil {
		t.Fatal("expected an internal source to still flag under Classification=Internal")
	}
}
