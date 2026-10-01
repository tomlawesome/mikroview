// SPDX-License-Identifier: AGPL-3.0-only

package routeros

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tomlawesome/mikroview/internal/blcatalogue"
)

// The blocklist builder's command generator (#1360, BUILD.md part 4).
//
// MikroView prints these commands and never runs them: the router fetches
// each list straight from its source on its own schedule, and MikroView
// never serves, copies or vendors list data (AGENTS.md, "List and lookup
// data"). What the block leaves on a router, all named so Undo can find
// it again:
//
//   - a loader script mv-bl-<key> that fetches the list to a file, reads
//     it in 32 KiB chunks, builds mv-bl-<key>-next and swaps it in only
//     when at least one entry was read;
//   - a scheduler entry mv-bl-<key> that runs it at the list's own start
//     time, so no two lists fetch together;
//   - raw prerouting drop rules, placed first, commented
//     "mikroview blocklist: <key> (from)" and "(to)".
//
// Every add is guarded add-or-set, and every set names disabled=no where
// the thing has one, so pasting the block twice changes nothing and
// pasting it after an incident turns enforcement back on (#1266, the
// v0.6.0 audit's Security stage).
//
// Checked on real CHRs at 7.18.2, 7.22.3 and 7.24.4
// (docs/routeros-verification-logs/<version>-blocklist.log, made by
// scripts/live-blocklist-chr.sh).

// BlocklistChoice is one list the operator switched on, and how.
type BlocklistChoice struct {
	Key       string                `json:"key"`
	Direction blcatalogue.Direction `json:"direction"`
	IPv6      bool                  `json:"ipv6"`
	Log       bool                  `json:"log"`
	Refresh   blcatalogue.Refresh   `json:"refresh"`
}

// BlocklistPush is what part 1 of the block re-renders the push script
// from: the address and token it posts with, and the kinds the router
// already pushes. raw-rule and address-list-count are added to Kinds
// here, so the caller passes what the router has rather than what the
// builder needs.
type BlocklistPush struct {
	Address string
	Token   string
	Kinds   []string
}

// BlocklistRequest is everything the generator needs. RouterOSVersion is
// the router's own, from its push -- never the client's say-so.
type BlocklistRequest struct {
	Device          string
	RouterOSVersion string
	Lists           []BlocklistChoice
	// Push renders part 1. A zero Push (no token) leaves part 1 out: the
	// caller has nothing to re-set the push script with.
	Push BlocklistPush
	// OnRouter is the router's own raw rules, by family and comment, as
	// its last push reported them (RawRuleKey). A variant the operator
	// has switched off -- the "(to)" twin after both -> from, the IPv6
	// pair after IPv6 -> no -- is removed when, and only when, the router
	// still holds it, so a re-paste converges without printing removals
	// for rules that were never there.
	OnRouter map[string]bool
	// HeldIPv6 lists the keys whose IPv6 list the router holds entries
	// in; switching IPv6 off removes that list too.
	HeldIPv6 map[string]bool
}

// RawRuleKey is how OnRouter names a raw rule: its family ("ip" or
// "ipv6") and its comment.
func RawRuleKey(family, comment string) string { return family + "|" + comment }

// Span is a run of text in the page's rendering of a part, with
// what marks it: "" plain, "elided" a script source folded to an
// ellipsis, "version" a token that depends on the router's version (the
// drawn time ink).
type Span struct {
	Text string `json:"text"`
	Mark string `json:"mark,omitempty"`
}

// Span marks.
const (
	MarkElided  = "elided"
	MarkVersion = "version"
)

// Part is one numbered part of the block, as drawn: a head
// ("# 2 · Spamhaus DROP — the fetch-and-load script"), the commands as
// the page shows them (sources folded), the fold line under them, and
// the exact text Copy puts on the clipboard.
type Part struct {
	Ordinal int `json:"ordinal"`
	// Ink is the list key, "push", or "" for the closing run-now line.
	Ink string `json:"ink"`
	// Title is the head's bold name; "" for the run-now line, whose head
	// is Note alone.
	Title string   `json:"title"`
	Note  []Span   `json:"note"`
	Shown [][]Span `json:"shown"`
	Fold  []Span   `json:"fold,omitempty"`
	// Commands is what this part contributes to the copied block.
	Commands string `json:"commands"`
	// Steps is Commands as the console commands it is made of, one per
	// line (a script's source keeps its line breaks as \n escapes). The
	// CHR check pastes these one at a time.
	Steps []string `json:"-"`
}

