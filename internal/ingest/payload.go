// SPDX-License-Identifier: AGPL-3.0-only

// Package ingest is the payload schema and validation for issue #186's
// RouterOS push ingest (step 2): what a router-side script may POST to
// POST /api/ingest/routeros, and the strict decoding that stands between
// that attacker-shaped input and the rest of mikroview.
//
// This package only knows how to turn bytes into validated Go values. It
// has no opinion on what happens to those values afterward -- applying
// them (step 4, additive-only) is the API handler's job, not this
// package's, the same separation internal/routeros draws between parsing
// a log line and deciding what a detector does with it.
package ingest

import (
	"encoding/json"
	"fmt"
	"math"
)

// Kind identifies which RouterOS data source a payload's records came
// from -- issue #186 step 4b's survey of what a read,test script can see
// on a real router. Deliberately closed rather than an open string: this
// is the full set of pushable data this build understands, and an
// unrecognised Kind is refused by DecodePayload rather than silently
// accepted and then never applied to anything.
type Kind string

const (
	KindAddressList        Kind = "address-list"
	KindFilterRule         Kind = "filter-rule"
	KindNATRule            Kind = "nat-rule"
	KindDNSStatic          Kind = "dns-static"
	KindDHCPLease          Kind = "dhcp-lease"
	KindARP                Kind = "arp"
	KindWireguardInterface Kind = "wireguard-interface"
	KindWireguardPeer      Kind = "wireguard-peer"
)

// AddressListEntry mirrors /ip/firewall/address-list. Dynamic separates
// operator-authored entries from ones a rule (e.g. a port-scan blocklist
// action) generated -- see issue #186 step 4b.
type AddressListEntry struct {
	List    string `json:"list"`
	Address string `json:"address"`
	Comment string `json:"comment"`
	Dynamic bool   `json:"dynamic"`
}

// FilterRule mirrors one /ip/firewall/filter rule. Ordinal is RouterOS's
// own display order (0-based, as `print` shows it) -- a display
// affordance only, since it shifts whenever rules are added, removed or
// reordered and never appears in a log line (see issue #186 step 4c).
// LogPrefix is what actually resolves a log line back to a rule, and
// only when the operator has set one.
type FilterRule struct {
	Ordinal        RouterOSInt `json:"ordinal"`
	Comment        string      `json:"comment"`
	Chain          string      `json:"chain"`
	Action         string      `json:"action"`
	SrcAddressList string      `json:"srcAddressList"`
	LogPrefix      string      `json:"logPrefix"`
}

// NATRule mirrors one /ip/firewall/nat rule. Display-table shape only
// (issue #186 step 4c: "NAT uses the second shape only") -- a log line
// gives a translation result, never which rule performed it, so there is
// no LogPrefix/event-resolution field here the way FilterRule has one.
type NATRule struct {
	Ordinal RouterOSInt `json:"ordinal"`
	Comment string      `json:"comment"`
	Chain   string      `json:"chain"`
	Action  string      `json:"action"`
}

// DNSStaticEntry mirrors one /ip/dns/static entry.
type DNSStaticEntry struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// DHCPLease mirrors one /ip/dhcp-server/lease entry -- the richest host
// identity source on a real LAN per issue #186 step 4b.
type DHCPLease struct {
	Hostname string `json:"hostname"`
	MAC      string `json:"mac"`
	Address  string `json:"address"`
}

// ARPEntry mirrors one /ip/arp entry -- the fallback identity source
// where DHCP is not in play (issue #186 step 4b).
type ARPEntry struct {
	Address string `json:"address"`
	MAC     string `json:"mac"`
}

// WireguardInterface mirrors one /interface/wireguard interface.
type WireguardInterface struct {
	Name       string      `json:"name"`
	Comment    string      `json:"comment"`
	PublicKey  string      `json:"publicKey"`
	ListenPort RouterOSInt `json:"listenPort"`
}

// WireguardPeer mirrors one /interface/wireguard/peers entry.
// AllowedAddress is what maps a CIDR to a named peer (issue #186 step
// 4b: "so traffic from it can read 'branch office'"). Only public keys
// ever appear here -- confirmed against a real router that private keys
// are absent from a read,test script's view entirely (step 4b).
type WireguardPeer struct {
	PublicKey       string `json:"publicKey"`
	AllowedAddress  string `json:"allowedAddress"`
	EndpointAddress string `json:"endpointAddress"`
	Comment         string `json:"comment"`
}

// RouterOSInt decodes an integer that RouterOS's :serialize to=json may
// emit as a float -- e.g. a port field arrives as 443.000000, not 443
// (measured against a real router; see
// docs/decisions/routeros-ingest-spike.md's step 2 landmine note). A
// plain int field rejects that shape outright, which would mean this
// schema refuses real payloads from real routers. This accepts a JSON
// number in either shape and requires it represent a whole number in
// int32 range, which a genuinely fractional or oversized value never
// legitimately would -- so it still rejects garbage, just not RouterOS's
// own encoding of an integer.
type RouterOSInt int

func (n *RouterOSInt) UnmarshalJSON(data []byte) error {
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f != math.Trunc(f) {
		return fmt.Errorf("ingest: expected a whole number, got %v", f)
	}
	if f < math.MinInt32 || f > math.MaxInt32 {
		return fmt.Errorf("ingest: number %v out of range", f)
	}
	*n = RouterOSInt(f)
	return nil
}
