// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// RetainedDays is the on-disk half of a corpus: the encrypted daily
// files internal/retention writes (#856).
//
// An interface rather than the concrete type so this package keeps
// knowing nothing about keys, files or encryption -- it asks for days
// and gets events. main wires the real implementation; a test wires a
// slice.
type RetainedDays interface {
	// Days lists the days held, oldest first.
	Days() ([]string, error)
	// ReplayDay visits one day's events oldest first, skipping any at or
	// after cutoff, and reports how many it visited.
	ReplayDay(day string, cutoff time.Time, visit func(store.Event)) (int, error)
}

// RetainedCorpus is Corpus reading disk first, then memory.
//
// This is the "retention-backed one later must be a new implementation,
// not a caller rewrite" that corpus.go's doc comment and issue #403
// reserved space for: no Replay call site changes, and the choice
// between this and MemoryCorpus is made at the one construction site.
//
// The two halves overlap by design -- every event on disk was in the
// ring first, and for as long as the ring holds it both copies exist --
// so this type reads the ring first to learn the oldest instant memory
// still covers, then reads the files strictly before that. No event is
// visited twice and there is no gap at the seam.
type RetainedCorpus struct {
	ring     *MemoryCorpus
	retained RetainedDays
}

// NewRetainedCorpus constructs a Corpus over both halves.
func NewRetainedCorpus(s *store.Store, retained RetainedDays) *RetainedCorpus {
	return &RetainedCorpus{ring: NewMemoryCorpus(s), retained: retained}
}

// Replay satisfies Corpus.
//
// Order of work, and why: the ring is read first even though its events
// are visited last, because the oldest instant it holds is the cutoff
// the disk pass needs. The ring is also the half that can be
// invalidated by concurrent ingest, so reading it first keeps the
// window it reports as close to the caller's own "now" as possible.
//
// The disk half is bounded by the same maxCorpusEvents ceiling
// MemoryCorpus obeys, and the bound is spent on the *newest* days
// rather than the oldest. That direction matters: truncating from the
// newest end would leave a hole between the last day read and the ring,
// and this pass would then report a continuous window it does not have
// -- the one thing a receipt must never do. Dropping the oldest days
// instead shortens the window honestly, and Truncated says so.
func (c *RetainedCorpus) Replay(visit func(store.Event)) CorpusWindow {
	ringEvents := make([]store.Event, 0, 1024)
	ringWindow := c.ring.Replay(func(e store.Event) {
		ringEvents = append(ringEvents, e)
	})

	// The cutoff is the oldest instant memory still covers. An empty
	// ring leaves it zero, which the disk pass reads as "no cutoff":
	// nothing in memory means nothing to overlap with.
	cutoff := ringWindow.Start

	out := CorpusWindow{Truncated: ringWindow.Truncated}
	budget := maxCorpusEvents - len(ringEvents)
	if budget < 0 {
		budget = 0
	}

	byDay, diskTruncated := c.readNewestDaysThatFit(cutoff, budget)
	out.Truncated = out.Truncated || diskTruncated

	for _, day := range byDay {
		for _, e := range day {
			if out.Start.IsZero() || e.ReceivedAt.Before(out.Start) {
				out.Start = e.ReceivedAt
			}
			if e.ReceivedAt.After(out.End) {
				out.End = e.ReceivedAt
			}
			out.Count++
			visit(e)
		}
	}
	for _, e := range ringEvents {
		if out.Start.IsZero() || e.ReceivedAt.Before(out.Start) {
			out.Start = e.ReceivedAt
		}
		if e.ReceivedAt.After(out.End) {
			out.End = e.ReceivedAt
		}
		out.Count++
		visit(e)
	}
	return out
}

// readNewestDaysThatFit walks the retained days from newest to oldest,
// collecting whole days until the budget runs out, and returns them in
// oldest-first order.
//
// A day that would overflow the budget contributes its newest events
// only, for the same reason the walk starts at the newest day: what is
// dropped has to be at the far end from the ring, never between.
//
// A day that fails to read stops the walk rather than being skipped
// over. Skipping it would put a hole in the middle of the window --
// events either side of a day nobody could open -- and report the
// result as continuous. Stopping makes the window shorter and true,
// which is the honest of the two, and Truncated marks it.
func (c *RetainedCorpus) readNewestDaysThatFit(cutoff time.Time, budget int) ([][]store.Event, bool) {
	if c.retained == nil {
		return nil, false
	}
	days, err := c.retained.Days()
	if err != nil {
		logger.Warn("could not list the retained history", "err", err)
		return nil, true
	}
	// Nothing on disk cannot have been truncated, and the days are
	// asked for before the budget is checked so it can say so. This
	// matters since #910: retention can now be turned on and off while
	// the process runs, so the corpus is always constructed over a
	// retained half whether or not one is currently open, and "the
	// budget ran out" would otherwise report a history that was dropped
	// from a disk holding nothing at all.
	if len(days) == 0 {
		return nil, false
	}
	if budget <= 0 {
		return nil, true
	}

	var collected [][]store.Event
	truncated := false
	for i := len(days) - 1; i >= 0; i-- {
		if budget <= 0 {
			truncated = true
			break
		}
		var events []store.Event
		if _, err := c.retained.ReplayDay(days[i], cutoff, func(e store.Event) {
			events = append(events, e)
		}); err != nil {
			// Reported, not silent: this is the wrong key, or a file
			// somebody has altered. See retention.replayFile.
			logger.Warn("stopped reading the retained history", "day", days[i], "err", err)
			truncated = true
			break
		}
		if len(events) == 0 {
			continue
		}
		if len(events) > budget {
			events = events[len(events)-budget:]
			truncated = true
		}
		budget -= len(events)
		collected = append(collected, events)
	}

	// Collected newest-first; the caller visits oldest-first.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, truncated
}
