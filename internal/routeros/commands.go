// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"fmt"
	"strings"
)

// The RouterOS command templates the setup wizard renders (#320), moved
// here from frontend/src/lib/setupsteps.ts by #436 so the API can render
// them server-side (#435 needs them there regardless) and the frontend
// stops holding RouterOS command syntax at all.
//
// Every one of these takes a dialect, even though mikroview holds
// exactly one today ("a", see dialects.go) -- the parameter is the seam
// a second dialect would use, not a currently-live switch. Every command
// is emitted with the operator's real values already in it: the wizard
// never renders a placeholder, since a saved script still containing
// `<mikroview-host>` was one of the failures that prompted this feature
// in the first place, and it fails much later, somewhere else.

// quote escapes s for use inside a RouterOS double-quoted string:
// backslash first, then quote, so an already-escaped backslash cannot
// swallow the quote-escape that follows it (#1095). Every value this
// package places inside a quoted string goes through this rather than
// its own hand-rolled escaping, so there is exactly one place the rule
// can be wrong.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// QuoteScriptString escapes s for the inside of any RouterOS
// double-quoted string that RouterOS itself may still interpret --
// quote()'s backslash-then-quote rule, plus `$` as `\$`, because
// RouterOS expands `$name` and `$[cmd]` inside a double-quoted string
// wherever one appears, not only inside a script's own source="..."
// body. Exported for internal/droplist (#1224 hardening, security
// review): the .rsc feed places an operator-authored entry reason
// inside a RouterOS comment="..." on an /import line, and without this
// a reason of `blocked $[/user add name=x]` would run as a command the
// moment the router imports the generated script, not merely display
// as text. This is that one place the escaping rule lives, rather than
// a second copy of it in a package that has to trust it stays in step.
func QuoteScriptString(s string) string {
	return strings.ReplaceAll(quote(s), `$`, `\$`)
}

// scriptSource escapes a whole script body for the inside of a
// `source="..."` value: QuoteScriptString's backslash/quote/dollar
// rule -- a push script is nothing but `$v`, `$alRecs` and their kin,
// and unescaped, RouterOS would save a script with every variable
// already substituted away to nothing.
//
// Newlines are left as they are: BackupScript's multi-line
// source="..." is the proven form, run end to end against a real CHR
// under #394, so a script body keeps its own line breaks rather than
// being folded into `\n` escapes.
func scriptSource(body string) string {
	return QuoteScriptString(body)
}

// scriptAdd wraps a script body in the `/system script add` that saves
// it under name, so what the wizard hands over is one block the
// operator pastes into the terminal as it stands (#1131). The form it
// replaced printed the body in one box and
// `source="<paste the script above>"` in another, which asked the
// operator to nest one clipboard inside another and to do the escaping
// this function does.
//
// Guarded by a find, same idiom and same reason as SyslogCommands'
// action line (#1266): docs/routeros-setup.md promises re-pasting a
// wizard block is safe and changes nothing on a router that is already
// correct. An unguarded add broke that promise -- re-pasting step 4 or
// step 6b after a wizard version bump left the router with a second
// mv-push or mv-backup-https script rather than the new one replacing
// the old.
func scriptAdd(name, policy, body string) string {
	source := scriptSource(body)
	return fmt.Sprintf(`:if ([:len [/system script find name=%s]] = 0) do={ /system script add name=%s policy=%s source="%s" } else={ /system script set [find name=%s] policy=%s source="%s" }`, name, name, policy, source, name, policy, source)
}

// schedulerAdd wraps a `/system scheduler add` in the same guarded
// idiom scriptAdd uses for a script: add only when no entry of that
// name exists yet, otherwise set the existing one to match. settings is
// everything after `name=<name>` -- interval, start-time, policy and
// on-event -- passed identically to both branches so a re-paste
// converges an existing entry rather than leaving it as it was.
//
// Guarded for the same reason scriptAdd is (#1266): an unguarded add
// left a second mv-push, mv-backup or mv-backup-https scheduler entry
// on the router when a wizard block was pasted a second time, so the
// script it runs fired twice as often as intended rather than merely
// twice at setup.
func schedulerAdd(name, settings string) string {
	return fmt.Sprintf(`:if ([:len [/system scheduler find name=%s]] = 0) do={ /system scheduler add name=%s %s } else={ /system scheduler set [find name=%s] %s }`, name, name, settings, name, settings)
}

