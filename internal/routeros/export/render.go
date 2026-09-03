// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"sort"
	"strings"
)

// LogPrefixFunc computes the log-prefix a rule should get when it has
// none of its own, given its action -- routeros.LogPrefixForAction is
// the production implementation; Render takes it as a parameter rather
// than importing internal/routeros itself, so this package stays free
// of any dependency on RouterOS's command-dialect table.
type LogPrefixFunc func(action string) string

// Render returns e's text with logging switched on for every rule in
// selected (by Index), and how many rules were actually changed -- an
// id with no matching rule is silently skipped, since the caller is
// expected to have validated selected against its own earlier Analyse
// call.
//
// For each selected rule: log is set to yes; log-prefix is set to
// prefixFor(rule.Action) only when the rule's own log-prefix is
// currently empty, otherwise left exactly as it was. Every other
// attribute's original token text -- including its original quoting --
// is reused verbatim, and every line outside a selected rule's own
// Line..LineEnd range is copied through completely unchanged. That is
// what keeps the result's diff against the input scoped to logging
// attributes: Render never rewrites, reorders or reflows anything it
// was not asked to change.
func (e *Export) Render(selected []int, prefixFor LogPrefixFunc) (annotated string, changed int) {
	want := make(map[int]bool, len(selected))
	for _, id := range selected {
		want[id] = true
	}

	byIndex := make(map[int]Rule, len(e.FilterRules))
	for _, r := range e.FilterRules {
		byIndex[r.Index] = r
	}

	var edited []Rule
	for id := range want {
		if r, ok := byIndex[id]; ok {
			edited = append(edited, r)
		}
	}
	sort.Slice(edited, func(i, j int) bool { return edited[i].Line < edited[j].Line })

	var out []string
	pos := 0 // 0-based index into e.Lines, first line not yet copied
	for _, r := range edited {
		start := r.Line - 1
		out = append(out, e.Lines[pos:start]...)
		out = append(out, r.setLogging(prefixFor(r.Action)))
		pos = r.LineEnd
		changed++
	}
	out = append(out, e.Lines[pos:]...)

	return joinLines(out), changed
}

// setLogging returns r's add line rewritten with log=yes and, if r's
// own log-prefix is empty, log-prefix=prefixIfEmpty. Every other
// attribute is reused from r's original tokens verbatim, in their
// original order; log/log-prefix are appended at the end when r did
// not already carry them.
func (r Rule) setLogging(prefixIfEmpty string) string {
	toks := append([]ruleToken(nil), r.tokens...)
	logSet, prefixSet := false, false
	for i, t := range toks {
		switch t.key {
		case "log":
			toks[i].raw = "yes"
			logSet = true
		case "log-prefix":
			prefixSet = true
			if unquote(t.raw) == "" && prefixIfEmpty != "" {
				toks[i].raw = Quote(prefixIfEmpty)
			}
		}
	}
	if !logSet {
		toks = append(toks, ruleToken{key: "log", raw: "yes"})
	}
	if !prefixSet && prefixIfEmpty != "" {
		toks = append(toks, ruleToken{key: "log-prefix", raw: Quote(prefixIfEmpty)})
	}

	var b strings.Builder
	b.WriteString("add")
	for _, t := range toks {
		b.WriteByte(' ')
		b.WriteString(t.key)
		b.WriteByte('=')
		b.WriteString(t.raw)
	}
	return b.String()
}
