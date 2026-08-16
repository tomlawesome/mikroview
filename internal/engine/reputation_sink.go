// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"math"
	"net"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

var reputationSinkLogger = logging.New("engine-reputation")

// ReputationLookup is the subset of *reputation.Client's API this sink
// depends on -- mirrors internal/detect.reputationLookup (reputation.go)
// exactly, for the same reason that package keeps its own small
// interface rather than depending on the concrete type: tests can inject
// a fake without making real network calls.
type ReputationLookup interface {
	Lookup(ctx context.Context, ip string) (reputation.Result, error)
}

// reputationLookupTimeout mirrors internal/detect.reputationLookupTimeout
// -- generous headroom above reputation.Client's own internal 5s HTTP
// timeout, belt-and-braces against a leaked/hung context rather than the
// primary bound.
const reputationLookupTimeout = 10 * time.Second

// ReputationSink is FlagsSink plus a best-effort, async, pool-bounded
// reputation lookup against a newly-raised (never a re-fire) episode's
// Target -- the engine-side counterpart to internal/detect's
// maybeCheckReputation (reputation.go), for a declarative definition
// whose Target is a single source IP (port_scan, critical_port, ...).
// Ported alongside each detector's own #405 move so this doesn't
// silently regress detector by detector -- the old detect.Detector had
// this wired for every single-IP-keyed detector via WithReputation.
//
// client == nil is a valid, explicit "not configured" no-op -- returns
// plain FlagsSink(fs) rather than a lookup path that would always skip
// anyway, the same "nil is inert" convention every optional dependency in
// this codebase follows (internal/detect.Detector's own
// reputation/entities/knownBad/netclass fields).
//
// concurrency bounds in-flight lookups -- a *separate* pool from
// internal/detect's own reputationLookupConcurrency-sized one, not
// shared, since the two subsystems still run side by side (feeding the
// same *reputation.Client, but budgeting their own concurrency
// independently) until every detector has moved off internal/detect.
// main.go is expected to pass the same concurrency figure
// internal/detect uses today, so the two pools' combined worst case
// stays within what this codebase has already tuned AbuseIPDB's free-tier
// quota against, until #405 finishes and internal/detect's own pool is
// deleted.
func ReputationSink(fs *flags.Store, client ReputationLookup, concurrency int) func(RoutedEmission) {
	if client == nil {
		return FlagsSink(fs)
	}
	slots := make(chan struct{}, concurrency)
	return func(r RoutedEmission) {
		isNew := raiseDetectionFlag(fs, r)
		if !isNew || r.Detection == nil {
			return
		}
		f := r.Detection
		// r.SourceIP where the definition supplied one, falling back to
		// the Target: the two coincide for a source-keyed definition
		// (port_scan, critical_port), and differ for one whose Target is a
		// composite (repeated_drops' "<source> -> port <N>"). This is the
		// same (target, ip) split internal/detect.maybeCheckReputation
		// took as two parameters -- the flag is keyed on the target, the
		// lookup is about the address.
		ip := r.SourceIP
		if ip == "" {
			ip = f.Target
		}
		if !isPublicIPAddress(ip) {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			return // pool saturated -- skip this episode's lookup, same as detect.maybeCheckReputation
		}
		go func() {
			defer func() { <-slots }()
			defer logging.Recover(reputationSinkLogger)
			ctx, cancel := context.WithTimeout(context.Background(), reputationLookupTimeout)
			defer cancel()
			result, err := client.Lookup(ctx, ip)
			if err != nil {
				return
			}
			// Stored even without an AbuseScore -- see
			// internal/detect.maybeCheckReputation's identical comment:
			// a Shodan-only result is still worth capturing as a
			// snapshot, and flags.Store.ApplyReputationSnapshot only
			// raises the confidence floor when a score is actually
			// present.
			fs.ApplyReputationSnapshot(f.Type, f.Target, result)
		}()
	}
}

// isPublicIPAddress mirrors internal/detect.isPublic (and this package's
// own isPublicIP, which takes a net.IP rather than a string) -- a
// definition's Target is not always a parseable IP (KeyGlobal's "global",
// a rule label for a future rule-keyed definition), so an unparseable
// value is treated as "not a lookup candidate," not an error.
func isPublicIPAddress(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return isPublicIP(ip)
}

// reputationGroupSampleSize/reputationGroupMinSignificantSamples mirror
// internal/detect's constants of the same names (reputation.go): how
// many of a group's distinct members get checked per episode, and how
// many must return real data before the aggregate is trusted at all.
// Checking every member of a distributed brute force is unreasonable in
// raw count and against AbuseIPDB's rate limit; a single bad-reputation
// IP out of twenty-five is not meaningful signal, several out of a
// bounded sample is closer to it.
const (
	reputationGroupSampleSize            = 10
	reputationGroupMinSignificantSamples = 3
)