// Hostname strips a port. Certificate names never carry one, so this is
// what tls.hosts is compared against.
func Hostname(hostPort string) string {
	// IPv6 literals arrive as [::1]:8080.
	if strings.HasPrefix(hostPort, "[") {
		end := strings.Index(hostPort, "]")
		if end == -1 {
			return hostPort
		}
		return hostPort[1:end]
	}
	colon := strings.LastIndex(hostPort, ":")
	if colon == -1 {
		return hostPort
	}
	return hostPort[:colon]
}

// PortOf takes the port out of a listen address like ":6514" or
// "0.0.0.0:6514" -- the router needs the port, not the bind address.
func PortOf(listenAddr string) string {
	colon := strings.LastIndex(listenAddr, ":")
	if colon == -1 {
		return listenAddr
	}
	return listenAddr[colon+1:]
}

// CaTrustCommands is step 1: fetch mikroview's certificate authority and
// import it, so the router will open a TLS connection to it at all.
func CaTrustCommands(address, dialect string) string {
	return strings.Join([]string{
		fmt.Sprintf(`/tool fetch url="https://%s/ca.crt" check-certificate=no dst-path=mikroview-ca.crt`, quote(address)),
		`/certificate import file-name=mikroview-ca.crt passphrase=""`,
	}, "\n")
}

// WizardVersion stamps every push block with which wizard wrote the
// script a router is running (#1241). **Bump it whenever any block this
// package pastes changes** -- a line added, removed or reworded in
// CaTrustCommands, SyslogCommands, RuleTaggingCommands, PushBlock,
// ScheduleCommands or the backup blocks. That is the whole contract: a
// router reports the number its pasted script carries, and mikroview
// compares it against this one to say whether the setup on that router
// is behind the current wizard -- without ever connecting to the router
// to look (docs/decisions/upgrade-framework.md).
//
// Deliberately not the release version. Two releases whose pasted
// blocks are identical share a wizard version, and a router is not
// behind merely because mikroview was upgraded around it.
const WizardVersion = 2

// LoggingSetup is what the current wizard's SyslogCommands leaves on a
// router, in the router's own vocabulary: the mikroview logging
// action's remote, remote-port and remote-log-format, and the topics of
// the rule that feeds it. Exactly the fields #1241 counts as drift, and
// no others -- a router may have any amount of other logging
// configuration and none of it is mikroview's business.
//
// This is the "what the current wizard would push" half of the
// comparison; internal/setup holds the reported half and does the
// comparing. An empty Remote or RemotePort means this instance does not
// know its own answer yet (no operator-set address, no syslog
// listener), in which case that field is not compared at all rather
// than compared against "".
//
// src-address is named in #1241's drift list but is absent here on
// purpose: the ratified page on #1206 carries exactly six action fields
// and src-address is not one of them, so there is nothing reported to
// compare. It joins this struct the day the page carries it, not
// before.
type LoggingSetup struct {
	Remote          string
	RemotePort      string
	RemoteLogFormat string
	Topics          []string
}

// wizardLogFormat/wizardTopics are the two constants SyslogCommands and
// WizardLogging both read, so the commands the wizard pastes and the
// setup it later checks for cannot say different things.
const (
	wizardLogFormat = "syslog"
	wizardTopics    = "firewall,info"
)

// WizardLogging is what SyslogCommands would leave on a router for this
// instance's address and syslog port -- the same two values that block
// is rendered from, read back as fields rather than as command text.
func WizardLogging(address, syslogPort, dialect string) LoggingSetup {
	return LoggingSetup{
		Remote:          Hostname(address),
		RemotePort:      PortOf(syslogPort),
		RemoteLogFormat: wizardLogFormat,
		Topics:          strings.Split(wizardTopics, ","),
	}
}

