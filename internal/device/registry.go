// SPDX-License-Identifier: AGPL-3.0-only

// Package device resolves syslog source IPs to RouterOS device identity.
package device

import (
	"net"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/evict"
)

// Info describes a RouterOS device mikroview has received log data from.
type Info struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SourceIP   string    `json:"sourceIp"`
	Configured bool      `json:"configured"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
	EventCount uint64    `json:"eventCount"`
}

// NameLookup is the one method of internal/naming.Resolver this package
// needs: the display name to show for a device id, "" when nothing
// names it. An interface rather than the concrete type so device does
// not import naming (naming's resolver is built later in main, once the
// entity store exists) and so tests can supply a name without one.
type NameLookup interface {
	Device(id string) string
}

// Registry resolves syslog source IPs to device identity, and tracks
// liveness/volume per device for the /api/devices endpoint.
//
// Sources not present in the configured device list are auto-discovered
// rather than dropped, so events are never lost just because a router
// hasn't been added to config.yaml yet — the user can see the
// unregistered source IP in the UI and add it there.
type Registry struct {
	mu   sync.RWMutex
	byIP map[string]*Info

	// configuredEntries is how many of byIP's entries came from
	// config.yaml -- set once in NewRegistry and stable for the
	// Registry's whole lifetime, since Resolve only ever creates a *new*
	// entry for a key not already present and never replaces an existing
	// one. pruneLocked's O(1) guard needs this: byIP mixes non-evictable
	// configured entries with evictable discovered ones in a single map,
	// so a guard comparing len(byIP) against maxDiscoveredDevices alone
	// would start pruning before the registry actually held
	// maxDiscoveredDevices *discovered* entries. See #370.
	configuredEntries int

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

// maxDiscoveredDevices bounds how many *auto-discovered* sources the
// registry retains. Resolve creates an entry for any previously-unseen
// syslog source IP, so without a cap the map grows with whatever
// reaches the listener.
//
// This comment used to justify the cap by UDP source spoofing. That is
// no longer the shape of it: #189 removed every plaintext listener, and
// syslog.ListenTLS is the only one left, so minting an entry now costs
// a completed TCP+TLS handshake from the address in question -- an
// attacker cannot forge arbitrary sources, only their own. What has not
// changed is that it takes *no credentials*: the listener sets no
// ClientAuth (RouterOS's logging action has no client-certificate
// option), so anyone who can reach the port can still add their own
// address as a device. The cap is still needed; the reason is narrower
// than it was, and worth stating accurately because this is the comment
// the next person reasons from.
//
// Configured devices are never counted against this cap or evicted by
// it: those are routers the operator declared in config.yaml, and
// losing one to a flood of forged packets would be the attack
// succeeding by another route. Only discovered entries are evictable.
//
// 4096 matches internal/detect's maxTrackedSources, which bounds the
// same class of per-source state for the same reason. A var so tests
// can shrink it.
var maxDiscoveredDevices = 4096

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
	r := &Registry{byIP: make(map[string]*Info)}
	for _, d := range configured {
		key := normalizeIP(d.SourceIP)
		r.byIP[key] = &Info{
			ID:         d.ID,
			Name:       d.Name,
			SourceIP:   key,
			Configured: true,
		}
	}
	// Counted once, here, rather than derived from byIP on every prune --
	// see configuredEntries' doc comment.
	r.configuredEntries = len(r.byIP)
	return r
}

// Resolve maps a syslog source IP to a device ID, recording that an event
// was just received from it. It is intended to be called from the single
// store-writer goroutine on the ingest path, so the write lock it takes is
// uncontended in practice; the RWMutex exists for concurrent /api/devices
// reads, not for ingest-side concurrency.
func (r *Registry) Resolve(sourceIP string, now time.Time) (deviceID string) {
	key := normalizeIP(sourceIP)

	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.byIP[key]
	if !ok {
		info = &Info{ID: key, Name: key, SourceIP: key}
		r.byIP[key] = info
	}
	if info.FirstSeen.IsZero() {
		info.FirstSeen = now
	}
	info.LastSeen = now
	info.EventCount++
	r.pruneLocked()
	return info.ID
}

// pruneLocked evicts the least-recently-seen *discovered* devices once
// they exceed maxDiscoveredDevices. Oldest-LastSeen-first, mirroring
// MACRegistry.pruneLocked -- under a flood of spoofed sources the
// genuine routers are the ones still sending, so they are exactly the
// ones this keeps. Configured devices are never evicted and never
// counted against the cap.
//
// #370: this used to walk and re-sort the entire byIP map on every
// single call, with no guard at all, and then evicted back to exactly
// the cap -- so the very next newly-discovered source overflowed again
// and paid the full walk once more. Resolve runs synchronously on the
// single ingest goroutine for every ingested event, so once the
// 4096-entry discovery cap filled, every subsequent event paid that
// walk: measured at roughly 108us/call at the cap against 0.4us/call
// empty, enough to back up the raw ingest channel and start silently
// dropping real router log records. Same defect and same remedy as
// MACRegistry.pruneLocked and rules.Store.pruneLocked (see #285 and
// 3d27200): an O(1) guard first, then a batched shed via internal/evict
// so a prune leaves headroom instead of putting the registry right back
// at the cap.
//
// The guard compares against maxDiscoveredDevices *plus*
// configuredEntries, not maxDiscoveredDevices alone, because byIP
// interleaves non-evictable configured entries with evictable
// discovered ones in one map -- see configuredEntries' doc comment.
// That interleaving also means byIP can't be handed to evict.DownTo
// directly, so once the guard trips, the discovered subset is
// materialized into its own map first; that extra scan only happens
// behind the same guard, so it is amortised the same way the shed
// itself is.
func (r *Registry) pruneLocked() {
	if len(r.byIP) <= maxDiscoveredDevices+r.configuredEntries {
		return
	}

	discovered := make(map[string]*Info, len(r.byIP)-r.configuredEntries)
	for k, info := range r.byIP {
		if !info.Configured {
			discovered[k] = info
		}
	}
	keys := make([]string, 0, len(discovered))
	for k := range discovered {
		keys = append(keys, k)
	}

	evict.DownTo(discovered, evict.Target(maxDiscoveredDevices), func(info *Info) time.Time {
		return info.LastSeen
	})

	for _, k := range keys {
		if _, kept := discovered[k]; !kept {
			delete(r.byIP, k)
		}
	}
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

// List returns a snapshot of all known devices, configured and
// auto-discovered alike, with each Name resolved through NameLookup
// (issue #600) so a stored rename is what every reader sees.
//
// Ordering is configured devices first, then by id -- stable, and not
// the map order this used to return. Callers picking devices[0] as
// "the router this instance watches" were relying on a single-device
// deployment for that to hold: with two, the same request answered in a
// different order each time (live-setup-wizard-source-split.mjs records
// what that cost, and live-env.sh declares one device to avoid it).
// Configured first matches what the fleet already sorts by
// (frontend/src/lib/fleet.ts): a declared router is what an operator
// set out to watch; a discovered one is secondary information.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Info, 0, len(r.byIP))
	for _, info := range r.byIP {
		v := *info
		if r.names != nil {
			if name := r.names.Device(v.ID); name != "" {
				v.Name = name
			}
		}
		// A device always displays as something. A discovered one is
		// already named after its own source address (Resolve), and a
		// declaration with no name would otherwise show as an empty
		// cell in every fleet card and every row -- the raw id is the
		// honest fallback, and the one the editor promises when a label
		// is removed ("leave empty to show the raw value again").
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

// MultihomedCandidate pairs one declared device that has never received
// an event under its own sourceIp with every currently auto-discovered
// device -- the evidence shape issue #442 describes: "the declared
// device... never matches anything and sits idle" while "the real
// stream auto-discovers as a second device". A RouterOS device is
// multi-homed by definition (an address on every subnet it routes), so
// syslog frequently arrives stamped with a different interface address
// than the one declared as sourceIp.
type MultihomedCandidate struct {
	// DeclaredID and DeclaredSourceIP identify the configured device
	// that has never matched any syslog traffic.
	DeclaredID       string
	DeclaredSourceIP string
	// Discovered is every device auto-discovered from syslog traffic
	// that has not been declared in config.yaml, in ID order. Registry
	// has no further evidence to narrow this to a single "same box"
	// answer -- see MultihomedCandidates' own comment -- so every live
	// discovered device is reported, not a guess at one.
	Discovered []Info
}

// MultihomedCandidates returns one entry per configured device that has
// received zero events under its own declared sourceIp, alongside every
// device auto-discovered from syslog traffic that is not itself
// declared. nil when either side is empty: a declared device with no
// traffic yet is unremarkable if nothing has been discovered either
// (the router may simply not have started logging yet), and a
// discovered device is unremarkable on its own if every declared device
// is receiving its own traffic fine.
//
// This is deliberately just the evidence, not a diagnosis: Registry
// only knows source IPs, first/last-seen times, and which devices came
// from config.yaml, so it cannot itself tell which discovered device
// (if any) is actually the same physical router as a given silent
// declared one -- that judgement, and where to surface it to the
// operator, is left to the caller. See #442.
func (r *Registry) MultihomedCandidates() []MultihomedCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var silent []Info
	var discovered []Info
	for _, info := range r.byIP {
		if info.Configured {
			if info.EventCount == 0 {
				silent = append(silent, *info)
			}
			continue
		}
		discovered = append(discovered, *info)
	}
	if len(silent) == 0 || len(discovered) == 0 {
		return nil
	}

	sort.Slice(silent, func(i, j int) bool { return silent[i].ID < silent[j].ID })
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].ID < discovered[j].ID })

	out := make([]MultihomedCandidate, 0, len(silent))
	for _, s := range silent {
		out = append(out, MultihomedCandidate{
			DeclaredID:       s.ID,
			DeclaredSourceIP: s.SourceIP,
			Discovered:       discovered,
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
