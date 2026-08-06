package syslog

import (
	"strings"
	"testing"
	"time"
)

// FuzzParseEnvelope covers the other half of mikroview's unauthenticated
// network input: every syslog datagram hits ParseEnvelope before
// routeros.Parse ever sees the body. It does raw index arithmetic on
// attacker-controlled bytes (rest[1:end], rest[:len(bsdTimeLayout)],
// rest[sp+1:]), which is exactly the shape that slices out of range on
// an input nobody thought of.
//
// Like Parse, this is lenient by contract -- an unparseable envelope
// leaves the whole line as Message rather than erroring -- so the
// assertions are the invariants that must hold for any input:
//
//  1. No panic.
//  2. Facility/Severity are either the "absent" sentinel (-1) or a legal
//     RFC3164 value. A syslog PRI is 0-191, giving facility 0-23 and
//     severity 0-7; anything else means the PRI parse let something
//     through it shouldn't have.
//  3. Message is never longer than the input. It's a slice of the input,
//     so growth would mean the parser invented bytes -- cheap to assert,
//     and it pins the "prefix that doesn't parse stays in Message"
//     contract the doc comment promises.
func FuzzParseEnvelope(f *testing.F) {
	f.Add([]byte("<134>Aug  6 20:15:04 router1 A|lan-wan|forward: proto TCP, 1.2.3.4:80->5.6.7.8:443, len 60"))
	f.Add([]byte("<0>Jan  1 00:00:00 h m"))
	f.Add([]byte("<191>Dec 31 23:59:59 host message"))
	f.Add([]byte("<192>Aug  6 20:15:04 router1 msg")) // PRI above the legal max
	f.Add([]byte("<9999>Aug  6 20:15:04 h m"))        // PRI too long for the end<=4 guard
	f.Add([]byte("<-1>Aug  6 20:15:04 h m"))          // negative PRI
	f.Add([]byte("<>"))
	f.Add([]byte("<"))
	f.Add([]byte(">"))
	f.Add([]byte("Aug  6 20:15:04"))      // timestamp with nothing after it
	f.Add([]byte("Aug  6 20:15:04 "))     // trailing space, no hostname
	f.Add([]byte("Aug  6 20:15:04 host")) // hostname with no message
	f.Add([]byte("Xxx 99 99:99:99 h m"))  // well-shaped but invalid timestamp
	f.Add([]byte(""))
	f.Add([]byte("\r\n"))
	f.Add([]byte("\x00<134>\x00"))
	f.Add([]byte(strings.Repeat("<", 10000)))
	f.Add([]byte("<134>" + strings.Repeat("A", 70000))) // beyond the UDP read buffer

	now := time.Date(2026, time.August, 6, 20, 15, 4, 0, time.UTC)

	f.Fuzz(func(t *testing.T, raw []byte) {
		env := ParseEnvelope(raw, now) // must not panic

		if env.Facility != -1 && (env.Facility < 0 || env.Facility > 23) {
			t.Errorf("Facility = %d, want -1 or 0-23 (input %q)", env.Facility, raw)
		}
		if env.Severity != -1 && (env.Severity < 0 || env.Severity > 7) {
			t.Errorf("Severity = %d, want -1 or 0-7 (input %q)", env.Severity, raw)
		}
		if len(env.Message) > len(raw) {
			t.Errorf("Message is %d bytes from a %d-byte input -- the parser cannot invent bytes (input %q)",
				len(env.Message), len(raw), raw)
		}
	})
}
