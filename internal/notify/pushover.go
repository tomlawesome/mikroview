// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// pushoverAPIURL is Pushover's fixed message endpoint -- no per-account
// URL, just a token+user pair.
const pushoverAPIURL = "https://api.pushover.net/1/messages.json"

// pushoverMaxLines caps how many flags are listed in one push message --
// Pushover's own message field is capped at 1024 characters, and a long
// batch is more useful as "N flags, here are a few" than a message the
// API silently truncates mid-line.
const pushoverMaxLines = 10

// pushoverMaxMessageLen mirrors Pushover's own documented message-field
// limit -- a hard safety truncation on top of pushoverMaxLines, in case
// even a handful of long Detail strings add up.
const pushoverMaxMessageLen = 1024

// pushoverTimeout bounds how long a single Send waits on Pushover's API
// -- this runs on Dispatcher's single goroutine, so a hung request must
// not stall every other configured channel's next flush indefinitely.
const pushoverTimeout = 10 * time.Second

type PushoverConfig struct {
	Token string
	User  string
}

// PushoverNotifier sends a batch of newly-raised flags as one Pushover
// notification -- the simpler of the two push targets in issue #31 (no
// VAPID/service-worker/subscription management, just a token+user pair).
// True web push is a separate, not-yet-built target, scoped alongside
// PWA feasibility (#32) since a lot of that plumbing overlaps.
type PushoverNotifier struct {
	cfg    PushoverConfig
	client *http.Client
	// apiURL defaults to pushoverAPIURL -- overridable only by tests
	// (same package), pointed at a fake HTTP server instead of the real
	// Pushover API.
	apiURL string
}

func NewPushoverNotifier(cfg PushoverConfig) *PushoverNotifier {
	return &PushoverNotifier{cfg: cfg, client: &http.Client{Timeout: pushoverTimeout}, apiURL: pushoverAPIURL}
}

func (n *PushoverNotifier) Send(batch []flags.Flag) error {
	if len(batch) == 0 {
		return nil
	}

	form := url.Values{
		"token":   {n.cfg.Token},
		"user":    {n.cfg.User},
		"title":   {fmt.Sprintf("mikroview: %d new flag%s", len(batch), plural(len(batch)))},
		"message": {n.message(batch)},
	}

	resp, err := n.client.PostForm(n.apiURL, form)
	if err != nil {
		return fmt.Errorf("notify/pushover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notify/pushover: unexpected status %s", resp.Status)
	}
	return nil
}

// message builds a one-line-per-flag summary, same content as
// SMTPNotifier.message but capped for Pushover's much smaller message
// field -- see pushoverMaxLines/pushoverMaxMessageLen.
func (n *PushoverNotifier) message(batch []flags.Flag) string {
	lines := batch
	var trailer string
	if len(lines) > pushoverMaxLines {
		trailer = fmt.Sprintf("\n... and %d more", len(lines)-pushoverMaxLines)
		lines = lines[:pushoverMaxLines]
	}

	var b strings.Builder
	for i, f := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		confidence := "unscored"
		if f.Confidence != nil {
			confidence = fmt.Sprintf("%d%%", *f.Confidence)
		}
		fmt.Fprintf(&b, "[%s] %s -- %s (%s)", f.Type, f.Target, f.Detail, confidence)
	}
	b.WriteString(trailer)

	msg := b.String()
	if len(msg) > pushoverMaxMessageLen {
		msg = truncateRunes(msg, pushoverMaxMessageLen)
	}
	return msg
}

// truncateRunes cuts s to at most n bytes without splitting a rune.
//
// A plain s[:n] can land mid-sequence and produce invalid UTF-8, which
// is reachable here: the message is built from a flag's Target and
// Detail, both of which come from a syslog line whoever sent it wrote,
// and a hostname or rule label with non-ASCII text can put a multi-byte
// rune across the boundary of a long batch. What Pushover does with
// invalid UTF-8 has not been established, so this avoids sending it
// rather than relying on the answer.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Walk back to a boundary. utf8.RuneStart is the cheap way to ask
	// "is this byte the first of a sequence"; at most three steps, since
	// a UTF-8 sequence is at most four bytes.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
