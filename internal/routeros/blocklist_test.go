// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"errors"
	"flag"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
	"github.com/tomlawesome/mikroview/internal/store"
)

// update regenerates the blocklist golden files in testdata/:
//
//	go test ./internal/routeros/ -run TestBlocklistBlockExactText -update
//
// and the resulting diff is the review: every byte a router will be
// handed is in it.
var update = flag.Bool("update", false, "update golden files in testdata/ instead of comparing against them")

// defaultChoice is a list switched on with its catalogue defaults, as
// the page opens it, on a router with or without scheduler days.
func defaultChoice(t *testing.T, key string, schedulerDays bool) BlocklistChoice {
	t.Helper()
	l, ok := blcatalogue.Lookup(key)
	if !ok {
		t.Fatalf("no catalogue list %q", key)
	}
	return BlocklistChoice{Key: key, Direction: l.DefaultDirection, IPv6: l.IPv6, Log: true, Refresh: l.RefreshDefaultFor(schedulerDays)}
}

func allDefaults(t *testing.T, schedulerDays bool) []BlocklistChoice {
	var out []BlocklistChoice
	for _, l := range blcatalogue.Lists() {
		out = append(out, defaultChoice(t, l.Key, schedulerDays))
	}
	return out
}

var testPush = BlocklistPush{Address: "mikroview.lan:8443", Token: "tok", Kinds: []string{"filter-rule", "address-list"}}

func mustBlock(t *testing.T, req BlocklistRequest) Block {
	t.Helper()
	b, err := BlocklistBlock(req)
	if err != nil {
		t.Fatalf("BlocklistBlock: %v", err)
	}
	return b
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from what the generator now writes; if the change is meant, run with -update and read the diff.\n got:\n%s", path, got)
	}
}

// The drawn differences between 7.24.4 and 7.19.4, byte for byte, for
// Emerging Threats on its own (BUILD.md part 4): http-max-redirect-count
// and an :onerror flag on 7.19.4, :continue and days= on 7.24.4. The
// golden files were pasted into real CHRs by scripts/live-blocklist-chr.sh
// (the 7.22.3 run carries 7.19.4's shape bar :continue and redirects).
func TestBlocklistBlockExactText(t *testing.T) {
	for _, tc := range []struct {
		version string
		days    bool
	}{{"7.24.4", true}, {"7.19.4", false}} {
		b := mustBlock(t, BlocklistRequest{Device: "rb5009", RouterOSVersion: tc.version, Lists: []BlocklistChoice{defaultChoice(t, "et", tc.days)}})
		golden(t, "blocklist-et-"+tc.version+".rsc", b.CopyText()+"\n")
	}

	new, old := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{defaultChoice(t, "et", true)}}).CopyText(),
		mustBlock(t, BlocklistRequest{RouterOSVersion: "7.19.4", Lists: []BlocklistChoice{defaultChoice(t, "et", false)}}).CopyText()
	for _, c := range []struct {
		text     string
		on724    bool
		on719    bool
		whatItIs string
	}{
		{"http-max-redirect-count=2", false, true, "the redirect count, set by hand before 7.22 made 2 the default"},
		{":continue", true, false, ":continue, new in 7.22"},
		{":set ok false", false, true, "the :onerror flag that stands in for :continue before 7.22"},
		{"days=mon,tue,wed,thu,fri", true, false, "scheduler days, new in 7.24"},
		{"check-certificate=yes", true, true, "a verified fetch, on every router with built-in roots (7.19)"},
	} {
		if strings.Contains(new, c.text) != c.on724 {
			t.Errorf("7.24.4: %s (%q) present = %v, want %v", c.whatItIs, c.text, !c.on724, c.on724)
		}
		if strings.Contains(old, c.text) != c.on719 {
			t.Errorf("7.19.4: %s (%q) present = %v, want %v", c.whatItIs, c.text, !c.on719, c.on719)
		}
	}
	// 7.18 has no built-in roots, so a checked fetch would fail every
	// time; it goes unchecked there rather than never loading.
	floor := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.18.2", Lists: []BlocklistChoice{defaultChoice(t, "et", false)}}).CopyText()
	if strings.Contains(floor, "check-certificate") {
		t.Error("7.18.2 fetches with check-certificate, which fails with no root store to check against")
	}
}

