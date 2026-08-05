package logging

import (
	"strings"
	"testing"
)

func TestBannerIsMultiLine(t *testing.T) {
	lines := strings.Split(banner, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected a multi-line banner, got %d line(s)", len(lines))
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Error("expected every banner line to have visible content, got a blank line")
		}
	}
}

func TestPrintBannerDoesNotPanic(t *testing.T) {
	PrintBanner()
}
