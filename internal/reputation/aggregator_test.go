package reputation

import (
	"context"
	"errors"
	"testing"
)

// fakeSource is a minimal Source for exercising Aggregator's merge/
// fallback behavior without any real network call -- same "inject a
// fake, don't hit the network" approach internal/detect's own
// reputation_test.go uses for reputationLookup.
type fakeSource struct {
	result Result
	err    error
}

func (f fakeSource) Lookup(ctx context.Context, ip string) (Result, error) {
	return f.result, f.err
}

func intPtr(n int) *int { return &n }

func TestAggregatorRejectsNonPublicIPWithoutQueryingSources(t *testing.T) {
	called := false
	a := NewAggregator(fakeSourceFunc(func(ctx context.Context, ip string) (Result, error) {
		called = true
		return Result{}, nil
	}))
	_, err := a.Lookup(context.Background(), "192.168.1.1")
	if err != ErrNotPublic {
		t.Fatalf("err = %v, want ErrNotPublic", err)
	}
	if called {
		t.Error("expected no source to be queried for a non-public IP")
	}
}

// fakeSourceFunc adapts a plain function to Source, for the one test
// above that needs to observe whether it was called at all rather than
// control what it returns.
type fakeSourceFunc func(ctx context.Context, ip string) (Result, error)

func (f fakeSourceFunc) Lookup(ctx context.Context, ip string) (Result, error) {
	return f(ctx, ip)
}

func TestAggregatorWorstCaseAbuseScoreWins(t *testing.T) {
	a := NewAggregator(
		fakeSource{result: Result{AbuseScore: intPtr(20)}},
		fakeSource{result: Result{AbuseScore: intPtr(85)}},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if r.AbuseScore == nil || *r.AbuseScore != 85 {
		t.Errorf("AbuseScore = %v, want 85 (the higher of the two)", r.AbuseScore)
	}
}

func TestAggregatorFallsBackWhenOneSourceErrors(t *testing.T) {
	a := NewAggregator(
		fakeSource{err: errors.New("network error")},
		fakeSource{result: Result{AbuseScore: intPtr(60), CountryCode: "US"}},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if r.AbuseScore == nil || *r.AbuseScore != 60 || r.CountryCode != "US" {
		t.Errorf("expected the healthy source's data to survive the other source's error, got %+v", r)
	}
}

func TestAggregatorAllSourcesErrorReturnsZeroResultNoError(t *testing.T) {
	// Same contract Client.Lookup already has for a failed Shodan/
	// AbuseIPDB fetch: a source-level failure is absorbed, not
	// surfaced as an Aggregator-level error -- only ErrNotPublic (bad
	// input) is ever returned as an error.
	a := NewAggregator(
		fakeSource{err: errors.New("network error 1")},
		fakeSource{err: errors.New("network error 2")},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if r.AbuseScore != nil {
		t.Errorf("expected a zero Result, got %+v", r)
	}
}

func TestAggregatorMergesUnionsPortsHostnamesTagsAndVulns(t *testing.T) {
	a := NewAggregator(
		fakeSource{result: Result{Ports: []int{22, 80}, Hostnames: []string{"a.example"}, Tags: []string{"scanner"}}},
		fakeSource{result: Result{Ports: []int{80, 443}, Vulns: []string{"CVE-2024-0001"}, Tags: []string{"scanner", "bot"}}},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(r.Ports) != 3 {
		t.Errorf("Ports = %v, want 3 unique entries (22, 80, 443)", r.Ports)
	}
	if len(r.Hostnames) != 1 || r.Hostnames[0] != "a.example" {
		t.Errorf("Hostnames = %v, want [a.example]", r.Hostnames)
	}
	if len(r.Vulns) != 1 || r.Vulns[0] != "CVE-2024-0001" {
		t.Errorf("Vulns = %v, want [CVE-2024-0001]", r.Vulns)
	}
	if len(r.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 unique entries (scanner, bot)", r.Tags)
	}
}

func TestAggregatorMergesORsBooleanSignals(t *testing.T) {
	a := NewAggregator(
		fakeSource{result: Result{IsTor: false, Noise: true, Riot: false}},
		fakeSource{result: Result{IsTor: true, Noise: false, Riot: true}},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !r.IsTor || !r.Noise || !r.Riot {
		t.Errorf("expected every boolean signal to OR together, got %+v", r)
	}
}

func TestAggregatorFirstNonEmptyStringFieldWins(t *testing.T) {
	a := NewAggregator(
		fakeSource{result: Result{}}, // first source: nothing to contribute
		fakeSource{result: Result{CountryCode: "DE", ISP: "Example ISP", Classification: "malicious", ActorName: "Mirai"}},
	)
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if r.CountryCode != "DE" || r.ISP != "Example ISP" || r.Classification != "malicious" || r.ActorName != "Mirai" {
		t.Errorf("expected the second source's fields to fill in where the first had nothing, got %+v", r)
	}
}

func TestAggregatorDropsNilSources(t *testing.T) {
	var nilGreyNoise *GreyNoiseClient
	a := NewAggregator(fakeSource{result: Result{AbuseScore: intPtr(42)}}, nilGreyNoise, nil)
	if len(a.sources) != 1 {
		t.Fatalf("expected nil sources to be dropped, got %d sources", len(a.sources))
	}
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if r.AbuseScore == nil || *r.AbuseScore != 42 {
		t.Errorf("expected the non-nil source's data, got %+v", r)
	}
}

func TestAggregatorWithNoSourcesReturnsZeroResult(t *testing.T) {
	a := NewAggregator()
	r, err := a.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if r.IP != "203.0.113.9" || r.AbuseScore != nil {
		t.Errorf("expected a zero-value-except-IP Result, got %+v", r)
	}
}

func TestAggregatorSatisfiesSourceInterface(t *testing.T) {
	var _ Source = (*Aggregator)(nil)
	var _ Source = (*Client)(nil)
	var _ Source = (*GreyNoiseClient)(nil)
}
