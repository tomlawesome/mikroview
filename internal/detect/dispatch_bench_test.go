// SPDX-License-Identifier: AGPL-3.0-only

package detect

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// This is the ingest-budget baseline the evaluation-engine ADR
// (docs/decisions/evaluation-engine.md, "Costs, stated") requires before
// any engine code touches internal/detect or internal/watchlist:
// "Declarative dispatch must not cost what hand-fused loops do not... a
// dispatch benchmark run before and after."
//
// Design point (from the issue): mikroview's measured ingest rate is
// ~594 events/sec daily average, ~1,100-1,300/s in a busy hour, and up
// to ~3,900/s in a burst minute. At 3,900 events/sec, the per-event
// budget is 1e9/3900 =~ 256,410 ns -- BenchmarkDispatch's ns/op is
// directly comparable against that figure: an ns/op comfortably under
// it survives the burst; anything approaching or exceeding it does not,
// and this is where that would first show up.
//
// Two things this benchmark deliberately does NOT do, both matching
// existing precedent elsewhere in this codebase rather than inventing a
// new convention:
//
//   - The watchlist entries never match the synthetic stream (see
//     syntheticEvents' port/rule pools vs buildWatchlist's), the same
//     choice internal/watchlist/evaluator_test.go's own
//     BenchmarkEvaluateRecovered makes ("this measures the common 'no
//     entry matched' case... not matchLog.Append's fsync cost"). A
//     match's fsync cost is real but is a distinct, separately
//     understood cost from the dispatch/scan cost this benchmark exists
//     to measure -- mixing them in would make a slow disk look like a
//     slow dispatcher.
//   - The watchlist side calls the exported Store.List() once per event
//     rather than the unexported, unsorted entriesSnapshot() that
//     internal/watchlist.Evaluator.evaluateRecovered actually uses on
//     the hot path (see evaluator.go) -- entriesSnapshot is not reachable
//     from this package. List() additionally sorts by ID on every call,
//     a cost evaluateRecovered does not pay, which makes this
//     benchmark's watchlist-side numbers a conservative (slightly
//     pessimistic) upper bound on the real per-event watchlist cost, not
//     an exact match to it. internal/watchlist's own
//     BenchmarkEvaluateRecovered (evaluator_test.go) is the precise
//     figure for the watchlist side alone, at the same 10-5000 entry
//     tiers; this benchmark's contribution is the combined detect+
//     watchlist per-event cost real ingest actually pays, which neither
//     package's own benchmark measures alone.

// syntheticEvents builds a deterministic (fixed-seed), realistically
// varied event stream: mostly ordinary LAN-to-external traffic on common
// ports, a slice of external sources probing critical ports and varying
// destination ports (port-scan/critical-port/distributed-brute-force
// shaped), a slice of internal hosts touching many distinct internal and
// external destinations (recon/outbound-anomaly shaped), and a slice of
// rule-labeled drops against locally-hosted ports (repeated-drops/
// rule-spike shaped) -- one synthetic stream exercising every detector's
// own code path, not a single repeated event shape that would flatter
// map/ring lookups into appearing cheaper than they are.
func syntheticEvents(n int, seed int64) []store.Event {
	r := rand.New(rand.NewSource(seed))

	lan := make([]string, 60)
	for i := range lan {
		lan[i] = fmt.Sprintf("192.168.%d.%d", 1+i/250, 2+i%250)
	}
	// A wide pool of external "regular" destinations ordinary LAN
	// traffic talks to (browsing, updates, etc.) -- kept separate from
	// the "attacker" pool below so outbound-anomaly's own distinct-count
	// logic sees realistic breadth, not one host hammering one IP.
	extDest := make([]string, 300)
	for i := range extDest {
		extDest[i] = fmt.Sprintf("203.0.%d.%d", 20+i/250, 2+i%250)
	}
	// A wide pool of external sources probing in from the internet --
	// critical-port attempts, distributed-brute-force, port/low-slow
	// scanning.
	attackers := make([]string, 500)
	for i := range attackers {
		attackers[i] = fmt.Sprintf("198.51.%d.%d", 100+i/250, 2+i%250)
	}
	internalDest := make([]string, 40)
	for i := range internalDest {
		internalDest[i] = fmt.Sprintf("192.168.50.%d", 2+i%250)
	}

	rules := make([]string, 15)
	for i := range rules {
		rules[i] = fmt.Sprintf("r%d", i+1)
	}
	commonPorts := []int{80, 443, 53, 123, 8080, 5353}
	criticalPorts := []int{21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729}
	connStates := []string{"", "new", "established", "established"} // established weighted, matches real accept-rule logging
	actions := []store.Action{store.ActionAccept, store.ActionAccept, store.ActionAccept, store.ActionDrop, store.ActionReject}

	events := make([]store.Event, n)
	for i := range events {
		switch {
		case i%10 < 7: // 70%: ordinary LAN -> external traffic
			events[i] = store.Event{
				SrcIP: lan[r.Intn(len(lan))], DstIP: extDest[r.Intn(len(extDest))],
				DstPort: commonPorts[r.Intn(len(commonPorts))], Action: actions[r.Intn(len(actions))],
				ConnState: connStates[r.Intn(len(connStates))],
			}
		case i%10 < 8: // 10%: external source probing in
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort:   pick(r, criticalPorts, r.Intn(60000)+1024),
				Action:    store.ActionDrop,
				ConnState: "new", SrcCountry: "DE",
			}
		case i%10 < 9: // 10%: LAN source with wide destination spread (recon/outbound-anomaly shaped)
			dst := internalDest[r.Intn(len(internalDest))]
			if r.Intn(2) == 0 {
				dst = extDest[r.Intn(len(extDest))]
			}
			events[i] = store.Event{
				SrcIP: lan[r.Intn(10)], DstIP: dst, // a narrower set of LAN sources so distinct-destination counts actually build up
				DstPort: commonPorts[r.Intn(len(commonPorts))], Action: store.ActionAccept, ConnState: "new",
			}
		default: // 10%: rule-labeled drop against a locally-hosted port
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort: 8080, Action: store.ActionDrop, ConnState: "new",
				RuleLabel: rules[r.Intn(len(rules))],
			}
		}
	}
	return events
}

