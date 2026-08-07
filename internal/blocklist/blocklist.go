// Package blocklist implements local IP/CIDR "known-bad" matching
// against a small, vetted menu of free, curated threat-intel feeds
// (issue #113 Part B) -- deliberately *not* an arbitrary user-supplied
// URL field. Two things bound that decision: trust (an operator ticking
// a checkbox next to "Spamhaus DROP" is trusting mikroview's own vetting
// of that source; an arbitrary URL field would instead be trusting
// whatever the operator points it at, with no vetting at all -- and a
// malicious or compromised feed URL is a direct path to false "known
// bad" flags, or worse, an oversized response aimed at this package's
// own memory/CPU), and performance (every list gets consulted on the hot
// per-event ingest path, so the set of lists that can ever be loaded has
// to stay small and enumerable -- see maxTotalEntries below).
//
// The menu today:
//   - Spamhaus DROP + EDROP (default-enabled): small (~1-2k combined
//     CIDR ranges per the issue's own research), free, no registration,
//     and deliberately conservative -- Spamhaus only lists netblocks
//     they're confident are entirely malicious-controlled (hijacked/
//     stolen allocations, bulletproof hosting), which is exactly what
//     "safe to flag on sight, no behavioral corroboration needed" needs.
//   - Emerging Threats' compromised-IPs list (opt-in, not
//     default-enabled): a much larger, faster-changing list of
//     individual compromised hosts (not curated netblocks) -- still a
//     well-known, free, no-registration feed, but noisier and bigger by
//     nature, so it's offered on the menu without being part of the
//     conservative default.
//
// Performance: matching is a per-feed binary search over that feed's own
// sorted, disjoint (lo, hi) address ranges -- O(log n) per feed, never a
// linear scan, regardless of list size. Refresh is a fixed daily cycle
// (see RefreshInterval), not configurable -- an explicit product
// decision (issue #113) to avoid over-polling Spamhaus/ET's free
// infrastructure. A feed that fails to refresh keeps serving whatever it
// last successfully fetched (see Blocklist.Refresh) rather than going
// blind or blocking startup on network reachability.
package blocklist

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"sort"
	"sync"
	"time"
)

// Source identifies one entry on the vetted menu -- the only values a
// config.yaml "sources" list may contain (see internal/config.Blocklist
// and New's validation of unknown values).
type Source string

const (
	SourceSpamhausDROP               Source = "spamhaus_drop"
	SourceSpamhausEDROP              Source = "spamhaus_edrop"
	SourceEmergingThreatsCompromised Source = "emerging_threats_compromised"
)

// feedDef is one menu entry's fetch/parse definition -- see feedRegistry.
type feedDef struct {
	Source Source
	Label  string
	URL    string
	Parse  func([]byte) ([]netip.Prefix, error)
}

// feedRegistry is the entire vetted menu, in priority order -- see
// Blocklist.orderedEnabledSources' doc comment for what that order
// controls (which feed gets first claim on maxTotalEntries' shared
// budget during a Refresh). Spamhaus's two feeds are listed first
// because they're the recommended, default-enabled, curated-and-small
// choice; Emerging Threats' larger list is listed last so it never
// starves either Spamhaus feed of its combined-cap budget just because
// of enabled-map iteration order.
var feedRegistry = []feedDef{
	{
		Source: SourceSpamhausDROP,
		Label:  "Spamhaus DROP",
		URL:    "https://www.spamhaus.org/drop/drop.txt",
		Parse:  parseSpamhaus,
	},
	{
		Source: SourceSpamhausEDROP,
		Label:  "Spamhaus EDROP",
		URL:    "https://www.spamhaus.org/drop/edrop.txt",
		Parse:  parseSpamhaus,
	},
	{
		Source: SourceEmergingThreatsCompromised,
		Label:  "Emerging Threats compromised IPs",
		URL:    "https://rules.emergingthreats.net/blockrules/compromised-ips.txt",
		Parse:  parseEmergingThreatsCompromised,
	},
}

// registryBySource indexes feedRegistry for O(1) lookup by Source, built
// once at package init -- feedRegistry itself stays the source of truth
// for iteration order (see its own doc comment).
var registryBySource = func() map[Source]feedDef {
	m := make(map[Source]feedDef, len(feedRegistry))
	for _, f := range feedRegistry {
		m[f.Source] = f
	}
	return m
}()

// DefaultSources is the issue's own recommended starting point -- small,
// free, no registration, conservative. Used by internal/config's
// defaults(); exported so that default stays defined in exactly one
// place rather than duplicated as a string literal in two packages.
var DefaultSources = []string{string(SourceSpamhausDROP), string(SourceSpamhausEDROP)}

