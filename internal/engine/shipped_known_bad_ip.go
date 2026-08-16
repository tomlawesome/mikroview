// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("known_bad_ip", buildKnownBadIPDefinition)
}

// knownBadIPConfidence is the confidence a blocklist match is raised at,
// and the floor it applies to any other currently-active source-keyed
// flag for the same address. Lifted unchanged from internal/detect, and
// deliberately high: unlike a crowd-sourced abuse score, Spamhaus DROP is
// hand-curated to only include netblocks Spamhaus is confident are
// entirely malicious-controlled, which is about as strong a signal as
// this codebase has -- stronger than the Tor-exit and hosting-provider
// floors (60/30), though deliberately short of 100, since no automated
// signal should ever claim absolute certainty.
//
// It is the shipped default of this definition's confidence param rather
// than a hard-coded value, so an operator who trusts their own feed less
// (or more) can say so.
const knownBadIPConfidence = 90

// reinforcedFlagTypes is every shipped definition whose Target is a plain
// source address -- the set a reinforcement pass can usefully raise a
// confidence floor on, shared by known_bad_ip and netclass exactly as
// internal/detect's knownBadReinforcedTypes was.
//
// Membership is structural, not a preference, which is why it is not a
// param (see KnownBadIPParamSchema): flags.Store.RaiseConfidenceFloor
// matches its target exactly, so a definition whose target is a
// destination port ("port 22"), a rule label, a composite
// ("<ip> -> port N"), a device ID or the literal "global" could never be
// matched by a source address -- listing it would not tune anything, it
// would simply never fire.
//
// known_bad_ip itself is excluded: it already carries this confidence
// directly on the flag it raises, so reinforcing it would be a no-op
// against itself.
var reinforcedFlagTypes = []FlagType{
	FlagType(flags.TypePortScan),
	FlagType(flags.TypeActivitySpike),
	FlagType(flags.TypeCriticalPort),
	FlagType(flags.TypeOutboundAnomaly),
	FlagType(flags.TypeInternalRecon),
	FlagType(flags.TypeLowSlowScan),
	FlagType(flags.TypeOffHoursActivity),
}

// knownBadIPDefinition is known_bad_ip ported onto the chassis (issue
// #405, originally #113 Part B): a source address matching the locally
// cached blocklist.
//
// # Two jobs, and the second is why it declares an order
//
// It raises its own flag, and it raises the confidence floor of every
// other active source-keyed flag for the same address. The second half
// only works if every definition that could raise such a flag has
// already run for this same event: flags.Store.RaiseConfidenceFloor
// no-ops on a target it does not yet know about, so running early costs
// a silently missing floor rather than an error -- the worst kind of bug
// to have.
//
// internal/detect guaranteed that by writing the call last in one
// function. The engine cannot: definitions are separate registrations and
// a map has no order. So this declares ReinforcementOrder through the
// Ordered contract, and Engine.evaluateEvent iterating a sorted slice is
// what makes it an invariant rather than an accident. See
// TestShippedKnownBadIPReinforcesAFlagRaisedByTheSameEvent, which pins
// the guarantee end to end through two real definitions rather than only
// through the chassis-level ordering test.
//
// # Not scoped, and that is inherited rather than assumed
//
// internal/detect ran this outside its DetectorName/Scope machinery
// entirely -- a blocklist match is a fact about an address, not a pattern
// over a window that an operator would narrow by host or port. It gets
// the envelope every definition has, and its Scope is consulted for the
// source address only (scopeMatchesSource), so a deployment that does
// narrow it gets what it asked for; the shipped default is unscoped,
// which is exactly today's behaviour.
type knownBadIPDefinition struct {
	programmaticBase

	confidence int

	knownBad KnownBadIPLookup
	flagsAPI ConfidenceFloorRaiser
}

func buildKnownBadIPDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	confidence, err := paramInt(params, "confidence")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	return &knownBadIPDefinition{
		programmaticBase: programmaticBase{def: def, order: ReinforcementOrder},
		confidence:       confidence,
		knownBad:         deps.KnownBad,
		flagsAPI:         deps.Flags,
	}, nil
}

// Evaluate satisfies Evaluated.
//
// A local lookup is an in-memory binary search, not a network call, so
// this runs synchronously and unconditionally on every applicable event
// -- unlike the reputation path's async, pool-limited design.
func (d *knownBadIPDefinition) Evaluate(e store.Event) {
	if !d.def.Enabled || d.knownBad == nil {
		return
	}
	if !isPublicIPAddress(e.SrcIP) || !scopeMatchesSource(d.def.Scope, e.SrcIP) {
		return
	}
	label, cidr, ok := d.knownBad.MatchIP(e.SrcIP)
	if !ok {
		return
	}

	confidence := d.confidence
	d.emit(Emission{
		Target:     e.SrcIP,
		Detail:     fmt.Sprintf("matches %s (%s)", label, cidr),
		Confidence: &confidence,
		// No Evidence: internal/detect passed flags.Evidence{} explicitly.
		Country:   e.SrcCountry,
		SourceIP:  e.SrcIP,
		EventTime: e.ReceivedAt,
	})

	if d.flagsAPI == nil {
		return
	}
	for _, t := range reinforcedFlagTypes {
		d.flagsAPI.RaiseConfidenceFloor(t, e.SrcIP, d.confidence)
	}
}

// NonReplayableReason satisfies NonReplayable.
//
// This is #403's reputation case exactly, only with a local cache instead
// of a remote API. The blocklist this consults is today's download: a
// netblock added to Spamhaus DROP last week reads as matched for every
// event in the corpus, including ones from before it was listed, and one
// delisted since reads as clean for events from while it was still on
// the list. A replay would therefore report what this definition says
// now about addresses seen then -- a statement about the feed's current
// contents dressed up as a statement about past traffic.
//
// The reinforcement half is not replayable at all for a second reason
// worth stating separately: raising the confidence floor of another
// definition's flag is an effect on live flags.Store state, not an
// emission. There is no count of it to report, and simulating it would
// mean replaying every other definition in lockstep and inventing a
// notion of "flags that would have existed", which is a different
// contract from the one Receipt offers.
//
// Neither is fixed by a longer corpus, so this is declared once rather
// than declined per call. If blocklist snapshots ever gained
// listed-at/delisted-at history, the first half would be worth
// revisiting; the second would not.
func (d *knownBadIPDefinition) NonReplayableReason() string {
	return "known_bad_ip matches against the blocklist as it stands today, so a replay would report what the current feed says about addresses seen in the past rather than what was true at the time; its reinforcement pass is not an emission at all, but an effect on other definitions' live flags, which a receipt has no way to count"
}
