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

// TestLaneInksAreDefinedTokens guards the topography's lane palette
// (#715 item 11): every ink the lane row wears must be a token app.css
// actually defines, and none of them may be --marked.
//
// The two halves catch different mistakes. An undefined token fails
// silently in the browser -- the fill simply does not paint, and no
// unit test that asserts on the attribute string can see it, because
// the attribute is exactly right. --marked is the specific collision
// Fable ruled on: it is the watch chips' fill and the watch half of
// every aggregate bar on this same screen, so a lane wearing it made
// one colour carry two meanings on one map.
//
// Written as a Go test because it has to read two files in different
// languages. Vite's ?raw import returns an empty string for a .css
// file, so the frontend suite cannot check the stylesheet's own text.
func TestLaneInksAreDefinedTokens(t *testing.T) {
	comp, err := os.ReadFile("frontend/src/components/Topography.svelte")
	if err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile("frontend/src/app.css")
	if err != nil {
		t.Fatal(err)
	}

	inks := regexp.MustCompile(`const LANE_INKS = \[([^\]]*)\]`).FindStringSubmatch(string(comp))
	if inks == nil {
		t.Fatal("Topography.svelte no longer declares LANE_INKS -- if the lane palette moved, move this guard with it")
	}

	tokens := regexp.MustCompile(`var\((--[a-zA-Z0-9-]+)\)`).FindAllStringSubmatch(inks[1], -1)
	if len(tokens) == 0 {
		t.Fatalf("LANE_INKS names no CSS tokens: %s", inks[1])
	}

	for _, m := range tokens {
		token := m[1]
		if token == "--marked" {
			t.Errorf("LANE_INKS wears %s, which means watchers on this same screen (#715 item 11)", token)
			continue
		}
		if !regexp.MustCompile(regexp.QuoteMeta(token) + `:\s*[^;]+;`).Match(css) {
			t.Errorf("LANE_INKS wears %s, which app.css does not define -- the lane would paint nothing", token)
		}
	}

	// The fifth lane's own value, not just its name: the ruling rests on
	// this colour clearing the other four for a red-green colour-blind
	// reader, so a later retint is a new decision, not a tweak.
	if !regexp.MustCompile(`--lane-5:\s*#7b6a0b;`).Match(css) {
		t.Error("--lane-5 is no longer the validated #7b6a0b (Fable 5, #715 item 11) -- rerun the palette validation before changing it")
	}
}
