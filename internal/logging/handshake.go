// SPDX-License-Identifier: AGPL-3.0-only

package logging

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// HTTPErrorLog builds the *log.Logger net/http writes its own errors to,
// translating the TLS handshake failures into something an operator can
// act on and collapsing repeats.
//
// Go's line is accurate and unreadable:
//
//	http: TLS handshake error from 192.168.254.123:61400: remote error: tls: unknown certificate
//
// Every word of that matters and none of it is obvious. "remote error"
// means the *other end* rejected us, not that we failed -- the single
// most diagnostic bit in the line, and it is two words of Go jargon.
// Meanwhile a phone with a stale certificate exception retries every few
// seconds, so this arrives dozens of times an hour and buries anything
// real (#321, #322).
//
// Unrecognised errors pass through unchanged. A translation that guesses
// is worse than jargon.
func HTTPErrorLog(logger *slog.Logger) *log.Logger {
	return log.New(&handshakeWriter{logger: logger, gate: newKeyedLimiter(handshakeLogWindow)}, "", 0)
}

// handshakeLogWindow is how long one (peer, cause) pair stays quiet
// after being logged. A minute is long enough to collapse a reconnect
// loop and short enough that a genuinely new problem is not hidden.
const handshakeLogWindow = time.Minute

type handshakeWriter struct {
	logger *slog.Logger
	gate   *keyedLimiter
}

func (w *handshakeWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	peer, raw, ok := parseHandshakeError(line)
	if !ok {
		// Not a handshake error (a panic trace, a superfluous
		// WriteHeader) -- those are ours, rare, and worth reading whole.
		w.logger.Warn(line)
		return len(p), nil
	}

	explanation, cause := explainHandshake(peer, raw)
	suppressed, allow := w.gate.allow(peer + "|" + cause)
	if !allow {
		return len(p), nil
	}
	msg := explanation
	if suppressed > 1 {
		msg = fmt.Sprintf("%s (%d times in the last minute)", msg, suppressed)
	}
	w.logger.Warn(msg + ". Detail: " + raw)
	return len(p), nil
}

// parseHandshakeError splits Go's own format. Returns ok=false for
// anything that is not a TLS handshake error.
func parseHandshakeError(line string) (peer, raw string, ok bool) {
	const prefix = "http: TLS handshake error from "
	rest, found := strings.CutPrefix(line, prefix)
	if !found {
		return "", "", false
	}
	addr, detail, found := strings.Cut(rest, ": ")
	if !found {
		return "", "", false
	}
	// The source port is noise -- it is different on every retry, which
	// is what made a reconnect loop look like a port scan to the owner
	// reading these lines.
	if host, _, hasPort := strings.Cut(addr, ":"); hasPort {
		addr = host
	}
	return addr, detail, true
}

// explainHandshake renders one handshake failure in plain language, and
// returns a stable cause key for the repeat gate.
//
// The order matters: "remote error:" means they rejected us, and that
// distinction leads the sentence.
func explainHandshake(peer, raw string) (explanation, cause string) {
	switch {
	case strings.Contains(raw, "unknown certificate"),
		strings.Contains(raw, "bad certificate"),
		strings.Contains(raw, "unknown ca"),
		strings.Contains(raw, "unknown certificate authority"):
		return peer + " refused our certificate (browser: re-accept the warning; router: re-import /ca.crt)", "refused-cert"
	case strings.Contains(raw, "expired certificate"):
		return peer + " says our certificate has expired -- check the clock on both ends, then renew", "expired-cert"
	case strings.Contains(raw, "certificate is not valid for any names"),
		strings.Contains(raw, "certificate is valid for"):
		return peer + " reached us by a name our certificate does not cover -- add it to tls.hosts and restart", "wrong-name"
	case strings.Contains(raw, "first record does not look like a TLS handshake"):
		return peer + " spoke plain HTTP to the HTTPS port -- use https:// for this address", "plaintext"
	case strings.Contains(raw, "unsupported versions"), strings.Contains(raw, "protocol version not supported"):
		return "we turned " + peer + " away: it asked for a TLS version older than 1.2", "old-tls"
	case strings.Contains(raw, "no cipher suite supported"), strings.Contains(raw, "handshake failure"):
		return "we turned " + peer + " away: no encryption settings in common", "no-cipher"
	case strings.Contains(raw, "bad record MAC"):
		// TLS 1.3 encrypts the client's alert, so a client that rejects
		// our certificate mid-handshake surfaces here as a decryption
		// failure rather than as "remote error: tls: unknown
		// certificate" (which is what the same rejection looks like
		// over TLS 1.2). Observed with both curl and Python's ssl.
		// Worded to say what is certain and name the likely cause
		// without asserting it -- genuine corruption looks identical.
		return peer + " hung up during the handshake -- most often that means it did not trust our certificate", "hung-up"
	case strings.Contains(raw, "EOF"), strings.Contains(raw, "timeout"), strings.Contains(raw, "connection reset"):
		return peer + " connected and went away before finishing -- a port scan or a health check looks like this", "went-away"
	case strings.HasPrefix(raw, "remote error:"):
		return peer + " rejected our certificate", "refused-other"
	default:
		// Deliberately not guessed at: the raw error still gets logged
		// by the caller, under a line that says only what is certain.
		return "TLS handshake with " + peer + " failed", "other"
	}
}

// maxHandshakeKeys bounds the repeat gate's map. The key contains a peer
// address, so it is chosen by whoever connects -- an unbounded map here
// is the same attacker-keyed growth internal/device and internal/rules
// were both fixed for. Past the cap the map is cleared rather than
// grown: losing the gate's memory means at worst a repeat line, while
// growing it without limit means memory an unauthenticated client
// controls.
const maxHandshakeKeys = 1024

// keyedLimiter is Limiter, per key, with a bounded key set.
type keyedLimiter struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]*keyedEntry
}

type keyedEntry struct {
	last  time.Time
	count uint64
}

func newKeyedLimiter(window time.Duration) *keyedLimiter {
	return &keyedLimiter{window: window, seen: make(map[string]*keyedEntry)}
}

// allow reports whether this key's line should be written now, and how
// many occurrences it stands for (including the ones suppressed since
// the last written line).
func (l *keyedLimiter) allow(key string) (count uint64, ok bool) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.seen) >= maxHandshakeKeys {
		l.seen = make(map[string]*keyedEntry)
	}
	e, found := l.seen[key]
	if !found {
		l.seen[key] = &keyedEntry{last: now, count: 1}
		return 1, true
	}
	e.count++
	if now.Sub(e.last) < l.window {
		return e.count, false
	}
	n := e.count
	e.last = now
	e.count = 0
	return n, true
}
