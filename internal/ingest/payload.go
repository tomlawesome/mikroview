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
	"strconv"
	"strings"
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
//
// DstPort and Protocol were added for issue #243 slice 5 (suggesting
// watchlist entries from a rule's already-blocked ports). DstPort is
// RouterOSPortSpec, not a plain string -- verified against a real
// RouterOS 7.23.3 router: a rule with a single numeric port
// (dst-port=3389) serialises that as a JSON *number* (3389.000000, the
// same float-landmine RouterOSInt exists for), while a rule with a list
// or range (dst-port=22,23 or 1000-2000) serialises as a JSON *string*.
// A plain Go string field rejects the numeric shape outright, which
// would mean this schema refuses real payloads from real routers for
// every rule that scopes exactly one port -- the common case.
//
// Log, DstAddress and SrcAddress were added for #274 item 1 (telling an
// operator when a watchlist entry can never match, because no rule logs
// traffic in its scope). All three verified against a real RouterOS
// 7.23.3 before being added, since the DstPort case above is the
// standing warning against assuming a shape:
//
//   - Log is the field that actually decides whether a rule can feed
//     mikroview anything, and it was the missing one. #274 framed the
//     blocker as the absent destination address, and that is real, but a
//     rule with log=no produces nothing regardless of its addresses.
//     LogPrefix's presence was the only available proxy and it is a bad
//     one in both directions -- a logging rule with no prefix produces
//     events (as action "unknown"), and there is no prefix without
//     logging only by convention.
//
//     Absent means false. A non-logging rule omits the key entirely
//     rather than serialising "log":false, which is why this is a plain
//     bool: encoding/json leaves it zero, which is the right answer.
//
//   - DstAddress/SrcAddress are always JSON *strings*, unlike DstPort,
//     in every shape RouterOS accepts: a bare address ("203.0.113.9"),
//     a CIDR ("10.0.0.0/8"), a range ("10.0.0.1-10.0.0.5") and a negated
//     form ("!10.0.0.0/8"). A bare IPv4 contains dots, so it cannot
//     serialise as a number the way a single port does. Absent when
//     unset, which means "any address" -- not "no addresses", and the
//     difference decides whether a rule covers an entry.
type FilterRule struct {
	Ordinal        RouterOSInt      `json:"ordinal"`
	Comment        string           `json:"comment"`
	Chain          string           `json:"chain"`
	Action         string           `json:"action"`
	SrcAddressList string           `json:"srcAddressList"`
	LogPrefix      string           `json:"logPrefix"`
	DstPort        RouterOSPortSpec `json:"dstPort"`
	Protocol       string           `json:"protocol"`
	Log            bool             `json:"log"`
	DstAddress     string           `json:"dstAddress"`
	SrcAddress     string           `json:"srcAddress"`
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
//
// AllowedAddress is RouterOSList because a peer holds *several* allowed
// addresses and RouterOS says so on the wire: /interface/wireguard/peers
// print as-value yields allowed-address as an array, so :serialize
// to=json emits a JSON array, and the string field this used to be
// refused it -- with the docs' own reference pattern producing the
// refused payload (issue #443, found on a real deployment: "json: cannot
// unmarshal array into Go struct field WireguardPeer.allowedAddress of
// type string"). Every CIDR is kept rather than the first, because
// naming traffic by peer is the whole point of the field and a peer's
// second subnet is not less named than its first.
type WireguardPeer struct {
	PublicKey       string       `json:"publicKey"`
	AllowedAddress  RouterOSList `json:"allowedAddress"`
	EndpointAddress string       `json:"endpointAddress"`
	Comment         string       `json:"comment"`
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

// RouterOSPortSpec decodes a field that is a JSON *string* when RouterOS
// holds a list or range ("22,23", "1000-2000") and a JSON *number* when
// it holds exactly one port (:serialize to=json's usual float shape,
// e.g. 3389.000000) -- confirmed against a real RouterOS 7.23.3 router
// (issue #243 slice 5), the same kind of shape-depends-on-content
// landmine RouterOSInt exists for. Always decodes to a plain string
// value so a caller never needs to know which JSON shape it arrived as.
type RouterOSPortSpec string

// RouterOSList decodes a field RouterOS holds as a *set* of values: a
// WireGuard peer's allowed addresses, a rule's connection-state match.
// Such a field arrives as a JSON **array of strings** through
// :serialize to=json, which is what issue #443 cost a live deployment a
// refused push to establish -- the same shape-depends-on-content family
// RouterOSInt and RouterOSPortSpec exist for, and the third member of
// it this schema has met.
//
// A bare JSON string is accepted too, split on commas, for two real
// callers: a script that joins the array by hand (the workaround #443
// documented while the schema was still string-only), and RouterOS's own
// joined rendering of a set ("established,related"). Either way a caller
// reads a []string and never needs to know which shape it arrived as --
// the same promise RouterOSPortSpec makes.
//
// Absent decodes to nil, which means "unset" -- never "matches nothing".
// A nil list marshals back as [] rather than null, so a JSON consumer
// (the rule-table endpoints) always sees an array.
type RouterOSList []string

func (l *RouterOSList) UnmarshalJSON(data []byte) error {
	var items []string
	if err := json.Unmarshal(data, &items); err == nil {
		*l = items
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("ingest: expected a list of strings or a comma-separated string: %w", err)
	}
	if strings.TrimSpace(s) == "" {
		*l = nil
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	*l = out
	return nil
}

func (l RouterOSList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(l))
}

// String renders the list the way RouterOS itself prints a set, for a
// caller that wants one displayable value rather than the elements.
func (l RouterOSList) String() string { return strings.Join(l, ",") }

func (p *RouterOSPortSpec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = RouterOSPortSpec(s)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("ingest: expected a port string or number: %w", err)
	}
	if f != math.Trunc(f) {
		return fmt.Errorf("ingest: expected a whole port number, got %v", f)
	}
	if f < 0 || f > 65535 {
		return fmt.Errorf("ingest: port %v out of range", f)
	}
	*p = RouterOSPortSpec(strconv.FormatFloat(f, 'f', 0, 64))
	return nil
}
