// SPDX-License-Identifier: AGPL-3.0-only

// External test package on purpose: internal/routeros imports store, so
// building events the way main.go does can only be done from outside.
package store_test

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/store"
)

// What a stored event costs is an operator-facing number: store.maxEvents
// is the only control over mikroview's resident memory, and the ring is
// allocated in full at startup (store.New's make([]Event, capacity)), so
// choosing a value is choosing a memory reservation. The figures these
// tests print are the ones documented for that choice -- they live in a
// committed test rather than a one-off benchmark so they can be
// re-derived on demand instead of being trusted from a changelog.

// eventStructSize is the size of one store.Event on 64-bit, excluding the
// string bytes its fields point at. It is asserted rather than merely
// logged because it is the fixed part of the ring's cost: capacity is
// multiplied by exactly this at startup, whatever traffic later does.
//
// If this fails after a deliberate field change, update the constant and
// the figures in the operator docs together -- the number is quoted there,
// and a silent drift makes those docs wrong rather than stale.
const eventStructSize = 456

func TestEventStructSize(t *testing.T) {
	if got := unsafe.Sizeof(store.Event{}); got != eventStructSize {
		t.Errorf("unsafe.Sizeof(store.Event{}) = %d, want %d\n"+
			"A field was added, removed or reordered. The ring reserves "+
			"capacity*this many bytes at startup, so store.maxEvents "+
			"guidance in docs/configuration.md changes with it.", got, eventStructSize)
	}
}

// ruleLabels mirrors a real router: a modest fixed set of rules doing the
// logging, not a unique label per event. The store keeps a per-label
// counter map, so a unique-label workload would measure that map's growth
// rather than the ring's.
var ruleLabels = []string{
	"lan-wan", "wan-in", "drop-invalid", "r13", "port-scan",
	"dns-out", "ntp-out", "mgmt-ssh", "guest-isolate", "icmp-limit",
}

// typicalLine is a representative RouterOS forward-chain log line with
// every field the parser recognises populated. Addresses and ports vary
// per event so no two events share backing bytes, which is what happens
// on a real feed.
func typicalLine(i int) string {
	return fmt.Sprintf(
		"%s|%s|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:%02x, "+
			"proto TCP (SYN), 192.168.%d.%d:%d->203.0.113.%d:443, len 60",
		"A", ruleLabels[i%len(ruleLabels)], i%256,
		(i/256)%256, i%256, 1024+i%60000, i%256)
}

// longLine pads the same shape out with a long interface name, standing
// in for VLAN/bridge naming conventions that produce much longer lines
// than the sample above without being anywhere near the wire limit.
func longLine(i int) string {
	pad := strings.Repeat("vlan-guest-isolated-", 12) // ~240 bytes
	return fmt.Sprintf(
		"%s|%s|forward: in:%s out:%s, connection-state:new src-mac aa:bb:cc:dd:ee:%02x, "+
			"proto TCP (SYN), 192.168.%d.%d:%d->203.0.113.%d:443, len 60",
		"A", ruleLabels[i%len(ruleLabels)], pad, pad, i%256,
		(i/256)%256, i%256, 1024+i%60000, i%256)
}

// worstCaseLine is the largest line the TCP listener will accept
// (internal/syslog.maxTCPMessageBytes, 64KB). Parse clamps every
// extracted field to 256 bytes but deliberately keeps Raw verbatim, so
// this is the real per-event upper bound -- the reason "maxEvents bounds
// memory" holds for typical traffic but is not a hard guarantee.
func worstCaseLine(i int) string {
	const maxTCPMessageBytes = 64 * 1024
	head := typicalLine(i)
	return head + strings.Repeat("x", maxTCPMessageBytes-len(head))
}

