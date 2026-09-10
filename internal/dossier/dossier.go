// SPDX-License-Identifier: AGPL-3.0-only

// Package dossier assembles everything mikroview already knows about one
// host into a single evidence card (issue #410): what it talks to, on
// which ports and how often, its hardware address and who made it,
// whether its address is a lease or fixed, the names it goes by and
// where each came from, when it was first and last seen, which firewall
// rules its traffic matched, and -- from a small explicit table -- a
// suggested identity that always carries its evidence and a confidence
// in words.
//
// Two rules run through the whole package.
//
// Absent data is reported as absent. Every block says whether it knows
// anything and, when it does not, why: "no DHCP table has been pushed
// from this router" is a different fact from "this address is not a
// lease", and an operator's next step differs between them. Nothing here
// fills a gap with a plausible value.
//
// A suggestion is never a claim. The identity block names the profile
// that matched, the evidence lines that matched it, and how much weight
// that evidence carries -- weak, fair or strong. The card narrows;
// naming is a bonus.
//
// The package is pure: it takes assembled data in and returns the
// dossier, with no store, HTTP or clock dependencies beyond what the
// caller passes. That is what makes the heuristics testable without
// standing up a server, and it keeps internal/api's handler to
// gathering.
package dossier

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/oui"
	"github.com/tomlawesome/mikroview/internal/store"
)

// Limits on how much of a busy host's traffic is listed. A dossier is a
// card an operator reads, not an export: the top handful of each
// dimension is what makes a host recognisable, and the counts of what
// was left out are reported so the card never pretends the list is
// everything.
const (
	maxPeers = 12
	maxPorts = 12
	maxRules = 12
)

// VendorSource is the OUI registry as this package needs it --
// *oui.Registry satisfies it, and a test can satisfy it in six lines.
// Nil means the vendor feed is switched off, which is reported as such
// rather than as an unknown vendor.
type VendorSource interface {
	Lookup(m oui.MAC) oui.Vendor
	Status() oui.Status
}

// Lookups are the naming callbacks the caller already has wired up. All
// are optional: a nil one simply means that kind of name is not shown,
// never that the dossier fails.
type Lookups struct {
	// PortName is the friendly name for a port number, if one is known.
	PortName func(port int) string
	// PeerName is the friendly name for another host's address.
	PeerName func(ip string) string
	// RuleName is the friendly name for a rule label.
	RuleName func(label string) string
	// RuleComment is the comment on the pushed firewall rule carrying
	// this log prefix, when the router has pushed its rule table. The
	// bool is what separates "the router pushed its rules and this one
	// has no comment" from "no rule table has been pushed".
	RuleComment func(device, label string) (string, bool)
}

// Presence is one row the host presence register holds for this address
// -- one per boundary interface it has been seen on. It is the only
// first-seen that survives a restart, so it outranks the event window's
// own oldest event.
type Presence struct {
	Iface     string
	FirstSeen time.Time
	LastSeen  time.Time
	Events    uint64
}

// MACHistory is what the persisted MAC registry holds for the hardware
// address, when it holds anything: mikroview has seen this MAC since
// FirstSeen, which is a longer memory than the event ring's.
type MACHistory struct {
	FirstSeen time.Time
	LastSeen  time.Time
}

// RouterFacts is one router's answer about this address. The Pushed
// booleans are the point of the type: they distinguish "this router's
// DHCP table does not list the address" from "this router has never
// pushed a DHCP table", which is the difference between evidence and
// silence.
type RouterFacts struct {
	Device string

	DHCPPushed   bool
	DHCPPushedAt time.Time
	// Lease is the matching lease, when the table was pushed and
	// contains this address.
	Lease *Lease

	ARPPushed   bool
	ARPPushedAt time.Time
	// ARPMAC is the hardware address this router's ARP table pairs with
	// this address, when it has one.
	ARPMAC string
}

