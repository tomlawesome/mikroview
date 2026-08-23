// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("device-mac")

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
	mu sync.RWMutex
	// wb is nil when persistence isn't configured -- see
	// persist.WriteBehind for what it now owns: write-behind, the
	// backend deadline, the after-write-stamped rate limit/back-off, and
	// version bookkeeping (issue #400). Every method on it is a safe
	// no-op on a nil receiver.
	wb    *persist.WriteBehind
	byMAC map[string]*MACEntry
}

// macRegistryPersistMinInterval rate-limits persistLocked's actual disk
// writes, same reasoning as flags.persistMinInterval: Seen is called on
// the ingest hot path for every single event carrying a SrcMAC, not just
// the rare truly-new ones, so an unconditional marshal + atomic rename
// per call would put disk I/O directly on that path. A var rather than a
// const so tests that need every call to persist immediately can shrink
// it.
// Now persist.WriteBehind's MinInterval (see OpenMACRegistryWithBackend)
// rather than a field this type checks itself -- the rate-limiting/
// back-off logic that used to live here, and its #377 stall-under-load
// defect, both moved to that type (issue #400).
var macRegistryPersistMinInterval = time.Second

// OpenMACRegistry loads path if it exists (a missing file is the
// expected first-run case, not an error) and returns a MACRegistry that
// persists to it from then on. An empty path is the expected
// "persistence not configured" case: a fully usable, in-memory-only
// registry is returned -- every MAC will look "new" again on every
// restart, same trade-off flags.Open's empty-path case documents. A
// document that exists but cannot be read or parsed is a hard error
// (issue #378): the caller gets (nil, err) rather than a registry whose
// live backend would overwrite that document on the first write. See
// persist.Open.
func OpenMACRegistry(path string) (*MACRegistry, error) {
	if path == "" {
		return OpenMACRegistryWithBackend(nil)
	}
	return OpenMACRegistryWithBackend(persist.NewFileBackend(path))
}

// OpenMACRegistryWithBackend is OpenMACRegistry against any persist.Backend
// -- a JSON file by default, or Postgres when configured (issue #131).
func OpenMACRegistryWithBackend(b persist.Backend) (*MACRegistry, error) {
	r := &MACRegistry{byMAC: make(map[string]*MACEntry)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the MAC registry", persist.WriteBehindOptions{
		MinInterval: macRegistryPersistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var list []*MACEntry
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		for _, e := range list {
			// A JSON array containing `null` unmarshals successfully
			// into a nil *MACEntry -- valid JSON, so the err check above
			// never catches it. Same defensive skip flags.Open uses.
			if e == nil || e.MAC == "" {
				continue
			}
			r.byMAC[normalizeMAC(e.MAC)] = e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.wb = wb
	return r, nil
}

// Flush forces this registry's write-behind writer to persist whatever
// is currently dirty now, without waiting out its usual debounce
// interval, and blocks until that attempt finishes or ctx expires -- see
// flags.Store.Flush's own doc comment for when this is the right call
// (a test, or a `-backup` CLI invocation racing a still-running
// process). A registry with no backend configured (wb == nil) is a safe
// no-op.
func (r *MACRegistry) Flush(ctx context.Context) error {
	return r.wb.Flush(ctx)
}

// Close stops this registry's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A registry with no backend configured (wb ==
// nil) is a safe no-op. Not safe to call Seen after Close.
func (r *MACRegistry) Close(ctx context.Context) error {
	return r.wb.Close(ctx)
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
//
// It sheds a batch rather than the exact overflow. Evicting back to
// exactly the cap leaves the registry full, so the *next* new MAC
// overflows too and pays the whole scan again -- and Seen runs on the
// single ingest goroutine, on a key (src-mac) that comes straight off
// unauthenticated syslog. Measured on the old code: 1,529 ns per Seen
// under the cap against 16-21 ms at it, from one TLS connection sending
// 50,000 lines with a rotating src-mac, which took about 75 ms to set
// up. Ingest fell to roughly 47 events/s. Persistence is on by default,
// so the poisoned registry came back after a restart and stayed until
// an operator deleted the file.
//
// Same defect and same remedy as internal/detect's, which was found and
// fixed first; internal/evict now holds the one implementation. See
// #285.
func (r *MACRegistry) pruneLocked() {
	if len(r.byMAC) <= maxMACRegistryEntries {
		return
	}
	evict.DownTo(r.byMAC, evict.Target(maxMACRegistryEntries), func(e *MACEntry) time.Time {
		return e.LastSeen
	})
}

// persistLocked encodes the current state and hands it to the
// write-behind writer (see persist.WriteBehind), which coalesces it
// with whatever else is pending and persists it off this goroutine,
// under its own deadline and rate limit. Marshal failures are swallowed
// rather than surfaced to Seen's caller: the in-memory state (which
// every read goes through) stays correct either way, so a transient
// disk issue degrades to "won't survive a restart right now" rather
// than breaking live detection. Must be called with r.mu already held --
// see flags.Store.persistLocked's own doc comment for the "lock covers
// the encode, not the backend call" contract this mirrors.
func (r *MACRegistry) persistLocked() {
	if r.wb == nil {
		return
	}
	data, err := json.MarshalIndent(r.listLocked(), "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding MAC registry for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	r.wb.MarkDirty(data)
}
