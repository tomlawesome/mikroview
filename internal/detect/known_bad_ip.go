package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/blocklist"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// knownBadIPLookup is the subset of *blocklist.Blocklist's API
// internal/detect depends on -- kept as a small interface (rather than
// depending on the concrete type) purely so tests can inject a fake
// without needing a real Spamhaus/Emerging Threats fetch, same reasoning
// reputationLookup's doc comment gives for the AbuseIPDB-informed path.
type knownBadIPLookup interface {
	Match(ip string) (blocklist.MatchResult, bool)
}

// knownBadIPConfidence is the confidence TypeKnownBadIP is raised at,
// and the floor applied to any other currently-active source-IP-keyed
// flag for the same target (see knownBadReinforcedTypes below) --
// deliberately high: unlike AbuseIPDB's crowd-sourced abuse score,
// Spamhaus DROP/EDROP is hand-curated specifically to only include
// netblocks Spamhaus is confident are entirely malicious-controlled
// (see internal/blocklist's doc comment) -- a match is about as strong
// a signal as this codebase has, stronger than
// reputation.TorExitNodeFloor/HostingProviderFloor (60/30), though
// deliberately short of 100 since no automated signal should ever
// claim absolute certainty.
const knownBadIPConfidence = 90

// knownBadReinforcedTypes is every flags.Type whose Target convention is
// a plain source IP (see flags.Flag.Target's doc comment) -- the set a
// synchronous local-blocklist match can usefully reinforce via
// RaiseConfidenceFloor, mirroring the role internal/detect's async
// reputation-informed checks already play for these same flag types (see
// maybeCheckReputation). TypeKnownBadIP itself is excluded (it already
// gets this confidence directly via AddWithDetail in observeKnownBadIP);
// every type whose target is something other than a plain source IP
// (TypeDistributedBruteForce's port, TypeRuleSpike/TypeStaleRule's rule
// label, TypeRepeatedDrops' "ip -> port N" composite,
// TypeDeviceSilence's device ID, TypeGlobalSpike/TypeNewDevice's fixed
// non-IP targets) is excluded too, since RaiseConfidenceFloor's target
// must match exactly.
var knownBadReinforcedTypes = []flags.Type{
	flags.TypePortScan,
	flags.TypeActivitySpike,
	flags.TypeCriticalPort,
	flags.TypeOutboundAnomaly,
	flags.TypeInternalRecon,
	flags.TypeLowSlowScan,
	flags.TypeOffHoursActivity,
}

// WithKnownBadIPs attaches an optional local blocklist matcher -- see
// WithReputation's doc comment for the same nil-is-a-valid-no-op,
// chainable, never-set-by-test-helpers contract. Returns d for chaining.
func (d *Detector) WithKnownBadIPs(bl knownBadIPLookup) *Detector {
	d.knownBad = bl
	return d
}

// observeKnownBadIP checks e.SrcIP against the locally-cached blocklist
// (issue #113 Part B) and, on a match, raises TypeKnownBadIP directly --
// unlike every other per-event detector in this package, this isn't
// gated by DetectorName/Scope (see flags.TypeKnownBadIP's doc comment
// for why). A local lookup is a plain in-memory binary search (see
// internal/blocklist), not a network call, so this runs synchronously
// and unconditionally on every applicable event, unlike
// maybeCheckReputation's async, pool-limited design.
//
// Called last in Observe, after every other per-event detector, so any
// flag newly raised by *this same event* (e.g. this same burst also
// just crossed the port-scan threshold) already exists in fs by the
// time the knownBadReinforcedTypes loop below calls
// RaiseConfidenceFloor -- calling this any earlier would silently miss
// reinforcing a flag raised later in the same Observe call
// (RaiseConfidenceFloor no-ops on a target it doesn't yet know about,
// see its own doc comment).
func (d *Detector) observeKnownBadIP(e store.Event, now time.Time) {
	if d.knownBad == nil || !isPublic(e.SrcIP) {
		return
	}
	match, ok := d.knownBad.Match(e.SrcIP)
	if !ok {
		return
	}

	d.fs.AddWithDetail(flags.TypeKnownBadIP, e.SrcIP,
		fmt.Sprintf("matches %s (%s)", match.Label, match.Range),
		knownBadIPConfidence, flags.Evidence{}, e.SrcCountry, now)

	for _, t := range knownBadReinforcedTypes {
		d.fs.RaiseConfidenceFloor(t, e.SrcIP, knownBadIPConfidence)
	}
}