// Lease is a DHCP lease as pushed by a router.
type Lease struct {
	MAC      string
	Hostname string
}

// NameFacts is the naming layer's answer for this address, verbatim
// from naming.Provenance so the dossier reports provenance rather than
// just a string.
type NameFacts struct {
	Name string
	// Source is naming.Provenance.Source: "none", "entity", "config",
	// "router-dns-static", "router-dhcp-lease", "router-wireguard-peer".
	Source string
	// Label is the operator's own saved label, set whether or not it is
	// the name being displayed.
	Label string
}

// Input is everything the caller gathered. Every field is optional
// except IP and Now: a dossier assembled from nothing is a valid
// dossier that says it knows nothing, which is exactly what should
// happen for an address nobody has heard of.
type Input struct {
	IP  string
	Now time.Time

	// Events are the retained events involving this address, in any
	// order.
	Events []store.Event
	// WindowStart is the oldest moment the event ring could have
	// answered for, so "first seen in the window" is never mistaken for
	// "first ever seen".
	WindowStart time.Time
	// EventsTruncated says the caller's query hit its own limit, so
	// Events is the most recent slice of this host's traffic rather
	// than all of it. Reported on the card: a fingerprint read from a
	// truncated sample is still evidence, but it is not a census.
	EventsTruncated bool

	Name     NameFacts
	Presence []Presence
	// EventMAC is the source MAC carried on this host's own events, if
	// any -- a direct observation, unlike the ARP table's.
	EventMAC   string
	MACHistory *MACHistory
	Routers    []RouterFacts

	Vendors VendorSource
	Lookups Lookups
}

// Dossier is the assembled card.
type Dossier struct {
	IP          string    `json:"ip"`
	GeneratedAt time.Time `json:"generatedAt"`

	Seen     Seen     `json:"seen"`
	Names    Names    `json:"names"`
	MAC      MACBlock `json:"mac"`
	Address  Address  `json:"address"`
	Traffic  Traffic  `json:"traffic"`
	Firewall Firewall `json:"firewall"`
	Identity Identity `json:"identity"`

	// SuggestedProbe is a command the *operator* may run. mikroview
	// never connects to a host on the operator's network -- see the
	// Probe type.
	SuggestedProbe *Probe `json:"suggestedProbe,omitempty"`

	// Absent lists, in plain words, everything this dossier could not
	// answer and why. It is the card's honesty surface: a UI that shows
	// nothing else still has to show this.
	Absent []string `json:"absent,omitempty"`
}

