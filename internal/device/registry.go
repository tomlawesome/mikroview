// SPDX-License-Identifier: AGPL-3.0-only

// Package device resolves syslog source IPs to RouterOS device identity.
package device

import (
	"net"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
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
// ones this keeps. Configured devices are skipped entirely.
func (r *Registry) pruneLocked() {
	discovered := make([]*Info, 0, len(r.byIP))
	for _, info := range r.byIP {
		if !info.Configured {
			discovered = append(discovered, info)
		}
	}
	over := len(discovered) - maxDiscoveredDevices
	if over <= 0 {
		return
	}
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].LastSeen.Before(discovered[j].LastSeen) })
	for i := 0; i < over && i < len(discovered); i++ {
		delete(r.byIP, discovered[i].SourceIP)
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
