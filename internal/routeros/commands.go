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
		fmt.Sprintf(`/tool fetch url="https://%s/ca.crt" check-certificate=no dst-path=mikroview-ca.crt`, address),
		`/certificate import file-name=mikroview-ca.crt passphrase=""`,
	}, "\n")
}

// SyslogCommands is step 2: point the router's logging at mikroview,
// over the configured syslog port rather than an assumed one.
func SyslogCommands(address, syslogPort, dialect string) string {
	host := Hostname(address)
	port := PortOf(syslogPort)
	return strings.Join([]string{
		fmt.Sprintf(`/system logging action add name=mikroview target=remote remote=%s remote-port=%s remote-protocol=tls check-certificate=yes`, host, port),
		`/system logging add topics=firewall,info action=mikroview`,
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
func RuleTaggingCommands(dialect string) string {
	return strings.Join([]string{
		`/ip firewall filter set [find where !dynamic action=drop] log=yes log-prefix="D|drop|"`,
		`/ip firewall filter set [find where !dynamic action=reject] log=yes log-prefix="R|reject|"`,
		`/ip firewall filter set [find where !dynamic action=accept] log=yes log-prefix="A|accept|"`,
		``,
		`# The established/related accept rule logs every packet, not every`,
		`# connection -- that is your whole traffic volume. Turn it back off:`,
		`/ip firewall filter set [find connection-state=established,related] log=no log-prefix=""`,
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
	// counter whether or not the rule logs, so the tune-logging helper
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
		// routerosVersion rides the payload rather than a record: it
		// describes the router, not a row of any table (#408 carrying
		// #436's derived version source). Optional server-side, and the
		// same line in every block.
		fmt.Sprintf(`:local %s [:serialize to=json value={"kind"="%s"; "page"=1; "pages"=1; "routerosVersion"=[/system/resource get version]; "records"=$%s}]`, payload, kind, recs),
		fmt.Sprintf(`/tool fetch url="https://%s/api/ingest/routeros" http-method=post http-data=$%s http-header-field=("Content-Type: application/json,Authorization: Bearer %s") check-certificate=yes output=none`, address, payload, token),
	}, "\n")
}

// PushScript builds the whole state-push script with the token and
// address already embedded. One block per table, each an independent
// fetch, so one failing does not stop the others.
func PushScript(address, token string, kinds []string, dialect string) string {
	var blocks []string
	for _, kind := range kinds {
		if b := PushBlock(address, token, kind, dialect); b != "" {
			blocks = append(blocks, b)
		}
	}
	return strings.Join(blocks, "\n\n")
}

// ScheduleCommands is step 4's scheduler entry: turn the pasted push
// script into a script object RouterOS runs on a timer, then run it once
// immediately so the first push does not wait for the interval to pass.
func ScheduleCommands(dialect string) string {
	return strings.Join([]string{
		`/system script add name=mv-push policy=read,test source="<paste the script above>"`,
		`/system scheduler add name=mv-push interval=20m policy=read,test on-event="/system script run mv-push"`,
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
// (the restore copy), exports the plain config (no secrets, per #394's
// reversed decision 38), pushes both to the drop box over SFTP with the
// router's own name and ingest token, and removes both local files.
// address is mikroview's own host (no port); port is the drop box's own
// port (config's backup.listen, NOT the ingest/syslog ports the other
// wizard steps use) -- device is both the SFTP username and the
// destination file stem, matching internal/backupsftp's
// kindForFilename.
func BackupScript(address, port, device, token, dialect string) string {
	return fmt.Sprintf(`/system script add name=mv-backup policy=%s source="
  /system backup save name=mv-backup dont-encrypt=yes
  /export file=mv-backup
  /tool fetch mode=sftp upload=yes address=%s port=%s user=%s password=\"%s\" src-path=mv-backup.backup dst-path=%s.backup
  /tool fetch mode=sftp upload=yes address=%s port=%s user=%s password=\"%s\" src-path=mv-backup.rsc dst-path=%s.rsc
  /file remove mv-backup.backup
  /file remove mv-backup.rsc
"`, BackupScriptPolicy, address, port, device, token, device, address, port, device, token, device)
}

// BackupScheduleCommands is step 6's scheduler entry: nightly at 03:00,
// then one run now so the first pair does not wait for the interval to
// pass -- same "run once immediately" idiom as ScheduleCommands.
func BackupScheduleCommands(dialect string) string {
	return strings.Join([]string{
		fmt.Sprintf(`/system scheduler add name=mv-backup interval=1d start-time=03:00:00 policy=%s on-event="/system script run mv-backup"`, BackupScriptPolicy),
		`/system script run mv-backup`,
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