// Seen is when this address was first and last heard from, and how much
// of that is inside the retained event window.
type Seen struct {
	Known bool `json:"known"`
	// FirstSeen, LastSeen and WindowStart are pointers so "never" is
	// absent from the JSON rather than the year 1, which reads as a
	// date and is exactly the kind of filled-in gap this card refuses.
	FirstSeen       *time.Time `json:"firstSeen,omitempty"`
	FirstSeenSource string     `json:"firstSeenSource,omitempty"`
	LastSeen        *time.Time `json:"lastSeen,omitempty"`
	// Events is how many retained events involve this address.
	Events int `json:"events"`
	// WindowStart is the oldest moment those events could come from.
	WindowStart *time.Time `json:"windowStart,omitempty"`
	// Interfaces are the boundary interfaces the host has been seen on.
	Interfaces []string `json:"interfaces,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// Names is the name in use and where it came from.
type Names struct {
	Known bool   `json:"known"`
	Name  string `json:"name,omitempty"`
	// Source is the naming layer's own provenance token.
	Source string `json:"source,omitempty"`
	// SourceNote says what that token means in words.
	SourceNote string `json:"sourceNote,omitempty"`
	// OwnLabel is the operator's saved label, reported separately
	// because a router-pushed name shadows it rather than replacing it.
	OwnLabel string `json:"ownLabel,omitempty"`
	Note     string `json:"note,omitempty"`
}

// MACBlock is the hardware address and everything read off it.
type MACBlock struct {
	Known   bool   `json:"known"`
	Address string `json:"address,omitempty"`
	// Source says how the address was learned: from the host's own
	// events, or from a router's ARP table.
	Source string `json:"source,omitempty"`
	// LocallyAdministered is the bit this issue exists to surface. True
	// means no vendor exists -- see LocallyAdministeredNote.
	LocallyAdministered     bool   `json:"locallyAdministered"`
	LocallyAdministeredNote string `json:"locallyAdministeredNote,omitempty"`
	// GroupNote is set only in the anomalous case of a group/multicast
	// bit on what is meant to be a station address.
	GroupNote string     `json:"groupNote,omitempty"`
	Vendor    oui.Vendor `json:"vendor"`
	// Registry is the vendor feed's own status: source, fetch time,
	// staleness. A vendor name is never shown without it.
	Registry oui.Status `json:"registry"`
	// FirstSeen and LastSeen come from the persisted MAC registry, and
	// are absent rather than zero when it holds nothing for this
	// address.
	FirstSeen *time.Time `json:"firstSeen,omitempty"`
	LastSeen  *time.Time `json:"lastSeen,omitempty"`
	Note      string     `json:"note,omitempty"`
}

// Address reports whether the host holds a DHCP lease or a fixed
// address, strictly from what routers have actually pushed.
type Address struct {
	// Assignment is "lease", "not-a-lease" or "unknown".
	Assignment string `json:"assignment"`
	Note       string `json:"note"`
	// Device is the router whose push answered.
	Device   string `json:"device,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	// LeaseMAC is the MAC the lease is held by, when there is a lease.
	LeaseMAC string `json:"leaseMac,omitempty"`
	// ARPMAC is the MAC a router's ARP table pairs with this address.
	ARPMAC string `json:"arpMac,omitempty"`
	// Consulted names the routers whose pushed state was read.
	Consulted []string `json:"consulted,omitempty"`
}

// Peer is another host this one exchanged traffic with.
type Peer struct {
	IP       string    `json:"ip"`
	Name     string    `json:"name,omitempty"`
	Country  string    `json:"country,omitempty"`
	Scope    string    `json:"scope"` // "local" or "internet"
	Events   int       `json:"events"`
	Ports    []int     `json:"ports,omitempty"`
	LastSeen time.Time `json:"lastSeen"`
}

// PortUse is one port the host used, and in which direction.
type PortUse struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
	Name     string `json:"name,omitempty"`
	// Direction is "out" (the host reached this port on something else)
	// or "in" (something else reached this port on the host).
	Direction string    `json:"direction"`
	Events    int       `json:"events"`
	Peers     int       `json:"peers"`
	LastSeen  time.Time `json:"lastSeen"`
}

