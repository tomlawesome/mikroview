// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
)

// maxLoginLimiterKeys bounds LoginLimiter's tracked-key map the same way
// every other unbounded-growth-risk map in mikroview has an explicit
// ceiling (see internal/detect's maxTrackedSources) -- without it, an
// attacker trying many distinct usernames could grow this without bound.
var maxLoginLimiterKeys = 4096

// LoginLimiter guards against brute-force login attempts with a simple
// sliding-window counter per key (username or source IP) -- the same
// technique internal/detect's per-source windows already use throughout
// this codebase, just scoped to login attempts instead of firewall
// events.
type LoginLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	threshold int
	window    time.Duration
}

func NewLoginLimiter(threshold int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{attempts: make(map[string][]time.Time), threshold: threshold, window: window}
}

// Allow reports whether key is still under the failed-attempt
// threshold. Does not itself record an attempt.
//
// Prefer Reserve for anything guarding an expensive operation: Allow is
// a read, so check-then-act around a slow call lets a simultaneous burst
// pass the check together and consume far more than threshold attempts.
func (l *LoginLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, now)) < l.threshold
}

// Reserve atomically checks the threshold and claims one attempt against
// it, returning false if key is already at the limit.
//
// This exists because Allow-then-hash-then-RecordFailure is a
// check-then-act race with a ~100ms Argon2id computation sitting in the
// gap. Sixteen simultaneous login attempts all called Allow before any
// of them reached RecordFailure, so all sixteen were admitted against a
// threshold of five -- both a brute-force bypass and, at 64 MiB of
// working memory each, a memory-exhaustion path.
//
// Callers hold the reservation for the duration of the slow operation
// and call Release only on success, so a failure simply leaves the
// attempt counted.
func (l *LoginLimiter) Reserve(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pruneLocked(key, now)) >= l.threshold {
		return false
	}
	if _, exists := l.attempts[key]; !exists && len(l.attempts) >= maxLoginLimiterKeys {
		l.evictOldestLocked()
	}
	l.attempts[key] = append(l.attempts[key], now)
	return true
}

// Release returns one reservation taken by Reserve -- called after a
// *successful* attempt, so that legitimate logins don't accumulate
// toward the threshold. Entries are interchangeable timestamps, so
// dropping the most recent is equivalent to dropping "ours".
func (l *LoginLimiter) Release(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entries := l.pruneLocked(key, now); len(entries) > 0 {
		if len(entries) == 1 {
			delete(l.attempts, key) // same reasoning as pruneLocked: no empty entries
			return
		}
		l.attempts[key] = entries[:len(entries)-1]
	}
}

// RecordFailure records one failed attempt for key.
func (l *LoginLimiter) RecordFailure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.attempts[key]; !exists && len(l.attempts) >= maxLoginLimiterKeys {
		l.evictOldestLocked()
	}
	l.attempts[key] = append(l.pruneLocked(key, now), now)
}

// pruneLocked drops key's attempts that have aged out of the window and
// returns what remains.
//
// It must not leave an entry behind for a key with nothing left, and
// must not create one for a key it has never seen. Doing so used to make
// maxLoginLimiterKeys unenforceable on the Reserve path: Reserve calls
// this first, the old body assigned `l.attempts[key] = kept`
// unconditionally, so by the time the cap check asked `if _, exists`,
// the entry always existed and the eviction branch was dead code.
// RecordFailure asks *before* pruning, which is why the cap worked there
// and not here.
//
// Measured on the old code: 200,000 requests from varying source
// addresses left 200,006 tracked keys against a documented cap of 4,096,
// retaining 22.69 MiB -- unauthenticated, and cheap to sustain because
// this path never reaches the Argon2id cost. A single IPv4 source
// short-circuits harmlessly; a routed IPv6 /64 or a small botnet does
// not. See #285.
func (l *LoginLimiter) pruneLocked(key string, now time.Time) []time.Time {
	entries, ok := l.attempts[key]
	if !ok {
		return nil
	}
	cutoff := now.Add(-l.window)
	kept := entries[:0]
	for _, t := range entries {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		// An empty entry is not free: it is one map entry per source
		// address, held after every attempt from it has aged out.
		delete(l.attempts, key)
		return nil
	}
	l.attempts[key] = kept
	return kept
}

// evictOldestLocked sheds the least-recently-active keys once the
// limiter is at maxLoginLimiterKeys.
//
// A batch rather than one, for internal/evict's reason: the keys are
// source addresses an attacker varies per request, so evicting exactly
// one leaves the map full and makes every subsequent request pay the
// whole scan.
func (l *LoginLimiter) evictOldestLocked() {
	evict.DownTo(l.attempts, evict.Target(maxLoginLimiterKeys), func(times []time.Time) time.Time {
		if len(times) == 0 {
			return time.Time{} // shed first; pruneLocked should have removed it already
		}
		return times[len(times)-1]
	})
}
