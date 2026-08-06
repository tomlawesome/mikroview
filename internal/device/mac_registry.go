package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MACEntry is one LAN client MAC address' first/last-seen history.
type MACEntry struct {
	MAC       string    `json:"mac"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// maxMACRegistryEntries bounds the registry the same way every other
// buffer in mikroview has an explicit ceiling (see flags.maxFlags,
// detect.maxTrackedSources, the frontend's MAX_CLIENT_EVENTS) -- a LAN's
// distinct-MAC count is normally in the hundreds at most, so this is a
// generous safety net against unbounded growth (e.g. a spoofed/rotating
// source MAC on the LAN side) rather than a limit expected to be hit in
// normal use. A var rather than a const so tests can shrink it.
var maxMACRegistryEntries = 50_000

// MACRegistry persists every store.Event.SrcMAC mikroview has ever
// observed, keyed by the MAC address itself -- a different concept from
// Registry above, which tracks *router* source IPs (which RouterOS
// device sent this syslog line), not LAN client MACs. Its whole purpose
// is answering "have we ever seen this MAC before," which only means
// anything if the answer survives a restart -- the 24h event-retention
// window alone is nowhere near long enough for "new" to be meaningful,
// so this follows the same JSON-file + atomic-write + mutex persistence
// convention as flags.Store rather than the in-memory-only default the
// rest of mikroview uses (see SECURITY.md's "Data handling" section).
// The zero value is not usable; construct with OpenMACRegistry.
type MACRegistry struct {
	mu    sync.RWMutex
	path  string
	byMAC map[string]*MACEntry

	// lastPersist backs persistLocked's rate limiting -- see
	// macRegistryPersistMinInterval.
	lastPersist time.Time
}

// macRegistryPersistMinInterval rate-limits persistLocked's actual disk
// writes, same reasoning as flags.persistMinInterval: Seen is called on
// the ingest hot path for every single event carrying a SrcMAC, not just
// the rare truly-new ones, so an unconditional marshal + atomic rename
// per call would put disk I/O directly on that path. A var rather than a
// const so tests that need every call to persist immediately can shrink
// it.
var macRegistryPersistMinInterval = time.Second

// OpenMACRegistry loads path if it exists (a missing file is the
// expected first-run case, not an error) and returns a MACRegistry that
// persists to it from then on. An empty path is the expected
// "persistence not configured" case: a fully usable, in-memory-only
// registry is returned -- every MAC will look "new" again on every
// restart, same trade-off flags.Open's empty-path case documents. A
// malformed file is treated as empty rather than failing, so a
// corrupted registry file never blocks mikroview from starting. Either
// way the returned *MACRegistry is always safe to use unconditionally;
// a non-nil error is only ever informational, for the caller to log.
func OpenMACRegistry(path string) (*MACRegistry, error) {
	r := &MACRegistry{path: path, byMAC: make(map[string]*MACEntry)}
	if path == "" {
		return r, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return r, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return r, err
	}

	var list []*MACEntry
	if err := json.Unmarshal(data, &list); err != nil {
		return r, err
	}
	for _, e := range list {
		// A JSON array containing `null` unmarshals successfully into a
		// nil *MACEntry -- valid JSON, so the err check above never
		// catches it. Same defensive skip flags.Open uses.
		if e == nil || e.MAC == "" {
			continue
		}
		r.byMAC[normalizeMAC(e.MAC)] = e
	}
	return r, nil
}

// normalizeMAC lowercases a MAC address so textually-different forms of
// the same address (RouterOS logs lowercase in practice, but nothing
// guarantees every source does) collapse to one registry entry, the same
// reasoning Registry's normalizeIP already establishes for source IPs.
func normalizeMAC(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
}

// Seen records that mac was observed at now, and reports whether this is
// the first time this registry has ever seen it -- true exactly once per
// MAC, across the registry's entire persisted history, not just this
// process run. An empty mac is a no-op reporting false: not every event
// carries a SrcMAC (RouterOS only reports it on LAN-side/bridge-aware
// rules; WAN-side rules typically don't, since L2 info is gone by the
// time traffic is routed), and an empty string is never a meaningful
// device identity worth tracking as "new."
func (r *MACRegistry) Seen(mac string, now time.Time) bool {
	if mac == "" {
		return false
	}
	key := normalizeMAC(mac)
	if key == "" {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.byMAC[key]
	isNew := !ok
	if !ok {
		e = &MACEntry{MAC: key, FirstSeen: now}
		r.byMAC[key] = e
	}
	e.LastSeen = now

	r.pruneLocked()
	r.persistLocked()
	return isNew
}

// List returns a snapshot of every known MAC entry, most-recently-seen
// first. Mutating the returned slice/entries never affects the
// registry's own state -- same independent-copy contract as
// Registry.List/flags.Store.List.
func (r *MACRegistry) List() []MACEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked()
}

func (r *MACRegistry) listLocked() []MACEntry {
	out := make([]MACEntry, 0, len(r.byMAC))
	for _, e := range r.byMAC {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// pruneLocked evicts the oldest-by-LastSeen entries once the registry is
// over maxMACRegistryEntries -- unlike flags.Store's pruneLocked (which
// only ever evicts *cleared* flags, since an active flag is something a
// human hasn't looked at yet), every entry here is equally disposable:
// there's no human-attention state attached to a MAC registry entry, so
// the simplest bound is "keep the most recently active MACs."
func (r *MACRegistry) pruneLocked() {
	over := len(r.byMAC) - maxMACRegistryEntries
	if over <= 0 {
		return
	}

	all := make([]*MACEntry, 0, len(r.byMAC))
	for _, e := range r.byMAC {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LastSeen.Before(all[j].LastSeen) })

	for i := 0; i < over && i < len(all); i++ {
		delete(r.byMAC, all[i].MAC)
	}
}

// persistLocked writes the current state to disk if persistence is
// configured and enough time has passed since the last write -- see
// macRegistryPersistMinInterval. Write failures are swallowed rather
// than surfaced to Seen's caller: the in-memory state (which every read
// goes through) stays correct either way, so a transient disk issue
// degrades to "won't survive a restart right now" rather than breaking
// live detection.
func (r *MACRegistry) persistLocked() {
	if r.path == "" {
		return
	}
	if now := time.Now(); now.Sub(r.lastPersist) < macRegistryPersistMinInterval {
		return
	} else {
		r.lastPersist = now
	}
	data, err := json.MarshalIndent(r.listLocked(), "", "  ")
	if err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, r.path) // same filesystem, so this is atomic
}
