// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"testing"
	"time"
)

func TestParseEnvelope(t *testing.T) {
	recv := time.Date(2026, time.January, 15, 10, 22, 31, 0, time.UTC)

	tests := []struct {
		name         string
		raw          string
		wantHost     string
		wantFacility int
		wantSeverity int
		wantMsg      string
	}{
		{
			name:         "standard RouterOS line",
			raw:          "<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1 out:bridge1, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60",
			wantHost:     "MikroTik",
			wantFacility: 16, // 134/8
			wantSeverity: 6,  // 134%8
			wantMsg:      "A|lan-wan|forward: in:ether1 out:bridge1, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60",
		},
		{
			name:         "single digit day (double space)",
			raw:          "<131>Jan  5 09:00:00 router1 D|invalid|input: in:ether1, proto TCP (RST), len 40",
			wantHost:     "router1",
			wantFacility: 16,
			wantSeverity: 3,
			wantMsg:      "D|invalid|input: in:ether1, proto TCP (RST), len 40",
		},
		{
			name:         "no envelope at all - message preserved verbatim",
			raw:          "just a bare message with no syslog wrapper",
			wantHost:     "",
			wantFacility: -1,
			wantSeverity: -1,
			wantMsg:      "just a bare message with no syslog wrapper",
		},
		{
			name:         "PRI only, no timestamp/hostname",
			raw:          "<134>not a valid timestamp here",
			wantHost:     "",
			wantFacility: 16,
			wantSeverity: 6,
			wantMsg:      "not a valid timestamp here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ParseEnvelope([]byte(tt.raw), recv)
			if env.Hostname != tt.wantHost {
				t.Errorf("Hostname = %q, want %q", env.Hostname, tt.wantHost)
			}
			if env.Facility != tt.wantFacility {
				t.Errorf("Facility = %d, want %d", env.Facility, tt.wantFacility)
			}
			if env.Severity != tt.wantSeverity {
				t.Errorf("Severity = %d, want %d", env.Severity, tt.wantSeverity)
			}
			if env.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", env.Message, tt.wantMsg)
			}
		})
	}
}

func TestInferYear(t *testing.T) {
	tests := []struct {
		name     string
		recv     time.Time
		raw      string
		wantYear int
		wantMon  time.Month
		wantDay  int
	}{
		{
			name:     "same year, normal case",
			recv:     time.Date(2026, time.June, 15, 10, 0, 0, 0, time.UTC),
			raw:      "<134>Jun 15 09:58:00 host msg",
			wantYear: 2026, wantMon: time.June, wantDay: 15,
		},
		{
			name:     "december message arrives just after new year",
			recv:     time.Date(2027, time.January, 1, 0, 5, 0, 0, time.UTC),
			raw:      "<134>Dec 31 23:58:00 host msg",
			wantYear: 2026, wantMon: time.December, wantDay: 31,
		},
		{
			name:     "january message received just before new year (clock skew)",
			recv:     time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC),
			raw:      "<134>Jan  1 00:02:00 host msg",
			wantYear: 2027, wantMon: time.January, wantDay: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ParseEnvelope([]byte(tt.raw), tt.recv)
			if env.Timestamp.Year() != tt.wantYear || env.Timestamp.Month() != tt.wantMon || env.Timestamp.Day() != tt.wantDay {
				t.Errorf("Timestamp = %v, want year=%d month=%v day=%d", env.Timestamp, tt.wantYear, tt.wantMon, tt.wantDay)
			}
		})
	}
}
