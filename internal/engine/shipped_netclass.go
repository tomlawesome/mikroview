// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("netclass", buildNetClassDefinition)
}

// netclassVPNFloor is the confidence floor a commercial-VPN-exit match
// contributes, lifted unchanged from internal/detect. There was no
// existing constant to reuse the way Tor has one: an abuse-score feed
// carries no distinct "VPN" signal. It sits between the
// hosting-provider floor (30, a weak signal covering ordinary hosting
// and business use as well as abuse) and the Tor-exit floor (60),
// because #114's research measured X4BNet's VPN list as two orders of
// magnitude more precise than its own datacenter list (~0.08% of IPv4
// against ~10%) -- so it earns more weight than generic hosting, while a
// VPN exit is still a much weaker claim of malice than Tor. A starting
// point, not a calibrated value, and now a param rather than a constant
// so an operator can say so.
const netclassVPNFloor = 40

// netClassDefinition is netclass ported onto the chassis (issue #405,
// originally #114's rescoped, direction-aware plan): a source that is a
// known Tor exit or commercial VPN exit, reinforcing the confidence of
// flags other definitions already raised for it.
//
// # It raises nothing
//
// This is the only shipped definition that never emits. Its whole effect
// is RaiseConfidenceFloor, which is a no-op against a target the flag
// store does not already know about -- so an unclassified source, or a
// classified but otherwise quiet one, contributes exactly nothing. That
// is #114's explicit non-goal made structural: "flagging any connection
// from a VPN/VPS IP on its own" is not something this definition can do,
// however the feeds are configured. It is also why its id is the only one
// in the shipped catalogue that is not a flags.Type.
//
// # Direction-gated, which was the highest-value refinement
//
// Only the source is classified, and only when it is public and reaching
// a private destination -- traffic arriving from outside, which #114's
// research measured as the genuinely unusual case. The reverse (a LAN
// host's outbound traffic classified by destination) is deliberately
// never checked: outbound to cloud/VPN ranges is ~all modern traffic
// (Private Relay, WARP, ordinary CDN-backed browsing), and the research
// found it would contribute nothing but noise.
//
// # Two categories reinforce, two never do
//
// Datacenter and privacy-relay matches never raise anything, by design:
// datacenter space alone covers more than 10% of routable IPv4 (weak
// signal, kept display-only rather than given an arbitrary small weight
// that would still mostly be noise), and privacy relays exist precisely
// to identify traffic that must never read as suspicious.
//
// # Ordered, for the same reason known_bad_ip is
//
// It reinforces flags raised by other definitions for the same event, so
// it must run after every one of them -- see knownBadIPDefinition's own
// doc comment, and TestShippedNetClassReinforcesAFlagRaisedByTheSameEvent
// for the end-to-end pin.
type netClassDefinition struct {
	programmaticBase

	torFloor int
	vpnFloor int

	netclass NetClassLookup
	flagsAPI ConfidenceFloorRaiser
}

func buildNetClassDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	torFloor, err := paramInt(params, "torFloor")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	vpnFloor, err := paramInt(params, "vpnFloor")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	return &netClassDefinition{
		programmaticBase: programmaticBase{def: def, order: ReinforcementOrder},
		torFloor:         torFloor,
		vpnFloor:         vpnFloor,
		netclass:         deps.NetClass,
		flagsAPI:         deps.Flags,
	}, nil
}

// netclass category names, as NetClassLookup reports them -- string
// constants rather than an import of internal/netclass's own enum,
// because ShippedDeps deliberately keeps that package out of the
// chassis's dependency set (see NetClassLookup).
const (
	netclassCategoryTor = "tor"
	netclassCategoryVPN = "vpn"
)

// Evaluate satisfies Evaluated.
func (d *netClassDefinition) Evaluate(e store.Event) {
	if !d.def.Enabled || d.netclass == nil || d.flagsAPI == nil {
		return
	}
	if !isPublicIPAddress(e.SrcIP) || e.DstIP == "" || isPublicIPAddress(e.DstIP) {
		return
	}
	if !scopeMatchesSource(d.def.Scope, e.SrcIP) {
		return
	}

	matched, category, _ := d.netclass.LookupClass(e.SrcIP)
	if !matched {
		return
	}

	var floor int
	switch category {
	case netclassCategoryTor:
		floor = d.torFloor
	case netclassCategoryVPN:
		floor = d.vpnFloor
	default:
		return // datacenter, privacy relay: display-only, never reinforcing
	}

	for _, t := range reinforcedFlagTypes {
		d.flagsAPI.RaiseConfidenceFloor(t, e.SrcIP, floor)
	}
}

// NonReplayableReason satisfies NonReplayable.
//
// Two reasons, and the second is the stronger one.
//
// First, the same today's-data problem known_bad_ip and reputation have:
// the classification lists this consults are the ones downloaded now. An
// address that became a Tor exit last week reads as one for every event
// in the corpus, including those from before it was; one that stopped
// being a VPN exit reads as clean for events from while it still was.
//
// Second, and decisively: this definition has no emissions to count. Its
// entire output is a confidence floor applied to flags OTHER definitions
// raised, which a Receipt has no shape for -- "would have fired N times"
// is not a question that has an answer here, because it never fires.
// Reporting zero would be true and useless; reporting the number of
// matched events would be reporting something this definition does not
// claim. A replay that cannot express the effect it is being asked about
// should decline, which is what this does.
func (d *netClassDefinition) NonReplayableReason() string {
	return "netclass raises no flag of its own -- it only reinforces the confidence of flags other definitions raised -- so there is no emission count a replay receipt could report; it also classifies against lists as they stand today, which says nothing about what an address was at the time of a past event"
}
