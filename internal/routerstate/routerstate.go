// SPDX-License-Identifier: AGPL-3.0-only

// Package routerstate holds the most recent state each router pushed via
// POST /api/ingest/routeros (issue #186 step 4): its firewall rule table,
// NAT table, and the host-identity records (DNS static entries, DHCP
// leases, WireGuard peers) that give addresses the operator's own names.
//
// Deliberately in-memory only, with no persistence path at all. That is
// step 5 ("never persist raw payloads wholesale") satisfied by
// construction rather than by review: this data is reconnaissance-grade
// router config, the router re-pushes it on a 15-30 minute schedule
// anyway, and everything built on it degrades to syslog-only behaviour
// when it is absent -- so a restart losing it costs one push interval of
// enrichment, not correctness. There is nothing to redact from a backup
// because it is never in one.
//
// Pushed data is applied whole-records-per-page with no reassembly: a
// page replaces its own slot and the current view is the union of
// whatever pages have arrived. A missing page means less enrichment,
// never "those records were deleted" -- absence of evidence is not
// evidence, the same rule every other external signal in this codebase
// follows.
//
// Nothing in here imports internal/flags or internal/detect, and a test
// enforces that in both directions: pushed data structurally cannot
// raise, lower, clear or suppress a suspicion signal. If a future change
// wants router data to contribute a signal, it goes through an explicit,
// narrow caller that argues its case (the shape internal/detect's
// netclass.go established), not through this package growing a path.
//
// One stated exception, so the sentence above is not read wider than it
// holds: watchlist *coverage* reads pushed filter rules
// (internal/watchlist.Coverage). Coverage is not a suspicion signal --
// it raises nothing and scores nothing -- but it is the answer to "could
// anything have fed this entry at all", and pushed rules decide it. See
// that function's own comment, and #333, for what that means for a
// leaked ingest token.
package routerstate

