// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"sync"
	"sync/atomic"
	"time"
)

// Issue #1234: a router whose mikroview logging block was pasted more
// than once carries several identical `mikroview` logging rules, so
// the same event arrives two or three times over -- inflating every
// downstream count (syslog volume, event rows, rule usage, coverage,
// baselines) without anything downstream being able to tell. This file
// detects that pattern from the raw lines as they arrive, before
// they're handed to the ingest channel (see emit in tcp_listener.go).
//
// The rule (decided on the issue, not invented here): per source, keep
// a fixed-size ring of recent raw-line hashes with their arrival
// times. A duplicate sighting is the same hash arriving again from the
// same source within a very short window -- long enough to catch a
// router's own dispatch of the same event to two logging actions at
// once, short enough that ordinary repeated activity (a retried
// connection, a repeated port-scan hit) never qualifies. Drift is
// reported only once a source has racked up enough sightings that it
// looks sustained rather than coincidental.

const (
	// dupRingSize is how many of a source's most recent raw lines are
	// remembered as hashes. 64 is comfortably more than one RouterOS
	// device can log inside dupSightingWindow even under a burst, so a
	// genuine duplicate is never pushed out of the ring before it can
	// be matched, while staying small enough that the fixed array costs
	// a little over 1KiB per source (64 * 16 bytes) -- cheap to keep
	// per source, allocated once when a source is first seen and never
	// resized.
	dupRingSize = 64

	// dupSightingWindow is how close together two identical lines from
	// the same source must land to count as the same event dispatched
	// twice, rather than the router legitimately logging the same
	// thing again on its own. A duplicated logging rule re-emits a
	// byte-identical line -- timestamp included -- within the same
	// dispatch, well under 50ms; ordinary repeated activity is never
	// this tight.
	dupSightingWindow = 50 * time.Millisecond

	// dupSightingsToReportDrift is how many duplicate sightings must
	// accumulate, from one source, inside the existing ingest-loss
	// window (lossWindowOversized -- see tcp_listener.go: the same
	// 60-second dwell already used for "ordinary defensive behaviour
	// working as intended, not itself an emergency once it stops
	// recurring", which is exactly the character of a duplicated
	// logging rule) before it's worth a Settings line, rather than a
	// couple of coincidental repeats.
	dupSightingsToReportDrift = 20

	// dupMultiplicityBuckets caps the distinct multiplicities tracked
	// per source for the "most common multiplicity" calculation.
	// Multiplicity starts at 2 (bucket 0), so this covers 2 through 8
	// copies individually; anything higher is folded into the last
	// bucket, which only trades precision on an already-extreme case
	// (a router with eight or more copies of the block pasted in) for
	// keeping the histogram a fixed, allocation-free array.
	dupMultiplicityBuckets = 7

	// dupTrackedSourcesCap bounds how many distinct source hosts get
	// their own ring: unlike tcpOversizedHost's single overwritten
	// slot ("there is nothing here for an attacker to grow"), a
	// duplicate ring must persist per source across reconnects, so it
	// can't reuse that trick. A source arriving once this many are
	// already tracked is simply not tracked (existing sources are
	// unaffected) -- 256 matches maxTCPConnections' own default
	// ceiling on concurrent sources, which this cannot exceed by more
	// than the churn of sources connecting and disconnecting over the
	// process lifetime.
	dupTrackedSourcesCap = 256
)

// dupRingEntry is one remembered raw line: its hash and when it
// arrived.
type dupRingEntry struct {
	hash uint64
	at   time.Time
}

// sourceDupState is one source's duplicate-detection state: the ring
// of recent line hashes, plus the current episode of duplicate
// sightings and a histogram of their multiplicities. One mutex guards
// all of it, since a line's ring scan, ring write, and (if it's a
// sighting) episode update must happen as a single step.
type sourceDupState struct {
	mu   sync.Mutex
	ring [dupRingSize]dupRingEntry
	next int

	sightings      uint64
	lastSightingAt time.Time // zero means no sighting yet in this episode
	multiplicity   [dupMultiplicityBuckets]uint64

	// clusterOpen, clusterHash, clusterBucket and clusterAt track the
	// duplicate cluster the most recent multiplicity-histogram entry
	// belongs to -- the same dispatched event, seen one copy further
	// along (see noteDuplicateLine's doc comment). Recognising a
	// continuation moves that entry's weight to the new, higher bucket
	// in place, rather than counting the event a second time at its
	// old, lower one.
	clusterOpen   bool
	clusterHash   uint64
	clusterBucket int
	clusterAt     time.Time

	// lastLineAt is when this source last sent anything at all, not
	// just a duplicate. dupStateFor reads it to decide which entry to
	// drop when the cap is full: without it, the first 256 addresses to
	// arrive would hold the map for the process's lifetime and a real
	// router connecting later -- after a restart, or behind them in a
	// burst -- would never be tracked at all.
	lastLineAt time.Time
}

