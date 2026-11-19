// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// secretKey is one config.yaml setting that is, or points at, something
// secret.
type secretKey struct {
	// Key is the full dotted yaml path.
	Key string
	// Path is true when the setting holds the path to a file with the
	// secret in it rather than the secret itself. Those are not masked:
	// a path is not the secret, and the operator needs to see which file
	// is meant.
	Path bool
	// Map is true when the setting is a map whose every value is secret
	// (notify.webhook.headers, usually an Authorization credential):
	// each value is masked under "<key>.<map key>".
	Map bool
}

// secretKeys is the one list of config.yaml settings that hold a secret
// or the path to one (#1347). Before it the knowledge lived only in
// per-field comments on config.go and two checks in validate.go; the
// config editor's masking reads it, and so should anything else that
// ever prints config.
//
// TestSecretKeysExist holds every entry to a real field, so a rename in
// config.go cannot leave a secret unmasked here without a failing test.
var secretKeys = []secretKey{
	{Key: "reputation.abuseIPDBKey"},
	{Key: "notify.smtp.password"},
	{Key: "notify.pushover.token"},
	// The Pushover user/group key is borderline -- it identifies rather
	// than authenticates -- but with the app token it is enough to send
	// as the operator, so it is masked with it.
	{Key: "notify.pushover.user"},
	{Key: "notify.webhook.headers", Map: true},
	{Key: "oidc.clientSecret"},
	{Key: "postgres.dsnFile", Path: true},
	{Key: "tls.keyFile", Path: true},
	{Key: "history.keyFile", Path: true},
	{Key: "auth.recoveryPepperPath", Path: true},
	{Key: "auth.recoveryKeysPath", Path: true},
}

// secretPlaceholderPattern matches one mask. The key part is limited to
// what a dotted config path (and an HTTP header name) can contain, so a
// placeholder can never swallow surrounding YAML.
var secretPlaceholderPattern = regexp.MustCompile(`<<secret:([A-Za-z0-9_.\-]+)>>`)

// SecretPlaceholder is the mask MaskSecrets writes in place of key's value.
func SecretPlaceholder(key string) string { return "<<secret:" + key + ">>" }

// SecretPlaceholders lists the keys of every mask still in text, sorted
// and without repeats -- after UnmaskSecrets, any left are ones there
// was no value for.
func SecretPlaceholders(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range secretPlaceholderPattern.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// MaskSecrets replaces the value of every secret-holding setting in text
// with its placeholder, "<<secret:oidc.clientSecret>>", leaving every
// other byte of the file -- comments, quoting, spacing -- as it was.
//
// values maps each masked key to the value exactly as it was written in
// the file, quotes included, so UnmaskSecrets puts back the original
// text rather than a re-encoding of it; a block scalar ("password: |")
// is kept whole, its lines included. Empty values are left alone: there
// is nothing to hide, and a placeholder would turn "not set" into "set".
//
// Text that does not parse is returned unchanged with no values: masking
// needs to know where each value is, and guessing on a broken file could
// leave a secret in the clear under a line that merely looks masked.
func MaskSecrets(text string) (string, map[string]string) {
	values := map[string]string{}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return text, values
	}
	lines := splitLines(text)

	for _, s := range secretSpans(&doc, lines) {
		first := []rune(lines[s.line])
		last := []rune(lines[s.endLine])
		var raw strings.Builder
		if s.line == s.endLine {
			raw.WriteString(string(first[s.col:s.endCol]))
		} else {
			raw.WriteString(string(first[s.col:]))
			for l := s.line + 1; l < s.endLine; l++ {
				raw.WriteString("\n" + lines[l])
			}
			raw.WriteString("\n" + string(last[:s.endCol]))
		}
		values[s.key] = raw.String()
		merged := string(first[:s.col]) + SecretPlaceholder(s.key) + string(last[s.endCol:])
		lines = append(lines[:s.line], append([]string{merged}, lines[s.endLine+1:]...)...)
	}
	out := joinLines(lines)
	if !strings.HasSuffix(text, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out, values
}

// keyedSpan is one secret setting's value and the placeholder key it is
// masked under.
type keyedSpan struct {
	key string
	textSpan
}

// secretSpans is where the value of each secret-holding setting
// (secretKeys, Path ones excluded) sits in lines, sorted from the end of
// the file backwards so earlier positions stay valid while later ones are
// rewritten. Only a scalar at one of those settings counts: a mask
// anywhere else -- a comment, another setting, a key -- is not a secret's
// place.
func secretSpans(doc *yaml.Node, lines []string) []keyedSpan {
	var spans []keyedSpan
	add := func(key string, v *yaml.Node) {
		if v.Kind != yaml.ScalarNode || v.Value == "" || v.Tag == "!!null" {
			return
		}
		if s, ok := scalarSpan(lines, v); ok {
			spans = append(spans, keyedSpan{key: key, textSpan: s})
		}
	}
	for _, sk := range secretKeys {
		if sk.Path {
			continue
		}
		v := lookupNode(doc, sk.Key)
		if v == nil {
			continue
		}
		if sk.Map {
			if v.Kind != yaml.MappingNode {
				continue
			}
			for i := 0; i+1 < len(v.Content); i += 2 {
				add(sk.Key+"."+v.Content[i].Value, v.Content[i+1])
			}
			continue
		}
		add(sk.Key, v)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].line != spans[j].line {
			return spans[i].line > spans[j].line
		}
		return spans[i].col > spans[j].col
	})
	return spans
}

