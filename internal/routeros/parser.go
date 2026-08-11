// SPDX-License-Identifier: AGPL-3.0-only

// Package routeros decodes the body of a RouterOS firewall log message
// (the part after the syslog envelope has already been stripped) into
// structured fields.
package routeros

import (
	"strconv"
	"strings"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
)

// Parsed holds the fields decoded from a single firewall log line. It
// deliberately mirrors the subset of store.Event that this package has
// enough information to fill in; the caller (which knows the device,
// receive time, etc.) assembles the full Event.
type Parsed struct {
	Action    store.Action
	RuleLabel string
	Chain     string

	InInterface  string
	OutInterface string
	ConnState    string
	Protocol     string
	SrcMAC       string

	SrcIP   string
	SrcPort int
	DstIP   string
	DstPort int

	// NatIP/NatPort/NatRaw: see store.Event's fields of the same name.
	NatIP   string
	NatPort int
	NatRaw  string

	Length int
	Flags  string

	Raw string
}

// Parse decodes a single RouterOS firewall log message body into its
// structured fields. It never errors: firewall log shape varies across
// chains, protocols, and RouterOS versions (ICMP has no ports, some chains
// omit src-mac, etc.), so this extracts whatever fields it recognizes and
// leaves the rest zero-valued rather than rejecting the line.
// maxFieldLen caps every string field Parse extracts. The parser's
// input is an unauthenticated syslog line -- anything able to reach the
// syslog port chooses these bytes, up to the 64KB line limit the TCP
// listener allows. Uncapped, a single crafted line puts a 40KB "MAC
// address" into a flag's Target and Detail, into detector map keys, into
// the persisted flags file, and into every notification sent about it.
//
// 256 is far above any real value: MACs are 17 characters, interface
// names and rule labels are RouterOS identifiers, and IPs are at most 45
// (IPv6 with an embedded IPv4). Anything longer is malformed by
// definition, so truncating loses nothing genuine -- and Raw keeps the
// untouched line regardless, so nothing is actually lost for
// investigation.
const maxFieldLen = 256

// clampField truncates s to maxFieldLen bytes. Byte-oriented rather than
// rune-oriented on purpose: the cap exists to bound memory, and a
// truncated multi-byte sequence is harmless here (these fields are
// compared and displayed, never decoded).
func clampField(s string) string {
	if len(s) > maxFieldLen {
		return s[:maxFieldLen]
	}
	return s
}

// safeField is clampField plus logging.Printable: bounded in length, and
// free of control characters, ANSI escapes and Unicode format characters.
//
// The length cap alone was not enough. These fields are attacker-authored
// -- anything able to reach the syslog port controls them entirely -- and
// they do not stay inside mikroview's own UI, where Svelte's escaping
// would be the whole answer. They become a flag's Target and Detail, and
// from there they are written into flags.json, into the watchlist match
// log, into an SMTP body and into a Pushover message. A terminal
// rendering `cat flags.json` executes an ANSI escape sequence; that is
// the CVE class this codebase already cites elsewhere, and
// logging.Printable already exists for it -- it was simply applied only
// to usernames, at seven call sites, and never on the event path.
//
// Doing it here rather than at each sink is deliberate: a sink is easy to
// add and easy to forget, and internal/watchlist/invert.go had already
// reasoned itself into writing an unvalidated address on the grounds
// that it was "already derived from a real event" -- which is the wrong
// test, since the event itself is attacker-authored. One choke point on
// the way in makes that reasoning correct rather than merely common.
//
// None of these fields can legitimately contain a control character:
// they are IPs, MAC addresses, RouterOS identifiers and protocol names.
// See #285.
func safeField(s string) string {
	return logging.Printable(clampField(s))
}

