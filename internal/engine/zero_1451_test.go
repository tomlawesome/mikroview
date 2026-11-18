// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// #1451: paths no other test reached.

func TestDefinitionsStoreResetParamsRestoresShippedValues(t *testing.T) {
	s := seededStore(t)
	before, ok := s.Get("port_scan")
	if !ok {
		t.Fatal("port_scan not seeded")
	}
	if err := s.SetParams("port_scan", Params{"threshold": 97, "window": "60s"}); err != nil {
		t.Fatalf("SetParams: %v", err)
	}
	if err := s.ResetParams("port_scan"); err != nil {
		t.Fatalf("ResetParams: %v", err)
	}
	after, _ := s.Get("port_scan")
	if !reflect.DeepEqual(after.Definition.Params, before.Definition.Provenance.ShippedParams) {
		t.Errorf("params after reset = %v, want shipped %v", after.Definition.Params, before.Definition.Provenance.ShippedParams)
	}
}

func TestDefinitionsStoreResetParamsRefusesWithoutShippedDefaults(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 3, "60s")
	if err := s.Upsert(def); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.ResetParams(def.ID); !errors.Is(err, ErrNoShippedDefaults) {
		t.Errorf("ResetParams on a custom definition = %v, want ErrNoShippedDefaults", err)
	}
}

func TestDefinitionsStoreResetEmptiesAndCountsAndNotifies(t *testing.T) {
	s := seededStore(t)
	want := len(s.List())
	if want == 0 {
		t.Fatal("seeded store is empty")
	}
	notified := 0
	s.SetOnChange(func() { notified++ })
	if got := s.Reset(); got != want {
		t.Errorf("Reset() = %d, want %d", got, want)
	}
	if notified != 1 {
		t.Errorf("change hook ran %d times after Reset, want 1", notified)
	}
	if n := len(s.List()); n != 0 {
		t.Errorf("%d definitions left after Reset", n)
	}
	if got := s.Reset(); got != 0 {
		t.Errorf("second Reset() = %d, want 0", got)
	}
}

func TestDeclarativeDefinitionAccessorsAndStructure(t *testing.T) {
	dd := buildReplayTestDef(t)
	if dd.Kind() != string(KindDeclarative) {
		t.Errorf("Kind() = %q", dd.Kind())
	}
	if dd.Definition().ID != dd.ID() {
		t.Errorf("Definition().ID = %q, ID() = %q", dd.Definition().ID, dd.ID())
	}
	st := dd.Structure()
	if st.Key != KeyPerSource || st.Counting != CountingTotal {
		t.Errorf("Structure key/counting = %v/%v", st.Key, st.Counting)
	}
	if st.DetailTemplate != "{PortCount} hits on watched ports from this source" {
		t.Errorf("Structure detail template = %q", st.DetailTemplate)
	}
	if len(st.Conditions) != 2 || st.Conditions[0].Field != FieldDestinationPort {
		t.Errorf("Structure conditions = %+v", st.Conditions)
	}
	// Conditions() hands out a copy: mutating it must not change the definition.
	c := dd.Conditions()
	c[0].Field = FieldAction
	if dd.Conditions()[0].Field != FieldDestinationPort {
		t.Error("Conditions() exposed the definition's own slice")
	}
}

func TestReplayDefinitionRunsACustomDetectionOverACorpus(t *testing.T) {
	def := customDetection(&DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} from {SourceAddress}",
	}, 5, "10s")
	events := watchedEvents(time.Now().Add(-2*time.Minute), 15)
	res, err := ReplayDefinition(def, fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("ReplayDefinition: %v", err)
	}
	if res.Receipt == nil || res.Decline != nil {
		t.Fatalf("want a receipt, got %+v", res)
	}
	if got := res.Receipt.EmissionCount(); got != 11 {
		t.Errorf("EmissionCount = %d, want 11", got)
	}
}

func TestReplayDefinitionRefusesANonReplayableDefinition(t *testing.T) {
	def := NewDefinition("x", IntentDetection, KindProgrammatic)
	def.ID = "mail_sender"
	def.Provenance = Provenance{Origin: ProvenanceShipped}
	if _, err := ReplayDefinition(def, fakeCorpus{}, nil); err == nil {
		t.Error("ReplayDefinition of a non-replayable definition returned no error")
	}
}

// A source whose hour bucket is mature is judged against that bucket, and
// the flag it raises says which hour and how many days of history it was
// judged against (emitBucketFiring), not the fallback wording.
func TestShippedActivitySpikeFiresAgainstAMatureHourBucket(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs,
		Params{"threshold": 20, "window": (5 * time.Second).String(), "warmupSamples": 50, "baselineMultiplier": 2.0}, Scope{})
	ip := "198.51.100.4"
	night1 := time.Date(2024, 3, 1, 22, 0, 0, 0, time.UTC)
	daytime := night1.Add(-10 * time.Hour)
	ev := func(at time.Time) store.Event {
		return store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 80, ConnState: "new", ReceivedAt: at}
	}
	for i := 0; i < 10; i++ {
		d.Evaluate(ev(daytime.Add(time.Duration(i) * 6 * time.Second)))
	}
	burst := func(start time.Time, n int, gap time.Duration) {
		for i := 0; i < n; i++ {
			d.Evaluate(ev(start.Add(time.Duration(i) * gap)))
		}
	}
	burst(night1, 30, 100*time.Millisecond)
	d.Evaluate(ev(night1.Add(10 * time.Second)))
	fs.SetVerdict(asFlagOfType(fs).ID, flags.VerdictChecked, "operator", "", night1.Add(11*time.Second))

	// Night two folds night one's peak (30) into the hour-22 bucket; a burst
	// far above 2x that peak is a spike against the bucket.
	night2 := night1.Add(24 * time.Hour)
	burst(night2, 100, 10*time.Millisecond)

	f := asFlagOfType(fs)
	if f == nil {
		t.Fatal("no flag")
	}
	if f.Cleared {
		t.Fatalf("a burst 3x the mature bucket's value did not fire: %+v", f)
	}
	for _, want := range []string{"vs a baseline of", "22:00", "day(s) of consistent history at this hour"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("Detail = %q, want it to contain %q", f.Detail, want)
		}
	}
}
