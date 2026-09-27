// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Change is one thing Carry forward did to a config text, for the
// editor's Problems rail to list (#1347).
type Change struct {
	// Kind is "header" (the four header lines added or brought up to
	// date), "removed" (a key this build no longer reads was dropped),
	// "renamed" (a key was given its new name, value unchanged) or
	// "moved" (a top-level section was moved into its place in the
	// layout). The API adds "snapshot" for the copy it keeps of the text
	// before the rewrite (internal/api/configeditor.go).
	Kind string `json:"kind"`
	Key  string `json:"key"`
	// To is the new name of a renamed key; empty otherwise.
	To string `json:"to"`
	// Line is the 1-based line in the text Carry forward was given, 0
	// when the change is not about one line (the header).
	Line int    `json:"line"`
	Note string `json:"note"`
}

// layoutSections is layout 1 (owner, 2026-09-25): every top-level key in
// the order deploy/config.example.yaml lays it out, grouped into its
// three sections. TestExampleConfigIsInTheLayout holds the example to
// this table and TestLayoutCoversEveryTopLevelKey holds the table to
// Config, so neither can gain a key the other lacks.
var layoutSections = []struct {
	Name   string
	Banner string
	Keys   []string
}{
	{
		Name: "the settings that must be set",
		Banner: rulerLine + "\n" +
			"#  REQUIRED -- set these for your install\n" +
			rulerLine + "\n" +
			"# MikroView starts with no config at all; the one thing it cannot guess\n" +
			"# is which router is which, so name yours under devices: below.\n" +
			"# Behind a reverse proxy only, also set listen.trustedProxies (in the\n" +
			"# defaults section at the bottom).",
		Keys: []string{"devices"},
	},
	{
		Name: "optional features",
		Banner: rulerLine + "\n" +
			"#  OPTIONAL FEATURES -- uncomment a block to use it\n" +
			rulerLine,
		Keys: []string{
			"ui", "geoip", "reputation", "history", "backup", "blocklist", "netClass", "oui",
			"ruleNames", "hostNames", "publicUrl", "notify", "oidc", "postgres",
		},
	},
	{
		Name: "the defaults",
		Banner: rulerLine + "\n" +
			rulerLine + "\n" +
			"# ==                                                                  ==\n" +
			"# ==  DEFAULTS -- ONLY UNCOMMENT A SETTING BELOW TO CHANGE IT         ==\n" +
			"# ==                                                                  ==\n" +
			"# ==  Everything from here down restates a built-in default. The live ==\n" +
			"# ==  listen: and store: blocks do too: leaving them as they are, or  ==\n" +
			"# ==  deleting them, changes nothing.                                 ==\n" +
			"# ==                                                                  ==\n" +
			rulerLine + "\n" +
			rulerLine,
		Keys: []string{
			"listen", "store", "watchlist", "log", "flags", "deviceMac", "deviceRegistry", "engine",
			"snapshot", "droplist", "prefs", "entities", "coverage", "hosts", "seen", "baseline",
			"audit", "setup", "auth", "tls",
		},
	},
}

// rulerLine opens every banner the layout draws. Carry forward treats a
// comment paragraph starting with exactly this line as layout, not as
// the operator's own words, and redraws it: that is how an earlier
// layout's banners (and the pre-#1347 "HEADER -- DO NOT DELETE" block)
// are replaced rather than duplicated.
const rulerLine = "# ======================================================================"

// commentedKeyPattern matches a commented-out top-level key -- "# oidc:"
// or "#oidc:" -- and not an indented one ("#   smtp:"), which belongs to
// the block above it.
var commentedKeyPattern = regexp.MustCompile(`^#\s?([a-zA-Z][a-zA-Z0-9]*):(\s|$)`)

// layoutPosition is where a top-level key goes: its section, and its
// place within the section. A key the layout does not know (a typo,
// which ValidateText reports) goes after everything else.
func layoutPosition(key string) (section, index int) {
	for s, sec := range layoutSections {
		for i, k := range sec.Keys {
			if k == key {
				return s, i
			}
		}
	}
	return len(layoutSections), 0
}