// clampAll applies safeField to every extracted string field. Raw is
// deliberately excluded -- it is already bounded by the listeners' own
// read limits, and it is the verbatim evidence an operator needs, which
// is worth exactly nothing if this rewrites it before they see it.
func (p *Parsed) clampAll() {
	p.RuleLabel = safeField(p.RuleLabel)
	p.Chain = safeField(p.Chain)
	p.InInterface = safeField(p.InInterface)
	p.OutInterface = safeField(p.OutInterface)
	p.ConnState = safeField(p.ConnState)
	p.Protocol = safeField(p.Protocol)
	p.SrcMAC = safeField(p.SrcMAC)
	p.SrcIP = safeField(p.SrcIP)
	p.DstIP = safeField(p.DstIP)
	p.NatIP = safeField(p.NatIP)
	p.NatRaw = safeField(p.NatRaw)
}

// The named return matters: clampAll runs in a defer, and with an
// unnamed return Go copies the result *before* defers execute, so the
// truncation would silently never reach the caller. Verified -- an
// unnamed return left a 40,000-byte RuleLabel intact.
func Parse(msg string) (p Parsed) {
	p = Parsed{Raw: msg}
	defer p.clampAll()
	msg = stripTopics(msg)

	action, label, rest := stripPrefix(msg)
	p.Action, p.RuleLabel = action, label
	if p.Action == "" {
		p.Action = store.ActionUnknown
	}

	chain, body, ok := strings.Cut(rest, ":")
	if ok {
		p.Chain = strings.TrimSpace(chain)
		rest = strings.TrimSpace(body)
	} else {
		rest = strings.TrimSpace(rest)
	}

	for _, seg := range splitTopLevel(rest, ", ") {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "in:"):
			parseInOut(seg, &p)
		case strings.HasPrefix(seg, "out:"):
			p.OutInterface = strings.TrimSpace(strings.TrimPrefix(seg, "out:"))
		case strings.HasPrefix(seg, "connection-state:"):
			parseConnState(seg, &p)
		case strings.HasPrefix(seg, "src-mac "):
			p.SrcMAC = strings.TrimSpace(strings.TrimPrefix(seg, "src-mac "))
		case strings.HasPrefix(seg, "proto "):
			parseProto(seg, &p)
		case strings.HasPrefix(seg, "len "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(seg, "len "))); err == nil {
				p.Length = n
			}
		case strings.HasPrefix(seg, "NAT "):
			// Must be checked before the generic "->" case below: a NAT
			// annotation also contains "->" internally, and would
			// otherwise be misparsed as (and overwrite) the main address
			// pair.
			parseNAT(seg, &p)
		case strings.Contains(seg, "->"):
			parseAddrPair(seg, &p)
		}
	}

	return p
}

// stripTopics removes a leading RouterOS topic tag (e.g. "firewall,info ")
// if the message starts with one. Whether topics are included as literal
// text in the forwarded syslog message body -- ahead of our own
// log-prefix -- turns out to depend on the router (observed on a real
// device sending "firewall,info A|r21| forward: ..."), so this can't
// assume either way; it detects the shape instead.
func stripTopics(msg string) string {
	sp := strings.IndexByte(msg, ' ')
	if sp <= 0 {
		return msg
	}
	for _, word := range strings.Split(msg[:sp], ",") {
		if !isTopicWord(word) {
			return msg
		}
	}
	return msg[sp+1:]
}

func isTopicWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func parseInOut(seg string, p *Parsed) {
	v := strings.TrimPrefix(seg, "in:")
	if in, out, found := strings.Cut(v, " out:"); found {
		p.InInterface = strings.TrimSpace(in)
		p.OutInterface = strings.TrimSpace(out)
		return
	}
	p.InInterface = strings.TrimSpace(v)
}

func parseConnState(seg string, p *Parsed) {
	v := strings.TrimPrefix(seg, "connection-state:")
	if state, mac, found := strings.Cut(v, " src-mac "); found {
		p.ConnState = strings.TrimSpace(state)
		p.SrcMAC = strings.TrimSpace(mac)
		return
	}
	p.ConnState = strings.TrimSpace(v)
}

func parseProto(seg string, p *Parsed) {
	v := strings.TrimPrefix(seg, "proto ")
	if proto, detail, found := strings.Cut(v, " ("); found {
		p.Protocol = strings.TrimSpace(proto)
		p.Flags = strings.TrimSuffix(detail, ")")
		return
	}
	p.Protocol = strings.TrimSpace(v)
}

