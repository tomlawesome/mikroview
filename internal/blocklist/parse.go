package blocklist

import (
	"bufio"
	"bytes"
	"net/netip"
	"strings"
)

// parseSpamhaus parses Spamhaus DROP/EDROP's plain-text format: one CIDR
// per line, optionally followed by a "; SBLnnnnn" comment, plus
// full-line comments starting with ";" and blank lines. Example line:
//
//	1.10.16.0/20 ; SBL2472
//
// A malformed line is skipped, not fatal -- one bad/reformatted line
// from an upstream feed mikroview doesn't control shouldn't discard
// every other valid entry in the same fetch.
func parseSpamhaus(body []byte) ([]netip.Prefix, error) {
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
		if p, err := netip.ParsePrefix(field); err == nil {
			out = append(out, p)
		}
		// A bare IP with no /bits (shouldn't occur in DROP/EDROP, which
		// always publish CIDR ranges, but handled defensively) is
		// skipped rather than guessed at -- guessing a host's intended
		// prefix length wrong is worse than not loading that one line.
	}
	return out, scanner.Err()
}

// parseEmergingThreatsCompromised parses Emerging Threats'
// compromised-ips.txt format: one bare IP address per line (no CIDR
// notation), plus "#"-prefixed comments and blank lines. Each address is
// treated as an exact /32 (or /128 for the rare IPv6 entry) match.
func parseEmergingThreatsCompromised(body []byte) ([]netip.Prefix, error) {
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
	return out, scanner.Err()
}
