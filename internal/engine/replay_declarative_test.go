// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeCorpus is a Corpus implementation entirely under test control --
// used throughout this file instead of MemoryCorpus so DeclarativeDefinition.Replay's
// own logic (candidate override, decline threshold, sample bounding,
// state isolation) can be pinned precisely, independent of
// MemoryCorpus/store.Store's own behaviour (covered separately by
// corpus_test.go and corpus_concurrency_test.go).
type fakeCorpus struct {
	events    []store.Event
	truncated bool
}

func (c fakeCorpus) Replay(visit func(store.Event)) CorpusWindow {
	for _, e := range c.events {
		visit(e)
	}
	var start, end time.Time
	if len(c.events) > 0 {
		start = c.events[0].ReceivedAt
		end = c.events[len(c.events)-1].ReceivedAt
	}
	return CorpusWindow{Start: start, End: end, Count: len(c.events), Truncated: c.truncated}
}

// replayTestWindow is buildReplayTestDef's own window -- short (10s)
// relative to watchedEvents' 1-second spacing so a modest event count
// (15, giving a 14s corpus span) comfortably clears it, without every
// test needing dozens of events to avoid an incidental Decline.
const replayTestWindow = 10 * time.Second

// buildReplayTestDef builds a DeclarativeDefinition matching destination
// port 22, keyed per-source, threshold 5 over replayTestWindow -- small
// and easy to reason about across every test in this file.
func buildReplayTestDef(t *testing.T) *DeclarativeDefinition {
	t.Helper()
	def := declTestDef(IntentDetection)
	conds := []Condition{
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}},
		{Field: FieldAction, Operator: OpEquals, Values: []string{string(store.ActionDrop)}},
	}
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         replayTestWindow,
		Threshold:      5,
		CountingMode:   CountingTotal,
		DetailTemplate: "{PortCount} hits on watched ports from this source",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}
	return dd
}

// watchedEvents builds n events, one per second starting at base, that
// match buildReplayTestDef's own conditions, all from the same source
// (so KeyPerSource's window/threshold counting applies to all of them
// together). n should be at least 12 wherever the corpus must clear
// replayTestWindow (a 10s window): n-1 seconds of span, so n=15 gives a
// comfortable 14s margin.
func watchedEvents(base time.Time, n int) []store.Event {
	events := make([]store.Event, n)
	for i := range events {
		events[i] = evtAt("198.51.100.7", fmt.Sprintf("10.0.0.%d", i%5), 22, base.Add(time.Duration(i)*time.Second))
		events[i].Action = store.ActionDrop
	}
	return events
}

func TestDeclarativeDefinitionReplayProducesReceiptOverLongEnoughCorpus(t *testing.T) {
	dd := buildReplayTestDef(t)
	base := time.Now().Add(-2 * time.Minute)
	events := watchedEvents(base, 15) // threshold=5, window=10s, span=14s -> crosses at event 5, stays crossed for 5-15

	result, err := dd.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Decline != nil {
		t.Fatalf("Replay declined unexpectedly: %+v", result.Decline)
	}
	if result.Receipt == nil {
		t.Fatal("Replay returned neither a Receipt nor a Decline")
	}
	r := *result.Receipt

	// Events 5..15 (eleven events) are at/above the threshold once counted.
	if want := 11; r.EmissionCount() != want {
		t.Errorf("EmissionCount() = %d, want %d", r.EmissionCount(), want)
	}
	if r.Window().EventCount() != len(events) {
		t.Errorf("Window().EventCount() = %d, want %d", r.Window().EventCount(), len(events))
	}
	if !r.Window().Start().Equal(events[0].ReceivedAt) || !r.Window().End().Equal(events[len(events)-1].ReceivedAt) {
		t.Errorf("Window() span (%s, %s) does not match the corpus's actual span (%s, %s)",
			r.Window().Start(), r.Window().End(), events[0].ReceivedAt, events[len(events)-1].ReceivedAt)
	}
	if r.AnyProvisional() {
		t.Error("AnyProvisional() = true; DeclarativeDefinition has no baseline concept, want false always")
	}
	sample := r.Sample()
	if len(sample) != r.EmissionCount() {
		t.Fatalf("len(Sample()) = %d, want %d (below replaySampleBound)", len(sample), r.EmissionCount())
	}
	for _, s := range sample {
		if s.Target != "198.51.100.7" {
			t.Errorf("sample Target = %q, want the per-source key", s.Target)
		}
		if s.Detail == "" {
			t.Error("sample Detail is empty, want the rendered template")
		}
		if len(s.Ports) == 0 {
			t.Error("sample Ports is empty, want the accumulated destination port(s)")
		}
	}
}

