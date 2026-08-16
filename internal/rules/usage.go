// SPDX-License-Identifier: AGPL-3.0-only

// Package rules persists a long-lived, per-rule-label usage record --
// when a firewall rule was first observed firing, when it last fired,
// and how many times -- independent of internal/store's ring buffer.
//
// internal/store/ring.go already tracks totalByRule, but purely
// in-memory and logically windowed to the store's retention period (24h
// by default, config.go's Store.Retention) -- fine for "what's hot right
// now," useless for "hasn't fired in 30+ days," which needs a record
// that outlives both a restart and the retention window. This package
// exists to give that data an unbounded-time, persisted home, using the
// same convention as every other small persisted store in this codebase
// (internal/flags, internal/auth, internal/detect's SettingsStore): a
// mutex-guarded map plus an atomic JSON write, not a database.
package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("rules")

// Usage is one rule label's lifetime record.
type Usage struct {
	Rule      string    `json:"rule"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Count     uint64    `json:"count"`
}

// Store holds every known rule label's Usage record, keyed by rule
// label, safe for concurrent use. The zero value is not usable;
// construct with Open.
type Store struct {
	mu sync.RWMutex
	// wb is nil when persistence isn't configured -- see
	// persist.WriteBehind for what it now owns: write-behind, the
	// backend deadline, the after-write-stamped rate limit/back-off, and
	// version bookkeeping (issue #400). Every method on it is a safe
	// no-op on a nil receiver.
	wb     *persist.WriteBehind
	byRule map[string]*Usage
}

// Open loads path if it exists (a missing file is the expected
// first-run case, not an error) and returns a Store that persists to it
// from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Store is returned. A
// document that exists but cannot be read or parsed is a hard error
// (issue #378): the caller gets (nil, err) rather than a store whose
// live backend would overwrite that document on the first write. See
// persist.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend
// -- a JSON file by default, or Postgres when configured (issue #131).
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{byRule: make(map[string]*Usage)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the rule-usage store", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var list []*Usage
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		for _, u := range list {
			// Same "a JSON array containing null unmarshals into a nil
			// pointer" edge case flags.Open's doc comment calls out.
			if u == nil || u.Rule == "" {
				continue
			}
			s.byRule[u.Rule] = u
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// Flush forces this store's write-behind writer to persist whatever is
// currently dirty now, without waiting out its usual debounce interval,
// and blocks until that attempt finishes or ctx expires -- see
// flags.Store.Flush's own doc comment for when this is the right call
// (a test, or a `-backup` CLI invocation racing a still-running
// process). A store with no backend configured (wb == nil) is a safe
// no-op.
func (s *Store) Flush(ctx context.Context) error {
	return s.wb.Flush(ctx)
}

// Close stops this store's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A store with no backend configured (wb == nil)
// is a safe no-op. Not safe to call any mutating method after Close.
func (s *Store) Close(ctx context.Context) error {
	return s.wb.Close(ctx)
}

// Touch records rule as having fired at now, creating a new record
// (FirstSeen = now) the first time this label is seen. A blank rule
// label is a no-op, mirroring internal/store/ring.go's own totalByRule
// bump, which likewise skips events with no rule label -- the intended
// call site is right alongside that bump (see main.go's ingestOneRecovered),
// so both stay in sync on exactly the same per-event trigger rather than
// this being a separately-derived, potentially-lagging pass over events.
func (s *Store) Touch(rule string, now time.Time) {
	if rule == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byRule[rule]
	if !ok {
		u = &Usage{Rule: rule, FirstSeen: now}
		s.byRule[rule] = u
	}
	u.LastSeen = now
	u.Count++

	s.pruneLocked()
	s.persistLocked()
}

// List returns every known rule's usage record, most-recently-fired
// first.
func (s *Store) List() []Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Usage {
	out := make([]Usage, 0, len(s.byRule))
	for _, u := range s.byRule {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Stale returns every rule whose LastSeen is older than maxAge as of
// now, sorted by rule label for a stable, deterministic sweep order.
// Used by internal/detect.StaleRuleDetector's periodic sweep (see
// main.go) to decide which rules to (re-)flag.
func (s *Store) Stale(maxAge time.Duration, now time.Time) []Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := now.Add(-maxAge)
	out := make([]Usage, 0)
	for _, u := range s.byRule {
		if u.LastSeen.Before(cutoff) {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out
}

// persistMinInterval rate-limits persistLocked's actual disk writes --
// Touch is called on every single ingested event with a rule label (the
// same rate ring.go's totalByRule bump runs at), so without this a
// sustained high event rate would mean a full JSON marshal + atomic
// rename on every event, directly on the ingest hot path. Same trade-off
// flags.persistMinInterval documents: the very latest state is only
// durably persisted once another Touch call arrives after this interval
// elapses, so state written right before a crash/kill can lose up to
// this long -- acceptable since in-memory state (which every read goes
// through) is always immediately correct regardless.
//
// A var rather than a const so tests that need every call to persist
// immediately can shrink it, same convention as flags.persistMinInterval.
// Now persist.WriteBehind's MinInterval (see OpenWithBackend) rather
// than a field this type checks itself -- the rate-limiting/back-off
// logic that used to live here, and its #377 stall-under-load defect,
// both moved to that type (issue #400).
var persistMinInterval = time.Second

// maxRuleEntries bounds byRule, which is keyed on the rule label parsed
// straight out of a syslog line -- an entirely unauthenticated input.
// Anything able to reach the syslog port chooses these keys, so without
// a cap a flood of unique labels grows this map without limit and,
// once persisted, the on-disk file with it. Mirrors the existing caps
// on device.MACRegistry (50k) and flags.Store (1000), both of which
// bound the same class of attacker-influenced key.
//
// 20k is far above any plausible real deployment: a rule label comes
// from a RouterOS log-prefix an operator configured by hand, so real
// installs have tens, not thousands. A var rather than a const so tests
// can shrink it.
var maxRuleEntries = 20_000

// pruneLocked evicts the least-recently-seen entries once the store is
// over maxRuleEntries. Oldest-LastSeen-first, matching
// device.MACRegistry.pruneLocked: under a flood of junk labels the real
// rules are the ones still being touched, so they are exactly the ones
// this keeps.
//
// Sheds a batch rather than the exact overflow, for the reason
// internal/evict documents: evicting back to exactly the cap leaves the
// map full, so every subsequent new label pays a full sort. Touch runs
// synchronously on the ingest goroutine for essentially every event
// carrying a rule label, and the label is a RouterOS log-prefix an
// attacker can vary per line. Measured on the old code: 724 ns per
// Touch on an empty store against 7,455 ns at the cap, reached with
// 20,000 unique-label lines. See #285.
func (s *Store) pruneLocked() {
	if len(s.byRule) <= maxRuleEntries {
		return
	}
	evict.DownTo(s.byRule, evict.Target(maxRuleEntries), func(u *Usage) time.Time {
		return u.LastSeen
	})
}

// persistLocked encodes the current state and hands it to the
// write-behind writer (see persist.WriteBehind), which coalesces it
// with whatever else is pending and persists it off this goroutine,
// under its own deadline and rate limit. Marshal failures are swallowed
// rather than surfaced to Touch's caller: the in-memory state stays
// correct either way, so a transient disk issue degrades to "won't
// survive a restart right now" rather than breaking live use. Must be
// called with s.mu already held -- see flags.Store.persistLocked's own
// doc comment for the "lock covers the encode, not the backend call"
// contract this mirrors.
func (s *Store) persistLocked() {
	if s.wb == nil {
		return
	}

	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding rule usage for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	s.wb.MarkDirty(data)
}
