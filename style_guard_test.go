// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestNoStaticStyleAttributesInSvelte guards a defect that nothing else in
// the suite can see: the app's Content-Security-Policy is
// `default-src 'self'` with no style-src carve-out, so a static
// style="..." attribute written directly into markup is an inline style,
// and Firefox rejects inline styles under that policy. Chromium tolerates
// the same markup.
//
// On 2026-08-30 (#645) the door's rain strokes each carried a static
// style="left: ...px" attribute. All seventeen collapsed onto one spot in
// the owner's Firefox. live-check drives Chromium, vitest never renders a
// real browser, and the review screenshots were Chromium too -- so
// nothing in the suite noticed. Fixed for that component in f996525 by
// moving the geometry into the stylesheet as nth-child rules. This test
// is the guard that stops the same defect recurring elsewhere; see #659.
//
// House rule this test enforces:
//   - interpolated style attributes (style="left: {x}px", style={expr})
//     stay allowed: Svelte applies them from script at runtime as a
//     property write, not as parsed markup, and Firefox accepts that
//     under this CSP.
//   - style: directives (style:left="{x}px", style:left={x}) are the
//     preferred spelling for new code -- same runtime behaviour, and the
//     property being set is explicit in the attribute name.
//   - a static style="..." attribute -- no {interpolation} anywhere in
//     its value -- is banned outright, because it is parsed as markup
//     and Firefox refuses it under this CSP.
//
// Matching is deliberately crude, in the same spirit as
// injection_sinks_test.go: a regex over raw file text, not a real Svelte
// template parse. Consequences accepted on purpose:
//   - style="..." (or style='...') inside an HTML comment, or inside a
//     string literal in a <script> block, trips this test exactly as if
//     it were live markup. That is a false positive, but the fix is a
//     one-line reword, and teaching this test to parse comments and JS
//     string literals is real parser work to save someone a trivial
//     edit. Checked: nothing in this tree currently has "style=" inside
//     a <!-- --> comment or a .ts/.svelte string, so the crude version
//     costs nothing today.
//   - a hyphenated attribute ending in "-style" (there are none in this
//     tree, e.g. a hypothetical data-style="...") would also match: the
//     word-boundary check does not require whitespace before "style".
//     Same trade -- crude on purpose, over-inclusive rather than blind.
//   - multi-line attribute values and single-quoted style='...' are
//     matched too: the character classes below ([^"]*, [^']*) span
//     newlines in Go's regexp package, so splitting a value across lines
//     is not a way to dodge the check.
var staticStyleAttr = regexp.MustCompile(`\bstyle\s*=\s*("[^"]*"|'[^']*')`)

func TestNoStaticStyleAttributesInSvelte(t *testing.T) {
	var failures []string

	for _, rel := range sources(t, "frontend/src/*.svelte") {
		data, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)

		for _, loc := range staticStyleAttr.FindAllStringIndex(src, -1) {
			match := src[loc[0]:loc[1]]
			if strings.Contains(match, "{") {
				continue // interpolated: Svelte sets it from script, not parsed as markup
			}
			line := strings.Count(src[:loc[0]], "\n") + 1
			failures = append(failures, fmt.Sprintf("%s:%d: %s", rel, line, oneLine(match)))
		}
	}

	if len(failures) > 0 {
		t.Errorf("static style attribute(s) found -- these are inline styles and Firefox "+
			"rejects them under this CSP (default-src 'self', no style-src carve-out), even "+
			"though Chromium (live-check) and vitest do not catch it. Use an interpolated "+
			"style attribute or a style: directive instead, and move static geometry into "+
			"the component stylesheet. See #659.\n%s",
			strings.Join(failures, "\n"))
	}
}

// oneLine collapses a matched attribute onto a single line for the
// failure report; a legal multi-line style value would otherwise make an
// unreadable Errorf line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
