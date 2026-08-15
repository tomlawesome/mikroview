// SPDX-License-Identifier: AGPL-3.0-only

// Package netclass attributes an IP address to a network class -- Tor
// exit, commercial VPN, cloud/datacenter, privacy relay -- from a small
// vetted menu of feeds, refreshed on a timer and matched locally.
//
// It is display-first, by deliberate design (issue #114). The research
// on that issue measured the "raise a flag on a datacenter hit" version
// against the live feeds and found it would fire on more than one in ten
// routable IPv4 addresses -- Google DNS, Akamai edge, and every Apple
// Private Relay user among them. So this package classifies and labels;
// it does not score. "Absence of evidence is not evidence" cuts both
// ways here: a non-match is not a clean bill of health, and this package
// never returns one -- it returns "no classification", which is not the
// same thing.
//
// Nothing in here imports internal/flags or internal/detect, and a test
// enforces that: a network-class match must not be able to reach the
// suspicion machinery except through a caller that has explicitly, and
// narrowly, decided to let it (direction-aware, per-category, elsewhere).
package netclass

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gaissmai/bart"
)

// Class is the result of classifying one address. Zero value (Matched
// false) means "no classification available for this IP", which is
// distinct from "this IP is clean".
type Class struct {
	Matched  bool
	Category Category
	Source   Source
	// Label is the feed's human name ("AWS"); Detail is the finer label
	// when the feed carried one (a region, "eu-west-1"). Both come from a
	// third-party file, so both are validated on the way in -- see
	// sanitiseDetail.
	Label  string
	Detail string
}

// String renders a Class for display: "AWS (eu-west-1)" or "Tor exit".
func (c Class) String() string {
	if !c.Matched {
		return ""
	}
	if c.Detail != "" {
		return fmt.Sprintf("%s (%s)", c.Label, c.Detail)
	}
	return c.Label
}

// entry is what the bart table stores per prefix -- an index into the
// classifier's interned entry slice rather than the strings themselves,
// so 120k prefixes sharing a few dozen distinct (source, detail) pairs
// cost a few dozen strings, not 120k.
type classEntry struct {
	source Source
	detail string
}

// RefreshInterval is fixed, not user-configurable, for the same
// over-polling reason internal/blocklist documents. main.go drives the
// ticker; the per-install jitter is applied there.
const RefreshInterval = 24 * time.Hour

// Classifier holds the current tables and serves Lookup. Safe for
// concurrent use: Lookup takes a read lock, Refresh swaps a fully-built
// table in under the write lock, so a lookup never sees a half-populated
// table.
type Classifier struct {
	mu      sync.RWMutex
	table   *bart.Table[uint32]
	entries []classEntry
	sources map[Source]feedDef
	order   []Source
	// priorPrefixes retains the last parsed prefix set per source, so a
	// source that fails a refresh keeps serving its previous data rather
	// than dropping to empty. Guarded by mu.
	priorPrefixes map[Source][]classifiedPrefix
	// coverage is the address count each source contributed at its last
	// successful refresh, for the coverage-delta guard.
	coverage  map[Source]uint64
	fetchedAt map[Source]time.Time
	etag      map[Source]string
	client    *fetchClient
	log       *slog.Logger
}

// New builds a Classifier for the given enabled sources. Unknown names
// are logged and skipped -- the same "malformed config degrades, does
// not crash" contract as the rest of the codebase. No fetch happens
// here; Lookup misses until the first Refresh.
func New(sourceNames []string, log *slog.Logger) *Classifier {
	c := &Classifier{
		table:         &bart.Table[uint32]{},
		sources:       make(map[Source]feedDef),
		priorPrefixes: make(map[Source][]classifiedPrefix),
		coverage:      make(map[Source]uint64),
		fetchedAt:     make(map[Source]time.Time),
		etag:          make(map[Source]string),
		client:        newFetchClient(),
		log:           log,
	}
	for _, name := range sourceNames {
		src := Source(name)
		def, ok := registryBySource[src]
		if !ok {
			log.Warn(fmt.Sprintf("unknown netclass source %q ignored (see docs/configuration.md for the supported menu)", name))
			continue
		}
		c.sources[src] = def
	}
	// Fixed registry order, so the combined budget is allocated
	// deterministically rather than by map iteration order.
	for _, f := range feedRegistry {
		if _, ok := c.sources[f.Source]; ok {
			c.order = append(c.order, f.Source)
		}
	}
	return c
}

// HasSources reports whether any recognised source is enabled -- main.go
// uses it to decide whether to start the refresh ticker at all.
func (c *Classifier) HasSources() bool {
	return len(c.sources) > 0
}

// Ready reports whether at least one source has been fetched. The UI uses
// it to show a "attribution data still downloading" state on first run
// rather than looking broken before the first refresh completes.
func (c *Classifier) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.fetchedAt) > 0
}