// SyslogCommands is step 2: point the router's logging at mikroview,
// over the configured syslog port rather than an assumed one.
//
// Both lines are guarded rather than bare `add` (#1208): RouterOS does
// not deduplicate, so a wizard re-run -- and an upgraded install goes
// through this more than once -- appended another action and another
// rule every time, and a router the owner had upgraded several times
// carried three identical rules, tripling every firewall event it sent.
// The rule only needs a guarded add: nothing about it changes between
// versions. The action is different -- its argument list *has* changed
// before (remote-log-format=syslog, #614/#1173) and can again -- so an
// upgrade must still reach a router whose action already exists,
// which is why the action branches to `set` on the existing one rather
// than leaving it as it was the day it was first created.
func SyslogCommands(address, syslogPort, dialect string) string {
	want := WizardLogging(address, syslogPort, dialect)
	// host and port are placed bare, not inside a quoted string -- the
	// handler validates address/syslogPort's charset before either
	// reaches here (#1095), so there is nothing for quote() to do.
	//
	// remote-log-format=syslog gives every message its own standard
	// header, so a burst of matching lines arriving at once is read as
	// separate lines rather than one garbled one (#614). Keep this
	// identical to docs/routeros-setup.md's block.
	actionArgs := fmt.Sprintf(`target=remote remote=%s remote-port=%s remote-protocol=tls remote-log-format=%s check-certificate=yes`, want.Remote, want.RemotePort, want.RemoteLogFormat)
	return strings.Join([]string{
		fmt.Sprintf(`:if ([:len [/system logging action find name=mikroview]] = 0) do={ /system logging action add name=mikroview %s } else={ /system logging action set [find name=mikroview] %s }`, actionArgs, actionArgs),
		fmt.Sprintf(`:if ([:len [/system logging find action=mikroview]] = 0) do={ /system logging add topics=%s action=mikroview }`, strings.Join(want.Topics, ",")),
	}, "\n")
}

// RuleTaggingCommands bulk-tags existing rules by action, which is the
// only way one command can set the right letter: mikroview decodes
// accept/drop/reject from the prefix, so a single generic prefix would
// label every row the same.
//
// Filter rules only, deliberately. The prefix convention also covers
// mangle (M) and NAT (N) rules -- see docs/routeros-setup.md -- but
// bulk-enabling log=yes across every mangle rule can turn a router's
// whole packet throughput into log lines, since mark-packet matches per
// packet rather than per connection. That is the established/related
// trap below, one order of magnitude worse, and it is not something to
// do to someone from a "run this" box. The doc walks it per rule.
//
// The established/related accept rule is excluded rather than switched
// back off afterwards (#1230). It used to be tagged with everything
// else and then undone by
// `set [find connection-state=established,related] log=no`, which
// relies on the whole value matching exactly: RouterOS 7's own default
// firewall writes that rule as `established,related,untracked`, so the
// undo matched nothing and said nothing, and a live router went from
// ~15 to ~1500 events/s -- every packet of every established
// connection. Measured on a CHR running 7.23.3: with both a
// `established,related,untracked` rule and a plain
// `established,related` one present, `find connection-state=...`
// returned 0 matches for either spelling. Enabling and then undoing is
// the wrong shape whatever the match string is -- one silent miss
// floods the operator's log -- so the accept line now never switches
// logging on for a rule whose connection-state mentions established or
// related at all. `~` is RouterOS's regex-match operator and it does
// work against connection-state's multi-value form; that was measured
// on the same CHR rather than assumed.
//
// Skipping those rules is not enough on its own, which is why the block
// ends by switching logging off on them. Every install that ran the old
// step 3 against a RouterOS 7 default firewall already has log=yes on
// its established/related accept rules, and a version of this block that
// only excludes them leaves that router flooding: re-running step 3
// would not repair the damage it caused. The two repair lines are one
// per term rather than one line with `or`, because two `~` predicates in
// one `find` are the form already measured on the CHR and `or` inside a
// `find where` is not; they are idempotent, and on a router that was
// never bitten they match rules that are already log=no.
func RuleTaggingCommands(dialect string) string {
	return strings.Join([]string{
		`/ip firewall filter set [find where !dynamic action=drop] log=yes log-prefix="D|drop|"`,
		`/ip firewall filter set [find where !dynamic action=reject] log=yes log-prefix="R|reject|"`,
		``,
		`# An accept rule matching established or related traffic logs every`,
		`# packet, not every connection -- that is your whole traffic volume.`,
		`# So the accept line below skips any rule whose connection-state`,
		`# mentions either, whatever else is in the list: RouterOS 7's default`,
		`# rule says established,related,untracked.`,
		`/ip firewall filter set [find where !dynamic and action=accept and !(connection-state~"established") and !(connection-state~"related")] log=yes log-prefix="A|accept|"`,
		``,
		`# Repairs a router an earlier version of this block flooded, and matches the posture in section 6 of the setup guide: these rules never log.`,
		`/ip firewall filter set [find where !dynamic and action=accept and connection-state~"established"] log=no log-prefix=""`,
		`/ip firewall filter set [find where !dynamic and action=accept and connection-state~"related"] log=no log-prefix=""`,
	}, "\n")
}