func TestBlocklistBlockRefusesBelowTheFloor(t *testing.T) {
	for _, v := range []string{"7.17.2", "7.12.1", "", "stable"} {
		if _, err := BlocklistBlock(BlocklistRequest{RouterOSVersion: v, Lists: []BlocklistChoice{defaultChoice(t, "et", false)}}); !errors.Is(err, ErrBlocklistBelowFloor) {
			t.Errorf("version %q: err = %v, want ErrBlocklistBelowFloor", v, err)
		}
	}
}

func TestBlocklistBlockRefusesWhatTheListDoesNotOffer(t *testing.T) {
	et := defaultChoice(t, "et", true)
	for _, tc := range []struct {
		name    string
		version string
		lists   []BlocklistChoice
		want    error
	}{
		{"unknown key", "7.24.4", []BlocklistChoice{{Key: "firehol", Direction: "from", Refresh: "daily"}}, ErrBlocklistUnknownKey},
		{"a key that only looks like one", "7.24.4", []BlocklistChoice{{Key: "et;/system reset-configuration", Direction: "from", Refresh: "daily"}}, ErrBlocklistUnknownKey},
		{"chosen twice", "7.24.4", []BlocklistChoice{et, et}, ErrBlocklistDuplicate},
		{"no direction", "7.24.4", []BlocklistChoice{{Key: "et", Refresh: "daily"}}, ErrBlocklistChoice},
		{"a made-up direction", "7.24.4", []BlocklistChoice{{Key: "et", Direction: "sideways", Refresh: "daily"}}, ErrBlocklistChoice},
		{"IPv6 on a list with no IPv6 file", "7.24.4", []BlocklistChoice{{Key: "et", Direction: "from", IPv6: true, Refresh: "daily"}}, ErrBlocklistChoice},
		{"a refresh the list does not offer", "7.24.4", []BlocklistChoice{{Key: "spamhaus", Direction: "from", Refresh: "hourly"}}, ErrBlocklistChoice},
		{"weekdays before 7.24", "7.23.3", []BlocklistChoice{{Key: "et", Direction: "both", Refresh: "weekdays"}}, ErrBlocklistChoice},
	} {
		if _, err := BlocklistBlock(BlocklistRequest{RouterOSVersion: tc.version, Lists: tc.lists}); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// The drawn shape: the push, then per list its script, schedule and
// rules, then run now -- two lists, eight parts, in catalogue order
// whatever order they were chosen in.
func TestBlocklistBlockPartsAreNumberedAsDrawn(t *testing.T) {
	b := mustBlock(t, BlocklistRequest{
		RouterOSVersion: "7.24.4",
		Lists:           []BlocklistChoice{defaultChoice(t, "et", true), defaultChoice(t, "spamhaus", true)},
		Push:            testPush,
	})
	want := []struct {
		ink, title, note string
	}{
		{"push", "The push", "updated once for every list"},
		{"spamhaus", "Spamhaus DROP", "the fetch-and-load script (guarded: adds once, then sets)"},
		{"spamhaus", "Spamhaus DROP", "the schedule: daily, 04:17"},
		{"spamhaus", "Spamhaus DROP", "the drop rules: raw, first, logged · IPv4 and IPv6"},
		{"et", "Emerging Threats compromised IPs", "the fetch-and-load script"},
		{"et", "Emerging Threats compromised IPs", "the schedule: weekdays, 04:31"},
		{"et", "Emerging Threats compromised IPs", "two drop rules: from them, and to them (one prefix; MikroView reads the direction from the line)"},
		{"", "", "Run both now, rather than waiting for 04:17"},
	}
	if len(b.Parts) != len(want) {
		t.Fatalf("%d parts, want %d", len(b.Parts), len(want))
	}
	for i, w := range want {
		p := b.Parts[i]
		note := spansText(p.Note)
		if p.Ordinal != i+1 || p.Ink != w.ink || p.Title != w.title || !strings.HasPrefix(note, w.note) {
			t.Errorf("part %d = {%d %q %q %q}, want {%d %q %q %q…}", i+1, p.Ordinal, p.Ink, p.Title, note, i+1, w.ink, w.title, w.note)
		}
	}
	if note := spansText(b.Parts[4].Note); note != "the fetch-and-load script" {
		t.Errorf("a later loader's head says %q; only the first says it is guarded", note)
	}

	// Not now on Emerging Threats: its three parts go and the rest
	// renumber.
	one := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{defaultChoice(t, "spamhaus", true)}, Push: testPush})
	if len(one.Parts) != 5 || one.Parts[4].Ordinal != 5 || spansText(one.Parts[4].Note) != "Run it now, rather than waiting for 04:17" {
		t.Errorf("one list: %d parts, last %+v", len(one.Parts), one.Parts[len(one.Parts)-1])
	}
	// No token, no part 1.
	if b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{defaultChoice(t, "et", true)}}); b.Parts[0].Ink != "et" {
		t.Errorf("without a push token the block still opens with %q", b.Parts[0].Ink)
	}
	// Nothing chosen: the push alone, and no run-now line with nothing to run.
	if b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Push: testPush}); len(b.Parts) != 1 {
		t.Errorf("nothing chosen gives %d parts, want the push alone", len(b.Parts))
	}
}

