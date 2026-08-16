// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// The two end-to-end pins in this file moved here from
// internal/watchlist/characterization_test.go with the code they
// characterize (#397's watchlist suite, #406's port). They drove a real
// Store + Evaluator + matchlog; the Evaluator is gone, so they drive a
// real Store + the engine's expectation definitions + matchlog instead.
//
// Every assertion is the one that file made, unchanged in what it
// claims -- which is the point of a characterization pin: the port is
// only honest if the behaviour it describes survives verbatim. What
// changed is the four lines that hand an event to the evaluation path,
// and the harness below that rebuilds the definitions when the entry set
// changes (production does the same thing, through
// DefinitionsStore.SetOnChange -- see main.go and Registry).
//
// The entry set itself moved again with #407: watchlist.Store is deleted
// and entries are expectation definitions in DefinitionsStore. Every
// assertion below is still the one internal/watchlist wrote -- what
// changed is which store the harness upserts into, which is exactly what
// a characterization pin is for.
//
// The rest of that file stayed where it is: matchNonInverted's and
// matchInverted's own rules, the matchlog row shapes and Coverage's four
// states are all pinned against code that did not move.

// expectationHarness is a live watchlist entry set wired onto the
// engine's expectation definitions exactly as main.go wires it: the two
// registrations are rebuilt whenever the entry set changes, and both
// emit into one real matchlog.Store.
type expectationHarness struct {
	entries *DefinitionsStore
	ml      matchlog.Store
	members AddressListMembership

	mu       sync.Mutex
	decl     *DeclarativeSet
	inverted *InvertedExpectations
}

func newExpectationHarness(t *testing.T, members AddressListMembership, capacity int) *expectationHarness {
	t.Helper()
	entries, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatalf("OpenDefinitionsStore: %v", err)
	}
	ml, err := matchlog.Open(filepath.Join(t.TempDir(), "matchlog.jsonl"), capacity)
	if err != nil {
		t.Fatalf("matchlog.Open: %v", err)
	}
	t.Cleanup(func() { ml.Close() })

	h := &expectationHarness{entries: entries, ml: ml, members: members}
	entries.SetOnChange(func() { h.rebuild(t) })
	h.rebuild(t)
	return h
}