import (
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// maxDevices bounds how many distinct devices this store tracks -- same
// explicit-ceiling convention as every other unbounded-growth-risk map
// in this codebase (detect's maxTrackedSources, auth's
// maxLoginLimiterKeys). Devices only enter via an admin-issued ingest
// token, so this is a safety net against token misuse, not a limit a
// real deployment approaches.
var maxDevices = 256

// maxRecordsPerKind bounds the union of all pages for one (device, kind)
// pair. The ingest schema already caps a page at 1000 records and pages
// at 1000, but 1000 pages x 1000 records is a million records nothing
// legitimate produces -- #186 measured ~500 rules as a large real
// deployment. 5000 is generous headroom over that while keeping the
// worst case an ingest token can hold resident to a bounded, boring
// number.
var maxRecordsPerKind = 5000

// Store is safe for concurrent use: Apply takes the write lock, every
// read takes the read lock. Construct with New.
type Store struct {
	mu      sync.RWMutex
	devices map[string]*deviceState
}

type deviceState struct {
	kinds map[ingest.Kind]*kindState
	// routerosVersion is the last version this device reported on a push
	// envelope, and when it reported it (#436's derived source, carried
	// by #408's schema). Per device, like everything else here: a router
	// says what *it* runs, never what another one does.
	routerosVersion   string
	routerosVersionAt time.Time
	// hostsExact/hostsCIDR are the identity index rebuilt whenever an
	// identity-carrying kind changes -- see rebuildIdentityLocked. Kept
	// per device rather than globally so one device's re-push doesn't
	// rebuild every other's.
	hostsExact map[string]hostName
	hostsCIDR  []cidrName
}

// hostName is one resolved router-supplied name plus which pushed table
// supplied it. The source travels with the name because a caller that
// only learns "the router named this" cannot tell an operator where to
// go and change it -- and #413's editor has to name that place, since
// the name it displays is the one that wins (see internal/naming's
// RouterOS-always-wins precedence).
type hostName struct {
	name   string
	source string
}

type cidrName struct {
	prefix netip.Prefix
	name   string
}

// Host name sources, as reported by HostNameSource -- which pushed
// table a name came out of, named after the ingest kind that carried
// it so the two never drift apart.
const (
	HostSourceDNSStatic     = "dns-static"
	HostSourceDHCPLease     = "dhcp-lease"
	HostSourceWireguardPeer = "wireguard-peer"
)

type kindState struct {
	// pages holds each arrived page's full validated payload, keyed by
	// page number. Self-contained whole records per page is what makes
	// out-of-order arrival a non-event -- nothing is ever reassembled.
	pages     map[int]ingest.Payload
	pagesSeen int // the Pages total the current cycle declared
	updatedAt time.Time
}

func New() *Store {
	return &Store{devices: make(map[string]*deviceState)}
}

// Apply stores one validated page of pushed state for device. The
// payload has already been strictly decoded and validated by
// internal/ingest -- this only decides where it lands.
//
// A page whose Pages total differs from the pages already held for that
// kind starts a fresh cycle: the old pages are dropped rather than mixed
// with the new ones, since a table that shrank from 4 pages to 2 would
// otherwise permanently serve two stale pages alongside two fresh ones.
func (s *Store) Apply(device string, p ingest.Payload, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ds, ok := s.devices[device]
	if !ok {
		if len(s.devices) >= maxDevices {
			return fmt.Errorf("routerstate: %d devices already tracked -- refusing a new one", len(s.devices))
		}
		ds = &deviceState{kinds: make(map[ingest.Kind]*kindState), hostsExact: make(map[string]hostName)}
		s.devices[device] = ds
	}

	ks, ok := ds.kinds[p.Kind]
	if !ok {
		ks = &kindState{pages: make(map[int]ingest.Payload)}
		ds.kinds[p.Kind] = ks
	}
	if ks.pagesSeen != p.Pages {
		ks.pages = make(map[int]ingest.Payload)
		ks.pagesSeen = p.Pages
	}

	// Count what the union would hold with this page in place, before
	// committing it -- a page that would blow the ceiling is refused
	// whole, mirroring internal/ingest's own refuse-don't-trim stance.
	total := p.RecordCount()
	for page, existing := range ks.pages {
		if page == p.Page {
			continue
		}
		total += existing.RecordCount()
	}
	if total > maxRecordsPerKind {
		return fmt.Errorf("routerstate: %d records for %s would exceed the %d cap for one device", total, p.Kind, maxRecordsPerKind)
	}

	ks.pages[p.Page] = p
	ks.updatedAt = now

	// A push that names a version replaces what this device reported; a
	// push that omits it leaves the last answer alone rather than
	// clearing it. Absence of evidence is not evidence here either -- an
	// operator who reverts to an older push script has not downgraded
	// RouterOS, and forgetting on the strength of a silent page would
	// invent a state change nothing observed.
	if p.RouterOSVersion != "" {
		ds.routerosVersion = p.RouterOSVersion
		ds.routerosVersionAt = now
	}

	switch p.Kind {
	case ingest.KindDNSStatic, ingest.KindDHCPLease, ingest.KindWireguardPeer:
		ds.rebuildIdentityLocked()
	}
	return nil
}

// rebuildIdentityLocked rebuilds this device's host-identity index from
// its current pages. Precedence when two records name the same address:
// DNS static beats a DHCP lease's hostname (the former is the operator's
// own written intent, the latter is whatever name the device gave
// itself), and both beat a WireGuard peer's comment only in the sense
// that exact-address entries are consulted before CIDR containment --
// see Store.HostName.
//
// Rebuilt whole on every identity-kind page rather than incrementally:
// pushes arrive on a 15-30 minute schedule, and the index is a few
// thousand entries at most (maxRecordsPerKind), so simplicity wins over
// a delta scheme nothing needs.
func (ds *deviceState) rebuildIdentityLocked() {
	exact := make(map[string]hostName)
	var cidrs []cidrName

	// DHCP first, then DNS static over the top, so a static entry wins
	// the map slot when both name the same address.
	if ks, ok := ds.kinds[ingest.KindDHCPLease]; ok {
		for _, p := range ks.pages {
			for _, l := range p.DHCPLeases {
				if l.Address != "" && l.Hostname != "" {
					exact[l.Address] = hostName{name: l.Hostname, source: HostSourceDHCPLease}
				}
			}
		}
	}
	if ks, ok := ds.kinds[ingest.KindDNSStatic]; ok {
		for _, p := range ks.pages {
			for _, e := range p.DNSStatic {
				if e.Address != "" && e.Name != "" {
					exact[e.Address] = hostName{name: e.Name, source: HostSourceDNSStatic}
				}
			}
		}
	}
	if ks, ok := ds.kinds[ingest.KindWireguardPeer]; ok {
		for _, p := range ks.pages {
			for _, peer := range p.WireguardPeers {
				if peer.Comment == "" {
					continue
				}
				// Every allowed address the peer holds names the peer,
				// not just the first: a peer routing two branch subnets
				// is "branch office" on both (issue #443, which is where
				// this field stopped being a single string).
				for _, allowed := range peer.AllowedAddress {
					if allowed == "" {
						continue
					}
					prefix, err := netip.ParsePrefix(allowed)
					if err != nil {
						// A bare address is fine too -- same promotion
						// parse rule internal/netclass's plain-CIDR parser
						// applies.
						addr, aerr := netip.ParseAddr(allowed)
						if aerr != nil {
							continue
						}
						bits := 32
						if addr.Is6() {
							bits = 128
						}
						prefix = netip.PrefixFrom(addr, bits)
					}
					cidrs = append(cidrs, cidrName{prefix: prefix.Masked(), name: peer.Comment})
				}
			}
		}
	}
	// Most-specific-first, so a /32 peer wins over a /24 site range when
	// both contain the queried address.
	sort.Slice(cidrs, func(i, j int) bool { return cidrs[i].prefix.Bits() > cidrs[j].prefix.Bits() })

	ds.hostsExact = exact
	ds.hostsCIDR = cidrs
}

// HostName returns the name device pushed for ip, or "" -- exact
// DNS-static/DHCP entries first, then WireGuard peer ranges by most
// specific containment ("traffic from 10.10.0.0/24 reads 'branch
// office'", #186 step 4b).
//
// device scopes the answer, and that scoping is the point. This used to
// iterate every device that had ever pushed and return the first match,
// which broke the guarantee internal/auth/token.go states in its own
// words -- that an ingest token is scoped to one router precisely
// because "one compromised router could report state for every other
// device in the deployment" -- and that internal/api/ingest.go repeats:
// "a router cannot report state for any device but the one its
// credential is scoped to." For host names, it could.
//
// The holder of one router's token (which token.go notes any RouterOS
// user with `read` can print out of a script) could push a dns-static
// record naming any address in the world, and that name became the
// displayed host name for that address on traffic seen through every
// other monitored router. A single WireGuard peer with AllowedAddress
// 0.0.0.0/0 became the catch-all name for every otherwise-unlabelled
// address in the deployment. The mildest version needs no attacker at
// all: two independently-administered routers both using 192.168.1.0/24
// cross-contaminate each other's displayed names.
//
// Every other read in this file was already scoped through
// kindLocked(device, kind), and TestDevicesAreIsolated already asserted
// it for the rule tables. HostName was the one accessor that never got
// the same treatment, and TestHostNamePrecedence only ever exercised a
// single device, so nothing exercised the gap. Found independently by
// two of #272's phase 2 reviewers and reproduced by a third; see #285,
// #283, #284.
//
// An empty device returns "" rather than searching every device: a
// caller that does not know which router an event came from must not be
// handed a name on the strength of some other router's claim.
//
// Called on the per-event ingest hot path via naming.Resolver, so it is
// one read lock, one map hit, and a linear scan of a small CIDR list
// only when the map misses.
func (s *Store) HostName(device, ip string) string {
	name, _ := s.HostNameSource(device, ip)
	return name
}

// HostNameSource is HostName plus which pushed table the name came out
// of -- one of the HostSource* constants, or "" whenever name is "".
//
// The source exists for #413's inline editor, which must refuse an edit
// that would have no effect. Because RouterOS wins (internal/naming),
// a mikroview-side label for a router-named host is never displayed, so
// the editor disables the field and says where the name really comes
// from. "The router named it" is not enough to act on -- an operator
// told that still has to find *which* of dns-static, a DHCP lease or a
// WireGuard peer comment to go and change -- so the answer names the
// table.
//
// HostName is the hot-path caller and delegates here rather than the
// other way round: the work is identical (one map hit, or one scan of
// the small CIDR list), so there is no faster path to preserve, and one
// implementation cannot drift from the other.
func (s *Store) HostNameSource(device, ip string) (name, source string) {
	if device == "" || ip == "" {
		return "", ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	ds, ok := s.devices[device]
	if !ok {
		return "", ""
	}
	if v, ok := ds.hostsExact[ip]; ok {
		return v.name, v.source
	}
	if len(ds.hostsCIDR) > 0 {
		if addr, err := netip.ParseAddr(ip); err == nil {
			addr = addr.Unmap()
			for _, c := range ds.hostsCIDR {
				if c.prefix.Contains(addr) {
					return c.name, HostSourceWireguardPeer
				}
			}
		}
	}
	return "", ""
}

// FilterRules returns device's pushed firewall filter table in RouterOS's
// own display order (by ordinal -- the numbering an operator sees in
// `/ip/firewall/filter print`), with when it was last updated. ok is
// false when nothing has been pushed for that device+kind, which the UI
// renders as "no data pushed yet" -- not an error, and not an empty
// table pretending to be a real one.
func (s *Store) FilterRules(device string) (rules []ingest.FilterRule, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindFilterRule)
	if !found {
		return nil, time.Time{}, false
	}
	for _, p := range ks.pages {
		rules = append(rules, p.FilterRules...)
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Ordinal < rules[j].Ordinal })
	return rules, ks.updatedAt, true
}