// KnownSources returns every Source on the menu, in registry order --
// for config validation error messages and docs/tests, not consulted on
// any hot path.
func KnownSources() []Source {
	out := make([]Source, len(feedRegistry))
	for i, f := range feedRegistry {
		out[i] = f.Source
	}
	return out
}

const (
	// fetchTimeout bounds one feed's HTTP fetch -- generous for a
	// megabyte-scale plain-text download over a possibly-slow path, but
	// still bounded so a hung upstream can't wedge Refresh forever.
	fetchTimeout = 30 * time.Second
	// maxFetchBytes caps how much of a feed response is ever read into
	// memory -- defense in depth against a compromised or misbehaving
	// upstream serving an oversized response, even though every URL on
	// the menu is fixed and vetted (see the package doc comment). Well
	// above any plausible real size for these feeds (a few MB at most).
	maxFetchBytes = 16 << 20
	// maxTotalEntries is the hard cap on combined raw entries across
	// every *enabled* feed, enforced during Refresh (see its doc
	// comment) -- the answer to issue #113's explicitly-left-open
	// "exact entry-count performance ceiling" question, resolved here
	// with real measurements rather than estimates:
	//
	// A live fetch confirms current real sizes: Spamhaus DROP+EDROP
	// combined is ~1.7k CIDR ranges, Emerging Threats compromised-IPs is
	// ~0.6k individual addresses -- ~2.2k combined today, nowhere near
	// even the old 10,000 cap. Separately, benchmarking searchRanges and
	// buildRanges directly (this package's actual hot-path lookup and
	// Refresh-time rebuild) shows neither is remotely close to a real
	// ceiling at 10,000: Match-path lookups cost ~100-150ns whether the
	// table holds 10k or 5 million entries (O(log n) barely moves), and
	// a full daily rebuild takes ~3ms at 10k, ~33ms at 100k, and still
	// only ~280ms at 1 million, with memory scaling linearly at ~76
	// bytes/entry (~7.5MB at 100k). 100,000 is chosen as real headroom
	// (~45x today's actual combined feed size, not just ~5x) while
	// staying an order of magnitude below where memory footprint would
	// start to be worth another look on constrained self-hosted
	// hardware -- there's no technical reason to go further than that
	// without a concrete feed on the menu that would actually need it.
	// A feed that would push the combined total over this cap has its
	// excess entries truncated (see Refresh), not rejected outright -- a
	// partially-loaded list still catches most of what it would have,
	// which is strictly better than the alternative of loading nothing
	// at all from that feed.
	maxTotalEntries = 100_000
	// RefreshInterval is fixed, not user-configurable -- see the
	// package doc comment for why (avoiding over-polling Spamhaus/ET's
	// free infrastructure was an explicit issue #113 requirement, not
	// left to per-deployment taste). main.go drives the actual ticker
	// (see its own doc comment for why this lives there rather than
	// inside a Run method on this type), the same
	// StaleRuleDetector/globalSpikeCheckInterval ticker shape
	// internal/detect and main.go already use elsewhere.
	RefreshInterval = 24 * time.Hour
)

// rangeEntry is one merged, disjoint (lo, hi) address range within a
// single feed's sorted table -- see buildRanges for how overlapping/
// nested input prefixes get merged into this shape, which is what makes
// the binary search in Blocklist.Match provably correct (a binary search
// over ranges that can partially overlap is *not* correct in general;
// disjoint, sorted ranges are the property that makes it work).
type rangeEntry struct {
	lo, hi netip.Addr
	// cidr is the original CIDR text, kept only when this range came
	// from exactly one input prefix (the overwhelmingly common case --
	// Spamhaus's own curation policy keeps DROP/EDROP's entries
	// disjoint). Empty when this range absorbed more than one input
	// prefix during merging, in which case label() below falls back to
	// a plain lo-hi address-range string.
	cidr string
}

// label returns the human-readable form of r for a flag's Detail
// string -- the original CIDR when there is one, otherwise the merged
// range's own bounds.
func (r rangeEntry) label() string {
	if r.cidr != "" {
		return r.cidr
	}
	if r.lo == r.hi {
		return r.lo.String()
	}
	return r.lo.String() + "-" + r.hi.String()
}

// feedState is one enabled feed's current data plus refresh bookkeeping,
// guarded by Blocklist.mu.
type feedState struct {
	def       feedDef
	ranges    []rangeEntry // sorted by lo, disjoint -- see buildRanges
	rawCount  int          // entry count before merging, for logging/observability
	fetchedAt time.Time    // zero until the first successful fetch
	lastErr   error
}

// MatchResult describes one local-blocklist hit -- returned by
// Blocklist.Match, and the shape internal/detect.knownBadIPLookup
// depends on (see internal/detect/known_bad_ip.go).
type MatchResult struct {
	Source Source
	Label  string // e.g. "Spamhaus DROP"
	Range  string // the matched CIDR or address range, e.g. "1.2.3.0/24"
}

