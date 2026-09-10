// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// snapshottedDef is a fakeDef that also carries state across a restart,
// so the chassis-level routing can be tested without any real
// definition's arithmetic in the way.
type snapshottedDef struct {
	fakeDef

	value       string
	exportErr   error
	importErr   error
	importCalls int
	importTaken time.Time
	importNow   time.Time
}

func (s *snapshottedDef) ExportState() (json.RawMessage, error) {
	if s.exportErr != nil {
		return nil, s.exportErr
	}
	return json.Marshal(s.value)
}

func (s *snapshottedDef) ImportState(raw json.RawMessage, taken, now time.Time) error {
	s.importCalls++
	s.importTaken, s.importNow = taken, now
	if s.importErr != nil {
		return s.importErr
	}
	return json.Unmarshal(raw, &s.value)
}

func newSnapshottedDef(id, value string) *snapshottedDef {
	return &snapshottedDef{fakeDef: fakeDef{id: id, kind: "programmatic"}, value: value}
}

// waitForRunning blocks until Run has reached its loop. A test cannot
// infer that from Run's goroutine having been started, and ExportState
// does not imply it either: an export that wins the race for mu is
// served inline, before Run has begun (which is exactly the interlock
// runOnEvaluationGoroutine exists for).
func waitForRunning(t *testing.T, e *Engine) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		running := e.running
		e.mu.Unlock()
		if running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Run did not start within 2s")
}

func TestEngineExportStateCoversOnlySnapshottedDefinitions(t *testing.T) {
	e := New()
	e.Register(newSnapshottedDef("carries_state", "before"))
	e.Register(&fakeDef{id: "carries_nothing", kind: "declarative"})

	raw, err := e.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	var doc engineStateDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshalling the export: %v", err)
	}
	if len(doc.Definitions) != 1 {
		t.Fatalf("exported %d definition(s): %v, want only the Snapshotted one", len(doc.Definitions), doc.Definitions)
	}
	if _, ok := doc.Definitions["carries_state"]; !ok {
		t.Errorf("expected the Snapshotted definition's state in the document, got %v", doc.Definitions)
	}
}

func TestEngineExportStateSurvivesOneDefinitionFailing(t *testing.T) {
	broken := newSnapshottedDef("broken", "")
	broken.exportErr = errors.New("cannot render its state")
	e := New()
	e.Register(broken)
	e.Register(newSnapshottedDef("healthy", "kept"))

	raw, err := e.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v -- one broken definition must not fail the whole snapshot", err)
	}
	var doc engineStateDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshalling the export: %v", err)
	}
	if _, ok := doc.Definitions["broken"]; ok {
		t.Error("expected the failing definition to be omitted")
	}
	if _, ok := doc.Definitions["healthy"]; !ok {
		t.Errorf("expected every other definition to still be exported, got %v", doc.Definitions)
	}
}

func TestEngineImportStateRoundTripsThroughTheDefinitionIDs(t *testing.T) {
	source := New()
	source.Register(newSnapshottedDef("a", "state-a"))
	source.Register(newSnapshottedDef("b", "state-b"))
	raw, err := source.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}

	restoredA, restoredB := newSnapshottedDef("a", ""), newSnapshottedDef("b", "")
	target := New()
	target.Register(restoredA)
	target.Register(restoredB)

	taken := exportStart
	now := exportStart.Add(4 * time.Minute)
	if err := target.ImportState(raw, taken, now); err != nil {
		t.Fatalf("ImportState: %v", err)
	}
	if restoredA.value != "state-a" || restoredB.value != "state-b" {
		t.Errorf("restored values = (%q, %q), want (state-a, state-b)", restoredA.value, restoredB.value)
	}
	if !restoredA.importTaken.Equal(taken) || !restoredA.importNow.Equal(now) {
		t.Errorf("definition saw taken/now = (%v, %v), want (%v, %v)", restoredA.importTaken, restoredA.importNow, taken, now)
	}
}

func TestEngineImportStateSkipsUnknownIDsAndSurvivesABadPart(t *testing.T) {
	broken := newSnapshottedDef("broken", "")
	broken.importErr = errors.New("state it cannot make sense of")
	healthy := newSnapshottedDef("healthy", "")

	e := New()
	e.Register(broken)
	e.Register(healthy)
	e.Register(&fakeDef{id: "carries_nothing", kind: "declarative"})

	// "removed_last_release" is a definition that existed when the
	// snapshot was written and does not now; "carries_nothing" is one
	// that no longer carries state. Neither is an error.
	raw := json.RawMessage(`{"definitions":{` +
		`"removed_last_release":"whatever it used to hold",` +
		`"carries_nothing":"state from a build where it did",` +
		`"broken":"unreadable",` +
		`"healthy":"restored"}}`)

	if err := e.ImportState(raw, exportStart, exportStart); err != nil {
		t.Fatalf("ImportState: %v -- an unknown ID and one failing part must not stop the rest", err)
	}
	if healthy.value != "restored" {
		t.Errorf("healthy definition value = %q, want it restored despite the other entries", healthy.value)
	}
	if broken.importCalls != 1 {
		t.Errorf("broken definition import called %d time(s), want 1 attempt", broken.importCalls)
	}
}

