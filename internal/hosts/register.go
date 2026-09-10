// SPDX-License-Identifier: AGPL-3.0-only

// Package hosts is the presence register behind the living topology
// (issue #1016): the record of which hosts the syslog feed has actually
// shown, so a host that stops talking can go quiet on the map instead of
// silently disappearing from it.
//
// The map used to derive its hosts entirely in the browser, from the
// live event buffer (frontend/src/lib/zones.svelte.ts). That makes
// presence a property of the buffer rather than of the network: a host
// that stops talking scrolls out and vanishes, with nothing left to say
// it was ever there. The owner's intention for #1016 is the opposite --
// "a host that stops talking is not removed: it goes quiet and turns
// grey. You can mark it as intended (quiet on purpose), or dismiss it,
// which removes it. If it reappears in the feed it comes back by
// itself, whichever you chose."
//
// This package holds the "was ever there" half. It records nothing the
// feed did not already show and asks the network nothing -- mikroview
// observes, it never probes (see AGENTS.md) -- so a host exists here
// only because an event carrying it arrived.
//
// # What counts as a host
//
// Exactly what the map already counted: the source address of an event
// arriving on an inbound interface, when that address is not public.
// See Registers, which is a faithful port of the browser's own rule so
// the two halves cannot disagree about what a host is.
//
// # The cap
//
// MaxHosts bounds the register at 10,000 entries. Source addresses are
// attacker-controlled -- a spoofed source is one forged field in one
// syslog line -- so an unbounded map keyed by them is a memory-growth
// vector reachable by anybody able to make the router log. At the cap
// the oldest LastSeen *without* a mark is evicted: an operator's own
// decision about a host is the one thing here that cannot be rebuilt
// from the feed, so it is the last thing dropped. When every entry
// carries a mark, the new observation is shed instead (see shed).
//
// # Persistence
//
// Optional JSON through internal/persist, on the same "empty path means
// in-memory only" contract as every other optional store in this
// codebase (see SECURITY.md). Unlike internal/coverage, which persists
// synchronously because a declaration is a rare interactive write, this
// is updated on *every ingested event*, so it follows internal/flags
// instead: write-behind and encoded off the ingest goroutine (see
// persistDirtyLocked and runPersistLoop), never a disk write -- nor a
// JSON marshal -- on the ingest path.
package hosts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("hosts")

// MaxHosts is the hard ceiling on how many hosts the register holds --
// see the package comment for why it exists. A real network's private
// address space behind one router is orders of magnitude smaller than
// this; reaching it means either an address range far larger than a
// mikroview deployment is meant for, or forged source addresses.
const MaxHosts = 10_000

// maxKeyLength/maxReasonLength bound a mark's Key and Reason, the same
// ceilings and for the same reasons as internal/coverage's: generous
// next to any real interface|address pair or human-written note, tight
// enough that the UI and the audit trail stay renderable.
const (
	maxKeyLength    = 120
	maxReasonLength = 400
)

// MarkKind is what an operator said about a quiet host.
type MarkKind string

const (
	// MarkIntended: quiet on purpose. Survives the host reappearing --
	// "say why once and it stays said", the same promise a coverage
	// declaration makes about a quiet boundary.
	MarkIntended MarkKind = "intended"
	// MarkDismissed: take it off the map. Cleared automatically the next
	// time an event for that host arrives, because a host that is back
	// is not dismissed -- see Observe.
	MarkDismissed MarkKind = "dismissed"
)

// Valid reports whether k is one of the two kinds. Anything else is a
// caller error rather than a stored value.
func (k MarkKind) Valid() bool { return k == MarkIntended || k == MarkDismissed }

// Mark is an operator's decision about one quiet host. By and At are
// always set server-side, from the session and the clock -- a caller
// never gets to sign a mark with somebody else's name or backdate it,
// the same convention coverage.Declaration follows.
type Mark struct {
	Kind   MarkKind  `json:"kind"`
	Reason string    `json:"reason,omitempty"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

// Host is one entry in the register: a host the feed has shown, and
// whatever an operator has said about it.
//
// Label is the last hostname seen for the address and may be empty --
// naming is resolved per event upstream (internal/naming), and a host
// whose name nothing supplies is still a host. It is deliberately not
// sticky-per-field beyond "last non-empty wins": the register reports
// what the feed said most recently, not a merged best guess.
type Host struct {
	Key       string    `json:"key"`
	Iface     string    `json:"iface"`
	IP        string    `json:"ip"`
	Label     string    `json:"label,omitempty"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Events    uint64    `json:"events"`
	Mark      *Mark     `json:"mark,omitempty"`
}