// Cadence is the shape of the host's traffic over time -- the "and on
// what cadence" half of a fingerprint. Deliberately coarse: a device
// that talks every five minutes and one that talks in bursts are
// different animals, and finer statistics would invite claims the data
// cannot carry.
type Cadence struct {
	Known bool `json:"known"`
	// Span is between the first and last retained event, in seconds.
	SpanSeconds float64 `json:"spanSeconds,omitempty"`
	// MeanGapSeconds and MedianGapSeconds are between consecutive
	// events.
	MeanGapSeconds   float64 `json:"meanGapSeconds,omitempty"`
	MedianGapSeconds float64 `json:"medianGapSeconds,omitempty"`
	// Shape is "regular", "bursty" or "occasional".
	Shape string `json:"shape,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Traffic is the fingerprint: who it talks to, what it answers on, and
// how often.
type Traffic struct {
	Known bool `json:"known"`
	// Destinations are hosts this host reached.
	Destinations []Peer `json:"destinations,omitempty"`
	// Talkers are hosts that reached this host.
	Talkers []Peer `json:"talkers,omitempty"`
	// Ports is every port seen, in both directions.
	Ports []PortUse `json:"ports,omitempty"`
	// MoreDestinations/MoreTalkers/MorePorts are what the caps above
	// left out, so a truncated list is never read as a complete one.
	MoreDestinations int     `json:"moreDestinations,omitempty"`
	MoreTalkers      int     `json:"moreTalkers,omitempty"`
	MorePorts        int     `json:"morePorts,omitempty"`
	Cadence          Cadence `json:"cadence"`
	Note             string  `json:"note,omitempty"`
}

// RuleMatch is one firewall rule this host's traffic matched.
type RuleMatch struct {
	Label   string `json:"label"`
	Name    string `json:"name,omitempty"`
	Chain   string `json:"chain,omitempty"`
	Action  string `json:"action,omitempty"`
	Device  string `json:"device,omitempty"`
	Comment string `json:"comment,omitempty"`
	// CommentKnown separates "the router pushed its rules and this one
	// carries no comment" from "no rule table has been pushed".
	CommentKnown bool      `json:"commentKnown"`
	Events       int       `json:"events"`
	LastSeen     time.Time `json:"lastSeen"`
}

// Firewall is which rules the host's traffic matched.
type Firewall struct {
	Known bool        `json:"known"`
	Rules []RuleMatch `json:"rules,omitempty"`
	More  int         `json:"more,omitempty"`
	Note  string      `json:"note,omitempty"`
}

// Probe is a command for the operator to run, printed and never
// executed.
//
// mikroview is a passive observer of the operator's network: it reads
// what routers send it and connects to nothing on the LAN. An observer
// that starts probing changes character, and starts appearing in other
// tools' logs as a scanner. So the card may print the probe it would
// suggest; running it is the operator's decision and the operator's
// packet.
type Probe struct {
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
	Note    string `json:"note"`
}

// Assemble builds the dossier. It never fails: an Input with nothing in
// it produces a dossier that says so.
func Assemble(in Input) Dossier {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	d := Dossier{IP: in.IP, GeneratedAt: now}

	var absent []string
	d.Seen, absent = buildSeen(in, absent)
	d.Names, absent = buildNames(in, absent)
	mac, macFacts, absent := buildMAC(in, absent)
	d.MAC = mac
	d.Address, absent = buildAddress(in, absent)
	traffic, fp, absent := buildTraffic(in, absent)
	d.Traffic = traffic
	d.Firewall, absent = buildFirewall(in, absent)
	d.Identity = suggest(fp, macFacts)
	d.SuggestedProbe = suggestProbe(in.IP, fp)
	d.Absent = absent
	return d
}

func buildSeen(in Input, absent []string) (Seen, []string) {
	s := Seen{Events: len(in.Events), WindowStart: timeOrNil(in.WindowStart)}

	var first, last time.Time
	for _, p := range in.Presence {
		if p.Iface != "" {
			s.Interfaces = append(s.Interfaces, p.Iface)
		}
		if !p.FirstSeen.IsZero() && (first.IsZero() || p.FirstSeen.Before(first)) {
			first = p.FirstSeen
		}
		if p.LastSeen.After(last) {
			last = p.LastSeen
		}
	}
	sort.Strings(s.Interfaces)
	if !first.IsZero() {
		s.FirstSeenSource = "the host presence register, which survives restarts"
	}

	// The event window is a weaker source for first-seen -- it only
	// reaches back as far as retention -- so it is used only when the
	// register has nothing, and it says which it is.
	for _, e := range in.Events {
		t := eventTime(e)
		if t.IsZero() {
			continue
		}
		if first.IsZero() || t.Before(first) {
			if s.FirstSeenSource == "" {
				s.FirstSeenSource = "the oldest retained event, so it may only mean the window starts here"
			}
			first = t
		}
		if t.After(last) {
			last = t
		}
	}

	s.FirstSeen, s.LastSeen = timeOrNil(first), timeOrNil(last)
	s.Known = !first.IsZero() || !last.IsZero()
	if !s.Known {
		s.Note = "this address has not been seen at all: no retained event mentions it and the presence register has no row for it"
		absent = append(absent, "first/last seen: nothing has ever been recorded for this address")
	}
	return s, absent
}

// sourceNotes puts the naming layer's provenance tokens into words. A
// token is a fine thing for a UI to switch on and a poor thing to show
// an operator.
var sourceNotes = map[string]string{
	"router-dns-static":     "a static DNS entry pushed by the router, which outranks anything named here",
	"router-dhcp-lease":     "the hostname on the router's own DHCP lease",
	"router-wireguard-peer": "the comment on the router's WireGuard peer",
	"entity":                "a label you saved in mikroview",
	"config":                "an alias in mikroview's config file",
	"none":                  "nothing has named this address",
}

func buildNames(in Input, absent []string) (Names, []string) {
	n := Names{
		Name:     in.Name.Name,
		Source:   in.Name.Source,
		OwnLabel: in.Name.Label,
	}
	n.Known = n.Name != ""
	n.SourceNote = sourceNotes[in.Name.Source]
	if !n.Known {
		if n.SourceNote == "" {
			n.SourceNote = sourceNotes["none"]
		}
		n.Note = "no DNS entry, lease hostname, saved label or config alias names this address"
		absent = append(absent, "names: nothing names this address in any layer")
	}
	return n, absent
}

func buildMAC(in Input, absent []string) (MACBlock, macFacts, []string) {
	var b MACBlock
	var facts macFacts

	raw, source := in.EventMAC, "the host's own events"
	if raw == "" {
		for _, r := range in.Routers {
			if r.ARPMAC != "" {
				raw, source = r.ARPMAC, "the ARP table pushed by "+r.Device
				break
			}
		}
	}
	if raw == "" {
		b.Note = "no event carried a source MAC for this address and no pushed ARP table pairs one with it"
		b.Vendor = oui.Vendor{Reason: "no MAC address to look up"}
		if in.Vendors != nil {
			b.Registry = in.Vendors.Status()
		}
		absent = append(absent, "MAC: no hardware address has been observed for this host")
		return b, facts, absent
	}

	m, ok := oui.Parse(raw)
	if !ok {
		b.Address, b.Source = raw, source
		b.Note = fmt.Sprintf("%q could not be read as a hardware address", raw)
		absent = append(absent, "MAC: the observed address could not be parsed")
		return b, facts, absent
	}

	b.Known = true
	b.Address, b.Source = m.Address, source
	b.LocallyAdministered = m.LocallyAdministered
	if m.LocallyAdministered {
		// The single highest-value line on the card: it redirects the
		// question from "what gadget is this" to "which host made this".
		b.LocallyAdministeredNote = "the locally-administered bit is set, so no vendor exists to look up -- this address was made up by a hypervisor, a container runtime, or a device randomising its MAC"
	}
	if m.Group {
		b.GroupNote = "the group bit is set, which a station address should never have -- treat this address as forged or malformed"
	}
	if in.MACHistory != nil {
		b.FirstSeen, b.LastSeen = timeOrNil(in.MACHistory.FirstSeen), timeOrNil(in.MACHistory.LastSeen)
	}

	if in.Vendors == nil {
		b.Vendor = oui.Vendor{OUI: m.OUI, Reason: "the vendor registry feed is switched off"}
		b.Registry = oui.Status{Note: "the OUI registry feed is switched off, so no vendor names are available"}
		absent = append(absent, "vendor: the OUI registry feed is switched off")
	} else {
		b.Vendor = in.Vendors.Lookup(m)
		b.Registry = in.Vendors.Status()
		if !b.Vendor.Known && !m.LocallyAdministered {
			absent = append(absent, "vendor: "+b.Vendor.Reason)
		}
	}

	facts = macFacts{
		parsed: true,
		mac:    m,
		vendor: b.Vendor,
	}
	return b, facts, absent
}

func buildAddress(in Input, absent []string) (Address, []string) {
	a := Address{Assignment: "unknown"}
	if len(in.Routers) == 0 {
		a.Note = "no router has pushed DHCP or ARP state, so whether this address is a lease or fixed is not known here"
		absent = append(absent, "lease vs fixed: no router push covers this")
		return a, absent
	}

	var dhcpPushed bool
	for _, r := range in.Routers {
		a.Consulted = append(a.Consulted, r.Device)
		if r.ARPMAC != "" && a.ARPMAC == "" {
			a.ARPMAC = r.ARPMAC
		}
		if r.DHCPPushed {
			dhcpPushed = true
		}
		if r.Lease != nil && a.Assignment != "lease" {
			a.Assignment = "lease"
			a.Device = r.Device
			a.Hostname = r.Lease.Hostname
			a.LeaseMAC = r.Lease.MAC
		}
	}
	sort.Strings(a.Consulted)

	switch {
	case a.Assignment == "lease":
		a.Note = fmt.Sprintf("held on a DHCP lease from %s", a.Device)
	case dhcpPushed:
		// The push covered it and the address is not in it. That is
		// evidence, and it is the only case where "fixed" can be said.
		a.Assignment = "not-a-lease"
		a.Note = "no DHCP lease covers this address on " + strings.Join(a.Consulted, ", ") + ", so it is configured statically or handed out by something else"
	default:
		a.Note = "no DHCP table has been pushed by " + strings.Join(a.Consulted, ", ") + ", so lease-versus-fixed cannot be answered from evidence"
		absent = append(absent, "lease vs fixed: no DHCP table has been pushed")
	}
	return a, absent
}

func buildFirewall(in Input, absent []string) (Firewall, []string) {
	type key struct{ label, chain, action, device string }
	agg := map[key]*RuleMatch{}
	for _, e := range in.Events {
		if e.RuleLabel == "" {
			continue
		}
		k := key{e.RuleLabel, e.Chain, string(e.Action), e.DeviceID}
		m := agg[k]
		if m == nil {
			m = &RuleMatch{
				Label:  e.RuleLabel,
				Name:   e.RuleName,
				Chain:  e.Chain,
				Action: string(e.Action),
				Device: e.DeviceID,
			}
			if m.Name == "" && in.Lookups.RuleName != nil {
				m.Name = in.Lookups.RuleName(e.RuleLabel)
			}
			if in.Lookups.RuleComment != nil {
				if c, ok := in.Lookups.RuleComment(e.DeviceID, e.RuleLabel); ok {
					m.Comment, m.CommentKnown = c, true
				}
			}
			agg[k] = m
		}
		m.Events++
		if t := eventTime(e); t.After(m.LastSeen) {
			m.LastSeen = t
		}
	}

	var f Firewall
	if len(agg) == 0 {
		f.Note = "no retained event for this host names a firewall rule"
		absent = append(absent, "firewall rules: no retained event for this host carries a rule label")
		return f, absent
	}
	f.Known = true
	rules := make([]RuleMatch, 0, len(agg))
	for _, m := range agg {
		rules = append(rules, *m)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Events != rules[j].Events {
			return rules[i].Events > rules[j].Events
		}
		return rules[i].Label < rules[j].Label
	})
	if len(rules) > maxRules {
		f.More = len(rules) - maxRules
		rules = rules[:maxRules]
	}
	f.Rules = rules
	return f, absent
}

// timeOrNil drops a zero time rather than reporting it: absent is the
// honest rendering of "never", and the year 1 is not.
func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// eventTime prefers the router's own clock and falls back to receipt
// time, which is what every other reader of an Event does.
func eventTime(e store.Event) time.Time {
	if !e.Time.IsZero() {
		return e.Time
	}
	return e.ReceivedAt
}