// NATRules is FilterRules for the NAT table -- display-table shape only,
// per #186 step 4c: a log line carries a translation result, never which
// rule performed it, so there is no event-to-NAT-rule resolution to
// offer, just the faithful numbered table.
func (s *Store) NATRules(device string) (rules []ingest.NATRule, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindNATRule)
	if !found {
		return nil, time.Time{}, false
	}
	for _, p := range ks.pages {
		rules = append(rules, p.NATRules...)
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Ordinal < rules[j].Ordinal })
	return rules, ks.updatedAt, true
}

// RulesForLogPrefix returns every filter rule on device whose LogPrefix
// is exactly prefix -- the event-to-rule join #186 step 4c settled:
// resolving an event back to a rule is only possible through an
// operator-set log-prefix (the ordinal never appears in a log line), its
// uniqueness is entirely the operator's convention, and when several
// rules share a prefix the honest answer is all of them, not a guess at
// one.
func (s *Store) RulesForLogPrefix(device, prefix string) []ingest.FilterRule {
	if prefix == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindFilterRule)
	if !found {
		return nil
	}
	var out []ingest.FilterRule
	for _, p := range ks.pages {
		for _, r := range p.FilterRules {
			if r.LogPrefix == prefix {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out
}

// DHCPLeases returns device's pushed DHCP lease table, sorted by address
// for a stable, deterministic order (leases have no ordinal the way
// filter/NAT rules do). ok is false when nothing has been pushed for
// that device+kind yet -- same "no data yet" vs "empty table" distinction
// FilterRules makes.
func (s *Store) DHCPLeases(device string) (leases []ingest.DHCPLease, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindDHCPLease)
	if !found {
		return nil, time.Time{}, false
	}
	for _, p := range ks.pages {
		leases = append(leases, p.DHCPLeases...)
	}
	sort.SliceStable(leases, func(i, j int) bool { return leases[i].Address < leases[j].Address })
	return leases, ks.updatedAt, true
}

// ARPEntries returns device's pushed ARP table, sorted by address. The
// fallback identity source for devices with no DHCP lease -- see
// ingest.ARPEntry's own doc comment.
func (s *Store) ARPEntries(device string) (entries []ingest.ARPEntry, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindARP)
	if !found {
		return nil, time.Time{}, false
	}
	for _, p := range ks.pages {
		entries = append(entries, p.ARP...)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Address < entries[j].Address })
	return entries, ks.updatedAt, true
}

