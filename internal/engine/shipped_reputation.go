// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("reputation", buildReputationDefinition)
}

// ReputationPolicy is everything the reputation lookup paths
// (ReputationSink, GroupReputationSink) need to know about how hard to
// look and how much to trust a partial answer.
//
// It is a value rather than four constants because it is exactly what
// the reputation definition's params carry -- see reputationDefinition.
type ReputationPolicy struct {
	// Concurrency bounds in-flight lookups across both the single-address
	// and group paths -- one pool-wide budget, not one per mechanism, so
	// a burst of group episodes cannot starve single-address lookups or
	// vice versa. A saturated pool skips that episode's lookup rather
	// than queuing, which would only burn each lookup's timeout budget
	// waiting instead of in flight.
	Concurrency int
	// Timeout bounds one lookup's context -- generous headroom above the
	// reputation client's own internal HTTP timeout, belt-and-braces
	// against a leaked or hung context rather than the primary bound.
	Timeout time.Duration
	// GroupSampleSize caps how many of a group episode's distinct
	// addresses are checked. Kept at or below Concurrency: a group's
	// sampling loop is synchronous and does not retry a member it skipped
	// for a saturated pool, so a smaller pool than sample size means a
	// group check starting from idle could never reach its own cap even
	// in the best case.
	GroupSampleSize int
	// GroupMinSignificantSamples is the floor on how many sampled
	// addresses must return a real score before the aggregate is trusted
	// at all. One bad-reputation address out of twenty-five is not
	// meaningful signal; several out of a bounded sample is closer to it.
	// Below this, no floor is applied -- insufficient evidence either way.
	GroupMinSignificantSamples int
}

// DefaultReputationPolicy is internal/detect's four hard-coded constants
// (reputationLookupConcurrency, reputationLookupTimeout,
// reputationGroupSampleSize, reputationGroupMinSignificantSamples), at
// exactly the values they were compiled with -- which is what makes
// turning them into params a no-behaviour-change move.
func DefaultReputationPolicy() ReputationPolicy {
	return ReputationPolicy{
		Concurrency:                8,
		Timeout:                    10 * time.Second,
		GroupSampleSize:            10,
		GroupMinSignificantSamples: 3,
	}
}

// reputationDefinition is reputation ported onto the chassis (issue
// #405). It is the odd one out in the shipped catalogue and it is worth
// being explicit about why it is here at all.
//
// # It is a policy, not a detector
//
// internal/detect's reputation.go was never a detector: it had no
// threshold, no window and no flag of its own. It was a best-effort,
// async enrichment attached to *other* detectors' newly-raised episodes,
// plus four constants governing how hard to look. The enrichment itself
// ported early, with the first declarative definitions, as
// ReputationSink/GroupReputationSink -- what did not port were the
// constants, which main.go was left duplicating: a literal 8 with a
// comment saying it was kept in sync by hand with internal/detect's
// unexported value "until that pool is deleted."
//
// This definition is where they land. It exists so the policy has the
// same envelope, the same persistence and the same operator-facing
// tunability as everything else the engine evaluates, rather than being
// a magic number in process wiring -- and so deleting internal/detect
// does not leave that number with no partner to be in sync with.
//
// # It evaluates nothing, deliberately
//
// Evaluate is a no-op, like global_spike's: there is nothing a single
// event means to a lookup policy. It is still registered rather than
// held aside, because "every shipped definition is registered and
// evaluated" is a property worth being able to state without exceptions
// -- the alternative is a special case in main.go's build loop, which is
// exactly the kind of quiet divergence the catalogue-vs-builder
// agreement tests exist to catch.
type reputationDefinition struct {
	programmaticBase

	policy ReputationPolicy
}

func buildReputationDefinition(def Definition, _ ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	concurrency, err := paramInt(params, "lookupConcurrency")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	timeout, err := paramDuration(params, "lookupTimeout")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	sampleSize, err := paramInt(params, "groupSampleSize")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minSignificant, err := paramInt(params, "groupMinSignificantSamples")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	return &reputationDefinition{
		programmaticBase: programmaticBase{def: def},
		policy: ReputationPolicy{
			Concurrency:                concurrency,
			Timeout:                    timeout,
			GroupSampleSize:            sampleSize,
			GroupMinSignificantSamples: minSignificant,
		},
	}, nil
}

// Evaluate satisfies Evaluated and does nothing -- see this type's own
// doc comment.
func (d *reputationDefinition) Evaluate(store.Event) {}

// Policy returns the lookup policy this definition carries, for the
// wiring that builds the reputation sinks.
func (d *reputationDefinition) Policy() ReputationPolicy { return d.policy }

// ReputationPolicyFrom reads the shipped reputation definition's policy
// out of a definitions store, falling back to DefaultReputationPolicy
// when there is no such definition, it is unavailable, or it fails to
// build.
//
// The fallback is deliberate and is not a silent swallow: a deployment
// whose definitions document predates this definition, or whose
// reputation entry is somehow unbuildable, should keep looking things up
// at the shipped policy rather than stop enriching flags altogether. The
// enrichment is best-effort by design at every other level too (a
// saturated pool skips, a failed lookup is dropped), so degrading to the
// shipped numbers is consistent with the rest of the path. Seeding makes
// the fallback rare in practice -- SeedShippedDefinitions adds this
// definition on every boot where it is missing.
func ReputationPolicyFrom(s *DefinitionsStore) ReputationPolicy {
	if s == nil {
		return DefaultReputationPolicy()
	}
	stored, ok := s.Get("reputation")
	if !ok || !stored.Available {
		return DefaultReputationPolicy()
	}
	built, err := BuildShippedProgrammaticDefinition(stored.Definition, ShippedDeps{})
	if err != nil {
		return DefaultReputationPolicy()
	}
	rd, ok := built.(*reputationDefinition)
	if !ok {
		return DefaultReputationPolicy()
	}
	return rd.Policy()
}

// NonReplayableReason satisfies NonReplayable, and this is the case
// issue #403 named first and by name.
//
// A replay-time lookup returns what an address's reputation is *now*.
// Using that to answer "what would this have said at the time each corpus
// event occurred" silently mixes the two: an address whose abuse score
// climbed last week reads as having been bad all along, and one that has
// since been cleaned up reads as having always been fine. The receipt
// would carry the authority of a count while being a statement about
// today's third-party data.
//
// There is a second, independent reason, the same one netclass gives:
// this definition produces no emissions at all. It raises no flag and
// crosses no threshold -- it enriches episodes other definitions raised
// -- so "would have fired N times" has no answer here rather than an
// unknown one.
//
// Neither is fixed by a longer corpus, so this is declared once rather
// than declined per call.
func (d *reputationDefinition) NonReplayableReason() string {
	return "reputation raises nothing of its own -- it enriches other definitions' episodes -- so there is no emission count a replay receipt could report; and a lookup made during replay returns an address's reputation today, which is not evidence about what would have been true when each corpus event actually occurred"
}
