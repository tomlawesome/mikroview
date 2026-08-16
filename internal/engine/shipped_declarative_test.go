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
	// Ports and nothing else: internal/detect's port_scan raised
	// flags.Evidence{Ports: ...}, never Hosts. Pinned explicitly because
	// the first cut of this port did silently start carrying the
	// destination addresses as well -- see DeclarativeSpec.Evidence, which
	// is what makes the category a declaration rather than a side effect.
	if len(f.Evidence.Hosts) != 0 {
		t.Errorf("Evidence.Hosts = %v, want empty (port_scan never carried hosts)", f.Evidence.Hosts)
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

// ---------------------------------------------------------------------------
// critical_port (issue #405)
// ---------------------------------------------------------------------------

// newShippedCriticalPortDefinition builds critical_port's live
// DeclarativeDefinition at DefaultConfig's real ports/threshold/window
// (21,22,23,445,3389,5900,8291,8728,8729 / 5 / 5m -- see
// internal/detect.DefaultConfig), wired to raise into fs the same way
// main.go wires every detection-intent definition.
func newShippedCriticalPortDefinition(t *testing.T, fs *flags.Store, ports []int, threshold int, window time.Duration, scope Scope) *DeclarativeDefinition {
	t.Helper()
	def := Definition{
		ID:          "critical_port",
		Name:        "Critical port",
		Intent:      IntentDetection,
		Kind:        KindDeclarative,
		Enabled:     true,
		Scope:       scope,
		Params:      Params{"ports": ports, "threshold": threshold, "window": window.String()},
		ParamSchema: CriticalPortParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	dd, err := BuildShippedDeclarativeDefinition(def)
	if err != nil {
		t.Fatalf("BuildShippedDeclarativeDefinition(critical_port): %v", err)
	}
	dd.OnRoutedEmission = FlagsSink(fs)
	return dd
}

// defaultCriticalPorts mirrors internal/detect.DefaultConfig()'s
// CriticalPorts, restated here rather than imported so this package's
// tests do not depend on internal/detect at all (that dependency exists
// only for the one-way migration, definitions_migrate.go, and is due to
// disappear with the rest of that package).
var defaultCriticalPorts = []int{21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729}

func cpFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeCriticalPort {
			return &f
		}
	}
	return nil
}

