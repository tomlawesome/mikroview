// SPDX-License-Identifier: AGPL-3.0-only

// Package matchlog persists watchlist matches (#243) -- the discrete,
// rare, high-value events a control-port/watchlist entry raises,
// distinct from internal/store's high-volume, volatile event ring and
// from internal/flags' aggregate, capped-evidence judgements. A match
// is a fact that matters individually, so it is kept at full fidelity
// and survives a restart, which neither of those two stores does.
//
// Two backends: FileStore (file.go), capacity-bounded, refusing new
// records once full; and PostgresStore (postgres.go), a dedicated
// indexed table rather than a row in the shared blob table, bounded by
// age (retention) rather than count instead -- see
// docs/decisions/postgres-backend.md §1a for why a genuinely indexed
// table here doesn't reopen that decision.
package matchlog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// Identity is a matched event's device identity: MAC-preferred, IP as a
// fallback, never neither (#243 section 1). A MAC-bound identity
// survives its device's IP changing under DHCP; an IP-bound one does
// not -- an accepted limitation, not a silent one (see MatchesSource).
type Identity struct {
	MAC string `json:"mac,omitempty"`
	IP  string `json:"ip,omitempty"`
}

// Empty reports whether neither field is set -- constructing an entry or
// a query from an event with no MAC and no usable source IP, which
// Append and Query both refuse.
func (id Identity) Empty() bool { return id.MAC == "" && id.IP == "" }

// identityKey is the MAC-preferred key both collapsing (Tuple.key) and
// matching (MatchesSource) resolve an identity to: the MAC alone when
// known, the IP alone otherwise. Deliberately the *only* place this
// preference is expressed -- Append and Query used to implement it
// separately, and drifted: Append's key embedded the raw (MAC, IP) pair,
// so a device's IP changing under DHCP silently stopped collapsing even
// though querying by MAC still worked (caught by
// TestMatchingPrefersMACOverIP). Requires a non-empty identity --
// callers check Empty() first (Append/Query both refuse an empty one via
// ErrEmptyIdentity), so this never has to decide what an empty identity
// should match.
//
// The MAC is lowercased before comparison because the two sources a MAC
// reaches mikroview from disagree on case, and comparing them byte for
// byte silently loses matches. A real RouterOS 7.23.3 emits it
// uppercase, in both the syslog line and the pushed ARP table:
//
//	firewall,info A|live-in| input: ... src-mac 52:55:0A:00:02:02, ...
//	{"address":"10.0.2.2","mac-address":"52:55:0A:00:02:02", ...}
//
// while the conventional written form -- what an operator types into the
// watchlist entry's own free-text MAC field, and what every example in
// this repository and its docs uses -- is lowercase. Byte-exact, an
// entry typed as 52:55:0a:00:02:02 never fires against that router's
// traffic, and a match query typed the same way returns nothing even
// when matches exist. Both failures are silent: no error, no empty-state
// hint, just an entry that looks configured and does nothing.
//
// Reproduced end to end against a booted CHR (#273): the same entry
// created twice, once each case, only the uppercase one recorded a
// match. internal/device's MAC registry already lowercases its key for
// exactly this reason (normalizeMAC there) -- this is that same rule
// reaching the other store that keys on a MAC, not a new idea.
//
// The key is normalised, never the stored Identity: a Record keeps the
// router's own casing, since it is verbatim evidence.
func (id Identity) identityKey() string {
	if id.MAC != "" {
		return "mac:" + strings.ToLower(id.MAC)
	}
	return "ip:" + id.IP
}

// Key exports identityKey for a caller that needs to *index* by identity
// rather than compare two of them pairwise -- internal/engine's inverted
// expectation dispatch, which buckets definitions by the device they
// scope so an event consults only the ones that could be about it,
// instead of asking every one of them in turn (#406).
//
// Exported rather than reimplemented at that call site precisely because
// of what identityKey's own doc comment records: the MAC-preferred rule
// and its lowercasing were once written twice, drifted, and silently
// stopped collapsing a device's matches when its lease changed. A second
// copy for indexing would be the same mistake in a new place -- two
// identities that MatchesSource each other must always land in the same
// bucket, which is only guaranteed if the bucket key is literally this
// function.
func (id Identity) Key() string { return id.identityKey() }

// MatchesSource reports whether a candidate event identity should be
// treated as the same source as this stored identity, following the
// same MAC-preferred, IP-fallback rule Tuple.key uses to decide whether a
// match collapses (#243 section 1): if this identity has a MAC, the
// candidate must share it -- its IP is irrelevant, precisely so a
// MAC-bound entry survives a lease change. Only when this identity has
// no MAC does the candidate's IP get compared instead.
func (id Identity) MatchesSource(candidate Identity) bool {
	return id.identityKey() == candidate.identityKey()
}