// parseAddrPair handles "src:port->dst:port" (IPv4/TCP/UDP), bracketed
// IPv6 "[src]:port->[dst]:port", and portless pairs like ICMP's
// "src->dst".
func parseAddrPair(seg string, p *Parsed) {
	left, right, ok := strings.Cut(seg, "->")
	if !ok {
		return
	}
	p.SrcIP, p.SrcPort = splitHostPort(strings.TrimSpace(left))
	p.DstIP, p.DstPort = splitHostPort(strings.TrimSpace(right))
}

// parseNAT decodes the "NAT (...)" annotation RouterOS appends to a
// srcnat/dstnat chain's log line for a connection that has been
// translated. MikroTik doesn't document a fixed grammar for this
// annotation, and real-world examples disagree on where the translated
// address sits relative to the original tuple and any parentheses, so
// this doesn't assume a position: it extracts every address:port token in
// the segment and picks whichever one does NOT match the already-parsed
// main src/dst tuple (parseAddrPair runs on an earlier segment before
// this one, since it appears earlier in the raw line) -- that's the
// translated address, regardless of the annotation's exact shape. NatRaw
// keeps the verbatim text too, so nothing is lost if this guess is wrong.
func parseNAT(seg string, p *Parsed) {
	detail := strings.TrimSpace(strings.TrimPrefix(seg, "NAT "))
	p.NatRaw = detail

	for _, tok := range strings.Split(detail, "->") {
		tok = strings.Trim(strings.TrimSpace(tok), "()")
		if tok == "" {
			continue
		}
		ip, port := splitHostPort(tok)
		if ip == "" {
			continue
		}
		if ip == p.SrcIP && port == p.SrcPort {
			continue
		}
		if ip == p.DstIP && port == p.DstPort {
			continue
		}
		p.NatIP, p.NatPort = ip, port
	}
}

// splitHostPort handles IPv4/hostname "host:port", bracketed IPv6
// "[addr]:port" or "[addr]", and bare hosts with no port. Bare
// (unbracketed) IPv6 addresses are not reliably split from a trailing
// port, since the address itself contains colons — RouterOS brackets IPv6
// in its ip6 firewall logs, so this is not expected to occur in practice.
func splitHostPort(s string) (string, int) {
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end >= 0 {
			host := s[1:end]
			if port, ok := strings.CutPrefix(s[end+1:], ":"); ok {
				if n, ok := parsePort(port); ok {
					return host, n
				}
			}
			return host, 0
		}
	}
	if idx := strings.LastIndexByte(s, ':'); idx >= 0 {
		if n, ok := parsePort(s[idx+1:]); ok {
			return s[:idx], n
		}
	}
	return s, 0
}

// parsePort accepts only a legal TCP/UDP port. strconv.Atoi alone
// happily returns -1 or 99999, and this parser's input is entirely
// attacker-controlled -- the syslog listeners are unauthenticated, so
// anything that can reach port 514 chooses these bytes. An impossible
// port doesn't stay cosmetic: it becomes a detector key (port-scan
// distinct-port sets, critical-port comparisons) and a flag target, so
// it pollutes detection state and the UI with values no real packet
// could carry. Out-of-range reads as "no port parsed" (0), matching
// what splitHostPort already returns for input it can't parse at all.
func parsePort(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 65535 {
		return 0, false
	}
	return n, true
}

// splitTopLevel splits s on sep, but never inside parentheses — needed
// because ICMP's "proto ICMP (type 8, code 0)" contains the field
// separator ", " inside its own parenthetical detail.
func splitTopLevel(s, sep string) []string {
	if strings.Count(s, "(") != strings.Count(s, ")") {
		// Unbalanced parens (e.g. a truncated log line) would otherwise
		// leave depth stuck above 0 for the rest of the string, silently
		// merging every remaining field into one segment. Falling back to a
		// naive split loses paren-awareness for this line, but that's a far
		// smaller loss than dropping every field after the break.
		return strings.Split(s, sep)
	}

	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			out = append(out, s[start:i])
			i += len(sep) - 1
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