// TestShippedCriticalPort_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationCriticalPort_FieldsRefireClearRevive, moved: pins
// critical_port's boundary at DefaultConfig's real 5-attempts/5-minute
// threshold against port 22.
//
// One pinned value changes here, deliberately, and it is #379's
// critical_port item: the Detail sentence used to name e.DstPort -- the
// single triggering event's port -- for a count aggregated across every
// critical port, and left flags.Evidence empty. It now names the
// accumulated port set and populates Evidence.Ports. Per-source
// aggregation across the critical-port set is unchanged (settings.go:99-102
// records it as deliberate); only the sentence and the Evidence changed.
// Every other value below -- the boundary, Target, Country, Confidence,
// Count, and the whole re-fire/clear/revive sequence -- is unchanged from
// the internal/detect original.
func TestShippedCriticalPort_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, defaultCriticalPorts, 5, 5*time.Minute, Scope{})
	ip := "198.51.100.4"
	t0 := time.Now()

	for i := 0; i < 4; i++ {
		dd.Evaluate(psEvtCountry(ip, "RU", 22, t0.Add(time.Duration(i)*30*time.Second)))
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag at 4 attempts, got %+v", got)
	}

	dd.Evaluate(psEvtCountry(ip, "RU", 22, t0.Add(4*30*time.Second)))
	f := cpFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag at exactly 5 attempts")
	}
	if f.Target != ip {
		t.Errorf("Target = %q, want %q", f.Target, ip)
	}
	// #379: was "5 attempts against port 22 in 5m0s".
	if want := "5 attempts against critical ports 22 in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "RU" {
		t.Errorf("Country = %q, want RU", f.Country)
	}
	// #379: was the zero Evidence value.
	if fmt.Sprint(f.Evidence.Ports) != fmt.Sprint([]int{22}) {
		t.Errorf("Evidence.Ports = %v, want [22]", f.Evidence.Ports)
	}
	if len(f.Evidence.Hosts) != 0 {
		t.Errorf("Evidence.Hosts = %v, want empty (critical_port is about ports, not destinations)", f.Evidence.Hosts)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 (exactly at threshold)", f.Confidence)
	}

	// Re-fire.
	dd.Evaluate(psEvtCountry(ip, "RU", 22, t0.Add(5*30*time.Second)))
	f2 := cpFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2 after a re-fire, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 10 {
		t.Errorf("Confidence after re-fire = %v, want 10 (overshootConfidence(6,5))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(6*30*time.Second)) {
		t.Fatal("expected Clear to succeed")
	}
	reviveAt := t0.Add(7 * 30 * time.Second)
	dd.Evaluate(psEvtCountry(ip, "RU", 22, reviveAt))
	f3 := cpFlagOfType(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if !f3.FirstSeen.Equal(reviveAt) {
		t.Errorf("FirstSeen after revival = %v, want %v", f3.FirstSeen, reviveAt)
	}
	if want := "7 attempts against critical ports 22 in 5m0s"; f3.Detail != want {
		t.Errorf("Detail after revival = %q, want %q", f3.Detail, want)
	}
	if f3.Confidence == nil || *f3.Confidence != 20 {
		t.Errorf("Confidence after revival = %v, want 20 (overshootConfidence(7,5))", f3.Confidence)
	}
}

// TestShippedCriticalPort_DetailNamesTheSetOfPortsTouched replaces
// internal/detect/characterization_test.go's
// TestCharacterizationCriticalPort_DetailNamesOnlyTheLastPort -- the pin
// #397 wrote for #379's known-wrong behaviour, updated here by the fix
// that closes it, in the same commit, which is the whole reason it was
// pinned rather than omitted.
//
// The old pin: 3 attempts against port 22 then 2 against port 23 from one
// source produced "5 attempts against port 23 in 5m0s" -- naming only the
// last event's port for a count spanning both. Today it names the set.
func TestShippedCriticalPort_DetailNamesTheSetOfPortsTouched(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22, 23}, 5, 5*time.Minute, Scope{})
	ip := "198.51.100.7"
	t0 := time.Now()

	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt(ip, 22, t0.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 2; i++ {
		dd.Evaluate(psEvt(ip, 23, t0.Add(time.Duration(3+i)*time.Second)))
	}

	f := cpFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag once the combined count across both ports reaches the threshold")
	}
	if want := "5 attempts against critical ports 22, 23 in 5m0s"; f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if fmt.Sprint(f.Evidence.Ports) != fmt.Sprint([]int{22, 23}) {
		t.Errorf("Evidence.Ports = %v, want [22 23]", f.Evidence.Ports)
	}
}

// TestShippedCriticalPortFlagsOnlyForExternalSources is
// internal/detect/detect_test.go's TestCriticalPortFlagsOnlyForExternalSources,
// moved: internal/detect gated this behind isPublic(e.SrcIP) in Observe;
// the definition expresses the same gate as a sourceAddress
// matchesClassification "external" condition.
func TestShippedCriticalPortFlagsOnlyForExternalSources(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 3, time.Minute, Scope{})
	now := time.Now()

	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("192.168.1.50", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag for a private-source critical-port attempt, got %+v", fs.List())
	}

	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Type != flags.TypeCriticalPort || list[0].Target != "198.51.100.4" {
		t.Fatalf("expected a critical_port flag for the external source only, got %+v", list)
	}
}

// TestShippedCriticalPortIgnoresNonCriticalPorts is
// internal/detect/detect_test.go's TestCriticalPortIgnoresNonCriticalPorts,
// moved: the curated port list is now a destinationPort inSet condition.
func TestShippedCriticalPortIgnoresNonCriticalPorts(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 1, 5*time.Minute, Scope{})

	dd.Evaluate(psEvt("198.51.100.4", 80, time.Now()))
	if len(fs.List()) != 0 {
		t.Fatalf("expected no flag for a non-critical port, got %+v", fs.List())
	}
}