// TestDeclarativeDefinitionReplayMatchesLiveEvaluate cross-checks
// Replay's emission count against what Evaluate itself produces for the
// exact same event sequence -- the two code paths must agree, since
// Replay's whole purpose is to answer "what would live evaluation have
// done."
func TestDeclarativeDefinitionReplayMatchesLiveEvaluate(t *testing.T) {
	base := time.Now().Add(-2 * time.Minute)
	events := watchedEvents(base, 12)

	live := buildReplayTestDef(t)
	var liveEmissions int
	live.OnRoutedEmission = func(RoutedEmission) { liveEmissions++ }
	for _, e := range events {
		live.Evaluate(e)
	}

	replay := buildReplayTestDef(t)
	result, err := replay.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt == nil {
		t.Fatalf("Replay declined unexpectedly: %+v", result.Decline)
	}

	if result.Receipt.EmissionCount() != liveEmissions {
		t.Errorf("Replay EmissionCount() = %d, live Evaluate produced %d emissions -- must agree", result.Receipt.EmissionCount(), liveEmissions)
	}
}

// TestDeclarativeDefinitionReplayDeclinesWhenCorpusShorterThanWindow
// pins issue #403's own requirement: "A receipt over a corpus shorter
// than the definition's own window DECLINES ... rather than reporting a
// misleading zero."
func TestDeclarativeDefinitionReplayDeclinesWhenCorpusShorterThanWindow(t *testing.T) {
	dd := buildReplayTestDef(t) // window = replayTestWindow (10s)
	base := time.Now().Add(-30 * time.Second)
	events := watchedEvents(base, 3) // spans ~2s -- far short of the 10s window

	result, err := dd.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt != nil {
		t.Fatalf("Replay returned a Receipt for a corpus shorter than the definition's window: %+v", result.Receipt)
	}
	if result.Decline == nil {
		t.Fatal("Replay returned neither a Receipt nor a Decline")
	}
	if result.Decline.DefinitionWindow != replayTestWindow {
		t.Errorf("Decline.DefinitionWindow = %s, want %s", result.Decline.DefinitionWindow, replayTestWindow)
	}
	if result.Decline.Reason == "" {
		t.Error("Decline.Reason is empty, want a stated reason")
	}
}

func TestDeclarativeDefinitionReplayDeclinesOnEmptyCorpus(t *testing.T) {
	dd := buildReplayTestDef(t)
	result, err := dd.Replay(fakeCorpus{}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt != nil {
		t.Fatal("Replay returned a Receipt for an empty corpus, want a Decline")
	}
	if result.Decline == nil {
		t.Fatal("Replay returned neither a Receipt nor a Decline for an empty corpus")
	}
}

// TestDeclarativeDefinitionReplayCandidateOverridesWindowAndThreshold
// pins that a candidate Params value actually changes what Replay
// judges -- the whole point of "candidate params" per issue #403: "at X
// this would have fired 6 times, not 41."
func TestDeclarativeDefinitionReplayCandidateOverridesWindowAndThreshold(t *testing.T) {
	dd := buildReplayTestDef(t) // stock: window=10s, threshold=5
	base := time.Now().Add(-2 * time.Minute)
	events := watchedEvents(base, 15)

	stock, err := dd.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay (stock): %v", err)
	}
	if stock.Receipt == nil {
		t.Fatalf("stock Replay declined unexpectedly: %+v", stock.Decline)
	}

	// A much higher candidate threshold should fire less often.
	tighter, err := dd.Replay(fakeCorpus{events: events}, Params{"threshold": 9})
	if err != nil {
		t.Fatalf("Replay (candidate threshold=9): %v", err)
	}
	if tighter.Receipt == nil {
		t.Fatalf("candidate Replay declined unexpectedly: %+v", tighter.Decline)
	}
	if tighter.Receipt.EmissionCount() >= stock.Receipt.EmissionCount() {
		t.Errorf("candidate threshold=9 produced %d emissions, stock threshold=5 produced %d -- want strictly fewer",
			tighter.Receipt.EmissionCount(), stock.Receipt.EmissionCount())
	}

	// Confirm the live definition's own window/threshold were not
	// mutated by the candidate override.
	if dd.window != replayTestWindow || dd.threshold != 5 {
		t.Errorf("Replay mutated the live definition's own window/threshold: window=%s threshold=%d", dd.window, dd.threshold)
	}
}

func TestDeclarativeDefinitionReplayRejectsUnknownCandidateParam(t *testing.T) {
	dd := buildReplayTestDef(t)
	_, err := dd.Replay(fakeCorpus{events: watchedEvents(time.Now(), 1)}, Params{"bogus": 1})
	if err == nil {
		t.Fatal("Replay succeeded with an unknown candidate param, want a hard failure")
	}
}

func TestDeclarativeDefinitionReplayRejectsNonPositiveCandidateOverride(t *testing.T) {
	dd := buildReplayTestDef(t)
	events := watchedEvents(time.Now().Add(-2*time.Minute), 10)

	if _, err := dd.Replay(fakeCorpus{events: events}, Params{"threshold": 0}); err == nil {
		t.Fatal("Replay succeeded with threshold=0, want a hard failure (rejected by replayParamSchema's Min bound)")
	}
	if _, err := dd.Replay(fakeCorpus{events: events}, Params{"window": "0s"}); err == nil {
		t.Fatal("Replay succeeded with window=0s, want a hard failure (rejected by replayParamSchema's Min bound)")
	}
}