// blockSpec is one pushed table's fetch source and the record shape it
// is rewritten into -- mikroview's own field names, not RouterOS's.
type blockSpec struct {
	varName string
	source  string
	record  string
}

// blockSpecs mirrors docs/routeros-setup.md's table, itself verified
// against a real RouterOS 7.23.3 router -- see dialects.go's Rows, which
// is what the scheduled freshness check now compares its newest bound
// against (scripts/routeros-freshness.sh, #436). Changing the commands
// here without adding or updating a row leaves the two disagreeing. The
// field renaming is the one place a typo silently breaks a feature
// without RouterOS complaining, so it lives in exactly one place.
var blockSpecs = map[string]blockSpec{
	// packets/bytes were added for #435: RouterOS keeps a per-rule hit
	// counter whether or not the rule logs, so the Log every rule helper
	// can show a rule's real cost -- "fired 41,000 times in the last
	// day" -- beside its tick-box before any logging is switched on.
	"filter-rule": {
		varName: "rule",
		source:  "/ip/firewall/filter",
		record: `{"ordinal"=$i; "comment"=($v->"comment"); "chain"=($v->"chain"); "action"=($v->"action"); ` +
			`"srcAddressList"=($v->"src-address-list"); "logPrefix"=($v->"log-prefix"); "dstPort"=($v->"dst-port"); ` +
			`"protocol"=($v->"protocol"); "log"=($v->"log"); "dstAddress"=($v->"dst-address"); "srcAddress"=($v->"src-address"); ` +
			`"connectionState"=($v->"connection-state"); "inInterface"=($v->"in-interface"); "outInterface"=($v->"out-interface"); ` +
			`"disabled"=($v->"disabled"); "packets"=($v->"packets"); "bytes"=($v->"bytes")}`,
	},
	"address-list": {
		varName: "al",
		source:  "/ip/firewall/address-list",
		record:  `{"list"=($v->"list"); "address"=($v->"address"); "comment"=($v->"comment"); "dynamic"=($v->"dynamic")}`,
	},
	"dhcp-lease": {
		varName: "lease",
		source:  "/ip/dhcp-server/lease",
		record:  `{"hostname"=($v->"host-name"); "mac"=($v->"mac-address"); "address"=($v->"address")}`,
	},
	"arp": {
		varName: "arp",
		source:  "/ip/arp",
		record:  `{"address"=($v->"address"); "mac"=($v->"mac-address")}`,
	},
	"ip-address": {
		varName: "addr",
		source:  "/ip/address",
		record:  `{"address"=($v->"address"); "network"=($v->"network"); "interface"=($v->"interface"); "comment"=($v->"comment")}`,
	},
}

// PushBlock renders one table's push block: fetch every record, rewrite
// it into mikroview's field names, and POST the lot to
// /api/ingest/routeros. Returns "" for a kind blockSpecs does not know,
// so PushScript can simply skip it.
func PushBlock(address, token, kind, dialect string) string {
	if kind == string(loggingKind) {
		return loggingPushBlock(address, token, dialect)
	}
	spec, ok := blockSpecs[kind]
	if !ok {
		return ""
	}
	recs := spec.varName + "Recs"
	payload := spec.varName + "Payload"
	return strings.Join([]string{
		fmt.Sprintf(`:local %s [:toarray ""]`, recs),
		fmt.Sprintf(`:foreach i,v in=[%s print as-value] do={`, spec.source),
		fmt.Sprintf(`  :local rec %s`, spec.record),
		fmt.Sprintf(`  :set %s ($%s, {$rec})`, recs, recs),
		`}`,
		// routerosVersion and wizardVersion ride the payload rather than a
		// record: both describe the router's own setup, not a row of any
		// table (#408 carrying #436's derived version source; #1241 the
		// script stamp). Optional server-side, and the same line in every
		// block.
		fmt.Sprintf(`:local %s [:serialize to=json value={"kind"="%s"; "page"=1; "pages"=1; "routerosVersion"=[/system/resource get version]; "wizardVersion"=%d; "records"=$%s}]`, payload, kind, WizardVersion, recs),
		// address sits inside url="...", so it goes through quote();
		// token in the Bearer header is placed bare, relying on the
		// handler's Token validation to keep it well-formed (#1095).
		fmt.Sprintf(`/tool fetch url="https://%s/api/ingest/routeros" http-method=post http-data=$%s http-header-field=("Content-Type: application/json,Authorization: Bearer %s") check-certificate=yes output=none`, quote(address), payload, token),
	}, "\n")
}

