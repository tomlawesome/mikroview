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
	// LastIP is the source IP this MAC was last paired with on an event
	// (issue #675) -- an observation, not an identity: DHCP can hand the
	// same MAC a different address later, so this is "where to find it
	// now," not a claim the pairing is permanent. Set by NoteIP, entirely
	// separate from Seen's first/last-seen bookkeeping so a caller that
	// only wants "is this MAC new" is unaffected by whether IP pairing is
	// tracked at all. Empty until the first event carrying both a MAC and
	// an IP arrives for it.
	LastIP string `json:"lastIp,omitempty"`
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

	// dirty is set by Seen/NoteIP under mu and cleared once
	// runPersistLoop has encoded and handed the current state to wb --
	// see persistDirtyLocked and runPersistLoop. This is the fix for
	// #1087: Seen, called on the ingest hot path for every event
	// carrying a SrcMAC, only ever flips a bool under a lock it already
	// holds; it never marshals.
	dirty bool

	// wake/stopPersist/persistDone drive runPersistLoop, the one
	// background goroutine a registry with a backend runs -- started in
	// OpenMACRegistryWithBackend, stopped by Close. All nil when wb is
	// nil (no backend configured, nothing to run off the ingest
	// goroutine). wake is buffered 1, the same "never block the caller,
	// coalesce a burst into one wakeup" shape persist.WriteBehind's own
	// wake channel uses.
	wake        chan struct{}
	stopPersist chan struct{}
	persistDone chan struct{}
}

// macRegistryPersistMinInterval rate-limits the write-behind writer's
// actual disk writes, same reasoning as flags.persistMinInterval: Seen
// is called on the ingest hot path for every single event carrying a
// SrcMAC, not just the rare truly-new ones, so an unconditional marshal
// + atomic rename per call would put disk I/O directly on that path. A
// var rather than a const so tests that need every call to persist
// immediately can shrink it. See macRegistryPersistFlushInterval for the
// separate rate limit on the encode itself.
// Now persist.WriteBehind's MinInterval (see OpenMACRegistryWithBackend)
// rather than a field this type checks itself -- the rate-limiting/
// back-off logic that used to live here, and its #377 stall-under-load
// defect, both moved to that type (issue #400).
var macRegistryPersistMinInterval = time.Second

// macRegistryPersistClock is a test seam for the write-behind back-off's
// own clock -- persist.WriteBehind's run loop reads it and waits on it --
// with nil (every production caller) meaning persist's real one. Same
// "package var a test overrides, real by default" convention
// macRegistryPersistMinInterval above already uses, and the exact twin of
// flags.persistClock (#941). #1039: this store's copy of the
// sustained-failure back-off test measured real elapsed wall-clock time
// around a real sleep and inferred how many back-off windows "must" have
// passed, which flaked under a loaded CI runner precisely because that
// inference is itself scheduling-dependent. A test setting this to a fake
// persist.Clock advances the window by hand instead of guessing at it
// from elapsed time.
var macRegistryPersistClock persist.Clock

// macRegistryPersistFlushInterval is the minimum spacing runPersistLoop
// leaves between one encode finishing and the next starting, stamped
// after an encode completes rather than before it starts -- the same
// "stamped after" reasoning persist.WriteBehind's own MinInterval
// documents (its fix for #377), applied one layer up. Unrelated to
// macRegistryPersistMinInterval, which rate-limits the write-behind
// writer's own backend attempts -- this rate-limits the encode itself,
// which used to run on the caller's goroutine on every single Seen call
// (issue #1087). A var rather than a const so a test can shrink it
// rather than wait out two real seconds.
var macRegistryPersistFlushInterval = 2 * time.Second

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
		Clock:       macRegistryPersistClock,
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
	if r.wb != nil {
		r.wake = make(chan struct{}, 1)
		r.stopPersist = make(chan struct{})
		r.persistDone = make(chan struct{})
		go r.runPersistLoop()
	}
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
	r.persistIfDirty()
	return r.wb.Flush(ctx)
}

// Close stops runPersistLoop and this registry's write-behind writer
// goroutine, flushing whatever is still dirty within persist.SaveTimeout
// before returning -- main's shutdown joins on this so a change made
// right before exit is not silently dropped. A registry with no backend
// configured (wb == nil) is a safe no-op. Not safe to call Seen after
// Close.
func (r *MACRegistry) Close(ctx context.Context) error {
	if r.wb != nil {
		close(r.stopPersist)
		<-r.persistDone
	}
	r.persistIfDirty()
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
	r.persistDirtyLocked()
	return isNew
}