// TestShippedCriticalPortConfidenceScalesWithOvershoot is
// internal/detect/detect_test.go's
// TestCriticalPortConfidenceScalesWithOvershoot, moved unchanged.
func TestShippedCriticalPortConfidenceScalesWithOvershoot(t *testing.T) {
	now := time.Now()

	fs := newTestFlagsStore(t)
	justOver := newShippedCriticalPortDefinition(t, fs, []int{22}, 5, time.Minute, Scope{})
	for i := 0; i < 5; i++ {
		justOver.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	list := fs.List()
	if len(list) != 1 || list[0].Confidence == nil || *list[0].Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", list)
	}

	fs2 := newTestFlagsStore(t)
	wellOver := newShippedCriticalPortDefinition(t, fs2, []int{22}, 5, time.Minute, Scope{})
	for i := 0; i < 15; i++ {
		wellOver.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	list2 := fs2.List()
	if len(list2) != 1 || list2[0].Confidence == nil || *list2[0].Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", list2)
	}
}

// TestShippedCriticalPortReFiringUpdatesExistingFlagInPlace is
// internal/detect/detect_test.go's TestReFiringUpdatesExistingFlagInPlace,
// moved -- it used critical_port only as a convenient re-firing detector.
func TestShippedCriticalPortReFiringUpdatesExistingFlagInPlace(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 3, time.Hour, Scope{})

	now := time.Now()
	dd.Evaluate(psEvt("203.0.113.9", 22, now))
	dd.Evaluate(psEvt("203.0.113.9", 22, now))
	dd.Evaluate(psEvt("203.0.113.9", 22, now)) // crosses the threshold
	dd.Evaluate(psEvt("203.0.113.9", 22, now.Add(time.Second)))

	list := fs.List()
	if len(list) != 1 {
		t.Fatalf("expected re-firing to update one flag in place, not create another, got %d: %+v", len(list), list)
	}
	if list[0].Count < 2 {
		t.Errorf("expected Count to reflect multiple firings, got %d", list[0].Count)
	}
}

// TestShippedCriticalPortCarriesCountry is
// internal/detect/detect_test.go's TestCriticalPortCarriesCountry, moved.
func TestShippedCriticalPortCarriesCountry(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 1, 5*time.Minute, Scope{})

	dd.Evaluate(psEvtCountry("198.51.100.4", "RU", 22, time.Now()))

	list := fs.List()
	if len(list) != 1 || list[0].Country != "RU" {
		t.Fatalf("expected Country to be threaded through, got %+v", list)
	}
}

// TestShippedCriticalPortIgnoresEstablishedTraffic pins the
// connectionState condition -- internal/detect's isTrackableConnState
// filter, which observeCriticalPort applied before touching its window.
func TestShippedCriticalPortIgnoresEstablishedTraffic(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 3, time.Minute, Scope{})
	now := time.Now()

	for i := 0; i < 10; i++ {
		e := psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second))
		e.ConnState = "established"
		dd.Evaluate(e)
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected established traffic never to reach the critical-port window, got %+v", got)
	}
}

// TestShippedCriticalPortScope_HostsAllow is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_HostsAllow, moved: pins the Hosts axis under
// ListModeAllow at critical_port's real DefaultConfig scale.
func TestShippedCriticalPortScope_HostsAllow(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, defaultCriticalPorts, 5, 5*time.Minute,
		Scope{Hosts: []string{"198.51.100.4"}, HostsMode: ListModeAllow})
	now := time.Now()

	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second))) // not on the allow list
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected a host outside the allow list to never flag, got %+v", got)
	}

	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*30*time.Second))) // allow-listed
	}
	if got := cpFlagOfType(fs); got == nil {
		t.Fatal("expected the allow-listed host to still flag")
	}
}

