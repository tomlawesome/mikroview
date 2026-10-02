// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"fmt"
	"strings"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
)

// The loader script a list's scheduler runs (#1360, BUILD.md part 4).
// What it does, in order, for each file (Spamhaus has two, IPv4 and
// IPv6):
//
//  1. /tool fetch the file to mv-bl-<key>.src. A failed fetch logs a
//     warning and leaves yesterday's list standing.
//  2. Clear mv-bl-<key>-next, then read the file in 32 KiB chunks with
//     /file read (a script variable holds at most 63 KiB; chunk-size
//     tops out at 32768, 7.24.4), carrying a line split across two
//     chunks into the next.
//  3. Per line, by the list's Format, find the address; refuse anything
//     that is not an address or prefix, anything wider than /8 (IPv4) or
//     /16 (IPv6) as a corrupted line, and anything that overlaps private,
//     loopback, link-local or multicast space; add the rest to -next.
//  4. Only if at least one entry went in: remove the live list and
//     rename -next onto it (the swap internal/droplist/rsc.go makes). An
//     empty load is a failure, and yesterday's list stands.
//  5. Remove the fetched file: the counts the ledger shows come from the
//     address list, so the file need not hold flash (BUILD.md,
//     Decisions).
//
// Nothing read from a list is ever evaluated as script: each line is cut
// with :pick and :find, checked with :toip/:toip6, and handed to the
// add as a value. :deserialize parses Spamhaus's JSON as data.
//
// A bad line is skipped with :continue from 7.22, and with an :onerror
// flag before it (Features.LoopContinue): the two drawn variants.

// specialV4 and specialV6 are the address space a list may never hold.
// A raw prerouting drop placed first on any of it would drop the
// operator's own LAN or the router's own traffic -- the reason FireHOL
// level 1 is left out of the catalogue -- so a list that grew some,
// through corruption or compromise at its source, has those lines
// refused rather than loaded.
var (
	specialV4 = []struct{ start, prefix string }{
		{"0.0.0.0", "0.0.0.0/8"},
		{"10.0.0.0", "10.0.0.0/8"},
		{"100.64.0.0", "100.64.0.0/10"},
		{"127.0.0.0", "127.0.0.0/8"},
		{"169.254.0.0", "169.254.0.0/16"},
		{"172.16.0.0", "172.16.0.0/12"},
		{"192.168.0.0", "192.168.0.0/16"},
		{"224.0.0.0", "224.0.0.0/3"},
	}
	specialV6 = []string{"::/8", "fc00::/7", "fe80::/10", "ff00::/8"}
)

// widest is the shortest prefix a list may load, per family.
const (
	widestV4 = 8
	widestV6 = 16
)

// loaderScript is the whole source of a list's loader.
func loaderScript(l blcatalogue.List, c BlocklistChoice, feat Features) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# mikroview blocklist: %s -- %s, fetched by this router from its source.\n", l.Key, l.Name)
	b.WriteString("# MikroView wrote this script and never runs it; the router does the fetching.\n")
	b.WriteString(loadSection(l, l.URL, "ip", blcatalogue.ListName(l.Key), feat))
	if c.IPv6 && l.URL6 != "" {
		b.WriteString("\n")
		b.WriteString(loadSection(l, l.URL6, "ipv6", blcatalogue.ListName6(l.Key), feat))
	}
	return strings.TrimRight(b.String(), "\n")
}

// lineCode builds the per-line body of the read loop. In continue mode
// each rejection is a :continue; in flag mode it sets $ok false and every
// later step runs only while $ok holds.
type lineCode struct {
	cont  bool
	steps []string
}

// step adds a statement that runs only if the line is still wanted.
func (c *lineCode) step(s string) {
	if c.cont || len(c.steps) == 0 {
		c.steps = append(c.steps, s)
		return
	}
	c.steps = append(c.steps, ":if ($ok) do={ "+s+" }")
}

// reject is the statement that drops the current line: quietly for a
// comment or a blank, counted for a line that should have been an entry.
func (c *lineCode) reject(counted bool) string {
	count := ""
	if counted {
		count = ":set skipped ($skipped + 1); "
	}
	if c.cont {
		return count + ":continue"
	}
	return count + ":set ok false"
}

func (c *lineCode) String() string {
	return "      " + strings.Join(c.steps, "\n      ")
}