func pick(r *rand.Rand, list []int, fallback int) int {
	if r.Intn(3) == 0 {
		return fallback // mostly non-critical, occasionally a critical port
	}
	return list[r.Intn(len(list))]
}

// buildBenchWatchlist builds an in-memory watchlist.Store of n entries
// that never match syntheticEvents' traffic (ports 2222+, well outside
// commonPorts/criticalPorts/8080 above) -- see this file's header
// comment for why.
func buildBenchWatchlist(b *testing.B, n int) *watchlist.Store {
	b.Helper()
	s, err := watchlist.OpenWithBackend(nil) // nil backend: in-memory only, no file I/O in the loop
	if err != nil {
		b.Fatalf("watchlist.OpenWithBackend: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := s.Upsert(watchlist.Entry{ID: fmt.Sprintf("e%d", i), Ports: []int{2222 + i}}); err != nil {
			b.Fatalf("Upsert: %v", err)
		}
	}
	return s
}

// BenchmarkDispatch drives Detector.Observe (every detector enabled, at
// DefaultConfig scale) and watchlist evaluation together over the
// synthetic stream, at watchlist sizes of 0 (detect alone), 1,000 and
// 5,000 entries. Run with -bench -benchmem -count>=3; see this file's
// header comment for how to read ns/op against the burst budget.
func BenchmarkDispatch(b *testing.B) {
	const poolSize = 20000
	events := syntheticEvents(poolSize, 1)

	for _, watchlistEntries := range []int{0, 1000, 5000} {
		b.Run(fmt.Sprintf("watchlist_entries=%d", watchlistEntries), func(b *testing.B) {
			fs, err := flags.Open("")
			if err != nil {
				b.Fatalf("flags.Open: %v", err)
			}
			d := NewWithSettings(DefaultConfig(), fs, AllEnabledSettingsStore())
			wl := buildBenchWatchlist(b, watchlistEntries)

			// Timestamps advance at the busy-hour rate (1,200/s =~ 833us
			// apart) so window/ring/eviction behaviour is representative
			// of sustained real load, independent of b.N and of how long
			// this process actually takes to run each iteration -- see
			// this file's header comment for why the *reported* ns/op is
			// what gets compared against the burst budget, not this
			// spacing.
			base := time.Now()
			step := time.Second / 1200

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e := events[i%poolSize]
				e.ReceivedAt = base.Add(time.Duration(i) * step)

				d.Observe(e)
				for _, entry := range wl.List() {
					watchlist.Match(entry, e)
				}
			}
		})
	}
}
