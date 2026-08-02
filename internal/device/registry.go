// Package device resolves syslog source IPs to RouterOS device identity.
package device

import (
	"net"
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
		info = &Info{ID: key, Name: key, SourceIP: key, FirstSeen: now}
		r.byIP[key] = info
	}
	info.LastSeen = now
	info.EventCount++
	return info.ID
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