// loadSection fetches one file and loads it into list on family's menu.
func loadSection(l blcatalogue.List, url, family, list string, feat Features) string {
	menu := "/ip firewall address-list"
	if family == "ipv6" {
		menu = "/ipv6 firewall address-list"
	}
	file := list + ".src"
	what := l.Key
	if family == "ipv6" {
		what += " (IPv6)"
	}

	fetch := fmt.Sprintf(`/tool fetch url="%s" dst-path=$blFile`, quote(url))
	if !feat.FetchFollowsRedirects {
		fetch += " http-max-redirect-count=2"
	}
	if feat.FetchTrustsBuiltinRoots {
		fetch += " check-certificate=yes"
	}
	fetch += " output=file"

	code := &lineCode{cont: feat.LoopContinue}
	parseLine(code, l.Format)
	validate(code, family)
	add := fmt.Sprintf("%s add list=$blNext address=$a", menu)
	if l.Format == blcatalogue.FormatSpamhausJSON {
		add += " comment=$cmt"
	}
	if code.cont {
		code.step(fmt.Sprintf(":onerror e in={ %s } do={ %s }", add, code.reject(true)))
		code.step(":set n ($n + 1)")
	} else {
		code.step(fmt.Sprintf(":onerror e in={ %s } do={ %s }", add, code.reject(true)))
		code.steps = append(code.steps, ":if ($ok) do={ :set n ($n + 1) }")
	}

	var locals string
	if code.cont {
		locals = `      :local a ""; :local cmt ""; :local ip; :local len 0; :local bad false`
	} else {
		locals = `      :local ok true; :local a ""; :local cmt ""; :local ip; :local len 0; :local bad false`
	}

	return fmt.Sprintf(`{
:local blList "%[1]s"
:local blNext "%[1]s-next"
:local blFile "%[2]s"
:local got true
:onerror e in={ %[3]s } do={ :set got false }
:if (!$got) do={
  :log warning "mikroview blocklist: %[4]s: the fetch failed; the list stands as it was"
} else={
  %[5]s remove [find list=$blNext]
  :local size [/file get [find name=$blFile] size]
  :local off 0
  :local carry ""
  :local n 0
  :local skipped 0
  :while ($off < $size) do={
    :local buf ($carry . ([/file read file=$blFile offset=$off chunk-size=32768 as-value]->"data"))
    :set off ($off + 32768)
    :if ($off >= $size) do={ :set buf ($buf . "\n") }
    :local pos 0
    :local nl [:find $buf "\n" -1]
    :while ([:typeof $nl] = "num") do={
      :local line [:pick $buf $pos $nl]
      :set pos ($nl + 1)
      :set nl [:find $buf "\n" $nl]
%[6]s
%[7]s
    }
    :set carry [:pick $buf $pos [:len $buf]]
  }
  :if ($n > 0) do={
    %[5]s remove [find list=$blList]
    %[5]s set [find list=$blNext] list=$blList
    :log info "mikroview blocklist: %[4]s: $n loaded, $skipped skipped"
  } else={
    %[5]s remove [find list=$blNext]
    :log warning "mikroview blocklist: %[4]s: nothing could be read from the file; the list stands as it was"
  }
}
:onerror e in={ /file remove [find name=$blFile] } do={ }
}
`, list, file, fetch, what, menu, locals, code.String())
}