// TestDeclarativeDefinitionReplayDoesNotTouchLiveState pins that Replay
// runs against fresh, call-local state, never d.state: replaying enough
// matching events to cross threshold must not leave the live definition
// primed, so a subsequent live Evaluate call sees exactly the same
// sequence of emissions it would have without any replay ever having
// happened.
func TestDeclarativeDefinitionReplayDoesNotTouchLiveState(t *testing.T) {
	dd := buildReplayTestDef(t)
	base := time.Now().Add(-2 * time.Minute)
	replayEvents := watchedEvents(base, 10)

	if _, err := dd.Replay(fakeCorpus{events: replayEvents}, nil); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	var liveEmissions int
	dd.OnRoutedEmission = func(RoutedEmission) { liveEmissions++ }

	now := time.Now()
	for i := 0; i < 4; i++ {
		dd.Evaluate(evtAt("198.51.100.7", "10.0.0.9", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if liveEmissions != 0 {
		t.Fatalf("got %d live emission(s) after only 4 matching live events, want 0 -- Replay must not have pre-primed live state toward the threshold", liveEmissions)
	}
	dd.Evaluate(evtAt("198.51.100.7", "10.0.0.9", 22, now.Add(4*time.Second)))
	if liveEmissions != 1 {
		t.Fatalf("got %d live emission(s) after the 5th matching live event crossed threshold=5, want exactly 1", liveEmissions)
	}
}

// TestDeclarativeDefinitionReplaySampleBound pins that a candidate
// producing more emissions than replaySampleBound still reports the
// true EmissionCount while bounding Sample -- issue #403's "here are
// the 35 dropped" requirement, and its bound.
func TestDeclarativeDefinitionReplaySampleBound(t *testing.T) {
	def := declTestDef(IntentDetection)
	conds := []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}}
	// threshold=1: every single matching event is itself an emission,
	// so replaySampleBound+20 matching events produce that many
	// emissions. window=replayTestWindow, spaced 1s apart, so the
	// corpus span (n-1 seconds) comfortably clears it.
	dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
		Conditions:     conds,
		Key:            KeyPerSource,
		Window:         replayTestWindow,
		Threshold:      1,
		CountingMode:   CountingTotal,
		DetailTemplate: "hit",
		Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
	})
	if err != nil {
		t.Fatalf("NewDeclarativeDefinition: %v", err)
	}

	base := time.Now().Add(-2 * time.Minute)
	n := replaySampleBound + 20
	events := make([]store.Event, n)
	for i := range events {
		events[i] = evtAt("198.51.100.7", "10.0.0.1", 22, base.Add(time.Duration(i)*time.Second))
	}

	result, err := dd.Replay(fakeCorpus{events: events}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt == nil {
		t.Fatalf("Replay declined unexpectedly: %+v", result.Decline)
	}
	r := *result.Receipt
	if r.EmissionCount() != n {
		t.Fatalf("EmissionCount() = %d, want %d", r.EmissionCount(), n)
	}
	if len(r.Sample()) != replaySampleBound {
		t.Fatalf("len(Sample()) = %d, want exactly replaySampleBound=%d", len(r.Sample()), replaySampleBound)
	}
	if !r.SampleTruncated() {
		t.Error("SampleTruncated() = false, want true")
	}
}

func TestDeclarativeDefinitionReplayCorpusTruncatedPropagatesToReceipt(t *testing.T) {
	dd := buildReplayTestDef(t)
	events := watchedEvents(time.Now().Add(-2*time.Minute), 15)

	result, err := dd.Replay(fakeCorpus{events: events, truncated: true}, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt == nil {
		t.Fatalf("Replay declined unexpectedly: %+v", result.Decline)
	}
	if !result.Receipt.CorpusTruncated() {
		t.Error("Receipt.CorpusTruncated() = false, want true (fakeCorpus reported Truncated=true)")
	}
}

// TestDeclarativeDefinitionReplayOverMemoryCorpus is the one end-to-end
// check in this file that goes through the real MemoryCorpus/store.Store
// path rather than fakeCorpus, proving the two pieces (Corpus
// implementation, Replayable implementation) actually compose.
func TestDeclarativeDefinitionReplayOverMemoryCorpus(t *testing.T) {
	dd := buildReplayTestDef(t)
	s := store.New(1000, time.Hour)
	base := time.Now().Add(-2 * time.Minute)
	for _, e := range watchedEvents(base, 15) {
		s.Insert(e)
	}

	result, err := dd.Replay(NewMemoryCorpus(s), nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if result.Receipt == nil {
		t.Fatalf("Replay declined unexpectedly: %+v", result.Decline)
	}
	if result.Receipt.EmissionCount() != 11 {
		t.Errorf("EmissionCount() = %d, want 11", result.Receipt.EmissionCount())
	}
}
