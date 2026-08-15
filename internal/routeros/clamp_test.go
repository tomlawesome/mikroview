// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"reflect"
	"strings"
	"testing"
)

// clampedStringFields returns every string field of p that Parse's clamp
// is supposed to cover, keyed by field name -- every string field of
// Parsed except Raw, discovered by reflection rather than hand-enumerated.
//
// A hand-enumerated list is exactly how Flags escaped the clamp the first
// time: parseProto set it from an unbounded parenthetical (parser.go's
// parseProto), and clampAll's hand-written field-by-field list simply
// never mentioned it (#369). Reflecting over the struct means a field
// added to Parsed in the future is covered automatically -- it does not
// depend on whoever adds it also remembering to update a second list.
func clampedStringFields(p Parsed) map[string]string {
	fields := map[string]string{}
	v := reflect.ValueOf(p)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Name == "Raw" || f.Type.Kind() != reflect.String {
			continue
		}
		fields[f.Name] = v.Field(i).String()
	}
	return fields
}

// assertFieldsClamped is the shared invariant behind both the
// example-based tests below and FuzzParse (fuzz_test.go): no field
// clampedStringFields returns may exceed maxFieldLen. Sharing this
// between the example test and the fuzz target means both enforce the
// identical contract, rather than the fuzz target checking a
// hand-copied approximation of what the example test checks.
func assertFieldsClamped(t testing.TB, p Parsed, input string) {
	t.Helper()
	for name, v := range clampedStringFields(p) {
		if len(v) > maxFieldLen {
			t.Errorf("%s is %d bytes, want <= %d -- an unauthenticated line must not mint unbounded strings (input %q)",
				name, len(v), maxFieldLen, input)
		}
	}
}

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
	assertFieldsClamped(t, p, line)

	// Raw is deliberately exempt: it's already bounded by the listeners'
	// own read limits and is the verbatim evidence for investigation.
	if len(p.Raw) != len(line) {
		t.Errorf("Raw was truncated (%d vs %d); it must stay verbatim", len(p.Raw), len(line))
	}
}

// TestFlagsFieldIsClamped is the regression test for #369: parseProto
// sets Flags from proto's parenthetical detail (e.g. "SYN" or "type 8,
// code 0"), which is exactly as attacker-controlled and exactly as
// unbounded as every other field clampAll covers -- it was simply
// missing from clampAll's hand-written field list, reopening the same
// 107x memory overrun #285 finding 5 fixed for Raw.
//
// The payload sits inside balanced parens with no top-level comma
// inside them, so splitTopLevel takes its paren-aware path rather than
// falling back to the naive split -- matching how the finding was
// reproduced end-to-end through the real ingest path.
func TestFlagsFieldIsClamped(t *testing.T) {
	long := strings.Repeat("a", 65000)
	line := "A|r1|forward: in:ether1 out:bridge1, proto TCP (" + long +
		"), 192.168.1.5:1024->203.0.113.9:443, len 60"

	p := Parse(line)
	if len(p.Flags) > maxFieldLen {
		t.Errorf("Flags is %d bytes, want <= %d -- proto's parenthetical detail is attacker-controlled and must be clamped like every other extracted field (#369)",
			len(p.Flags), maxFieldLen)
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
	if p.Flags != "SYN" {
		t.Errorf("Flags = %q, want SYN", p.Flags)
	}
}
