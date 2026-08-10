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
	mu      sync.RWMutex
	backend persist.Backend
	// version is the backend's token for the document as of the last
	// load or save -- see persist.SaveWithRetry.
	version int64
	byRule  map[string]*Usage

	// lastPersist backs persistLocked's rate limiting -- see
	// persistMinInterval.
	lastPersist time.Time
}

// Open loads path if it exists (a missing file is the expected
// first-run case, not an error) and returns a Store that persists to it
// from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Store is returned. A
// malformed file is treated as empty rather than failing -- a corrupted
// rule-usage file should never block mikroview from starting, since
// this is a helper signal, not critical state. Either way the returned
// Store is always safe to use unconditionally; a non-nil error is only
// ever informational, for the caller to log. Mirrors flags.Open's
// contract exactly.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend
// -- a JSON file by default, or Postgres when configured (issue #131).
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{backend: b, byRule: make(map[string]*Usage)}

	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	data, version, err := persist.LoadDocument(ctx, b)
	if err != nil {
		return s, err
	}
	if data == nil {
		return s, nil
	}
	s.version = version

	var list []*Usage
	if err := json.Unmarshal(data, &list); err != nil {
		return s, err
	}
	for _, u := range list {
		// Same "a JSON array containing null unmarshals into a nil
		// pointer" edge case flags.Open's doc comment calls out --
		// skipping it here is what actually delivers the "malformed
		// file treated as empty" contract for that specific case.
		if u == nil || u.Rule == "" {
			continue
		}
		s.byRule[u.Rule] = u
	}
	return s, nil
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
var persistMinInterval = time.Second

// persistTimeout bounds every Load/Save against backend. Touch runs
// synchronously on the single ingest goroutine (see main.go's
// ingestOneRecovered), so an unresponsive backend -- a Postgres
// connection stuck behind a network blackhole or a long lock wait, not
// a clean disconnect -- would otherwise block that goroutine forever
// under context.Background(), freezing the whole ingest pipeline until
// the syslog listener's buffered channel fills and starts silently
// dropping packets (internal/syslog/tcp_listener.go). 5s is generous
// for a write this small: long enough that ordinary latency never trips
// it, short enough that a genuinely stuck backend degrades to a logged
// failure (see persistLocked) rather than an indefinite hang.
const persistTimeout = 5 * time.Second

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
func (s *Store) pruneLocked() {
	over := len(s.byRule) - maxRuleEntries
	if over <= 0 {
		return
	}

	all := make([]*Usage, 0, len(s.byRule))
	for _, u := range s.byRule {
		all = append(all, u)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LastSeen.Before(all[j].LastSeen) })

	for i := 0; i < over && i < len(all); i++ {
		delete(s.byRule, all[i].Rule)
	}
}

// persistLocked writes the current state to disk if persistence is
// configured and enough time has passed since the last write. Write
// failures are swallowed rather than surfaced to Touch's caller: the
// in-memory state stays correct either way, so a transient disk issue
// degrades to "won't survive a restart right now" rather than breaking
// live use. Mirrors flags.Store.persistLocked exactly.
func (s *Store) persistLocked() {
	if s.backend == nil {
		return
	}
	if now := time.Now(); now.Sub(s.lastPersist) < persistMinInterval {
		return
	} else {
		s.lastPersist = now
	}

	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding rule usage for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	version, conflicted, err := persist.SaveWithRetry(ctx, s.backend, data, s.version)
	if err != nil {
		persistLog.Error(fmt.Sprintf("writing rule usage to %s failed: %v -- this change exists only in memory and will be lost on restart", s.backend.Describe(), err))
		return
	}
	if conflicted {
		persistLog.Warn(fmt.Sprintf("the rule usage store was modified by another process while this change was pending (%s); this change was applied on top", s.backend.Describe()))
	}
	s.version = version
}
