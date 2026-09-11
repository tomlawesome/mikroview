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
		// #435's rule counters -- the cost the tune-logging helper shows
		// beside a tick-box before any logging is switched on.
		`"packets"=($v->"packets")`,
		`"bytes"=($v->"bytes")`,
		// The wrapping that makes it a list of records rather than one
		// merged map -- silently wrong without it.
		`{$rec}`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("pushBlock(filter-rule) missing %q:\n%s", want, block)
		}
	}
}

// TestLogPrefixForAction pins the convention RuleTaggingCommands' bulk
// commands hard-code (D|drop|, R|reject|, A|accept|) plus the general
// <INITIAL>|<action>| form #435's per-rule render step needs for every
// other action RouterOS accepts.
func TestLogPrefixForAction(t *testing.T) {
	for _, tc := range []struct{ action, want string }{
		{"drop", "D|drop|"},
		{"reject", "R|reject|"},
		{"accept", "A|accept|"},
		{"tarpit", "T|tarpit|"},
		{"jump", "J|jump|"},
		{"", ""},
	} {
		if got := LogPrefixForAction(tc.action, "a"); got != tc.want {
			t.Errorf("LogPrefixForAction(%q) = %q, want %q", tc.action, got, tc.want)
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
	want := "/ip firewall filter set [find where !dynamic action=drop] log=yes log-prefix=\"D|drop|\"\n" +
		"/ip firewall filter set [find where !dynamic action=reject] log=yes log-prefix=\"R|reject|\"\n" +
		"/ip firewall filter set [find where !dynamic action=accept] log=yes log-prefix=\"A|accept|\"\n" +
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

// TestBackupScriptMatchesRound45 pins the wizard's step 6 script
// (docs/design/concepts/round-45/build.py's SCRIPT constant) byte for
// byte -- the copy is drawn, not invented, and a builder must match it
// word for word (AGENTS.md, "Building a ratified design").
func TestBackupScriptMatchesRound45(t *testing.T) {
	got := BackupScript("10.0.40.5", "47022", "rb5009", `mvt-8f3a2c…c21e`, "a")
	want := "/system script add name=mv-backup policy=read,write,test,sensitive source=\"\n" +
		"  /system backup save name=mv-backup dont-encrypt=yes\n" +
		"  /export file=mv-backup\n" +
		"  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=47022 user=rb5009 password=\\\"mvt-8f3a2c…c21e\\\" src-path=mv-backup.backup dst-path=rb5009.backup\n" +
		"  /tool fetch mode=sftp upload=yes address=10.0.40.5 port=47022 user=rb5009 password=\\\"mvt-8f3a2c…c21e\\\" src-path=mv-backup.rsc dst-path=rb5009.rsc\n" +
		"  /file remove mv-backup.backup\n" +
		"  /file remove mv-backup.rsc\n" +
		"\""
	if got != want {
		t.Errorf("BackupScript =\n%s\nwant\n%s", got, want)
	}
}

func TestBackupScheduleCommandsMatchesRound45(t *testing.T) {
	got := BackupScheduleCommands("a")
	want := "/system scheduler add name=mv-backup interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event=\"/system script run mv-backup\"\n" +
		"/system script run mv-backup"
	if got != want {
		t.Errorf("BackupScheduleCommands =\n%s\nwant\n%s", got, want)
	}
}

// TestBackupScriptEmbedsTheRealDevicePerFile guards the round-45 detail
// that the destination file names are the device's own name, not a
// fixed literal -- a second router must not collide with the first's
// generations in the vault.
func TestBackupScriptEmbedsTheRealDevicePerFile(t *testing.T) {
	got := BackupScript("10.0.40.5", "47022", "hap-ax2", "tok", "a")
	if !strings.Contains(got, "dst-path=hap-ax2.backup") || !strings.Contains(got, "dst-path=hap-ax2.rsc") {
		t.Errorf("BackupScript did not use the device name in both destination paths:\n%s", got)
	}
}

// TestQuoteEscapesBackslashAndQuote pins quote's own contract: backslash
// escaped first, then quote -- the order that keeps a value's own
// backslashes from swallowing the quote-escape that follows them.
func TestQuoteEscapesBackslashAndQuote(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		{`a\"b`, `a\\\"b`},
		{`""`, `\"\"`},
	} {
		if got := quote(tc.in); got != tc.want {
			t.Errorf("quote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestBackupPushScriptSlicesBothFilesThroughTheIngestEndpoint pins the
// #955 wire contract on the generated script: both files get a "begin"
// declaring size/slice-count, both loop /file read at the 32768
// chunk-size ceiling #394 measured, and every POST carries the same
// Bearer token PushBlock's ingest push already uses.
func TestBackupPushScriptSlicesBothFilesThroughTheIngestEndpoint(t *testing.T) {
	script := BackupPushScript("192.0.2.10:8080", "tok-123", "a")
	for _, want := range []string{
		`https://192.0.2.10:8080/api/ingest/router-backup`,
		`"op"="begin"; "kind"="backup"`,
		`"op"="begin"; "kind"="rsc"`,
		`chunk-size=$bakTake`,
		`chunk-size=$rscTake`,
		`32768`,
		`"op"="slice"; "transferId"=$bakTransferId; "index"=$bakIndex; "data"=$bakData64`,
		`"op"="slice"; "transferId"=$rscTransferId; "index"=$rscIndex; "data"=$rscData64`,
		`/file remove mv-backup.backup`,
		`/file remove mv-backup.rsc`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("BackupPushScript missing %q:\n%s", want, script)
		}
	}
	if n := strings.Count(script, "Bearer tok-123"); n != 4 {
		t.Errorf("BackupPushScript embedded the token %d times, want 4 (begin+slice per file): %s", n, script)
	}
	// sha256 is never sent -- see BackupPushScript's own doc comment for
	// why: nothing measured shows RouterOS can hash a file.
	if strings.Contains(script, "sha256") {
		t.Errorf("BackupPushScript sent a sha256 field it cannot honestly compute:\n%s", script)
	}
	if placeholders.MatchString(script) {
		t.Errorf("BackupPushScript leaked a placeholder: %s", script)
	}
}

// TestBackupPushScriptGivesEachFileItsOwnVariables guards the same trap
// PushBlock's per-kind varName avoids: pasting the backup block and the
// rsc block one after the other into the same script must not redeclare
// a :local name RouterOS would refuse the second time.
func TestBackupPushScriptGivesEachFileItsOwnVariables(t *testing.T) {
	script := BackupPushScript("h", "t", "a")
	for _, v := range []string{"bakSize", "rscSize", "bakTransferId", "rscTransferId"} {
		if !strings.Contains(script, ":local "+v) {
			t.Errorf("BackupPushScript did not scope %s as its own :local:\n%s", v, script)
		}
	}
}

// TestBackupPushScheduleCommandsMatchesTheHTTPSIdiom pins
// BackupPushScheduleCommands' shape: ScheduleCommands' two-step "paste
// the script above" form (BackupPushScript is not self-contained, see
// its own doc comment), a name distinct from the SFTP script's
// mv-backup so both can coexist, and the same nightly 03:00 interval
// BackupScheduleCommands uses.
func TestBackupPushScheduleCommandsMatchesTheHTTPSIdiom(t *testing.T) {
	got := BackupPushScheduleCommands("a")
	want := "/system script add name=mv-backup-https policy=read,write,test,sensitive source=\"<paste the script above>\"\n" +
		"/system scheduler add name=mv-backup-https interval=1d start-time=03:00:00 policy=read,write,test,sensitive on-event=\"/system script run mv-backup-https\"\n" +
		"/system script run mv-backup-https"
	if got != want {
		t.Errorf("BackupPushScheduleCommands =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "name=mv-backup ") || strings.Contains(got, "name=mv-backup\"") {
		t.Errorf("BackupPushScheduleCommands collided with the SFTP script's mv-backup name: %s", got)
	}
}

// TestBackupPushScriptEscapesQuotedHost covers #1095 for the HTTPS
// variant's own quoted spot: address sits inside /tool fetch's url="...",
// same as PushBlock's ingest push, so a hostile host must come out
// escaped rather than closing that string early.
func TestBackupPushScriptEscapesQuotedHost(t *testing.T) {
	benign := BackupPushScript("192.0.2.10:8080", "tok", "a")
	tricky := BackupPushScript(`evil.example"; /system reset\`, "tok", "a")
	if !strings.Contains(tricky, `evil.example\"; /system reset\\`) {
		t.Errorf("BackupPushScript did not escape the host:\n%s", tricky)
	}
	if got, want := unescapedQuoteCount(tricky), unescapedQuoteCount(benign); got != want {
		t.Errorf("BackupPushScript unescaped quote count = %d, want %d (same structure as a benign host):\n%s", got, want, tricky)
	}
}

// TestBackupPushScriptEscapesQuotedToken is TestBackupPushScriptEscapesQuotedHost's
// twin for the token: unlike PushBlock's bare Bearer header (which
// relies on the handler's own Token validation), this function quotes
// token itself -- AGENTS.md's "validated and quoted, never interpolated
// raw" rule -- so a hostile token cannot break out of the
// http-header-field string either.
func TestBackupPushScriptEscapesQuotedToken(t *testing.T) {
	benign := BackupPushScript("192.0.2.10:8080", "tok-123", "a")
	tricky := BackupPushScript("192.0.2.10:8080", `tok"; /system reset\`, "a")
	if !strings.Contains(tricky, `Bearer tok\"; /system reset\\`) {
		t.Errorf("BackupPushScript did not escape the token:\n%s", tricky)
	}
	if got, want := unescapedQuoteCount(tricky), unescapedQuoteCount(benign); got != want {
		t.Errorf("BackupPushScript unescaped quote count = %d, want %d (same structure as a benign token):\n%s", got, want, tricky)
	}
}

// unescapedQuoteCount counts the '"' runes in s that are not part of a
// \" escape sequence -- the quotes that actually matter to the RouterOS
// parser reading the surrounding block, as opposed to ones a builder has
// escaped out of the way. Used below to check that a builder quoting a
// value never changes how many structurally-significant quotes its
// output carries, regardless of what the value itself contains.
func unescapedQuoteCount(s string) int {
	s = strings.ReplaceAll(s, `\\`, "")
	s = strings.ReplaceAll(s, `\"`, "")
	return strings.Count(s, `"`)
}

// TestCaTrustCommandsEscapesQuotedAddress covers #1095: address lands
// inside CaTrustCommands' only quoted string (the fetch url=), so a
// value carrying '"' or '\' must come out escaped rather than closing
// that string early.
func TestCaTrustCommandsEscapesQuotedAddress(t *testing.T) {
	benign := CaTrustCommands("192.0.2.10:8080", "a")
	tricky := CaTrustCommands(`evil.example"; /system reset\`, "a")
	if !strings.Contains(tricky, `evil.example\"; /system reset\\`) {
		t.Errorf("CaTrustCommands did not escape the address:\n%s", tricky)
	}
	if got, want := unescapedQuoteCount(tricky), unescapedQuoteCount(benign); got != want {
		t.Errorf("CaTrustCommands unescaped quote count = %d, want %d (same structure as a benign address):\n%s", got, want, tricky)
	}
}

// TestPushBlockEscapesQuotedAddress covers #1095's other quoted-address
// spot: the /tool fetch url= in the ingest push block.
func TestPushBlockEscapesQuotedAddress(t *testing.T) {
	benign := PushBlock("192.0.2.10:8080", "tok", "arp", "a")
	tricky := PushBlock(`evil.example"; /system reset\`, "tok", "arp", "a")
	if !strings.Contains(tricky, `evil.example\"; /system reset\\`) {
		t.Errorf("PushBlock did not escape the address:\n%s", tricky)
	}
	if got, want := unescapedQuoteCount(tricky), unescapedQuoteCount(benign); got != want {
		t.Errorf("PushBlock unescaped quote count = %d, want %d (same structure as a benign address):\n%s", got, want, tricky)
	}
}

// TestBackupScriptEscapesQuotedToken covers #1095's password=\"...\"
// spot: BackupScript's hand-written quote wrapper around token must
// route through the same escaping quote gives every other quoted value,
// so a token carrying '"' or '\' cannot break out of it.
func TestBackupScriptEscapesQuotedToken(t *testing.T) {
	benign := BackupScript("10.0.40.5", "47022", "rb5009", "tok-123", "a")
	tricky := BackupScript("10.0.40.5", "47022", "rb5009", `tok"; /system reset\`, "a")
	if !strings.Contains(tricky, `password=\"tok\"; /system reset\\\"`) {
		t.Errorf("BackupScript did not escape the token:\n%s", tricky)
	}
	if got, want := unescapedQuoteCount(tricky), unescapedQuoteCount(benign); got != want {
		t.Errorf("BackupScript unescaped quote count = %d, want %d (same structure as a benign token):\n%s", got, want, tricky)
	}
}
