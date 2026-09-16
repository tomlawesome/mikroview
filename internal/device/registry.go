// SPDX-License-Identifier: AGPL-3.0-only

// Package device holds the one list of RouterOS devices mikroview
// knows about, and attributes syslog source addresses to them.
//
// #1170's ruling, and the reason this package is the single list every
// count reads: a device is what an ingest token names. The token is the
// strongest identity mikroview has -- the operator minted it for one
// router and the wizard wrote it into that router -- so a push from
// token device X puts X here (Ensure). Syslog gives no identity at all,
// only a source address, so the syslog side never creates a device: it
// attaches to one (Resolve), or the address is remembered as an
// unattributed Source and is not counted as a router anywhere.
package device

import (
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/ingest"
)

// Info describes a RouterOS device mikroview has received log data from.
//
// SourceIP is the address config.yaml declared for it, or -- for a
// device that only an ingest token names -- the first source address
// attributed to it by its own pushed address table, empty until one is.
// A router is multi-homed by definition, so this is where its syslog
// was first seen arriving from, never the list of addresses it has.
type Info struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SourceIP   string    `json:"sourceIp"`
	Configured bool      `json:"configured"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
	EventCount uint64    `json:"eventCount"`
}

// Source is a syslog source address no device has claimed: neither a
// config.yaml devices[].sourceIp nor an address exactly one router has
// pushed as its own. #1170's ruling in one type -- a syslog packet
// carries no identity, only an address, so an unclaimed one is
// remembered as a source and never invented into a device named after
// its own IP. Its lines keep flowing and are stored under that address
// as their device id, exactly as the raw-IP device row it replaces
// was; only the claim that it is a router is gone.
type Source struct {
	Address   string    `json:"address"`
	Lines     uint64    `json:"lines"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// Claimants names every device whose pushed address table carries
	// this address, and is set only when two or more do -- the one case
	// where the registry can say *why* it could not attribute, rather
	// than only that it could not. Device ids: a caller showing them to
	// an operator resolves display names the same way it does for any
	// other device id.
	Claimants []string `json:"claimants,omitempty"`
}

// NameLookup is the one method of internal/naming.Resolver this package
// needs: the display name to show for a device id, "" when nothing
// names it. An interface rather than the concrete type so device does
// not import naming (naming's resolver is built later in main, once the
// entity store exists) and so tests can supply a name without one.
type NameLookup interface {
	Device(id string) string
}

// AddressTables is the half of internal/routerstate.Store this package
// needs: which addresses each device has pushed as its own, from its
// /ip/address table. An interface for the same two reasons NameLookup
// is one -- device does not import routerstate, and a test can supply
// a table without a store.
type AddressTables interface {
	// Devices returns every device that has pushed anything.
	Devices() []string
	// IPAddresses returns device's pushed /ip/address table; ok is
	// false when that device has never pushed one.
	IPAddresses(device string) (entries []ingest.IPAddressEntry, updatedAt time.Time, ok bool)
}

