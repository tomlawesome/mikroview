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

// The tests below (renamed from their original TestCriticalPort*/
// TestReputation* names naming critical_port) used critical_port, then
// repeated_drops, then activity_spike, purely as a convenient,
// cheap-to-trigger flag-raiser for a public source IP -- none of them are
// actually about any one detector's own behaviour, just about
// maybeCheckReputation's async lookup/floor-application path. Now that
// activity_spike has also moved to internal/engine as a shipped
// programmatic definition (issue #405) and internal/detect no longer
// evaluates it at all, they are retargeted onto low_slow_scan instead --
// the only other detector still evaluated by internal/detect whose
// Observe path calls the single-IP maybeCheckReputation (not the group
// variant dest_spread.go's outbound_anomaly uses) with a plain source IP
// as both target and ip (see low_slow_scan.go's observeLowSlowScan). Its
// own test file's lowSlowCfg()/lowSlowEvt()/feedPacedScan() helpers
// (low_slow_scan_test.go, same package) already give a small,
// deterministic recipe -- 8 paced events, 5 minutes apart, each a
// distinct (port, host) pair with store.ActionDrop -- that clears every
// one of low_slow_scan's axes (breadth, drop ratio, observation window,
// baseline) at once, the same role activity_spike's six-events-one-
// second-apart recipe played before it.
//
// Two of the original four no longer need a home here at all:
// TestRepeatedDropsAppliesReputationFloor's engine counterpart is
// internal/engine/reputation_sink_test.go's
// TestReputationSinkLooksUpTheSourceNotTheCompositeTarget, and
// TestReputationSkippedForInternalSource's is
// TestReputationSinkSkippedForAnInternalSourceBehindACompositeTarget --
// both deleted here rather than retargeted a third time.

// TestLowSlowScanReputationSnapshotCapturedWithoutAbuseScore,
// TestLowSlowScanReputationLookupOnlyFiresOnNewEpisode and
// TestLowSlowScanPoolSaturationSkipsExcessLookups all moved to
// internal/engine (issue #405: low_slow_scan is now a shipped
// programmatic definition, and the single-address reputation path it
// drove is engine.ReputationSink). Their pins live on in
// internal/engine/reputation_sink_test.go's
// TestReputationSinkSnapshotCapturedWithoutAbuseScore,
// TestReputationSinkOnlyLooksUpOnANewEpisode and
// TestReputationSinkPoolSaturationSkipsExcessLookups -- pinned against
// the sink itself rather than against whichever detector happened to
// drive it, which is what stops the same three guarantees needing a
// fourth retargeting every time a detector moves. The engine also pins
// the lookup actually being requested for a low_slow_scan episode
// specifically: see
// TestShippedLowSlowScanTriggersReputationLookup.

// TestOutboundAnomalyGroupReputationSamplesAreCapped is
// TestGroupReputationSamplesAreCapped, retargeted onto outbound_anomaly
// now that distributed_brute_force has also moved to internal/engine as
// a shipped declarative definition (issue #405) and internal/detect no
// longer evaluates it at all -- see internal/engine/reputation_sink_test
// .go's TestGroupReputationSinkAppliesTheDiscountedMeanFloor and
// TestGroupReputationSinkStaysSilentBelowTheSignificanceFloor for that
// side's coverage of the discounted-mean-floor math itself. This test's
// own focus -- that the sampling loop's *lookup count*, not just the
// resulting floor, is capped at min(reputationGroupSampleSize,
// reputationLookupConcurrency) even when a group has far more members
// than that -- has no engine-side counterpart yet, so it stays here
// against outbound_anomaly, whose observeDestSpread call still drives
// maybeCheckGroupReputation directly (dest_spread.go). Target moves from
// distributed_brute_force's "port 22" to outbound_anomaly's own Target
// shape, the LAN source IP; members move from distinct source IPs
// hitting one port to distinct external destinations one LAN source
// contacts.
func TestOutboundAnomalyGroupReputationSamplesAreCapped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OutboundAnomalyThreshold = 15
	cfg.InternalReconThreshold = 1000
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
		dst := fakeExternalIP(i)
		fake.setScore(dst, 80)
		d.Observe(lanEvt("192.168.1.50", dst, now.Add(time.Duration(i)*time.Millisecond)))
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
	waitForConfidence(t, fs, "192.168.1.50", wantConfidence)
}

// fakeExternalIP builds a distinct public IP in the TEST-NET-3 range
// for group-reputation tests that need many distinct source addresses.
func fakeExternalIP(n int) string {
	return fmt.Sprintf("203.0.113.%d", n)
}

// The three tests below exercise groupReputationCollector directly
// (bypassing Observe/maybeCheckGroupReputation entirely), so which
// flags.Type they tag their synthetic flags.Store entry with is
// arbitrary -- it was flags.TypeDistributedBruteForce/"port 22" before
// #405 moved distributed_brute_force to internal/engine; now that
// internal/detect's own maybeCheckGroupReputation is only ever called
// for outbound_anomaly (dest_spread.go), they use
// flags.TypeOutboundAnomaly and a LAN-IP-shaped target instead, so
// nothing here misleadingly implies internal/detect still evaluates
// distributed_brute_force. The math under test -- reputationGroup
// MinSignificantSamples/reputationGroupSampleSize, mean-of-scores
// discounted by sample significance -- is unchanged by that swap; it's
// also pinned end-to-end (through Observe) engine-side by
// TestGroupReputationSinkAppliesTheDiscountedMeanFloor and
// TestGroupReputationSinkStaysSilentBelowTheSignificanceFloor in
// internal/engine/reputation_sink_test.go.
func TestGroupReputationCollectorRequiresMinimumSignificantSamples(t *testing.T) {
	fs, err := flags.Open("")
	if err != nil {
		t.Fatal(err)
	}
	fs.AddWithConfidence(flags.TypeOutboundAnomaly, "192.168.1.50", "detail", 20, time.Now())

	c := &groupReputationCollector{pending: 5, t: flags.TypeOutboundAnomaly, target: "192.168.1.50", fs: fs}
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
	fs.AddWithConfidence(flags.TypeOutboundAnomaly, "192.168.1.50", "detail", 5, time.Now())

	c := &groupReputationCollector{pending: reputationGroupSampleSize, t: flags.TypeOutboundAnomaly, target: "192.168.1.50", fs: fs}
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
	fs.AddWithConfidence(flags.TypeOutboundAnomaly, "192.168.1.50", "detail", 5, time.Now())

	c := &groupReputationCollector{pending: reputationGroupSampleSize, t: flags.TypeOutboundAnomaly, target: "192.168.1.50", fs: fs}
	s := 80
	for i := 0; i < reputationGroupSampleSize; i++ {
		c.recordAndMaybeApply(&s)
	}

	if got := *fs.List()[0].Confidence; got != 80 {
		t.Errorf("expected a full, confident sample to apply the raw mean, got %d", got)
	}
}
