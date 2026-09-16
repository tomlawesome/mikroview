// SPDX-License-Identifier: AGPL-3.0-only

// Package seen is the register of values this instance has actually
// observed for the two filter fields that have no option list anywhere
// else in the app -- protocol, and interface (issue #1226).
//
// # Why it exists
//
// Every other filter control in the stream is a picker over something
// known: actions are a fixed vocabulary, rules come from the rule store,
// hosts and ports come from internal/entities. `proto` and `interface`
// had nothing behind them, so the strip offered a free-text box and the
// operator typed `tcp` and hoped. The two options that were put to the
// owner -- a hardcoded list of well-known protocols, or a list derived
// from whatever happens to be on screen -- were both rejected: the first
// is a guess about somebody else's network, the second is thrown away on
// reload and lies after a quiet hour. The answer (owner, 2026-09-13) is
// that mikroview "should grow a real list of what it has seen over time,
// and be persisted".
//
// So this records nothing the feed did not already show and asks the
// network nothing -- mikroview observes, it never probes (see
// AGENTS.md). A value is here only because an event carrying it arrived.
//
// # What a field is
//
// Two fields, matching what internal/store's Query can actually filter
// on rather than what an Event happens to carry:
//
//   - FieldProto is Event.Protocol. Query.Protocol matches it with
//     EqualFold, so values are folded to lower case on the way in --
//     "TCP" and "tcp" are one menu entry because they are one filter.
//   - FieldInterface is Event.InInterface and Event.OutInterface
//     together, in one list. That is deliberate and follows the filter:
//     Query.Interface matches an event whose *in or out* interface is the
//     value, so there is exactly one interface filter and it deserves
//     exactly one list. Splitting it into two menus would offer a
//     distinction the filter cannot express.
//
// # Retention
//
// Decided on #1226 (2026-09-15) and implemented in MaxAge and MaxValues:
// a value is kept while it has been seen in the last 90 days, and each
// field holds at most 200 values, the least recently seen evicted first.
// A value that has arrived at all in three months is one the operator
// may still want to filter on; one that has not is noise in a menu. The
// cap is the part that matters for safety: interface names and protocol
// names both arrive in a syslog line, so both are attacker-influenced,
// and an unbounded map keyed by them is a memory-growth vector reachable
// by anybody able to make the router log.
//
// Both figures are constants, not configuration: nobody has asked to
// tune them, and a documented knob costs more than the knob is worth.
//
// Expiry is applied at write time -- when a value is recorded, and when
// the register is read or encoded -- never on a timer. The register
// therefore runs no scheduler of its own, and an instance that stops
// ingesting stops expiring, which is the honest behaviour: nothing was
// observed, so nothing changed.
//
// # Persistence
//
// Optional JSON through internal/persist, on the same "empty path means
// in-memory only" contract as every other optional store here. Updated
// on every ingested event, so it follows internal/hosts: the ingest
// goroutine only ever takes a mutex, updates a map and flips a bool.
// Every JSON encode and every disk write happens on this package's own
// background goroutine -- see persistDirtyLocked and runPersistLoop.
package seen

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("seen")

// MaxAge is how long a value stays in the register after the last time
// it was seen. See the package comment for the decision.
const MaxAge = 90 * 24 * time.Hour

// MaxValues is the hard ceiling on how many values one field holds. At
// the cap the least recently seen is evicted to make room.
const MaxValues = 200

// maxValueLength bounds one recorded value. A protocol name is a handful
// of characters and a RouterOS interface name is a short identifier, so
// this is generous next to anything real and tight enough that a forged
// syslog field cannot put a wall of text into a menu.
const maxValueLength = 64

// lastSeenGranularity is how far LastSeen must move before a repeat
// observation counts as a change worth persisting.
//
// Without it, a busy feed marks the register dirty on every single
// event -- protocol is present on nearly all of them -- and the write
// behind writer then re-encodes and re-writes the same few hundred
// entries once a second forever, to record that `tcp` was last seen a
// second more recently than it already said. Against a 90-day retention
// that difference is worth nothing. A new value, or the cap or expiry
// moving an entry, always persists immediately regardless.
const lastSeenGranularity = time.Minute

// Field is one of the two filter fields this register tracks. The string
// values are the API's field names and the on-disk keys, so they are
// contract: see docs/configuration.md.
type Field string