// eventFrom assembles a store.Event from a log line exactly as main.go's
// ingest path does, minus the lookups that add no bytes of their own
// (device/geo/name resolution return either "" or strings shared across
// every event, so they do not scale with capacity).
func eventFrom(line string) store.Event {
	p := routeros.Parse(line)
	return store.Event{
		Time:         time.Now(),
		DeviceID:     "core-router",
		SourceIP:     "192.168.1.1",
		Action:       p.Action,
		RuleLabel:    p.RuleLabel,
		Chain:        p.Chain,
		InInterface:  p.InInterface,
		OutInterface: p.OutInterface,
		ConnState:    p.ConnState,
		Protocol:     p.Protocol,
		SrcMAC:       p.SrcMAC,
		SrcIP:        p.SrcIP,
		SrcPort:      p.SrcPort,
		DstIP:        p.DstIP,
		DstPort:      p.DstPort,
		NatIP:        p.NatIP,
		NatPort:      p.NatPort,
		NatRaw:       p.NatRaw,
		Length:       p.Length,
		Flags:        p.Flags,
		Raw:          p.Raw,
	}
}

// bytesPerEvent fills a full ring and reports the heap it retains,
// divided by capacity. Both readings are taken after a GC so that the
// intermediate garbage (the generated lines, the parser's working
// strings) is excluded and only what the ring still holds is counted.
func bytesPerEvent(capacity int, line func(int) string) float64 {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	s := store.New(capacity, time.Hour)
	for i := 0; i < capacity; i++ {
		s.Insert(eventFrom(line(i)))
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	// Without this the store is unreachable by the second reading and the
	// GC above is entitled to have collected the very thing being measured.
	runtime.KeepAlive(s)

	return float64(after.HeapAlloc-before.HeapAlloc) / float64(capacity)
}

// TestRetainedBytesPerEvent measures what a full ring actually costs per
// event, for line lengths spanning typical traffic to the wire limit.
//
// The assertions are deliberately loose bounds, not exact figures: heap
// accounting varies with allocator size classes and Go version, and a
// test that pins it to the byte would fail for reasons that tell an
// operator nothing. The logged values are the point -- run with -v to
// read them.
func TestRetainedBytesPerEvent(t *testing.T) {
	cases := []struct {
		name      string
		capacity  int
		line      func(int) string
		min, max  float64
		lineBytes int
	}{
		{"typical", 200_000, typicalLine, 500, 1_200, len(typicalLine(0))},
		{"long", 100_000, longLine, 800, 2_000, len(longLine(0))},
		{"worst-case-64KB", 1_000, worstCaseLine, 60_000, 80_000, len(worstCaseLine(0))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bytesPerEvent(tc.capacity, tc.line)
			// heap is the part that is not the struct: one allocation
			// holding the raw line, which every string field slices into.
			// It tracks the line length rounded up to an allocator size
			// class, which is what makes the documented cost a formula
			// (struct + rounded line) rather than a measured constant.
			t.Logf("%s: %.0f bytes/event retained = %d struct + %.0f heap "+
				"(line %d bytes, capacity %d) -- %.1f MB at 200k",
				tc.name, got, eventStructSize, got-eventStructSize,
				tc.lineBytes, tc.capacity, got*200_000/(1024*1024))
			if got < tc.min || got > tc.max {
				t.Errorf("%s: %.0f bytes/event, outside the expected %.0f-%.0f range",
					tc.name, got, tc.min, tc.max)
			}
		})
	}
}

// TestPerEventCostIsIndependentOfCapacity checks that the per-event figure
// is a property of the event rather than of the run, so quoting a single
// bytes-per-event number and multiplying by capacity is legitimate.
func TestPerEventCostIsIndependentOfCapacity(t *testing.T) {
	small := bytesPerEvent(50_000, typicalLine)
	large := bytesPerEvent(200_000, typicalLine)

	t.Logf("50k capacity: %.0f bytes/event; 200k capacity: %.0f bytes/event", small, large)

	diff := small - large
	if diff < 0 {
		diff = -diff
	}
	if pct := diff / large * 100; pct > 5 {
		t.Errorf("per-event cost moved %.1f%% between capacities (%.0f vs %.0f bytes/event); "+
			"the cost is not linear in capacity, so a single figure cannot be documented", pct, small, large)
	}
}