// ErrInvalidMark is returned by Register.Mark when the key, kind or
// reason fails validation: an unknown kind, or text that is empty, too
// long, not valid UTF-8, or carrying a control or Unicode format
// character. Same no-control-characters rule as
// internal/coverage.validateText and internal/entities, since both Key
// and Reason flow into the UI and the audit trail.
var ErrInvalidMark = errors.New("hosts: kind must be intended or dismissed, and key/reason must be valid text within the length limit")

// ErrUnknownHost is returned by Register.Mark for a key no event has
// ever registered. Marking a host the feed has never shown would invent
// presence rather than record it, which is the one thing this package
// must not do.
var ErrUnknownHost = errors.New("hosts: no host with that key has been seen on the feed")

// ValidateKey reports whether key is text this package will address: a
// non-empty, valid-UTF-8 string within maxKeyLength that carries no
// control or Unicode-format characters. Exported so an HTTP handler can
// check a key taken from a URL path *before* using it -- a key from a
// route is caller-controlled input, and it reaches the audit trail on
// the way past. Returns ErrInvalidMark or nil.
func ValidateKey(key string) error { return validateText(key, maxKeyLength) }

// KeyFor builds the register's key for an (interface, address) pair --
// the same "<iface>|<value>" style internal/coverage's declaration keys
// use, so the two read alike in a URL and in an audit line.
func KeyFor(iface, ip string) string { return iface + "|" + ip }