const (
	// FieldProto is Event.Protocol -- "tcp", "udp", "icmp".
	FieldProto Field = "proto"
	// FieldInterface is Event.InInterface and Event.OutInterface pooled
	// into one list, because Query.Interface matches either.
	FieldInterface Field = "interface"
)

// Fields is every field this register tracks, in the order the API
// reports them. Iterating this rather than ranging a map keeps the
// served document stable between calls.
var Fields = []Field{FieldProto, FieldInterface}

// Valid reports whether f is a field this register knows.
func (f Field) Valid() bool { return f == FieldProto || f == FieldInterface }

// Value is one observed value and the window it has been seen across.
type Value struct {
	Value     string    `json:"value"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// storeFile is the on-disk shape: an object keyed by field name,
// mirroring internal/hosts' storeFile. An object rather than a bare map
// of arrays so a later field can be added beside this one without
// changing the document's top-level type.
type storeFile struct {
	Fields map[Field][]*Value `json:"fields"`
}

// Register holds every value the feed has shown for each tracked field.
// The zero value is not usable; construct with Open.
type Register struct {
	mu sync.RWMutex
	// wb is nil when persistence is not configured, the same "nil means
	// off" convention internal/hosts.Register and internal/flags.Store
	// follow. Every method on it is a safe no-op on a nil receiver.
	wb     *persist.WriteBehind
	byName map[Field]map[string]*Value

	// dirty, wake, stopPersist, persistDone and encodeMu are
	// internal/hosts' write-behind machinery, for the same reason it has
	// it (#1087): this is written on the ingest path, so the ingest
	// goroutine must never marshal. It flips dirty under a lock it
	// already holds; runPersistLoop owns every encode.
	dirty       bool
	wake        chan struct{}
	stopPersist chan struct{}
	persistDone chan struct{}
	encodeMu    sync.Mutex

	// shed counts observations dropped because the value itself was
	// unusable -- too long, or carrying control characters. Worth being
	// able to say how many rather than only that it happened, same as
	// internal/hosts.Register.shed.
	shed uint64
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Register that persists to it from
// then on. An empty path is the expected "persistence not configured"
// case: a fully usable, in-memory-only Register is returned. A document
// that exists but cannot be read or parsed is a hard error, the same
// fail-closed contract as internal/hosts.Open -- a live backend must
// never silently overwrite a document it could not read (#378).
func Open(path string) (*Register, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured.
func OpenWithBackend(b persist.Backend) (*Register, error) {
	r := &Register{byName: make(map[Field]map[string]*Value, len(Fields))}
	for _, f := range Fields {
		r.byName[f] = make(map[string]*Value)
	}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the seen-values register", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return err
		}
		for field, values := range file.Fields {
			// A field this build does not know is dropped rather than
			// kept: it could never be served or filtered on, and keeping
			// it would let a stale document hold slots against the cap.
			if !field.Valid() {
				continue
			}
			for _, v := range values {
				// A JSON array containing `null` is syntactically valid
				// and unmarshals into a nil *Value -- skipped here so a
				// malformed document cannot crash startup, the same
				// defensive load internal/hosts does.
				if v == nil {
					continue
				}
				name, ok := normalise(field, v.Value)
				if !ok {
					continue
				}
				if v.LastSeen.IsZero() {
					continue
				}
				if v.FirstSeen.IsZero() || v.FirstSeen.After(v.LastSeen) {
					v.FirstSeen = v.LastSeen
				}
				v.Value = name
				r.byName[field][name] = v
			}
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

// Observe records the values one ingested event carried: its protocol,
// and its inbound and outbound interface names. Any of them may be
// empty, which is the ordinary case for an event that does not carry
// that field -- an empty value is not a value and is skipped, never
// recorded as one.
//
// Reports whether anything changed that is worth persisting.
//
// Called once per ingested event, on the single ingest goroutine (see
// main.go's ingestOneRecovered). One mutex, up to three map lookups, and
// a dirty flag flip. No JSON encode and no disk write ever happen on
// this path -- see persistDirtyLocked and runPersistLoop.
func (r *Register) Observe(proto, inIface, outIface string, at time.Time) bool {
	// Nil-receiver safe, the same convention hosts.Register.Observe and
	// persist.WriteBehind follow: a caller with no register configured
	// (a test harness, a CLI mode that never serves the API) should not
	// have to nil-check on the ingest path.
	if r == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	changed := r.recordLocked(FieldProto, proto, at)
	if r.recordLocked(FieldInterface, inIface, at) {
		changed = true
	}
	// An event whose in and out interface are the same name records it
	// once: the second call finds the entry it just wrote and reports no
	// further change.
	if r.recordLocked(FieldInterface, outIface, at) {
		changed = true
	}

	if changed {
		r.persistDirtyLocked()
	}
	return changed
}

// recordLocked notes one value against one field, applying retention on
// the way in. Reports whether the register changed by enough to be worth
// an encode -- see lastSeenGranularity. Must be called with r.mu held.
func (r *Register) recordLocked(f Field, raw string, at time.Time) bool {
	name, ok := normalise(f, raw)
	if !ok {
		if strings.TrimSpace(raw) != "" {
			r.shed++
			if r.shed == 1 || r.shed%1000 == 0 {
				persistLog.Warn(fmt.Sprintf(
					"%d observed %s value(s) have been ignored for being over %d characters or carrying control characters -- "+
						"a real protocol or interface name is neither, so this is a malformed or forged log line",
					r.shed, f, maxValueLength))
			}
		}
		return false
	}
	values := r.byName[f]
	if values == nil {
		return false
	}

	if v, exists := values[name]; exists {
		// FirstSeen can move backwards: history replayed after a restore
		// can carry an event older than anything this process has seen,
		// and the register's claim is "first seen", not "first recorded".
		if at.Before(v.FirstSeen) {
			v.FirstSeen = at
		}
		if !at.After(v.LastSeen) {
			return false
		}
		moved := at.Sub(v.LastSeen) >= lastSeenGranularity
		v.LastSeen = at
		return moved
	}

	// A value arriving for the first time is the only moment the
	// register grows, so it is the moment retention is applied: drop
	// what has aged out, then evict the least recently seen if the cap
	// is still reached. Doing it here and not on a timer is what keeps
	// this package free of a scheduler (see the package comment).
	r.expireLocked(f, at)
	for len(values) >= MaxValues {
		if !r.evictOldestLocked(f) {
			break
		}
	}
	values[name] = &Value{Value: name, FirstSeen: at, LastSeen: at}
	return true
}

// expireLocked drops every value for f last seen more than MaxAge before
// now. Must be called with r.mu held.
func (r *Register) expireLocked(f Field, now time.Time) {
	cutoff := now.Add(-MaxAge)
	for name, v := range r.byName[f] {
		if v.LastSeen.Before(cutoff) {
			delete(r.byName[f], name)
		}
	}
}

// evictOldestLocked drops f's least recently seen value, making room for
// one new one. Reports whether it found anything to drop -- false only
// when the field is already empty, which the cap makes unreachable in
// practice. Must be called with r.mu held.
func (r *Register) evictOldestLocked(f Field) bool {
	var oldest *Value
	for _, v := range r.byName[f] {
		if oldest == nil || v.LastSeen.Before(oldest.LastSeen) {
			oldest = v
		}
	}
	if oldest == nil {
		return false
	}
	delete(r.byName[f], oldest.Value)
	return true
}

// List returns the live values for one field, most recently seen first,
// with anything that has aged out left off. It does not mutate the
// register: a value past MaxAge is not reported and will be deleted the
// next time a new value for that field arrives, so what a caller sees
// and what an expiring register holds never disagree.
//
// An unknown field returns nil rather than an error: the API serves a
// fixed set of fields, so there is no caller able to ask for another
// one.
func (r *Register) List(f Field) []Value {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listLocked(f, time.Now())
}

// All returns every field's live values, keyed by field -- what the API
// serves in one call, so the frontend fetches both menus at once.
func (r *Register) All() map[Field][]Value {
	out := make(map[Field][]Value, len(Fields))
	if r == nil {
		for _, f := range Fields {
			out[f] = []Value{}
		}
		return out
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now()
	for _, f := range Fields {
		values := r.listLocked(f, now)
		if values == nil {
			values = []Value{}
		}
		out[f] = values
	}
	return out
}

// listLocked is List's body against a caller-supplied clock, so the
// encoder and the reader agree on what has expired. Must be called with
// r.mu held (read or write).
func (r *Register) listLocked(f Field, now time.Time) []Value {
	values := r.byName[f]
	if len(values) == 0 {
		return nil
	}
	cutoff := now.Add(-MaxAge)
	out := make([]Value, 0, len(values))
	for _, v := range values {
		if v.LastSeen.Before(cutoff) {
			continue
		}
		out = append(out, *v)
	}
	// Most recently seen first: the menu's top is what the network is
	// doing now. Ties break on the value itself so the order is total
	// and a document does not shuffle between encodes.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// Shed reports how many observations have been ignored for carrying an
// unusable value over this process's lifetime.
func (r *Register) Shed() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.shed
}

// normalise turns a raw field off an event into the value this register
// stores, reporting false for anything that is not one.
//
// Protocol is folded to lower case because Query.Protocol matches with
// EqualFold: "TCP" and "tcp" select identically, so offering both in a
// menu would be offering the same filter twice. An interface name is
// kept verbatim, because Query.Interface matches it exactly and folding
// it would produce a menu entry that selects nothing.
func normalise(f Field, raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", false
	}
	if f == FieldProto {
		v = strings.ToLower(v)
	}
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > maxValueLength {
		return "", false
	}
	for _, r := range v {
		// Control and Unicode-format characters (the bidi overrides that
		// let a string render in an order other than the one it is
		// stored in) are refused for the same reason
		// internal/hosts.validateText refuses them: this value flows
		// into the UI.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", false
		}
	}
	return v, true
}

// persistMinInterval rate-limits the actual backend write inside
// persist.WriteBehind -- unrelated to how often this register itself
// encodes (see persistFlushInterval). A var rather than a const so a
// test needing every write to reach the backend immediately can shrink
// it, the same convention internal/hosts.persistMinInterval uses.
var persistMinInterval = time.Second

// persistFlushInterval is the minimum spacing runPersistLoop leaves
// between one encode finishing and the next starting. Same shape, same
// reasoning and same "var so tests can shrink it" convention as
// internal/hosts.persistFlushInterval.
var persistFlushInterval = 2 * time.Second

// persistDirtyLocked records that the in-memory state has changed since
// the last encode and wakes runPersistLoop -- a non-blocking, buffered
// send, so a burst of calls while the loop is already awake costs
// nothing beyond the flag write. It never marshals and never touches wb
// directly. Must be called with r.mu held.
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
// that owns every encode. A faithful copy of internal/hosts'
// runPersistLoop, including why it is shaped this way: idle it blocks on
// wake and costs nothing, and once woken it leaves at least
// persistFlushInterval between encodes however many mutations land in
// between.
func (r *Register) runPersistLoop() {
	defer close(r.persistDone)
	var lastEncode time.Time
	for {
		r.mu.RLock()
		dirty := r.dirty
		r.mu.RUnlock()

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
// takes the snapshot, so a mutation landing after this unlocks is never
// lost -- it simply sets r.dirty again for the next call. A marshal
// failure puts the flag back so the next call retries rather than
// silently giving up on the change.
//
// The snapshot is listLocked's, which leaves out anything past MaxAge --
// so the document on disk sheds expired values on the next write after
// they age out, without a sweep of its own.
func (r *Register) persistIfDirty() {
	r.encodeMu.Lock()
	defer r.encodeMu.Unlock()
	r.mu.Lock()
	if !r.dirty || r.wb == nil {
		r.mu.Unlock()
		return
	}
	r.dirty = false
	now := time.Now()
	file := storeFile{Fields: make(map[Field][]*Value, len(Fields))}
	for _, f := range Fields {
		list := r.listLocked(f, now)
		ptrs := make([]*Value, len(list))
		for i := range list {
			ptrs[i] = &list[i]
		}
		file.Fields[f] = ptrs
	}
	r.mu.Unlock()

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding the seen-values register failed: %v -- this change exists only in memory and will be lost on restart", err))
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
// `-backup` racing a change made moments earlier). A register with no
// backend configured is a safe no-op.
func (r *Register) Flush(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.persistIfDirty()
	return r.wb.Flush(ctx)
}

// Close stops runPersistLoop and the write-behind writer, flushing
// whatever is still dirty before returning -- main's shutdown joins on
// this (closeStoreOnShutdown) so a value seen right before exit is not
// silently dropped. A register with no backend configured is a safe
// no-op. Not safe to call any mutating method after Close.
func (r *Register) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.wb != nil {
		close(r.stopPersist)
		<-r.persistDone
	}
	r.persistIfDirty()
	return r.wb.Close(ctx)
}