// Blocklist holds every enabled feed's current data and serves
// Match against it -- the zero value is not usable; construct with New.
// Safe for concurrent use: Match takes a read lock, Refresh takes a
// write lock only around the specific feed it just finished fetching
// (not the whole refresh pass), so a slow/failing feed's fetch never
// blocks Match or another feed's own refresh.
type Blocklist struct {
	mu     sync.RWMutex
	feeds  map[Source]*feedState
	client *http.Client
	log    *slog.Logger
}

// New builds a Blocklist for sourceNames (config.yaml's blocklist.sources
// list, e.g. ["spamhaus_drop", "spamhaus_edrop"]) -- an unrecognized name
// is logged and skipped, same "malformed input degrades, doesn't crash
// mikroview" contract as every other optional integration in this
// codebase. No fetch happens here -- feeds start out empty (Match always
// misses) until the first Refresh call, same as every other
// eventually-populated cache in mikroview.
func New(sourceNames []string, log *slog.Logger) *Blocklist {
	b := &Blocklist{
		feeds:  make(map[Source]*feedState),
		client: &http.Client{Timeout: fetchTimeout},
		log:    log,
	}
	for _, name := range sourceNames {
		src := Source(name)
		def, ok := registryBySource[src]
		if !ok {
			log.Warn(fmt.Sprintf("unknown blocklist source %q ignored (see docs/configuration.md for the supported menu)", name))
			continue
		}
		b.feeds[src] = &feedState{def: def}
	}
	return b
}

// HasFeeds reports whether at least one recognized source is enabled --
// main.go uses this to decide whether the refresh ticker goroutines are
// worth starting at all (an empty list means the feature is off, same
// "don't start a goroutine for something that's disabled" precedent
// internal/notify's dispatcher already sets).
func (b *Blocklist) HasFeeds() bool {
	return len(b.feeds) > 0
}

// orderedEnabledSources returns this Blocklist's enabled sources in
// feedRegistry's fixed priority order -- what Refresh actually iterates,
// so that maxTotalEntries' shared budget (see that const's doc comment)
// is allocated deterministically (Spamhaus's two feeds first) rather
// than depending on Go's randomized map iteration order.
func (b *Blocklist) orderedEnabledSources() []Source {
	out := make([]Source, 0, len(b.feeds))
	for _, f := range feedRegistry {
		if _, ok := b.feeds[f.Source]; ok {
			out = append(out, f.Source)
		}
	}
	return out
}

// Match reports whether ipStr falls within any enabled feed's current
// range table -- O(log n) per feed via searchRanges, so O(k log n)
// overall for k enabled feeds (k is at most len(feedRegistry), a small
// fixed constant, so this is still effectively O(log n) against the
// combined entry cap). Malformed input or no match reports ok=false,
// never an error -- same "absence of evidence" shape every other
// best-effort lookup in this codebase uses.
func (b *Blocklist) Match(ipStr string) (MatchResult, bool) {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return MatchResult{}, false
	}
	addr = addr.Unmap()

	b.mu.RLock()
	defer b.mu.RUnlock()
	for src, fs := range b.feeds {
		if idx, ok := searchRanges(fs.ranges, addr); ok {
			return MatchResult{Source: src, Label: fs.def.Label, Range: fs.ranges[idx].label()}, true
		}
	}
	return MatchResult{}, false
}

// searchRanges binary-searches ranges (sorted by lo, disjoint -- see
// buildRanges) for one containing addr. Standard "point inside sorted
// disjoint intervals" search: find the rightmost range whose lo is <=
// addr, then check whether addr also falls at or before that range's
// hi. Correct only because buildRanges guarantees the disjoint
// precondition -- see rangeEntry's doc comment for why that matters.
func searchRanges(ranges []rangeEntry, addr netip.Addr) (int, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].lo.Compare(addr) > 0 })
	if i == 0 {
		return 0, false
	}
	cand := i - 1
	if ranges[cand].hi.Compare(addr) >= 0 {
		return cand, true
	}
	return 0, false
}

