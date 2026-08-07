package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// TestConcurrentLoginsCannotBypassTheRateLimiter guards a fixed
// vulnerability: the limiter used to be Allow-then-hash-then-
// RecordFailure, a check-then-act race with a ~100ms Argon2id
// computation in the gap. A simultaneous burst all passed Allow before
// any failure was recorded, so every request was admitted regardless of
// the threshold.
//
// That was both a brute-force bypass (16 password guesses admitted
// against a threshold of 5) and an unauthenticated memory-exhaustion
// path: each admitted attempt reserves argon2Memory (64 MiB), so 16 of
// them imply ~1 GiB against a container documented to run under 128 MiB.
//
// Anything above the threshold reaching a 401 means it completed a full
// hash, so the count of 401s is the assertion that matters.
func TestConcurrentLoginsCannotBypassTheRateLimiter(t *testing.T) {
	s, _ := newTestServer(t)
	const threshold = 5
	s.LoginLimiter = auth.NewLoginLimiter(threshold, time.Minute)
	authStore, err := auth.Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.Register("admin", "correct-horse-battery-staple", time.Now()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	s.Auth = authStore
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	const attempts = 16
	var hashed, limited atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // fire simultaneously
			b, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(csrfHeaderName, csrfHeaderValue)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				hashed.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	t.Logf("of %d simultaneous attempts: %d reached Argon2 (401), %d were rate-limited (429)",
		attempts, hashed.Load(), limited.Load())
	t.Logf("peak memory implied by concurrent hashing: ~%d MiB", hashed.Load()*64)

	if hashed.Load() > threshold {
		t.Errorf("%d requests reached Argon2id despite a threshold of %d -- the limiter is bypassable by concurrency",
			hashed.Load(), threshold)
	}
}