var (
	dupSourcesMu sync.Mutex
	dupSources   = make(map[string]*sourceDupState)
)

// dupStateFor returns host's duplicate-detection state, creating it if
// host hasn't been seen before and the cap allows it. Returns nil past
// dupTrackedSourcesCap for a source not already tracked -- see its
// doc comment.
func dupStateFor(host string, now time.Time) *sourceDupState {
	dupSourcesMu.Lock()
	defer dupSourcesMu.Unlock()

	if st, ok := dupSources[host]; ok {
		return st
	}
	if len(dupSources) >= dupTrackedSourcesCap {
		// Full. Drop the source that has been quiet longest and take
		// its place, rather than refusing the newcomer: a cap that
		// only ever refuses is a cap the first 256 addresses to arrive
		// can hold forever, which would let a burst of one-line
		// senders lock the operator's own router out of detection for
		// as long as the process runs. Evicting loses that source's
		// episode, which is the right thing to lose -- it has not been
		// heard from since.
		stalest, stalestAt := "", time.Time{}
		for h, st := range dupSources {
			st.mu.Lock()
			at := st.lastLineAt
			st.mu.Unlock()
			if stalest == "" || at.Before(stalestAt) {
				stalest, stalestAt = h, at
			}
		}
		delete(dupSources, stalest)
	}
	st := &sourceDupState{lastLineAt: now}
	dupSources[host] = st
	return st
}