// Block is the whole blocklist block, in paste order.
type Block struct {
	Parts []Part `json:"parts"`
}

// CopyText is the block as one paste: every part's commands, in order.
func (b Block) CopyText() string {
	var out []string
	for _, p := range b.Parts {
		out = append(out, p.Commands)
	}
	return strings.Join(out, "\n")
}

// Steps is every part's steps, in order.
func (b Block) Steps() []string {
	var out []string
	for _, p := range b.Parts {
		out = append(out, p.Steps...)
	}
	return out
}

// Errors BlocklistBlock refuses with. The API turns the first into the
// below-floor standing, the rest into a 400.
var (
	ErrBlocklistBelowFloor = errors.New("the router's RouterOS version is below MikroView's floor, or unreadable")
	ErrBlocklistUnknownKey = errors.New("no such list in the catalogue")
	ErrBlocklistChoice     = errors.New("a choice the list does not offer")
	ErrBlocklistDuplicate  = errors.New("a list chosen twice")
)

// BlocklistScriptPolicy is what each loader script and its scheduler
// entry run under: fetch writes the file (read, write, test), /file read
// and /file remove need ftp.
const BlocklistScriptPolicy = "read,write,test,ftp"

// blocklistRuleComment is the comment a list's raw rule carries, per
// direction ("from" or "to"). Undo removes by it.
func blocklistRuleComment(key, dir string) string {
	return fmt.Sprintf("mikroview blocklist: %s (%s)", key, dir)
}

// BlocklistRuleCommentPrefix is what every raw rule of key's carries at
// the start of its comment, either direction: how the ledger finds them
// in the router's pushed raw table.
func BlocklistRuleCommentPrefix(key string) string {
	return fmt.Sprintf("mikroview blocklist: %s (", key)
}

// BlocklistStartTime is key's scheduled fetch time ("04:17"), or "" for a
// key the catalogue does not have.
func BlocklistStartTime(key string) string {
	t := blocklistStartTimes[key]
	if len(t) < 5 {
		return ""
	}
	return t[:5]
}

// BlocklistLogPrefix is a list's drop prefix, D|bl-<key>| (owner, 13a on
// #1360): D reads as a drop, the label names the list. One prefix for
// both directions; MikroView reads the direction off the line.
func BlocklistLogPrefix(key string) string {
	return "D|bl-" + key + "|"
}

// blocklistStartTimes spreads the seven lists' fetches over the small
// hours so no two run together (BUILD.md part 4).
var blocklistStartTimes = map[string]string{
	"spamhaus":  "04:17:00",
	"et":        "04:31:00",
	"cins":      "04:45:00",
	"blde":      "04:59:00",
	"greensnow": "05:13:00",
	"dshield":   "05:27:00",
	"bindef":    "05:41:00",
}

// refreshInterval is the scheduler interval for each refresh choice.
// Weekdays is a day schedule rather than an interval: on 7.24.4 a
// scheduler with days= and interval=1d works but carries RouterOS's own
// warning "multi-day intervals are not supported with active day
// scheduling", and interval=0s with days= fires once on each listed day
// (docs/routeros-verification-logs/7.24.4-blocklist.log).
var refreshInterval = map[blcatalogue.Refresh]string{
	blcatalogue.RefreshHourly:   "1h",
	blcatalogue.RefreshSixHours: "6h",
	blcatalogue.RefreshDaily:    "1d",
	blcatalogue.RefreshWeekdays: "0s",
	blcatalogue.RefreshWeekly:   "7d",
}

// refreshWords is how a part's head says a refresh.
var refreshWords = map[blcatalogue.Refresh]string{
	blcatalogue.RefreshHourly:   "hourly",
	blcatalogue.RefreshSixHours: "every 6 h",
	blcatalogue.RefreshDaily:    "daily",
	blcatalogue.RefreshWeekdays: "weekdays",
	blcatalogue.RefreshWeekly:   "weekly",
}

const weekdays = "mon,tue,wed,thu,fri"

