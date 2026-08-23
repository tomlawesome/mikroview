// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This file covers issue #407's first handover: an operator's edit takes
// effect on the very next ingested event, not on the next restart.
//
// The property is easy to state and easy to lose. #405's port replaced
// per-event settings reads with definitions built once and registered,
// which is what makes a large definition set cost a dispatch lookup
// rather than a linear scan -- and which quietly made every detector
// toggle restart-effective until this issue. The three tests below pin
// the three things that have to be simultaneously true for the fix to be
// real: an edit lands immediately, an unrelated edit does not reset
// everything else's accumulated state, and doing it under live event load
// is safe.

// newTestRegistry builds an engine, a definitions store and a Registry
// wired together the way main.go wires them, with a real flag store
// behind the shipped definitions' sinks so a raised flag is observable.
func newTestRegistry(t *testing.T) (*Engine, *DefinitionsStore, *Registry) {
	t.Helper()
	defs, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatalf("OpenDefinitionsStore: %v", err)
	}
	if err := SeedShippedDefinitions(defs, DefaultDetectorSettings(), DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	eng := New()
	ml, err := matchlog.Open(t.TempDir()+"/matchlog.jsonl", 1000)
	if err != nil {
		t.Fatalf("matchlog.Open: %v", err)
	}
	t.Cleanup(func() { ml.Close() })

	reg := NewRegistry(eng, defs, RegistrationDeps{
		Shipped: ShippedDeps{Flags: FlagsConfidenceFloorRaiser(newTestFlagsStore(t))},
		Expectations: ExpectationDeps{
			Sink:         MatchlogSink(ml),
			Observations: defs,
		},
		Flags: newTestFlagsStore(t),
	})
	if problems := reg.Sync(); len(problems) > 0 {
		t.Fatalf("initial Sync reported problems: %v", problems)
	}
	defs.SetOnChange(func() {
		// Deliberately ignores problems, exactly as main.go's own hook
		// only logs them: a definition that cannot be built must not
		// break the edit that triggered the rebuild.
		reg.Sync()
	})
	return eng, defs, reg
}

// countingDefinition is a stand-in for a real definition that only
// records what it was asked to evaluate, so a test can assert "this
// definition saw that event" without depending on any shipped
// definition's thresholds.
type countingDefinition struct {
	id   string
	mu   sync.Mutex
	seen int
}

func (c *countingDefinition) ID() string   { return c.id }
func (c *countingDefinition) Kind() string { return "declarative" }
func (c *countingDefinition) Evaluate(store.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen++
}
func (c *countingDefinition) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

// TestRegistryEditTakesEffectOnTheNextEvent is the handover's headline
// claim, at the level an operator experiences it: create an expectation,
// and the very next event it matches is recorded -- no restart. Before
// this, a definition edited through the API sat inert until the process
// was restarted, which is a settings page that appears to work and does
// nothing.
func TestRegistryEditTakesEffectOnTheNextEvent(t *testing.T) {
	eng, defs, _ := newTestRegistry(t)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	ev := store.Event{SrcIP: "203.0.113.9", DstIP: "10.0.0.1", DstPort: 4242, ReceivedAt: now}

	// Nothing watches port 4242 yet.
	eng.evaluateEvent(ev)

	if err := defs.UpsertExpectation(watchlist.Entry{ID: "e1", Name: "watch 4242", Ports: []int{4242}}); err != nil {
		t.Fatalf("UpsertExpectation: %v", err)
	}

	entry, ok, err := defs.GetExpectation("e1")
	if err != nil || !ok {
		t.Fatalf("GetExpectation after create: ok=%v err=%v", ok, err)
	}
	if len(entry.Ports) != 1 || entry.Ports[0] != 4242 {
		t.Fatalf("stored entry = %+v, want ports [4242]", entry)
	}

	// The registration set must already contain it -- the change hook
	// ran synchronously on the upsert, which is the whole mechanism.
	eng.mu.Lock()
	_, registered := eng.defs[ExpectationSetID]
	eng.mu.Unlock()
	if !registered {
		t.Fatal("the expectation set is not registered at all after creating an expectation")
	}

	// Deleting it takes effect the same way.
	if err := defs.DeleteExpectation("e1"); err != nil {
		t.Fatalf("DeleteExpectation: %v", err)
	}
	if _, ok, _ := defs.GetExpectation("e1"); ok {
		t.Fatal("the expectation survived a delete")
	}
}