// PushScript builds the whole state-push script with the token and
// address already embedded. One block per table, each an independent
// fetch, so one failing does not stop the others.
func PushScript(address, token string, kinds []string, dialect string) string {
	var blocks []string
	for _, kind := range kinds {
		// The logging block is not one of the operator's tables, and is
		// appended below whether or not it is asked for -- skipped here so
		// a caller that does name it does not get it twice.
		if kind == string(loggingKind) {
			continue
		}
		if b := PushBlock(address, token, kind, dialect); b != "" {
			blocks = append(blocks, b)
		}
	}
	// #1241's setup report goes in every push script, unconditionally:
	// it is not a table the operator chooses to send, it is the script
	// saying what the wizard left on this router and which version of
	// the wizard left it. A script that could be rendered without it
	// would be a script mikroview cannot tell is out of date, which is
	// the whole problem the page exists to solve.
	blocks = append(blocks, loggingPushBlock(address, token, dialect))
	return strings.Join(blocks, "\n\n")
}

// loggingKind is the ingest kind #1241's page arrives under. Spelled
// here rather than imported from internal/ingest: this package renders
// commands and has never imported the schema they feed.
const loggingKind = "logging"

// loggingPushBlock renders #1241's setup report: the mikroview logging
// action and every /system logging rule pointing at it, in one page of
// the same shape as the table blocks above, stamped with the wizard
// version that wrote this script.
//
// Both menus are printed whole and filtered in the script with :if,
// rather than with a `print ... where` predicate. `print as-value` and
// `:if` are the two forms already proven on a real router by every
// other block this package renders; a `where` on `print` is not, and a
// predicate that silently matched nothing would push an empty page,
// which reads exactly like a router with no mikroview logging at all.
//
// Nothing else from /system logging is sent -- not the other actions,
// not the rules feeding them. What the operator logs elsewhere is not
// mikroview's business, and the only question this page answers is
// whether the wizard's own setup is still what the wizard would write.
func loggingPushBlock(address, token, dialect string) string {
	return strings.Join([]string{
		`:local logRecs [:toarray ""]`,
		`:foreach i,v in=[/system/logging/action print as-value] do={`,
		`  :if (($v->"name") = "mikroview") do={`,
		`    :local rec {"type"="action"; "name"=($v->"name"); "target"=($v->"target"); "remote"=($v->"remote"); ` +
			`"remotePort"=($v->"remote-port"); "remoteProtocol"=($v->"remote-protocol"); ` +
			`"remoteLogFormat"=($v->"remote-log-format"); "checkCertificate"=($v->"check-certificate")}`,
		`    :set logRecs ($logRecs, {$rec})`,
		`  }`,
		`}`,
		`:foreach i,v in=[/system/logging print as-value] do={`,
		`  :if (($v->"action") = "mikroview") do={`,
		`    :local rec {"type"="rule"; "topics"=($v->"topics"); "action"=($v->"action"); "disabled"=($v->"disabled")}`,
		`    :set logRecs ($logRecs, {$rec})`,
		`  }`,
		`}`,
		fmt.Sprintf(`:local logPayload [:serialize to=json value={"kind"="%s"; "page"=1; "pages"=1; "routerosVersion"=[/system/resource get version]; "wizardVersion"=%d; "records"=$logRecs}]`, loggingKind, WizardVersion),
		fmt.Sprintf(`/tool fetch url="https://%s/api/ingest/routeros" http-method=post http-data=$logPayload http-header-field=("Content-Type: application/json,Authorization: Bearer %s") check-certificate=yes output=none`, quote(address), token),
	}, "\n")
}

// PushScriptPolicy is the policy mv-push is saved and scheduled under:
// it reads router state and posts it, and does nothing else. Named
// rather than repeated so the script and its scheduler entry cannot
// drift apart, the same reason BackupScriptPolicy exists.
const PushScriptPolicy = "read,test"

