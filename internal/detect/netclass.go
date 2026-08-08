// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"time"

	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/store"
)

// netClassLookup is the subset of *netclass.Classifier's API this package
// depends on -- same small-interface-for-testability reasoning as
// knownBadIPLookup/reputationLookup, so a test can inject a fake table
// without needing a real feed fetch.
type netClassLookup interface {
	Lookup(ip string) netclass.Class
}

// netclassVPNFloor is the confidence floor a commercial-VPN-exit match
// contributes. There is no existing constant to reuse here the way Tor
// has reputation.TorExitNodeFloor -- AbuseIPDB carries no distinct "VPN"
// signal, only isTor and usageType. Set between
// reputation.HostingProviderFloor (30, a weak signal covering ordinary
// hosting/business use as well as abuse) and reputation.TorExitNodeFloor
// (60): X4BNet's VPN list is two orders of magnitude more precise than
// its own datacenter list (~0.08% of IPv4 vs ~10%, #114's research), so
// it earns more weight than generic hosting, but a VPN exit is still a
// much weaker claim of malice than Tor. A starting point, not a
// calibrated value -- same caveat reputation.go's own constants carry.
const netclassVPNFloor = 40

// WithNetClass attaches an optional network-class lookup -- see
// WithReputation's doc comment for the same nil-is-a-valid-no-op,
// chainable, never-set-by-test-helpers contract. Returns d for chaining.
func (d *Detector) WithNetClass(nc netClassLookup) *Detector {
	d.netclass = nc
	return d
}

// observeNetClass reinforces the confidence of an already-active,
// SrcIP-keyed flag when e's source is a known Tor exit or commercial VPN
// (issue #114's rescoped, direction-aware plan). It never raises a flag
// on its own -- RaiseConfidenceFloor is a no-op against a target fs does
// not already know about (see its own doc comment), so an unclassified
// or classified-but-otherwise-quiet source contributes nothing here,
// matching the issue's explicit non-goal ("flagging any connection from
// a VPN/VPS IP on its own").
//
// Direction-gated, which #114's research called the single highest-value
// refinement: only e.SrcIP is classified, and only when it is public and
// reaching a private (LAN) destination -- i.e. traffic arriving from the
// outside, the "genuinely unusual" case the research measured. The
// reverse (a LAN host's outbound traffic classified via e.DstIP) is
// deliberately never checked: outbound to a cloud/VPN range is ~all
// modern traffic (Private Relay, WARP, ordinary CDN-backed browsing) and
// the research found it would contribute nothing but noise.
//
// CategoryDatacenter and CategoryPrivacyRelay never reinforce anything,
// by design: datacenter space alone covers >10% of routable IPv4 (weak
// signal, kept display-only rather than assigned an arbitrary small
// weight that would still mostly be noise), and PrivacyRelay exists
// specifically to identify traffic that must never read as suspicious.
// Only CategoryTor and CategoryVPN -- the two high-precision categories
// -- ever call RaiseConfidenceFloor.
//
// Called last in Observe, after every other per-event detector and after
// observeKnownBadIP, for the identical reason that function documents:
// any flag raised earlier in this same Observe call must already exist
// in fs before this reinforcement pass looks for it.
func (d *Detector) observeNetClass(e store.Event, now time.Time) {
	if d.netclass == nil || !isPublic(e.SrcIP) || e.DstIP == "" || isPublic(e.DstIP) {
		return
	}

	class := d.netclass.Lookup(e.SrcIP)
	if !class.Matched {
		return
	}

	var floor int
	switch class.Category {
	case netclass.CategoryTor:
		floor = reputation.TorExitNodeFloor
	case netclass.CategoryVPN:
		floor = netclassVPNFloor
	default:
		return
	}

	for _, t := range knownBadReinforcedTypes {
		d.fs.RaiseConfidenceFloor(t, e.SrcIP, floor)
	}
}