// fnv1aHash64 is FNV-1a over data. Hand-rolled rather than
// hash/fnv.New64a() (internal/persist/file.go's choice for the same
// algorithm): New64a returns a hash.Hash64 interface value, and a
// Write/Sum64 pair through that interface is not reliably stack-only,
// which matters here and didn't there -- this runs once per arriving
// raw line, on the hot ingest path noteDuplicateLine documents below.
func fnv1aHash64(data []byte) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range data {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// noteDuplicateLine is called once per raw line, from emit in
// tcp_listener.go, before the line is handed to the ingest channel.
// It must stay cheap and allocation-free: a fixed-size ring scan and
// write, one map lookup (only a new source ever inserts), no per-line
// allocation.
func noteDuplicateLine(host string, data []byte) {
	// Declared sources only, the same gate oversizedIsSetupDrift applies
	// to #1205's sibling warning. The TLS listener asks for no client
	// certificate, so anyone who can reach the syslog port can send the
	// same line twice twenty times over and otherwise have Settings tell
	// the operator that *their* address has duplicate mikroview logging
	// rules -- advice about a router mikroview has never been told
	// about, which would also displace a true warning about one it has.
	if !isConfiguredSource(host) {
		return
	}

	now := lossNow()
	st := dupStateFor(host, now)
	if st == nil {
		return
	}

	hash := fnv1aHash64(data)

	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastLineAt = now

	multiplicity := 1
	for _, e := range st.ring {
		if e.hash == hash && !e.at.IsZero() && now.Sub(e.at) <= dupSightingWindow {
			multiplicity++
		}
	}
	st.ring[st.next] = dupRingEntry{hash: hash, at: now}
	st.next = (st.next + 1) % dupRingSize

	if multiplicity < 2 {
		return
	}

	tcpDuplicateSightingsTotal.Add(1)

	if st.lastSightingAt.IsZero() || now.Sub(st.lastSightingAt) > lossWindowOversized {
		st.sightings = 0
		st.multiplicity = [dupMultiplicityBuckets]uint64{}
		st.clusterOpen = false
	}
	st.sightings++
	st.lastSightingAt = now

	bucket := multiplicity - 2
	if bucket >= dupMultiplicityBuckets {
		bucket = dupMultiplicityBuckets - 1
	}
	// A router pasting its logging block N times dispatches the same
	// event N times, so this line's own multiplicity climbs by one on
	// each successive copy of it: 2 on the second copy, 3 on the third,
	// and so on. Left alone, that scored one histogram sighting at each
	// intermediate count -- 2, 3, ..., N -- spreading an N-times-pasted
	// event's weight evenly across N-1 buckets, and since ties favor
	// the smaller multiplicity (modalMultiplicityLocked), the modal
	// count reported for it could never rise past 2, however many
	// copies were actually pasted (v0.6.0 pre-release audit: "a router
	// pasted three times reports 2, ... the multiplicity histogram is
	// dead"). A line recognised as the same cluster the previous entry
	// already counted -- same hash, still inside the window -- moves
	// that entry's weight to the new bucket instead of adding a second,
	// independent one, so an N-times-pasted event ends up contributing
	// exactly one histogram entry, at N.
	sameCluster := st.clusterOpen && st.clusterHash == hash && now.Sub(st.clusterAt) <= dupSightingWindow
	switch {
	case sameCluster && bucket != st.clusterBucket:
		st.multiplicity[st.clusterBucket]--
		st.multiplicity[bucket]++
	case !sameCluster:
		st.multiplicity[bucket]++
	}
	st.clusterOpen = true
	st.clusterHash = hash
	st.clusterBucket = bucket
	st.clusterAt = now

	if st.sightings < dupSightingsToReportDrift {
		return
	}
	modal, _ := st.modalMultiplicityLocked()
	if modal < 2 {
		return
	}
	noteDuplicateDrift(host, modal, st.sightings, now)
}

// modalMultiplicityLocked returns the most common multiplicity seen in
// the current episode (the histogram bucket with the highest count)
// and that count. Ties favor the smaller multiplicity: an "apparent
// copy count" of 2 is never wrong when copies of 3 are equally common,
// only conservative. Caller must hold st.mu.
func (st *sourceDupState) modalMultiplicityLocked() (modal int, count uint64) {
	best := -1
	var bestCount uint64
	for i, c := range st.multiplicity {
		if c > bestCount {
			bestCount = c
			best = i
		}
	}
	if best < 0 {
		return 0, 0
	}
	return best + 2, bestCount
}

// tcpDuplicateSightingsTotal is the all-time count of duplicate
// sightings across every source -- ListenerStats.DuplicateSightings,
// paired with the windowed view below the same way Dropped/Oversized
// pair their own atomic total with a Loss.* entry.
var tcpDuplicateSightingsTotal atomic.Uint64

// tcpDuplicateHost, tcpDuplicateCopyCount and tcpDuplicateSightingsRecent
// cache the most recently qualifying source -- the same "just the
// latest value" approach as tcpOversizedHost, and for the same reason:
// only one drifting source is ever shown at a time, so overwriting in
// place is O(1) regardless of how many sources or how often. The
// per-source state above (dupSources) is what actually judges each
// source; this is only the reported snapshot.
var (
	tcpDuplicateHostMu       sync.Mutex
	tcpDuplicateHost         string
	tcpDuplicateCopyCount    uint64
	tcpDuplicateSightingsRec uint64
	tcpDuplicateHostLastAt   time.Time
)

// noteDuplicateDrift records host as the current drifting source: at
// least dupSightingsToReportDrift duplicate sightings inside the
// window, with copyCount as the modal multiplicity among them.
func noteDuplicateDrift(host string, copyCount int, sightings uint64, now time.Time) {
	tcpDuplicateHostMu.Lock()
	tcpDuplicateHost = host
	tcpDuplicateCopyCount = uint64(copyCount)
	tcpDuplicateSightingsRec = sightings
	tcpDuplicateHostLastAt = now
	tcpDuplicateHostMu.Unlock()
}

// duplicateLossCounterStats builds LossStats.Duplicate, judged against
// now -- see lossCounterStats, which this mirrors by hand since the
// underlying state isn't a plain lossFreshness (it needs Host and
// CopyCount alongside Recent/LastAt/Active).
func duplicateLossCounterStats(now time.Time) LossCounterStats {
	tcpDuplicateHostMu.Lock()
	host := tcpDuplicateHost
	copyCount := tcpDuplicateCopyCount
	sightings := tcpDuplicateSightingsRec
	lastAt := tcpDuplicateHostLastAt
	tcpDuplicateHostMu.Unlock()

	if host == "" || lastAt.IsZero() {
		return LossCounterStats{}
	}

	stats := LossCounterStats{
		Recent: sightings,
		Active: now.Sub(lastAt) <= lossWindowOversized,
	}
	s := lastAt.UTC().Format(time.RFC3339)
	stats.LastAt = &s
	if stats.Active {
		stats.Host = host
		stats.Declared = isConfiguredSource(host)
		stats.CopyCount = copyCount
	}
	return stats
}

// clearDuplicateState resets every source's ring and episode plus the
// reported single-slot cache -- part of ClearLoss (issue #1015's
// "Clear all", extended by #1234).
func clearDuplicateState() {
	dupSourcesMu.Lock()
	dupSources = make(map[string]*sourceDupState)
	dupSourcesMu.Unlock()

	tcpDuplicateHostMu.Lock()
	tcpDuplicateHost = ""
	tcpDuplicateCopyCount = 0
	tcpDuplicateSightingsRec = 0
	tcpDuplicateHostLastAt = time.Time{}
	tcpDuplicateHostMu.Unlock()
}