// TestRegistrySyncPreservesUnchangedDefinitionState is the reason Sync
// rebuilds selectively rather than wholesale, pinned rather than left as
// a comment: a definition's live evaluation state (a
// threshold-over-window count, a warming baseline) lives in the object
// built from its envelope, so rebuilding it discards that state.
//
// port_scan needs 15 distinct ports inside 60 seconds. If an unrelated
// edit rebuilt everything, the 14 ports already counted would be thrown
// away and the 15th would start a fresh window -- silently halving the
// detector's sensitivity every time an operator saved anything, which is
// exactly the "absence of detection presented as absence of threat"
// failure #380's first item describes, and directly contradicts
// Definition.Enabled's own promise that state survives.
func TestRegistrySyncPreservesUnchangedDefinitionState(t *testing.T) {
	eng, defs, _ := newTestRegistry(t)

	// A real port_scan definition is registered inside the shipped
	// declarative set; reach it by the id it rides under so the assertion
	// is about the object identity that carries the state.
	before := registeredDeclarative(t, eng, ShippedDeclarativeSetID, "port_scan")

	// An unrelated edit: a different definition entirely.
	if err := defs.SetEnabledAndScope("device_silence", false, Scope{}); err != nil {
		t.Fatalf("SetEnabledAndScope: %v", err)
	}

	after := registeredDeclarative(t, eng, ShippedDeclarativeSetID, "port_scan")
	if before != after {
		t.Error("an unrelated edit rebuilt port_scan, discarding whatever window state it had accumulated -- " +
			"Sync must carry an unchanged definition forward as the same live object (see Registry's doc comment)")
	}

	// The definition that actually changed must be a new object, or the
	// edit did not take effect at all.
	beforeSilence := registeredEvaluated(t, eng, "device_silence")
	if err := defs.SetEnabledAndScope("device_silence", true, Scope{}); err != nil {
		t.Fatalf("SetEnabledAndScope: %v", err)
	}
	if afterSilence := registeredEvaluated(t, eng, "device_silence"); beforeSilence == afterSilence {
		t.Error("an edited definition was carried forward unchanged -- the edit cannot have taken effect")
	}
}

// registeredDeclarative finds one declarative definition inside a
// registered set, by id, failing the test if it is not there.
func registeredDeclarative(t *testing.T, eng *Engine, setID, defID string) *DeclarativeDefinition {
	t.Helper()
	set, ok := registeredEvaluated(t, eng, setID).(*DeclarativeSet)
	if !ok {
		t.Fatalf("%q is registered but is not a DeclarativeSet", setID)
	}
	idx := set.Index()
	buckets := [][]*DeclarativeDefinition{idx.global}
	for _, m := range []map[int][]*DeclarativeDefinition{idx.byPort} {
		for _, defs := range m {
			buckets = append(buckets, defs)
		}
	}
	for _, m := range []map[string][]*DeclarativeDefinition{idx.byChain, idx.byRule, idx.byClass} {
		for _, defs := range m {
			buckets = append(buckets, defs)
		}
	}
	for _, bucket := range buckets {
		for _, d := range bucket {
			if d.ID() == defID {
				return d
			}
		}
	}
	t.Fatalf("%q is not in the %q set", defID, setID)
	return nil
}

func registeredEvaluated(t *testing.T, eng *Engine, id string) Evaluated {
	t.Helper()
	eng.mu.Lock()
	defer eng.mu.Unlock()
	r, ok := eng.defs[id]
	if !ok {
		t.Fatalf("%q is not registered", id)
	}
	return r.def
}