// parseLine sets $a to the line's address, by format, rejecting comments
// and blanks quietly.
func parseLine(c *lineCode, f blcatalogue.Format) {
	// A trailing \r (a file written on Windows) and surrounding blanks are
	// not part of the address. An empty line is tested by its length: :pick
	// over an empty range gives nil, not "", so $line = "" misses it
	// (7.24.4). Every comparison inside || and && is parenthesised.
	c.step(`:if ([:len $line] > 0) do={ :if ([:pick $line ([:len $line] - 1)] = "\r") do={ :set line [:pick $line 0 ([:len $line] - 1)] } }`)
	c.step(`:while (([:len $line] > 0) && (([:pick $line 0] = " ") || ([:pick $line 0] = "\t"))) do={ :set line [:pick $line 1 [:len $line]] }`)
	quiet := c.reject(false)
	switch f {
	case blcatalogue.FormatSpamhausJSON:
		// One object per line; the file's last line is metadata with no
		// cidr, skipped quietly. :deserialize types cidr as an ip-prefix
		// (7.24.4), so it is turned back into text for the checks below.
		c.step(`:if ([:len $line] = 0) do={ ` + quiet + ` }`)
		c.step(`:local o; :onerror e in={ :set o [:deserialize from=json value=$line] } do={ :set bad true }; :if ($bad) do={ ` + c.reject(true) + ` } else={ :set a [:tostr ($o->"cidr")]; :set cmt ([:pick ($o->"sblid") 0 24] . " \C2\A9 The Spamhaus Project") }`)
		c.step(`:if ([:len $a] = 0) do={ ` + quiet + ` }`)
	case blcatalogue.FormatDShield:
		// Tab-separated "start end mask attacks name country email" after
		// a # header: the entry is start/mask.
		c.step(`:if (([:len $line] = 0) || ([:pick $line 0] = "#")) do={ ` + quiet + ` }`)
		c.step(`:local t1 [:find $line "\t" -1]; :local t2; :local t3; :if ([:typeof $t1] = "num") do={ :set t2 [:find $line "\t" $t1] }; :if ([:typeof $t2] = "num") do={ :set t3 [:find $line "\t" $t2] }; :if ([:typeof $t3] != "num") do={ ` + c.reject(true) + ` } else={ :set a ([:pick $line 0 $t1] . "/" . [:pick $line ($t2 + 1) $t3]) }`)
	default:
		// One address or prefix per line; # and ; start a comment line,
		// and anything after the first blank on a line is not the entry.
		c.step(`:if (([:len $line] = 0) || ([:pick $line 0] = "#") || ([:pick $line 0] = ";")) do={ ` + quiet + ` }`)
		c.step(`:local sp [:find $line " " -1]; :if ([:typeof $sp] = "num") do={ :set line [:pick $line 0 $sp] }; :set sp [:find $line "\t" -1]; :if ([:typeof $sp] = "num") do={ :set line [:pick $line 0 $sp] }; :set a $line`)
	}
}

// validate rejects $a unless it is an address or prefix of the family,
// no wider than the family's limit, clear of special space.
func validate(c *lineCode, family string) {
	toip, all, width, widest := ":toip", "255.255.255.255", 32, widestV4
	if family == "ipv6" {
		toip, width, widest = ":toip6", 128, widestV6
	}
	c.step(fmt.Sprintf(`:local slash [:find $a "/" -1]; :if ([:typeof $slash] = "num") do={ :set ip [%[1]s [:pick $a 0 $slash]]; :set len [:tonum [:pick $a ($slash + 1) [:len $a]]] } else={ :set ip [%[1]s $a]; :set len %[2]d }`, toip, width))
	c.step(fmt.Sprintf(`:if (([:typeof $ip] != "%s") || ([:typeof $len] != "num")) do={ %s }`, map[string]string{"ip": "ip", "ipv6": "ip6"}[family], c.reject(true)))
	c.step(fmt.Sprintf(`:if (($len < %d) || ($len > %d)) do={ %s }`, widest, width, c.reject(true)))
	if family == "ipv6" {
		// Every special IPv6 range is wider than /16, the widest a line
		// may be, so an entry overlaps one only by sitting inside it, and
		// any address in the entry answers that. (Masking it first is not
		// an option: 7.18.2 refuses to shift an IPv6 value.)
		c.step(fmt.Sprintf(`:foreach s in={%s} do={ :if ($ip in $s) do={ :set bad true } }; :if ($bad) do={ %s }`,
			strings.Join(specialV6, ";"), c.reject(true)))
		return
	}
	var prefixes, starts []string
	for _, s := range specialV4 {
		prefixes = append(prefixes, s.prefix)
		starts = append(starts, s.start)
	}
	// An entry overlaps a special range by sitting inside it, or -- only
	// possible when the entry is wider than /16, the narrowest special
	// range -- by containing it.
	c.step(fmt.Sprintf(`:foreach s in={%s} do={ :if ($ip in $s) do={ :set bad true } }; :if ($len < 16) do={ :local m (%s << (%d - $len)); :foreach s in={%s} do={ :if (($s & $m) = ($ip & $m)) do={ :set bad true } } }; :if ($bad) do={ %s }`,
		strings.Join(prefixes, ";"), all, width, strings.Join(starts, ";"), c.reject(true)))
}