// groupReputationCollector aggregates up to len(sample) independent
// async lookups for one flag episode into a single confidence floor,
// applied once every sample has resolved (data, no-data, or skipped for
// a saturated pool -- all three count as resolved, so this always
// completes). Unchanged in behaviour from internal/detect's collector of
// the same name: the floor is the mean of the successfully scored
// members, discounted by how much of the sample cap was actually filled
// with real data.
type groupReputationCollector struct {
	mu      sync.Mutex
	scores  []int
	pending int
	t       flags.Type
	target  string
	fs      *flags.Store
}

func (c *groupReputationCollector) recordAndMaybeApply(score *int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if score != nil {
		c.scores = append(c.scores, *score)
	}
	c.pending--
	if c.pending > 0 {
		return
	}
	if len(c.scores) < reputationGroupMinSignificantSamples {
		return
	}
	sum := 0
	for _, s := range c.scores {
		sum += s
	}
	mean := float64(sum) / float64(len(c.scores))
	significance := math.Min(1, float64(len(c.scores))/float64(reputationGroupSampleSize))
	c.fs.RaiseConfidenceFloor(c.t, c.target, int(math.Round(mean*significance)))
}

// GroupReputationSink is ReputationSink's counterpart for a definition
// whose emission represents *many* distinct external addresses rather
// than one -- distributed_brute_force's source set, outbound_anomaly's
// destination set. The engine-side port of
// internal/detect.maybeCheckGroupReputation.
//
// The sample comes from the emission's own accumulated Evidence.Hosts,
// which is the honest source: it is exactly the set the flag itself
// shows an operator, so a floor derived from it is a floor derived from
// what was actually claimed. One consequence worth stating rather than
// discovering later: Evidence.Hosts is capped (maxEvidenceHosts, 20)
// where internal/detect sampled from the full uncapped member map, so on
// an episode with more than 20 distinct members the sampling pool is
// narrower here. Both then take at most reputationGroupSampleSize (10)
// from it, so the sample size itself is unchanged; only which 10 are
// eligible differs, and the capped set is the deterministic, sorted one
// rather than Go's randomized map order.
//
// client == nil is a valid, explicit "not configured" no-op, same
// convention as ReputationSink.
func GroupReputationSink(fs *flags.Store, client ReputationLookup, concurrency int) func(RoutedEmission) {
	if client == nil {
		return FlagsSink(fs)
	}
	slots := make(chan struct{}, concurrency)
	return func(r RoutedEmission) {
		isNew := raiseDetectionFlag(fs, r)
		if !isNew || r.Detection == nil {
			return
		}
		f := r.Detection
		sample := make([]string, 0, reputationGroupSampleSize)
		for _, ip := range f.Evidence.Hosts {
			if !isPublicIPAddress(ip) {
				continue
			}
			sample = append(sample, ip)
			if len(sample) >= reputationGroupSampleSize {
				break
			}
		}
		if len(sample) == 0 {
			return
		}

		collector := &groupReputationCollector{pending: len(sample), t: f.Type, target: f.Target, fs: fs}
		for _, ip := range sample {
			select {
			case slots <- struct{}{}:
			default:
				// Pool saturated -- counts as resolved-with-no-data, not a
				// permanent stall, exactly as internal/detect's own loop does.
				collector.recordAndMaybeApply(nil)
				continue
			}
			go func() {
				defer func() { <-slots }()
				defer logging.Recover(reputationSinkLogger)
				ctx, cancel := context.WithTimeout(context.Background(), reputationLookupTimeout)
				defer cancel()
				result, err := client.Lookup(ctx, ip)
				if err != nil {
					collector.recordAndMaybeApply(nil)
					return
				}
				collector.recordAndMaybeApply(result.AbuseScore)
			}()
		}
	}
}

// shippedGroupReputationIDs names the shipped definitions whose emission
// is about a *set* of external addresses rather than one, and so wants
// GroupReputationSink instead of ReputationSink -- the same split
// internal/detect drew by calling maybeCheckGroupReputation from
// distributed_brute_force and dest_spread's outbound half, and
// maybeCheckReputation everywhere else. Kept as a table next to the
// sinks, rather than a branch in main.go, for the same reason
// shippedDeclarativeBuilders is a table: "which shipped definition
// behaves how" is this package's knowledge, not the process wiring's.
var shippedGroupReputationIDs = map[string]bool{
	"distributed_brute_force": true,
	// outbound_anomaly's emission is about the set of external
	// destinations one LAN source reached, not about the source itself
	// (which is a LAN address and never a lookup candidate at all) --
	// internal/detect called maybeCheckGroupReputation here for exactly
	// that reason. internal_recon deliberately has no entry: its
	// destinations are internal by construction, so every member of its
	// set would be skipped as non-public, and internal/detect made no
	// reputation call from it either.
	"outbound_anomaly": true,
}

// ShippedDeclarativeSink picks the emission sink a shipped definition
// should be wired to: the group-sampling reputation path for a
// definition whose flag represents many external addresses, the
// single-address path otherwise. This is the one call site main.go needs.
func ShippedDeclarativeSink(def Definition, fs *flags.Store, client ReputationLookup, concurrency int) func(RoutedEmission) {
	if shippedGroupReputationIDs[def.ID] {
		return GroupReputationSink(fs, client, concurrency)
	}
	return ReputationSink(fs, client, concurrency)
}