func spansText(s []Span) string {
	var b strings.Builder
	for _, x := range s {
		b.WriteString(x.Text)
	}
	return b.String()
}

// Part 1 re-sets the whole push script with the router's kinds and the
// two the builder needs, guarded like the wizard's own step 4.
func TestBlocklistPushPartReSetsThePushScript(t *testing.T) {
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Push: testPush})
	want := ScheduleCommands(PushScript(testPush.Address, testPush.Token, []string{"filter-rule", "address-list", "raw-rule", "address-list-count"}, "a"), "a")
	if b.Parts[0].Commands != want {
		t.Errorf("part 1 is not ScheduleCommands(PushScript(kinds + raw-rule + address-list-count)):\n%s", b.Parts[0].Commands)
	}
	if got := BlocklistPushKinds([]string{"raw-rule", "arp"}); strings.Join(got, ",") != "raw-rule,arp,address-list-count" {
		t.Errorf("BlocklistPushKinds repeated or reordered a kind: %v", got)
	}
	if !strings.Contains(spansText(b.Parts[0].Fold), "re-set with the two new kinds") {
		t.Errorf("part 1's fold = %q", spansText(b.Parts[0].Fold))
	}
}

// Every console command in the block is one line: a pasted line break
// inside quotes is lost before 7.19 (scriptSource).
func TestBlocklistStepsAreOneLineEach(t *testing.T) {
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.18.2", Lists: allDefaults(t, false), Push: testPush})
	steps := b.Steps()
	if strings.Join(steps, "\n") != b.CopyText() {
		t.Error("the copied block is not its steps joined by line breaks")
	}
	for _, s := range steps {
		if strings.Contains(s, "\n") {
			t.Errorf("a step spans lines, which 7.18 would join into one:\n%.200s", s)
		}
	}
}