// TestShippedCriticalPortScope_PortsModeDeny is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_PortsModeDeny, moved: Scope.Ports restricts
// the *effective* subset of the definition's own critical-port list (see
// Scope's doc comment), layered on top of the destinationPort inSet
// condition rather than replacing it.
func TestShippedCriticalPortScope_PortsModeDeny(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, defaultCriticalPorts, 5, 5*time.Minute,
		Scope{Ports: []int{23}, PortsMode: ListModeDeny})
	now := time.Now()

	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 23, now.Add(time.Duration(i)*30*time.Second))) // denied port
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected the denylisted port to never count toward the threshold, got %+v", got)
	}

	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second))) // not denied
	}
	if got := cpFlagOfType(fs); got == nil {
		t.Fatal("expected a non-denylisted critical port to still flag at threshold")
	}
}

// TestShippedCriticalPortScope_AxesCombineWithAND is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_AxesCombineWithAND, moved: #44's model --
// multiple active Scope axes combine with AND, not OR.
func TestShippedCriticalPortScope_AxesCombineWithAND(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, defaultCriticalPorts, 5, 5*time.Minute, Scope{
		Hosts: []string{"198.51.100.4"}, HostsMode: ListModeAllow,
		Ports: []int{22}, PortsMode: ListModeAllow,
	})
	now := time.Now()

	// Matches the host axis but not the ports axis -- port 21 is a real
	// critical port, but not in the ports allow-list.
	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 21, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected host-only match (wrong port) to never flag, got %+v", got)
	}

	// Matches the ports axis but not the hosts axis.
	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.5", 22, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected port-only match (wrong host) to never flag, got %+v", got)
	}

	// Matches both axes.
	for i := 0; i < 5; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*30*time.Second)))
	}
	if got := cpFlagOfType(fs); got == nil {
		t.Fatal("expected a match on both axes together to flag")
	}
}

