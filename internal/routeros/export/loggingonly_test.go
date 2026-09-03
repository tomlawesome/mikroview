package export

import "testing"

const loggingOnlyBase = `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=WAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" dst-port=443 in-interface=bridge1 \
    out-interface=ether1 protocol=tcp
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
`

func mustParse(t *testing.T, text string) *Export {
	t.Helper()
	e, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLoggingOnlyDiff(t *testing.T) {
	before := mustParse(t, loggingOnlyBase)
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"identical", loggingOnlyBase, true},
		{"log switched on", `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=WAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" dst-port=443 in-interface=bridge1 \
    out-interface=ether1 protocol=tcp log=yes log-prefix="A|accept|"
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
`, true},
		// The case Fingerprint alone cannot see: an attribute it does
		// not carry goes missing.
		{"dst-port dropped", `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=WAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" in-interface=bridge1 \
    out-interface=ether1 protocol=tcp log=yes
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
`, false},
		{"line outside the rules changed", `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=LAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" dst-port=443 in-interface=bridge1 \
    out-interface=ether1 protocol=tcp
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
`, false},
		{"rule removed", `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=WAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" dst-port=443 in-interface=bridge1 \
    out-interface=ether1 protocol=tcp
`, false},
		{"attribute value changed", `# 2026/09/01 10:00:00 by RouterOS 7.24.1
/interface list
add name=WAN
/ip firewall filter
add action=accept chain=forward comment="lan to wan" dst-port=8443 in-interface=bridge1 \
    out-interface=ether1 protocol=tcp
add action=drop chain=forward comment="wan to lan" in-interface=ether1 out-interface=bridge1
`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := mustParse(t, tc.text)
			if got := LoggingOnlyDiff(before, after); got != tc.want {
				t.Fatalf("LoggingOnlyDiff = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderPassesLoggingOnlyDiff(t *testing.T) {
	before := mustParse(t, loggingOnlyBase)
	annotated, changed := before.Render([]int{0, 1}, func(string) string { return "X|x|" })
	if changed != 2 {
		t.Fatalf("changed = %d, want 2", changed)
	}
	after := mustParse(t, annotated)
	if !LoggingOnlyDiff(before, after) {
		t.Fatalf("Render's own output failed LoggingOnlyDiff:\n%s", annotated)
	}
}