// residentBytes reads the process's resident set size from /proc, i.e.
// the number an operator actually sees in `docker stats` or `top`.
// Returns 0 where /proc is unavailable.
func residentBytes() uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line) // "VmRSS:", "<n>", "kB"
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// rssChildEnv, when set, makes the test binary do nothing but fill a ring
// and print its resident memory. RSS only ever grows within a process --
// Go returns freed pages to the OS lazily, if at all -- so measuring it
// after the other tests in this file have already allocated hundreds of
// megabytes measures their leftovers, not the ring. A clean child process
// is the only way to get a figure worth publishing.
const rssChildEnv = "MIKROVIEW_RSS_MEASUREMENT_CHILD"

// TestResidentMemoryTracksTheRing records the gap between Go's heap
// accounting and the process's resident memory. Heap figures alone
// understate what an operator must provision: the Go runtime, the binary
// and allocator overhead are all in RSS and none of them are in
// HeapAlloc, and RSS is what `docker stats` and `top` report.
//
// The ratio is environment-dependent, so it is logged for the docs rather
// than asserted; the assertion is only that the ring is actually resident.
func TestResidentMemoryTracksTheRing(t *testing.T) {
	if residentBytes() == 0 {
		t.Skip("no /proc/self/status on this platform")
	}

	if os.Getenv(rssChildEnv) != "" {
		rssChildMain()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestResidentMemoryTracksTheRing$", "-test.v")
	cmd.Env = append(os.Environ(), rssChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child measurement failed: %v\n%s", err, out)
	}

	var heapMB, rssMB, baselineMB float64
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "RSS-MEASUREMENT ") {
			if _, err := fmt.Sscanf(line, "RSS-MEASUREMENT baseline=%f heap=%f rss=%f",
				&baselineMB, &heapMB, &rssMB); err != nil {
				t.Fatalf("could not read child output %q: %v", line, err)
			}
		}
	}
	if rssMB == 0 {
		t.Fatalf("child produced no measurement:\n%s", out)
	}

	ringMB := float64(rssMeasurementCapacity*eventStructSize) / (1024 * 1024)
	t.Logf("capacity %d filled in a clean process: baseline RSS %.1f MB, heap %.1f MB, RSS %.1f MB "+
		"(ring alone %.1f MB) -- RSS is %.2fx the heap, %.2fx the ring",
		rssMeasurementCapacity, baselineMB, heapMB, rssMB, ringMB, rssMB/heapMB, rssMB/ringMB)

	if rssMB < ringMB {
		t.Errorf("RSS %.1f MB is below the ring's own %.1f MB -- the measurement is not capturing the ring",
			rssMB, ringMB)
	}
}

const rssMeasurementCapacity = 200_000

// rssChildMain runs in the child process: fill a full ring, then report
// heap and resident memory. Output is parsed by the parent above.
func rssChildMain() {
	runtime.GC()
	baseline := residentBytes()

	s := store.New(rssMeasurementCapacity, time.Hour)
	for i := 0; i < rssMeasurementCapacity; i++ {
		s.Insert(eventFrom(typicalLine(i)))
	}

	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss := residentBytes()
	runtime.KeepAlive(s)

	const mb = 1024 * 1024
	fmt.Printf("RSS-MEASUREMENT baseline=%.1f heap=%.1f rss=%.1f\n",
		float64(baseline)/mb, float64(ms.HeapAlloc)/mb, float64(rss)/mb)
}

// TestEmptyRingCostsItsFullCapacity is the operator-facing surprise worth
// pinning: an idle mikroview holding zero events still reserves the whole
// ring. maxEvents is a memory reservation made at startup, not a limit
// approached over time.
func TestEmptyRingCostsItsFullCapacity(t *testing.T) {
	const capacity = 500_000

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	s := store.New(capacity, time.Hour)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)

	got := after.HeapAlloc - before.HeapAlloc
	want := uint64(capacity) * eventStructSize
	t.Logf("empty ring of %d: %d bytes reserved (%.1f MB), %d events held",
		capacity, got, float64(got)/(1024*1024), s.Stats().Count)

	// Within 10% either way: the allocator may round the backing array up,
	// and the two readings straddle a GC that can free unrelated heap, so
	// the delta lands slightly either side of the exact product.
	if got < want-want/10 || got > want+want/10 {
		t.Errorf("empty ring reserved %d bytes, expected about %d (capacity * %d)",
			got, want, eventStructSize)
	}
}