// TestShippedCriticalPortDisabledIsInert pins the enable/disable gate for
// the ported definition -- internal/detect read this off its
// SettingsStore on every event; a Definition carries it on the envelope
// (Definition.Enabled), and Evaluate returns immediately for a disabled
// one, touching no state.
func TestShippedCriticalPortDisabledIsInert(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, defaultCriticalPorts, 5, 5*time.Minute, Scope{})
	dd.def.Enabled = false
	now := time.Now()

	for i := 0; i < 20; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if got := cpFlagOfType(fs); got != nil {
		t.Fatalf("expected a disabled definition to never flag, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// repeated_drops (issue #405)
// ---------------------------------------------------------------------------

// newShippedRepeatedDropsDefinition builds repeated_drops' live
// DeclarativeDefinition -- DefaultConfig's real 10-attempts/15-minute
// threshold unless a test deliberately shrinks it (see
// internal/detect.DefaultConfig).
func newShippedRepeatedDropsDefinition(t *testing.T, fs *flags.Store, threshold int, window time.Duration, scope Scope) *DeclarativeDefinition {
	t.Helper()
	def := Definition{
		ID:          "repeated_drops",
		Name:        "Repeated drops",
		Intent:      IntentDetection,
		Kind:        KindDeclarative,
		Enabled:     true,
		Scope:       scope,
		Params:      Params{"threshold": threshold, "window": window.String()},
		ParamSchema: RepeatedDropsParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	dd, err := BuildShippedDeclarativeDefinition(def)
	if err != nil {
		t.Fatalf("BuildShippedDeclarativeDefinition(repeated_drops): %v", err)
	}
	dd.OnRoutedEmission = FlagsSink(fs)
	return dd
}

// rdEvt is a refused attempt against a locally-hosted service -- the
// event shape repeated_drops' conditions select for.
func rdEvt(srcIP, dstIP string, dstPort int, at time.Time) store.Event {
	return store.Event{SrcIP: srcIP, DstIP: dstIP, DstPort: dstPort, Action: store.ActionDrop, ReceivedAt: at}
}

func rdFlagOfType(fs *flags.Store) *flags.Flag {
	for _, f := range fs.List() {
		f := f
		if f.Type == flags.TypeRepeatedDrops {
			return &f
		}
	}
	return nil
}

// TestShippedRepeatedDrops_FieldsRefireClearRevive is
// internal/detect/characterization_test.go's
// TestCharacterizationRepeatedDrops_FieldsRefireClearRevive, moved.
//
// One pinned value changes, deliberately: #379's repeated_drops item.
// The Detail string named the single triggering event's destination
// address for a count keyed only on (source, destination port) -- the
// sentence claimed single-destination attribution the aggregation never
// had. It now names only the destination port, which is a key component
// and therefore true of every counted attempt, and the distinct
// destination set moved into Evidence.Hosts. Target, Country,
// Confidence, Count, the firing boundary and the whole
// re-fire/clear/revive sequence are unchanged.
func TestShippedRepeatedDrops_FieldsRefireClearRevive(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 10, 15*time.Minute, Scope{})
	src, dstIP, dstPort := "203.0.113.9", "192.168.1.1", 8080
	t0 := time.Now()

	dropEvt := func(at time.Time) store.Event {
		e := rdEvt(src, dstIP, dstPort, at)
		e.SrcCountry = "NL"
		return e
	}

	for i := 0; i < 9; i++ {
		dd.Evaluate(dropEvt(t0.Add(time.Duration(i) * time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected no flag at 9 attempts, got %+v", got)
	}

	dd.Evaluate(dropEvt(t0.Add(9 * time.Minute)))
	f := rdFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag at exactly 10 attempts")
	}
	if want := "203.0.113.9 -> port 8080"; f.Target != want {
		t.Errorf("Target = %q, want %q", f.Target, want)
	}
	// #379: was "10 attempts against 192.168.1.1:8080 dropped in 15m0s -- ...".
	want := "10 attempts against port 8080 dropped in 15m0s -- check whether this port is meant to be open"
	if f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if f.Country != "NL" {
		t.Errorf("Country = %q, want NL", f.Country)
	}
	// #379: the distinct destination set, which used to be nowhere.
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint([]string{dstIP}) {
		t.Errorf("Evidence.Hosts = %v, want [%s]", f.Evidence.Hosts, dstIP)
	}
	if f.Evidence.NAT != nil {
		t.Errorf("Evidence.NAT = %+v, want nil (no NAT fields set on the triggering event)", f.Evidence.NAT)
	}
	if f.Confidence == nil || *f.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0", f.Confidence)
	}

	// Re-fire.
	dd.Evaluate(dropEvt(t0.Add(10 * time.Minute)))
	f2 := rdFlagOfType(fs)
	if f2 == nil || f2.Count != 2 {
		t.Fatalf("expected Count=2, got %+v", f2)
	}
	if f2.Confidence == nil || *f2.Confidence != 5 {
		t.Errorf("Confidence after re-fire = %v, want 5 (overshootConfidence(11,10))", f2.Confidence)
	}

	// Clear + revive.
	if !fs.Clear(f2.ID, t0.Add(11*time.Minute)) {
		t.Fatal("expected Clear to succeed")
	}
	dd.Evaluate(dropEvt(t0.Add(12 * time.Minute)))
	f3 := rdFlagOfType(fs)
	if f3 == nil || f3.Cleared {
		t.Fatalf("expected the flag to revive as active, got %+v", f3)
	}
	if f3.Count != 1 {
		t.Errorf("Count after revival = %d, want 1", f3.Count)
	}
	if f3.Confidence == nil || *f3.Confidence != 10 {
		t.Errorf("Confidence after revival = %v, want 10 (overshootConfidence(12,10))", f3.Confidence)
	}
}

// TestShippedRepeatedDrops_DetailCarriesTheDestinationSetAsEvidence
// replaces internal/detect/characterization_test.go's
// TestCharacterizationRepeatedDrops_DetailNamesOnlyTheLastDestination --
// #397's pin of #379's known-wrong behaviour, updated here by the fix
// that closes it, in the same commit.
//
// The old pin: four refused attempts from one source against port 8080,
// spread across four different internal destinations, produced
// "4 attempts against 192.168.1.4:8080 dropped in 15m0s -- ...", naming
// the last destination for a count three-quarters of which went
// elsewhere. Today the sentence names only the port, and the four
// destinations are in Evidence.Hosts.
func TestShippedRepeatedDrops_DetailCarriesTheDestinationSetAsEvidence(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 4, 15*time.Minute, Scope{})
	t0 := time.Now()

	dests := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4"}
	for i, dst := range dests {
		dd.Evaluate(rdEvt("203.0.113.9", dst, 8080, t0.Add(time.Duration(i)*time.Minute)))
	}

	f := rdFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag once the combined count across all four destinations reaches the threshold")
	}
	want := "4 attempts against port 8080 dropped in 15m0s -- check whether this port is meant to be open"
	if f.Detail != want {
		t.Errorf("Detail = %q, want %q", f.Detail, want)
	}
	if fmt.Sprint(f.Evidence.Hosts) != fmt.Sprint(dests) {
		t.Errorf("Evidence.Hosts = %v, want %v", f.Evidence.Hosts, dests)
	}
}

// TestShippedRepeatedDrops_EvidenceCapturesNAT is
// internal/detect/characterization_test.go's
// TestCharacterizationRepeatedDrops_EvidenceCapturesNAT, moved unchanged:
// Evidence.NAT carries the triggering event's translation detail, and
// only the triggering event's -- see EvidenceSet.SetNAT for why that one
// category is last-writer-wins.
func TestShippedRepeatedDrops_EvidenceCapturesNAT(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 2, 15*time.Minute, Scope{})
	t0 := time.Now()

	dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, t0))
	nat := rdEvt("203.0.113.9", "192.168.1.1", 8080, t0.Add(time.Minute))
	nat.NatIP, nat.NatPort, nat.NatRaw = "10.0.0.5", 51820, "dst-nat(10.0.0.5:51820)"
	dd.Evaluate(nat)

	f := rdFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a flag")
	}
	if f.Evidence.NAT == nil {
		t.Fatal("expected Evidence.NAT to be set")
	}
	if f.Evidence.NAT.IP != "10.0.0.5" || f.Evidence.NAT.Port != 51820 || f.Evidence.NAT.Raw != "dst-nat(10.0.0.5:51820)" {
		t.Errorf("Evidence.NAT = %+v, want {IP:10.0.0.5 Port:51820 Raw:dst-nat(10.0.0.5:51820)}", f.Evidence.NAT)
	}
}

