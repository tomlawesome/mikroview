// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"encoding/json"
	"fmt"
	"time"
)

// This file is the chassis half of warm restart (#795): one document for
// the whole engine, assembled from and routed back to whichever
// registered definitions implement Snapshotted.
//
// The engine owns the definition set (Engine.defs, keyed by definition
// ID), so the aggregate lives here rather than on Registry, which only
// builds definitions and hands them over. Routing is by definition ID
// and nothing else: the chassis never interprets a definition's bytes,
// which is what lets a definition change what it carries without the
// chassis, the snapshot writer or the document's shape changing with it.
//
// # Both methods are callable from another goroutine, by different means
//
// A definition's windows are single-writer state (see
// Engine.runOnEvaluationGoroutine for why, and why locking them instead
// is not on the table). Export and import each need that exclusivity,
// and they get it differently because they happen at different moments:
//
//   - ExportState runs every few minutes for the life of the process, so
//     it borrows the evaluation goroutine for the duration of one export
//     -- the caller's own goroutine blocks, the ingest queue absorbs
//     what arrives meanwhile, and nothing on the per-event path takes a
//     new lock.
//   - ImportState runs once, at boot, before evaluation starts. There is
//     nothing to borrow yet, so it does the work inline and refuses
//     outright if evaluation has already begun.

// engineStateDocument is the engine's snapshot shape: per-definition
// opaque state, keyed by definition ID. Deliberately the same
// {"definitions": {...}} shape stateDocument already uses (state.go), so
// an operator looking at either file recognises the other -- and, like
// that one, it carries no version field: an unknown key decodes away and
// a definition that no longer exists is skipped by name, which is all
// the forward growth this document has needed (see stateDocument's own
// comment and #873).
type engineStateDocument struct {
	Definitions map[string]json.RawMessage `json:"definitions"`
}

// ExportState renders every registered Snapshotted definition's state
// into one document, for the periodic snapshot #795 writes. Definitions
// that do not implement Snapshotted contribute nothing and are not
// listed.
//
// Safe to call from the snapshot writer's own goroutine while evaluation
// is running -- see this file's doc comment for how. It blocks until the
// evaluation goroutine has finished the export, so a caller on a ticker
// should treat it as it would any other few-milliseconds-of-work call
// and not, say, run it from the ingest path.
//
// A definition whose export fails is logged and omitted rather than
// failing the whole snapshot: one broken definition must not cost every
// other definition its warm restart. The returned error is reserved for
// a failure to encode the document itself.
//
// A nil *Engine is a valid no-op, the same convention Enqueue and Tick
// use, so wiring that runs without the chassis needs no nil check.
func (e *Engine) ExportState() (json.RawMessage, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	regs := append([]*registration(nil), e.order...)
	doc := engineStateDocument{Definitions: make(map[string]json.RawMessage, len(regs))}
	e.runOnEvaluationGoroutine(func() {
		for _, r := range regs {
			s, ok := r.def.(Snapshotted)
			if !ok {
				continue
			}
			raw, err := s.ExportState()
			if err != nil {
				logger.Error(fmt.Sprintf("definition %q could not export its state for the snapshot: %v -- it will start cold after a restart, every other definition is unaffected", r.def.ID(), err))
				continue
			}
			doc.Definitions[r.def.ID()] = raw
		}
	})
	e.mu.Unlock()
	return json.Marshal(doc)
}

// ImportState routes each entry in raw to the registered definition of
// that ID, as state taken at taken being restored into an engine
// evaluating as of now.
//
// # Call it before the ingest loop starts
//
// A definition's imported state replaces the windows its own Evaluate
// owns as a single writer (see Evaluated.Evaluate and Keyed.GetOrCreate).
// Importing while events are being evaluated would discard whatever the
// evaluation goroutine had accumulated for a restored key and hand it a
// value it did not create, mid-decision. Wiring must call this after
// Register and before Run.
//
// That requirement is enforced rather than merely documented: an import
// is refused with an error, changing nothing, once Run is driving the
// engine or once any event has been evaluated. The refusal is the honest
// outcome -- a half-restored engine is harder to reason about than one
// that stayed cold and said so in a log line.
//
// # What is skipped rather than fatal
//
// An entry whose definition ID is not registered is skipped: a
// definition deleted or renamed since the snapshot was written is
// expected, not an error. So is an entry for a definition that no longer
// implements Snapshotted. A definition whose own import fails is logged,
// one line each, and the rest still import -- the same
// one-broken-part-does-not-cost-the-others rule ExportState follows. The
// returned error is reserved for a document that cannot be parsed at all
// and for the too-late refusal above.
func (e *Engine) ImportState(raw json.RawMessage, taken, now time.Time) error {
	if e == nil {
		return nil
	}
	// Held for the whole import: Run takes mu before its first event
	// (see setRunning), so this is what makes "nothing else is touching
	// definition state" true for the duration rather than at the instant
	// it was checked.
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine: ImportState called while the engine is running -- restoring state must happen before the ingest loop starts, so this snapshot was not applied")
	}
	if n := e.evaluatedEvents.Load(); n > 0 {
		return fmt.Errorf("engine: ImportState called after %d event(s) were already evaluated -- restoring state must happen before the ingest loop starts, so this snapshot was not applied", n)
	}
	var doc engineStateDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("engine: ImportState: %w", err)
	}
	if taken.After(now) {
		// Not fatal here: every timestamp inside the document is judged
		// on its own (a ring bucket stamped after now is dropped by the
		// ring itself), so a snapshot from a host whose clock has since
		// gone backwards restores what is still defensible. Said out
		// loud because it is otherwise invisible.
		logger.Warn(fmt.Sprintf("engine state snapshot is stamped %s in the future (taken %s, now %s) -- restoring only what is still inside each window",
			taken.Sub(now), taken.Format(time.RFC3339), now.Format(time.RFC3339)))
	}

	for id, state := range doc.Definitions {
		r, registered := e.defs[id]
		if !registered {
			logger.Info(fmt.Sprintf("engine state snapshot holds state for definition %q, which is not registered -- skipping it", id))
			continue
		}
		s, ok := r.def.(Snapshotted)
		if !ok {
			logger.Info(fmt.Sprintf("engine state snapshot holds state for definition %q, which no longer carries state across a restart -- skipping it", id))
			continue
		}
		if err := s.ImportState(state, taken, now); err != nil {
			logger.Warn(fmt.Sprintf("definition %q could not restore its state from the snapshot: %v -- it starts cold, every other definition is unaffected", id, err))
		}
	}
	return nil
}
