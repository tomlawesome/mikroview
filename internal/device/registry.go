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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// deviceLog is this package's logger -- distinct from mac_registry.go's
// persistLog ("device-mac"), which names the separate MAC-history store.
var deviceLog = logging.New("device")

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
	// AcceptedIP is issue #1281's enrolled address: empty until a valid
	// enrolment token is redeemed at some source address (Registry.
	// TryEnrol), or until the upgrade-time one-shot check
	// (EnrolFromPushedAddresses) finds this device the sole claimant of
	// one of its own pushed addresses. Distinct from SourceIP, which is
	// either the operator's config.yaml declaration or the first address
	// attribution ever happened to see -- AcceptedIP is the one address
	// this instance has actual evidence (a token, not merely a claim) is
	// this router. Persisted (see persistLocked) so an enrolment
	// survives a restart.
	AcceptedIP string `json:"acceptedIp"`
	// EnrolledAt is when AcceptedIP was set, zero until then.
	EnrolledAt time.Time `json:"enrolledAt,omitzero"`
	// RegisteredAt is when the operator confirmed this router on the
	// device itself -- the ledger's final Register step (#1291) -- zero
	// until they do. It records intent and grants nothing: registering
	// never sets or changes AcceptedIP, so spoofing the click buys an
	// attacker no acceptance. Acceptance stays where #1281 put it, in a
	// token arriving over real syslog traffic from the address being
	// accepted.
	//
	// Enrolled and registered are therefore independent, and the pair
	// reads as the operator's progress: enrolled but never registered is
	// an enrolment someone walked away from part way, and the ledger
	// reopens at what is left.
	RegisteredAt time.Time `json:"registeredAt,omitzero"`
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
	// Claimants named every device whose pushed address table carried
	// this address, when two or more did. Issue #1281's audit removed
	// the pushed-address-table claim from attribution entirely (a router
	// cannot be trusted to name its own address over syslog -- see
	// Registry.Resolve's doc comment), so this is never populated any
	// more; it stays on the wire shape rather than being deleted so a
	// client reading it sees a stable "no conflict" answer instead of a
	// field disappearing.
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
//
// No longer consulted by Resolve (issue #1281's audit: a router's own
// claim about its address, made over syslog, is not evidence strong
// enough to attribute identity -- see Resolve's doc comment). The one
// remaining caller is EnrolFromPushedAddresses, the once-at-startup
// upgrade step that offers a still-unenrolled device the same "sole
// claimant" evidence as a one-time, logged, operator-visible nudge
// rather than a standing security decision. The pushed tables
// themselves are untouched and keep serving the display-only table
// endpoints (GET /api/routeros/{device}/addresses).
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
// Devices enter one of three ways: a config.yaml declaration
// (OpenRegistry), an ingest token minted for a device that then pushed
// (Ensure), or an admin declaring a syslog-only router by name (Create,
// issue #1281) ahead of enrolling it. Syslog traffic on its own adds
// none: an address that resolves to no device is kept as a Source, so
// events are never lost just because a router has not been declared --
// the operator sees the unattributed address in the UI and can declare
// it there.
type Registry struct {
	mu sync.RWMutex
	// byIP maps a config.yaml-declared devices[].sourceIp to its
	// device: attribution step (a), the operator's own word on which
	// address is which router, and the only lookup strong enough to be
	// cached permanently.
	byIP map[string]*Info
	// byAcceptedIP maps a device's enrolled address (Info.AcceptedIP) to
	// its device: attribution step (b), issue #1281's replacement for
	// the pushed-address-table claim this package used to trust. An
	// address lands here only through TryEnrol (a token minted by an
	// admin, redeemed by a line actually carrying it) or
	// EnrolFromPushedAddresses' one-shot upgrade nudge -- never merely
	// because a router's own pushed table says so.
	byAcceptedIP map[string]*Info
	// byID holds every device by id, declared, push-named and
	// admin-created alike. This is the list List returns and everything
	// counts.
	byID map[string]*Info
	// sources holds the unattributed syslog source addresses, keyed by
	// the normalised address. The only map here that grows from
	// unauthenticated traffic, so the only one pruneLocked bounds. Since
	// #1281's listener gate, a source only ever reaches Resolve (and so
	// this map) if it was already sourceIp/acceptedIp -- an address that
	// is neither is refused at the listener and never becomes a Source
	// at all; see Refused for where those addresses are counted instead.
	sources map[string]*Source

	// refused holds every syslog source address the listener gate has
	// refused a line from -- issue #1281's GET /api/devices/refused.
	// Bounded and evicted the same oldest-last-seen-first way as
	// sources; see pruneRefusedLocked.
	refused map[string]*Refused
	// pendingByDevice holds each device's current enrolment token, by
	// device id -- at most one per device, replaced (never
	// accumulated) by MintEnrolment. Only the token's hash is kept; see
	// pendingToken.
	pendingByDevice map[string]pendingToken
	// pendingByHash is pendingByDevice's reverse index, so the listener
	// gate's TryEnrol -- called for every line from a not-yet-allowed
	// address -- costs one map lookup rather than a walk of every
	// device's pending token.
	pendingByHash map[string]string

	// backend/version are this registry's own optional persistence
	// (issue #1281): only AcceptedIP/EnrolledAt and the identity of any
	// device not declared in config.yaml need to survive a restart --
	// see persistLocked. Same JSON-file + atomic-write convention as
	// every other small store in this codebase (internal/droplist,
	// internal/suggest); nil backend (the default) means memory-only,
	// same as those.
	backend persist.Backend
	version int64

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

// NewRegistry builds a memory-only registry: every persistence-backed
// caller (main.go) wants OpenRegistry instead, but the many tests that
// have no need to exercise persistence keep this shorter spelling.
func NewRegistry(configured []config.Device) *Registry {
	r, err := OpenRegistryWithBackend(nil, configured)
	if err != nil {
		// Unreachable: OpenRegistryWithBackend only ever fails reading
		// from a real backend, and nil is the documented "no backend"
		// case (see persist.Open).
		panic("device: NewRegistry: " + err.Error())
	}
	return r
}

// OpenRegistry is NewRegistry plus issue #1281's own persistence: path
// loads (if it exists -- a missing file is the expected first-run case)
// and every device this registry itself created is written back to it
// from then on, atomically, the same convention internal/droplist and
// internal/suggest already use. An empty path keeps everything
// memory-only, same optional-persistence contract as every other small
// store in this codebase.
func OpenRegistry(path string, configured []config.Device) (*Registry, error) {
	if path == "" {
		return OpenRegistryWithBackend(nil, configured)
	}
	return OpenRegistryWithBackend(persist.NewFileBackend(path), configured)
}

// OpenRegistryWithBackend is OpenRegistry against any persist.Backend.
//
// Only a device this registry itself created (never one config.yaml
// declares, which is rebuilt from that file on every boot regardless)
// is written to the document -- see persistLocked -- and its
// AcceptedIP/EnrolledAt are read back onto it here so an enrolment
// survives a restart. A persisted record whose id collides with a
// config.yaml declaration merges onto that declared Info instead of
// creating a second entry, config.yaml's Name winning either way.
func OpenRegistryWithBackend(b persist.Backend, configured []config.Device) (*Registry, error) {
	r := &Registry{
		byIP:            make(map[string]*Info),
		byAcceptedIP:    make(map[string]*Info),
		byID:            make(map[string]*Info),
		sources:         make(map[string]*Source),
		refused:         make(map[string]*Refused),
		pendingByDevice: make(map[string]pendingToken),
		pendingByHash:   make(map[string]string),
		backend:         b,
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

	version, existed, err := persist.Open(context.Background(), b, "the device registry", func(data []byte) error {
		var file registryFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, pd := range file.Devices {
			if pd == nil || pd.ID == "" {
				continue
			}
			info, ok := r.byID[pd.ID]
			if !ok {
				info = &Info{ID: pd.ID, Name: pd.Name}
				r.byID[pd.ID] = info
			}
			if pd.AcceptedIP != "" {
				info.AcceptedIP = pd.AcceptedIP
				info.EnrolledAt = pd.EnrolledAt
				r.byAcceptedIP[normalizeIP(pd.AcceptedIP)] = info
			}
			info.RegisteredAt = pd.RegisteredAt
			// A registry written before #1291 has no registeredAt at
			// all, so every router already enrolled under #1281 would
			// read as an enrolment someone abandoned part way -- and the
			// ledger would reopen on routers whose operator finished
			// every step the ledger asked of them at the time. Treat an
			// enrolment that predates this as its own registration,
			// dated when it was enrolled: the operator's intent is not
			// in doubt for a router that went on to present a valid
			// token. Nothing is granted by this -- AcceptedIP is
			// untouched here, read back above from what was already
			// persisted. See docs/upgrades.md.
			if info.RegisteredAt.IsZero() && info.AcceptedIP != "" {
				info.RegisteredAt = info.EnrolledAt
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if existed {
		r.version = version
	}
	return r, nil
}

// registryFile is the on-disk shape OpenRegistryWithBackend/
// persistLocked read and write.
type registryFile struct {
	Devices []*persistedDevice `json:"devices"`
}

// persistedDevice is one registry-created device's durable half: its
// identity (so it exists at all on the next boot -- nothing else would
// recreate it) plus its enrolment. FirstSeen/LastSeen/EventCount are
// deliberately absent: those are syslog liveness, never persisted for
// any device before this feature either, and starting them fresh on
// restart is the existing, unremarked-on behaviour.
type persistedDevice struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	AcceptedIP   string    `json:"acceptedIp,omitempty"`
	EnrolledAt   time.Time `json:"enrolledAt,omitzero"`
	RegisteredAt time.Time `json:"registeredAt,omitzero"`
}

// persistLocked writes every non-config.yaml device to disk, if
// persistence is configured. Write failures are swallowed rather than
// surfaced to the caller -- the in-memory state (which every read goes
// through) stays correct either way, same contract as every other
// store's persistLocked in this codebase (e.g. internal/droplist).
// Must be called with r.mu held.
func (r *Registry) persistLocked() {
	if r.backend == nil {
		return
	}
	devices := make([]*persistedDevice, 0, len(r.byID))
	for _, info := range r.byID {
		if info.Configured {
			// Rebuilt from config.yaml on every boot regardless; nothing
			// here would ever be read back for it except a redundant
			// AcceptedIP, since a config-declared device is already
			// enrolled at its SourceIP with no token needed (see
			// Registry.Resolve).
			continue
		}
		devices = append(devices, &persistedDevice{
			ID:           info.ID,
			Name:         info.Name,
			AcceptedIP:   info.AcceptedIP,
			EnrolledAt:   info.EnrolledAt,
			RegisteredAt: info.RegisteredAt,
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })

	data, err := json.MarshalIndent(registryFile{Devices: devices}, "", "  ")
	if err != nil {
		deviceLog.Error(fmt.Sprintf("encoding the device registry for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	version, conflicted, err := persist.SaveWithRetry(context.Background(), r.backend, data, r.version)
	if err != nil {
		deviceLog.Error(fmt.Sprintf("writing the device registry to %s failed: %v -- this change exists only in memory and will be lost on restart", r.backend.Describe(), err))
		return
	}
	if conflicted {
		deviceLog.Warn(fmt.Sprintf("the device registry was modified by another process while this change was pending (%s); this change was applied on top", r.backend.Describe()))
	}
	r.version = version
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
	if _, created := r.ensureLocked(deviceID, now); created {
		r.persistLocked()
	}
}

// ensureLocked returns deviceID's Info, creating it (with created =
// true) the first time it is seen. Since #1281's audit, this is the
// only remaining consumer of clear-on-every-push cache invalidation --
// there is no cache left to clear (see Resolve's doc comment) -- so a
// caller only ever needs to persist on the created branch.
func (r *Registry) ensureLocked(deviceID string, now time.Time) (info *Info, created bool) {
	info, ok := r.byID[deviceID]
	if !ok {
		info = &Info{ID: deviceID, Name: deviceID, FirstSeen: now}
		r.byID[deviceID] = info
		created = true
	}
	if info.FirstSeen.IsZero() {
		info.FirstSeen = now
	}
	return info, created
}

// Resolve maps a syslog source IP to the id its events are stored
// under, recording that a line was just received from it. It is
// intended to be called from the single store-writer goroutine on the
// ingest path, so the write lock it takes is uncontended in practice;
// the RWMutex exists for concurrent /api/devices reads, not for
// ingest-side concurrency.
//
// Issue #1281's audit narrowed attribution to two steps, both actual
// operator evidence rather than a router's own claim about itself:
//
//   - (a) config.yaml's devices[].sourceIp -- the operator said so.
//   - (b) Info.AcceptedIP -- a device the operator issued an enrolment
//     token for, redeemed by a "mikroview-enrol <token>" line actually
//     arriving from this address (Registry.TryEnrol), or by the
//     once-at-startup upgrade nudge (EnrolFromPushedAddresses).
//   - otherwise the source is unattributed. It is remembered as a
//     Source, not minted as a device named after its own IP, and the
//     address itself is returned so the lines are stored and shown
//     exactly as before -- under an id that claims nothing.
//
// What this no longer does is the removed step (b) from before #1281:
// trusting a router's own pushed /ip/address table to say which address
// is its own. That table is still stored and served for display (GET
// /api/routeros/{device}/addresses), but a router is not a trustworthy
// witness to its own identity purely by asserting an address in a
// payload an ingest token merely let it push -- see docs/decisions and
// this issue's audit for the full reasoning. In production this branch
// is close to unreachable besides: the listener gate (internal/syslog,
// EnrolmentGate) refuses a line from any address that is not already
// sourceIp or AcceptedIP before Resolve is ever called with it, so an
// address only lands here as an unattributed Source through a caller
// that bypasses the gate (a test, or a future second ingestion path).
func (r *Registry) Resolve(sourceIP string, now time.Time) (deviceID string) {
	key := normalizeIP(sourceIP)

	r.mu.Lock()
	defer r.mu.Unlock()

	// (a) declared in config.yaml.
	if info, ok := r.byIP[key]; ok {
		r.seenLocked(info, key, now)
		return info.ID
	}

	// (b) enrolled by token.
	if info, ok := r.byAcceptedIP[key]; ok {
		r.seenLocked(info, key, now)
		// Attributed now, so it is no longer an address nobody has
		// claimed -- drop any record of it as one, same as step (a)
		// always implicitly does (a config.yaml address is never in
		// r.sources to begin with). The lines it sent while unattributed
		// stay where they were stored.
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
}

// pruneRefusedLocked is pruneLocked for the refused-address list --
// same oldest-last-seen-first eviction, capped at maxRefusedAddresses.
func (r *Registry) pruneRefusedLocked() {
	if len(r.refused) <= maxRefusedAddresses {
		return
	}
	evict.DownTo(r.refused, evict.Target(maxRefusedAddresses), func(s *Refused) time.Time {
		return s.LastSeen
	})
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

// ErrDeviceExists is returned by Create for an id already in the
// registry, config.yaml-declared or otherwise.
var ErrDeviceExists = errors.New("device: a device with that id already exists")

// ErrDeviceNotFound is returned by Delete/MintEnrolment/BurnEnrolment
// for an id this registry does not hold.
var ErrDeviceNotFound = errors.New("device: no such device")

// ErrDeviceConfigured is returned by Delete for a device declared in
// config.yaml -- it is recreated from that file on every boot
// regardless of anything an API call does to it, so deleting it here
// would only reappear confusingly on the next restart. Remove it from
// config.yaml instead.
var ErrDeviceConfigured = errors.New("device: this device is declared in config.yaml; remove it there instead")

// Create declares a device by name alone, with no address (issue
// #1281): the admin path for a router that only ever sends logs and so
// has no ingest token to auto-discover it through Ensure, and no
// address to declare in config.yaml either -- what POST /api/devices
// backs. id is what every other device identity in this codebase is
// keyed by; the caller (the API handler) derives and validates it from
// the operator-supplied name before this is ever called.
func (r *Registry) Create(id, name string, now time.Time) (Info, error) {
	if id == "" {
		return Info{}, ErrDeviceNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[id]; exists {
		return Info{}, ErrDeviceExists
	}
	info := &Info{ID: id, Name: name}
	r.byID[id] = info
	r.persistLocked()
	return *info, nil
}

// Register records the operator's confirmation of a router on the
// device itself -- the ledger's final step (issue #1291) -- stamping
// RegisteredAt and taking the name they confirmed it under.
//
// It grants nothing. AcceptedIP is deliberately not touched here, and
// no path through this function can set it: that is the whole point of
// splitting registration from acceptance. An attacker who can make an
// admin's browser issue this request gets a renamed device with a date
// on it, and no ability to have any address treated as a log source.
// Acceptance still requires a valid, unexpired, single-use enrolment
// token arriving over real syslog traffic from the address being
// accepted (TryEnrol).
//
// Registering again is idempotent in effect but re-stamps the date --
// the operator confirmed it again, and the later confirmation is the
// true one. A config.yaml-declared device refuses with
// ErrDeviceConfigured for Delete's reason: it is rebuilt from that file
// on every boot, so nothing written here would survive a restart.
func (r *Registry) Register(id, name string, now time.Time) (Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.byID[id]
	if !ok {
		return Info{}, ErrDeviceNotFound
	}
	if info.Configured {
		return Info{}, ErrDeviceConfigured
	}
	if name != "" {
		info.Name = name
	}
	info.RegisteredAt = now
	r.persistLocked()
	return *info, nil
}

// Delete removes a device this registry itself created (Create or
// Ensure) -- a config.yaml declaration refuses with ErrDeviceConfigured
// instead, since it would simply reappear on the next boot. Clears the
// device's enrolled address (if any) and any pending enrolment token
// along with it, matching #1281's contract that deleting a device
// clears its address.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, ok := r.byID[id]
	if !ok {
		return ErrDeviceNotFound
	}
	if info.Configured {
		return ErrDeviceConfigured
	}
	if info.AcceptedIP != "" {
		delete(r.byAcceptedIP, normalizeIP(info.AcceptedIP))
	}
	r.burnPendingLocked(id)
	delete(r.byID, id)
	r.persistLocked()
	return nil
}

// OwnPrefixes returns every device's enrolled and config-declared
// address as a /32 (or /128) prefix -- issue #1281's replacement source
// for internal/droplist.OwnRanges' "that is your router's own address"
// refusal, which used to read the routers' own pushed /ip/address
// tables (routerstate.Store.OwnPrefixes) the same way attribution used
// to. A pushed table is exactly the kind of self-reported claim this
// issue's audit stopped trusting for anything security-relevant; a
// device's SourceIP/AcceptedIP is real operator or token evidence, so
// this is the narrower, no-longer-router-supplied answer to the same
// question. Only IPv4 is returned: droplist.Validate only ever accepts
// IPv4 entries, so an IPv6 address here could never overlap anything it
// would check against anyway.
func (r *Registry) OwnPrefixes() []netip.Prefix {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []netip.Prefix
	seen := make(map[string]bool)
	add := func(addr string) {
		if addr == "" || seen[addr] {
			return
		}
		a, err := netip.ParseAddr(addr)
		if err != nil || !a.Is4() {
			return
		}
		seen[addr] = true
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	for _, info := range r.byID {
		add(info.SourceIP)
		add(info.AcceptedIP)
	}
	return out
}
