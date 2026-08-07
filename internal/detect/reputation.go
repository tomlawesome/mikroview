package detect

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

// reputationLookup is the subset of *reputation.Client's API
// internal/detect depends on -- kept as a small interface (rather than
// depending on the concrete type) purely so tests can inject a fake
// without making real network calls, since some test fixture IPs (RFC
// 5737 TEST-NET ranges, e.g. "203.0.113.9") are classified "public" by
// isPublic().
type reputationLookup interface {
	Lookup(ctx context.Context, ip string) (reputation.Result, error)
}

// reputationLookupTimeout bounds one lookup's context -- generous
// headroom above reputation.Client's own internal 5s HTTP timeout,
// belt-and-braces against a leaked/hung context rather than the
// primary bound.
const reputationLookupTimeout = 10 * time.Second

// reputationLookupConcurrency caps in-flight lookups, shared by both
// the single-IP and group paths (one pool-wide budget, not one per
// mechanism) -- a burst of many *new* episodes (many distinct source
// IPs crossing threshold at once, or several group episodes at once) is
// exactly this feature's target scenario, and AbuseIPDB's free-tier
// daily quota is the sharper constraint than goroutine count. A
// saturated pool skips the lookup for that episode/sample-member
// (non-blocking enqueue) rather than queuing -- queuing would just burn
// each lookup's timeout budget waiting instead of in flight.
//
// Kept >= reputationGroupSampleSize: a single group episode's own
// sampling loop runs synchronously and doesn't retry a member it
// skipped for a saturated pool, so if this were smaller than the
// sample size, a group check starting from an idle pool could never
// reach its own cap even in the best case -- the two constants need to
// stay coherent with each other, not just individually reasonable.
const reputationLookupConcurrency = 8

// reputationGroupSampleSize caps how many of a group's distinct members
// get checked per episode -- checking all of them isn't reasonable in
// raw count or against AbuseIPDB's rate limit. Go's own randomized map
// iteration order gives an effectively random sample of the group for
// free, so no separate shuffling is needed.
const reputationGroupSampleSize = 10

// reputationGroupMinSignificantSamples is the floor on how many
// *successfully scored* members are needed before the aggregate is
// trusted at all -- a single bad-reputation IP out of a group of 25
// isn't meaningful signal; several out of a bounded sample is closer to
// it. Below this, RaiseConfidenceFloor is never called at all
// (insufficient evidence either way).
const reputationGroupMinSignificantSamples = 3

// WithReputation attaches an optional reputation client for confidence-
// floor lookups against external source IPs. Returns d for chaining;
// nil (the default) is a valid, explicit "not configured" no-op --
// never set by any test helper, so tests never make real network calls
// unless a test explicitly injects a fake.
func (d *Detector) WithReputation(client reputationLookup) *Detector {
	d.reputation = client
	return d
}

// WithEntities attaches the shared entity store (internal/entities,
// issue #107) that observeMailSender consults for its
// trusted-mail-sender allowlist (issue #108). Returns d for chaining,
// same pattern as WithReputation. nil (the default, if never called) is
// a valid, explicit "no allowlist configured" state -- observeMailSender
// treats it as "nothing is tagged trusted," not as an error.
func (d *Detector) WithEntities(es *entities.Store) *Detector {
	d.entities = es
	return d
}

// maybeCheckReputation kicks off a best-effort, async reputation lookup
// for a newly-raised (not re-fired) flag against a public source IP.
// target is the flags.Store key (may differ from ip -- see
// observeRepeatedDrops, whose target is a composite "<ip> -> port <N>"
// string). No-ops if reputation isn't configured, this wasn't a new
// episode, ip isn't public, or the lookup pool is saturated.
//
// Only ever touches d.fs (flags.Store, its own lock domain) and
// d.reputation (read-only after construction via WithReputation, which
// always runs before the ingest goroutine starts) -- never
// d.perSource/d.destWindows/etc, so this never needs to synchronize
// with the single ingest goroutine detect.Observe runs on.
func (d *Detector) maybeCheckReputation(t flags.Type, target, ip string, isNewEpisode bool) {
	if d.reputation == nil || !isNewEpisode || !isPublic(ip) {
		return
	}
	select {
	case d.lookupSlots <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-d.lookupSlots }()
		defer logging.Recover(logger)
		ctx, cancel := context.WithTimeout(context.Background(), reputationLookupTimeout)
		defer cancel()
		result, err := d.reputation.Lookup(ctx, ip)
		if err != nil {
			return
		}
		// Stored even without an AbuseScore -- a Shodan-only result (no
		// AbuseIPDB key configured) is still worth capturing as a
		// snapshot; ApplyReputationSnapshot only raises the confidence
		// floor when a score is actually present.
		d.fs.ApplyReputationSnapshot(t, target, result)
	}()
}

// groupReputationCollector aggregates up to len(sample) independent
// async lookups for one flag episode into a single confidence floor,
// applied once every sample has resolved (data, no-data, or skipped for
// a saturated pool -- all three still count toward "resolved" so this
// always completes). The floor is the mean of the successfully scored
// members, discounted by how much of the sample cap was actually filled
// with real data -- a complete, confident sample counts for more than a
// thin one.
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

// maybeCheckGroupReputation is maybeCheckReputation's counterpart for
// detectors whose flag represents *many* distinct external IPs
// (distributed_brute_force's source IPs, outbound_anomaly's
// destinations) rather than one -- checking every member isn't
// reasonable, so this samples up to reputationGroupSampleSize of them
// and requires at least reputationGroupMinSignificantSamples to return
// real data before trusting the aggregate. Shares d.lookupSlots with
// the single-IP path -- one pool-wide budget, not a separate one per
// mechanism, so a burst of group episodes can't starve single-IP
// lookups or vice versa.
func (d *Detector) maybeCheckGroupReputation(t flags.Type, target string, members map[string]struct{}, isNewEpisode bool) {
	if d.reputation == nil || !isNewEpisode {
		return
	}
	sample := make([]string, 0, reputationGroupSampleSize)
	for ip := range members {
		if !isPublic(ip) {
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

	collector := &groupReputationCollector{pending: len(sample), t: t, target: target, fs: d.fs}
	for _, ip := range sample {
		ip := ip
		select {
		case d.lookupSlots <- struct{}{}:
		default:
			collector.recordAndMaybeApply(nil) // pool saturated -- counts as resolved-with-no-data, not a permanent stall
			continue
		}
		go func() {
			defer func() { <-d.lookupSlots }()
			defer logging.Recover(logger)
			ctx, cancel := context.WithTimeout(context.Background(), reputationLookupTimeout)
			defer cancel()
			result, err := d.reputation.Lookup(ctx, ip)
			if err != nil {
				collector.recordAndMaybeApply(nil)
				return
			}
			collector.recordAndMaybeApply(result.AbuseScore)
		}()
	}
}
