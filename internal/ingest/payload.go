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
	KindIPAddress          Kind = "ip-address"
	// KindPPPActive is issue #874's second table: /ppp/active print
	// as-value, one row per currently connected PPP session -- L2TP,
	// PPTP, SSTP and OVPN alike, since RouterOS surfaces all four
	// through the same /ppp/active menu. Unlike the WireGuard tables,
	// there is no separate "configured tunnels" table backing this one:
	// a session exists here only while it is up, which is exactly the
	// presence-means-up reading #874 settled on.
	KindPPPActive Kind = "ppp-active"
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
//
// ConnectionState, InInterface and OutInterface were added for issue
// #408, and nothing reads them yet -- that is deliberate, and the issue
// says so: the field has to be flowing before the consumer designed
// against it (#392's coverage model, phase 2) has any real pushed data
// to be shaped by rather than assumed ones.
//
//   - ConnectionState is RouterOSList, not a plain string, for the same
//     reason WireguardPeer.AllowedAddress is: RouterOS holds
//     connection-state as a *set* (established,related is two values,
//     not one string), and #443 is this schema's paid-for lesson about
//     what a set serialises as -- an array through :serialize to=json,
//     which a plain string field refuses outright. The list type takes
//     the array shape and the joined-string shape both, so neither a
//     RouterOS-native push nor a script that joins by hand is refused,
//     and a negated state ("!invalid") is just an element.
//
//     Absent means unset, which means "any state" -- deliberately not
//     Log's absent-means-false convention, because "matched no state"
//     and "did not match on state at all" are different claims and only
//     the second is true of a rule that carries no connection-state.
//
//   - InInterface/OutInterface are single interface names, so they are
//     plain strings: a rule matches at most one of each (an interface
//     *list* match is a separate RouterOS property, not in this schema),
//     and a name cannot serialise as a number the way a single port
//     does. Negation ("!ether1") is part of the string.
type FilterRule struct {
	Ordinal         RouterOSInt      `json:"ordinal"`
	Comment         string           `json:"comment"`
	Chain           string           `json:"chain"`
	Action          string           `json:"action"`
	SrcAddressList  string           `json:"srcAddressList"`
	LogPrefix       string           `json:"logPrefix"`
	DstPort         RouterOSPortSpec `json:"dstPort"`
	Protocol        string           `json:"protocol"`
	Log             bool             `json:"log"`
	DstAddress      string           `json:"dstAddress"`
	SrcAddress      string           `json:"srcAddress"`
	ConnectionState RouterOSList     `json:"connectionState"`
	InInterface     string           `json:"inInterface"`
	OutInterface    string           `json:"outInterface"`
	// Disabled is what lets a count of this table mean "rules doing
	// something" rather than "rules present" (#701 fact 2). NATRule has
	// carried it since #445; the filter table was never asked for it.
	// A push made before the field was added omits it, which decodes as
	// false -- enabled -- so an old push over-counts rather than
	// under-counts, and re-pushing corrects it.
	Disabled bool `json:"disabled"`
}