// ScheduleCommands is step 4's whole hand-over: body saved as the
// mv-push script, the scheduler entry that runs it every 20 minutes,
// and one run now so the first push does not wait for the interval to
// pass. One block, pasted into a RouterOS terminal as it stands
// (#1131) -- body is the push script PushScript built, already escaped
// for the source="..." it sits inside.
func ScheduleCommands(body, dialect string) string {
	return strings.Join([]string{
		scriptAdd("mv-push", PushScriptPolicy, body),
		schedulerAdd("mv-push", fmt.Sprintf(`interval=20m policy=%s on-event="/system script run mv-push"`, PushScriptPolicy)),
		`/system script run mv-push`,
	}, "\n")
}

// BackupScriptPolicy is the RouterOS script/scheduler policy list the
// wizard's step 6 script is printed with -- round 45's drawn
// read,write,test,sensitive, run end to end against a real CHR (7.23.3,
// #394's build note H) as both an ad-hoc `/system script run` and a
// scheduler entry with the matching policy: `/system backup save`,
// `/export`, both `/tool fetch mode=sftp` uploads and both `/file
// remove` steps all completed, and a generation landed in the vault
// each time.
//
// It is not, in the sense the build note asked, a *minimum* --
// measurement on the same CHR found the script's own declared policy is
// not enforced against its own actions at all while it is owned by an
// admin (the wizard's only realistic operator): dropping it to `test`
// alone still ran the whole script successfully. What the declared
// value actually gates is `/system script add` itself, checked against
// the *adding* user's own group, and RouterOS's own group-based check on
// whoever later runs it -- for a user genuinely restricted to this
// policy list (tested with a purpose-built RouterOS group), `/system
// backup save`/`/export` additionally refused until `policy` and `ftp`
// were added to that group, which this script does not declare. Kept at
// round 45's drawn value rather than narrowed (there is no functional
// reason to prefer a shorter string an admin owner ignores anyway) or
// widened to cover a restricted-owner scenario this issue does not ask
// for; recorded on the issue rather than silently assumed.
const BackupScriptPolicy = "read,write,test,sensitive"

// BackupScript is the wizard's step 6 script (round 45): one
// `/system script add` whose source saves the binary backup unencrypted
// (the restore copy), exports the readable config, pushes both to the
// drop box over SFTP with the router's own name and ingest token, and
// removes both local files.
//
// The export is `hide-sensitive` into its own `mv-export.rsc` (#895),
// not the plain `/export file=mv-backup` this used to run. Two reasons.
// The flag is RouterOS's own pass, and saying it outright is the
// router's half of the promise #895's ingest-time redaction makes --
// mikroview's pass behind it is a second net, not the only one. And a
// file stem of its own keeps the pair legible: `mv-backup.backup` is
// the restore copy, `mv-export.rsc` is the readable one, and neither
// name has to be read twice to work out which is which.
//
// The `.backup` is pushed first, and that ordering is load-bearing:
// backupvault.Store opens a new generation on a `.backup` and attaches
// a `.rsc` to the generation still waiting for one, so the pair shares
// a generation only in this order (#895's item 2).
// address is mikroview's own host (no port); port is the drop box's own
// port (config's backup.listen, NOT the ingest/syslog ports the other
// wizard steps use) -- device is both the SFTP username and the
// destination file stem, matching internal/backupsftp's
// kindForFilename.
func BackupScript(address, port, device, token, dialect string) string {
	// address, port, device (user=/dst-path=) and token are placed bare
	// here, inside a plain "..." string in the script body -- not yet
	// the outer source="..." this whole body still has to sit inside.
	// scriptAdd's scriptSource does that escaping now, over the body as
	// a whole, the same as every other saved script in this file; it is
	// what turns this bare password="%s" into the round-45 pin's
	// password=\"...\", not a second hand-rolled wrapper here.
	body := fmt.Sprintf(`
  /system backup save name=mv-backup dont-encrypt=yes
  /export hide-sensitive file=mv-export
  /tool fetch mode=sftp upload=yes address=%s port=%s user=%s password="%s" src-path=mv-backup.backup dst-path=%s.backup
  /tool fetch mode=sftp upload=yes address=%s port=%s user=%s password="%s" src-path=mv-export.rsc dst-path=%s.rsc
  /file remove mv-backup.backup
  /file remove mv-export.rsc
`, address, port, device, token, device, address, port, device, token, device)
	// Guarded by scriptAdd, same idiom and same reason as ScheduleCommands
	// (#1266): this used to build its own unguarded `/system script add`,
	// so re-pasting step 6 after a wizard version bump left a second
	// mv-backup script on the router rather than the new one replacing
	// the old.
	return scriptAdd("mv-backup", BackupScriptPolicy, body)
}

