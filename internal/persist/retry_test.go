// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These exercise openPoolWithRetry's own logic against a fake connector,
// not a real Postgres -- the point is the retry/give-up boundary, which
// #702's real-database repro doesn't isolate on its own. Backoff is 0 so
// the failing case doesn't spend a full second running.

func TestOpenPoolWithRetryRecoversFromAResetThenSuccess(t *testing.T) {
	// The shape from #702: a connection reset (or "database system is
	// starting up") on the first attempts, then a normal success once
	// the service finishes coming up.
	wantPool := &Pool{pool: &pgxpool.Pool{}}
	calls := 0
	connect := func() (*Pool, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("connection reset by peer")
		}
		return wantPool, nil
	}

	got, err := openPoolWithRetry(setupRetryAttempts, 0, connect)
	if err != nil {
		t.Fatalf("openPoolWithRetry: %v", err)
	}
	if got != wantPool {
		t.Errorf("returned pool = %v, want the one connect eventually produced", got)
	}
	if calls != 3 {
		t.Errorf("connect called %d times, want exactly 3 (2 failures then success)", calls)
	}
}

func TestOpenPoolWithRetryGivesUpOnRepeatedFailure(t *testing.T) {
	const attempts = 4
	calls := 0
	wantErr := errors.New("database system is starting up")
	connect := func() (*Pool, error) {
		calls++
		return nil, wantErr
	}

	_, err := openPoolWithRetry(attempts, 0, connect)
	if !errors.Is(err, wantErr) {
		t.Fatalf("openPoolWithRetry error = %v, want %v", err, wantErr)
	}
	if calls != attempts {
		t.Errorf("connect called %d times, want exactly %d -- retry must be bounded", calls, attempts)
	}
}

func TestOpenPoolWithRetrySleepsBetweenAttemptsOnly(t *testing.T) {
	// Proves the backoff is spent *between* attempts, not after the
	// last one and not before the first -- a bounded retry that still
	// pays its full backoff on the attempt that was going to fail
	// anyway wastes exactly the time a "short backoff" is meant to cap.
	const attempts = 3
	backoff := 10 * time.Millisecond
	calls := 0
	connect := func() (*Pool, error) {
		calls++
		return nil, errors.New("still starting up")
	}

	start := time.Now()
	_, err := openPoolWithRetry(attempts, backoff, connect)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("openPoolWithRetry: want an error after exhausting attempts")
	}
	wantMin := time.Duration(attempts-1) * backoff
	if elapsed < wantMin {
		t.Errorf("elapsed = %v, want at least %v (%d sleeps of %v)", elapsed, wantMin, attempts-1, backoff)
	}
}