// The adds MikroView generates must survive being pasted again
// (TestSetupDocAddsAreAllGuarded's rule, applied to the block): every
// add of a script, scheduler entry or raw rule sits in a find guard with
// a set branch that names disabled=no where the thing has one, and
// place-before stays in the add branch.
func TestBlocklistAddsAreGuardedAndReEnable(t *testing.T) {
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: allDefaults(t, true), Push: testPush})
	adds := regexp.MustCompile(`(/system script|/system scheduler|/ip firewall raw|/ipv6 firewall raw) add `)
	for _, s := range b.Steps() {
		if !adds.MatchString(elided(s)) {
			continue
		}
		if !strings.HasPrefix(s, ":if ([:len [") {
			t.Errorf("a bare add, which a second paste would repeat:\n%.200s", s)
			continue
		}
		// The outer set branch is the last else: a rule's add branch holds
		// an else of its own, for the empty raw table.
		e := elided(s)
		i := strings.LastIndex(e, "} else={")
		ok := i >= 0
		head, set := e, ""
		if ok {
			head, set = e[:i], e[i:]
		}
		if !ok {
			t.Errorf("no set branch, so a re-paste leaves an existing entry as it was:\n%.200s", s)
			continue
		}
		if strings.Contains(set, "place-before") {
			t.Errorf("the set branch re-places an existing rule:\n%s", set)
		}
		if strings.Contains(head, "scheduler add") || strings.Contains(head, "raw add") {
			if !strings.Contains(set, "disabled=no") {
				t.Errorf("the set branch leaves a disabled entry disabled, so a re-paste reports success and enforcement stays off:\n%s", set)
			}
		}
	}
}

// elided is a step with every script source folded away, so a test
// reading the commands is not misled by what the loader itself runs.
func elided(s string) string {
	var b strings.Builder
	for _, x := range elideSources(s) {
		b.WriteString(x.Text)
	}
	return b.String()
}

// On an empty raw table place-before=0 is refused ("no such item",
// 7.18.2 and 7.24.4), so each rule's add has a branch for that table.
func TestBlocklistRulesHandleAnEmptyRawTable(t *testing.T) {
	got := rawRuleAdd("/ip firewall raw", "mv-bl-et", "et", "from", true)
	want := `:if ([:len [/ip firewall raw find comment="mikroview blocklist: et (from)"]] = 0) do={ :if ([:len [/ip firewall raw find]] = 0) do={ /ip firewall raw add chain=prerouting src-address-list=mv-bl-et action=drop log=yes log-prefix="D|bl-et|" comment="mikroview blocklist: et (from)" } else={ /ip firewall raw add chain=prerouting src-address-list=mv-bl-et action=drop log=yes log-prefix="D|bl-et|" comment="mikroview blocklist: et (from)" place-before=0 } } else={ /ip firewall raw set [find comment="mikroview blocklist: et (from)"] chain=prerouting src-address-list=mv-bl-et action=drop log=yes log-prefix="D|bl-et|" disabled=no }`
	if got != want {
		t.Errorf("rawRuleAdd =\n%s\nwant\n%s", got, want)
	}
}

// Switching a variant off removes it when, and only when, the router's
// last push says it still holds it.
func TestBlocklistRemovesAVariantSwitchedOff(t *testing.T) {
	sp := defaultChoice(t, "spamhaus", true)
	sp.IPv6 = false
	et := defaultChoice(t, "et", true)
	et.Direction = blcatalogue.DirectionFrom

	quiet := elided(mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{sp, et}}).CopyText())
	if strings.Contains(quiet, " remove ") {
		t.Errorf("removals printed for rules the router never reported:\n%s", quiet)
	}

	b := mustBlock(t, BlocklistRequest{
		RouterOSVersion: "7.24.4",
		Lists:           []BlocklistChoice{sp, et},
		OnRouter: map[string]bool{
			RawRuleKey("ip", "mikroview blocklist: et (to)"):           true,
			RawRuleKey("ipv6", "mikroview blocklist: spamhaus (from)"): true,
		},
		HeldIPv6: map[string]bool{"spamhaus": true},
	}).CopyText()
	for _, want := range []string{
		`/ip firewall raw remove [find comment="mikroview blocklist: et (to)"]`,
		`/ipv6 firewall raw remove [find comment="mikroview blocklist: spamhaus (from)"]`,
		`/ipv6 firewall address-list remove [find list=mv-bl-spamhaus6]`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(b, `/ip firewall raw remove [find comment="mikroview blocklist: et (from)"]`) {
		t.Error("removed the rule the operator kept")
	}
}