// CarryForward rewrites a config text for this build (#1347): keys a
// release removed are dropped, renamed ones get their new name, every
// top-level section moves into its place in layout 1 under the layout's
// banners, and the four header lines are written for CurrentSchema and
// writtenBy (the running release, "v0.6.1"). Every value, comment and
// blank line of the operator's own is kept; what changed is listed, and
// the problems are ValidateText's verdict on the result.
//
// It works on the text, not on a re-encoding of parsed YAML: yaml.v3 can
// re-emit a document, but it moves comments between nodes and loses
// blank lines, and "keeps the operator's comments" has to mean all of
// them, where they were. The parser is used only to find where each key
// sits. A section is the key's lines plus the comment lines directly
// above it; a commented-out section ("# oidc:" and the lines under it)
// is a section too, so an example-derived file keeps its commented
// blocks with the features they describe.
//
// Text that does not parse, or whose top level is not a block of
// "key: value" lines, is returned unchanged with the problems saying
// why: rearranging a file whose structure cannot be read would be
// guessing.
func CarryForward(text, writtenBy string) (string, []Change, []TextProblem) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return text, nil, ValidateText(text)
	}
	if root := documentRoot(&doc); root != nil && (root.Kind != yaml.MappingNode || root.Style&yaml.FlowStyle != 0) {
		return text, nil, append(ValidateText(text), TextProblem{
			Line: root.Line, Severity: SeverityFatal.String(),
			Message: "Carry forward needs the file's top level to be settings, one per line (key: value) -- nothing was changed",
		})
	}

	lines := splitLines(text)
	origLine := make([]int, len(lines))
	for i := range origLine {
		origLine[i] = i + 1
	}

	var changes []Change
	var stuck []TextProblem
	if old, ok := ParseHeader(text); ok {
		if old.Schema != CurrentSchema || old.WrittenBy != "v"+strings.TrimPrefix(writtenBy, "v") || old.Layout != CurrentLayout {
			changes = append(changes, Change{Kind: "header", Note: fmt.Sprintf(
				"header brought up to date: schema %d -> %d, written by %s -> %s, layout %d -> %d",
				old.Schema, CurrentSchema, old.WrittenBy, "v"+strings.TrimPrefix(writtenBy, "v"), old.Layout, CurrentLayout)})
		}
	} else {
		changes = append(changes, Change{Kind: "header", Note: fmt.Sprintf(
			"added the four header lines: schema %d, written by %s, layout %d", CurrentSchema, "v"+strings.TrimPrefix(writtenBy, "v"), CurrentLayout)})
	}

	// Removed and renamed keys, found by path anywhere in the tree.
	drop := map[int]bool{}
	owners := nodeLineOwners(&doc)
	var visit func(n *yaml.Node, prefix string)
	visit = func(n *yaml.Node, prefix string) {
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			path := k.Value
			if prefix != "" {
				path = prefix + "." + k.Value
			}
			removed, ok := removedOrRenamedKeys[path]
			if !ok {
				visit(v, path)
				continue
			}
			first, last := k.Line, maxNodeLine(v, k.Line)
			if n.Style&yaml.FlowStyle != 0 || sharesLines(owners, first, last, k, v) {
				stuck = append(stuck, TextProblem{Line: k.Line, Key: path, Severity: SeverityFatal.String(), Message: fmt.Sprintf(
					"%s shares its line with other settings, so Carry forward left it for you to take out by hand: it was removed in %s: %s",
					path, removed.Version, removed.Why)})
				continue
			}
			if to := removed.RenamedTo; to != "" && parentPath(to) == prefix && renameKeyInPlace(lines, k, lastPathPart(to)) {
				changes = append(changes, Change{Kind: "renamed", Key: path, To: to, Line: k.Line, Note: fmt.Sprintf(
					"renamed in %s: %s", removed.Version, removed.Why)})
				continue
			}
			for l := first; l <= last; l++ {
				drop[l-1] = true
			}
			changes = append(changes, Change{Kind: "removed", Key: path, To: removed.RenamedTo, Line: k.Line, Note: fmt.Sprintf(
				"removed in %s: %s", removed.Version, removed.Why)})
		}
	}
	if root := documentRoot(&doc); root != nil {
		visit(root, "")
	}
	if len(drop) > 0 {
		var kept []string
		var keptOrig []int
		for i, l := range lines {
			if !drop[i] {
				kept = append(kept, l)
				keptOrig = append(keptOrig, origLine[i])
			}
		}
		lines, origLine = kept, keptOrig
	}

	// Where each top-level section starts, live or commented out.
	var doc2 yaml.Node
	if err := yaml.Unmarshal([]byte(joinLines(lines)), &doc2); err != nil {
		return text, nil, append(ValidateText(text), TextProblem{Severity: SeverityFatal.String(),
			Message: "Carry forward could not read the file back after removing old keys (" + err.Error() + ") -- nothing was changed"})
	}
	topLevel := map[string]bool{}
	for _, sec := range layoutSections {
		for _, k := range sec.Keys {
			topLevel[k] = true
		}
	}
	anchorKey := map[int]string{}
	if root := documentRoot(&doc2); root != nil {
		for i := 0; i+1 < len(root.Content); i += 2 {
			anchorKey[root.Content[i].Line-1] = root.Content[i].Value
		}
	}
	for i, l := range lines {
		if _, live := anchorKey[i]; live {
			continue
		}
		if m := commentedKeyPattern.FindStringSubmatch(l); m != nil && topLevel[m[1]] && nextCodeLineIsTopLevel(lines, i) {
			anchorKey[i] = m[1]
		}
	}

	// Layout lines to redraw rather than keep: the header, and every
	// comment paragraph that opens with the ruler.
	chrome := map[int]bool{}
	if start, end, _, ok := headerSpan(lines); ok {
		for i := start; i < end; i++ {
			chrome[i] = true
		}
	}
	for i := 0; i < len(lines); i++ {
		if lines[i] != rulerLine {
			continue
		}
		for ; i < len(lines) && isTopComment(lines[i]); i++ {
			if _, anchor := anchorKey[i]; anchor {
				break
			}
			chrome[i] = true
		}
	}

	// Sections: each anchor plus the comment lines directly above it,
	// through to where the next section starts. An anchor whose comment
	// lines run straight up into the section above (no blank line
	// between, as "# ruleNames:" and "# hostNames:" share one commented
	// block) stays part of that section rather than splitting it.
	type section struct {
		key    string
		start  int
		lines  []string
		origAt int
	}
	var anchors []int
	for i := range anchorKey {
		anchors = append(anchors, i)
	}
	sort.Ints(anchors)
	var sections []section
	for _, a := range anchors {
		start := a
		merged := false
		for start > 0 && isTopComment(lines[start-1]) && !chrome[start-1] {
			if _, anchor := anchorKey[start-1]; anchor {
				merged = true
				break
			}
			start--
		}
		if merged {
			// The anchor above was handled first (anchors are in line
			// order), so its section is the last one appended.
			continue
		}
		sections = append(sections, section{key: anchorKey[a], start: start, origAt: origLine[a]})
	}
	end := len(lines)
	for i := len(sections) - 1; i >= 0; i-- {
		for l := sections[i].start; l < end; l++ {
			if !chrome[l] {
				sections[i].lines = append(sections[i].lines, lines[l])
			}
		}
		end = sections[i].start
	}
	var preamble []string
	for l := 0; l < end; l++ {
		if !chrome[l] {
			preamble = append(preamble, lines[l])
		}
	}

	// Order by the layout, keeping the file's own order among sections
	// that share a place (a live block and a commented copy of it).
	order := make([]int, len(sections))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		sa, ia := layoutPosition(sections[order[a]].key)
		sb, ib := layoutPosition(sections[order[b]].key)
		if sa != sb {
			return sa < sb
		}
		return ia < ib
	})
	rank := make([]int, len(sections))
	for pos, i := range order {
		rank[i] = pos
	}
	stay := longestIncreasing(rank)
	for i, s := range sections {
		if stay[i] {
			continue
		}
		sec, _ := layoutPosition(s.key)
		where := "after the layout's sections (MikroView does not know this key)"
		if sec < len(layoutSections) {
			where = "into " + layoutSections[sec].Name
		}
		changes = append(changes, Change{Kind: "moved", Key: s.key, Line: s.origAt, Note: "moved " + where})
	}

	out := []string{HeaderLines(Header{Schema: CurrentSchema, WrittenBy: writtenBy, Layout: CurrentLayout}), ""}
	if body := trimBlank(preamble); len(body) > 0 {
		out = append(out, body...)
		out = append(out, "")
	}
	next := 0
	for s := range layoutSections {
		out = append(out, layoutSections[s].Banner, "")
		for ; next < len(order); next++ {
			if sec, _ := layoutPosition(sections[order[next]].key); sec != s {
				break
			}
			out = append(out, trimBlank(sections[order[next]].lines)...)
			out = append(out, "")
		}
	}
	for ; next < len(order); next++ {
		out = append(out, trimBlank(sections[order[next]].lines)...)
		out = append(out, "")
	}
	result := joinLines(collapseBlankRuns(trimBlank(splitLines(strings.Join(out, "\n")))))

	sort.SliceStable(changes, func(a, b int) bool { return changes[a].Line < changes[b].Line })
	return result, changes, append(stuck, ValidateText(result)...)
}

