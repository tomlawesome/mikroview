// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// BenchmarkIngest is issue #405's own budget gate, and the successor to
// internal/detect/dispatch_bench_test.go's BenchmarkDispatch, which was
// deleted with that package.
//
// It exists because BenchmarkDispatch measured a shrinking thing. That
// benchmark drove detect.Detector.Observe, and every detector #405 ported
// left that path -- so its ns/op fell with each commit for reasons that
// had nothing to do with the ingest cost an operator actually pays.
// Comparing it against #397's recorded baseline mid-port would have
// flattered the port by measuring less of it. This benchmark drives every
// shipped definition per event, exactly as main.go's ingest goroutine
// does: one DeclarativeSet holding every declarative definition (so its
// dispatch pre-index is exercised) plus each programmatic definition in
// turn. Its ns/op is therefore directly comparable against #397's
// recorded figures for the whole detection path -- with one caveat now
// that the port is finished, stated here rather than left to be
// rediscovered: #397's figures included internal/detect's own per-event
// work, and internal/detect no longer exists. The comparison is still
// like-for-like in the only sense that matters -- both numbers are "what
// one event costs the ingest path" -- but the composition of that number
// has changed, and a drop is expected rather than suspicious.
//
// The budget, unchanged from #397: mikroview's measured burst rate is
// ~3,900 events/sec, so the per-event budget is 1e9/3900 =~ 256,410 ns.
//
// The synthetic stream and the watchlist tiers deliberately mirror
// BenchmarkDispatch's -- same shape, same seed, same tiers -- because
// the point is comparability with the recorded baseline, not a better
// benchmark.
//
// What changed under the tiers, and why the comparison still holds
// (#406): the watchlist half used to be a linear scan, driven here the
// way production drove it -- Store.List() per event, then
// watchlist.Match against every entry in turn. Watchlist entries are
// expectation definitions on this engine now, so that is what this
// drives: one DeclarativeSet holding every non-inverted expectation,
// consulted through its dispatch pre-index, exactly as main.go
// registers it. Both numbers are still "what one event costs the ingest
// path", which is the only sense in which #397's figures were ever
// comparable; the fall at the 1,000- and 5,000-entry tiers is the
// pre-index doing the job the ADR specified it for, and #397's own
// caveat about Store.List() being a conservative upper bound no longer
// applies because nothing calls it per event any more.
//
// One non-goal carries over unchanged: the entries are seeded on ports
// the synthetic stream mostly does not use, so this measures dispatch
// and matching rather than matchlog's fsync.
func BenchmarkIngest(b *testing.B) {
	const poolSize = 20000
	events := ingestBenchEvents(poolSize, 1)

	for _, watchlistEntries := range []int{0, 1000, 5000} {
		b.Run(fmt.Sprintf("watchlist_entries=%d", watchlistEntries), func(b *testing.B) {
			fs, err := flags.Open("")
			if err != nil {
				b.Fatalf("flags.Open: %v", err)
			}
			set := benchShippedDeclarativeSet(b, fs)
			prog := benchShippedProgrammaticDefs(b, fs)
			expectations := buildIngestBenchExpectations(b, watchlistEntries)

			base := time.Now()
			step := time.Second / 1200 // busy-hour spacing, see BenchmarkDispatch

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e := events[i%poolSize]
				e.ReceivedAt = base.Add(time.Duration(i) * step)

				set.Evaluate(e)
				for _, p := range prog {
					p.Evaluate(e)
				}
				expectations.Evaluate(e)
			}
		})
	}
}

// benchShippedDeclarativeSet builds every shipped declarative definition
// at its migrated default params -- the same set main.go registers, built
// the same way (shippedDetectors' defaults through
// BuildShippedDeclarativeDefinition, wrapped in one DeclarativeSet so the
// dispatch pre-index is exercised too).
func benchShippedDeclarativeSet(b *testing.B, fs *flags.Store) *DeclarativeSet {
	b.Helper()
	cfg := DefaultShippedDefaults()
	var defs []*DeclarativeDefinition
	for _, sd := range shippedDetectors {
		if sd.kind != KindDeclarative {
			continue
		}
		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			b.Fatalf("%s: default params: %v", sd.id, err)
		}
		dd, err := BuildShippedDeclarativeDefinition(Definition{
			ID:          sd.id,
			Name:        shippedDetectorDisplayNames[sd.id],
			Intent:      IntentDetection,
			Kind:        KindDeclarative,
			Enabled:     true,
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		})
		if err != nil {
			b.Fatalf("%s: BuildShippedDeclarativeDefinition: %v", sd.id, err)
		}
		dd.OnRoutedEmission = FlagsSink(fs)
		defs = append(defs, dd)
	}
	return NewDeclarativeSet("shipped-declarative", defs)
}

