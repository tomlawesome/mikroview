// SPDX-License-Identifier: AGPL-3.0-only

package blocklist

import (
	"bufio"
	"bytes"
	"net/netip"
	"strings"
)

// maxNoticeLines bounds how much of a feed's leading comment block is
// retained as its attribution notice. Spamhaus's own header is four
// lines; this leaves room for a reformat without letting a feed that
// prepends a manifesto grow unbounded in memory.
const maxNoticeLines = 12

// leadingNotice collects a feed's opening comment block -- the lines
// before the first data line, each starting with commentPrefix.
//
// This exists for licence compliance, not decoration. Spamhaus's DROP
// terms require that "credit must be given to Spamhaus Project, and the
// date and © text should remain with the file and data" -- and that text
// lives exactly in these lines, which the parsers below otherwise
// discard. Retaining it here is what lets the copyright and list date
// travel with the data in memory rather than being thrown away the
// instant it arrives. See AGENTS.md's "Never vendor list or lookup
// data".
func leadingNotice(body []byte, commentPrefix string) string {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, commentPrefix) {
			break // first data line -- the header block is over
		}
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, commentPrefix)))
		if len(lines) == maxNoticeLines {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// parseSpamhaus parses Spamhaus DROP's plain-text format: one CIDR per
// line, optionally followed by a "; SBLnnnnn" comment, plus full-line
// comments starting with ";" and blank lines. Example line:
//
//	1.10.16.0/20 ; SBL2472
//
// Returns the feed's leading comment block as its attribution notice --
// see leadingNotice for why that is a requirement rather than a nicety.
//
// A malformed line is skipped, not fatal -- one bad/reformatted line
// from an upstream feed mikroview doesn't control shouldn't discard
// every other valid entry in the same fetch.
func parseSpamhaus(body []byte) ([]netip.Prefix, string, error) {
	var out []netip.Prefix
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		field := line
		if i := strings.IndexByte(line, ';'); i >= 0 {
			field = line[:i]
		}
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if p, err := netip.ParsePrefix(field); err == nil && plausiblePrefix(p) {
			out = append(out, p)
		}
		// A bare IP with no /bits (shouldn't occur in DROP, which always
		// publishes CIDR ranges, but handled defensively) is skipped
		// rather than guessed at -- guessing a host's intended prefix
		// length wrong is worse than not loading that one line.
	}
	return out, leadingNotice(body, ";"), scanner.Err()
}

// parseEmergingThreatsCompromised parses Emerging Threats'
// compromised-ips.txt format: one bare IP address per line (no CIDR
// notation), plus "#"-prefixed comments and blank lines. Each address is
// treated as an exact /32 (or /128 for the rare IPv6 entry) match.
//
// Returns its leading comment block too, for the same reason as
// parseSpamhaus -- ET's list currently ships no header, in which case
// this is simply empty, which is handled rather than special-cased.
func parseEmergingThreatsCompromised(body []byte) ([]netip.Prefix, string, error) {
	var out []netip.Prefix
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		addr, err := netip.ParseAddr(line)
		if err != nil {
			continue
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, leadingNotice(body, "#"), scanner.Err()
}

// minPrefixBits is the widest prefix a threat-intel feed may contribute.
//
// The feeds are parsed with no bound on prefix width, so a single
// poisoned or truncated line saying "0.0.0.0/0" would mark every address
// mikroview ever sees as known-bad -- every source IP raising a
// known_bad_ip flag, which is both useless and drowns the real ones. A
// /8 is already 16.7 million addresses and far wider than anything
// Spamhaus DROP publishes (its widest entries are /16-ish); anything
// broader is a malformed or hostile line, not a range.
//
// Skipped rather than rejecting the whole feed, matching how this parser
// already treats a bare IP with no /bits: one bad line loses one line.
// See #285.
const minPrefixBits = 8

func plausiblePrefix(p netip.Prefix) bool {
	if p.Addr().Is4() {
		return p.Bits() >= minPrefixBits
	}
	// The IPv6 equivalent scaled to its own address size: /8 of a
	// 32-bit space is 1/256th of it, and /32 of a 128-bit space is the
	// smallest allocation a regional registry hands out.
	return p.Bits() >= 32
}
