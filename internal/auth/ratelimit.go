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
// threshold. Does not itself record an attempt -- call RecordFailure
// after a failed login; a successful login should never call either.
func (l *LoginLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, now)) < l.threshold
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
