// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// This file covers #640's engine half: every shipped definition declares
// what its size is -- or explicitly declares none -- and a definition
// that declares one carries the observed value all the way to the flags
// store, which is what decides whether an expectation absorbs the
// firing.

// TestShippedSizeMeasureCoversEveryShippedDefinition is #640's "every
// shipped detector declares its size, or declares none" made
// mechanical, in the same spirit as
// TestShippedParamSchemaCoversEveryConfigField: it reflects over the
// real shipped catalogue rather than a list written alongside the
// declarations, so adding a definition without deciding its size fails
// here instead of silently defaulting to "none".
func TestShippedSizeMeasureCoversEveryShippedDefinition(t *testing.T) {
	for _, sd := range shippedDetectors {
		m, ok := ShippedSizeMeasure(sd.id)
		if !ok {
			t.Errorf("shipped definition %q has no size declaration -- every shipped definition must declare its size or explicitly declare none (SizeNone), see #640", sd.id)
			continue
		}
		// A declaration always says something, whether or not it names a
		// unit: a blank one is indistinguishable from a forgotten entry,
		// and the ledger has nothing to show for it.
		if m.Description == "" {
			t.Errorf("shipped definition %q declares a size with no description -- say what is counted, or why nothing is", sd.id)
		}
	}

	// And nothing declares a size for an id the catalogue does not ship:
	// a stale entry would quietly describe a definition that no longer
	// exists.
	shipped := make(map[string]bool, len(shippedDetectors))
	for _, sd := range shippedDetectors {
		shipped[sd.id] = true
	}
	for id := range shippedSizeMeasures {
		if !shipped[id] {
			t.Errorf("size declaration for %q, which is not in the shipped catalogue", id)
		}
	}
}

// TestShippedSizeMeasureNamesTheThresholdUnit checks the declarations
// that do name a size against the param schema they claim to measure
// against, so the two cannot drift apart: port_scan's size is "distinct
// ports" precisely because that is what its threshold param counts.
//
// Only the definitions whose size really is a threshold param's own unit
// are listed -- low_slow_scan measures against portThreshold, whose
// schema entry carries the same unit, while off_hours_activity's minCount
// is expressed in events. A definition declaring none is not in the map
// and is covered by the exhaustiveness test above.
func TestShippedSizeMeasureNamesTheThresholdUnit(t *testing.T) {
	cases := map[string]struct {
		schema    []ParamSchema
		paramName string
	}{
		string(flags.TypePortScan):              {PortScanParamSchema, "threshold"},
		string(flags.TypeActivitySpike):         {ActivitySpikeParamSchema, "threshold"},
		string(flags.TypeCriticalPort):          {CriticalPortParamSchema, "threshold"},
		string(flags.TypeDistributedBruteForce): {DistributedBruteForceParamSchema, "threshold"},
		string(flags.TypeOutboundAnomaly):       {OutboundAnomalyParamSchema, "threshold"},
		string(flags.TypeInternalRecon):         {InternalReconParamSchema, "threshold"},
		string(flags.TypeRepeatedDrops):         {RepeatedDropsParamSchema, "threshold"},
		string(flags.TypeLowSlowScan):           {LowSlowScanParamSchema, "portThreshold"},
	}
	for id, c := range cases {
		m, ok := ShippedSizeMeasure(id)
		if !ok || !m.Declared() {
			t.Errorf("%s should declare a size, got %+v (found=%v)", id, m, ok)
			continue
		}
		var unit string
		found := false
		for _, entry := range c.schema {
			if entry.Name == c.paramName {
				unit, found = entry.Unit, true
				break
			}
		}
		if !found {
			t.Errorf("%s: no param %q in its schema to measure a size against", id, c.paramName)
			continue
		}
		if m.Unit != unit {
			t.Errorf("%s: size unit %q does not match the unit of the %q param it is compared against (%q) -- the two must not drift",
				id, m.Unit, c.paramName, unit)
		}
	}
}

// TestDeclarativeEmissionCarriesItsCountAsSize pins the one rule for
// every declarative definition, shipped or operator-authored: its size
// is the counting-mode tally that crossed the threshold. Driven through
// a real shipped definition rather than by constructing an Emission, so
// what is proven is the number a real firing actually produces.
func TestDeclarativeEmissionCarriesItsCountAsSize(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{})

	t0 := time.Now()
	for port := 1; port <= 15; port++ {
		dd.Evaluate(psEvt("203.0.113.9", port, t0.Add(time.Duration(port)*time.Second)))
	}
	f := psFlagOfType(fs)
	if f == nil {
		t.Fatal("expected a port_scan flag at threshold")
	}
	if f.Size == nil || *f.Size != 15 {
		t.Fatalf("expected the flag to carry the 15 distinct ports that crossed the threshold as its size, got %v", f.Size)
	}
	if f.ExpectedSize != nil {
		t.Errorf("a flag with no expectation behind it must carry no expected size, got %v", f.ExpectedSize)
	}
}