// Registry is every device mikroview knows about, plus the syslog
// sources it has not been able to attribute to one, and per-device
// liveness/volume for the /api/devices endpoint.
//
// Devices enter one of two ways, both of them an act by the operator: a
// config.yaml declaration (NewRegistry), or an ingest token they minted
// naming the device that pushed (Ensure). Syslog traffic adds none: an
// address that resolves to no device is kept as a Source, so events are
// never lost just because a router has not been declared -- the
// operator sees the unattributed address in the UI and can declare it
// there.
type Registry struct {
	mu sync.RWMutex
	// byIP maps a config.yaml-declared devices[].sourceIp to its
	// device: attribution step (a), the operator's own word on which
	// address is which router, and the only lookup strong enough to be
	// cached permanently. Attribution derived from pushed tables
	// (step (b)) deliberately does not land here -- see attribution.
	byIP map[string]*Info
	// byID holds every device by id, declared and push-named alike.
	// This is the list List returns and everything counts.
	byID map[string]*Info
	// sources holds the unattributed syslog source addresses, keyed by
	// the normalised address. The only map here that grows from
	// unauthenticated traffic, so the only one pruneLocked bounds.
	sources map[string]*Source
	// attribution caches what the pushed address tables said about one
	// source address -- the miss as much as the hit, because Resolve
	// runs on every ingested line and a walk of every device's address
	// table per line is exactly the per-event cost #370 took out of
	// this function. Derived, never authoritative: every push clears it
	// (Ensure), so an attribution is never resting on evidence the
	// routers have since revised.
	attribution map[string]claim
	// addresses is the pushed-address-table source for step (b), nil
	// until wired (SetAddressTables) and on a registry whose owner has
	// no router state at all -- attribution then stops at step (a),
	// which is a narrower answer, never a wrong one.
	addresses AddressTables

	// names, when set, resolves the display name for a device id --
	// config.yaml's declared name, else an operator's stored label
	// (issue #600). Held here rather than applied by each caller so
	// every consumer of List (GET /api/devices, the setup wizard's
	// command blocks, the device-silence flag's own wording) shows the
	// one name, which is the whole point of the issue: a rename typed
	// by one person is what everybody reads.
	//
	// Read under the same lock as byIP, but never on the ingest path:
	// Resolve returns the id, and only List asks for a name.
	names NameLookup
}

// claim is what the pushed address tables say about one source
// address: the single device that claims it, the two-or-more that all
// do, or neither.
type claim struct {
	id        string
	claimants []string
}

// maxUnattributedSources bounds how many unclaimed syslog source
// addresses the registry retains. Resolve records an entry for any
// previously-unseen source address, so without a cap the map grows with
// whatever reaches the listener.
//
// This comment used to justify the cap by UDP source spoofing. That is
// no longer the shape of it: #189 removed every plaintext listener, and
// syslog.ListenTLS is the only one left, so minting an entry now costs
// a completed TCP+TLS handshake from the address in question -- an
// attacker cannot forge arbitrary sources, only their own. What has not
// changed is that it takes *no credentials*: the listener sets no
// ClientAuth (RouterOS's logging action has no client-certificate
// option), so anyone who can reach the port can still add their own
// address to this list.
//
// Devices are not bounded by it and never evicted by it. Since #1170 a
// device exists only because the operator declared it or minted an
// ingest token for it, so that map grows by admin action alone -- and
// losing a declared router to a flood of unclaimed sources would be the
// attack succeeding by another route.
//
// 4096 matches internal/detect's maxTrackedSources, which bounds the
// same class of per-source state for the same reason. A var so tests
// can shrink it.
var maxUnattributedSources = 4096

// ConfigNames is the device-name map internal/naming.Resolver.Devices
// takes: the display name config.yaml declares for each device, keyed
// by the id everything downstream uses. Lives here, beside the registry
// built from the same slice, rather than in main -- the two readings of
// cfg.Devices belong together.
func ConfigNames(configured []config.Device) map[string]string {
	out := make(map[string]string, len(configured))
	for _, d := range configured {
		if d.Name != "" {
			out[d.ID] = d.Name
		}
	}
	return out
}

func NewRegistry(configured []config.Device) *Registry {
	r := &Registry{
		byIP:        make(map[string]*Info),
		byID:        make(map[string]*Info),
		sources:     make(map[string]*Source),
		attribution: make(map[string]claim),
	}
	for _, d := range configured {
		key := normalizeIP(d.SourceIP)
		info := &Info{
			ID:         d.ID,
			Name:       d.Name,
			SourceIP:   key,
			Configured: true,
		}
		r.byIP[key] = info
		r.byID[info.ID] = info
	}
	return r
}

// SetAddressTables wires attribution step (b) in: the routers' own
// pushed /ip/address tables. Separate from NewRegistry for the same
// reason SetNames is -- the store it reads is built later in main --
// and a Registry without one attributes by config.yaml alone.
func (r *Registry) SetAddressTables(a AddressTables) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addresses = a
	clear(r.attribution)
}