// Weekdays is a day schedule; leaving it clears days= with unset, since
// days="" means never on 7.24.4.
func TestBlocklistSchedules(t *testing.T) {
	et := defaultChoice(t, "et", true)
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{et}})
	sched := b.Parts[1].Commands
	if !strings.Contains(sched, "interval=0s days=mon,tue,wed,thu,fri start-time=04:31:00") || strings.Contains(sched, "unset") {
		t.Errorf("weekdays schedule:\n%s", sched)
	}
	var marked []string
	for _, s := range b.Parts[1].Shown[0] {
		if s.Mark == MarkVersion {
			marked = append(marked, s.Text)
		}
	}
	if strings.Join(marked, ",") != "days=mon,tue,wed,thu,fri,days=mon,tue,wed,thu,fri" {
		t.Errorf("days= is not marked as the version-dependent token: %q", marked)
	}

	et.Refresh = blcatalogue.RefreshWeekly
	sched = mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: []BlocklistChoice{et}}).Parts[1].Commands
	if !strings.Contains(sched, "interval=7d start-time=04:31:00") || !strings.HasSuffix(sched, "disabled=no; /system scheduler unset [find name=mv-bl-et] days }") {
		t.Errorf("weekly on 7.24 must clear days= in the set branch:\n%s", sched)
	}
	sched = mustBlock(t, BlocklistRequest{RouterOSVersion: "7.23.3", Lists: []BlocklistChoice{et}}).Parts[1].Commands
	if strings.Contains(sched, "days") {
		t.Errorf("a router without scheduler days is handed days:\n%s", sched)
	}

	// No two lists fetch together.
	seen := map[string]string{}
	for _, l := range blcatalogue.Lists() {
		at, ok := blocklistStartTimes[l.Key]
		if !ok {
			t.Errorf("%s has no start time", l.Key)
		}
		if other, dup := seen[at]; dup {
			t.Errorf("%s and %s both start at %s", l.Key, other, at)
		}
		seen[at] = l.Key
	}
	for _, r := range []blcatalogue.Refresh{blcatalogue.RefreshHourly, blcatalogue.RefreshSixHours, blcatalogue.RefreshDaily, blcatalogue.RefreshWeekdays, blcatalogue.RefreshWeekly} {
		if refreshInterval[r] == "" || refreshWords[r] == "" {
			t.Errorf("refresh %q has no interval or words", r)
		}
	}
}

