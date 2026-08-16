// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"strings"

	"github.com/tomlawesome/mikroview/internal/store"
)

// stripPrefix decodes mikroview's own RouterOS log-prefix convention:
// "<ACTION>|<rule-slug>|<rest of message>", e.g. "A|lan-wan|forward: in:...".
//
// The trailing '|' is a deliberate self-delimiting terminator: RouterOS
// concatenates the configured log-prefix directly onto the message with no
// guaranteed separating space, so without a hard terminator there would be
// no reliable way to tell where the prefix ends and the chain name begins.
//
// Lines that don't match the convention (third-party rules, or a bare
// log=yes with no prefix) are passed through unchanged with action/label
// left empty — the caller falls back to store.ActionUnknown.
func stripPrefix(msg string) (action store.Action, label string, rest string) {
	if len(msg) < 2 || msg[1] != '|' {
		return "", "", msg
	}
	a, ok := actionFromCode(msg[0])
	if !ok {
		return "", "", msg
	}
	tail := msg[2:]
	end := strings.IndexByte(tail, '|')
	if end < 0 {
		return "", "", msg
	}
	return a, tail[:end], strings.TrimSpace(tail[end+1:])
}

// actionFromCode maps the one-letter action code in mikroview's
// log-prefix convention to an Action.
//
// A/D/R/L are the filter-table verdicts and have been here since the
// convention existed. M and N extend it to the two rule kinds that
// produce log lines without deciding a packet's fate, and which
// therefore had nowhere to go but "unknown" (#437):
//
//	M -- mangle mark rule (mark-connection / mark-routing / mark-packet)
//	N -- NAT rule (masquerade / src-nat / dst-nat / redirect / netmap)
//
// This is where the classification of a mangle rule *has* to come from.
// A mangle log line is byte-for-byte the shape of a filter line -- same
// chain names, same fields, no mention of the action or the mark -- so
// there is nothing in it to read the answer off. The operator declaring
// it in the log-prefix is the only honest source, exactly as it already
// is for accept-versus-drop.
//
// An unrecognised code is not an error and not a guess: stripPrefix
// passes the whole line through untouched, and the action stays unknown.
func actionFromCode(c byte) (store.Action, bool) {
	switch c {
	case 'A':
		return store.ActionAccept, true
	case 'D':
		return store.ActionDrop, true
	case 'R':
		return store.ActionReject, true
	case 'L':
		return store.ActionLog, true
	case 'M':
		return store.ActionMarked, true
	case 'N':
		return store.ActionNatted, true
	default:
		return "", false
	}
}