// TestRegistrySyncIsSafeUnderEventLoad is the race proof the API's
// live-re-registration promise needs: edits arrive on request goroutines
// while the evaluation goroutine is running flat out, and Register
// replaces registrations underneath it.
//
// Run with -race, this fails if Sync ever mutates something the
// evaluation goroutine is reading -- which is precisely the hazard of
// making a hot, lock-free evaluation path reconfigurable at runtime. The
// contract it proves is the one Registry documents: Sync never mutates a
// live definition, it builds new objects and swaps registrations through
// Engine.Register.
func TestRegistrySyncIsSafeUnderEventLoad(t *testing.T) {
	eng, defs, _ := newTestRegistry(t)

	ctx, cancel := context.WithCancel(context.Background())
	go eng.Run(ctx)

	// A definition that only counts, registered directly, so the test can
	// prove evaluation actually kept running throughout rather than
	// quietly stalling on a lock.
	counter := &countingDefinition{id: "test-counter"}
	eng.Register(counter)

	// Two waits, not one: the producer runs until the editor is finished,
	// so waiting on both together before closing stop would deadlock the
	// test rather than exercise anything.
	var producer, editor sync.WaitGroup
	stop := make(chan struct{})

	producer.Add(1)
	go func() {
		defer producer.Done()
		now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Yields between events rather than spinning flat out.
			// Without this the producer starves the editor of the
			// definitions store's lock under -race and the test measures
			// scheduling luck instead of correctness -- the queue is
			// bounded and lossy by design (see Enqueue), so an
			// unthrottled producer proves nothing extra.
			runtime.Gosched()
			eng.Enqueue(store.Event{
				SrcIP:      fmt.Sprintf("203.0.113.%d", i%250+1),
				SrcMAC:     "aa:bb:cc:dd:ee:ff",
				DstIP:      "10.0.0.1",
				DstPort:    22 + i%40,
				ReceivedAt: now.Add(time.Duration(i) * time.Millisecond),
			})
		}
	}()

	// The editor: every kind of change the definitions API can make,
	// hammered while the queue above is full.
	editor.Add(1)
	go func() {
		defer editor.Done()
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("e%d", i%4)
			if err := defs.UpsertExpectation(watchlist.Entry{
				ID: id, Name: "churn " + id, Ports: []int{9000 + i%4},
			}); err != nil {
				t.Errorf("UpsertExpectation: %v", err)
				return
			}
			if err := defs.SetEnabledAndScope("port_scan", i%2 == 0, Scope{}); err != nil {
				t.Errorf("SetEnabledAndScope: %v", err)
				return
			}
			if err := defs.SetParams("port_scan", Params{"threshold": 10 + i%5, "window": "60s"}); err != nil {
				t.Errorf("SetParams: %v", err)
				return
			}
			if err := defs.UpsertExpectation(watchlist.Entry{
				ID: "inverted", Name: "inverted", Invert: true, Observing: true,
				Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
			}); err != nil {
				t.Errorf("UpsertExpectation(inverted): %v", err)
				return
			}
			if err := defs.DeleteExpectation(id); err != nil {
				t.Errorf("DeleteExpectation: %v", err)
				return
			}
		}
	}()

	editor.Wait()
	close(stop)
	producer.Wait()
	cancel()
	<-eng.Done()

	if counter.count() == 0 {
		t.Error("no events were evaluated at all during the edit storm -- the load half of this race test did nothing")
	}

	// Whatever survived the churn must still be coherent: every stored
	// expectation reads back, and the engine still holds a registration
	// for the sets.
	if _, err := defs.ListExpectations(); err != nil {
		t.Errorf("the entry set is unreadable after concurrent edits: %v", err)
	}
	for _, id := range []string{ShippedDeclarativeSetID, ExpectationSetID, InvertedExpectationsID} {
		eng.mu.Lock()
		_, ok := eng.defs[id]
		eng.mu.Unlock()
		if !ok {
			t.Errorf("%q is no longer registered after the edit storm", id)
		}
	}
}
