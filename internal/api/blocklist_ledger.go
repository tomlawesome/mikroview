// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
	"github.com/tomlawesome/mikroview/internal/blocklist"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/routerstate"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// The blocklist builder's ledger (#1360, BUILD.md part 6): what each
// list is doing on a router, read from its own pushes -- never asked of
// the router, which MikroView does not connect to.

// blocklistActivity fills a ledger row's activity: how often the list's
// rules fired today, and the flags MikroView raised from it.
func (s *Server) blocklistActivity(device string, l blcatalogue.List, row *blocklistLedgerRow, now time.Time) {
	if s.RouterState != nil {
		midnight := localMidnight(now)
		var total int64
		seen := false
		for _, dir := range []string{"from", "to"} {
			comment := routeros.BlocklistRuleCommentPrefix(l.Key) + dir + ")"
			if n, ok := firedSince(s.RouterState.RawRuleSamples(device, comment), midnight); ok {
				total += n
				seen = true
			}
		}
		if seen {
			row.FiredToday = &total
		}
	}
	// Flags only where MikroView flags from the list and its flags name
	// the feed (known_bad_ip's detail reads "matches <label> (<range>)");
	// for every other list the card says MikroView does not flag from it,
	// and no count is invented.
	if label, ok := blocklist.FlagLabel(l.Key); ok && s.Flags != nil {
		n := countFeedFlags(s.Flags.List(), label, now.Add(-24*time.Hour))
		row.Flags24h = &n
	}
}

// localMidnight is the start of now's day in the instance's own
// timezone: "today" in "rule fired N times today".
func localMidnight(now time.Time) time.Time {
	t := now.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// firedSince is how many packets a rule's counter gained across the
// samples taken since since, oldest first (BUILD.md part 6): the latest
// reading less the earliest one today, except that a counter that falls
// -- the router rebooted, or the rule was re-added -- restarts the count
// from its new reading rather than going negative. ok is false with no
// sample today. Samples are kept from each push, so a day that began
// before MikroView did counts from its first push.
func firedSince(samples []routerstate.RawRuleSample, since time.Time) (int64, bool) {
	var total int64
	var prev int64
	seen := false
	for _, s := range samples {
		if s.At.Before(since) {
			continue
		}
		if !seen {
			prev, seen = s.Packets, true
			continue
		}
		if s.Packets >= prev {
			total += s.Packets - prev
		} else {
			total += s.Packets
		}
		prev = s.Packets
	}
	return total, seen
}

// countFeedFlags counts the known_bad_ip flags naming label that fired
// since since.
func countFeedFlags(all []flags.Flag, label string, since time.Time) int {
	prefix := "matches " + label + " ("
	n := 0
	for _, f := range all {
		if f.Type == flags.TypeKnownBadIP && strings.HasPrefix(f.Detail, prefix) && !f.LastSeen.Before(since) {
			n++
		}
	}
	return n
}

// noteBlocklistWitness is record 8's witness (#1360): the first push
// that reports a blocklist list holding entries is the first-run tail's
// proof, written with what it showed. NoteWitnessed writes once and is a
// no-op after, so this costs a map read on every later push.
func (s *Server) noteBlocklistWitness(device string, p ingest.Payload, now time.Time) {
	if s.Setup == nil || p.Kind != ingest.KindAddressListCount {
		return
	}
	var held []string
	for _, l := range blcatalogue.Lists() {
		for _, c := range p.AddressListCounts {
			if c.List == blcatalogue.ListName(l.Key) && c.Family == ingest.FamilyIP && c.Count > 0 {
				held = append(held, fmt.Sprintf("%s %s held", l.Short, groupThousands(int(c.Count))))
			}
		}
	}
	if len(held) == 0 {
		return
	}
	s.Setup.NoteWitnessed(setup.StepBlocklist, strings.Join(held, " · ")+" · on "+device, now)
}

// groupThousands writes n with comma thousands separators, as the page
// writes counts ("1,692").
func groupThousands(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + groupThousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