// TestExpectationAbsorbsAndReRaisesThroughTheEngine is #640's end-to-end
// shape: a real port_scan definition firing into a real flags store,
// with an expectation recorded from the first firing. The point of
// driving the whole path is that the size has to survive every hop --
// Emission, Route, FlagsSink, AddEmission -- for the store's check to
// see anything at all; a store-only test would pass with the engine
// silently dropping it.
func TestExpectationAbsorbsAndReRaisesThroughTheEngine(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedPortScanDefinition(t, fs, Scope{})
	ip := "203.0.113.9"
	t0 := time.Now()

	// A 20-port scan, judged normal for this host.
	for port := 1; port <= 20; port++ {
		dd.Evaluate(psEvt(ip, port, t0.Add(time.Duration(port)*time.Second)))
	}
	first := psFlagOfType(fs)
	if first == nil {
		t.Fatal("expected a first port_scan flag")
	}
	if first.Size == nil || *first.Size != 20 {
		t.Fatalf("expected the first firing to carry size 20, got %v", first.Size)
	}
	if _, ok := fs.SetVerdict(first.ID, flags.VerdictExpected, "operator", t0); !ok {
		t.Fatal("expected recording an expectation from the flag to succeed")
	}

	// A later 25-port scan is within 1.5x of 20 (ceiling 30) and must be
	// absorbed. A fresh definition, so the window state starts clean and
	// the count really is 25 rather than a continuation of the first.
	dd = newShippedPortScanDefinition(t, fs, Scope{})
	t1 := t0.Add(time.Hour)
	for port := 1; port <= 25; port++ {
		dd.Evaluate(psEvt(ip, port, t1.Add(time.Duration(port)*time.Second)))
	}
	if f := psFlagOfType(fs); f == nil || !f.Cleared {
		t.Fatalf("a 25-port scan is within 1.5x the expected 20 and must stay absorbed, got %+v", fs.List())
	}
	ex, ok := fs.Expectation(flags.TypePortScan, ip)
	if !ok {
		t.Fatal("expected the expectation to still be recorded")
	}
	if ex.Absorbed == 0 {
		t.Error("expected the absorbed firings to be counted on the expectation")
	}

	// A 40-port scan is past the ceiling of 30 and must come back,
	// carrying both numbers so a card can say "expected up to 20, saw 40".
	dd = newShippedPortScanDefinition(t, fs, Scope{})
	t2 := t0.Add(2 * time.Hour)
	for port := 1; port <= 40; port++ {
		dd.Evaluate(psEvt(ip, port, t2.Add(time.Duration(port)*time.Second)))
	}
	back := psFlagOfType(fs)
	if back == nil || back.Cleared {
		t.Fatalf("a 40-port scan is above 1.5x the expected 20 and must raise again, got %+v", fs.List())
	}
	if back.ExpectedSize == nil || *back.ExpectedSize != 20 {
		t.Errorf("expected the returned flag to carry the recorded size 20, got %v", back.ExpectedSize)
	}
	if back.Size == nil || *back.Size != 40 {
		t.Errorf("expected the returned flag to carry the observed size 40, got %v", back.Size)
	}
}

// TestSizelessDefinitionExpectationIgnoresOutright covers the other
// declaration: a definition that declares no size produces no size, so
// an expectation recorded from one of its flags absorbs everything --
// the older "ignore this host on this detector" meaning, kept intact.
//
// device_silence is the worked example (its declaration says silence has
// no magnitude), driven here through the flags store the same way its
// sweep would, since building its whole device-registry harness would
// prove nothing extra about the size path.
func TestSizelessDefinitionExpectationIgnoresOutright(t *testing.T) {
	if m, ok := ShippedSizeMeasure(string(flags.TypeDeviceSilence)); !ok || m.Declared() {
		t.Fatalf("this test assumes device_silence declares no size; declaration is %+v (found=%v)", m, ok)
	}

	fs := newTestFlagsStore(t)
	now := time.Now()
	// nil size: what a declares-none definition's emission carries.
	fs.AddEmission(flags.TypeDeviceSilence, "router-1", "no syslog for 2h", nil, flags.Evidence{}, "", false, nil, now)
	var id string
	for _, f := range fs.List() {
		if f.Type == flags.TypeDeviceSilence {
			id = f.ID
		}
	}
	if id == "" {
		t.Fatal("expected a device_silence flag to raise")
	}
	if _, ok := fs.SetVerdict(id, flags.VerdictExpected, "operator", now); !ok {
		t.Fatal("expected recording an expectation to succeed")
	}

	ex, _ := fs.Expectation(flags.TypeDeviceSilence, "router-1")
	if ex.Size != nil {
		t.Errorf("a declares-none definition must record a size-less expectation, got %v", ex.Size)
	}

	fs.AddEmission(flags.TypeDeviceSilence, "router-1", "no syslog for 9 days", nil, flags.Evidence{}, "", false, nil, now.Add(time.Hour))
	for _, f := range fs.List() {
		if f.Type == flags.TypeDeviceSilence && !f.Cleared {
			t.Errorf("a size-less expectation must ignore this pair outright, got an active flag %+v", f)
		}
	}
}
