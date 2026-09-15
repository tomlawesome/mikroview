// SPDX-License-Identifier: AGPL-3.0-only

// Package logging is mikroview's server-side log formatting: leveled,
// colorized (auto-disabled off a TTY or with NO_COLOR set), with a
// stable component column so a scrolling terminal or `docker logs` stays
// scannable. Built on log/slog rather than a third-party logging
// library, matching the rest of the codebase's near-zero-dependency
// posture (see go.mod).
//
// Not used for CLI recovery command output (-list-users,
// -recover-admin-account's password prompts, etc.) -- those print directly to
// stdout/stderr for scripting/piping, not through this leveled path.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

// componentWidth is the padded width of the component column, sized to
// the longest of mikroview's own runtime components (e.g.
// "healthcheck", "syslog-tcp") without truncating -- a rare longer
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

// Recover must be deferred directly -- `defer logging.Recover(logger)`
// -- never wrapped in another closure (`defer func() {
// logging.Recover(logger) }()` looks equivalent but is a real, common
// Go footgun: recover() only stops a panic when called directly by the
// function that was itself deferred. One layer of closure in between
// means recover() is no longer being called "directly" by the deferred
// call, so it returns nil and the panic keeps propagating exactly as
// if this were never called at all -- silently, since nothing about it
// looks wrong until it's actually exercised by a real panic. Logs and
// swallows an in-flight panic instead of letting it propagate.
//
// Go gives no default containment for a panic in a goroutine: only
// net/http's own per-request goroutine gets an automatic recover, and
// that protection doesn't extend to any goroutine spawned from within a
// handler, let alone mikroview's independently-started long-running
// goroutines (ingestion, detection, notification dispatch, the syslog
// listeners, ...). An unrecovered panic in any one of them takes down
// the entire process -- every connected browser's WebSocket, both
// syslog listeners, the whole in-memory event buffer -- not just the
// subsystem it happened in. Call this at the smallest unit of work a
// goroutine repeats (once per event/message/connection, not once for
// the goroutine's entire lifetime) so a single bad input degrades that
// one unit of work rather than silently ending the goroutine for good.
func Recover(logger *slog.Logger) {
	if r := recover(); r != nil {
		// Baked into the message rather than passed as a "stack" attr:
		// appendAttr (below) quotes any value containing a control
		// character, which a multi-line stack trace always does, and
		// a quoted, escaped stack trace is far less readable than one
		// printed as-is.
		logger.Error(fmt.Sprintf("recovered from panic: %v\n%s", r, debug.Stack()))
	}
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
// component field and a │ separator before the message, with any
// other attrs trailing the message as key=value pairs) isn't something
// TextHandler's ReplaceAttr hook can produce.
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
	var tail strings.Builder

	// Handler-level attrs (WithAttrs, in practice just New's
	// "component") come first, then the record's own -- the same order
	// slog.TextHandler uses, so a caller that does
	// logger.With("reqID", id).Warn(msg, "err", err) sees reqID before
	// err.
	for _, a := range h.attrs {
		if a.Key == "component" {
			component = a.Value.String()
			continue
		}
		appendAttr(&tail, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
			return true
		}
		appendAttr(&tail, "", a)
		return true
	})

	line := formatLine(r.Time.Format("15:04:05"), r.Level, component, r.Message, tail.String(), h.color)

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}

// appendAttr renders a into sb as a space-separated "key=value" token,
// space-prefixed unless sb is still empty. slog.Group attrs recurse
// with their key dotted onto prefix (mikroview.reason.code, not a
// second nesting syntax) -- an empty-keyed group (slog.Group("", ...),
// which slog treats as "inline these at the parent level") passes
// prefix through unchanged rather than adding a leading dot.
func appendAttr(sb *strings.Builder, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		childPrefix := prefix
		if a.Key != "" {
			if prefix != "" {
				childPrefix = prefix + "." + a.Key
			} else {
				childPrefix = a.Key
			}
		}
		for _, ga := range v.Group() {
			appendAttr(sb, childPrefix, ga)
		}
		return
	}
	if a.Key == "" {
		// Matches slog.TextHandler: an empty key with a non-group value
		// has nothing to label it with, so it's dropped rather than
		// printed as a bare "=value".
		return
	}
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	if sb.Len() > 0 {
		sb.WriteByte(' ')
	}
	sb.WriteString(key)
	sb.WriteByte('=')
	sb.WriteString(quoteAttrValue(v.String()))
}

// quoteAttrValue wraps s in Go-quoted form when it contains anything
// that would make the rendered "key=value" token ambiguous or unsafe
// to print -- a space or "=" would run into the next token or the
// separator, and a control character (e.g. a stray newline inside an
// error message) would otherwise break the one-line-per-record
// invariant every other reader of this log format relies on.
func quoteAttrValue(s string) string {
	if s == "" {
		return `""`
	}
	needsQuote := strings.ContainsAny(s, " \"=") || strings.ContainsFunc(s, unicode.IsControl)
	if !needsQuote {
		return s
	}
	return strconv.Quote(s)
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

// formatLine renders "HH:MM:SS LEVEL  component │ message key=value
// ...\n" -- the gaps after INFO/WARN and after short component names
// are the level/column padding lining up with ERROR and the longest
// common component name, not stray whitespace. attrs is already a
// fully space-joined "key=value key2=value2" tail (see appendAttr) or
// "" when the record carried no attrs beyond component; either way it
// never gets its own color treatment, matching the plain message.
func formatLine(ts string, level slog.Level, component, message, attrs string, color bool) string {
	levelToken := fmt.Sprintf("%-5s", levelWord(level))
	componentToken := fmt.Sprintf("%-*s", componentWidth, component)
	if attrs != "" {
		message = message + " " + attrs
	}

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

// Printable renders s safe to write to an operator's terminal.
//
// Needed because the CLI commands print stored values -- account names
// in -list-users, most obviously -- and a stored value can predate the
// validation that now rejects control characters (see
// auth.ValidateUsername), or arrive from an identity provider that was
// never under this deployment's control.
//
// An ANSI escape sequence written to a terminal is not text, it is an
// instruction: it can move the cursor, erase what was already printed,
// recolour a line, or on some terminals stuff the input buffer. That is
// enough to make `mikroview -list-users` show a different set of
// accounts than the one it actually read -- which is the exact defect in
// CVE-2025-55754 (Tomcat) and CVE-2025-48432 (Django).
//
// Control characters and Unicode format characters (the bidi overrides
// that let text render in an order other than the one it is stored in)
// are replaced with U+FFFD, so their presence is visible rather than
// silently dropped.
func Printable(s string) string {
	if !strings.ContainsFunc(s, unsafeForTerminal) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unsafeForTerminal(r) {
			return '�'
		}
		return r
	}, s)
}

func unsafeForTerminal(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError
}