// BlocklistBlock renders the block for req (BUILD.md part 4): the push
// re-set (part 1), then per chosen list its loader, its schedule and its
// rules, then one line that runs every chosen loader now. Lists render in
// catalogue order whatever order they were chosen in, so the parts are
// numbered as the cards are drawn.
func BlocklistBlock(req BlocklistRequest) (Block, error) {
	feat, ok := FeaturesFor(req.RouterOSVersion)
	if !ok {
		return Block{}, ErrBlocklistBelowFloor
	}
	chosen, err := orderedChoices(req.Lists, feat)
	if err != nil {
		return Block{}, err
	}
	dialect := "a"
	if row, ok := RowFor(req.RouterOSVersion); ok {
		dialect = row.Dialect
	}

	var parts []Part
	add := func(p Part) {
		p.Ordinal = len(parts) + 1
		p.Commands = strings.Join(p.Steps, "\n")
		parts = append(parts, p)
	}

	if req.Push.Token != "" {
		add(pushPart(req.Push, dialect))
	}
	for i, c := range chosen {
		l, _ := blcatalogue.Lookup(c.Key)
		add(loaderPart(l, c, feat, i == 0))
		add(schedulePart(l, c, feat))
		add(rulesPart(l, c, req))
	}
	if len(chosen) > 0 {
		add(runNowPart(chosen))
	}
	return Block{Parts: parts}, nil
}