// TestShippedRepeatedDropsIgnoresAcceptedTraffic is
// internal/detect/repeated_drops_test.go's test of the same name: the
// action condition admits only drop/reject.
func TestShippedRepeatedDropsIgnoresAcceptedTraffic(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 3, 15*time.Minute, Scope{})
	t0 := time.Now()

	for i := 0; i < 10; i++ {
		e := rdEvt("203.0.113.9", "192.168.1.1", 8080, t0.Add(time.Duration(i)*time.Minute))
		e.Action = store.ActionAccept
		dd.Evaluate(e)
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected accepted traffic never to count, got %+v", got)
	}
}

// TestShippedRepeatedDropsIgnoresExternalDestinations is
// internal/detect/repeated_drops_test.go's test of the same name: the
// destinationAddress classification condition admits only a
// locally-hosted service.
func TestShippedRepeatedDropsIgnoresExternalDestinations(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 3, 15*time.Minute, Scope{})
	t0 := time.Now()

	for i := 0; i < 10; i++ {
		dd.Evaluate(rdEvt("192.168.1.50", "203.0.113.20", 8080, t0.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected an external destination never to count, got %+v", got)
	}
}

// TestShippedRepeatedDropsTracksEachPortIndependently is
// internal/detect/repeated_drops_test.go's test of the same name: the
// key is (source, destination port), so two ports never pool their
// counts.
func TestShippedRepeatedDropsTracksEachPortIndependently(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 4, 15*time.Minute, Scope{})
	t0 := time.Now()

	for i := 0; i < 3; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, t0.Add(time.Duration(i)*time.Minute)))
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 9090, t0.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected 3 attempts per port to stay below a threshold of 4, got %+v", got)
	}

	dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, t0.Add(4*time.Minute)))
	f := rdFlagOfType(fs)
	if f == nil {
		t.Fatal("expected the 4th attempt against one port to fire")
	}
	if want := "203.0.113.9 -> port 8080"; f.Target != want {
		t.Errorf("Target = %q, want %q", f.Target, want)
	}
}

