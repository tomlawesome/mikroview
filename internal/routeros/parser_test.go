package routeros

import (
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