// Lookup classifies ipStr, returning the most specific match across all
// enabled sources. bart does longest-prefix-match, which is exactly what
// we want where feeds overlap: an AWS /24 inside X4B's /16 datacenter
// range attributes to AWS, the more specific and more useful answer.
//
// A parse failure or a miss returns a zero Class (Matched false) -- never
// an error, and never a "clean" verdict.
func (c *Classifier) Lookup(ipStr string) Class {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return Class{}
	}
	// IPv4-mapped IPv6 must be unmapped or every v4 lookup silently
	// misses -- the same bug internal/blocklist guards against.
	addr = addr.Unmap()

	c.mu.RLock()
	defer c.mu.RUnlock()
	idx, ok := c.table.Lookup(addr)
	if !ok {
		return Class{}
	}
	e := c.entries[idx]
	def := c.sources[e.source]
	return Class{
		Matched:  true,
		Category: def.Category,
		Source:   e.source,
		Label:    def.Label,
		Detail:   e.detail,
	}
}

// Refresh fetches every enabled source and rebuilds the table. Each
// source is independent: a fetch or parse failure, or a rejected
// oversized delta, leaves the previously-loaded data for that source in
// place -- fail to last-known-good, never to empty.
//
// The table is rebuilt whole and swapped atomically rather than mutated
// in place, so a concurrent Lookup either sees the entire previous table
// or the entire new one.
func (c *Classifier) Refresh(ctx context.Context) {
	// Start from a snapshot of what each source last contributed, so a
	// source that fails this pass keeps its prior prefixes.
	c.mu.RLock()
	prior := make(map[Source][]classifiedPrefix, len(c.order))
	for src, ps := range c.priorPrefixes {
		prior[src] = ps
	}
	priorCoverage := make(map[Source]uint64, len(c.coverage))
	for k, v := range c.coverage {
		priorCoverage[k] = v
	}
	c.mu.RUnlock()

	current := make(map[Source][]classifiedPrefix, len(c.order))
	newCoverage := make(map[Source]uint64, len(c.order))
	newFetchedAt := make(map[Source]time.Time, len(c.order))

	for _, src := range c.order {
		def := c.sources[src]
		body, notModified, err := c.client.fetch(ctx, c, src, def.URL)
		if err != nil {
			c.log.Warn(fmt.Sprintf("%s: refresh failed (%v) -- keeping the last good data", def.Label, err))
			current[src] = prior[src]
			newCoverage[src] = priorCoverage[src]
			continue
		}
		if notModified {
			current[src] = prior[src]
			newCoverage[src] = priorCoverage[src]
			c.markFetched(src, &newFetchedAt)
			continue
		}

		parsed := def.Parse(body)
		cov := coverageOf(parsed)

		// A 200 that yields nothing is not a legitimate empty feed --
		// these sources exist because they are never empty. It is a
		// truncated response, a provider outage answering with a blank
		// body, or an interception, and accepting it does two things:
		// the source silently stops classifying anything, and its
		// recorded coverage drops to zero, which disarms the 2x
		// poisoning guard below for the *next* refresh (2x of zero is
		// zero, so anything passes). Two refreshes and the guard is
		// gone.
		//
		// The doc comment above already promises "fail to last-known-
		// good, never to empty"; that only covered fetch and parse
		// errors, and a clean 200 is neither. Same treatment
		// Blocklist.Refresh already gives a feed that fetches cleanly
		// and yields nothing. See #285.
		if len(parsed) == 0 && len(prior[src]) > 0 {
			c.log.Warn(fmt.Sprintf("%s: refreshed feed fetched cleanly but parsed to zero prefixes (had %d) -- keeping the last good data",
				def.Label, len(prior[src])))
			current[src] = prior[src]
			newCoverage[src] = priorCoverage[src]
			continue
		}

		// Coverage-delta guard: a feed that suddenly claims far more
		// address space than last time is more likely poisoned than
		// legitimately grown. Reject the new copy and keep the old one.
		// The threshold is relative to the previous fetch, not absolute,
		// because absolute bands (as X4B's own build uses) leave room to
		// add tens of millions of addresses without tripping.
		// Both operands are capped at maxCoverage (1<<62), so prev*2
		// cannot overflow -- it did before #324, wrapping a saturated
		// prior to a tiny number and rejecting every subsequent refresh
		// of an IPv6-heavy feed forever. Saturated vs saturated
		// compares equal and is therefore accepted, which is the honest
		// answer: both are "unbounded", not "grew".
		if prev := priorCoverage[src]; prev > 0 && cov > prev*2 {
			c.log.Warn(fmt.Sprintf("%s: refreshed feed covers %s vs %s before (more than double) -- rejecting as a possible poisoned list, keeping the last good data",
				def.Label, describeCoverage(cov), describeCoverage(prev)))
			current[src] = prior[src]
			newCoverage[src] = prev
			continue
		}

		current[src] = parsed
		newCoverage[src] = cov
		c.markFetched(src, &newFetchedAt)
		c.log.Info(fmt.Sprintf("%s: refreshed, %d prefixes (%s)", def.Label, len(parsed), describeCoverage(cov)))
	}

	table, entries := buildTable(c.order, current)

	c.mu.Lock()
	c.table = table
	c.entries = entries
	c.priorPrefixes = current
	c.coverage = newCoverage
	for src, t := range newFetchedAt {
		c.fetchedAt[src] = t
	}
	c.mu.Unlock()
}