// TestShippedRepeatedDropsIgnoresPortlessEvents pins what
// internal/detect enforced with an explicit e.DstPort != 0 guard: an
// event with no destination port is not a (source, port) pair worth
// tracking. Here it is implied rather than written -- a zero port would
// key every such event into one window and render "port 0" -- so the
// guarantee is pinned rather than assumed.
func TestShippedRepeatedDropsIgnoresPortlessEvents(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 3, 15*time.Minute,
		Scope{Ports: []int{8080}, PortsMode: ListModeAllow})
	t0 := time.Now()

	for i := 0; i < 10; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 0, t0.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected a port-less event never to reach a port-scoped window, got %+v", got)
	}
}

// TestShippedRepeatedDropsScope_PortsAllow is
// internal/detect/characterization_test.go's
// TestCharacterizationScope_PortsAllow, moved: pins the Ports axis under
// ListModeAllow at repeated_drops' real DefaultConfig scale (10/15m).
func TestShippedRepeatedDropsScope_PortsAllow(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 10, 15*time.Minute,
		Scope{Ports: []int{8080}, PortsMode: ListModeAllow})
	now := time.Now()

	for i := 0; i < 10; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 9090, now.Add(time.Duration(i)*time.Minute))) // not on the allow list
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected a non-allowed port to never flag even at 10 attempts, got %+v", got)
	}

	for i := 0; i < 10; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, now.Add(time.Duration(i)*time.Minute))) // allow-listed
	}
	if got := rdFlagOfType(fs); got == nil {
		t.Fatal("expected the allow-listed port to still flag at threshold")
	}
}

// TestShippedRepeatedDropsHostsAndPortsScopeCombineWithAND is
// internal/detect/repeated_drops_test.go's test of the same name.
func TestShippedRepeatedDropsHostsAndPortsScopeCombineWithAND(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedRepeatedDropsDefinition(t, fs, 3, 15*time.Minute, Scope{
		Hosts: []string{"203.0.113.9"}, HostsMode: ListModeAllow,
		Ports: []int{8080}, PortsMode: ListModeAllow,
	})
	now := time.Now()

	// Right host, wrong port.
	for i := 0; i < 5; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 9090, now.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected host-only match (wrong port) to never flag, got %+v", got)
	}

	// Wrong host, right port.
	for i := 0; i < 5; i++ {
		dd.Evaluate(rdEvt("203.0.113.10", "192.168.1.1", 8080, now.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got != nil {
		t.Fatalf("expected port-only match (wrong host) to never flag, got %+v", got)
	}

	// Both.
	for i := 0; i < 3; i++ {
		dd.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, now.Add(time.Duration(i)*time.Minute)))
	}
	if got := rdFlagOfType(fs); got == nil {
		t.Fatal("expected a match on both axes together to flag")
	}
}

// TestShippedRepeatedDropsConfidenceScalesWithOvershoot is
// internal/detect/repeated_drops_test.go's test of the same name.
func TestShippedRepeatedDropsConfidenceScalesWithOvershoot(t *testing.T) {
	now := time.Now()

	fs := newTestFlagsStore(t)
	justOver := newShippedRepeatedDropsDefinition(t, fs, 5, 15*time.Minute, Scope{})
	for i := 0; i < 5; i++ {
		justOver.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, now.Add(time.Duration(i)*time.Minute)))
	}
	if f := rdFlagOfType(fs); f == nil || f.Confidence == nil || *f.Confidence != 0 {
		t.Fatalf("expected 0%% confidence exactly at threshold, got %+v", f)
	}

	fs2 := newTestFlagsStore(t)
	wellOver := newShippedRepeatedDropsDefinition(t, fs2, 5, 15*time.Minute, Scope{})
	for i := 0; i < 15; i++ {
		wellOver.Evaluate(rdEvt("203.0.113.9", "192.168.1.1", 8080, now.Add(time.Duration(i)*30*time.Second)))
	}
	if f := rdFlagOfType(fs2); f == nil || f.Confidence == nil || *f.Confidence != 100 {
		t.Fatalf("expected 100%% confidence at the overshoot ceiling, got %+v", f)
	}
}
