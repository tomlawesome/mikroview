// SPDX-License-Identifier: AGPL-3.0-only

// Ported from frontend/src/lib/setupsteps.test.ts's "generated commands"
// describe block (#436): the commands moved to this package, and these
// expectations moved with them so the port stays honest about producing
// byte-identical output for the same inputs.
package routeros

import (
	"regexp"
	"strings"
	"testing"
)

func TestHostname(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"192.0.2.10:8080", "192.0.2.10"},
		{"192.0.2.10", "192.0.2.10"},
		{"[2001:db8::1]:8080", "2001:db8::1"},
	} {
		if got := Hostname(tc.in); got != tc.want {
			t.Errorf("Hostname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPortOf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{":6514", "6514"},
		{"0.0.0.0:6514", "6514"},
	} {
		if got := PortOf(tc.in); got != tc.want {
			t.Errorf("PortOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// placeholders catches a template that leaked a <thing> marker into a
// rendered command -- the wizard must never emit one, since a saved
// script still containing <mikroview-host> fails much later, somewhere
// else. Same pattern setupsteps.test.ts checks with.
var placeholders = regexp.MustCompile(`<[a-z-]+>`)

func TestCaTrustCommands(t *testing.T) {
	cmd := CaTrustCommands("192.0.2.10:8080", "a")
	if !strings.Contains(cmd, "https://192.0.2.10:8080/ca.crt") {
		t.Errorf("caTrustCommands missing the fetch URL: %s", cmd)
	}
	if placeholders.MatchString(cmd) {
		t.Errorf("caTrustCommands leaked a placeholder: %s", cmd)
	}
	// Byte-identical to setupsteps.ts's caTrustCommands('192.0.2.10:8080').
	want := "/tool fetch url=\"https://192.0.2.10:8080/ca.crt\" check-certificate=no dst-path=mikroview-ca.crt\n" +
		"/certificate import file-name=mikroview-ca.crt passphrase=\"\""
	if cmd != want {
		t.Errorf("caTrustCommands =\n%s\nwant\n%s", cmd, want)
	}
}

func TestSyslogCommandsUsesConfiguredPort(t *testing.T) {
	if got := SyslogCommands("192.0.2.10:8080", ":16514", "a"); !strings.Contains(got, "remote-port=16514") {
		t.Errorf("syslogCommands did not honour the configured port: %s", got)
	}
}

func TestSyslogCommandsSendsHostWithoutWebPort(t *testing.T) {
	cmd := SyslogCommands("192.0.2.10:8080", ":6514", "a")
	if !strings.Contains(cmd, "remote=192.0.2.10") {
		t.Errorf("syslogCommands missing remote=host: %s", cmd)
	}
	if strings.Contains(cmd, "remote=192.0.2.10:8080") {
		t.Errorf("syslogCommands sent the web port to syslog: %s", cmd)
	}
}

func TestPushScriptEmbedsTokenInEveryBlock(t *testing.T) {
	script := PushScript("192.0.2.10:8080", "tok-123", []string{"filter-rule", "arp"}, "a")
	if n := strings.Count(script, "Bearer tok-123"); n != 2 {
		t.Errorf("pushScript embedded the token %d times, want 2: %s", n, script)
	}
	if placeholders.MatchString(script) {
		t.Errorf("pushScript leaked a placeholder: %s", script)
	}
}

func TestPushBlockRenamesFilterRuleFields(t *testing.T) {
	block := PushBlock("h", "t", "filter-rule", "a")
	for _, want := range []string{
		`"logPrefix"=($v->"log-prefix")`,
		`"srcAddressList"=($v->"src-address-list")`,
		// #408's fields. connection-state is a set, passed through as
		// the array RouterOS sends rather than joined by the script.
		`"connectionState"=($v->"connection-state")`,
		`"inInterface"=($v->"in-interface")`,
		`"outInterface"=($v->"out-interface")`,
		// The wrapping that makes it a list of records rather than one
		// merged map -- silently wrong without it.
		`{$rec}`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("pushBlock(filter-rule) missing %q:\n%s", want, block)
		}
	}
}

func TestPushScriptReportsVersionOnThePayloadNotARecord(t *testing.T) {
	script := PushScript("h", "t", []string{"filter-rule", "arp"}, "a")
	if n := strings.Count(script, `"routerosVersion"=[/system/resource get version]`); n != 2 {
		t.Errorf("pushScript carried the version marker %d times, want 2:\n%s", n, script)
	}
	// On the envelope beside kind/page/pages -- never inside the
	// per-record map, which describes a rule and not the router.
	if strings.Contains(script, `"routerosVersion"=[/system/resource get version]; "comment"`) {
		t.Errorf("pushScript put the version inside a record rather than on the payload:\n%s", script)
	}
}

func TestPushScriptGivesEachBlockItsOwnVariables(t *testing.T) {
	script := PushScript("h", "t", []string{"filter-rule", "arp"}, "a")
	if !strings.Contains(script, "ruleRecs") || !strings.Contains(script, "arpRecs") {
		t.Errorf("pushScript did not scope each block's variables:\n%s", script)
	}
}

func TestPushBlockEmitsNothingForAnUnknownKind(t *testing.T) {
	if got := PushBlock("h", "t", "not-a-kind", "a"); got != "" {
		t.Errorf("pushBlock(unknown kind) = %q, want empty", got)
	}
}

// #627: the pushed /ip/address table, same renaming contract as the
// filter-rule case above.
func TestPushBlockRenamesIPAddressFields(t *testing.T) {
	block := PushBlock("h", "t", "ip-address", "a")
	for _, want := range []string{
		"/ip/address print as-value",
		`"address"=($v->"address")`,
		`"network"=($v->"network")`,
		`"interface"=($v->"interface")`,
		`"comment"=($v->"comment")`,
		`{$rec}`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("pushBlock(ip-address) missing %q:\n%s", want, block)
		}
	}
}

func TestRuleTaggingCommandsIsFilterOnly(t *testing.T) {
	cmd := RuleTaggingCommands("a")
	want := "/ip firewall filter set [find !dynamic action=drop] log=yes log-prefix=\"D|drop|\"\n" +
		"/ip firewall filter set [find !dynamic action=reject] log=yes log-prefix=\"R|reject|\"\n" +
		"/ip firewall filter set [find !dynamic action=accept] log=yes log-prefix=\"A|accept|\"\n" +
		"\n" +
		"# The established/related accept rule logs every packet, not every\n" +
		"# connection -- that is your whole traffic volume. Turn it back off:\n" +
		"/ip firewall filter set [find connection-state=established,related] log=no log-prefix=\"\""
	if cmd != want {
		t.Errorf("ruleTaggingCommands =\n%s\nwant\n%s", cmd, want)
	}
	// Deliberately not mangle or NAT -- see the function's own doc
	// comment for why bulk-tagging those is a much worse trap.
	if strings.Contains(cmd, "mangle") || strings.Contains(cmd, "/ip firewall nat") {
		t.Errorf("ruleTaggingCommands touched mangle/NAT rules, which it must never bulk-tag: %s", cmd)
	}
}

func TestScheduleCommands(t *testing.T) {
	cmd := ScheduleCommands("a")
	want := "/system script add name=mv-push policy=read,test source=\"<paste the script above>\"\n" +
		"/system scheduler add name=mv-push interval=20m policy=read,test on-event=\"/system script run mv-push\"\n" +
		"/system script run mv-push"
	if cmd != want {
		t.Errorf("scheduleCommands =\n%s\nwant\n%s", cmd, want)
	}
}
