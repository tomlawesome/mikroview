// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

// fakeReputation mirrors internal/detect/reputation_test.go's fake of the
// same name: it blocks each call until the test releases it and reports
// every IP it was asked about on started, so a test can deterministically
// observe "a lookup was attempted" and control exactly when it resolves
// rather than racing a real goroutine with sleeps. Kept as this package's
// own small copy rather than exported from internal/detect, the same
// per-package-fake convention that package's own doc comment sets.
type fakeReputation struct {
	started chan string
	release chan struct{}

	mu     sync.Mutex
	scores map[string]int
}

func newFakeReputation() *fakeReputation {
	return &fakeReputation{
		started: make(chan string, 100),
		release: make(chan struct{}),
		scores:  make(map[string]int),
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
	score, ok := f.scores[ip]
	if !ok {
		return reputation.Result{IP: ip}, nil
	}
	return reputation.Result{IP: ip, AbuseScore: &score}, nil
}

func repExpectStarted(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case ip := <-ch:
		return ip
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a reputation lookup to start")
		return ""
	}
}

func repExpectNoneStarted(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case ip := <-ch:
		t.Fatalf("expected no reputation lookup, but one started for %s", ip)
	case <-time.After(100 * time.Millisecond):
	}
}

func repWaitForConfidence(t *testing.T, fs *flags.Store, target string, want int) {
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

// newReputationBackedCriticalPort builds critical_port wired through
// ReputationSink rather than plain FlagsSink -- exactly what main.go does
// for every shipped declarative definition.
func newReputationBackedCriticalPort(t *testing.T, fs *flags.Store, client ReputationLookup, concurrency, threshold int) *DeclarativeDefinition {
	t.Helper()
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, threshold, time.Minute, Scope{})
	dd.OnRoutedEmission = ReputationSink(fs, client, concurrency)
	return dd
}

// TestReputationSinkAppliesFloorOnANewEpisode is
// internal/detect/reputation_test.go's TestCriticalPortAppliesReputationFloor,
// moved: internal/detect raised critical_port and then called
// maybeCheckReputation on the same source IP; ReputationSink is the
// engine-side seam that does the same thing for a ported definition, and
// #405 wires it in main.go so no detector loses its reputation lookup on
// the way across.
func TestReputationSinkAppliesFloorOnANewEpisode(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()
	fake.setScore("198.51.100.4", 85)
	dd := newReputationBackedCriticalPort(t, fs, fake, 8, 3)

	now := time.Now()
	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}

	if ip := repExpectStarted(t, fake.started); ip != "198.51.100.4" {
		t.Fatalf("expected a lookup for 198.51.100.4, got %s", ip)
	}
	close(fake.release)
	repWaitForConfidence(t, fs, "198.51.100.4", 85)

	for _, f := range fs.List() {
		if f.Target == "198.51.100.4" {
			if f.Reputation == nil || f.Reputation.AbuseScore == nil || *f.Reputation.AbuseScore != 85 {
				t.Errorf("expected the reputation snapshot to be stored on the flag, got %+v", f.Reputation)
			}
		}
	}
}