// Ensure records that deviceID has pushed, creating its registry entry
// the first time -- #1170's "a push from token device X ensures X
// exists". deviceID is the ingest token's own Device, the identity the
// operator minted; nothing the payload claims about itself is consulted
// here any more than it is in internal/routerstate.
//
// A push sets FirstSeen (this is when mikroview first heard of the
// router at all) but never LastSeen or EventCount: those two are the
// syslog liveness the fleet's live/stale/never-seen status and the
// device-silence detector read, and a router that pushes its tables
// while logging nothing is exactly the silence they exist to report.
func (r *Registry) Ensure(deviceID string, now time.Time) {
	if deviceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureLocked(deviceID, now)
	// A push is new evidence about which addresses belong to which
	// router, so every derived attribution is dropped and recomputed on
	// the next line from that source.
	clear(r.attribution)
}

func (r *Registry) ensureLocked(deviceID string, now time.Time) *Info {
	info, ok := r.byID[deviceID]
	if !ok {
		info = &Info{ID: deviceID, Name: deviceID, FirstSeen: now}
		r.byID[deviceID] = info
	}
	if info.FirstSeen.IsZero() {
		info.FirstSeen = now
	}
	return info
}

// Resolve maps a syslog source IP to the id its events are stored
// under, recording that a line was just received from it. It is
// intended to be called from the single store-writer goroutine on the
// ingest path, so the write lock it takes is uncontended in practice;
// the RWMutex exists for concurrent /api/devices reads, not for
// ingest-side concurrency.
//
// #1170's attribution order, strongest evidence first:
//
//   - (a) config.yaml's devices[].sourceIp -- the operator said so.
//   - (b) the routers' own pushed /ip/address tables: an address that
//     appears in exactly one device's table belongs to that device.
//     The router told us its addresses, so we use them; two routers
//     pushing the same address tells us only that we cannot tell.
//   - otherwise the source is unattributed. It is remembered as a
//     Source, not minted as a device named after its own IP, and the
//     address itself is returned so the lines are stored and shown
//     exactly as before -- under an id that claims nothing.
func (r *Registry) Resolve(sourceIP string, now time.Time) (deviceID string) {
	key := normalizeIP(sourceIP)

	r.mu.Lock()
	defer r.mu.Unlock()

	// (a) declared in config.yaml.
	if info, ok := r.byIP[key]; ok {
		r.seenLocked(info, key, now)
		return info.ID
	}

	// (b) claimed by exactly one router's pushed address table.
	c := r.claimLocked(key)
	if c.id != "" {
		info := r.ensureLocked(c.id, now)
		r.seenLocked(info, key, now)
		// Attributed now, so it is no longer an address nobody has
		// claimed: drop any record of it as one. The lines it sent
		// while unattributed stay where they were stored.
		delete(r.sources, key)
		return info.ID
	}

	// Unattributed: a source, not a router.
	src, ok := r.sources[key]
	if !ok {
		src = &Source{Address: key, FirstSeen: now}
		r.sources[key] = src
	}
	src.LastSeen = now
	src.Lines++
	src.Claimants = c.claimants
	r.pruneLocked()
	return key
}

// seenLocked records that a line just arrived for info from address
// key. A device whose address was never declared takes the first one
// attributed to it as its SourceIP, so every card and row has an
// address to show; a later line from another of its addresses does not
// overwrite that -- see Info.SourceIP.
func (r *Registry) seenLocked(info *Info, key string, now time.Time) {
	if info.SourceIP == "" {
		info.SourceIP = key
	}
	if info.FirstSeen.IsZero() {
		info.FirstSeen = now
	}
	info.LastSeen = now
	info.EventCount++
}