// NATRule mirrors one /ip/firewall/nat rule.
//
// #186 step 4c ruled that a NAT log line "gives a translation result,
// never which rule performed it", and read that as meaning NAT could
// only ever be a display table. #445 kept the observation and dropped
// the conclusion. The line does not name the rule -- but neither does a
// filter line, and filter events resolve anyway, through the log-prefix
// the operator chose to put on the rule. LogPrefix is that same
// operator-set join, and it says exactly as much for NAT as it does for
// filter: the router did not identify the rule, the operator labelled
// it. A rule with no LogPrefix stays unanswerable for a translation, and
// #445's popup says so rather than guessing.
//
// Everything from ToAddresses down was added for issue #408 as #445's
// stated prerequisite, and #445 now reads it: an *unlogged* translation
// cannot name a rule, so the popup instead partitions the table by what
// the event positively contradicts (wrong chain, wrong protocol, a port
// outside dst-port, a disabled rule) and shows the reason against each
// exclusion. The old ordinal/chain/action/comment shape gives that
// nothing to compute, since every rule is equally consistent with
// everything -- which is the state the popup's layer-3 floor detects and
// says out loud.
//
// Shapes follow FilterRule's already-verified ones rather than fresh
// assumptions, since these are the same RouterOS properties on a sibling
// menu: ports through RouterOSPortSpec (a single port serialises as a
// JSON number, a list or range as a string), addresses and interface
// names as plain strings (dots and names cannot become numbers), and
// Disabled/Dynamic as plain bools where an absent key means false the
// way FilterRule.Log already documents.
type NATRule struct {
	Ordinal      RouterOSInt      `json:"ordinal"`
	Comment      string           `json:"comment"`
	Chain        string           `json:"chain"`
	Action       string           `json:"action"`
	LogPrefix    string           `json:"logPrefix"`
	ToAddresses  string           `json:"toAddresses"`
	ToPorts      RouterOSPortSpec `json:"toPorts"`
	DstPort      RouterOSPortSpec `json:"dstPort"`
	Protocol     string           `json:"protocol"`
	InInterface  string           `json:"inInterface"`
	OutInterface string           `json:"outInterface"`
	SrcAddress   string           `json:"srcAddress"`
	DstAddress   string           `json:"dstAddress"`
	Disabled     bool             `json:"disabled"`
	Dynamic      bool             `json:"dynamic"`
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
//
// LastHandshake, CurrentEndpointAddress, RX, TX and Disabled were added
// for issue #874, City 9's ingest side: today's push says which peers
// are configured but nothing about whether a tunnel is carrying
// traffic. All five are optional, like every field added to this
// schema after its first release -- a push script that predates #874
// decodes fine and leaves them at their zero value, which this
// package's callers must read as "not reported," never as "peer is
// down" or "zero bytes."
//
//   - LastHandshake mirrors RouterOS's own last-handshake property
//     verbatim: a time-*since* string ("1m23s", "2h13m5s"), relative to
//     the router's own clock at push time, so no clock agreement
//     between mikroview and the router is needed to use it. Empty when
//     the peer has never handshaken -- RouterOS omits the property
//     entirely in that case rather than sending an empty string, and a
//     plain string field already reads an absent key as "", so no
//     special-case decoding is needed. Kept as an opaque string here
//     deliberately: turning it into a duration is interpretation, and
//     issue #874 puts that step "on the API side," not in this
//     decode-only package.
//
//   - CurrentEndpointAddress mirrors current-endpoint-address, the
//     address the last packet actually arrived from -- distinct from
//     EndpointAddress, which is the peer's *configured* endpoint and
//     may be a DNS name or simply unset for a road-warrior peer that
//     connects from wherever it currently is.
//
//   - RX and TX are RouterOSInt64, not RouterOSInt: a byte counter on a
//     long-lived or fast tunnel outgrows int32 (2^31 bytes is 2GiB)
//     long before it outgrows anything realistic, and :serialize
//     to=json emits these as floats the same way it does every other
//     RouterOS integer -- the same landmine RouterOSInt exists for, at
//     a width that actually fits a byte counter.
//
//   - Disabled follows FilterRule.Log's convention: RouterOS omits the
//     property rather than sending "disabled":false, so a plain bool
//     already reads "absent" as false, which is the correct reading
//     for a property that defaults to enabled.
//
// Interface was added afterward, once #874's own API layer found it had
// no way to attribute a peer to a specific WireGuard interface: RouterOS
// carries that as the peer's own "interface" property, mirrored here
// verbatim. Optional like the five above -- a push script that predates
// it (or #874 itself) leaves every peer's Interface empty, which
// internal/api reads as "attribution unavailable" and falls back to
// treating every peer as belonging to every interface, rather than as
// "this peer belongs to no interface."
type WireguardPeer struct {
	PublicKey       string       `json:"publicKey"`
	AllowedAddress  RouterOSList `json:"allowedAddress"`
	EndpointAddress string       `json:"endpointAddress"`
	Comment         string       `json:"comment"`

	LastHandshake          string        `json:"lastHandshake,omitempty"`
	CurrentEndpointAddress string        `json:"currentEndpointAddress,omitempty"`
	RX                     RouterOSInt64 `json:"rx,omitempty"`
	TX                     RouterOSInt64 `json:"tx,omitempty"`
	Disabled               bool          `json:"disabled,omitempty"`
	Interface              string        `json:"interface,omitempty"`
}

// IPAddressEntry mirrors one /ip/address entry -- issue #627: an
// interface's own configured address, distinct from ARPEntry (what the
// router has observed answering) and DHCPLease (what it handed out).
// Address is the CIDR RouterOS shows (e.g. "192.168.1.1/24"); Network is
// the address's own network property, not derived from it here, since
// this package only decodes what a router says rather than recomputing
// it.
type IPAddressEntry struct {
	Address   string `json:"address"`
	Network   string `json:"network"`
	Interface string `json:"interface"`
	Comment   string `json:"comment"`
}

// PPPActiveSession mirrors one /ppp/active row -- issue #874's second
// table, the state source for L2TP, PPTP, SSTP and OVPN tunnels alike
// (RouterOS surfaces all four through this same menu). CallerID mirrors
// caller-id (the remote address or identifier RouterOS recorded for
// this session, protocol-dependent), and Uptime is a RouterOS
// time-*elapsed* string ("1m23s", "4w2d5h24m35s") the same shape and
// for the same reason WireguardPeer.LastHandshake is -- kept opaque
// here, interpreted where issue #874 puts that step.
//
// There is no companion "configured PPP tunnels" table: a row exists
// here only while RouterOS considers the session active, so presence
// in a page is itself the up signal, and absence is the down one --
// this package has nothing further to validate about that meaning.
type PPPActiveSession struct {
	Name     string `json:"name"`
	Service  string `json:"service"`
	Address  string `json:"address"`
	CallerID string `json:"callerId"`
	Uptime   string `json:"uptime"`
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

// RouterOSInt64 is RouterOSInt for a field whose legitimate range
// outgrows int32 -- issue #874's WireguardPeer.RX/TX, a byte counter
// that a long-lived or fast tunnel pushes past 2GiB (int32's ceiling)
// without doing anything unusual. :serialize to=json still emits it as
// a JSON float the same way it does every RouterOSInt field, so the
// decoding is identical; only the accepted range changes. float64
// represents every integer up to 2^53 exactly, several orders of
// magnitude past any byte counter this schema will ever see, so the
// same whole-number check RouterOSInt uses is still exact here.
type RouterOSInt64 int64

func (n *RouterOSInt64) UnmarshalJSON(data []byte) error {
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f != math.Trunc(f) {
		return fmt.Errorf("ingest: expected a whole number, got %v", f)
	}
	if f < -(1<<53) || f > (1<<53) {
		return fmt.Errorf("ingest: number %v out of range", f)
	}
	*n = RouterOSInt64(f)
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
