// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// This is #402's own benchmark gate, alongside re-running
// internal/detect/dispatch_bench_test.go's existing BenchmarkDispatch
// unmodified (this package's declarative code does not touch
// internal/detect or internal/watchlist at all -- see this file's
// header for how that is proven, not merely asserted). The budget is
// the same one BenchmarkDispatch documents: mikroview's measured burst
// rate is ~3,900 events/sec, giving a per-event budget of
// 1e9/3900 =~ 256,410 ns. BenchmarkDeclarativeDispatch's ns/op at
// definitions=50 is what gets compared against that figure for the new
// declarative dispatch path specifically.

// declarativeBenchEvents builds a deterministic (fixed-seed) synthetic
// event stream: mostly ordinary LAN traffic that lands in no
// discriminating bucket at all, a slice hitting one of a pool of
// "watched" destination ports (the destinationPort-discriminant shape),
// and a slice carrying a rule label (the ruleLabel-discriminant shape)
// -- enough variety that BuildDispatchIndex's bucket selection and
// DeclarativeSet.Evaluate's condition matching both do real work every
// call, not just a single flattering code path.
func declarativeBenchEvents(n int, seed int64) []store.Event {
	r := rand.New(rand.NewSource(seed))

	lan := make([]string, 60)
	for i := range lan {
		lan[i] = fmt.Sprintf("192.168.1.%d", 2+i)
	}
	attackers := make([]string, 200)
	for i := range attackers {
		attackers[i] = fmt.Sprintf("198.51.100.%d", 1+i%254)
	}
	chains := []string{"forward", "input", "output"}
	commonPorts := []int{80, 443, 53, 123, 8080}
	watchedPorts := make([]int, 20)
	for i := range watchedPorts {
		watchedPorts[i] = 20000 + i
	}

	events := make([]store.Event, n)
	for i := range events {
		switch {
		case i%10 < 6: // 60%: ordinary traffic, misses every discriminating bucket
			events[i] = store.Event{
				SrcIP: lan[r.Intn(len(lan))], DstIP: fmt.Sprintf("203.0.113.%d", 2+r.Intn(250)),
				DstPort: commonPorts[r.Intn(len(commonPorts))], Chain: "forward",
				Action: store.ActionAccept, ConnState: "established",
			}
		case i%10 < 8: // 20%: hits a watched destination port
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort: watchedPorts[r.Intn(len(watchedPorts))], Chain: chains[r.Intn(len(chains))],
				Action: store.ActionDrop, ConnState: "new",
			}
		default: // 20%: rule-labeled drop
			events[i] = store.Event{
				SrcIP: attackers[r.Intn(len(attackers))], DstIP: lan[0],
				DstPort: 8080, Chain: "forward", RuleLabel: fmt.Sprintf("r%d", 1+r.Intn(15)),
				Action: store.ActionDrop, ConnState: "new",
			}
		}
	}
	return events
}

// buildBenchDeclarativeSet builds n declarative definitions with mixed
// discriminating fields -- 40% destination port, 20% chain, 20% rule
// label, and 20% with no discriminating field at all (OpNotEquals never
// discriminates -- see discriminantValuesFor), landing in the always-
// consulted bucket. This is the "N declarative definitions... mixed
// discriminating fields" shape #402 asks the benchmark to cover.
func buildBenchDeclarativeSet(b *testing.B, n int) *DeclarativeSet {
	b.Helper()
	defs := make([]*DeclarativeDefinition, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("bench-%d", i)
		def := NewDefinition(id, IntentDetection, KindDeclarative)
		def.ID = id
		def.Enabled = true
		def.Provenance = Provenance{Origin: ProvenanceCustom}

		var cond Condition
		switch i % 5 {
		case 0, 1: // destination port
			port := 20000 + (i % 20)
			cond = Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{fmt.Sprintf("%d", port)}}
		case 2: // chain
			cond = Condition{Field: FieldChain, Operator: OpEquals, Values: []string{"input"}}
		case 3: // rule label
			cond = Condition{Field: FieldRuleLabel, Operator: OpEquals, Values: []string{fmt.Sprintf("r%d", (i%15)+1)}}
		default: // no discriminating field -- always-consulted
			cond = Condition{Field: FieldConnectionState, Operator: OpNotEquals, Values: []string{"established"}}
		}

		dd, err := NewDeclarativeDefinition(def, DeclarativeSpec{
			Conditions:     []Condition{cond},
			Key:            KeyPerSource,
			Window:         time.Minute,
			Threshold:      5,
			CountingMode:   CountingTotal,
			DetailTemplate: "{PortCount} hits",
			Evidence:       []EvidenceField{EvidencePorts, EvidenceHosts, EvidenceLabels},
		})
		if err != nil {
			b.Fatalf("NewDeclarativeDefinition: %v", err)
		}
		defs = append(defs, dd)
	}
	return NewDeclarativeSet("bench-set", defs)
}

// BenchmarkDeclarativeDispatch drives DeclarativeSet.Evaluate (dispatch
// pre-index selection, structured-condition matching, window/threshold
// counting and evidence accumulation together) over declarativeBenchEvents
// at 10, 50 and 200 declarative definitions. Run with
// -bench BenchmarkDeclarativeDispatch -benchmem -count=3; see this
// file's header comment for the burst budget ns/op is compared against.
func BenchmarkDeclarativeDispatch(b *testing.B) {
	const poolSize = 20000
	events := declarativeBenchEvents(poolSize, 1)

	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("definitions=%d", n), func(b *testing.B) {
			set := buildBenchDeclarativeSet(b, n)

			// Same busy-hour timestamp spacing as
			// internal/detect/dispatch_bench_test.go's own
			// BenchmarkDispatch, for the same reason: representative
			// window/ring/eviction behavior, independent of b.N.
			base := time.Now()
			step := time.Second / 1200

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e := events[i%poolSize]
				e.ReceivedAt = base.Add(time.Duration(i) * step)
				set.Evaluate(e)
			}
		})
	}
}