// claimLocked answers which devices have pushed key as one of their own
// addresses, from the cache when it can. A hit and a miss are cached
// alike: an unattributed source keeps sending, and rescanning every
// address table for every one of its lines is the cost this avoids.
func (r *Registry) claimLocked(key string) claim {
	if c, ok := r.attribution[key]; ok {
		return c
	}
	c := r.computeClaimLocked(key)
	// Bounded by the same reasoning as sources: every miss cached here
	// has a Source beside it, and a prune clears the whole cache rather
	// than tracking which entries went with the sources it shed.
	r.attribution[key] = c
	return c
}

func (r *Registry) computeClaimLocked(key string) claim {
	if r.addresses == nil || key == "" {
		return claim{}
	}
	var owners []string
	for _, dev := range r.addresses.Devices() {
		entries, _, ok := r.addresses.IPAddresses(dev)
		if !ok {
			continue
		}
		for _, e := range entries {
			if addressOf(e.Address) == key {
				owners = append(owners, dev)
				break
			}
		}
	}
	switch len(owners) {
	case 0:
		return claim{}
	case 1:
		return claim{id: owners[0]}
	default:
		sort.Strings(owners)
		return claim{claimants: owners}
	}
}

// addressOf reduces one /ip/address row to the address itself:
// RouterOS writes them as "a.b.c.d/nn", and it is the host address a
// router's syslog arrives from, not the prefix. Anything that parses as
// neither is returned normalised and simply fails to match, the same
// tolerant-of-one-bad-record convention routerstate's own readers use.
func addressOf(s string) string {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Addr().String()
	}
	return normalizeIP(s)
}

// pruneLocked evicts the least-recently-seen unattributed sources once
// they exceed maxUnattributedSources. Oldest-LastSeen-first, mirroring
// MACRegistry.pruneLocked -- under a flood of unclaimed sources the
// genuine ones are the ones still sending, so they are exactly the ones
// this keeps.
//
// #370: this used to walk and re-sort the entire map on every single
// call, with no guard at all, and then evicted back to exactly the cap
// -- so the very next newly-seen source overflowed again and paid the
// full walk once more. Resolve runs synchronously on the single ingest
// goroutine for every ingested event, so once the cap filled, every
// subsequent event paid that walk: measured at roughly 108us/call at
// the cap against 0.4us/call empty, enough to back up the raw ingest
// channel and start silently dropping real router log records. Same
// defect and same remedy as MACRegistry.pruneLocked and
// rules.Store.pruneLocked (see #285 and 3d27200): an O(1) guard first,
// then a batched shed via internal/evict so a prune leaves headroom
// instead of putting the map right back at the cap.
func (r *Registry) pruneLocked() {
	if len(r.sources) <= maxUnattributedSources {
		return
	}
	evict.DownTo(r.sources, evict.Target(maxUnattributedSources), func(s *Source) time.Time {
		return s.LastSeen
	})
	// The attribution cache is derived and cheap to refill, and its
	// misses are keyed by exactly the addresses just shed; dropping the
	// lot is simpler than tracking which went with which, and costs one
	// rescan per live source.
	clear(r.attribution)
}

// SetNames wires the display-name resolver in. Separate from
// NewRegistry because the resolver needs the entity store, which main
// opens long after the registry the ingest path needs; a Registry
// without one keeps answering the names config.yaml gave it.
func (r *Registry) SetNames(names NameLookup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = names
}

