// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/store"
)

// replayCorpus builds the corpus a replay reads.
//
// This is the one construction site issue #403 reserved: whether a
// replay reads memory alone or disk-then-memory is decided here and
// nowhere else, and every Replay call site is unchanged either way. Nil
// History is the ordinary memory-only deployment, not a fault.
//
// The retained half is wrapped rather than handed over raw, so every
// event coming back off disk is named as it would be named now -- see
// restampedDays.
func (s *Server) replayCorpus() engine.Corpus {
	if s.History == nil {
		return engine.NewMemoryCorpus(s.Store)
	}
	return engine.NewRetainedCorpus(s.Store, restampedDays{inner: s.History, names: s.Naming})
}

// restampedDays is engine.RetainedDays with the friendly-name fields
// re-resolved as the events come back off disk (#996).
//
// Retention writes each event exactly as ingest stamped it, so a day
// file holds the names that applied on the day it was written. A rename
// made afterwards was invisible to everything reading those events back
// -- the same staleness #993 fixed for the ring, on the surface #993
// left behind: a replay served from disk, and the restart that has only
// disk left to read from.
//
// The files themselves are left as written. They are sealed day files
// whose value is that they are what was received, and rewriting them to
// correct a display label would be both expensive (rewrite every frame
// of every retained day per rename) and a worse answer.
//
// So the re-stamp happens on the read path, once per event per load,
// never per render -- and through the same resolver ingest and
// restampBufferedNames use, so RouterOS-wins precedence (#186) and the
// config fallback decide what is shown, exactly as they would for an
// event arriving now. That is deliberately not the resolve-at-query
// overlay #993's decision rejects: nothing standing is kept, nothing is
// consulted at render, and the rewrite dies with the pass that made it.
type restampedDays struct {
	inner engine.RetainedDays
	names naming.Resolver
}

// Days passes straight through: which days are held is a fact about the
// directory, and no name appears in it.
func (d restampedDays) Days() ([]string, error) { return d.inner.Days() }

// ReplayDay re-stamps each event on its way to visit. The copy handed
// on is the visitor's own -- ReplayDay is given events by value -- so
// nothing on disk, and nothing any other reader holds, is touched.
func (d restampedDays) ReplayDay(day string, cutoff time.Time, visit func(store.Event)) (int, error) {
	return d.inner.ReplayDay(day, cutoff, func(e store.Event) {
		restampNames(d.names, &e)
		visit(e)
	})
}

// restampNames re-resolves every display name on one event from the
// identity fields beside it.
//
// Every field, not just the one an operator last edited: an event off
// disk may predate any number of renames, and there is no key here to
// narrow it by the way restampBufferedNames has one. Resolution is the
// same set of lookups ingest performs (main.go's ingest), so a re-stamp
// and a freshly arriving event agree.
//
// Display fields only. Identity, ordering and counting fields are what
// every reader of a replay counts from, and a name is not one of them
// -- the same contract store.Restamp keeps for the ring.
func restampNames(names naming.Resolver, e *store.Event) {
	e.SrcHostName = names.Host(e.DeviceID, e.SrcIP)
	e.DstHostName = names.Host(e.DeviceID, e.DstIP)
	e.SrcPortName = names.Port(e.SrcPort)
	e.DstPortName = names.Port(e.DstPort)
	e.RuleName = names.Rule(e.RuleLabel)
}