// orderedChoices checks every choice against the catalogue and the
// router's version and returns them in catalogue order.
func orderedChoices(in []BlocklistChoice, feat Features) ([]BlocklistChoice, error) {
	byKey := map[string]BlocklistChoice{}
	for _, c := range in {
		l, ok := blcatalogue.Lookup(c.Key)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrBlocklistUnknownKey, c.Key)
		}
		if _, dup := byKey[c.Key]; dup {
			return nil, fmt.Errorf("%w: %q", ErrBlocklistDuplicate, c.Key)
		}
		if c.Direction != blcatalogue.DirectionFrom && c.Direction != blcatalogue.DirectionBoth {
			return nil, fmt.Errorf("%w: direction %q for %s", ErrBlocklistChoice, c.Direction, c.Key)
		}
		if c.IPv6 && !l.IPv6 {
			return nil, fmt.Errorf("%w: IPv6 for %s, which has no IPv6 list", ErrBlocklistChoice, c.Key)
		}
		if !slices.Contains(l.RefreshChoicesFor(feat.SchedulerDays), c.Refresh) {
			return nil, fmt.Errorf("%w: refresh %q for %s on this version", ErrBlocklistChoice, c.Refresh, c.Key)
		}
		byKey[c.Key] = c
	}
	var out []BlocklistChoice
	for _, l := range blcatalogue.Lists() {
		if c, ok := byKey[l.Key]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// BlocklistPushKinds is the router's pushed kinds with the two the
// builder needs, in a stable order and without repeats.
func BlocklistPushKinds(kinds []string) []string {
	out := slices.Clone(kinds)
	for _, k := range []string{"raw-rule", addressListCountKind} {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// pushPart is part 1: the push script re-set with the two new kinds
// (BUILD.md, Decisions: the push script embeds the token and MikroView
// keeps only its hash, so the whole script is re-rendered with a token
// this page minted, rather than patched).
func pushPart(push BlocklistPush, dialect string) Part {
	body := PushScript(push.Address, push.Token, BlocklistPushKinds(push.Kinds), dialect)
	// Three one-line commands: the script add (its source's line breaks
	// escaped, see scriptSource), the scheduler, and a run.
	steps := strings.Split(ScheduleCommands(body, dialect), "\n")
	lines := strings.Count(body, "\n") + 1
	return Part{
		Ink:   "push",
		Title: "The push",
		Note: plain("updated once for every list: raw rules join the rule table it sends; lists named " +
			"mv-bl-* are sent as a count (guarded: sets the script the wizard made)"),
		Shown: shownSteps(steps),
		Fold: plain(fmt.Sprintf("   the push script, re-set with the two new kinds · %d lines · "+
			"a new token; the one before stays valid until revoked in Settings", lines)),
		Steps: steps,
	}
}

// loaderPart is a list's fetch-and-load script, saved under mv-bl-<key>.
// first is whether it is the block's first loader: that one's head and
// fold say what every loader does, and the rest say "the same".
func loaderPart(l blcatalogue.List, c BlocklistChoice, feat Features, first bool) Part {
	name := blcatalogue.ListName(l.Key)
	body := loaderScript(l, c, feat)
	step := scriptAdd(name, BlocklistScriptPolicy, body)
	note := "the fetch-and-load script"
	if first {
		note += " (guarded: adds once, then sets)"
	}
	lines := strings.Count(body, "\n") + 1
	return Part{
		Ink:   l.Key,
		Title: l.Name,
		Note:  plain(note),
		Shown: shownSteps([]string{step}),
		Fold:  loaderFold(l, c, feat, first, lines),
		Steps: []string{step},
	}
}

// loaderFold is the drawn summary of a loader's source.
func loaderFold(l blcatalogue.List, c BlocklistChoice, feat Features, first bool, lines int) []Span {
	var s []Span
	text := func(t string) { s = append(s, Span{Text: t}) }
	ver := func(t string) { s = append(s, Span{Text: t, Mark: MarkVersion}) }
	text(fmt.Sprintf("   the source, %d lines: ", lines))
	if first {
		text("/tool fetch " + l.URL)
	} else {
		text("fetch " + l.URL)
	}
	if c.IPv6 && l.URL6 != "" {
		text(" (and " + l.URL6[strings.LastIndex(l.URL6, "/")+1:] + ")")
	}
	if first {
		text(" to a file")
	}
	if !feat.FetchFollowsRedirects {
		text(" ")
		ver("http-max-redirect-count=2")
	}
	skip := func() {
		if feat.LoopContinue {
			text(", a bad line skipped with ")
			ver(":continue")
		} else {
			text(", a bad line skipped with an ")
			ver(":onerror")
			text(" flag")
		}
	}
	if first {
		text(" · read in 32 KiB chunks with /file read · ")
	} else {
		text(" · ")
	}
	switch l.Format {
	case blcatalogue.FormatSpamhausJSON:
		text("each line through ")
		ver(":deserialize from=json")
		skip()
		text(" · refuse wider than /8")
		if c.IPv6 {
			text(" (/16 for IPv6)")
		}
		text(` · the SBL id and "© The Spamhaus Project" kept in each entry's comment`)
	case blcatalogue.FormatDShield:
		text(`tab-separated rows, each start and mask loaded as start/mask, "#" lines skipped`)
		if first {
			skip()
			text(" · refuse wider than /8")
		}
	default:
		text(`one address per line, "#" lines skipped`)
		if first {
			skip()
			text(" · refuse wider than /8")
		}
	}
	if first {
		text(fmt.Sprintf(" · built as %s-next, swapped in only if the fetch and the parse both succeeded",
			blcatalogue.ListName(l.Key)))
	} else {
		text(" · the same chunked read and staging swap")
	}
	return s
}

// schedulePart is a list's scheduler entry.
func schedulePart(l blcatalogue.List, c BlocklistChoice, feat Features) Part {
	name := blcatalogue.ListName(l.Key)
	start := blocklistStartTimes[l.Key]
	when := fmt.Sprintf("interval=%s", refreshInterval[c.Refresh])
	shownWhen := []Span{{Text: when}}
	if c.Refresh == blcatalogue.RefreshWeekdays {
		when += " days=" + weekdays
		shownWhen = append(shownWhen, Span{Text: " "}, Span{Text: "days=" + weekdays, Mark: MarkVersion})
	}
	rest := fmt.Sprintf(` start-time=%s policy=%s on-event="/system script run %s"`, start, BlocklistScriptPolicy, name)
	settings := when + rest
	step := SchedulerAdd(name, settings)
	if feat.SchedulerDays && c.Refresh != blcatalogue.RefreshWeekdays {
		// A list switched off weekdays keeps its days= through a set, and
		// days="" means never (7.24.4); unset is what clears it.
		step = strings.TrimSuffix(step, " }") + fmt.Sprintf(`; /system scheduler unset [find name=%s] days }`, name)
	}
	// The shown line marks days= in the time ink, as drawn.
	var shown []Span
	if before, after, ok := strings.Cut(step, when); ok {
		shown = append(shown, Span{Text: before})
		shown = append(shown, shownWhen...)
		if b2, a2, ok := strings.Cut(after, when); ok {
			shown = append(shown, Span{Text: b2})
			shown = append(shown, shownWhen...)
			shown = append(shown, Span{Text: a2})
		} else {
			shown = append(shown, Span{Text: after})
		}
	} else {
		shown = plain(step)
	}
	return Part{
		Ink:   l.Key,
		Title: l.Name,
		Note:  plain(fmt.Sprintf("the schedule: %s, %s", refreshWords[c.Refresh], start[:5])),
		Shown: [][]Span{shown},
		Steps: []string{step},
	}
}

// rulesPart is a list's raw drop rules, and the removal of a variant the
// operator switched off that the router still holds.
func rulesPart(l blcatalogue.List, c BlocklistChoice, req BlocklistRequest) Part {
	var steps []string
	fams := []struct{ family, menu, list string }{{"ip", "/ip firewall raw", blcatalogue.ListName(l.Key)}}
	if l.IPv6 {
		fams = append(fams, struct{ family, menu, list string }{"ipv6", "/ipv6 firewall raw", blcatalogue.ListName6(l.Key)})
	}
	rules := 0
	for i, f := range fams {
		on := i == 0 || c.IPv6
		for _, dir := range []string{"from", "to"} {
			comment := blocklistRuleComment(l.Key, dir)
			if on && (dir == "from" || c.Direction == blcatalogue.DirectionBoth) {
				steps = append(steps, rawRuleAdd(f.menu, f.list, l.Key, dir, c.Log))
				rules++
				continue
			}
			if req.OnRouter[RawRuleKey(f.family, comment)] {
				steps = append(steps, fmt.Sprintf(`%s remove [find comment="%s"]`, f.menu, comment))
			}
		}
		if i > 0 && !c.IPv6 && req.HeldIPv6[l.Key] {
			steps = append(steps, fmt.Sprintf(`/ipv6 firewall address-list remove [find list=%s]`, f.list))
		}
	}
	return Part{
		Ink:   l.Key,
		Title: l.Name,
		Note:  plain(rulesNote(c, rules)),
		Shown: shownSteps(steps),
		Steps: steps,
	}
}

// rulesNote is the rules part's head, in the drawn words for the two
// drawn shapes (one family "from" and its IPv6 pair; "both" on IPv4).
func rulesNote(c BlocklistChoice, rules int) string {
	logged := "logged"
	if !c.Log {
		logged = "not logged"
	}
	switch {
	case c.Direction == blcatalogue.DirectionBoth && c.IPv6:
		return fmt.Sprintf("four drop rules: from them and to them, IPv4 and IPv6, %s (one prefix; MikroView reads the direction from the line)", logged)
	case c.Direction == blcatalogue.DirectionBoth:
		note := "two drop rules: from them, and to them (one prefix; MikroView reads the direction from the line)"
		if !c.Log {
			note += " · not logged"
		}
		return note
	case c.IPv6:
		return "the drop rules: raw, first, " + logged + " · IPv4 and IPv6"
	default:
		return "the drop rule: raw, first, " + logged
	}
}

// rawRuleAdd is one guarded raw rule. place-before=0 puts it first, but
// on an empty raw table RouterOS refuses it ("no such item", 7.18.2 and
// 7.24.4), so the add branch adds plainly there. place-before belongs to
// the add alone: a rule already on the router keeps its position.
func rawRuleAdd(menu, list, key, dir string, log bool) string {
	match := "src-address-list"
	if dir == "to" {
		match = "dst-address-list"
	}
	logv := "no"
	if log {
		logv = "yes"
	}
	comment := blocklistRuleComment(key, dir)
	props := fmt.Sprintf(`chain=prerouting %s=%s action=drop log=%s log-prefix="%s"`, match, list, logv, BlocklistLogPrefix(key))
	return fmt.Sprintf(`:if ([:len [%[1]s find comment="%[2]s"]] = 0) do={ :if ([:len [%[1]s find]] = 0) do={ %[1]s add %[3]s comment="%[2]s" } else={ %[1]s add %[3]s comment="%[2]s" place-before=0 } } else={ %[1]s set [find comment="%[2]s"] %[3]s disabled=no }`,
		menu, comment, props)
}

// runNowPart runs every chosen loader once, so the lists load now rather
// than at their first scheduled time.
func runNowPart(chosen []BlocklistChoice) Part {
	var runs []string
	earliest := ""
	for _, c := range chosen {
		runs = append(runs, "/system script run "+blcatalogue.ListName(c.Key))
		if t := blocklistStartTimes[c.Key]; earliest == "" || t < earliest {
			earliest = t
		}
	}
	step := strings.Join(runs, "; ")
	return Part{
		Note:  plain(fmt.Sprintf("Run %s now, rather than waiting for %s", howMany(len(chosen)), earliest[:5])),
		Shown: shownSteps([]string{step}),
		Steps: []string{step},
	}
}

func howMany(n int) string {
	switch n {
	case 1:
		return "it"
	case 2:
		return "both"
	}
	words := []string{"", "", "", "three", "four", "five", "six", "seven"}
	if n < len(words) {
		return "all " + words[n]
	}
	return fmt.Sprintf("all %d", n)
}

// plain is one unmarked span.
func plain(s string) []Span { return []Span{{Text: s}} }

// shownSteps is how the page shows steps: one line each, with every
// source="..." folded to an ellipsis.
func shownSteps(steps []string) [][]Span {
	var out [][]Span
	for _, s := range steps {
		out = append(out, elideSources(s))
	}
	return out
}

// elideSources splits a command at each source="..." and folds its body
// to "…". A source body escapes every quote it holds (scriptSource), so
// the first unescaped quote after source=" closes it.
func elideSources(cmd string) []Span {
	var out []Span
	const open = `source="`
	for {
		i := strings.Index(cmd, open)
		if i < 0 {
			break
		}
		out = append(out, Span{Text: cmd[:i+len(open)]}, Span{Text: "…", Mark: MarkElided})
		rest := cmd[i+len(open):]
		end := closingQuote(rest)
		cmd = rest[end:]
	}
	return append(out, Span{Text: cmd})
}

// closingQuote is the index of the first quote in s not escaped by a
// backslash, or len(s).
func closingQuote(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return len(s)
}

// BlocklistUndo removes everything the block left on the router for one
// list: its rules by comment, its scheduler and script by name, its
// address lists (with the staging and IPv6 ones) and any fetched file by
// ^mv-bl-<key> -- no key is a prefix of another (blcatalogue's
// TestListNames), so the pattern sweeps that list alone. It never
// touches the push script (BUILD.md, Decisions). The drawn undo
// (round-2 builder.html, undoSpamhaus), generalised.
func BlocklistUndo(key string) (string, error) {
	l, ok := blcatalogue.Lookup(key)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrBlocklistUnknownKey, key)
	}
	name := blcatalogue.ListName(l.Key)
	lines := []string{
		fmt.Sprintf(`/ip firewall raw remove [find comment="%s"]`, blocklistRuleComment(l.Key, "from")),
		fmt.Sprintf(`/ip firewall raw remove [find comment="%s"]`, blocklistRuleComment(l.Key, "to")),
	}
	if l.IPv6 {
		lines = append(lines,
			fmt.Sprintf(`/ipv6 firewall raw remove [find comment="%s"]`, blocklistRuleComment(l.Key, "from")),
			fmt.Sprintf(`/ipv6 firewall raw remove [find comment="%s"]`, blocklistRuleComment(l.Key, "to")))
	}
	lines = append(lines,
		fmt.Sprintf(`/system scheduler remove [find name=%s]`, name),
		fmt.Sprintf(`/system script remove [find name=%s]`, name),
		fmt.Sprintf(`/ip firewall address-list remove [find list~"^%s"]`, name),
	)
	if l.IPv6 {
		lines = append(lines, fmt.Sprintf(`/ipv6 firewall address-list remove [find list~"^%s"]`, name))
	}
	lines = append(lines, fmt.Sprintf(`/file remove [find name~"^%s"]`, name))
	return strings.Join(lines, "\n"), nil
}

// RouterOSUpgradeCommands is what the builder's page prints for a router
// below MikroView's floor (round 2, builder.html's bodyBelowFloor): check
// for the newest release on the router's channel, then install it. The
// router reboots; MikroView never runs either line.
func RouterOSUpgradeCommands() string {
	return "/system package update check-for-updates\n/system package update install"
}

// BlocklistDisableAll stops every blocklist rule dropping without
// removing anything (round 2's undo note): the rules stay, disabled, and
// a re-paste of the block turns them back on (its set branches name
// disabled=no).
func BlocklistDisableAll() string {
	return `/ip firewall raw disable [find comment~"^mikroview blocklist"]`
}

// BlocklistUndoAll removes everything the builder left on the router,
// for every list at once: the foot's "Undo everything". By prefix, so a
// list the catalogue has since dropped goes too. The drop list's own
// rule ("mikroview drop list") and the push script (mv-push) do not
// match either pattern.
func BlocklistUndoAll() string {
	return strings.Join([]string{
		`/ip firewall raw remove [find comment~"^mikroview blocklist: "]`,
		`/ipv6 firewall raw remove [find comment~"^mikroview blocklist: "]`,
		`/system scheduler remove [find name~"^` + blocklistListPrefix + `"]`,
		`/system script remove [find name~"^` + blocklistListPrefix + `"]`,
		`/ip firewall address-list remove [find list~"^` + blocklistListPrefix + `"]`,
		`/ipv6 firewall address-list remove [find list~"^` + blocklistListPrefix + `"]`,
		`/file remove [find name~"^` + blocklistListPrefix + `"]`,
	}, "\n")
}
