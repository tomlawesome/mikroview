package auth

import (
	"sync"
	"time"
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

func (l *LoginLimiter) pruneLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[key] = kept
	return kept
}

func (l *LoginLimiter) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, times := range l.attempts {
		if len(times) == 0 {
			continue
		}
		last := times[len(times)-1]
		if first || last.Before(oldest) {
			oldestKey, oldest, first = k, last, false
		}
	}
	if oldestKey != "" {
		delete(l.attempts, oldestKey)
	}
}