func (h *expectationHarness) rebuild(t *testing.T) {
	t.Helper()
	list, err := h.entries.ListExpectations()
	if err != nil {
		t.Fatalf("ListExpectations: %v", err)
	}
	decl, inverted, err := BuildExpectations(list, ExpectationDeps{
		Members:      h.members,
		Sink:         MatchlogSink(h.ml),
		Observations: h.entries,
	})
	if err != nil {
		t.Fatalf("BuildExpectations: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.decl, h.inverted = decl, inverted
}

// mustGet reads one entry back out of the definitions store, failing the
// test rather than returning a zero value: every call site below is
// asserting on what was stored, so an unreadable entry is a failure to
// report, never a comparison against an empty struct.
func (h *expectationHarness) mustGet(t *testing.T, id string) watchlist.Entry {
	t.Helper()
	e, ok, err := h.entries.GetExpectation(id)
	if err != nil {
		t.Fatalf("GetExpectation(%s): %v", id, err)
	}
	if !ok {
		t.Fatalf("GetExpectation(%s): no such expectation", id)
	}
	return e
}

// evaluate is the harness's stand-in for the ingest path handing one
// event to the engine -- both registrations, in turn, the same way
// Engine.evaluateEvent drives them.
func (h *expectationHarness) evaluate(e store.Event) {
	h.mu.Lock()
	decl, inverted := h.decl, h.inverted
	h.mu.Unlock()
	decl.Evaluate(e)
	inverted.Evaluate(e)
}

// TestCharacterizationNonInverted_EndToEnd runs a mixed entry set (one
// axis each: plain ports, MAC-scoped source, dest-IP-scoped, and
// address-list-scoped) through the engine's expectation definitions
// against a real matchlog.FileStore, and pins exactly which entries fire
// for which events -- proving the axes are independently enforced
// through the whole pipeline, not just inside one matcher in isolation.
func TestCharacterizationNonInverted_EndToEnd(t *testing.T) {
	h := newExpectationHarness(t, fakeLists{"core\x00mgmt": {"192.168.1.200"}}, 100)
	for _, e := range []watchlist.Entry{
		{ID: "by-port", Ports: []int{22}},
		{ID: "by-mac", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, Ports: []int{8080}},
		{ID: "by-destip", DestIP: "10.0.0.9", Ports: []int{443}},
		{ID: "by-addrlist", SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{9999}},
	} {
		if err := h.entries.UpsertExpectation(e); err != nil {
			t.Fatalf("UpsertExpectation(%s): %v", e.ID, err)
		}
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		event   store.Event
		wantHit string // entry ID expected to fire, or "" for none
	}{
		{"port match", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 22}, "by-port"},
		{"port mismatch", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 23}, ""},
		{"mac match", store.Event{SrcMAC: "aa:bb:cc:dd:ee:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, "by-mac"},
		{"mac mismatch", store.Event{SrcMAC: "ff:ff:ff:ff:ff:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, ""},
		{"destip match", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.9", DstPort: 443}, "by-destip"},
		{"destip mismatch", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.10", DstPort: 443}, ""},
		{"addrlist member", store.Event{SrcIP: "192.168.1.200", SrcMAC: "", DstIP: "10.0.0.3", DstPort: 9999}, "by-addrlist"},
		{"addrlist non-member", store.Event{SrcIP: "192.168.1.201", SrcMAC: "", DstIP: "10.0.0.3", DstPort: 9999}, ""},
	}
	for _, tc := range cases {
		e := tc.event
		e.ReceivedAt = now
		now = now.Add(time.Second)
		h.evaluate(e)
	}

	if stats := h.ml.Stats(); stats.Count != 4 {
		t.Fatalf("expected exactly 4 recorded matches (one per axis that should have hit), got %d", stats.Count)
	}
}

// TestCharacterizationInverted_ObserveToViolation runs the whole
// inverted lifecycle through the real Store + the engine: first
// observation, a repeat updating LastSeen/Count without touching
// FirstSeen, promotion to Permitted (which removes the pair from
// Observed), SetObserving(false) leaving observe mode, a permitted
// destination never firing, and a still-unpromoted destination firing
// as a Violation once observing stops.
func TestCharacterizationInverted_ObserveToViolation(t *testing.T) {
	h := newExpectationHarness(t, nil, 100)
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	if err := h.entries.UpsertExpectation(watchlist.Entry{ID: "device-x", Invert: true, Observing: true, Source: src}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

	destA := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: t0}
	h.evaluate(destA)

	e := h.mustGet(t, "device-x")
	if len(e.Observed) != 1 {
		t.Fatalf("expected 1 observed candidate after the first sighting, got %+v", e.Observed)
	}
	first := e.Observed[0]
	if first.DestIP != "198.51.100.10" || first.Port != 80 || first.Count != 1 {
		t.Fatalf("first observation = %+v, want {DestIP:198.51.100.10 Port:80 Count:1}", first)
	}
	if !first.FirstSeen.Equal(t0) || !first.LastSeen.Equal(t0) {
		t.Errorf("first observation FirstSeen/LastSeen = %v/%v, want both %v", first.FirstSeen, first.LastSeen, t0)
	}
	if h.ml.Stats().Count != 0 {
		t.Fatalf("expected no matchlog record while observing, got %d", h.ml.Stats().Count)
	}

	// Repeat: same destination/port, later time.
	t1 := t0.Add(time.Hour)
	destAAgain := destA
	destAAgain.ReceivedAt = t1
	h.evaluate(destAAgain)
	e = h.mustGet(t, "device-x")
	if len(e.Observed) != 1 {
		t.Fatalf("expected the repeat to update the existing candidate, not add a second one, got %+v", e.Observed)
	}
	repeat := e.Observed[0]
	if repeat.Count != 2 {
		t.Errorf("Count after a repeat = %d, want 2", repeat.Count)
	}
	if !repeat.FirstSeen.Equal(t0) {
		t.Errorf("FirstSeen after a repeat = %v, want unchanged at %v", repeat.FirstSeen, t0)
	}
	if !repeat.LastSeen.Equal(t1) {
		t.Errorf("LastSeen after a repeat = %v, want updated to %v", repeat.LastSeen, t1)
	}

	// A second, distinct destination.
	t2 := t1.Add(time.Minute)
	destB := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.20", DstPort: 443, ReceivedAt: t2}
	h.evaluate(destB)
	e = h.mustGet(t, "device-x")
	if len(e.Observed) != 2 {
		t.Fatalf("expected 2 distinct observed candidates, got %+v", e.Observed)
	}

	// Promote destA:80 -- removed from Observed, added to Permitted.
	// Observing is untouched by Promote (invert.go's own doc comment).
	if err := h.entries.UpdateExpectation("device-x", func(e *watchlist.Entry) error {
		e.Promote([]watchlist.PermittedDest{{DestIP: "198.51.100.10", Port: 80}})
		return nil
	}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	e = h.mustGet(t, "device-x")
	if len(e.Observed) != 1 || e.Observed[0].DestIP != "198.51.100.20" {
		t.Fatalf("expected only destB left in Observed after promoting destA, got %+v", e.Observed)
	}
	if len(e.Permitted) != 1 || e.Permitted[0] != (watchlist.PermittedDest{DestIP: "198.51.100.10", Port: 80}) {
		t.Fatalf("expected destA in Permitted, got %+v", e.Permitted)
	}
	if !e.Observing {
		t.Error("expected Promote to leave Observing untouched (still true)")
	}

	// Leave observe mode.
	if err := h.entries.UpdateExpectation("device-x", func(e *watchlist.Entry) error {
		e.Observing = false
		return nil
	}); err != nil {
		t.Fatalf("SetObserving: %v", err)
	}
	e = h.mustGet(t, "device-x")
	if e.Observing {
		t.Fatal("expected Observing to be false after SetObserving(false)")
	}

	// The promoted destination never fires, no matter how it got there.
	t3 := t2.Add(time.Minute)
	destAThird := destA
	destAThird.ReceivedAt = t3
	h.evaluate(destAThird)
	if h.ml.Stats().Count != 0 {
		t.Fatalf("expected a permitted destination to never violate even once observing has stopped, got %d matches", h.ml.Stats().Count)
	}

	// destB was observed but never promoted -- now that Observing is
	// false, the identical traffic that used to be recorded as a
	// candidate becomes a Violation instead.
	t4 := t3.Add(time.Minute)
	destBAgain := destB
	destBAgain.ReceivedAt = t4
	h.evaluate(destBAgain)
	if h.ml.Stats().Count != 1 {
		t.Fatalf("expected exactly 1 violation (destB, unpromoted) once observing stopped, got %d", h.ml.Stats().Count)
	}
	var recorded []matchlog.Record
	if err := h.ml.Query(context.Background(), matchlog.Query{Source: src}, func(r matchlog.Record) bool {
		recorded = append(recorded, r)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 1 || recorded[0].Tuple.DestIP != "198.51.100.20" || recorded[0].Tuple.Port != 443 {
		t.Fatalf("recorded violation = %+v, want destB:443", recorded)
	}

	// A brand-new, never-observed destination also violates immediately
	// -- there is no "must have been observed first" requirement once
	// Observing is false.
	t5 := t4.Add(time.Minute)
	destC := store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.30", DstPort: 22, ReceivedAt: t5}
	h.evaluate(destC)
	if h.ml.Stats().Count != 2 {
		t.Fatalf("expected a second violation for the brand-new destination, got %d", h.ml.Stats().Count)
	}
}

// TestCharacterizationExpectationEditTakesEffectOnTheNextEvent pins the
// property the port had to keep paying for: evaluation no longer
// re-reads the entry set per event, so an operator's edit only lands
// because the definitions are rebuilt on change. Without that, a new
// entry would sit inert until restart -- an entry that looks configured
// and does nothing, which is the exact silent failure the watchlist
// exists to avoid.
func TestCharacterizationExpectationEditTakesEffectOnTheNextEvent(t *testing.T) {
	h := newExpectationHarness(t, nil, 100)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	e := store.Event{SrcIP: "203.0.113.1", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now}

	h.evaluate(e)
	if got := h.ml.Stats().Count; got != 0 {
		t.Fatalf("an empty entry set recorded %d matches, want 0", got)
	}

	if err := h.entries.UpsertExpectation(watchlist.Entry{ID: "e1", Ports: []int{22}}); err != nil {
		t.Fatal(err)
	}
	e.ReceivedAt = now.Add(time.Second)
	h.evaluate(e)
	if got := h.ml.Stats().Count; got != 1 {
		t.Fatalf("a freshly created entry recorded %d matches on the next event, want 1", got)
	}

	if err := h.entries.DeleteExpectation("e1"); err != nil {
		t.Fatalf("DeleteExpectation: %v", err)
	}
	e.ReceivedAt = now.Add(2 * time.Second)
	h.evaluate(e)
	if got := h.ml.Stats().Count; got != 1 {
		t.Fatalf("a deleted entry recorded a further match: %d records, want 1", got)
	}
}