// TestReputationSinkSnapshotCapturedWithoutAbuseScore is
// internal/detect/reputation_test.go's
// TestReputationSnapshotCapturedWithoutAbuseScore, moved: a Shodan-only
// result (no AbuseIPDB key configured) is still stored as a snapshot, and
// still leaves the flag's confidence behaviour-only.
func TestReputationSinkSnapshotCapturedWithoutAbuseScore(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation() // no score set for this IP
	dd := newReputationBackedCriticalPort(t, fs, fake, 8, 3)

	now := time.Now()
	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	repExpectStarted(t, fake.started)
	close(fake.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range fs.List() {
			if f.Target == "198.51.100.4" && f.Reputation != nil {
				if f.Confidence == nil || *f.Confidence != overshootConfidence(3, 3) {
					t.Errorf("expected confidence to stay behavior-only without an AbuseScore, got %+v", f.Confidence)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the reputation snapshot to be stored")
}

// TestReputationSinkOnlyLooksUpOnANewEpisode is
// internal/detect/reputation_test.go's
// TestReputationLookupOnlyFiresOnNewEpisode, moved: a re-fire of a
// still-active flag must never start a second lookup. This is the whole
// reason raiseDetectionFlag returns AddProvisional's isNew (flags_sink.go)
// rather than discarding it.
func TestReputationSinkOnlyLooksUpOnANewEpisode(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()
	fake.setScore("198.51.100.4", 50)
	dd := newReputationBackedCriticalPort(t, fs, fake, 8, 3)

	now := time.Now()
	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	repExpectStarted(t, fake.started)
	close(fake.release)
	repWaitForConfidence(t, fs, "198.51.100.4", 50)

	dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(10*time.Second)))
	repExpectNoneStarted(t, fake.started)
}

// TestReputationSinkSkippedForInternalTarget is
// internal/detect/reputation_test.go's TestReputationSkippedForInternalSource,
// moved: a Target that is not a public IP is never a lookup candidate.
// Driven through a definition whose classification condition is relaxed
// (a bare per-source threshold) so a LAN source can actually reach the
// sink -- critical_port's own external-only condition would otherwise
// make this vacuous.
func TestReputationSinkSkippedForInternalTarget(t *testing.T) {
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()

	def := Definition{
		ID:          "port_scan",
		Name:        "Port scan",
		Intent:      IntentDetection,
		Kind:        KindDeclarative,
		Enabled:     true,
		Params:      Params{"threshold": 3, "window": time.Minute.String()},
		ParamSchema: PortScanParamSchema,
		Provenance:  Provenance{Origin: ProvenanceShipped},
	}
	dd, err := BuildShippedDeclarativeDefinition(def)
	if err != nil {
		t.Fatalf("BuildShippedDeclarativeDefinition(port_scan): %v", err)
	}
	dd.OnRoutedEmission = ReputationSink(fs, fake, 8)

	now := time.Now()
	for port := 1; port <= 3; port++ {
		dd.Evaluate(psEvt("192.168.1.50", port, now.Add(time.Duration(port)*time.Millisecond)))
	}
	if psFlagOfType(fs) == nil {
		t.Fatal("expected the definition to have fired for the LAN source (otherwise this test is vacuous)")
	}
	repExpectNoneStarted(t, fake.started)
}

// TestReputationSinkPoolSaturationSkipsExcessLookups is
// internal/detect/reputation_test.go's
// TestReputationPoolSaturationSkipsExcessLookups, moved: a saturated pool
// skips that episode's lookup (non-blocking enqueue) rather than queuing,
// since queuing would just burn each lookup's timeout budget waiting
// instead of in flight.
func TestReputationSinkPoolSaturationSkipsExcessLookups(t *testing.T) {
	const concurrency = 8
	fs := newTestFlagsStore(t)
	fake := newFakeReputation()
	dd := newReputationBackedCriticalPort(t, fs, fake, concurrency, 1)

	now := time.Now()
	for i := 1; i <= concurrency+1; i++ {
		dd.Evaluate(psEvt(repFakeExternalIP(i), 22, now.Add(time.Duration(i)*time.Second)))
	}

	seen := make(map[string]bool)
	for i := 0; i < concurrency; i++ {
		seen[repExpectStarted(t, fake.started)] = true
	}
	if len(seen) != concurrency {
		t.Fatalf("expected %d distinct lookups, got %d: %v", concurrency, len(seen), seen)
	}

	// The pool is now saturated -- the next episode's lookup is skipped.
	repExpectNoneStarted(t, fake.started)

	close(fake.release)
}

// repFakeExternalIP builds a distinct RFC 5737 TEST-NET-3 address per
// index -- synthetic fixtures only, never a real routable address.
func repFakeExternalIP(i int) string {
	return "203.0.113." + strconv.Itoa(i)
}

// TestReputationSinkWithNilClientIsPlainFlagsSink pins the explicit
// "not configured" case: no client means no lookup path at all, and the
// flag is still raised exactly as FlagsSink would raise it.
func TestReputationSinkWithNilClientIsPlainFlagsSink(t *testing.T) {
	fs := newTestFlagsStore(t)
	dd := newShippedCriticalPortDefinition(t, fs, []int{22}, 3, time.Minute, Scope{})
	dd.OnRoutedEmission = ReputationSink(fs, nil, 8)

	now := time.Now()
	for i := 0; i < 3; i++ {
		dd.Evaluate(psEvt("198.51.100.4", 22, now.Add(time.Duration(i)*time.Second)))
	}
	if got := cpFlagOfType(fs); got == nil {
		t.Fatal("expected the flag to be raised with no reputation client configured")
	}
}
