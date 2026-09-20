// SPDX-License-Identifier: AGPL-3.0-only

package export

// The line diff two stored exports are compared with (#895). It runs
// against the redacted copies the vault holds, so nothing secret can
// reach a diff that never had it to begin with.
//
// One line is ignored: the export's own date header, `# nov/02/2026
// 03:00:01 by RouterOS 7.23.3`. It changes on every single export and
// says nothing about the configuration, so a nightly comparison with
// it in would never be empty. Nothing else is ignored -- not the
// redaction marker, whose count changing is a real fact about the
// router, and not comments, which are where an operator writes down
// what a rule is for.

import "strings"

// Diff op values. Two, because the answer is two-coloured: a line the
// older export had and the newer does not, and a line the newer has
// and the older did not. A line both share is not in the result at all.
const (
	DiffRemoved = "-"
	DiffAdded   = "+"
)

// DiffLine is one line the two exports do not share. Line is 1-based,
// counted in whichever side the line belongs to -- the older one for a
// removal, the newer for an addition.
type DiffLine struct {
	Op   string `json:"op"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Diff limits. An export is capped at 16 MiB by the vault, which is
// far more lines than a diff is readable at, and Myers' cost grows
// with the number of edits rather than the file: a pair with nothing
// in common is the expensive case, and is also the case where a line
// diff tells an operator nothing a "this is a different config" would
// not. Past either limit the answer is the whole of one side removed
// and the whole of the other added, which is true, cheap and honest.
const (
	maxDiffLines = 50000
	maxDiffEdits = 2000
)

// Diff compares two exports line by line and returns only the lines
// they do not share, oldest side's removals and newest side's additions
// interleaved in the order they occur. An identical pair (bar the date
// header) returns an empty slice.
func Diff(from, to string) []DiffLine {
	// Cheap, allocation-free line counts first (#1280): diffSides used
	// to build a full []diffSide for both sides before maxDiffLines was
	// ever consulted, so two exports near the 16 MiB vault cap, crafted
	// as millions of short lines, allocated hundreds of MB per side
	// just to discover the pair was too big to diff at all. If either
	// raw side is already past the cap on its own, there is no need to
	// build either side in full: go straight to the same wholesale
	// answer, assembled by walking the text once per side. This trades
	// a rare case -- a pair this large that also happens to share a
	// long common prefix or suffix -- for never paying that allocation
	// unconditionally; a router export legitimately near the cap is not
	// expected to collapse to a small diff anyway.
	if diffLineCount(from) > maxDiffLines || diffLineCount(to) > maxDiffLines {
		return wholesaleText(from, to)
	}

	a := diffSides(from)
	b := diffSides(to)

	// Common prefix and suffix are the bulk of any two exports of the
	// same router, and stripping them is what keeps the edit distance
	// -- and so the work below -- proportional to what actually
	// changed.
	head := 0
	for head < len(a) && head < len(b) && a[head].text == b[head].text {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head &&
		a[len(a)-1-tail].text == b[len(b)-1-tail].text {
		tail++
	}
	ma := a[head : len(a)-tail]
	mb := b[head : len(b)-tail]

	if len(ma) == 0 && len(mb) == 0 {
		return []DiffLine{}
	}
	if len(ma)+len(mb) > maxDiffLines {
		return wholesale(ma, mb)
	}
	if out, ok := myers(ma, mb); ok {
		return out
	}
	return wholesale(ma, mb)
}

// diffSide is one line with the number it carries in its own file, kept
// alongside the text because the common-prefix trim above throws the
// index away.
type diffSide struct {
	line int
	text string
}

// diffSides splits an export into comparable lines, dropping the date
// header. A trailing newline's empty last line is dropped too: it is an
// artefact of the file ending properly, not a line either export
// "has".
func diffSides(text string) []diffSide {
	out := make([]diffSide, 0, diffLineCount(text))
	forEachLine(text, func(line int, t string) {
		out = append(out, diffSide{line: line, text: t})
	})
	return out
}

// forEachLine walks text's lines in the same order and with the same
// rules diffSides applies -- \r trimmed, the date header skipped, a
// trailing newline's empty last line not counted -- without ever
// holding more than one line at a time. It is what lets a caller that
// only needs a count or a single pass over the text (diffLineCount,
// wholesaleText) avoid diffSides' per-line []diffSide allocation
// entirely.
func forEachLine(text string, f func(line int, text string)) {
	if strings.HasSuffix(text, "\n") {
		text = text[:len(text)-1]
	}
	if text == "" {
		return
	}
	line := 0
	for {
		line++
		l := text
		i := strings.IndexByte(text, '\n')
		if i >= 0 {
			l = text[:i]
		}
		if !isDateHeader(l) {
			f(line, strings.TrimRight(l, "\r"))
		}
		if i < 0 {
			return
		}
		text = text[i+1:]
	}
}

// diffLineCount is the count forEachLine would call f for, computed by
// scanning for newlines rather than walking line by line -- the cheap
// check Diff needs before deciding whether a side is worth building at
// all. It over-counts by the number of date header lines (at most one
// in practice), the same slack diffSides' own capacity estimate always
// had; that cannot matter against a 50000-line cap.
func diffLineCount(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		n--
	}
	return n
}

// wholesaleText is wholesale's early-exit twin (#1280): once Diff has
// already decided from diffLineCount alone that a side is past
// maxDiffLines, it answers directly from the raw text so that
// diffSides' allocation, the common-prefix/suffix trim and Myers never
// run. It applies forEachLine's identical line rules to each side in
// turn, so for any pair where trimming would not have removed
// anything -- the case a pair this far past the cap realistically is
// -- the result is byte-for-byte what wholesale(diffSides(from),
// diffSides(to)) returns today.
func wholesaleText(from, to string) []DiffLine {
	out := make([]DiffLine, 0, diffLineCount(from)+diffLineCount(to))
	forEachLine(from, func(line int, text string) {
		out = append(out, DiffLine{Op: DiffRemoved, Line: line, Text: text})
	})
	forEachLine(to, func(line int, text string) {
		out = append(out, DiffLine{Op: DiffAdded, Line: line, Text: text})
	})
	return out
}

// isDateHeader reports the export's own "# <date> by RouterOS <ver>"
// header line -- the one line Diff ignores. It is recognised by the
// same "by RouterOS" phrasing Parse already reads the version out of,
// rather than by a second guess at RouterOS's date format.
func isDateHeader(line string) bool {
	t := strings.TrimSpace(strings.TrimRight(line, "\r"))
	return strings.HasPrefix(t, "#") && versionPattern.MatchString(t)
}

// wholesale is the fallback: everything on the left went, everything on
// the right arrived.
func wholesale(a, b []diffSide) []DiffLine {
	out := make([]DiffLine, 0, len(a)+len(b))
	for _, s := range a {
		out = append(out, DiffLine{Op: DiffRemoved, Line: s.line, Text: s.text})
	}
	for _, s := range b {
		out = append(out, DiffLine{Op: DiffAdded, Line: s.line, Text: s.text})
	}
	return out
}

// myers is the standard greedy shortest-edit-script search (Myers
// 1986). It returns ok=false once the edit distance passes
// maxDiffEdits, leaving Diff to answer wholesale rather than keep
// paying for a comparison nobody could read.
//
// A round of the search only ever holds diagonals -d..d, so snapshotting
// every round for backtracking costs about d² ints in total -- for a
// pair whose edit distance turns out to exceed maxDiffEdits, that ran to
// something like 39 MiB, built one round at a time and then thrown away
// the moment the search gave up and Diff fell back to wholesale (#1269).
// myersRun below finds the distance first, with no snapshot recorded at
// all; only once that distance is known to be within budget is it run a
// second time, up to that known depth, to build the trace worth having.
func myers(a, b []diffSide) ([]DiffLine, bool) {
	n, m := len(a), len(b)
	limit := n + m
	if limit > maxDiffEdits {
		limit = maxDiffEdits
	}

	d, ok := myersRun(a, b, limit, nil)
	if !ok {
		return nil, false
	}
	trace := make([][]int, 0, d+1)
	if _, ok := myersRun(a, b, d, &trace); !ok {
		// Unreachable: myersRun is deterministic given the same inputs,
		// so having already found a solution at round d, rerunning up
		// to exactly d cannot fail to find it again. Guarded rather
		// than trusted blindly.
		return nil, false
	}
	return backtrack(trace, a, b, d), true
}

// myersRun performs the search up to `limit` rounds, returning the
// round it found a solution at (and true), or false if none exists by
// then. When trace is non-nil, each round's diagonal array is appended
// to it first, exactly as myers used to record unconditionally -- the
// snapshot a caller needs to backtrack from once it has decided the
// answer is worth building.
func myersRun(a, b []diffSide, limit int, trace *[][]int) (int, bool) {
	n, m := len(a), len(b)
	offset := n + m
	v := make([]int, 2*(n+m)+1)

	for d := 0; d <= limit; d++ {
		if trace != nil {
			*trace = append(*trace, append([]int(nil), v[offset-d:offset+d+1]...))
		}
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x].text == b[y].text {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return d, true
			}
		}
	}
	return 0, false
}

// backtrack walks myers' snapshots from the end back to the start,
// collecting one DiffLine per edit, and reverses the result.
func backtrack(trace [][]int, a, b []diffSide, d int) []DiffLine {
	x, y := len(a), len(b)
	var rev []DiffLine
	for ; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[d+k-1] < v[d+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[d+prevK]
		prevY := prevX - prevK
		// Walk back down the diagonal (the lines both sides share)
		// before recording the one edit that got us onto it.
		for x > prevX && y > prevY {
			x--
			y--
		}
		if x == prevX {
			y--
			rev = append(rev, DiffLine{Op: DiffAdded, Line: b[y].line, Text: b[y].text})
		} else {
			x--
			rev = append(rev, DiffLine{Op: DiffRemoved, Line: a[x].line, Text: a[x].text})
		}
	}
	out := make([]DiffLine, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}