// AddressLists returns device's pushed /ip/firewall/address-list table,
// sorted by (list, address).
// InAddressList reports whether ip is currently in device's address
// list, from whatever that router last pushed (#274 item 2).
//
// "Currently" is the whole point: RouterOS edits these lists itself --
// its own rules add dynamic entries -- so a watchlist entry scoped to a
// list has to be resolved at match time. Answering from a copy taken
// when the entry was created would be stale the first time the list
// changed, silently.
//
// Compared as text, not as parsed addresses. A list entry can be a bare
// address, a CIDR or a range, and reading a range as a set of addresses
// here would quietly turn a display table into a matching engine. What
// this answers is "is this exact address listed", which is what the
// pushed table actually says; anything cleverer belongs behind a
// deliberate decision rather than arriving as a side effect.
func (s *Store) InAddressList(device, list, ip string) bool {
	if device == "" || list == "" || ip == "" {
		return false
	}
	entries, _, ok := s.AddressLists(device)
	if !ok {
		return false
	}
	for _, e := range entries {
		if e.List == list && e.Address == ip {
			return true
		}
	}
	return false
}

func (s *Store) AddressLists(device string) (entries []ingest.AddressListEntry, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ks, found := s.kindLocked(device, ingest.KindAddressList)
	if !found {
		return nil, time.Time{}, false
	}
	for _, p := range ks.pages {
		entries = append(entries, p.AddressList...)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].List != entries[j].List {
			return entries[i].List < entries[j].List
		}
		return entries[i].Address < entries[j].Address
	})
	return entries, ks.updatedAt, true
}

