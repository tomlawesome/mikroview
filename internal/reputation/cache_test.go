// SPDX-License-Identifier: AGPL-3.0-only

package reputation

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Lookup's cache had no test against the thing it exists for (#267
// finding 15). Its own doc comment says the cache is there "so repeat
// clicks... don't re-hit AbuseIPDB's free-tier daily quota" -- and if
// the cache silently never hit, nothing here would have noticed until
// the quota was gone. The individual fetch functions were tested; the
// guarantee built on top of them was not.
//
// countingTransport stands in for the network: no request leaves the
// process, and the count is the assertion.
type countingTransport struct {
	requests atomic.Int64
}

func (t *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.requests.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func newCountingClient(t *testing.T) (*Client, *countingTransport) {
	t.Helper()
	tr := &countingTransport{}
	c := New("")
	c.httpClient = &http.Client{Transport: tr}
	return c, tr
}

func TestLookupCacheHitSkipsTheNetwork(t *testing.T) {
	c, tr := newCountingClient(t)
	ctx := context.Background()

	if _, err := c.Lookup(ctx, "8.8.8.8"); err != nil {
		t.Fatalf("first Lookup: %v", err)
	}
	first := tr.requests.Load()
	if first == 0 {
		t.Fatal("the first lookup made no request at all -- this test is not exercising what it thinks it is")
	}

	for i := 0; i < 5; i++ {
		if _, err := c.Lookup(ctx, "8.8.8.8"); err != nil {
			t.Fatalf("cached Lookup: %v", err)
		}
	}
	if got := tr.requests.Load(); got != first {
		t.Errorf("five repeat lookups inside the TTL made %d extra request(s) -- the cache is not being hit, which is what burns the AbuseIPDB quota", got-first)
	}
}

func TestLookupRefetchesOnceTheTTLHasPassed(t *testing.T) {
	c, tr := newCountingClient(t)
	ctx := context.Background()

	if _, err := c.Lookup(ctx, "8.8.8.8"); err != nil {
		t.Fatalf("first Lookup: %v", err)
	}
	first := tr.requests.Load()

	// Age the entry rather than waiting 15 minutes. Reaching into the
	// map is fair game in the same package, and beats making cacheTTL a
	// var purely so a test can move it.
	c.mu.Lock()
	entry := c.cache["8.8.8.8"]
	entry.expires = time.Now().Add(-time.Second)
	c.cache["8.8.8.8"] = entry
	c.mu.Unlock()

	if _, err := c.Lookup(ctx, "8.8.8.8"); err != nil {
		t.Fatalf("Lookup after expiry: %v", err)
	}
	if got := tr.requests.Load(); got <= first {
		t.Errorf("an expired entry was served from cache anyway (%d requests, was %d) -- reputation data would never refresh", got, first)
	}
}

func TestLookupCacheStaysBounded(t *testing.T) {
	c, _ := newCountingClient(t)
	ctx := context.Background()

	// One under the ceiling, all unexpired, so evictExpiredLocked's TTL
	// pass removes nothing and only the size check can act.
	c.mu.Lock()
	for i := 0; i < maxCacheEntries-1; i++ {
		c.cache[randomishIP(i)] = cacheEntry{expires: time.Now().Add(cacheTTL)}
	}
	c.mu.Unlock()

	if _, err := c.Lookup(ctx, "8.8.8.8"); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	c.mu.Lock()
	size := len(c.cache)
	c.mu.Unlock()
	if size >= maxCacheEntries {
		t.Errorf("cache holds %d entries, at or past the %d ceiling -- it is not bounded", size, maxCacheEntries)
	}
}

func TestLookupRefusesNonPublicWithoutTouchingTheNetwork(t *testing.T) {
	c, tr := newCountingClient(t)
	for _, ip := range []string{"192.168.1.1", "127.0.0.1", "10.0.0.1", "not-an-ip"} {
		if _, err := c.Lookup(context.Background(), ip); err != ErrNotPublic {
			t.Errorf("Lookup(%q) returned %v, want ErrNotPublic", ip, err)
		}
	}
	if got := tr.requests.Load(); got != 0 {
		t.Errorf("a non-public address made %d outbound request(s); it should never leave the process", got)
	}
}

// randomishIP builds distinct dotted quads from an index, so the cache
// can be filled without a real address list.
func randomishIP(i int) string {
	var b strings.Builder
	b.WriteString("203.0.")
	b.WriteByte(byte('0' + (i/256)%10))
	b.WriteString(".")
	b.WriteByte(byte('0' + i%10))
	b.WriteString("-")
	b.WriteString(string(rune('a' + i%26)))
	return b.String()
}
