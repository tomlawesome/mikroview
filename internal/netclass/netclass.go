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

	"go4.org/netipx"
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

// detailRange is one source's contribution to its detail index: the
// address range a single feed entry covers, plus an index into the
// classifier's interned detail-string slice rather than the string
// itself -- so 120k Apple Private Relay prefixes sharing a few hundred
// distinct city names cost a few hundred strings, not 120k.
//
// Sorted per source by from, and looked up the same way netipx.IPSet
// itself resolves Contains: binary search for the last range starting
// at or before the address, then check it actually reaches that far.
// That is only correct when a single source's own ranges do not
// overlap -- true of every feed on the menu (each is a flat list of
// disjoint blocks from one publisher) but not enforced, so a future feed
// whose ranges nest could resolve to the wrong neighbour's detail. It
// cannot resolve to the wrong *source* or *category*: membership is
// decided by the per-source netipx.IPSet below, which handles overlap
// correctly by construction. Only the cosmetic detail string is at risk.
type detailRange struct {
	from, to netip.Addr
	detail   uint32
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
	mu sync.RWMutex
	// sets holds one netipx.IPSet per source -- pure membership, "does
	// this address belong to this source's data". Lookup checks them in
	// ClassOrder (see sources.go), not c.order, because c.order is fetch
	// priority and ClassOrder is match precedence; they answer different
	// questions and happen to differ (VPN is fetched after Private Relay,
	// for the exact-collision reason at SourceApplePrivateRelay's
	// registry entry, but is checked before it here).
	sets map[Source]*netipx.IPSet
	// details holds each source's detail index, present only for a
	// source that carries per-prefix detail (AWS regions, GCP scopes,
	// Apple cities) -- Tor and the X4BNet lists never do, so they have no
	// entry here at all.
	details       map[Source][]detailRange
	detailStrings []string
	sources       map[Source]feedDef
	order         []Source
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
		sets:          make(map[Source]*netipx.IPSet),
		details:       make(map[Source][]detailRange),
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

// Lookup classifies ipStr against every enabled source's set, in
// ClassOrder (see sources.go): the first source whose set contains the
// address wins, whichever source's prefix for that address happens to
// be wider or narrower. netipx.IPSet has no longest-prefix match --
// there is no trie to ask "which of these is more specific" -- so a
// fixed class order stands in for it. This is the one behaviour change
// from the bart-backed version: a Tor exit inside an AWS /16 now reads
// as Tor because Tor is checked first, not because its listing happens
// to be narrower.
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

	for _, src := range ClassOrder {
		set, ok := c.sets[src]
		if !ok || !set.Contains(addr) {
			continue
		}
		def := c.sources[src]
		return Class{
			Matched:  true,
			Category: def.Category,
			Source:   src,
			Label:    def.Label,
			Detail:   c.lookupDetail(src, addr),
		}
	}
	return Class{}
}

// lookupDetail resolves the finer per-prefix label (an AWS region, a
// GCP scope, an Apple city) for a source already known to contain addr.
// Returns "" for a source with no detail index at all -- Tor and the
// X4BNet lists, which never carry one.
func (c *Classifier) lookupDetail(src Source, addr netip.Addr) string {
	ranges := c.details[src]
	if len(ranges) == 0 {
		return ""
	}
	i := sort.Search(len(ranges), func(i int) bool { return addr.Less(ranges[i].from) })
	if i == 0 {
		return ""
	}
	r := ranges[i-1]
	if addr.Compare(r.to) > 0 {
		return ""
	}
	return c.detailStrings[r.detail]
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

	sets, details, detailStrings := buildSets(current)

	c.mu.Lock()
	c.sets = sets
	c.details = details
	c.detailStrings = detailStrings
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

// buildSets turns each source's parsed prefixes into a netipx.IPSet for
// membership, plus a sorted detail index for the sources that carry one.
// There is no cross-source precedence to resolve here any more -- each
// set only ever answers for its own source, and ClassOrder (sources.go)
// is what decides which source's answer Lookup reports when more than
// one contains the address. That replaces buildTable's exact-prefix
// precedence (table.Get before every Insert, source order deciding the
// tie): one set per source makes an exact collision between two sources
// resolve the same way ClassOrder already resolves a nested one, so a
// second tie-break mechanism would be redundant -- see
// TestClassOrderDecidesExactCollisions.
func buildSets(bySrc map[Source][]classifiedPrefix) (map[Source]*netipx.IPSet, map[Source][]detailRange, []string) {
	sets := make(map[Source]*netipx.IPSet, len(bySrc))
	details := make(map[Source][]detailRange, len(bySrc))
	var detailStrings []string
	intern := make(map[string]uint32)

	internDetail := func(s string) uint32 {
		if id, ok := intern[s]; ok {
			return id
		}
		id := uint32(len(detailStrings))
		detailStrings = append(detailStrings, s)
		intern[s] = id
		return id
	}

	for src, prefixes := range bySrc {
		var b netipx.IPSetBuilder
		ranges := make([]detailRange, 0, len(prefixes))
		hasDetail := false
		for _, cp := range prefixes {
			b.AddPrefix(cp.prefix)
			detail := sanitiseDetail(cp.detail)
			if detail != "" {
				hasDetail = true
			}
			r := netipx.RangeOfPrefix(cp.prefix)
			ranges = append(ranges, detailRange{from: r.From(), to: r.To(), detail: internDetail(detail)})
		}
		// Every prefix reaching here already passed acceptablePrefix at
		// parse time, so an error is not expected -- but IPSet() returns
		// a usable set of whatever was valid even when it reports one,
		// so the return value is degrade-gracefully correct either way.
		// There is no per-source logger here to name it against; Refresh
		// already covers fetch and parse failures for this source.
		set, _ := b.IPSet()
		sets[src] = set
		if hasDetail {
			sort.Slice(ranges, func(i, j int) bool { return ranges[i].from.Less(ranges[j].from) })
			details[src] = ranges
		}
	}
	return sets, details, detailStrings
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
