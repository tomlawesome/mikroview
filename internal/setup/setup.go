// SPDX-License-Identifier: AGPL-3.0-only

// Package setup records what mikroview has observed of a router's
// setup, so the guided wizard (issue #320) can tell an operator that a
// step worked instead of asking them to go and look.
//
// The whole point is that mikroview never connects to a router: it
// cannot poll one to check. But every step of the setup ends with the
// router arriving *here* -- fetching the CA, opening the syslog
// connection, sending a first event, pushing its tables -- so each one
// is observable from this side without dialling anything.
//
// Most of that is already recorded elsewhere and is read from there
// rather than duplicated: internal/device.Registry knows when a source
// first sent an event and how many it has sent, and
// internal/routerstate knows when each pushed table last arrived. This
// package holds only what nothing else does -- the CA fetch, the syslog
// connection itself (which happens before any event, and is what
// separates "the router cannot reach me" from "no rule is logging
// yet"), and how many events carried a decoded action, which is how the
// log-prefix step is checked.
package setup

import (
	"sync"
	"time"
)

// maxSources bounds every map here. The keys are source addresses, so
// they are chosen by whoever connects -- the syslog port takes no
// credentials, and /ca.crt is deliberately public. Same attacker-keyed
// growth internal/device.Registry and internal/rules bound, and bounded
// the same way: evict the least-recently-seen, never the whole map, so
// a flood cannot erase the record of the router the operator is
// actually setting up in one go.
const maxSources = 256

// Store is safe for concurrent use. Every Note* method is called from a
// hot path (the ingest goroutine, the accept loop, an HTTP handler), so
// they take the write lock only briefly and never allocate on the
// common path.
type Store struct {
	mu         sync.RWMutex
	caFetched  map[string]time.Time
	syslogSeen map[string]syslogObservation
	prefixes   map[string]prefixHealth
}

type syslogObservation struct {
	First time.Time
	Last  time.Time
}

// prefixHealth counts how many events from a device carried an action
// mikroview could decode from the rule's log-prefix. A router whose
// rules log without the prefix convention shows up as events arriving
// with none decoded -- which looks identical to "working" on every
// other measure, and is the single most common half-finished setup.
type prefixHealth struct {
	Events  uint64
	Decoded uint64
	Last    time.Time
}

func New() *Store {
	return &Store{
		caFetched:  make(map[string]time.Time),
		syslogSeen: make(map[string]syslogObservation),
		prefixes:   make(map[string]prefixHealth),
	}
}

// NoteCAFetch records that source downloaded /ca.crt. This is step 1 of
// the setup landing: the router has reached mikroview over the network
// and taken its certificate authority.
func (s *Store) NoteCAFetch(source string, now time.Time) {
	if source == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	evictOldest(s.caFetched, maxSources, func(t time.Time) time.Time { return t })
	s.caFetched[source] = now
}

// NoteSyslogConnection records that source opened a syslog TLS
// connection. Distinct from an event arriving: a connected router with
// no logging rules produces this and nothing else, which is exactly the
// state the wizard needs to name.
func (s *Store) NoteSyslogConnection(source string, now time.Time) {
	if source == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.syslogSeen[source]; ok {
		prev.Last = now
		s.syslogSeen[source] = prev
		return
	}
	evictOldest(s.syslogSeen, maxSources, func(o syslogObservation) time.Time { return o.Last })
	s.syslogSeen[source] = syslogObservation{First: now, Last: now}
}

// NoteEvent records one ingested event and whether its action was
// decoded from a log-prefix. Keyed by device id, not source address, so
// it lines up with what the rest of the UI calls a device.
func (s *Store) NoteEvent(device string, actionDecoded bool, now time.Time) {
	if device == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.prefixes[device]
	if !ok {
		evictOldest(s.prefixes, maxSources, func(p prefixHealth) time.Time { return p.Last })
	}
	h.Events++
	if actionDecoded {
		h.Decoded++
	}
	h.Last = now
	s.prefixes[device] = h
}

// SourceObservation is what has been seen from one source address.
type SourceObservation struct {
	Source            string     `json:"source"`
	CAFetchedAt       *time.Time `json:"caFetchedAt,omitempty"`
	SyslogFirstSeenAt *time.Time `json:"syslogFirstSeenAt,omitempty"`
	SyslogLastSeenAt  *time.Time `json:"syslogLastSeenAt,omitempty"`
}

// DeviceObservation is the log-prefix health of one device.
type DeviceObservation struct {
	Device string `json:"device"`
	Events uint64 `json:"events"`
	// Decoded is how many of those carried an action decoded from a
	// log-prefix. Decoded == 0 with Events > 0 means the rules log but
	// do not follow the <A|D|R|L>|slug| convention.
	Decoded uint64 `json:"decoded"`
}

// Snapshot returns everything observed, for the wizard's status API.
func (s *Store) Snapshot() ([]SourceObservation, []DeviceObservation) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bySource := make(map[string]*SourceObservation)
	at := func(src string) *SourceObservation {
		o, ok := bySource[src]
		if !ok {
			o = &SourceObservation{Source: src}
			bySource[src] = o
		}
		return o
	}
	for src, t := range s.caFetched {
		when := t
		at(src).CAFetchedAt = &when
	}
	for src, obs := range s.syslogSeen {
		first, last := obs.First, obs.Last
		o := at(src)
		o.SyslogFirstSeenAt = &first
		o.SyslogLastSeenAt = &last
	}

	sources := make([]SourceObservation, 0, len(bySource))
	for _, o := range bySource {
		sources = append(sources, *o)
	}
	devices := make([]DeviceObservation, 0, len(s.prefixes))
	for d, h := range s.prefixes {
		devices = append(devices, DeviceObservation{Device: d, Events: h.Events, Decoded: h.Decoded})
	}
	return sources, devices
}

// evictOldest drops the least-recently-seen entry when m is at cap, so
// adding one more keeps the map bounded. One at a time rather than a
// batch: these maps are small and this runs at most once per new source.
func evictOldest[V any](m map[string]V, cap int, stamp func(V) time.Time) {
	if len(m) < cap {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, v := range m {
		t := stamp(v)
		if oldestKey == "" || t.Before(oldest) {
			oldestKey, oldest = k, t
		}
	}
	delete(m, oldestKey)
}