// UnmaskSecrets puts each value back in place of its placeholder, but
// only where the placeholder stands as the whole value of the secret
// setting it names -- the one place MaskSecrets would mask it again. A
// placeholder anywhere else (a comment, a non-secret setting, a key, a
// quoted string) is left as the literal text: a value put there would
// come back out of MaskSecrets in the clear, and Carry forward, which
// needs no unlock, hands its text back. Text that does not parse is
// returned unchanged, placeholders and all, for the same reason: with no
// positions there is no safe place. A placeholder with no value is left
// as it is, for the caller to refuse (see SecretPlaceholders): writing
// it out would ship a file whose secret is the literal text of a mask.
func UnmaskSecrets(text string, values map[string]string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return text
	}
	lines := splitLines(text)
	for _, s := range secretSpans(&doc, lines) {
		if s.line != s.endLine {
			continue // a placeholder is one token on one line
		}
		row := []rune(lines[s.line])
		if string(row[s.col:s.endCol]) != SecretPlaceholder(s.key) {
			continue
		}
		v, ok := values[s.key]
		if !ok {
			continue
		}
		// v may hold newlines (a block scalar); joinLines carries them.
		lines[s.line] = string(row[:s.col]) + v + string(row[s.endCol:])
	}
	out := joinLines(lines)
	if !strings.HasSuffix(text, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// lookupNode finds the value node at a dotted path in doc, nil when any
// step is missing or is not a mapping.
func lookupNode(doc *yaml.Node, path string) *yaml.Node {
	n := doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	for _, part := range strings.Split(path, ".") {
		if n.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == part {
				next = n.Content[i+1]
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// textSpan is where a scalar's source text sits: 0-based lines, rune
// columns, end exclusive.
type textSpan struct {
	line, col, endLine, endCol int
}

// scalarSpan finds the source text of the scalar v in lines, quotes
// included. The yaml package reports where a node starts but not where it ends,
// so the end is found by reading the text the way YAML does for each
// style. ok is false when the text at v's position is not what v says it
// is, which is the signal to leave it alone rather than cut in the wrong
// place.
func scalarSpan(lines []string, v *yaml.Node) (textSpan, bool) {
	l, c := v.Line-1, v.Column-1
	if l < 0 || l >= len(lines) {
		return textSpan{}, false
	}
	row := []rune(lines[l])
	if c < 0 || c >= len(row) {
		return textSpan{}, false
	}
	switch {
	case v.Style&yaml.DoubleQuotedStyle != 0:
		if row[c] != '"' {
			return textSpan{}, false
		}
		for i := c + 1; i < len(row); i++ {
			if row[i] == '\\' {
				i++
				continue
			}
			if row[i] == '"' {
				return textSpan{l, c, l, i + 1}, true
			}
		}
		return textSpan{}, false // a quoted string folded over lines: leave it
	case v.Style&yaml.SingleQuotedStyle != 0:
		if row[c] != '\'' {
			return textSpan{}, false
		}
		for i := c + 1; i < len(row); i++ {
			if row[i] == '\'' {
				if i+1 < len(row) && row[i+1] == '\'' {
					i++
					continue
				}
				return textSpan{l, c, l, i + 1}, true
			}
		}
		return textSpan{}, false
	case v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		// The indicator line and every following line indented deeper
		// than the key (or blank) belong to the value.
		keyIndent := len(row) - len([]rune(strings.TrimLeft(string(row), " ")))
		end := l
		for i := l + 1; i < len(lines); i++ {
			t := lines[i]
			if strings.TrimSpace(t) == "" {
				continue
			}
			indent := len(t) - len(strings.TrimLeft(t, " "))
			if indent <= keyIndent {
				break
			}
			end = i
		}
		return textSpan{l, c, end, len([]rune(lines[end]))}, true
	default:
		want := []rune(v.Value)
		if c+len(want) > len(row) || string(row[c:c+len(want)]) != v.Value {
			return textSpan{}, false // a plain scalar folded over lines, or anything unexpected
		}
		return textSpan{l, c, l, c + len(want)}, true
	}
}
