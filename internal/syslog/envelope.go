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
	Facility  int // -1 if absent/unparseable
	Severity  int // -1 if absent/unparseable
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
			if pri, err := strconv.Atoi(rest[1:end]); err == nil {
				env.Facility = pri / 8
				env.Severity = pri % 8
				rest = rest[end+1:]
			}
		}
	}

	if len(rest) >= len(bsdTimeLayout) {
		if ts, err := time.Parse(bsdTimeLayout, rest[:len(bsdTimeLayout)]); err == nil {
			env.Timestamp = inferYear(ts, recvTime)
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