func TestEngineImportStateRejectsAMalformedDocument(t *testing.T) {
	e := New()
	e.Register(newSnapshottedDef("a", ""))
	if err := e.ImportState(json.RawMessage(`{"definitions":`), exportStart, exportStart); err == nil {
		t.Fatal("expected a truncated document to be an error")
	}
}

func TestEngineImportStateIsRefusedOnceEventsHaveBeenEvaluated(t *testing.T) {
	restored := newSnapshottedDef("a", "cold")
	e := New()
	e.Register(restored)

	// One event through the same path Run drives, so the engine has
	// evaluated something.
	e.evaluateEvent(evt("198.51.100.4"))

	raw := json.RawMessage(`{"definitions":{"a":"warm"}}`)
	err := e.ImportState(raw, exportStart, exportStart)
	if err == nil {
		t.Fatal("expected ImportState to be refused once evaluation has started")
	}
	if restored.importCalls != 0 {
		t.Errorf("definition import called %d time(s) after the refusal, want 0 -- a refused import must change nothing", restored.importCalls)
	}
	if restored.value != "cold" {
		t.Errorf("definition value = %q, want it untouched by the refused import", restored.value)
	}
}

func TestEngineExportImportAreNilSafe(t *testing.T) {
	// Enqueue and Tick already treat a nil *Engine as a valid no-op for
	// callers that do not want the chassis at all; the snapshot pair
	// follows that convention rather than being the one thing wiring has
	// to nil-check.
	var e *Engine
	raw, err := e.ExportState()
	if err != nil || raw != nil {
		t.Errorf("ExportState on a nil engine = (%s, %v), want (nil, nil)", raw, err)
	}
	if err := e.ImportState(json.RawMessage(`{}`), exportStart, exportStart); err != nil {
		t.Errorf("ImportState on a nil engine = %v, want nil", err)
	}
}

// TestEngineExportStateIsSafeWhileRunEvaluates is the -race proof that
// the snapshot writer can be its own goroutine, which is the whole shape
// #795 asks for: a periodic writer, not a hook in the ingest path. The
// definition under it is a real one, so the state being read while
// events are being evaluated is a real ring and not a stub's string.
func TestEngineExportStateIsSafeWhileRunEvaluates(t *testing.T) {
	fs := newTestFlagsStore(t)
	d := newShippedActivitySpikeDefinition(t, fs, nil, Scope{})
	e := New()
	e.Register(d)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)

	ingested := make(chan struct{})
	go func() {
		defer close(ingested)
		for i := 0; i < 2000; i++ {
			e.Enqueue(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 80, ConnState: "new",
				ReceivedAt: exportStart.Add(time.Duration(i) * 10 * time.Millisecond)})
		}
	}()
	for i := 0; i < 50; i++ {
		if _, err := e.ExportState(); err != nil {
			t.Fatalf("ExportState: %v", err)
		}
	}
	<-ingested

	raw, err := e.ExportState()
	if err != nil {
		t.Fatalf("ExportState: %v", err)
	}
	var doc engineStateDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshalling the export: %v", err)
	}
	if _, ok := doc.Definitions["activity_spike"]; !ok {
		t.Errorf("expected the running definition's state in the document, got %v", doc.Definitions)
	}

	cancel()
	<-e.Done()

	// Run has stopped: nothing is evaluating, so an export still works
	// -- it just does the work on the caller's own goroutine.
	if _, err := e.ExportState(); err != nil {
		t.Errorf("ExportState after Run stopped: %v", err)
	}
}

func TestEngineImportStateIsRefusedWhileRunning(t *testing.T) {
	restored := newSnapshottedDef("a", "cold")
	e := New()
	e.Register(restored)

	ctx, cancel := context.WithCancel(context.Background())
	// Joined rather than merely cancelled: a Run goroutine still
	// draining after its test returns reads package tuning vars the
	// next test is entitled to change (see withDrainTimeout).
	defer func() {
		cancel()
		<-e.Done()
	}()
	go e.Run(ctx)
	waitForRunning(t, e)

	if err := e.ImportState(json.RawMessage(`{"definitions":{"a":"warm"}}`), exportStart, exportStart); err == nil {
		t.Fatal("expected ImportState to be refused while the engine is running, even with no event evaluated yet")
	}
	if restored.value != "cold" {
		t.Errorf("definition value = %q, want it untouched by the refused import", restored.value)
	}
}
