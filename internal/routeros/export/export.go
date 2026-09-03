// SPDX-License-Identifier: AGPL-3.0-only

// Package export parses the text RouterOS's `/export hide-sensitive`
// produces, for #435's "Tune logging" helper and, per that issue's own
// decision record, #895's later scheduled-config-backup reader -- one
// parser, built once, shared by both rather than each growing its own.
//
// Every source line is kept verbatim (Export.Lines); only the
// /ip firewall filter section's `add` lines are decoded into
// attributes, which is all #435 needs from an export. Everything else
// -- interface lists, addresses, NAT, services, users -- passes through
// as opaque text, untouched.
//
// A value for any of a fixed set of secret-shaped keys (password,
// pre-shared keys, and their kin) is refused outright: RouterOS's own
// hide-sensitive export omits those keys' values, so their presence
// means the text handed in is a plain, unredacted /export instead, and
// this package will not carry that further.
package export

// Rule is one /ip firewall filter add line, decoded.
//
// Index is the 0-based ordinal among the filter section's add lines in
// this export -- stable for the same text, and (for a config with no
// dynamic filter rules, which /export never emits) the same number
// RouterOS's own `numbers=` addressing means. Line and LineEnd are the
// 1-based line numbers of the statement's first and last physical line
// in the source text, continuation lines included -- Line is what a
// caller shows an operator ("line 41"); the pair is what Export.Render
// uses to replace exactly this rule's own lines and nothing else.
type Rule struct {
	Index   int
	Line    int
	LineEnd int

	Chain            string
	Action           string
	Comment          string
	InInterface      string
	OutInterface     string
	InInterfaceList  string
	OutInterfaceList string
	Log              bool
	LogPrefix        string
	Disabled         bool

	// tokens is every key=value attribute this rule's add line carried,
	// in file order, with each value exactly as it appeared (quotes and
	// escaping included). Render reuses these verbatim for every
	// attribute except log/log-prefix, which is what keeps a rendered
	// rule's diff scoped to its logging attributes -- the invariant
	// #435's issue body states outright.
	tokens []ruleToken
}

type ruleToken struct {
	key string
	raw string
}

// Fingerprint is a rule's identity for a logging-only-diff check: every
// attribute Render's log/log-prefix mutation might touch, and nothing
// else. Two rules with equal fingerprints differ, if at all, only in
// whether they log and what they log as -- which is exactly the
// question POST /api/tune-logging/render's mechanical enforcement
// (parse before and after, strip logging, compare) needs answered, and
// a plain struct comparison (==) answers it.
type Fingerprint struct {
	Chain            string
	Action           string
	Comment          string
	InInterface      string
	OutInterface     string
	InInterfaceList  string
	OutInterfaceList string
	Disabled         bool
}

// Fingerprint returns r's comparable identity -- see Fingerprint's own
// doc comment.
func (r Rule) Fingerprint() Fingerprint {
	return Fingerprint{
		Chain:            r.Chain,
		Action:           r.Action,
		Comment:          r.Comment,
		InInterface:      r.InInterface,
		OutInterface:     r.OutInterface,
		InInterfaceList:  r.InInterfaceList,
		OutInterfaceList: r.OutInterfaceList,
		Disabled:         r.Disabled,
	}
}

// Export is one parsed RouterOS export.
type Export struct {
	// Lines is the source text split on "\n", each element still
	// carrying a trailing "\r" if the source used CRLF line endings --
	// so strings.Join(Lines, "\n") always reproduces the exact input
	// text, byte for byte, when nothing has been edited.
	Lines []string
	// Version is read from the header comment RouterOS's export always
	// opens with ("# .../.../... HH:MM:SS by RouterOS 7.24.1 ..."); ""
	// if no such comment was found.
	Version string
	// FilterRules is every /ip firewall filter add line, in file order
	// (which is also Index order).
	FilterRules []Rule
}

// Text renders e back to its source text. Called with no edits applied
// (i.e. straight from Parse), it is byte-identical to whatever text was
// parsed -- Lines is never rewritten, only ever spliced around by
// Render.
func (e *Export) Text() string {
	return joinLines(e.Lines)
}

// LoggingOnlyDiff reports whether after differs from before only in
// filter rules' log and log-prefix attributes. This is the mechanical
// guarantee behind Tune logging (#435): the lines outside the filter
// rules must be the same lines in the same order, the rule count must
// match, and each rule's other tokens must match key by key and byte by
// byte in their original order. A rule's own physical layout is not
// compared -- Render joins a wrapped rule's continuation lines when it
// rewrites the rule, which changes nothing RouterOS reads. Fingerprint
// is deliberately not used here: it names the attributes the coverage
// lens reads, not every attribute a rule carries, and a renderer that
// dropped dst-port from a rule would slip past a Fingerprint comparison.
func LoggingOnlyDiff(before, after *Export) bool {
	if before == nil || after == nil {
		return false
	}
	if len(before.FilterRules) != len(after.FilterRules) {
		return false
	}
	for i := range before.FilterRules {
		if !sameTokensIgnoringLogging(before.FilterRules[i].tokens, after.FilterRules[i].tokens) {
			return false
		}
	}
	b, a := linesOutsideRules(before), linesOutsideRules(after)
	if len(b) != len(a) {
		return false
	}
	for i := range b {
		if b[i] != a[i] {
			return false
		}
	}
	return true
}

// linesOutsideRules returns e's lines with every filter rule's span
// (Line..LineEnd, 1-based inclusive) removed, in order.
func linesOutsideRules(e *Export) []string {
	inRule := make([]bool, len(e.Lines))
	for _, r := range e.FilterRules {
		for l := r.Line; l <= r.LineEnd && l >= 1 && l <= len(inRule); l++ {
			inRule[l-1] = true
		}
	}
	out := make([]string, 0, len(e.Lines))
	for i, line := range e.Lines {
		if !inRule[i] {
			out = append(out, line)
		}
	}
	return out
}

func isLoggingKey(key string) bool { return key == "log" || key == "log-prefix" }

func sameTokensIgnoringLogging(a, b []ruleToken) bool {
	var fa, fb []ruleToken
	for _, t := range a {
		if !isLoggingKey(t.key) {
			fa = append(fa, t)
		}
	}
	for _, t := range b {
		if !isLoggingKey(t.key) {
			fb = append(fb, t)
		}
	}
	if len(fa) != len(fb) {
		return false
	}
	for i := range fa {
		if fa[i] != fb[i] {
			return false
		}
	}
	return true
}