// Refresh fetches and re-parses every enabled feed, in priority order
// (see orderedEnabledSources), applying the shared maxTotalEntries
// budget across them as it goes. Each feed is handled independently: a
// fetch/parse failure for one feed logs a warning and leaves that feed's
// existing ranges (and every other feed's) completely untouched --
// "keep serving the last successfully-fetched list on fetch failure" is
// the default outcome here, not a special case, because a failed feed
// simply never reaches the point where it would overwrite fs.ranges.
// Safe to call concurrently with Match (see Blocklist's own doc
// comment) and safe to call repeatedly (main.go's daily ticker, plus one
// best-effort call at startup).
func (b *Blocklist) Refresh(ctx context.Context) {
	remaining := maxTotalEntries
	for _, src := range b.orderedEnabledSources() {
		// b.feeds is only ever populated once, in New, and never added
		// to/removed from afterward -- so reading the map itself
		// (as opposed to the *feedState fields inside it, which do
		// mutate and are guarded below) needs no lock.
		fs := b.feeds[src]

		prefixes, err := fetchAndParse(ctx, b.client, fs.def)
		if err != nil {
			b.mu.Lock()
			fs.lastErr = err
			rawCount, fetchedAt := fs.rawCount, fs.fetchedAt
			b.mu.Unlock()
			if fetchedAt.IsZero() {
				b.log.Warn(fmt.Sprintf("%s: initial fetch failed (%v) -- no data from this feed until the next refresh succeeds", fs.def.Label, err))
			} else {
				b.log.Warn(fmt.Sprintf("%s: refresh failed (%v) -- continuing to serve the last successfully-fetched list (%d entries as of %s)",
					fs.def.Label, err, rawCount, fetchedAt.Format(time.RFC3339)))
			}
			continue
		}

		truncated := len(prefixes) > remaining
		if truncated {
			prefixes = prefixes[:remaining]
		}
		remaining -= len(prefixes)

		ranges := buildRanges(prefixes)
		b.mu.Lock()
		fs.ranges = ranges
		fs.rawCount = len(prefixes)
		fs.fetchedAt = time.Now()
		fs.lastErr = nil
		b.mu.Unlock()

		msg := fmt.Sprintf("%s: refreshed, %d entries (%d merged ranges)", fs.def.Label, len(prefixes), len(ranges))
		if truncated {
			msg += fmt.Sprintf(" -- truncated to stay within the %d-entry combined cap across all enabled lists", maxTotalEntries)
		}
		b.log.Info(msg)
	}
}

// fetchAndParse downloads def.URL (bounded by fetchTimeout/
// maxFetchBytes) and hands the body to def.Parse.
func fetchAndParse(ctx context.Context, client *http.Client, def feedDef) ([]netip.Prefix, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, def.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return nil, err
	}
	return def.Parse(body)
}

// buildRanges converts prefixes into a sorted table of disjoint (lo, hi)
// ranges, merging any that overlap or nest -- required for
// searchRanges' binary search to be correct (see rangeEntry's doc
// comment). In practice, for every feed on today's menu, this is a
// no-op merge (each output range corresponds to exactly one input
// prefix) since Spamhaus/ET both already keep their own entries
// disjoint -- but correctness here doesn't depend on that holding.
func buildRanges(prefixes []netip.Prefix) []rangeEntry {
	type raw struct {
		lo, hi netip.Addr
		cidr   string
	}
	raws := make([]raw, 0, len(prefixes))
	for _, p := range prefixes {
		p = p.Masked()
		raws = append(raws, raw{lo: p.Addr(), hi: lastAddr(p), cidr: p.String()})
	}
	sort.Slice(raws, func(i, j int) bool {
		if c := raws[i].lo.Compare(raws[j].lo); c != 0 {
			return c < 0
		}
		return raws[i].hi.Compare(raws[j].hi) < 0
	})

	merged := make([]rangeEntry, 0, len(raws))
	for _, r := range raws {
		if n := len(merged); n > 0 && r.lo.Compare(merged[n-1].hi) <= 0 {
			// Overlaps or nests inside the previous range -- extend it
			// rather than appending a new one, and drop the single-CIDR
			// label since this merged range no longer corresponds to
			// exactly one input prefix.
			if r.hi.Compare(merged[n-1].hi) > 0 {
				merged[n-1].hi = r.hi
			}
			merged[n-1].cidr = ""
			continue
		}
		merged = append(merged, rangeEntry(r))
	}
	return merged
}

// lastAddr returns the highest address covered by p (its broadcast/
// all-ones-host-bits address) -- net/netip has no built-in for this, so
// it's computed directly: p's own address with every bit past p.Bits()
// set to 1.
func lastAddr(p netip.Prefix) netip.Addr {
	bytes := p.Addr().AsSlice() // AsSlice returns a fresh copy -- safe to mutate in place
	totalBits := len(bytes) * 8
	for i := p.Bits(); i < totalBits; i++ {
		bytes[i/8] |= 1 << (7 - i%8)
	}
	last, ok := netip.AddrFromSlice(bytes)
	if !ok {
		// Unreachable: bytes' length always came from a valid Addr's
		// own AsSlice (4 or 16), which AddrFromSlice always accepts.
		return p.Addr()
	}
	return last
}