// documentRoot is the top-level node of doc, nil for an empty document.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return nil
}

// maxNodeLine is the last line any node under n starts on, at least min.
func maxNodeLine(n *yaml.Node, min int) int {
	m := min
	if n.Line > m {
		m = n.Line
	}
	for _, c := range n.Content {
		m = maxNodeLine(c, m)
	}
	return m
}

// nodeLineOwners maps each line to the scalars (keys and values) that
// start on it. Mappings and lists are left out: one starts on the line
// of its own first entry, so counting it would make every first key look
// as though it shared its line with its parent.
func nodeLineOwners(doc *yaml.Node) map[int][]*yaml.Node {
	out := map[int][]*yaml.Node{}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode || n.Kind == yaml.AliasNode {
			out[n.Line] = append(out[n.Line], n)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// sharesLines reports whether any node outside the key k and its value v
// starts on a line from first to last -- the case where deleting those
// lines would take another setting with them.
func sharesLines(owners map[int][]*yaml.Node, first, last int, k, v *yaml.Node) bool {
	inside := map[*yaml.Node]bool{k: true}
	var mark func(n *yaml.Node)
	mark = func(n *yaml.Node) {
		inside[n] = true
		for _, c := range n.Content {
			mark(c)
		}
	}
	mark(v)
	for l := first; l <= last; l++ {
		for _, n := range owners[l] {
			if !inside[n] {
				return true
			}
		}
	}
	return false
}

// renameKeyInPlace swaps key k's name for name on its own line, leaving
// the value and any comment on that line as they were. False when the
// text at k's position is not the plain key it should be.
func renameKeyInPlace(lines []string, k *yaml.Node, name string) bool {
	l, c := k.Line-1, k.Column-1
	if l < 0 || l >= len(lines) || k.Style != 0 {
		return false
	}
	row := []rune(lines[l])
	if c < 0 || c+len([]rune(k.Value)) > len(row) || string(row[c:c+len([]rune(k.Value))]) != k.Value {
		return false
	}
	lines[l] = string(row[:c]) + name + string(row[c+len([]rune(k.Value)):])
	return true
}

func parentPath(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

func lastPathPart(path string) string {
	return path[strings.LastIndex(path, ".")+1:]
}

// isTopComment reports whether line is a comment starting in column 0.
func isTopComment(line string) bool { return strings.HasPrefix(line, "#") }

// nextCodeLineIsTopLevel reports whether the first line after i that is
// neither blank nor a comment starts in column 0 (or there is none) --
// that is, whether a commented-out key at i sits between top-level
// sections rather than in the middle of a live one, where moving it
// would take the live section's remaining lines with it.
func nextCodeLineIsTopLevel(lines []string, i int) bool {
	for _, l := range lines[i+1:] {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// A list entry may sit in column 0 under its key ("devices:"
		// then "- id: ..."), and belongs to the section above.
		return !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, "-")
	}
	return true
}

// trimBlank drops leading and trailing blank lines.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// collapseBlankRuns turns every run of blank lines into one, so a banner
// or key taken out does not leave a gap behind it.
func collapseBlankRuns(lines []string) []string {
	var out []string
	for i, l := range lines {
		if strings.TrimSpace(l) == "" && i > 0 && strings.TrimSpace(lines[i-1]) == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// longestIncreasing marks the elements of seq on one longest strictly
// increasing subsequence: the sections that can stay where they are
// while the rest move around them, which is the fewest moves to report.
func longestIncreasing(seq []int) []bool {
	n := len(seq)
	length := make([]int, n)
	prev := make([]int, n)
	best := -1
	for i := range seq {
		length[i], prev[i] = 1, -1
		for j := 0; j < i; j++ {
			if seq[j] < seq[i] && length[j]+1 > length[i] {
				length[i], prev[i] = length[j]+1, j
			}
		}
		if best < 0 || length[i] > length[best] {
			best = i
		}
	}
	on := make([]bool, n)
	for i := best; i >= 0; i = prev[i] {
		on[i] = true
	}
	return on
}
