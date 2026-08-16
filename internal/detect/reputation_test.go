// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeReputation is a reputationLookup that blocks each call until the
// test releases it, and reports every IP it was asked to look up on
// started -- lets tests deterministically observe "a lookup was
// attempted" and control exactly when it resolves, instead of racing a
// real goroutine with sleeps.
type fakeReputation struct {
	started chan string
	release chan struct{}

	mu     sync.Mutex
	scores map[string]int
	fail   map[string]bool
}

func newFakeReputation() *fakeReputation {
	return &fakeReputation{
		started: make(chan string, 100),
		release: make(chan struct{}),
		scores:  make(map[string]int),
		fail:    make(map[string]bool),
	}
}

func (f *fakeReputation) setScore(ip string, score int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scores[ip] = score
}

func (f *fakeReputation) Lookup(ctx context.Context, ip string) (reputation.Result, error) {
	f.started <- ip
	select {
	case <-f.release:
	case <-ctx.Done():
		return reputation.Result{}, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[ip] {
		return reputation.Result{}, errors.New("fake failure")
	}
	score, ok := f.scores[ip]
	if !ok {
		return reputation.Result{IP: ip}, nil
	}
	return reputation.Result{IP: ip, AbuseScore: &score}, nil
}

func expectStarted(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case ip := <-ch:
		return ip
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a reputation lookup to start")
		return ""
	}
}

func expectNoneStarted(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case ip := <-ch:
		t.Fatalf("expected no reputation lookup, but one started for %s", ip)
	case <-time.After(100 * time.Millisecond):
	}
}

func waitForConfidence(t *testing.T, fs *flags.Store, target string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range fs.List() {
			if f.Target == target && f.Confidence != nil && *f.Confidence == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to reach confidence %d, got %+v", target, want, fs.List())
}

// The four tests below (renamed from their original TestCriticalPort*/
// TestReputation* names naming critical_port) used critical_port purely
// as a convenient, cheap-to-trigger flag-raiser for a public source IP --
// none of them are actually about critical_port's own behaviour, just
// about maybeCheckReputation's async lookup/floor-application path. Now
// that critical_port has moved to internal/engine as a shipped
// declarative definition (issue #405) and internal/detect no longer
// evaluates it at all, they are retargeted onto repeated_drops instead,
// which internal/detect still evaluates, calls maybeCheckReputation from
// observeRepeatedDrops with the source IP, and fires off a small threshold
// of dropped attempts against the same (SrcIP, DstPort) pair. Unlike
// critical_port's plain-IP Target, repeated_drops' Target is the composite
// "<ip> -> port <N>" string (see dropPairKey/observeRepeatedDrops), so
// every assertion keyed on the bare IP below is keyed on that composite
// string instead.

func TestRepeatedDropsAppliesReputationFloor(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3

	fake := newFakeReputation()
	fake.setScore("198.51.100.4", 85)
	d, fs := newTestDetector(t, cfg)
	d.WithReputation(fake)

	target := "198.51.100.4 -> port 8080"
	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Second)})
	}

	if ip := expectStarted(t, fake.started); ip != "198.51.100.4" {
		t.Fatalf("expected a lookup for 198.51.100.4, got %s", ip)
	}
	close(fake.release)
	waitForConfidence(t, fs, target, 85)

	for _, f := range fs.List() {
		if f.Target == target {
			if f.Reputation == nil || f.Reputation.AbuseScore == nil || *f.Reputation.AbuseScore != 85 {
				t.Errorf("expected the reputation snapshot to be stored on the flag, got %+v", f.Reputation)
			}
		}
	}
}

func TestReputationSnapshotCapturedWithoutAbuseScore(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3

	// Fake left with no score set for this IP -- simulates a Shodan-only
	// result (no AbuseIPDB key configured).
	fake := newFakeReputation()
	d, fs := newTestDetector(t, cfg)
	d.WithReputation(fake)

	target := "198.51.100.4 -> port 8080"
	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Second)})
	}
	expectStarted(t, fake.started)
	close(fake.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range fs.List() {
			if f.Target == target && f.Reputation != nil {
				if f.Confidence == nil || *f.Confidence != overshootConfidence(3, cfg.RepeatedDropsThreshold) {
					t.Errorf("expected confidence to stay behavior-only without an AbuseScore, got %+v", f.Confidence)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the reputation snapshot to be stored")
}

func TestRepeatedDropsReputationLookupOnlyFiresOnNewEpisode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3

	fake := newFakeReputation()
	fake.setScore("198.51.100.4", 50)
	d, fs := newTestDetector(t, cfg)
	d.WithReputation(fake)

	target := "198.51.100.4 -> port 8080"
	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Second)})
	}
	expectStarted(t, fake.started)
	close(fake.release)
	waitForConfidence(t, fs, target, 50)

	// A re-fire of the same still-active flag must not trigger a second lookup.
	d.Observe(store.Event{SrcIP: "198.51.100.4", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(10 * time.Second)})
	expectNoneStarted(t, fake.started)
}

// TestReputationSkippedForInternalSource used to drive port_scan, which
// no longer exists in this package (issue #405 moved it to
// internal/engine) -- as written it passed vacuously, exercising nothing.
// Retargeted onto repeated_drops with a private source IP so it actually
// exercises maybeCheckReputation's isPublic gate again.
func TestReputationSkippedForInternalSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 3

	fake := newFakeReputation()
	d, _ := newTestDetector(t, cfg)
	d.WithReputation(fake)

	now := time.Now()
	for i := 0; i < 3; i++ {
		d.Observe(store.Event{SrcIP: "192.168.1.50", DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Millisecond)})
	}
	expectNoneStarted(t, fake.started)
}

