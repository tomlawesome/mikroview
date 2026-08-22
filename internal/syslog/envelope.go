// SPDX-License-Identifier: AGPL-3.0-only

// Package syslog parses the BSD/RFC3164-style syslog lines that RouterOS
// emits, and listens for them over UDP/TCP.
package syslog

import (
	"strconv"
	"strings"
	"time"
)

// Envelope is the outer syslog wrapper around a RouterOS log message.
type Envelope struct {
	Facility int // -1 if absent/unparseable
	Severity int // -1 if absent/unparseable
	// Timestamp is the device's self-reported time, corrected for its
	// inferred UTC offset (see resolveOffset) rather than taken as literal
	// UTC. It falls back to recvTime when no BSD timestamp is present or
	// parseable.
	Timestamp time.Time
	Hostname  string
	Message   string
}

const bsdTimeLayout = "Jan _2 15:04:05"

// ParseEnvelope parses a single RFC3164-ish BSD syslog line as emitted by
// RouterOS: "<PRI>MMM DD HH:MM:SS HOSTNAME MESSAGE".
//
// It is deliberately lenient: a malformed or missing envelope never causes
// the line to be dropped, since the firewall body carried in Message is
// what actually matters downstream. Any prefix that doesn't parse is left
// in place as part of Message rather than discarded.
func ParseEnvelope(raw []byte, recvTime time.Time) Envelope {
	s := strings.TrimRight(string(raw), "\r\n")
	env := Envelope{
		Facility:  -1,
		Severity:  -1,
		Timestamp: recvTime,
		Message:   s,
	}

	rest := s

	if strings.HasPrefix(rest, "<") {
		if end := strings.IndexByte(rest, '>'); end > 0 && end <= 4 {
			// A syslog PRI is 0-191 (RFC 3164 s4.1.1): facility 0-23,
			// severity 0-7. Atoi alone accepts anything that fits the
			// <=4-char window, so "<192>" yielded facility 24 and
			// "<9999>" facility 1249 -- values no conforming sender can
			// produce. This input is unauthenticated (any host that can
			// reach the syslog port picks these bytes), so the range
			// check is the difference between "absent" and "attacker
			// chose it". Out-of-range leaves both fields at their -1
			// sentinel and the PRI text in Message, the same as any
			// other prefix that doesn't parse.
			//
			// Nothing consumes Facility/Severity today -- this keeps
			// them honest for whoever first filters or alerts on them.
			if pri, err := strconv.Atoi(rest[1:end]); err == nil && pri >= 0 && pri <= 191 {
				env.Facility = pri / 8
				env.Severity = pri % 8
				rest = rest[end+1:]
			}
		}
	}

	if len(rest) >= len(bsdTimeLayout) {
		if ts, err := time.Parse(bsdTimeLayout, rest[:len(bsdTimeLayout)]); err == nil {
			naive := inferYear(ts, recvTime)
			env.Timestamp = naive.Add(resolveOffset(recvTime.Sub(naive)))
			rest = strings.TrimPrefix(rest[len(bsdTimeLayout):], " ")

			if sp := strings.IndexByte(rest, ' '); sp > 0 {
				env.Hostname = rest[:sp]
				rest = rest[sp+1:]
			}
		}
	}

	env.Message = rest
	return env
}

// inferYear attaches a year to a BSD timestamp (which carries none) based on
// the time the packet was received, handling the December/January boundary
// by picking whichever year lands within six months of recvTime.
func inferYear(ts, recvTime time.Time) time.Time {
	candidate := time.Date(recvTime.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(), 0, time.UTC)
	const halfYear = 183 * 24 * time.Hour
	switch {
	case candidate.Sub(recvTime) > halfYear:
		candidate = candidate.AddDate(-1, 0, 0)
	case recvTime.Sub(candidate) > halfYear:
		candidate = candidate.AddDate(1, 0, 0)
	}
	return candidate
}

// resolveOffset infers the device's UTC offset from the gap between its
// self-reported wall-clock time and recvTime, instead of assuming that gap
// is zero (#379 item 4). RouterOS's remote-log-format=syslog output is bare
// RFC3164 -- "Jan 15 14:00:00" -- and carries no timezone at all, so a
// router whose system clock is set to a non-UTC zone previously had its
// local wall-clock digits taken as if they already were UTC: BST (UTC+1)
// logging 14:00 the instant a message arrived at 13:00 UTC produced a
// Timestamp an hour ahead of the moment it actually happened.
//
// recvTime is a trusted local clock, so the gap between it and the naive
// (assumed-UTC) candidate is itself evidence of the device's real offset.
// Every real-world UTC offset lands on a 15-minute boundary between -12:00
// and +14:00 (whole-hour zones down to quarter-hour ones like Kathmandu's
// +05:45), so rounding the gap to the nearest 15 minutes recovers a stable
// device's actual offset without needing an operator to declare it
// per-device. This is the same technique inferYear already uses to recover
// the year a BSD timestamp omits -- resolving what the wire didn't send
// from the one clock actually known to be right.
//
// A gap outside that real-world range isn't a timezone: it means the
// device's absolute clock is wrong by more than an offset (unsynced or
// stopped), which is the pre-existing, separate failure mode
// store.Event.Time's doc comment already warns about. That case is left
// uncorrected rather than guessed at.
func resolveOffset(gap time.Duration) time.Duration {
	const (
		quantum         = 15 * time.Minute
		maxOffsetAhead  = 14 * time.Hour
		maxOffsetBehind = -12 * time.Hour
	)
	rounded := gap.Round(quantum)
	if rounded > maxOffsetAhead || rounded < maxOffsetBehind {
		return 0
	}
	return rounded
}
