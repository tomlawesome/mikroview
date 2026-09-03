// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"fmt"
	"regexp"
	"strings"
)

// filterSectionPath is /ip firewall filter, normalized to its
// slash-path form -- the form isFilterSection compares every section
// header against, whichever of the two forms the export actually used.
const filterSectionPath = "/ip/firewall/filter"

// secretKeys is the fixed set from #435's design record: a value
// present for any of these means the text handed in is not
// `/export hide-sensitive` output, since hide-sensitive is documented
// to omit exactly these keys' values.
var secretKeys = map[string]bool{
	"password":            true,
	"secret":              true,
	"private-key":         true,
	"preshared-key":       true,
	"pre-shared-key":      true,
	"community":           true,
	"wpa-pre-shared-key":  true,
	"wpa2-pre-shared-key": true,
	"passphrase":          true,
	"key":                 true,
	"psk":                 true,
}

// SecretFieldError reports that Parse found a value for a key RouterOS's
// hide-sensitive export is documented to omit -- the sign that the text
// handed in is a plain, unredacted /export instead. Key and Line (the
// source's 1-based line number) name where.
type SecretFieldError struct {
	Key  string
	Line int
}

func (e *SecretFieldError) Error() string {
	return fmt.Sprintf("export: line %d sets %q, which /export hide-sensitive is documented to omit -- re-export with hide-sensitive", e.Line, e.Key)
}

// versionPattern reads the RouterOS version out of the export's header
// comment, e.g. "# 2026/09/01 10:00:00 by RouterOS 7.24.1".
var versionPattern = regexp.MustCompile(`by RouterOS (\S+)`)

// Parse reads text as a RouterOS `/export hide-sensitive` and returns
// the decoded Export, or a *SecretFieldError if a secret-shaped key
// carries a value (see secretKeys).
//
// Every line is kept verbatim in the returned Export.Lines; only
// /ip firewall filter's add lines (in either the space-separated
// "/ip firewall filter" or slash-path "/ip/firewall/filter" section
// header form) are decoded further. Everything else -- other sections,
// comments, blank lines -- passes through opaque and untouched.
func Parse(text string) (*Export, error) {
	lines := strings.Split(text, "\n")
	e := &Export{Lines: lines}

	currentSection := ""
	filterIndex := 0

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))

		if trimmed == "" {
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if e.Version == "" {
				if m := versionPattern.FindStringSubmatch(trimmed); m != nil {
					e.Version = m[1]
				}
			}
			i++
			continue
		}

		logical, lastLine := joinContinuation(lines, i)
		toks := tokenize(logical)
		if len(toks) == 0 {
			i = lastLine + 1
			continue
		}

		if err := scanForSecrets(toks, i+1); err != nil {
			return nil, err
		}

		if strings.HasPrefix(toks[0], "/") {
			currentSection = normalizeSection(logical)
			i = lastLine + 1
			continue
		}

		if currentSection == filterSectionPath && toks[0] == "add" {
			rule := parseRule(toks[1:])
			rule.Index = filterIndex
			rule.Line = i + 1
			rule.LineEnd = lastLine + 1
			e.FilterRules = append(e.FilterRules, rule)
			filterIndex++
		}

		i = lastLine + 1
	}

	return e, nil
}

// joinContinuation joins lines[start:] into one logical line, following
// RouterOS export's own continuation convention: a physical line ending
// in "\" (after stripping a trailing "\r") continues on the next line,
// whose leading indentation is stripped before joining. lastLine is the
// 0-based index of the last physical line consumed.
func joinContinuation(lines []string, start int) (logical string, lastLine int) {
	var b strings.Builder
	i := start
	for {
		content := strings.TrimRight(lines[i], "\r")
		withoutTrailingSpace := strings.TrimRight(content, " \t")
		continues := strings.HasSuffix(withoutTrailingSpace, `\`)

		piece := withoutTrailingSpace
		if continues {
			piece = strings.TrimSuffix(withoutTrailingSpace, `\`)
		}
		piece = strings.TrimSpace(piece)
		if piece != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(piece)
		}

		if !continues || i+1 >= len(lines) {
			return b.String(), i
		}
		i++
	}
}

// normalizeSection turns a section header's logical line -- either
// "/ip firewall filter" or "/ip/firewall/filter" -- into the
// slash-path form, so a caller need only ever compare against one
// shape.
func normalizeSection(logical string) string {
	return strings.ReplaceAll(strings.TrimSpace(logical), " ", "/")
}

// scanForSecrets checks every key=value token in toks against
// secretKeys, refusing the whole parse on the first hit whose value is
// non-empty. An empty value (RouterOS's own redaction, or simply an
// unset property) is not a hit -- see secretKeys' doc comment.
func scanForSecrets(toks []string, line int) error {
	for _, t := range toks {
		key, raw, ok := strings.Cut(t, "=")
		if !ok || !secretKeys[key] {
			continue
		}
		if unquote(raw) == "" {
			continue
		}
		return &SecretFieldError{Key: key, Line: line}
	}
	return nil
}

// parseRule decodes one add line's key=value tokens (with "add" itself
// already stripped) into a Rule, keeping every token's raw text for
// Render to reuse verbatim.
func parseRule(toks []string) Rule {
	var r Rule
	for _, t := range toks {
		key, raw, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		r.tokens = append(r.tokens, ruleToken{key: key, raw: raw})
		val := unquote(raw)
		switch key {
		case "chain":
			r.Chain = val
		case "action":
			r.Action = val
		case "comment":
			r.Comment = val
		case "in-interface":
			r.InInterface = val
		case "out-interface":
			r.OutInterface = val
		case "in-interface-list":
			r.InInterfaceList = val
		case "out-interface-list":
			r.OutInterfaceList = val
		case "log":
			r.Log = val == "yes" || val == "true"
		case "log-prefix":
			r.LogPrefix = val
		case "disabled":
			r.Disabled = val == "yes" || val == "true"
		}
	}
	return r
}

// tokenize splits a logical line on whitespace, treating a
// double-quoted span (RouterOS's own quoting, "\\\"" and "\\\\"
// escaped) as atomic even when it contains whitespace of its own --
// e.g. `comment="lan to wan"` is one token, not three.
func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			cur.WriteByte(c)
		case c == '\\' && inQuotes && i+1 < len(s):
			cur.WriteByte(c)
			i++
			cur.WriteByte(s[i])
		case (c == ' ' || c == '\t') && !inQuotes:
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

// unquote strips a raw token value's surrounding quotes (if any) and
// unescapes \" and \\, RouterOS's own two escapes. A bare (unquoted)
// value is returned unchanged.
func unquote(raw string) string {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return raw
	}
	inner := raw[1 : len(raw)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			b.WriteByte(inner[i])
			continue
		}
		b.WriteByte(inner[i])
	}
	return b.String()
}

// Quote renders s the way RouterOS quotes a value: wrapped in double
// quotes, with '"' and '\' backslash-escaped. Exported so a caller
// building a matching `[find comment=...]` selector (POST
// /api/tune-logging/render's per-rule commands) quotes a comment value
// the same way this package does, rather than a second, possibly
// diverging implementation.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
