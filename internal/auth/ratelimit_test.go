// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterAllowsUnderThreshold(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute)
	now := time.Now()

	for i := 0; i < 2; i++ {
		if !l.Allow("alice", now) {
			t.Fatalf("expected attempt %d to be allowed", i+1)
		}
		l.RecordFailure("alice", now)
	}
	if !l.Allow("alice", now) {
		t.Error("expected the 3rd attempt to still be allowed (threshold not yet reached)")
	}
}

func TestLoginLimiterBlocksAtThreshold(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute)
	now := time.Now()

	for i := 0; i < 3; i++ {
		l.RecordFailure("alice", now)
	}
	if l.Allow("alice", now) {
		t.Error("expected the 4th attempt to be blocked after 3 recorded failures")
	}
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	l := NewLoginLimiter(2, time.Minute)
	now := time.Now()

	l.RecordFailure("alice", now)
	l.RecordFailure("alice", now)
	if l.Allow("alice", now) {
		t.Fatal("expected the limit to be reached")
	}

	if !l.Allow("alice", now.Add(2*time.Minute)) {
		t.Error("expected attempts outside the window to no longer count")
	}
}

func TestLoginLimiterKeysAreIndependent(t *testing.T) {
	l := NewLoginLimiter(1, time.Minute)
	now := time.Now()

	l.RecordFailure("alice", now)
	if l.Allow("alice", now) {
		t.Fatal("expected alice to be blocked")
	}
	if !l.Allow("bob", now) {
		t.Error("expected bob to be unaffected by alice's failures")
	}
}

func TestLoginLimiterEvictsOldestKeyOverCap(t *testing.T) {
	orig := maxLoginLimiterKeys
	maxLoginLimiterKeys = 2
	defer func() { maxLoginLimiterKeys = orig }()

	l := NewLoginLimiter(10, time.Hour)
	now := time.Now()

	l.RecordFailure("alice", now)
	l.RecordFailure("bob", now.Add(time.Second))
	l.RecordFailure("carol", now.Add(2*time.Second)) // should evict alice (oldest)

	if len(l.attempts) != 2 {
		t.Fatalf("expected the tracked-key map to stay capped at 2, got %d", len(l.attempts))
	}
	if _, stillTracked := l.attempts["alice"]; stillTracked {
		t.Error("expected alice (the oldest) to have been evicted")
	}
}