func (c *Classifier) markFetched(src Source, into *map[Source]time.Time) {
	(*into)[src] = time.Now()
}

// buildTable interns the (source, detail) pairs and inserts every prefix,
// in source-priority order so that on an exact-prefix collision the
// higher-priority source wins. bart's Lookup already resolves overlaps by
// specificity; this only decides ties.
//
// table.Get is checked before every Insert specifically because
// bart.Table.Insert is last-write-wins on an exact prefix ("if the
// prefix already exists, its value is updated" -- its own doc comment),
// not first-write-wins. Iterating in priority order alone does not give
// higher-priority-wins: it gives whichever source happens to be iterated
// last for that exact prefix, whatever order collides to be. Caught by
// TestApplePrivateRelayWinsOverX4BVPNOnExactCollision, which failed
// against the naive unconditional-Insert version of this function --
// SourceX4BVPN (lower priority, iterated second) was silently
// overwriting SourceApplePrivateRelay's entry despite this function's
// own doc comment already claiming the opposite.
func buildTable(order []Source, bySrc map[Source][]classifiedPrefix) (*bart.Table[uint32], []classEntry) {
	table := &bart.Table[uint32]{}
	var entries []classEntry
	intern := make(map[classEntry]uint32)

	idFor := func(e classEntry) uint32 {
		if id, ok := intern[e]; ok {
			return id
		}
		id := uint32(len(entries))
		entries = append(entries, e)
		intern[e] = id
		return id
	}

	for _, src := range order {
		for _, cp := range bySrc[src] {
			if _, exists := table.Get(cp.prefix); exists {
				continue
			}
			id := idFor(classEntry{source: src, detail: sanitiseDetail(cp.detail)})
			table.Insert(cp.prefix, id)
		}
	}
	return table, entries
}

// maxCoverage is "more address space than any number means to a
// person". Both a single prefix's count and the running total saturate
// here, and the poisoning guard compares saturated totals as equal --
// so it is deliberately far enough below MaxUint64 that doubling it
// (which the guard does) cannot overflow.
const maxCoverage = uint64(1) << 62

// coverageOf sums the address counts of a prefix set, saturating at
// maxCoverage.
//
// Capping each prefix individually is not enough: four prefixes wider
// than /64 sum to 1<<64 and wrap the total to a small number (#324).
// Apple Private Relay carries two today, so this is one upstream change
// away rather than hypothetical -- and the number this returns is what
// the poisoning guard in Refresh reads, so a wrapped total does not
// merely misreport, it decides whether a feed is believed.
func coverageOf(ps []classifiedPrefix) uint64 {
	var total uint64
	for _, cp := range ps {
		bits := cp.prefix.Addr().BitLen() // 32 or 128
		hostBits := bits - cp.prefix.Bits()
		n := maxCoverage
		if hostBits < 62 {
			n = uint64(1) << hostBits
		}
		if total >= maxCoverage-n {
			return maxCoverage
		}
		total += n
	}
	return total
}

// describeCoverage renders a coverage figure for a person rather than
// for arithmetic. "9223372036854882389 addresses" is not a fact anyone
// can act on -- it reads as a bug even when it is not, which is how
// #324 was found.
func describeCoverage(v uint64) string {
	if v >= maxCoverage {
		return "more address space than is worth counting (a prefix wider than /64)"
	}
	return withThousands(v) + " addresses"
}

// withThousands groups digits so a large count can be read at a glance.
func withThousands(v uint64) string {
	s := strconv.FormatUint(v, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// sanitiseDetail validates a third-party detail string (an AWS region, a
// GCP scope) before it can reach the browser. A provider file is
// untrusted input; a value that needs escaping is a corrupt entry, so it
// is dropped rather than sanitised. Svelte auto-escapes and the codebase
// uses no {@html}, so there is no live XSS path -- this is defence
// against a future regression plus plain garbage-in protection.
func sanitiseDetail(s string) string {
	if len(s) == 0 || len(s) > 64 {
		if len(s) > 64 {
			return ""
		}
		return s
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' || r == ' '
		if !ok {
			return ""
		}
	}
	return s
}

// EnabledSources returns the enabled sources in priority order, for the
// config-report and tests.
func (c *Classifier) EnabledSources() []Source {
	out := make([]Source, len(c.order))
	copy(out, c.order)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