// List returns a snapshot of every device -- declared in config.yaml
// and named by an ingest token alike -- with each Name resolved through
// NameLookup (issue #600) so a stored rename is what every reader sees.
// Unattributed syslog sources are not in it, by #1170's ruling: see
// Unattributed, which is where they are.
//
// Ordering is configured devices first, then by id -- stable, and not
// the map order this used to return. Callers picking devices[0] as
// "the router this instance watches" were relying on a single-device
// deployment for that to hold: with two, the same request answered in a
// different order each time (live-setup-wizard-source-split.mjs records
// what that cost, and live-env.sh declares one device to avoid it).
// Configured first matches what the fleet already sorts by
// (frontend/src/lib/fleet.ts): a declared router is what an operator
// set out to watch; one that merely turned up pushing is secondary
// information.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Info, 0, len(r.byID))
	for _, info := range r.byID {
		v := *info
		if r.names != nil {
			if name := r.names.Device(v.ID); name != "" {
				v.Name = name
			}
		}
		// A device always displays as something. A declaration with no
		// name would otherwise show as an empty cell in every fleet card
		// and every row -- the raw id is the honest fallback, and the one
		// the editor promises when a label is removed ("leave empty to
		// show the raw value again").
		if v.Name == "" {
			v.Name = v.ID
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Configured != out[j].Configured {
			return out[i].Configured
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Unattributed returns every syslog source address no device has
// claimed, in address order -- #1170's third answer, the one the
// operator has to see as what it is rather than as a router named
// after an IP. The fix is theirs to make: declare the address under
// devices: in config.yaml, or check that the router's own pushed
// address table carries it (a NAT'd relay's never will, so config is
// the answer there).
func (r *Registry) Unattributed() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Source, 0, len(r.sources))
	for _, src := range r.sources {
		v := *src
		if len(src.Claimants) > 0 {
			v.Claimants = append([]string(nil), src.Claimants...)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// MultihomedCandidate pairs one declared device that has never received
// an event under its own sourceIp with every syslog source currently
// arriving unattributed -- the evidence shape issue #442 describes:
// "the declared device... never matches anything and sits idle" while
// "the real stream auto-discovers as a second device". A RouterOS
// device is multi-homed by definition (an address on every subnet it
// routes), so syslog frequently arrives stamped with a different
// interface address than the one declared as sourceIp.
//
// Since #1170 the registry tries the routers' own pushed address tables
// before giving up on a source, so a multi-homed router that pushes is
// attributed rather than reported here. What is left is the case the
// tables cannot settle: a router that has not pushed its addresses, or
// syslog arriving from an address (a NAT'd relay's) that no router has.
type MultihomedCandidate struct {
	// DeclaredID and DeclaredSourceIP identify the configured device
	// that has never matched any syslog traffic.
	DeclaredID       string
	DeclaredSourceIP string
	// Unattributed is every unclaimed syslog source, in address order.
	// Registry has no further evidence to narrow this to a single "same
	// box" answer -- see MultihomedCandidates' own comment -- so every
	// live unattributed source is reported, not a guess at one.
	Unattributed []Source
}

// MultihomedCandidates returns one entry per configured device that has
// received zero events under its own declared sourceIp, alongside every
// unattributed syslog source. nil when either side is empty: a declared
// device with no traffic yet is unremarkable if every source is
// attributed anyway (the router may simply not have started logging
// yet), and an unattributed source is unremarkable on its own if every
// declared device is receiving its own traffic fine.
//
// This is deliberately just the evidence, not a diagnosis: Registry
// knows source IPs, first/last-seen times, which devices came from
// config.yaml and which addresses the routers have pushed, so where the
// pushed tables are silent it cannot itself tell which unattributed
// address (if any) is actually the same physical router as a given
// silent declared one -- that judgement, and where to surface it to the
// operator, is left to the caller. See #442.
func (r *Registry) MultihomedCandidates() []MultihomedCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var silent []Info
	for _, info := range r.byID {
		if info.Configured && info.EventCount == 0 {
			silent = append(silent, *info)
		}
	}
	if len(silent) == 0 || len(r.sources) == 0 {
		return nil
	}

	arriving := make([]Source, 0, len(r.sources))
	for _, src := range r.sources {
		arriving = append(arriving, *src)
	}
	sort.Slice(arriving, func(i, j int) bool { return arriving[i].Address < arriving[j].Address })
	sort.Slice(silent, func(i, j int) bool { return silent[i].ID < silent[j].ID })

	out := make([]MultihomedCandidate, 0, len(silent))
	for _, s := range silent {
		out = append(out, MultihomedCandidate{
			DeclaredID:       s.ID,
			DeclaredSourceIP: s.SourceIP,
			Unattributed:     arriving,
		})
	}
	return out
}

func normalizeIP(s string) string {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}
