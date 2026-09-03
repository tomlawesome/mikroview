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
