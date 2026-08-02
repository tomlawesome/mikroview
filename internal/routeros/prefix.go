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
	default:
		return "", false
	}
}