// Undo removes what the block made, by the names the block made it
// under, and never the push script.
func TestBlocklistUndoMatchesTheForwardNames(t *testing.T) {
	all := allDefaults(t, true)
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.24.4", Lists: all, Push: testPush}).CopyText()
	for _, c := range all {
		undo, err := BlocklistUndo(c.Key)
		if err != nil {
			t.Fatal(err)
		}
		name := blcatalogue.ListName(c.Key)
		for _, forward := range []string{
			`/system script find name=` + name + `]`,
			`/system scheduler find name=` + name + `]`,
			`comment="mikroview blocklist: ` + c.Key + ` (from)"`,
			`\"` + name + `-next\"`,
		} {
			if !strings.Contains(b, forward) {
				t.Errorf("%s: the block no longer makes %s", c.Key, forward)
			}
		}
		for _, back := range []string{
			`/system script remove [find name=` + name + `]`,
			`/system scheduler remove [find name=` + name + `]`,
			`/ip firewall raw remove [find comment="mikroview blocklist: ` + c.Key + ` (from)"]`,
			`/ip firewall raw remove [find comment="mikroview blocklist: ` + c.Key + ` (to)"]`,
			`/ip firewall address-list remove [find list~"^` + name + `"]`,
			`/file remove [find name~"^` + name + `"]`,
		} {
			if !strings.Contains(undo, back) {
				t.Errorf("%s: undo lacks %s:\n%s", c.Key, back, undo)
			}
		}
		if strings.Contains(undo, "mv-push") {
			t.Errorf("%s: undo touches the push script", c.Key)
		}
	}
	sp, _ := BlocklistUndo("spamhaus")
	if !strings.Contains(sp, `/ipv6 firewall raw remove [find comment="mikroview blocklist: spamhaus (from)"]`) || !strings.Contains(sp, `/ipv6 firewall address-list remove [find list~"^mv-bl-spamhaus"]`) {
		t.Errorf("Spamhaus's undo leaves its IPv6 half:\n%s", sp)
	}
	if _, err := BlocklistUndo("nope"); !errors.Is(err, ErrBlocklistUnknownKey) {
		t.Errorf("BlocklistUndo(unknown) err = %v", err)
	}

	everything := BlocklistUndoAll()
	for _, keep := range []string{"mikroview drop list", "mv-push"} {
		if strings.Contains(everything, keep) {
			t.Errorf("Undo everything names %q", keep)
		}
	}
	// Its patterns reach every name the block writes.
	pat := regexp.MustCompile(`^` + blocklistListPrefix)
	for _, n := range blcatalogue.ListNames() {
		if !pat.MatchString(n) {
			t.Errorf("%s escapes Undo everything's ^%s", n, blocklistListPrefix)
		}
	}
	if !strings.Contains(everything, `comment~"^mikroview blocklist: "`) {
		t.Errorf("Undo everything does not reach the rules:\n%s", everything)
	}
}

// Owner, 13a on #1360: a blocklist drop logs as D|bl-<key>|. Every key
// has to fit RouterOS's 15 characters (maxLogPrefixLen) and read back
// through MikroView's own parser as a drop labelled with the list.
func TestBlocklistLogPrefixesFitAndParse(t *testing.T) {
	for _, l := range blcatalogue.Lists() {
		p := BlocklistLogPrefix(l.Key)
		if len(p) > maxLogPrefixLen {
			t.Errorf("%s: prefix %q is %d characters, over %d", l.Key, p, len(p), maxLogPrefixLen)
		}
		action, label, rest := stripPrefix(p + "prerouting: in:ether1 out:(unknown 0), proto TCP")
		if action != store.ActionDrop || label != "bl-"+l.Key || !strings.HasPrefix(rest, "prerouting:") {
			t.Errorf("%s: %q parses as %q / %q / %q", l.Key, p, action, label, rest)
		}
	}
	if p := BlocklistLogPrefix("greensnow"); len(p) != maxLogPrefixLen {
		t.Errorf("D|bl-greensnow| is the longest and exactly the limit; got %d", len(p))
	}
}

// The loader's own pitfalls, each met on a real CHR.
func TestBlocklistLoaderAvoidsTheTrapsMetOnTheRouter(t *testing.T) {
	for _, v := range []string{"7.18.2", "7.24.4"} {
		feat, _ := FeaturesFor(v)
		for _, l := range blcatalogue.Lists() {
			src := loaderScript(l, BlocklistChoice{Key: l.Key, IPv6: l.IPv6}, feat)
			// Inside [find ...], $list is the item's own list property,
			// not the script's local: [find list=$list] matched every
			// entry on the router and the swap removed them all (7.24.4).
			for _, m := range regexp.MustCompile(`\[find [^\]]*\$(\w+)`).FindAllStringSubmatch(src, -1) {
				if !strings.HasPrefix(m[1], "bl") {
					t.Errorf("%s on %s: [find ... $%s] -- a find reads $%s as the item's own property", l.Key, v, m[1], m[1])
				}
			}
			// :pick over an empty range is nil, not "" (7.24.4).
			if strings.Contains(src, `$line = ""`) {
				t.Errorf("%s on %s: tests a blank line with = \"\"", l.Key, v)
			}
			// 7.18.2 cannot shift an IPv6 value.
			if strings.Contains(src, "ffff:ffff") {
				t.Errorf("%s on %s: shifts an IPv6 mask", l.Key, v)
			}
			// :continue is 7.22's; before it the loop must not use it.
			if strings.Contains(src, ":continue") != feat.LoopContinue {
				t.Errorf("%s on %s: :continue present = %v", l.Key, v, !feat.LoopContinue)
			}
			// Nothing a list holds is ever run as script.
			if strings.Contains(src, ":parse") || strings.Contains(src, ":execute") {
				t.Errorf("%s on %s: the loader evaluates text", l.Key, v)
			}
			// The swap happens only on a load that found something.
			if !strings.Contains(src, ":if ($n > 0) do={") {
				t.Errorf("%s on %s: the swap is not guarded by a non-empty load", l.Key, v)
			}
		}
	}
}

