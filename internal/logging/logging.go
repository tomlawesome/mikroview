// Package logging is mikroview's server-side log formatting: leveled,
// colorized (auto-disabled off a TTY or with NO_COLOR set), with a
// stable component column so a scrolling terminal or `docker logs` stays
// scannable. Built on log/slog rather than a third-party logging
// library, matching the rest of the codebase's near-zero-dependency
// posture (see go.mod).
//
// Not used for CLI recovery command output (-list-users,
// -reset-password's password prompts, etc.) -- those print directly to
// stdout/stderr for scripting/piping, not through this leveled path.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// componentWidth is the padded width of the component column, sized to
// the longest of mikroview's own runtime components (e.g.
// "healthcheck", "syslog-udp") without truncating -- a rare longer
// component (the CLI recovery commands: "enable-auth-setup") just
// doesn't align as neatly, rather than ever cutting a name short.
const componentWidth = 11

// programLevel is shared by every logger returned from New -- SetLevel
// adjusts it once, at startup, from config/MIKROVIEW_LOG_LEVEL. A
// slog.LevelVar rather than a plain field so every already-created
// component logger picks up the change without needing to be re-created.
var programLevel = new(slog.LevelVar)

var sharedHandler = &handler{
	w:     os.Stdout,
	level: programLevel,
	color: colorEnabled(),
	mu:    &sync.Mutex{},
}

// colorEnabled follows the NO_COLOR convention (https://no-color.org)
// and auto-disables when stdout isn't a terminal (piped to a file, a
// log collector, `docker logs | grep`, etc.) -- ANSI escapes in that
// case would just show up as literal garbage, not color.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// New returns a logger tagged with component -- component renders in
// its own column (see handler.Handle) rather than as a prefix baked
// into every message string, so callers write plain messages the way
// they always did with log.Printf.
func New(component string) *slog.Logger {
	return slog.New(sharedHandler).With(slog.String("component", component))
}

// SetLevel parses one of debug/info/warn/error (case-insensitive) and
// applies it to every logger returned from New, past and future.
// Anything unrecognized (a typo, an empty string) falls back to info
// silently, matching how every other malformed config/env value in
// this codebase degrades rather than failing startup over a log
// setting.
func SetLevel(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		programLevel.Set(slog.LevelDebug)
	case "warn", "warning":
		programLevel.Set(slog.LevelWarn)
	case "error":
		programLevel.Set(slog.LevelError)
	default:
		programLevel.Set(slog.LevelInfo)
	}
}

// handler implements slog.Handler directly rather than customizing
// slog.NewTextHandler -- the target line shape (a fixed-column
// component field and a │ separator, not key=value pairs) isn't
// something TextHandler's ReplaceAttr hook can produce.
type handler struct {
	w     io.Writer
	level slog.Leveler
	color bool
	attrs []slog.Attr
	mu    *sync.Mutex
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	component := "mikroview"
	for _, a := range h.attrs {
		if a.Key == "component" {
			component = a.Value.String()
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
		}
		return true
	})

	line := formatLine(r.Time.Format("15:04:05"), r.Level, component, r.Message, h.color)

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}

// WithAttrs stores attrs (in practice, just the "component" attr New
// attaches) so Handle can read them back per record -- a plain slice
// append rather than baking them into a pre-rendered prefix, since
// nothing else in this handler needs generic attr support.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := *h
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	n.attrs = merged
	return &n
}

// WithGroup is a no-op: mikroview never groups attrs (component is
// the only one, added via New), so there's nothing to namespace.
func (h *handler) WithGroup(_ string) slog.Handler {
	return h
}

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[90m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
)

func levelWord(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return ansiRed
	case level >= slog.LevelWarn:
		return ansiYellow
	case level >= slog.LevelInfo:
		return ansiCyan
	default:
		return ansiDim
	}
}

// formatLine renders "HH:MM:SS LEVEL  component │ message\n" -- the
// gaps after INFO/WARN and after short component names are the level/
// column padding lining up with ERROR and the longest common component
// name, not stray whitespace.
func formatLine(ts string, level slog.Level, component, message string, color bool) string {
	levelToken := fmt.Sprintf("%-5s", levelWord(level))
	componentToken := fmt.Sprintf("%-*s", componentWidth, component)

	if !color {
		return fmt.Sprintf("%s %s %s │ %s\n", ts, levelToken, componentToken, message)
	}

	return fmt.Sprintf(
		"%s%s%s %s%s%s %s%s%s %s│%s %s\n",
		ansiDim, ts, ansiReset,
		levelColor(level), levelToken, ansiReset,
		ansiDim, componentToken, ansiReset,
		ansiDim, ansiReset,
		message,
	)
}
