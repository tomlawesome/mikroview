// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// ListName is the live RouterOS address-list a router's own firewall
// rules match against. StagingListName is where a fetched script builds
// the next generation before it ever touches the live list -- see
// Script's own doc comment for why the swap happens in that order.
const (
	ListName        = "mikroview-drop"
	StagingListName = "mikroview-drop-next"
)

// maxCommentRunes bounds a pushed address-list entry's comment. RouterOS
// itself has its own (looser) limit on a comment field; this cap exists
// so an operator-authored reason -- unbounded at the point it entered the
// droplist store, see internal/droplist's own validText -- cannot blow
// past whatever RouterOS enforces and abort the whole /import partway
// through, which is exactly the failure mode the staging-list swap below
// exists to be safe against.
const maxCommentRunes = 200

// entryComment renders one Entry's RouterOS comment: "mv: <reason>",
// with " [flag <id>]" appended when the entry was raised from a flag's
// drawer rather than typed in from scratch, capped at maxCommentRunes and
// only then escaped -- capping first, so a truncation can never land
// mid-escape-sequence and hand RouterOS a comment with an unbalanced
// trailing backslash.
//
// Escaped with QuoteScriptString, not the plain backslash/quote-only
// quote a comment might seem to need: RouterOS expands `$name` and
// `$[cmd]` inside any double-quoted string it parses, including a
// comment="..." on an /import line, so an unescaped reason of
// `blocked $[/user add name=x]` would run as a command the moment a
// router imports the generated script (security review, #1224
// hardening) rather than merely appear as text next to the blocked
// range.
func entryComment(e Entry) string {
	comment := "mv: " + e.Reason
	if e.FlagID != "" {
		comment += fmt.Sprintf(" [flag %s]", e.FlagID)
	}
	if utf8.RuneCountInString(comment) > maxCommentRunes {
		comment = string([]rune(comment)[:maxCommentRunes])
	}
	return routeros.QuoteScriptString(comment)
}

// scriptHash summarises entries independently of the order Script is
// asked to render them in: every entry's canonical CIDR string, sorted,
// joined one per line, SHA-256'd, first 12 hex characters. The header
// line carries this so an operator diffing two fetched scripts (or a
// router logging what it just imported) can tell whether the set of
// blocked ranges actually changed, without RouterOS itself understanding
// or checking it -- it never appears in an executable line, only in the
// leading `#` comment.
func scriptHash(entries []Entry) string {
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.CIDR.String()
	}
	sort.Strings(lines)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// Script renders the whole drop list as a RouterOS `/import`-able script
// (issue #1224): the .rsc feed a router's own scheduled `/tool fetch`
// pulls and imports. entries is rendered in the order given -- the
// caller (handleDroplistPull) passes Store.List()'s own CIDR order, so
// this need not re-sort it, only the header's own hash does (see
// scriptHash).
//
// The shape is a staging-list swap, not a direct rewrite of the live
// list, because `/import` aborts at the first line that errors and
// leaves every line after it un-run (RouterOS's own documented
// behaviour, not a guess) -- a script that cleared the live list first
// and then failed partway through adding the replacement would leave the
// router with an empty drop list, wide open, until the next successful
// fetch. So every line up to and including the last `add` touches only
// StagingListName; only the final two lines -- clear the live list, then
// rename the fully-built staging list onto it -- ever touch ListName, and
// by the time either of them runs the staging list is already complete
// or the import has already aborted before reaching them. A bad line
// anywhere above leaves the live list exactly as it was; a bad line at
// or after the swap is only possible once every entry already imported
// cleanly.
//
// Zero entries produces the header, the staging-list clear, the live
// list clear and the set -- nothing special-cased, since an empty
// entries slice simply skips the `add` loop below and still empties the
// router's live list the same way any other generation would replace it.
func Script(entries []Entry, now time.Time) []byte {
	lines := make([]string, 0, len(entries)+4)

	lines = append(lines, fmt.Sprintf("# mikroview drop list: %d entries, generated %s, sha256 %s",
		len(entries), now.UTC().Format(time.RFC3339), scriptHash(entries)))
	lines = append(lines, fmt.Sprintf("/ip firewall address-list remove [find list=%s]", StagingListName))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf(`/ip firewall address-list add list=%s address=%s comment="%s"`,
			StagingListName, e.CIDR.String(), entryComment(e)))
	}
	lines = append(lines, fmt.Sprintf("/ip firewall address-list remove [find list=%s]", ListName))
	lines = append(lines, fmt.Sprintf("/ip firewall address-list set [find list=%s] list=%s", StagingListName, ListName))

	return []byte(strings.Join(lines, "\n") + "\n")
}