// The parse recipes' assumptions about each source, checked against a
// real header and three real lines of each, fetched 2026-10-01 -- the
// way TestParseEmergingThreatsCompromised checks internal/blocklist's.
// recipe is the loader's per-line logic restated in Go; the loader
// itself is proven only on the CHR (docs/routeros-verification-logs).
func TestBlocklistParseRecipesAgainstRealSamples(t *testing.T) {
	samples := map[string]struct {
		body string
		want []string
	}{
		"spamhaus": {`{"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}
{"cidr":"1.19.0.0/16","sblid":"SBL434604","rir":"apnic"}
{"cidr":"1.32.128.0/18","sblid":"SBL286275","rir":"apnic"}
{"type":"metadata","timestamp":1790775842,"size":103312,"records":1693,"copyright":"(c) 2026 The Spamhaus Project SLU","terms":"https://www.spamhaus.org/drop/terms/"}
`, []string{"1.10.16.0/20", "1.19.0.0/16", "1.32.128.0/18"}},
		"spamhaus6": {`{"cidr":"2001:678:254::/48","sblid":"SBL697648","rir":"ripencc"}
{"cidr":"2001:678:6c0::/48","sblid":"SBL624855","rir":"ripencc"}
{"cidr":"2001:678:6c4::/48","sblid":"SBL626637","rir":"ripencc"}
{"type":"metadata","timestamp":1790586842,"size":5759,"records":91,"copyright":"(c) 2026 The Spamhaus Project SLU","terms":"https://www.spamhaus.org/drop/terms/"}
`, []string{"2001:678:254::/48", "2001:678:6c0::/48", "2001:678:6c4::/48"}},
		// ET, CINS, blocklist.de and GreenSnow have no header at all.
		"et":        {"101.47.134.74\n101.47.152.216\n101.47.28.226\n", []string{"101.47.134.74", "101.47.152.216", "101.47.28.226"}},
		"cins":      {"1.12.229.231\n1.15.14.29\n1.165.215.231\n", []string{"1.12.229.231", "1.15.14.29", "1.165.215.231"}},
		"blde":      {"1.212.225.99\n1.214.42.172\n1.238.106.229\n", []string{"1.212.225.99", "1.214.42.172", "1.238.106.229"}},
		"greensnow": {"194.180.49.70\n194.180.49.217\n79.124.58.134\n", []string{"194.180.49.70", "194.180.49.217", "79.124.58.134"}},
		"dshield": {"#\n#   DShield.org Recommended Block List \n#    (c) $year DShield.org\n#   some rights reserved. Details http://creativecommons.org/licenses/by-nc-sa/2.5/\n#    If a range is assigned to multiple users, the first one is listed. \n#     \n" +
			"65.49.1.0\t65.49.1.255\t24\t299\tHURRICANE\tUS\tabuse@he.net\n" +
			"147.185.132.0\t147.185.132.255\t24\t291\tGOOGLE-CLOUD-PLATFORM\tUS\tNone\n" +
			"64.62.156.0\t64.62.156.255\t24\t290\tHURRICANE\tUS\tabuse@he.net\n",
			[]string{"65.49.1.0/24", "147.185.132.0/24", "64.62.156.0/24"}},
		"bindef": {"#\n#\n#\n# Binary Defense Systems Artillery Threat Intelligence Feed and Banlist Feed\n# https://www.binarydefense.com\n#\n# Note that this is for public use only.\n# The ATIF feed may not be used for commercial resale or in products that are charging fees for such services.\n# Use of these feeds for commerical (having others pay for a service) use is strictly prohibited.\n#\n#\n#\n\n1.1.225.66\n1.9.108.229\n1.24.16.126\n",
			[]string{"1.1.225.66", "1.9.108.229", "1.24.16.126"}},
	}
	for key, s := range samples {
		lkey := strings.TrimSuffix(key, "6")
		l, ok := blcatalogue.Lookup(lkey)
		if !ok {
			t.Fatalf("no list %s", lkey)
		}
		v6 := strings.HasSuffix(key, "6")
		var got []string
		for _, line := range strings.Split(s.body, "\n") {
			if a, ok := recipe(l.Format, line); ok {
				p, err := parseEntry(a)
				if err != nil {
					t.Errorf("%s: %q is not an address or prefix: %v", key, a, err)
					continue
				}
				if p.Addr().Is6() != v6 {
					t.Errorf("%s: %q is the wrong family", key, a)
				}
				got = append(got, a)
			}
		}
		if strings.Join(got, ",") != strings.Join(s.want, ",") {
			t.Errorf("%s: recipe read %v, want %v", key, got, s.want)
		}
	}
}

