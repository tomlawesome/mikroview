// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"github.com/tomlawesome/mikroview/internal/flags"
)

// known_bad_ip moved to internal/engine as a shipped programmatic
// definition (issue #405, see shipped_known_bad_ip.go), taking
// knownBadIPConfidence, the blocklist lookup and the RaiseConfidenceFloor
// reinforcement pass with it -- including the "runs last" requirement,
// which is a declared ReinforcementOrder on the chassis rather than a
// call written at the bottom of one function.
//
// knownBadReinforcedTypes stays here only until netclass follows it in
// the very next port, since that pass shares this exact set.

// knownBadReinforcedTypes is every flags.Type whose Target convention is
// a plain source IP (see flags.Flag.Target's doc comment) -- the set a
// synchronous reinforcement pass can usefully raise a confidence floor
// on. Every type whose target is something other than a plain source IP
// (TypeDistributedBruteForce's port, TypeRuleSpike/TypeStaleRule's rule
// label, TypeRepeatedDrops' "ip -> port N" composite,
// TypeDeviceSilence's device ID, TypeGlobalSpike/TypeNewDevice's fixed
// non-IP targets) is excluded, since RaiseConfidenceFloor's target must
// match exactly.
var knownBadReinforcedTypes = []flags.Type{
	flags.TypePortScan,
	flags.TypeActivitySpike,
	flags.TypeCriticalPort,
	flags.TypeOutboundAnomaly,
	flags.TypeInternalRecon,
	flags.TypeLowSlowScan,
	flags.TypeOffHoursActivity,
}