// Tuple is what a watchlist entry actually matches on. Two matches with
// an identical Tuple for the same entry collapse into one Record rather
// than being stored individually (#243 section 4) -- repetition beyond
// the first carries no information besides "N of these," and collapsing
// is what stops a badly-tuned entry from recreating the haystack this
// feature exists to avoid.
type Tuple struct {
	Source Identity `json:"source"`
	DestIP string   `json:"destIp"`
	Port   int      `json:"port"`
}

// key is the string identity collapsing groups on: same entry, same
// device identity, same destination, same port. Two Tuples that differ
// only in which of Source.MAC/Source.IP happens to be set are NOT the
// same key -- that would let a device's match history split or merge
// depending on which chain a particular line arrived on, which is a
// correctness property worth keeping simple rather than papering over
// with a fuzzier notion of "same device."
func (t Tuple) key(entryID string) string {
	return entryID + "\x00" + t.Source.identityKey() + "\x00" + t.DestIP + "\x00" + strconv.Itoa(t.Port)
}

// Record is one watchlist match, evidence-first: it embeds the full
// matched event so an operator investigating has everything the live
// view would have shown, not a summary reconstructed later from a
// smaller record. FirstSeen/LastSeen/Count implement collapsing --
// Count > 1 means every occurrence after the first was identical on
// Tuple, and only its timestamps and count were updated in the log, not
// a new Record written.
type Record struct {
	ID        string      `json:"id"`
	EntryID   string      `json:"entryId"`
	Tuple     Tuple       `json:"tuple"`
	Event     store.Event `json:"event"`
	FirstSeen time.Time   `json:"firstSeen"`
	LastSeen  time.Time   `json:"lastSeen"`
	Count     uint64      `json:"count"`
	// Provisional marks a match recorded while its expectation
	// definition's baseline had not yet cleared its history floor --
	// internal/engine.Baseline's warm-up gating
	// (docs/decisions/evaluation-engine.md section 1, #368's fix made a
	// chassis contract), the matchlog counterpart to flags.Flag.Provisional.
	// false (the default, omitted from JSON) for every record recorded
	// today: nothing wires the watchlist onto internal/engine.Baseline
	// yet -- that is #406's job. Fixed at creation, like FirstSeen and
	// Tuple: a collapsed repeat (see Append/AppendProvisional) never
	// changes it.
	Provisional bool `json:"provisional,omitempty"`
}

// Query selects matches by source identity and a time window. Since is
// inclusive, Until is exclusive -- the same convention internal/store's
// Query uses. A record is selected if its window
// [FirstSeen, LastSeen] overlaps [Since, Until), not merely if FirstSeen
// falls inside it: a long-collapsed record (first seen before Since,
// still recurring after) is exactly the kind of ongoing activity a
// caller asking "what happened in this window" needs to see.
type Query struct {
	Source Identity
	Since  time.Time
	Until  time.Time
	// Limit caps how many records Query delivers, most recent first (by
	// LastSeen). Zero means DefaultLimit.
	Limit int
}

// RecentQuery asks the other question a match log gets asked: not "what
// has this device done", but "what has broken recently" -- the most
// recent matches across *every* watchlist entry, newest (by LastSeen)
// first (#586, the query the Matches tab of #584 is built on). Since
// and Until are optional and mean exactly what Query's do.
//
// Deliberately its own type, and its own Store method, rather than a
// Query whose Source is allowed to be empty. Two reasons, either
// sufficient alone:
//
// An empty identity has no safe meaning in Query. ErrEmptyIdentity
// exists precisely because "neither MAC nor IP" is a caller who does
// not know the device, and reading that as "every device" turns a bug
// -- an event whose source could not be resolved, a query parameter
// that failed to arrive -- into a silent full-log read. That failure
// mode is unchanged: Query still refuses an empty Source, and every
// caller that means "all entries" has to say so in a type that cannot
// be reached by accident.
//
// And the bound is the whole safety property, so it belongs in the
// type. Expectations are capped at 10,000
// (internal/engine/definitions_expectations.go), so an all-entries read
// with no ceiling is an arbitrarily large response on a route a
// read-only API token reaches with no rate limiter in front of it. Limit
// is therefore clamped exactly as Query's is (0 -> DefaultLimit, above
// MaxLimit -> MaxLimit): bounded by construction, not by the caller
// remembering to bound it.
//
// It also reaches evidence nothing else can. A non-inverted entry may
// have an empty Source -- "any source" (internal/watchlist.Entry) -- and
// its matches are recorded normally under whatever identity the event
// carried, but no per-identity query finds them unless the operator
// already guesses that identity. This mode is how they become visible.
type RecentQuery struct {
	Since time.Time
	Until time.Time
	// Limit caps how many records Recent delivers, most recent first
	// (by LastSeen). Zero means DefaultLimit; see the type's own doc
	// comment for why this is not optional in spirit.
	Limit int
}