// recipe restates the loader's parseLine in Go.
func recipe(f blcatalogue.Format, line string) (string, bool) {
	line = strings.TrimSuffix(line, "\r")
	line = strings.TrimLeft(line, " \t")
	if line == "" {
		return "", false
	}
	switch f {
	case blcatalogue.FormatSpamhausJSON:
		i := strings.Index(line, `"cidr":"`)
		if i < 0 {
			return "", false
		}
		rest := line[i+len(`"cidr":"`):]
		return rest[:strings.IndexByte(rest, '"')], true
	case blcatalogue.FormatDShield:
		if line[0] == '#' {
			return "", false
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			return "", false
		}
		return f[0] + "/" + f[2], true
	default:
		if line[0] == '#' || line[0] == ';' {
			return "", false
		}
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			line = line[:i]
		}
		return line, true
	}
}

func parseEntry(a string) (netip.Prefix, error) {
	if strings.Contains(a, "/") {
		return netip.ParsePrefix(a)
	}
	addr, err := netip.ParseAddr(a)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// The page shows every command with its sources folded; folding must
// cut nothing else.
func TestBlocklistShownFoldsOnlySources(t *testing.T) {
	b := mustBlock(t, BlocklistRequest{RouterOSVersion: "7.19.4", Lists: allDefaults(t, false), Push: testPush})
	for _, p := range b.Parts {
		if len(p.Shown) != len(p.Steps) {
			t.Fatalf("part %d shows %d lines for %d steps", p.Ordinal, len(p.Shown), len(p.Steps))
		}
		for i, line := range p.Shown {
			var sb strings.Builder
			for _, s := range line {
				if s.Mark == MarkElided {
					sb.WriteString("…")
					continue
				}
				sb.WriteString(s.Text)
			}
			if want := elided(p.Steps[i]); sb.String() != strings.ReplaceAll(want, "…", "…") {
				t.Errorf("part %d line %d shows %q, want %q", p.Ordinal, i, sb.String(), want)
			}
		}
	}
	// The drawn version tokens on 7.19.4: the redirect count and the
	// :onerror flag in the first loader's fold.
	var marked []string
	for _, s := range b.Parts[1].Fold {
		if s.Mark == MarkVersion {
			marked = append(marked, s.Text)
		}
	}
	if strings.Join(marked, " ") != "http-max-redirect-count=2 :deserialize from=json :onerror" {
		t.Errorf("first loader's fold marks %q", marked)
	}
}