// benchShippedProgrammaticDefs builds every shipped programmatic
// definition that has a builder registered, at its migrated default
// params -- the same set main.go registers individually on the engine.
// Driven per event here exactly as Engine.evaluateEvent drives them, so
// the measured cost is the real one rather than the declarative half
// alone.
//
// Ticked definitions are included: their Evaluate is a no-op by
// construction, and the point of driving them anyway is that the cost of
// having them registered at all -- one interface call per event per
// definition -- is part of what an operator pays and so part of what
// this benchmark should show.
func benchShippedProgrammaticDefs(b *testing.B, fs *flags.Store) []Evaluated {
	b.Helper()
	cfg := DefaultShippedDefaults()
	var out []Evaluated
	for _, sd := range shippedDetectors {
		if sd.kind != KindProgrammatic {
			continue
		}
		if _, ok := shippedProgrammaticBuilders[sd.id]; !ok {
			continue // not ported yet -- internal/detect still evaluates it
		}
		params, err := ValidateParams(sd.schema, sd.params(cfg))
		if err != nil {
			b.Fatalf("%s: default params: %v", sd.id, err)
		}
		pd, err := BuildShippedProgrammaticDefinition(Definition{
			ID:          sd.id,
			Name:        shippedDetectorDisplayNames[sd.id],
			Intent:      IntentDetection,
			Kind:        KindProgrammatic,
			Enabled:     true,
			Params:      params,
			ParamSchema: sd.schema,
			Provenance:  Provenance{Origin: ProvenanceShipped, ShippedParams: params},
		}, ShippedDeps{})
		if err != nil {
			b.Fatalf("%s: BuildShippedProgrammaticDefinition: %v", sd.id, err)
		}
		if sink, ok := pd.(interface{ SetSink(func(RoutedEmission)) }); ok {
			sink.SetSink(FlagsSink(fs))
		}
		out = append(out, pd)
	}
	return out
}

// buildIngestBenchExpectations builds n watchlist entries as expectation
// definitions, in one DeclarativeSet, exactly as main.go registers them
// -- same entry shape BenchmarkDispatch used (one distinct watched port
// each), so the tiers stay comparable with #397's recorded baseline.
//
// The sink is nil: an emission still renders and routes, it just reaches
// no match log, which keeps matchlog's fsync out of a measurement about
// dispatch.
func buildIngestBenchExpectations(b *testing.B, n int) *DeclarativeSet {
	b.Helper()
	entries := make([]watchlist.Entry, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, watchlist.Entry{ID: fmt.Sprintf("e%d", i), Ports: []int{2222 + i}})
	}
	set, _, err := BuildExpectations(entries, ExpectationDeps{})
	if err != nil {
		b.Fatalf("BuildExpectations: %v", err)
	}
	return set
}

// ingestBenchEvents is internal/detect/dispatch_bench_test.go's
// syntheticEvents, restated here (that function is unexported and in
// another package) with the same shape, pools and seed so the two
// benchmarks measure the same stream.
func ingestBenchEvents(n int, seed int64) []store.Event {
	r := rand.New(rand.NewSource(seed))

	lan := make([]string, 60)
	for i := range lan {
		lan[i] = fmt.Sprintf("192.168.%d.%d", 1+i/250, 2+i%250)
	}
	extDest := make([]string, 300)
	for i := range extDest {
		extDest[i] = fmt.Sprintf("203.0.%d.%d", 20+i/250, 2+i%250)
	}
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
	connStates := []string{"", "new", "established", "established"}
	actions := []store.Action{store.ActionAccept, store.ActionAccept, store.ActionAccept, store.ActionDrop, store.ActionReject}

	events := make([]store.Event, n)
	for i := range events {
		switch {
		case i%10 < 7:
			events[i] = store.Event{
				SrcIP: lan[r.Intn(len(lan))], DstIP: extDest[r.Intn(len(extDest))],
				DstPort: commonPorts[r.Intn(len(commonPorts))], Action: actions[r.Intn(len(actions))],
				ConnState: connStates[r.Intn(len(connStates))],
			}
		case i%10 < 8:
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort:   ingestBenchPick(r, criticalPorts, r.Intn(60000)+1024),
				Action:    store.ActionDrop,
				ConnState: "new", SrcCountry: "DE",
			}
		case i%10 < 9:
			dst := internalDest[r.Intn(len(internalDest))]
			if r.Intn(2) == 0 {
				dst = extDest[r.Intn(len(extDest))]
			}
			events[i] = store.Event{
				SrcIP: lan[r.Intn(10)], DstIP: dst,
				DstPort: commonPorts[r.Intn(len(commonPorts))], Action: store.ActionAccept, ConnState: "new",
			}
		default:
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort: 8080, Action: store.ActionDrop, ConnState: "new",
				RuleLabel: rules[r.Intn(len(rules))],
			}
		}
	}
	return events
}

func ingestBenchPick(r *rand.Rand, list []int, fallback int) int {
	if r.Intn(3) == 0 {
		return fallback
	}
	return list[r.Intn(len(list))]
}
