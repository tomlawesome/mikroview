package routeros

import (
	"strings"
	"testing"
)

// TestParsedFieldsAreLengthCapped is the regression test for an
// amplification vector on the unauthenticated syslog path. Every string
// field here comes from a log line that anything able to reach the
// syslog port fully controls, up to the TCP listener's 64KB line limit.
//
// Uncapped, a single crafted line put a 40,000-byte "MAC address" into
// a flag's Target *and* Detail, into detector map keys, into the
// persisted flags file, and into every notification raised about it --
// proven before this cap existed.
func TestParsedFieldsAreLengthCapped(t *testing.T) {
	long := strings.Repeat("a", 40000)
	line := "A|" + long + "|forward: in:" + long + " out:" + long +
		", connection-state:new src-mac " + long +
		", proto " + long + ", 1.2.3.4:80->5.6.7.8:443, len 60"

	p := Parse(line)

	fields := map[string]string{
		"RuleLabel":    p.RuleLabel,
		"Chain":        p.Chain,
		"InInterface":  p.InInterface,
		"OutInterface": p.OutInterface,
		"ConnState":    p.ConnState,
		"Protocol":     p.Protocol,
		"SrcMAC":       p.SrcMAC,
		"SrcIP":        p.SrcIP,
		"DstIP":        p.DstIP,
		"NatIP":        p.NatIP,
		"NatRaw":       p.NatRaw,
	}
	for name, v := range fields {
		if len(v) > maxFieldLen {
			t.Errorf("%s is %d bytes, want <= %d -- an unauthenticated line must not mint unbounded strings",
				name, len(v), maxFieldLen)
		}
	}

	// Raw is deliberately exempt: it's already bounded by the listeners'
	// own read limits and is the verbatim evidence for investigation.
	if len(p.Raw) != len(line) {
		t.Errorf("Raw was truncated (%d vs %d); it must stay verbatim", len(p.Raw), len(line))
	}
}

// TestNormalFieldsAreNotTruncated: the cap must be invisible to real
// traffic. 256 bytes is far above any legitimate value.
func TestNormalFieldsAreNotTruncated(t *testing.T) {
	line := "A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60"
	p := Parse(line)
	if p.RuleLabel != "lan-wan" {
		t.Errorf("RuleLabel = %q, want lan-wan", p.RuleLabel)
	}
	if p.SrcMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("SrcMAC = %q, want the full MAC", p.SrcMAC)
	}
	if p.InInterface != "ether1" || p.OutInterface != "bridge1" {
		t.Errorf("interfaces = %q / %q", p.InInterface, p.OutInterface)
	}
}
