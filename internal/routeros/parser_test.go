// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/store"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want Parsed
	}{
		{
			name: "RouterOS topic tag literally embedded ahead of the prefix",
			msg:  "firewall,info A|r21| forward: in:mgnt out:dmz, connection-state:new, proto TCP (SYN), 10.0.0.5:51234->1.2.3.4:443, len 60",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "r21", Chain: "forward",
				InInterface: "mgnt", OutInterface: "dmz",
				ConnState: "new",
				Protocol:  "TCP", Flags: "SYN",
				SrcIP: "10.0.0.5", SrcPort: 51234,
				DstIP: "1.2.3.4", DstPort: 443,
				Length: 60,
			},
		},
		{
			name: "accept tcp with prefix and src-mac",
			msg:  "A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "lan-wan", Chain: "forward",
				InInterface: "ether1", OutInterface: "bridge1",
				ConnState: "new", SrcMAC: "aa:bb:cc:dd:ee:ff",
				Protocol: "TCP", Flags: "SYN",
				SrcIP: "192.168.1.50", SrcPort: 51234,
				DstIP: "1.2.3.4", DstPort: 443,
				Length: 60,
			},
		},
		{
			name: "drop invalid, no src-mac, out unknown",
			msg:  "D|invalid|input: in:ether1 out:(unknown 0), connection-state:invalid, proto TCP (RST,ACK), 203.0.113.9:443->192.168.1.1:51234, len 40",
			want: Parsed{
				Action: store.ActionDrop, RuleLabel: "invalid", Chain: "input",
				InInterface: "ether1", OutInterface: "(unknown 0)",
				ConnState: "invalid",
				Protocol:  "TCP", Flags: "RST,ACK",
				SrcIP: "203.0.113.9", SrcPort: 443,
				DstIP: "192.168.1.1", DstPort: 51234,
				Length: 40,
			},
		},
		{
			name: "reject icmp - comma inside parens must not split fields",
			msg:  "R|no-match|forward: in:ether1 out:bridge1, connection-state:new, proto ICMP (type 8, code 0), 192.168.1.50->8.8.8.8, len 84",
			want: Parsed{
				Action: store.ActionReject, RuleLabel: "no-match", Chain: "forward",
				InInterface: "ether1", OutInterface: "bridge1",
				ConnState: "new",
				Protocol:  "ICMP", Flags: "type 8, code 0",
				SrcIP: "192.168.1.50", SrcPort: 0,
				DstIP: "8.8.8.8", DstPort: 0,
				Length: 84,
			},
		},
		{
			name: "no log-prefix at all - unknown action, chain still parsed",
			msg:  "forward: in:bridge1 out:ether1, connection-state:new src-mac 11:22:33:44:55:66, proto UDP, 192.168.1.20:53212->1.1.1.1:53, len 52",
			want: Parsed{
				Action: store.ActionUnknown, RuleLabel: "", Chain: "forward",
				InInterface: "bridge1", OutInterface: "ether1",
				ConnState: "new", SrcMAC: "11:22:33:44:55:66",
				Protocol: "UDP",
				SrcIP:    "192.168.1.20", SrcPort: 53212,
				DstIP: "1.1.1.1", DstPort: 53,
				Length: 52,
			},
		},
		{
			name: "bracketed ipv6 with ports",
			msg:  "A|v6-test|forward: in:ether1 out:ether2, connection-state:new, proto TCP (SYN), [2001:db8::1]:443->[2001:db8::2]:51234, len 60",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "v6-test", Chain: "forward",
				InInterface: "ether1", OutInterface: "ether2",
				ConnState: "new",
				Protocol:  "TCP", Flags: "SYN",
				SrcIP: "2001:db8::1", SrcPort: 443,
				DstIP: "2001:db8::2", DstPort: 51234,
				Length: 60,
			},
		},
		{
			name: "log-only action code",
			msg:  "L|audit|input: in:ether1, proto TCP (SYN), 10.0.0.1:1234->10.0.0.2:22, len 60",
			want: Parsed{
				Action: store.ActionLog, RuleLabel: "audit", Chain: "input",
				InInterface: "ether1",
				Protocol:    "TCP", Flags: "SYN",
				SrcIP: "10.0.0.1", SrcPort: 1234,
				DstIP: "10.0.0.2", DstPort: 22,
				Length: 60,
			},
		},
		{
			// srcnat/masquerade: translated source address is parenthesized
			// mid-annotation, original dst repeated at the end. The parser
			// doesn't assume this shape -- it diffs against the already-
			// parsed main tuple to find the address that changed.
			name: "srcnat with parenthesized translated source",
			msg:  "A|masq|srcnat: in:ether1 out:ether2, proto UDP, 10.0.0.3:51258->1.1.1.1:53, NAT (203.0.113.10:51258->1.1.1.1:53), len 73",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "masq", Chain: "srcnat",
				InInterface: "ether1", OutInterface: "ether2",
				Protocol: "UDP",
				SrcIP:    "10.0.0.3", SrcPort: 51258,
				DstIP: "1.1.1.1", DstPort: 53,
				NatIP: "203.0.113.10", NatPort: 51258,
				NatRaw: "(203.0.113.10:51258->1.1.1.1:53)",
				Length: 73,
			},
		},
		{
			// dstnat/port-forward: translated destination address trails
			// the annotation instead of leading it -- a different shape
			// from the srcnat case above, still resolved by diffing.
			name: "dstnat with trailing translated destination",
			msg:  "A|dnat|dstnat: in:ether1 out:ether2, proto TCP (SYN), 203.0.113.5:51234->203.0.113.10:8080, NAT 203.0.113.5:51234->(10.0.0.5:8080), len 60",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "dnat", Chain: "dstnat",
				InInterface: "ether1", OutInterface: "ether2",
				Protocol: "TCP", Flags: "SYN",
				SrcIP: "203.0.113.5", SrcPort: 51234,
				DstIP: "203.0.113.10", DstPort: 8080,
				NatIP: "10.0.0.5", NatPort: 8080,
				NatRaw: "203.0.113.5:51234->(10.0.0.5:8080)",
				Length: 60,
			},
		},
		{
			// A truncated line missing the closing ")" on the proto detail
			// must not swallow every field after it (see splitTopLevel):
			// the address pair and length should still parse even though
			// the ICMP detail itself is incomplete.
			name: "unterminated proto parenthetical still yields later fields",
			msg:  "A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new, proto ICMP (type 8, code 0, 10.0.0.5->1.2.3.4, len 84",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "lan-wan", Chain: "forward",
				InInterface: "ether1", OutInterface: "bridge1",
				ConnState: "new",
				Protocol:  "ICMP", Flags: "type 8",
				SrcIP:  "10.0.0.5",
				DstIP:  "1.2.3.4",
				Length: 84,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.msg)
			got.Raw = "" // not compared field-by-field below
			tt.want.Raw = ""
			if got != tt.want {
				t.Errorf("Parse() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

// TestParseRealRouterLines uses log lines captured verbatim from a real
// RouterOS 7.23.3 CHR (#273, scripts/live-routeros.sh), rather than
// lines written to exercise the parser.
//
// The distinction earns its own test. Every other case in this file was
// written alongside the code that reads it, so the two agree with each
// other by construction and cannot disagree with RouterOS. The ICMP case
// above assumes a comma after connection-state, because that is what TCP
// lines have -- and a real router does not put one there for ICMP. It
// left Protocol empty and ConnState holding "new proto ICMP (type 8,
// code 0)", which no isTrackableConnState check matches, so ICMP was
// silently invisible to the watchlist and its inverted entries.
//
// Add to this table only by pasting what a router actually printed.
func TestParseRealRouterLines(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want Parsed
	}{
		{
			// The comma RouterOS omits before proto on an ICMP line.
			name: "output chain, icmp, no comma after connection-state",
			msg:  "firewall,info A|live-out| output: in:(unknown 0) out:ether1, connection-state:new proto ICMP (type 8, code 0), 10.0.2.15->203.0.113.9, len 56",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "live-out", Chain: "output",
				InInterface: "(unknown 0)", OutInterface: "ether1",
				ConnState: "new",
				Protocol:  "ICMP", Flags: "type 8, code 0",
				SrcIP: "10.0.2.15", DstIP: "203.0.113.9",
				Length: 56,
			},
		},
		{
			name: "output chain, icmp unreachable, related state",
			msg:  "firewall,info A|live-out| output: in:(unknown 0) out:lo, connection-state:related proto ICMP (type 3, code 1), 192.168.88.1->192.168.88.1, len 84",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "live-out", Chain: "output",
				InInterface: "(unknown 0)", OutInterface: "lo",
				ConnState: "related",
				Protocol:  "ICMP", Flags: "type 3, code 1",
				SrcIP: "192.168.88.1", DstIP: "192.168.88.1",
				Length: 84,
			},
		},
		{
			// src-mac glued on the same way, which already worked -- kept
			// so the generalised handling cannot regress it. Note the
			// upper-case MAC: that is how RouterOS writes one, and
			// comparing it byte for byte against a lower-case one is what
			// matchlog.Identity.identityKey now avoids.
			name: "forward chain, dst-nat, real upper-case src-mac",
			msg:  "firewall,info A|lan-wan| forward: in:ether1 out:ether1, connection-state:new,dnat src-mac 52:55:0A:00:02:02, proto TCP (SYN), 172.17.0.1:33202->203.0.113.9:9999, NAT 172.17.0.1:33202->(10.0.2.15:15903->203.0.113.9:9999), len 44",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "lan-wan", Chain: "forward",
				InInterface: "ether1", OutInterface: "ether1",
				ConnState: "new,dnat", SrcMAC: "52:55:0A:00:02:02",
				Protocol: "TCP", Flags: "SYN",
				SrcIP: "172.17.0.1", SrcPort: 33202,
				DstIP: "203.0.113.9", DstPort: 9999,
				NatIP: "10.0.2.15", NatPort: 15903,
				NatRaw: "172.17.0.1:33202->(10.0.2.15:15903->203.0.113.9:9999)",
				Length: 44,
			},
		},
		{
			// The input chain carries src-mac on this firmware, which
			// parser.go's own comment says only forward reliably does.
			// Nothing depends on input lacking it, but the claim should
			// not outlive the evidence for it.
			name: "input chain also carries src-mac",
			msg:  "firewall,info A|live-in| input: in:ether1 out:(unknown 0), connection-state:new src-mac 52:55:0A:00:02:02, proto TCP (SYN), 172.17.0.1:55134->10.0.2.15:15902, len 44",
			want: Parsed{
				Action: store.ActionAccept, RuleLabel: "live-in", Chain: "input",
				InInterface: "ether1", OutInterface: "(unknown 0)",
				ConnState: "new", SrcMAC: "52:55:0A:00:02:02",
				Protocol: "TCP", Flags: "SYN",
				SrcIP: "172.17.0.1", SrcPort: 55134,
				DstIP: "10.0.2.15", DstPort: 15902,
				Length: 44,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.msg)
			got.Raw = ""
			tt.want.Raw = ""
			if got != tt.want {
				t.Errorf("Parse() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestSplitTopLevel(t *testing.T) {
	got := splitTopLevel("proto ICMP (type 8, code 0), 1.1.1.1->2.2.2.2, len 84", ", ")
	want := []string{"proto ICMP (type 8, code 0)", "1.1.1.1->2.2.2.2", "len 84"}
	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Every extracted field is attacker-authored -- whoever can reach the
// syslog port chooses these bytes -- and they do not stay inside
// mikroview's own UI, where Svelte's escaping would be the whole answer.
// They become a flag's Target and Detail, and from there they reach
// flags.json, the watchlist match log, an SMTP body and a Pushover
// message. A terminal rendering `cat flags.json` executes an ANSI escape
// sequence.
//
// The length cap was already here; this asserts the other half. See #285.
func TestParseStripsTerminalEscapesFromExtractedFields(t *testing.T) {
	const esc = "\x1b[2J\x1b[1;31m"
	p := Parse("D|" + esc + "rule| forward: in:" + esc + "ether1 out:ether2, " +
		"src-mac aa:bb:cc:" + esc + "dd:ee:ff, proto TCP (" + esc + "SYN), " +
		"192.0.2.1:1234->198.51.100.1:80, len 60")

	fields := map[string]string{
		"RuleLabel":   p.RuleLabel,
		"InInterface": p.InInterface,
		"SrcMAC":      p.SrcMAC,
		"Flags":       p.Flags,
	}
	for name, got := range fields {
		if strings.ContainsRune(got, 0x1b) {
			t.Errorf("%s = %q -- an escape sequence reached a field that is persisted and sent in notifications", name, got)
		}
		if got == "" {
			t.Errorf("%s is empty -- sanitising must replace the unsafe runes, not discard the value", name)
		}
	}

	// Raw is deliberately untouched: it is the verbatim evidence an
	// operator compares a row against, and rewriting it would defeat the
	// reason it is kept.
	if !strings.ContainsRune(p.Raw, 0x1b) {
		t.Error("Raw was sanitised -- it must stay byte-for-byte what the router sent")
	}
}