// ipv4Dotted is the browser rule's own shape test, character for
// character: four dot-separated runs of one to three digits, with no
// range check on the octets. Ported rather than replaced by
// net.ParseIP so the register and the map cannot disagree about what a
// host is -- see Registers.
var ipv4Dotted = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// isPublicIP is a faithful port of isPublicIp in
// frontend/src/lib/format.ts, including its two consequences worth
// naming out loud:
//
//   - anything that is not a dotted-quad IPv4 literal -- an IPv6
//     address, an empty string, a malformed field -- is reported *not*
//     public, and so counts as a host. That is what the map does today,
//     and this package's job is to record the same hosts the map draws,
//     not a better-judged set. MaxHosts is what bounds the cost of that
//     choice.
//   - the octets are not range-checked, so "999.1.2.3" reads as public
//     and is skipped.
//
// Each package in this codebase keeps its own small private copy of a
// public/private predicate rather than exporting one across a package
// boundary (see internal/engine/conditions.go's isPublicIP for the
// precedent). This copy exists to match the *frontend's* rule, which is
// not the same rule internal/engine's copy implements.
func isPublicIP(ip string) bool {
	m := ipv4Dotted.FindStringSubmatch(ip)
	if m == nil {
		return false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	switch {
	case a == 10:
		return false
	case a == 172 && b >= 16 && b <= 31:
		return false
	case a == 192 && b == 168:
		return false
	case a == 127:
		return false
	case a == 169 && b == 254:
		return false
	case a == 0:
		return false
	}
	return true
}

// Registers reports whether an event with this inbound interface and
// source address puts a host in the register.
//
// This is the map's own rule, mirrored: zones.svelte.ts adds a host to
// a zone when the event *arrived* on that interface (iface ===
// e.inInterface), the source address is present, and it is not public
// -- "the host that stands on this boundary is the private side of the
// event relative to it". Deliberately no broader than that: a
// destination address, an outbound interface or a public source would
// each be a different claim about what a host is than the one the map
// makes.
//
// The one place this is knowingly wider than what the map *draws*: the
// browser also drops interfaces it has classified as WAN or tunnel from
// its zone list, using router-pushed state the ingest path does not
// have in hand. A non-public source arriving on a WAN interface is
// unusual enough that recording it and letting the view decide is the
// honest split -- the register says what was seen, the map says what it
// draws.
func Registers(iface, ip string) bool {
	if iface == "" || ip == "" {
		return false
	}
	if isPublicIP(ip) {
		return false
	}
	// A key the mark endpoints could never address is not worth holding:
	// validation there would refuse it, so it could never be marked or
	// dismissed, and it would sit in the register consuming one of
	// MaxHosts' slots forever. This also bounds what a forged interface
	// or address field can put in the map.
	return validateText(KeyFor(iface, ip), maxKeyLength) == nil
}

// storeFile is the on-disk shape: an object wrapping the host list,
// mirroring internal/coverage's and internal/entities' storeFile.
type storeFile struct {
	Hosts []*Host `json:"hosts"`
}

// Register holds every host the feed has shown, keyed by KeyFor. The
// zero value is not usable; construct with Open.
type Register struct {
	mu sync.RWMutex
	// wb is nil when persistence is not configured, the same "nil means
	// off" convention internal/flags.Store follows. Every method on it
	// is a safe no-op on a nil receiver.
	wb    *persist.WriteBehind
	byKey map[string]*Host

	// dirty is set by any mutating method under mu and cleared once
	// runPersistLoop has encoded and handed the current state to wb --
	// see persistDirtyLocked and runPersistLoop. This is the whole fix
	// for #1087: the ingest goroutine only ever flips a bool under a
	// lock it already holds, never marshals.
	dirty bool

	// wake/stopPersist/persistDone drive runPersistLoop, the one
	// background goroutine a Register with a backend runs -- started in
	// OpenWithBackend, stopped by Close. All nil when wb is nil (no
	// backend configured, nothing to run off the ingest goroutine). wake
	// is buffered 1, the same "never block the caller, coalesce a burst
	// into one wakeup" shape persist.WriteBehind's own wake channel
	// uses.
	wake        chan struct{}
	stopPersist chan struct{}
	persistDone chan struct{}

	// encodeMu is held for the whole of persistIfDirty -- from taking
	// the dirty flag to handing the bytes to wb -- so a Flush arriving
	// while runPersistLoop is mid-encode waits for that encode instead of
	// finding nothing dirty anywhere and returning before the write.
	encodeMu sync.Mutex

	// shed counts observations dropped at the cap because every entry
	// held a mark, over this process's lifetime. Worth being able to say
	// how many rather than only that it happened, same as
	// internal/flags.Store.shedActive.
	shed uint64
}

// Open loads path if it exists (a missing file is the expected
// first-run case, not an error) and returns a Register that persists to
// it from then on. An empty path is the expected "persistence not
// configured" case: a fully usable, in-memory-only Register is
// returned. A document that exists but cannot be read or parsed is a
// hard error, the same fail-closed contract as internal/flags.Open --
// a live backend must never silently overwrite a document it could not
// read (#378).
func Open(path string) (*Register, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Register, error) {
	r := &Register{byKey: make(map[string]*Host)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the host presence register", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for _, h := range file.Hosts {
			// A JSON array containing `null` is syntactically valid and
			// unmarshals into a nil *Host -- skipped here so a malformed
			// document cannot crash startup by indexing through a nil
			// pointer, same defensive load as internal/coverage's.
			if h == nil || h.Key == "" {
				continue
			}
			if h.Mark != nil && !h.Mark.Kind.Valid() {
				// A kind this build does not recognise is dropped rather
				// than kept: an unknown mark would render as neither
				// intended nor dismissed and could never be cleared.
				h.Mark = nil
			}
			r.byKey[h.Key] = h
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

// Observe records that an event arrived on iface from ip at time at,
// creating the host if this is the first time it has been seen and
// refreshing it otherwise. label is the hostname resolved for that
// address on that event, empty when nothing supplies one.
//
// Reports whether anything was recorded: false means the pair is not a
// host by Registers' rule, or the register was full of marked entries.
//
// A dismissed host reappearing clears its mark -- the host is back, so
// it is not dismissed, and the operator's earlier "remove this" was
// about a host that had gone quiet, not a standing instruction to hide
// it forever. An intended mark is left exactly where it is, for the
// opposite reason: it says the quiet was on purpose, which is still
// true after a burst of traffic and would be irritating to have to say
// again.
//
// Called once per ingested event, on the single ingest goroutine (see
// main.go's ingestOneRecovered). One mutex-protected map update and a
// dirty flag flip. No JSON encode and no disk write ever happen on this
// path -- see persistDirtyLocked and runPersistLoop.
func (r *Register) Observe(iface, ip, label string, at time.Time) bool {
	// Nil-receiver safe, the same convention engine.Engine.Enqueue and
	// persist.WriteBehind follow: a caller with no register configured
	// (a test harness, a CLI mode that never serves the API) should not
	// have to nil-check on the ingest path.
	if r == nil || !Registers(iface, ip) {
		return false
	}

	key := KeyFor(iface, ip)

	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.byKey[key]; ok {
		h.Events++
		if at.After(h.LastSeen) {
			h.LastSeen = at
		}
		if label != "" {
			h.Label = label
		}
		if h.Mark != nil && h.Mark.Kind == MarkDismissed {
			h.Mark = nil
		}
		r.persistDirtyLocked()
		return true
	}

	if len(r.byKey) >= MaxHosts && !r.evictLocked() {
		r.shed++
		if r.shed == 1 || r.shed%1000 == 0 {
			persistLog.Warn(fmt.Sprintf(
				"the host presence register is full at %d hosts and every entry carries a mark, so %d newly-seen host(s) have been dropped -- "+
					"far more distinct private source addresses than a real network produces, which is itself worth investigating",
				MaxHosts, r.shed))
		}
		return false
	}

	r.byKey[key] = &Host{
		Key:       key,
		Iface:     iface,
		IP:        ip,
		Label:     label,
		FirstSeen: at,
		LastSeen:  at,
		Events:    1,
	}
	r.persistDirtyLocked()
	return true
}

// evictLocked drops the entry with the oldest LastSeen that carries no
// mark, making room for one new host. Reports whether it found one: a
// register in which every entry is marked has nothing evictable, and
// the caller sheds the new observation instead of discarding an
// operator's decision. Must be called with r.mu held.
func (r *Register) evictLocked() bool {
	var oldest *Host
	for _, h := range r.byKey {
		if h.Mark != nil {
			continue
		}
		if oldest == nil || h.LastSeen.Before(oldest.LastSeen) {
			oldest = h
		}
	}
	if oldest == nil {
		return false
	}
	delete(r.byKey, oldest.Key)
	return true
}

// Mark records an operator's decision about the host at key. kind must
// be MarkIntended or MarkDismissed. reason is required for an intended
// mark -- the reason *is* the mark, the thing that stays said so the
// question is not asked again -- and optional for a dismissal, which
// says "take this off my map" and needs no justification to be
// actionable.
//
// by is the actor, taken from the session by the caller, never from a
// request body. Returns ErrUnknownHost if no event has ever registered
// that key.
func (r *Register) Mark(key string, kind MarkKind, reason, by string) (Host, error) {
	if !kind.Valid() {
		return Host{}, ErrInvalidMark
	}
	if err := validateText(key, maxKeyLength); err != nil {
		return Host{}, err
	}
	if reason != "" || kind == MarkIntended {
		if err := validateText(reason, maxReasonLength); err != nil {
			return Host{}, err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	h, ok := r.byKey[key]
	if !ok {
		return Host{}, ErrUnknownHost
	}
	h.Mark = &Mark{Kind: kind, Reason: reason, By: by, At: time.Now()}
	r.persistDirtyLocked()

	out := *h
	mark := *h.Mark
	out.Mark = &mark
	return out, nil
}

// Unmark removes the mark on the host at key, putting it back to
// whatever its own LastSeen says it is. Reports whether a mark was
// actually there to remove -- an unknown key and an unmarked host both
// answer false, which the API turns into a 404 the same way
// handleCoverageDelete does: the caller looked this key up from the
// list, so nothing to remove is a meaningful signal rather than routine
// noise.
func (r *Register) Unmark(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	h, ok := r.byKey[key]
	if !ok || h.Mark == nil {
		return false
	}
	h.Mark = nil
	r.persistDirtyLocked()
	return true
}

// Get returns a copy of the host at key.
func (r *Register) Get(key string) (Host, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	h, ok := r.byKey[key]
	if !ok {
		return Host{}, false
	}
	return copyHost(h), true
}

// List returns every known host, sorted by Key for a stable,
// deterministic order across calls.
func (r *Register) List() []Host {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked()
}

func (r *Register) listLocked() []Host {
	out := make([]Host, 0, len(r.byKey))
	for _, h := range r.byKey {
		out = append(out, copyHost(h))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// copyHost deep-copies the one pointer field, so a caller holding a
// listed Host cannot reach back into the register through its Mark.
func copyHost(h *Host) Host {
	out := *h
	if h.Mark != nil {
		mark := *h.Mark
		out.Mark = &mark
	}
	return out
}

// persistMinInterval rate-limits the actual backend write inside
// persist.WriteBehind -- unrelated to how often this register itself
// encodes (see persistFlushInterval). A var rather than a const so a
// test needing every write to reach the backend immediately can shrink
// it, the same convention internal/flags.persistMinInterval uses.
var persistMinInterval = time.Second

// persistEncodeHookForTest, when set, runs inside persistIfDirty after
// r.dirty is cleared and before the encoded bytes reach wb -- the window
// a Flush must not slip through. Tests only; nil otherwise.
var persistEncodeHookForTest func()

// persistFlushInterval is the minimum spacing runPersistLoop leaves
// between one encode finishing and the next starting, stamped after an
// encode completes rather than before it starts -- the same "stamped
// after" reasoning persist.WriteBehind's own MinInterval documents (its
// fix for #377), applied one layer up. A var rather than a const so a
// test can shrink it rather than wait out two real seconds, the same
// "var so tests can shrink it" convention persistMinInterval already
// uses.
//
// #1087: this, not a rate limit on the caller's own goroutine, is what
// keeps Observe off the JSON encode entirely. Every mutating method
// used to marshal up to MaxHosts entries itself -- on the ingest
// goroutine, under r.mu -- at least once per persistMinInterval, and
// immediately for a structural change. Now every mutating method only
// ever flips r.dirty and pings a wake channel, both O(1) under a lock
// it already holds; the encode -- the actual cost -- happens on
// runPersistLoop's own goroutine, never on the path an event arrives
// on.
var persistFlushInterval = 2 * time.Second

// persistDirtyLocked records that the in-memory state has changed since
// the last encode and wakes runPersistLoop -- a non-blocking, buffered
// send, so a burst of calls while the loop is already awake (or busy
// encoding) costs nothing beyond the flag write. It never marshals and
// never touches wb directly -- see runPersistLoop and persistIfDirty for
// where that work actually happens, off this goroutine. Must be called
// with r.mu held.
func (r *Register) persistDirtyLocked() {
	if r.wb == nil {
		return
	}
	r.dirty = true
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// runPersistLoop is this register's one background goroutine -- started
// by OpenWithBackend when a backend is configured, stopped by Close --
// that owns every encode. Idle (nothing dirty), it blocks on wake so it
// costs nothing between changes. Once woken, it encodes right away if
// this is the first dirty change in a while (the same "no wait, since
// the last-attempt stamp starts zero" first-call behaviour
// persist.WriteBehind.run documents), then leaves at least
// persistFlushInterval before the next encode, however many further
// mutations land in between -- exactly the coalescing MarkDirty already
// gives the backend write, one layer up for the encode itself. Nothing
// else ever calls persistIfDirty concurrently with this loop except
// Close, which joins the loop first.
func (r *Register) runPersistLoop() {
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
			if wait := persistFlushInterval - time.Since(lastEncode); wait > 0 {
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
// retries rather than silently giving up on the change.
func (r *Register) persistIfDirty() {
	r.encodeMu.Lock()
	defer r.encodeMu.Unlock()
	r.mu.Lock()
	if !r.dirty || r.wb == nil {
		r.mu.Unlock()
		return
	}
	r.dirty = false
	list := r.listLocked()
	r.mu.Unlock()

	if persistEncodeHookForTest != nil {
		persistEncodeHookForTest()
	}
	ptrs := make([]*Host, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	data, err := json.MarshalIndent(storeFile{Hosts: ptrs}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the host presence register failed: %v -- this change exists only in memory and will be lost on restart", err))
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
		return
	}
	r.wb.MarkDirty(data)
}

// Flush forces whatever is currently dirty to the backend now, without
// waiting out the debounce interval, and blocks until that attempt
// finishes or ctx expires. For a caller that genuinely needs to know a
// change has reached the backend before proceeding (a test, or a
// `-backup` racing a change made moments earlier). Not meant for
// routine use: persistence off the hot path is the whole point. A
// register with no backend configured is a safe no-op.
func (r *Register) Flush(ctx context.Context) error {
	r.persistIfDirty()
	return r.wb.Flush(ctx)
}

// Close stops runPersistLoop and the write-behind writer, flushing
// whatever is still dirty before returning -- main's shutdown joins on
// this (closeStoreOnShutdown) so a host seen right before exit is not
// silently dropped. A register with no backend configured is a safe
// no-op. Not safe to call any mutating method after Close.
func (r *Register) Close(ctx context.Context) error {
	if r.wb != nil {
		close(r.stopPersist)
		<-r.persistDone
	}
	r.persistIfDirty()
	return r.wb.Close(ctx)
}

// validateText rejects an empty string, text over maxLen runes, invalid
// UTF-8, or control/Unicode-format characters (the bidi overrides that
// let a string render in an order other than the one it is stored in).
// Same rule and same reasoning as internal/coverage.validateText: Key
// and Reason both flow into the UI and the audit trail.
func validateText(s string, maxLen int) error {
	if s == "" {
		return ErrInvalidMark
	}
	if !utf8.ValidString(s) {
		return ErrInvalidMark
	}
	if utf8.RuneCountInString(s) > maxLen {
		return ErrInvalidMark
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrInvalidMark
		}
	}
	return nil
}