// DefaultLimit/MaxLimit mirror internal/store's own clamp-not-trust
// contract for a caller-supplied limit (see internal/store/limit_test.go)
// -- an untrusted or programmatically-generated Limit must not be able to
// force an unbounded allocation.
const (
	DefaultLimit = 100
	MaxLimit     = 5000
)

func clampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

// Stats reports a store's current size, for the "this ceiling is real
// and here is where you are against it" visibility #243 asks for --
// the same reasoning issue #244 gave for surfacing store.maxEvents'
// count/capacity rather than leaving an operator to find out by hitting
// it. Capacity is 0 for a backend with no ceiling (PostgresStore --
// bounded by age instead, see its own doc comment).
type Stats struct {
	Count    int  `json:"count"`
	Capacity int  `json:"capacity"`
	Full     bool `json:"full"`
}

// ErrCapacityReached is returned by Append for a genuinely new Tuple
// once a capacity-bounded backend is full. The file backend refuses to
// grow past its limit rather than degrading quietly (#243 section 3) --
// unlike internal/store's ring, which silently overwrites, silently
// discarding a rare, high-value match is exactly the failure this store
// exists to avoid, so the caller finds out instead.
//
// A repeat of an *already-open* tuple still succeeds even at capacity:
// collapsing an existing record costs no new capacity, and an entry's
// ongoing activity should not stop being tracked just because the log
// is full elsewhere.
var ErrCapacityReached = errors.New("matchlog: store is at capacity")

// ErrEmptyIdentity is returned by Append/Query for a Tuple/Query whose
// Source has neither MAC nor IP set -- there is nothing to match on, and
// storing or querying such a record would silently mean "matches
// everything" or "matches nothing" depending on the bug, not "I don't
// know this device."
var ErrEmptyIdentity = errors.New("matchlog: identity has neither MAC nor IP")

// Store persists watchlist matches. See FileStore and PostgresStore for
// the two implementations.
type Store interface {
	// Append records a match for (entryID, tuple) at t, evidence being
	// event. Collapses into an existing open record for the same
	// (entryID, tuple) per Tuple.key's rule, or starts a new one.
	Append(entryID string, tuple Tuple, event store.Event, t time.Time) error

	// AppendProvisional is Append, but marks a newly-created record's
	// Provisional field -- see that field's doc comment. Added by #399
	// alongside the field itself; no caller uses it yet (#406 wires the
	// watchlist side onto internal/engine.Baseline's warm-up gating) --
	// it exists now so the persisted shape, on both backends, and the
	// round trip are proven ahead of anything depending on them.
	// provisional is ignored when Append collapses into an already-open
	// record: Provisional is fixed at creation, like FirstSeen.
	AppendProvisional(entryID string, tuple Tuple, event store.Event, t time.Time, provisional bool) error

	// Query streams matches for q's source within [q.Since, q.Until),
	// most recent (by LastSeen) first, up to q.Limit, calling yield for
	// each. yield returning false stops delivery early. Query still
	// examines the whole log to fold collapsed records correctly --
	// linear in log size to answer, per #243 section 3's own accepted
	// cost model -- but never holds more in memory than the matching
	// result set, not the whole log.
	// ctx cancels the query. It matters most on the Postgres backend,
	// where the work happens server-side and a client that has gone
	// away would otherwise leave it running to completion; the file
	// backend checks it between records. GET /api/matches is
	// reachable with a read-only API token and has no rate limiter, so
	// "the caller left" needs to actually stop the work.
	Query(ctx context.Context, q Query, yield func(Record) bool) error

	// Recent streams the most recent matches across every entry within
	// [q.Since, q.Until), newest (by LastSeen) first, up to q.Limit,
	// calling yield for each. yield returning false stops delivery
	// early. Unlike Query it takes no identity and must not need the
	// identity-key index -- see RecentQuery for why that is a separate
	// entry point rather than Query with an empty Source, and why the
	// limit is not optional in spirit.
	//
	// ctx cancels the query, for the same reason Query's does: this is
	// reachable with a read-only API token and has no rate limiter, so
	// "the caller left" has to actually stop the work -- more so here,
	// where the work is not narrowed by an identity first.
	Recent(ctx context.Context, q RecentQuery, yield func(Record) bool) error

	Stats() Stats
	Close() error
}

// newID returns a random 32-character hex string. Unlike
// internal/auth's newID, this only needs to be unique within one store,
// not unguessable -- nothing here is a bearer credential -- but the same
// crypto/rand source is reused rather than inventing a second generator
// for no reason.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("matchlog: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
