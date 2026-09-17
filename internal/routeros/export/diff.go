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
	raw := strings.Split(text, "\n")
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	out := make([]diffSide, 0, len(raw))
	for i, l := range raw {
		if isDateHeader(l) {
			continue
		}
		out = append(out, diffSide{line: i + 1, text: strings.TrimRight(l, "\r")})
	}
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
// Each round of the search is snapshotted so the edit script can be
// walked back out of it; a round only ever holds diagonals -d..d, so
// the snapshots cost about d² ints in total rather than d×(n+m).
func myers(a, b []diffSide) ([]DiffLine, bool) {
	n, m := len(a), len(b)
	limit := n + m
	if limit > maxDiffEdits {
		limit = maxDiffEdits
	}
	offset := n + m
	v := make([]int, 2*(n+m)+1)
	trace := make([][]int, 0, limit+1)

	for d := 0; d <= limit; d++ {
		trace = append(trace, append([]int(nil), v[offset-d:offset+d+1]...))
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
				return backtrack(trace, a, b, d), true
			}
		}
	}
	return nil, false
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