// Devices returns every device with at least one pushed page, sorted by
// name -- the enumeration FilterRules/DHCPLeases/etc need a caller to
// already have a device name, this is how a caller (e.g. the suggestions
// generator, issue #243 slice 5) discovers what devices exist at all.
func (s *Store) Devices() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedDeviceNamesLocked(s.devices)
}

// RouterOSVersion reports what device last said it was running, and
// when it said it. ok is false when that device has never pushed a
// version at all -- which is every device whose push script predates
// issue #408's schema, and is not an error: it means "not stated", and a
// caller must not read it as old, unsupported, or anything else.
//
// The version is the router's own claim about itself, unverified by
// construction: mikroview never connects to a router (AGENTS.md,
// "mikroview observes; it never scans or connects"), so there is nothing
// to check it against. Scoped to the pushing device for the same reason
// every other read here is -- see HostName.
//
// Nothing warns on a mismatch yet; that is #436's own work. This is the
// field arriving and being readable, which is what #408 carried.
func (s *Store) RouterOSVersion(device string) (version string, updatedAt time.Time, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, found := s.devices[device]
	if !found || ds.routerosVersion == "" {
		return "", time.Time{}, false
	}
	return ds.routerosVersion, ds.routerosVersionAt, true
}

// PushedKinds reports, for one device, every table it has pushed and
// when that table last arrived. The setup wizard (#320) uses it to say
// which blocks of the push script are working -- "filter rules yes,
// DHCP leases no" is actionable in a way that "pushes are happening" is
// not.
func (s *Store) PushedKinds(device string) map[ingest.Kind]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.devices[device]
	if !ok {
		return nil
	}
	out := make(map[ingest.Kind]time.Time, len(ds.kinds))
	for kind, ks := range ds.kinds {
		if len(ks.pages) == 0 {
			continue
		}
		out[kind] = ks.updatedAt
	}
	return out
}

func (s *Store) kindLocked(device string, kind ingest.Kind) (*kindState, bool) {
	ds, ok := s.devices[device]
	if !ok {
		return nil, false
	}
	ks, ok := ds.kinds[kind]
	if !ok || len(ks.pages) == 0 {
		return nil, false
	}
	return ks, true
}

func sortedDeviceNamesLocked(devices map[string]*deviceState) []string {
	names := make([]string, 0, len(devices))
	for n := range devices {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