func TestRepeatedDropsPoolSaturationSkipsExcessLookups(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RepeatedDropsThreshold = 1
	cfg.DistributedBruteForceThreshold = 1000 // keep the group path quiet so it doesn't also compete for pool slots here

	fake := newFakeReputation()
	d, _ := newTestDetector(t, cfg)
	d.WithReputation(fake)

	now := time.Now()
	for i := 1; i <= reputationLookupConcurrency+1; i++ {
		ip := fakeExternalIP(i)
		d.Observe(store.Event{SrcIP: ip, DstIP: "192.168.1.1", DstPort: 8080, Action: store.ActionDrop, ReceivedAt: now.Add(time.Duration(i) * time.Second)})
	}

	// Exactly reputationLookupConcurrency lookups should start and then
	// block (nothing releases them yet).
	seen := make(map[string]bool)
	for i := 0; i < reputationLookupConcurrency; i++ {
		seen[expectStarted(t, fake.started)] = true
	}
	if len(seen) != reputationLookupConcurrency {
		t.Fatalf("expected %d distinct lookups, got %d: %v", reputationLookupConcurrency, len(seen), seen)
	}

	// The pool is now saturated -- the 9th episode's lookup must be skipped.
	expectNoneStarted(t, fake.started)

	close(fake.release)
}

func TestGroupReputationSamplesAreCapped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DistributedBruteForceThreshold = 15
	cfg.CriticalPorts = []int{22}
	cfg.CriticalPortThreshold = 1000
	cfg.PortScanThreshold = 1000
	cfg.ActivitySpikeThreshold = 1000

	fake := newFakeReputation()
	d, fs := newTestDetector(t, cfg)
	d.WithReputation(fake)

	// Every possible member gets the same score, so it doesn't matter
	// which members Go's randomized map iteration happens to pick, or
	// how many of them the shared lookup pool actually has room for.
	now := time.Now()
	for i := 1; i <= 15; i++ {
		ip := fakeExternalIP(i)
		fake.setScore(ip, 80)
		d.Observe(evt(ip, 22, now.Add(time.Duration(i)*time.Millisecond)))
	}

	// The group's sampling loop is synchronous and doesn't retry a
	// member skipped for a saturated pool, so from an otherwise-idle
	// pool it reaches min(reputationGroupSampleSize,
	// reputationLookupConcurrency) real lookups, not the full sample
	// cap -- the remaining sampled-but-skipped members are recorded as
	// no-data rather than retried.
	wantStarted := reputationGroupSampleSize
	if reputationLookupConcurrency < wantStarted {
		wantStarted = reputationLookupConcurrency
	}
	seen := make(map[string]bool)
	for i := 0; i < wantStarted; i++ {
		seen[expectStarted(t, fake.started)] = true
	}
	if len(seen) != wantStarted {
		t.Fatalf("expected exactly %d distinct lookups, got %d", wantStarted, len(seen))
	}
	expectNoneStarted(t, fake.started) // the group has 15 members -- must never exceed the pool

	close(fake.release)
	wantConfidence := int(math.Round(80 * (float64(wantStarted) / float64(reputationGroupSampleSize))))
	waitForConfidence(t, fs, "port 22", wantConfidence)
}

// fakeExternalIP builds a distinct public IP in the TEST-NET-3 range
// for group-reputation tests that need many distinct source addresses.
func fakeExternalIP(n int) string {
	return fmt.Sprintf("203.0.113.%d", n)
}

func TestGroupReputationCollectorRequiresMinimumSignificantSamples(t *testing.T) {
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 20, time.Now())

	c := &groupReputationCollector{pending: 5, t: flags.TypeDistributedBruteForce, target: "port 22", fs: fs}
	s1, s2 := 90, 95
	c.recordAndMaybeApply(&s1)
	c.recordAndMaybeApply(&s2)
	c.recordAndMaybeApply(nil)
	c.recordAndMaybeApply(nil)
	c.recordAndMaybeApply(nil)

	if got := *fs.List()[0].Confidence; got != 20 {
		t.Errorf("expected no floor applied below the significance threshold (2 < %d), got %d", reputationGroupMinSignificantSamples, got)
	}
}

func TestGroupReputationCollectorDiscountsThinSamples(t *testing.T) {
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 5, time.Now())

	c := &groupReputationCollector{pending: reputationGroupSampleSize, t: flags.TypeDistributedBruteForce, target: "port 22", fs: fs}
	s := 100
	for i := 0; i < reputationGroupMinSignificantSamples; i++ {
		c.recordAndMaybeApply(&s)
	}
	for i := reputationGroupMinSignificantSamples; i < reputationGroupSampleSize; i++ {
		c.recordAndMaybeApply(nil)
	}

	want := int(math.Round(100 * (float64(reputationGroupMinSignificantSamples) / float64(reputationGroupSampleSize))))
	if got := *fs.List()[0].Confidence; got != want {
		t.Errorf("expected a discounted floor of %d for a thin sample, got %d", want, got)
	}
}

func TestGroupReputationCollectorFullSampleAppliesRawMean(t *testing.T) {
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.AddWithConfidence(flags.TypeDistributedBruteForce, "port 22", "detail", 5, time.Now())

	c := &groupReputationCollector{pending: reputationGroupSampleSize, t: flags.TypeDistributedBruteForce, target: "port 22", fs: fs}
	s := 80
	for i := 0; i < reputationGroupSampleSize; i++ {
		c.recordAndMaybeApply(&s)
	}

	if got := *fs.List()[0].Confidence; got != 80 {
		t.Errorf("expected a full, confident sample to apply the raw mean, got %d", got)
	}
}
