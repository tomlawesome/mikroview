// SPDX-License-Identifier: AGPL-3.0-only

// Package device resolves syslog source IPs to RouterOS device identity.
package device

import (
	"net"
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

// List returns a snapshot of all known devices, configured and
// auto-discovered alike.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Info, 0, len(r.byIP))
	for _, info := range r.byIP {
		out = append(out, *info)
	}
	return out
}

func normalizeIP(s string) string {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}
