// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestFormatLineNoColorLayout(t *testing.T) {
	ts := "18:43:44"
	got := formatLine(ts, slog.LevelInfo, "auth", "no decision made yet", false)
	want := "18:43:44 INFO  auth        │ no decision made yet\n"
	if got != want {
		t.Errorf("formatLine =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatLineErrorLevelNoDoubleSpace(t *testing.T) {
	// ERROR is one character wider than INFO/WARN, so it should get one
	// space before the component, not two -- otherwise the │ column
	// drifts out of alignment on error lines specifically.
	got := formatLine("18:43:46", slog.LevelError, "syslog-udp", "bind: address already in use", false)
	want := "18:43:46 ERROR syslog-udp  │ bind: address already in use\n"
	if got != want {
		t.Errorf("formatLine =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatLineLongComponentDoesNotTruncate(t *testing.T) {
	got := formatLine("18:43:44", slog.LevelInfo, "enable-auth-setup", "re-armed", false)
	if !strings.Contains(got, "enable-auth-setup") {
		t.Errorf("expected the full component name to survive padding, got %q", got)
	}
	if !strings.HasSuffix(got, "│ re-armed\n") {
		t.Errorf("expected the message to still follow the separator, got %q", got)
	}
}

func TestFormatLineColorWrapsLevelAndDimsTheRest(t *testing.T) {
	got := formatLine("18:43:44", slog.LevelWarn, "flags", "permission denied", true)
	if !strings.Contains(got, ansiYellow+"WARN "+ansiReset) {
		t.Errorf("expected the level token colored yellow and reset, got %q", got)
	}
	if !strings.Contains(got, ansiDim+"18:43:44"+ansiReset) {
		t.Errorf("expected the timestamp dimmed, got %q", got)
	}
	// The message itself is never wrapped in an ANSI code -- only the
	// timestamp/level/component/separator get colored.
	if !strings.HasSuffix(got, "permission denied\n") {
		t.Errorf("expected the message to stay uncolored at the end of the line, got %q", got)
	}
}

func TestLevelWordAndColorByThreshold(t *testing.T) {
	cases := []struct {
		level slog.Level
		word  string
		color string
	}{
		{slog.LevelDebug, "DEBUG", ansiDim},
		{slog.LevelInfo, "INFO", ansiCyan},
		{slog.LevelWarn, "WARN", ansiYellow},
		{slog.LevelError, "ERROR", ansiRed},
	}
	for _, c := range cases {
		if got := levelWord(c.level); got != c.word {
			t.Errorf("levelWord(%v) = %q, want %q", c.level, got, c.word)
		}
		if got := levelColor(c.level); got != c.color {
			t.Errorf("levelColor(%v) = %q, want %q", c.level, got, c.color)
		}
	}
}

// TestNewAttachesComponentAndHandleRendersIt exercises the real
// slog.Logger/Handler path (New + Handle), not just formatLine, so the
// WithAttrs plumbing that carries "component" through to Handle is
// covered, not just the string-formatting helper.
func TestNewAttachesComponentAndHandleRendersIt(t *testing.T) {
	var buf strings.Builder
	h := &handler{w: &buf, level: slog.LevelInfo, color: false, mu: sharedHandler.mu}
	logger := slog.New(h).With(slog.String("component", "tls"))

	logger.Info("generated a local CA")

	got := buf.String()
	if !strings.Contains(got, "INFO") {
		t.Errorf("expected an INFO line, got %q", got)
	}
	if !strings.Contains(got, "tls") {
		t.Errorf("expected the component column to show tls, got %q", got)
	}
	if !strings.Contains(got, "generated a local CA") {
		t.Errorf("expected the message to appear, got %q", got)
	}
}

func TestSetLevelFiltersBelowThreshold(t *testing.T) {
	t.Cleanup(func() { SetLevel("info") })

	var buf strings.Builder
	h := &handler{w: &buf, level: programLevel, color: false, mu: sharedHandler.mu}
	logger := slog.New(h).With(slog.String("component", "test"))

	SetLevel("warn")
	logger.Info("should be filtered out")
	if buf.Len() != 0 {
		t.Errorf("expected Info to be filtered at warn threshold, got %q", buf.String())
	}

	logger.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Errorf("expected Warn to pass at warn threshold, got %q", buf.String())
	}
}

func TestSetLevelUnrecognizedFallsBackToInfo(t *testing.T) {
	t.Cleanup(func() { SetLevel("info") })

	SetLevel("nonsense")
	if programLevel.Level() != slog.LevelInfo {
		t.Errorf("expected an unrecognized level string to fall back to info, got %v", programLevel.Level())
	}
}

func TestSetLevelCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Cleanup(func() { SetLevel("info") })

	SetLevel("  ERROR  ")
	if programLevel.Level() != slog.LevelError {
		t.Errorf("expected case-insensitive/trimmed parsing, got %v", programLevel.Level())
	}
}

func TestHandleIsSafeForConcurrentUse(t *testing.T) {
	var buf strings.Builder
	h := &handler{w: &buf, level: slog.LevelInfo, color: false, mu: &sync.Mutex{}}
	logger := slog.New(h).With(slog.String("component", "concurrent"))

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			logger.Info("line")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}

	if got := strings.Count(buf.String(), "\n"); got != 20 {
		t.Errorf("expected 20 complete lines with no interleaving corruption, got %d newlines", got)
	}
}

// TestRecoverMustBeDeferredDirectly guards the exact footgun Recover's
// own doc comment warns about: recover() only stops a panic when
// called directly by the function that was itself deferred. Wrapping
// the call in another closure (`defer func() { Recover(logger) }()`)
// looks equivalent but silently doesn't work -- this test would crash
// with an unrecovered panic if Recover (or its calling convention) ever
// regressed to that pattern.
func TestRecoverMustBeDeferredDirectly(t *testing.T) {
	var buf strings.Builder
	h := &handler{w: &buf, level: slog.LevelInfo, color: false, mu: &sync.Mutex{}}
	logger := slog.New(h).With(slog.String("component", "recover-test"))

	func() {
		defer Recover(logger)
		panic("boom")
	}()

	if !strings.Contains(buf.String(), "recovered from panic") {
		t.Errorf("expected a logged recovery, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("expected the panic value in the log line, got %q", buf.String())
	}
}
