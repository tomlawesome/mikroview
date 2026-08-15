// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"strings"
	"testing"
)

// FuzzParse is the highest-value fuzz target in this codebase: Parse is
// reached by every syslog line mikroview receives, and the syslog
// listeners are unauthenticated by nature -- anything that can route a
// UDP packet to port 514 controls this input entirely. A panic here is
// a remote crash of the whole process, reachable without credentials.
//
// The contract asserted is deliberately narrow, because Parse is
// best-effort by design: garbage in produces a mostly-empty Parsed, not
// an error. So this checks the things that must hold for *any* input
// rather than trying to pin the parse of malformed lines:
//
//  1. It must not panic. (main.go's ingestOneRecovered does have a
//     recover(), so a panic wouldn't take the process down today -- but
//     it would silently drop the event and every event batched behind
//     it, and relying on a recover as the primary defence for a
//     network-facing parser is not a design worth resting on.)
//  2. Any port it reports must be a legal TCP/UDP port. Ports flow into
//     detector keys and critical-port comparisons, so an out-of-range
//     value is a real correctness bug, not a cosmetic one.
//  3. No extracted string field (everything but Raw) exceeds
//     maxFieldLen -- clamp_test.go's assertFieldsClamped, reflecting
//     over Parsed rather than a hand-enumerated list. This is the
//     invariant Flags escaped (#369): clampAll's field-by-field list
//     omitted it, so one crafted line put an unbounded string into every
//     downstream store and client buffer despite maxFieldLen existing.
//     A hand-enumerated check in a fuzz target has the exact same blind
//     spot a hand-enumerated clampAll does, so this asserts the
//     struct-derived version instead -- a future field with the same gap
//     fails here without needing its name added anywhere.
func FuzzParse(f *testing.F) {
	// Seeds: real-shaped lines first (so the fuzzer starts from valid
	// structure and mutates outward), then the shapes most likely to
	// break an index-arithmetic parser.
	f.Add("A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60")
	f.Add("D|wan-in|input: in:ether1 out:(unknown 0), proto UDP, 203.0.113.9:53->192.168.1.1:53, len 76")
	f.Add("R|drop-invalid|forward: proto ICMP, 10.0.0.1->10.0.0.2, len 84")
	f.Add("proto TCP (SYN), 1.2.3.4:99999->5.6.7.8:-1, len 60") // out-of-range ports
	f.Add("A||: ->:, len ")                                     // empty everything
	f.Add("A|x|y: 1.2.3.4:->5.6.7.8:, len 60")                  // colons with no port digits
	f.Add(":")
	f.Add("")
	f.Add("len 99999999999999999999999")                          // integer overflow bait
	f.Add(strings.Repeat("a:", 5000))                             // deeply repetitive, no valid structure
	f.Add("\x00\x00\x00")                                         // NUL bytes
	f.Add("A|\xff\xfe|forward: proto TCP, \xff:1->\xfe:2, len 1") // invalid UTF-8
	f.Add("A|r1|forward: in:ether1 out:bridge1, proto TCP (" + strings.Repeat("a", 65000) +
		"), 192.168.1.5:1024->203.0.113.9:443, len 60") // #369: unbounded proto-detail parenthetical into Flags

	f.Fuzz(func(t *testing.T, msg string) {
		p := Parse(msg) // must not panic

		if p.SrcPort < 0 || p.SrcPort > 65535 {
			t.Errorf("SrcPort = %d, outside the legal 0-65535 range (input %q)", p.SrcPort, msg)
		}
		if p.DstPort < 0 || p.DstPort > 65535 {
			t.Errorf("DstPort = %d, outside the legal 0-65535 range (input %q)", p.DstPort, msg)
		}
		assertFieldsClamped(t, p, msg)
	})
}
