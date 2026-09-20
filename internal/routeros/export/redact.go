// SPDX-License-Identifier: AGPL-3.0-only

package export

// The ingest-time redaction pass (#895). The router already runs
// `/export hide-sensitive`, which is RouterOS's own pass; this is
// mikroview's second net, run over the arriving text *before* anything
// parses it or writes it down. A value that gets past hide-sensitive --
// a key it does not cover, a router on a version that misses one, an
// operator's script that dropped the flag -- is replaced here, so the
// readable copy mikroview keeps never had the secret in it at all.
//
// Removal is visible, never silent: the marker sits where the value
// was, and a comment at the top of the file says how many went and on
// which lines. An operator reading a redacted export can see that
// something was there; they just cannot read it.
//
// This is not Parse's SecretFieldError, which refuses the whole text
// and belongs to #435's paste helper -- refusing a scheduled backup
// would throw away the one copy of the config that arrived. The backup
// path redacts and keeps; the paste path refuses and asks again.

import (
	"fmt"
	"strconv"
	"strings"
)

// RemovedValue is what a secret's value is replaced by: a quoted
// literal, so the redacted line is still a well-formed RouterOS token
// and still reads as "a value lived here".
const RemovedValue = `"<removed>"`

// Redaction reports what Redact took out. Lines are 1-based line
// numbers **in the returned text**, not in the text handed in: the
// marker comment occupies line 1, and a value spread over continuation
// lines is rewritten onto one, so the input's numbering is not the
// numbering whoever reads the redacted copy will be counting.
type Redaction struct {
	Count int
	Lines []int
}

// Marker is the comment Redact prepends, and is empty for a Redaction
// that removed nothing.
func (r Redaction) Marker() string {
	if r.Count == 0 {
		return ""
	}
	nums := make([]string, len(r.Lines))
	for i, n := range r.Lines {
		nums[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("# mikroview: %d secret values removed at ingest (lines %s)",
		r.Count, strings.Join(nums, ", "))
}

// Redact replaces the value of every `key=value` token whose key is in
// secretKeys, and prepends the marker comment when it removed anything.
//
// Text with nothing to remove comes back byte-identical, marker
// comment included: a file that never carried a secret is not annotated
// to say so.
//
// Continuation lines are followed the same way Parse follows them, so a
// value wrapped across two physical lines is caught whole. The one
// visible cost is that a wrapped line *that carried a secret* is
// rewritten onto a single line -- the alternative, splicing a
// replacement back across the wrap, risks leaving half a secret behind
// on the second line, which is the one outcome this pass exists to
// prevent. Lines with nothing removed are copied through untouched.
func Redact(text string) (string, Redaction) {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+1)
	var red Redaction

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, lines[i])
			i++
			continue
		}

		logical, lastLine := joinContinuation(lines, i)
		spans := secretSpans(logical)
		if len(spans) == 0 {
			out = append(out, lines[i:lastLine+1]...)
			i = lastLine + 1
			continue
		}

		var b strings.Builder
		b.WriteString(leadingBlank(lines[i]))
		prev := 0
		for _, sp := range spans {
			b.WriteString(logical[prev:sp.start])
			b.WriteString(sp.key)
			b.WriteByte('=')
			b.WriteString(RemovedValue)
			prev = sp.end
		}
		b.WriteString(logical[prev:])

		red.Count += len(spans)
		red.Lines = append(red.Lines, len(out)+1)
		out = append(out, b.String())
		i = lastLine + 1
	}

	if red.Count == 0 {
		return text, red
	}
	// Every recorded line shifts down by one, because the marker is
	// about to take line 1.
	for j := range red.Lines {
		red.Lines[j]++
	}
	return red.Marker() + "\n" + strings.Join(out, "\n"), red
}

// secretSpan is one `key=value` token Redact must rewrite: the byte
// range of the whole token within its logical line, and the key, which
// is written back out as-is.
type secretSpan struct {
	start, end int
	key        string
}

// secretSpans walks a logical line the way tokenize does -- same
// quoting rules, same escapes -- but keeps each token's byte range so a
// replacement can be spliced into the original text rather than
// rebuilt from tokens, which would lose the line's own spacing.
func secretSpans(s string) []secretSpan {
	var out []secretSpan
	inQuotes := false
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			if start < 0 {
				start = i
			}
		case c == '\\' && inQuotes && i+1 < len(s):
			if start < 0 {
				start = i
			}
			i++
		case (c == ' ' || c == '\t') && !inQuotes:
			if start >= 0 {
				if sp, ok := secretSpanAt(s, start, i); ok {
					out = append(out, sp)
				}
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		if sp, ok := secretSpanAt(s, start, len(s)); ok {
			out = append(out, sp)
		}
	}
	return out
}

// secretSpanAt decides whether one token is a secret carrying a value.
// An empty value is left alone for the same reason scanForSecrets skips
// it: `password=""` is RouterOS's own redaction or simply an unset
// property, and counting it as a removal would claim a secret was there
// when none was.
func secretSpanAt(s string, start, end int) (secretSpan, bool) {
	key, raw, ok := strings.Cut(s[start:end], "=")
	if !ok || !secretKeys[key] {
		return secretSpan{}, false
	}
	if unquote(raw) == "" {
		return secretSpan{}, false
	}
	return secretSpan{start: start, end: end, key: key}, true
}

// leadingBlank is a line's own indentation, kept so a rewritten line
// still sits where its neighbours do.
func leadingBlank(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return s[:i]
		}
	}
	return s
}