// BackupScheduleCommands is step 6's scheduler entry: nightly at 03:00,
// then one run now so the first pair does not wait for the interval to
// pass -- same "run once immediately" idiom as ScheduleCommands.
func BackupScheduleCommands(dialect string) string {
	return strings.Join([]string{
		schedulerAdd("mv-backup", fmt.Sprintf(`interval=1d start-time=03:00:00 policy=%s on-event="/system script run mv-backup"`, BackupScriptPolicy)),
		`/system script run mv-backup`,
	}, "\n")
}

// backupPushHTTPSBlock renders one local file's slice-push loop for
// BackupPushScript: read the file's size, POST a "begin" declaring the
// transfer, then loop /file read (RouterOS >=7.13, chunk-size capped at
// 32768 -- both measured #394) slices, each POSTed once the begin
// response's transfer id is known. v is this block's local-variable
// prefix (mirrors PushBlock's per-kind varName) so the backup block and
// the rsc block, pasted one after the other into the same script
// (BackupPushScript), never redeclare the same :local name.
//
// The three RouterOS primitives this block leans on were measured on a
// CHR running 7.23.3 (2026-09-10), because #394 and #955 had measured
// none of them and one of the three guesses was wrong:
//   - `as-value output=user` on `/tool fetch` does return the response
//     body in the result's `data` key, on a POST as well as a GET.
//   - `:deserialize from=json` parses it, in the positional form used
//     below. It is the inverse of the `:serialize to=json` this file
//     already relies on.
//   - **`:serialize to="base64"` does not exist** -- it is a syntax
//     error on 7.23.3. `:convert ... from=raw to=base64` is the
//     primitive, and it was checked for exactness rather than assumed:
//     a 36-byte text file and an 18,673-byte binary `.backup` both
//     arrived byte-identical, the server decoding what the router sent
//     and matching its SHA-256.
//
// One measured refusal has no fix here and belongs to whoever reads a
// failure log: `/tool fetch` cannot report a 401 that carries no
// `WWW-Authenticate` header. It fails with "ERROR parsing http" instead
// of the status, so an expired ingest token reads on the router as a
// malformed reply rather than as a rejected credential.
func backupPushHTTPSBlock(address, token, localFile, kind, v string) string {
	url := fmt.Sprintf(`https://%s/api/ingest/router-backup`, quote(address))
	// token is quoted here even though PushBlock's identical Bearer
	// header spot leaves it bare (relying on the handler's Token
	// validation, #1095) -- AGENTS.md's rule that templated command text
	// must be "validated and quoted, never interpolated raw" is easiest
	// to just satisfy outright in a function being written fresh, rather
	// than lean on a validator this package cannot see from here.
	header := fmt.Sprintf(`http-header-field=("Content-Type: application/json,Authorization: Bearer %s")`, quote(token))
	return strings.Join([]string{
		fmt.Sprintf(`:local %sSize [/file get %s size]`, v, localFile),
		fmt.Sprintf(`:local %sTotalSlices (($%sSize + 32767) / 32768)`, v, v),
		fmt.Sprintf(`:local %sBegin [:serialize to=json value={"op"="begin"; "kind"="%s"; "totalBytes"=$%sSize; "totalSlices"=$%sTotalSlices}]`, v, kind, v, v),
		fmt.Sprintf(`:local %sBeginResp [/tool fetch url="%s" http-method=post http-data=$%sBegin %s check-certificate=yes as-value output=user]`, v, url, v, header),
		fmt.Sprintf(`:local %sTransferId (([:deserialize from=json ($%sBeginResp->"data")])->"transferId")`, v, v),
		fmt.Sprintf(`:local %sSent 0`, v),
		fmt.Sprintf(`:local %sIndex 0`, v),
		fmt.Sprintf(`:while ($%sSent < $%sSize) do={`, v, v),
		fmt.Sprintf(`  :local %sTake ($%sSize - $%sSent)`, v, v, v),
		fmt.Sprintf(`  :if ($%sTake > 32768) do={ :set %sTake 32768 }`, v, v),
		fmt.Sprintf(`  :local %sChunk [/file read file=%s offset=$%sSent chunk-size=$%sTake as-value]`, v, localFile, v, v),
		fmt.Sprintf(`  :local %sData64 [:convert ($%sChunk->"data") from=raw to=base64]`, v, v),
		fmt.Sprintf(`  :local %sSlice [:serialize to=json value={"op"="slice"; "transferId"=$%sTransferId; "index"=$%sIndex; "data"=$%sData64}]`, v, v, v, v),
		fmt.Sprintf(`  /tool fetch url="%s" http-method=post http-data=$%sSlice %s check-certificate=yes output=none`, url, v, header),
		fmt.Sprintf(`  :set %sSent ($%sSent + $%sTake)`, v, v, v),
		fmt.Sprintf(`  :set %sIndex ($%sIndex + 1)`, v, v),
		`}`,
		fmt.Sprintf(`/file remove %s`, localFile),
	}, "\n")
}

