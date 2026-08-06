package reputation

import (
	"context"
	"net"
	"sync"
)

// Source is the interface both Client and GreyNoiseClient satisfy, and
// the one Aggregator itself also satisfies -- the same shape
// internal/detect's own private reputationLookup interface already
// depends on (see internal/detect/reputation.go), duplicated here as a
// public type so internal/api.Server.Reputation and main.go can hold
// either a lone *Client or an *Aggregator without either caller needing
// to know which.
type Source interface {
	Lookup(ctx context.Context, ip string) (Result, error)
}

// Aggregator (issue #113 Part A) queries every configured Source
// concurrently and merges their results, so internal/detect and
// internal/api see one combined reputation picture regardless of how
// many underlying sources are actually configured. Satisfies Source
// itself, so it's a drop-in replacement anywhere a lone *Client used to
// be handed to WithReputation/api.Server.Reputation -- callers never
// need to know whether they're talking to one source or several.
type Aggregator struct {
	sources []Source
}

// NewAggregator builds an Aggregator over sources, silently dropping any
// nil entry -- lets a caller write `NewAggregator(rep, greyNoiseClient)`
// unconditionally even when greyNoiseClient is nil (not configured)
// rather than needing its own conditional to decide whether to wrap at
// all. Safe (though pointless) to call with zero non-nil sources: Lookup
// then always returns a zero Result, same as a Source that's configured
// but has nothing to report.
func NewAggregator(sources ...Source) *Aggregator {
	a := &Aggregator{sources: make([]Source, 0, len(sources))}
	for _, s := range sources {
		if s == nil || isNilSource(s) {
			continue
		}
		a.sources = append(a.sources, s)
	}
	return a
}

// isNilSource catches a common footgun this package's own callers hit:
// `var c *reputation.GreyNoiseClient` left nil, then passed to
// NewAggregator as a Source -- the interface value itself is non-nil
// (it carries a concrete type), so a plain `s == nil` check above
// doesn't catch it, but calling Lookup on a nil *Client/*GreyNoiseClient
// would panic. Both concrete types are simple structs with no method
// that dereferences before a nil check of their own, so this uses a
// type switch rather than reflection to stay allocation-free on the
// common non-nil path.
func isNilSource(s Source) bool {
	switch v := s.(type) {
	case *Client:
		return v == nil
	case *GreyNoiseClient:
		return v == nil
	case *Aggregator:
		return v == nil
	default:
		return false
	}
}

// Lookup queries every configured source concurrently and merges their
// results (see mergeResults) -- the publicity/parse check happens once
// here rather than once per source, since every Source implementation
// applies the exact same isPublic rule anyway. Only errors on
// ErrNotPublic (bad input); an individual source failing (network,
// missing key, rate limit) just contributes a zero Result to the merge,
// same best-effort contract every Source already has on its own --
// Aggregator itself never fails just because one of several sources did.
func (a *Aggregator) Lookup(ctx context.Context, ipStr string) (Result, error) {
	if !isPublicString(ipStr) {
		return Result{}, ErrNotPublic
	}
	if len(a.sources) == 0 {
		return Result{IP: ipStr}, nil
	}

	// Each goroutine writes only to its own slot in results; the merge
	// below runs after wg.Wait() establishes a happens-before
	// relationship, so there's no concurrent access to shared state.
	results := make([]Result, len(a.sources))
	var wg sync.WaitGroup
	for i, src := range a.sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			r, err := src.Lookup(ctx, ipStr)
			if err == nil {
				results[i] = r
			}
		}(i, src)
	}
	wg.Wait()

	merged := mergeResults(ipStr, results)
	return merged, nil
}

// isPublicString mirrors Client.Lookup's own net.ParseIP/isPublic guard
// -- pulled out so Aggregator can apply it once up front instead of
// relying on every Source to reject the same way (they all do today,
// but Aggregator shouldn't depend on that). Deliberately just parses and
// checks locally rather than delegating to a Source's own Lookup, which
// would trigger a real outbound HTTP call (Client's Shodan fetch has no
// key gate) just to answer a yes/no question about the input string.
func isPublicString(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil && isPublic(ip)
}

// mergeResults combines every source's Result for the same IP into one.
// Default strategy, per source-specific field:
//   - AbuseScore/TotalReports: the higher of any two non-nil values
//     ("worst case wins" -- a higher abuse score or report count is
//     always the more cautious reading, never averaged or overwritten
//     by a lower one).
//   - IsTor/Noise/Riot: OR'd -- any source flagging it is enough.
//   - Ports/Hostnames/Vulns/Tags: unioned (deduplicated), since these
//     are "what's been observed," not competing verdicts.
//   - CountryCode/ISP/UsageType/Classification/ActorName: first non-
//     empty value wins, in source order -- these are descriptive facts
//     a given source either knows or doesn't, not something to merge
//     numerically.
//
// This is a reasonable default, not a hard requirement (see issue
// #113's own framing) -- worth revisiting once there's real multi-
// source usage data to judge against.
func mergeResults(ip string, results []Result) Result {
	merged := Result{IP: ip}
	seenPorts := make(map[int]bool)
	seenStrings := make(map[string]bool) // shared across Hostnames/Vulns/Tags -- each field's own slice below is what actually dedups, this just backs that per-field check without three separate maps

	for _, r := range results {
		if r.AbuseScore != nil && (merged.AbuseScore == nil || *r.AbuseScore > *merged.AbuseScore) {
			v := *r.AbuseScore
			merged.AbuseScore = &v
		}
		if r.TotalReports != nil && (merged.TotalReports == nil || *r.TotalReports > *merged.TotalReports) {
			v := *r.TotalReports
			merged.TotalReports = &v
		}
		merged.IsTor = merged.IsTor || r.IsTor
		merged.Noise = merged.Noise || r.Noise
		merged.Riot = merged.Riot || r.Riot

		if merged.CountryCode == "" {
			merged.CountryCode = r.CountryCode
		}
		if merged.ISP == "" {
			merged.ISP = r.ISP
		}
		if merged.UsageType == "" {
			merged.UsageType = r.UsageType
		}
		if merged.Classification == "" {
			merged.Classification = r.Classification
		}
		if merged.ActorName == "" {
			merged.ActorName = r.ActorName
		}

		for _, p := range r.Ports {
			if !seenPorts[p] {
				seenPorts[p] = true
				merged.Ports = append(merged.Ports, p)
			}
		}
		merged.Hostnames = appendUnique(merged.Hostnames, r.Hostnames, seenStrings, "h:")
		merged.Vulns = appendUnique(merged.Vulns, r.Vulns, seenStrings, "v:")
		merged.Tags = appendUnique(merged.Tags, r.Tags, seenStrings, "t:")
	}

	return merged
}

// appendUnique appends every entry of add not already recorded in seen
// (under the given prefix, so Hostnames/Vulns/Tags sharing one seen map
// in mergeResults can't collide with each other on an equal string) to
// out, returning the extended slice.
func appendUnique(out []string, add []string, seen map[string]bool, prefix string) []string {
	for _, s := range add {
		key := prefix + s
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	return out
}