// NoteIP records that mac was last paired with ip (issue #675: the
// Entities page's named-things table needs a way to show a host's MAC
// and its persisted first/last-seen history side by side with the
// entity, which is keyed on IP, not MAC). A no-op unless the registry
// already holds mac -- Seen always runs first at every call site this
// has today, so an absent entry means an empty mac, and there is
// nothing to pair an IP against. Also a no-op when ip is already the
// stored LastIP, so a steady stream from an unmoving device doesn't
// dirty the write-behind writer on every single packet.
func (r *MACRegistry) NoteIP(mac, ip string) {
	if mac == "" || ip == "" {
		return
	}
	key := normalizeMAC(mac)

	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.byMAC[key]
	if !ok || e.LastIP == ip {
		return
	}
	e.LastIP = ip
	r.persistDirtyLocked()
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

// persistDirtyLocked records that the in-memory state has changed since
// the last encode and wakes runPersistLoop -- a non-blocking, buffered
// send, so a burst of calls while the loop is already awake (or busy
// encoding) costs nothing beyond the flag write. It never marshals and
// never touches wb directly -- see runPersistLoop and persistIfDirty for
// where that work actually happens, off this goroutine. Every field
// Seen touches on a repeat sighting (LastSeen) is treated the same as a
// brand-new entry here: both simply mark the registry dirty and wake the
// loop, which decides when to actually pay for the encode -- see
// runPersistLoop's own doc comment for why a separate "structural vs.
// not" tier is not needed once the encode itself is off the ingest
// goroutine. Must be called with r.mu held.
func (r *MACRegistry) persistDirtyLocked() {
	if r.wb == nil {
		return
	}
	r.dirty = true
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// runPersistLoop is this registry's one background goroutine -- started
// by OpenMACRegistryWithBackend when a backend is configured, stopped by
// Close -- that owns every encode. Idle (nothing dirty), it blocks on
// wake so it costs nothing between changes. Once woken, it encodes right
// away if this is the first dirty change in a while (the same "no wait,
// since the last-attempt stamp starts zero" first-call behaviour
// persist.WriteBehind.run documents), then leaves at least
// macRegistryPersistFlushInterval before the next encode, however many
// further mutations land in between -- exactly the coalescing MarkDirty
// already gives the backend write, one layer up for the encode itself.
//
// Before #1087, Seen unconditionally ran json.MarshalIndent over the
// whole registry on every call, on the single ingest goroutine, under
// r.mu -- including the ordinary case where a MAC already known simply
// had its LastSeen bumped. Seen now only ever flips r.dirty; this loop
// is the only place that marshals.
func (r *MACRegistry) runPersistLoop() {
	defer close(r.persistDone)
	var lastEncode time.Time
	for {
		r.mu.Lock()
		dirty := r.dirty
		r.mu.Unlock()

		if !dirty {
			select {
			case <-r.wake:
				continue
			case <-r.stopPersist:
				return
			}
		}

		if !lastEncode.IsZero() {
			if wait := macRegistryPersistFlushInterval - time.Since(lastEncode); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-r.stopPersist:
					timer.Stop()
					r.persistIfDirty()
					return
				}
			}
		}

		r.persistIfDirty()
		lastEncode = time.Now()
	}
}

// persistIfDirty encodes and hands the current state to wb if anything
// has changed since the last successful encode, and is a no-op
// otherwise. The dirty flag is cleared in the same critical section that
// takes the listLocked snapshot, so a mutation landing after this
// unlocks is never lost -- it simply sets r.dirty again for the next
// call to pick up. A marshal failure puts the flag back so the next call
// retries rather than silently giving up on the change. Marshal failures
// are otherwise swallowed rather than surfaced to a caller: the
// in-memory state (which every read goes through) stays correct either
// way, so a transient encoding problem degrades to "won't survive a
// restart right now" rather than breaking live detection.
func (r *MACRegistry) persistIfDirty() {
	r.mu.Lock()
	if !r.dirty || r.wb == nil {
		r.mu.Unlock()
		return
	}
	r.dirty = false
	list := r.listLocked()
	r.mu.Unlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding MAC registry for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
		return
	}
	r.wb.MarkDirty(data)
}
