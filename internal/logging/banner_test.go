// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// bannerLines is every rendered line, in order, without colour.
func bannerLines() []string {
	out := []string{bannerTopRule}
	out = append(out, bannerSkyAbove...)
	out = append(out, bannerWordmark...)
	out = append(out, bannerSkyBelow...)
	return append(out, bannerBottomRule)
}

func TestBannerHasContentOnEveryLine(t *testing.T) {
	lines := bannerLines()
	if len(lines) < 10 {
		t.Fatalf("expected a substantial banner, got %d lines", len(lines))
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("line %d is blank -- a blank line in the middle of the art breaks the block", i)
		}
	}
}

// The wordmark rows must all be the same rendered width, or the extruded
// edges shear and it stops looking like one solid object.
func TestWordmarkRowsAlign(t *testing.T) {
	want := utf8.RuneCountInString(bannerWordmark[0])
	for i, l := range bannerWordmark {
		if got := utf8.RuneCountInString(l); got != want {
			t.Errorf("wordmark row %d is %d runes wide, want %d -- the rows must align", i, got, want)
		}
	}
}

// Nothing may be wider than the rules that bracket it, or the art
// visibly overflows its own frame.
func TestNothingOverflowsTheRules(t *testing.T) {
	width := utf8.RuneCountInString(bannerTopRule)
	for i, l := range bannerLines() {
		if got := utf8.RuneCountInString(l); got > width {
			t.Errorf("line %d is %d runes wide, wider than the %d-rune rule", i, got, width)
		}
	}
	if utf8.RuneCountInString(bannerBottomRule) != width {
		t.Error("the top and bottom rules are different widths")
	}
}

// The art itself carries no escape sequences: colour is applied at print
// time and dropped off a TTY, so `docker logs` and a log collector get
// the same shape as a terminal does, just without the colour. An escape
// baked into the constants would defeat that.
func TestBannerArtCarriesNoEscapeSequences(t *testing.T) {
	for i, l := range bannerLines() {
		if strings.ContainsRune(l, '\x1b') {
			t.Errorf("line %d contains an ANSI escape -- colour belongs in PrintBanner, not the art", i)
		}
	}
}

// It has to survive a container's stdout, which is not a terminal. This
// is the whole reason the banner exists: marking a restart in
// `docker logs`.
func TestBannerIsValidUTF8(t *testing.T) {
	for i, l := range bannerLines() {
		if !utf8.ValidString(l) {
			t.Errorf("line %d is not valid UTF-8", i)
		}
	}
}

func TestPrintBannerDoesNotPanic(t *testing.T) {
	PrintBanner()
}