// BackupPushScript is the HTTPS-only alternative to BackupScript (#955):
// for a deployment that can only reach mikroview over HTTPS -- no SFTP
// port open -- the router reads its own backup and export files in
// <=32KiB slices with /file read and POSTs each slice as JSON through
// the same ingest endpoint pattern and bearer token PushBlock already
// uses, rather than needing a second listener. One refused slice aborts
// the script (measured #394: a failing /tool fetch aborts at that line,
// no retry) -- the next scheduled run starts over from the beginning;
// this deliberately builds no resume logic.
//
// Like PushScript, this returns the bare script body: what makes it a
// saved script is BackupPushScheduleCommands, which wraps it through
// scriptAdd (#1131) rather than printing it in one box and a
// `source="<paste the script above>"` line in another.
//
// address is mikroview's own host (with port -- the same combined value
// CaTrustCommands/PushBlock take, not BackupScript's bare-host form);
// token is the device's ingest token. There is no device or port
// parameter, unlike BackupScript's SFTP form: the wire protocol
// identifies the device from the bearer token alone, and mikroview
// reassembles a transfer by the id its own "begin" response hands back,
// never by a destination filename.
//
// sha256 is deliberately never sent: nothing in #394's measurements or
// RouterOS's documented scripting primitives shows a way to hash a
// file's contents from a script, and the wire contract's sha256 field
// is optional precisely so a script that cannot compute one can omit
// it rather than fake it.
//
// It runs the same `/export hide-sensitive file=mv-export` the SFTP
// script does, and pushes the same two files in the same order, for
// the reasons given on BackupScript: the pair shares a vault
// generation only if the `.backup` goes first.
func BackupPushScript(address, token, dialect string) string {
	return strings.Join([]string{
		`/system backup save name=mv-backup dont-encrypt=yes`,
		`/export hide-sensitive file=mv-export`,
		backupPushHTTPSBlock(address, token, "mv-backup.backup", "backup", "bak"),
		backupPushHTTPSBlock(address, token, "mv-export.rsc", "rsc", "rsc"),
	}, "\n\n")
}

// BackupPushScheduleCommands is BackupPushScript's whole hand-over,
// the same shape ScheduleCommands has: body saved as the
// mv-backup-https script, the nightly scheduler entry, and one run now
// (#1131). Named mv-backup-https, distinct from the SFTP script's
// mv-backup, so an operator can have both set up without a name
// collision. Same nightly 03:00 interval as BackupScheduleCommands and
// the same run-once-now idiom every scheduler helper in this file uses,
// so the first push does not wait for the interval to pass.
func BackupPushScheduleCommands(body, dialect string) string {
	return strings.Join([]string{
		scriptAdd("mv-backup-https", BackupScriptPolicy, body),
		schedulerAdd("mv-backup-https", fmt.Sprintf(`interval=1d start-time=03:00:00 policy=%s on-event="/system script run mv-backup-https"`, BackupScriptPolicy)),
		`/system script run mv-backup-https`,
	}, "\n")
}

// LogPrefixForAction is the log-prefix convention RuleTaggingCommands'
// bulk `[find action=...]` commands give a rule, applied to a single
// action rather than to every rule of that action at once: D|drop|,
// R|reject|, A|accept|, and <INITIAL>|<action>| for everything else,
// INITIAL being the action's own first letter, upper-cased. #435's
// per-rule tune-logging render step (POST /api/tune-logging/render)
// calls this so a rule tagged individually gets the identical prefix
// bulk-tagging would have given it, rather than a second convention
// that could drift from the first. dialect is accepted for the same
// reason every other function in this file takes one -- the seam a
// second dialect would use, not a currently-live switch.
func LogPrefixForAction(action, dialect string) string {
	if action == "" {
		return ""
	}
	return strings.ToUpper(action[:1]) + "|" + action + "|"
}
