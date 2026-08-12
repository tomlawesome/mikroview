// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import "testing"

func TestCoversAddress(t *testing.T) {
	tests := []struct {
		spec string
		ip   string
		want Coverage
	}{
		// Unset means "any address". Getting this backwards would report
		// "nothing watches this" for the most common rule there is.
		{"", "10.0.0.5", Covers},

		{"203.0.113.9", "203.0.113.9", Covers},
		{"203.0.113.9", "203.0.113.10", Excludes},

		{"10.0.0.0/8", "10.1.2.3", Covers},
		{"10.0.0.0/8", "192.168.1.1", Excludes},

		{"10.0.0.1-10.0.0.5", "10.0.0.3", Covers},
		{"10.0.0.1-10.0.0.5", "10.0.0.1", Covers},
		{"10.0.0.1-10.0.0.5", "10.0.0.5", Covers},
		{"10.0.0.1-10.0.0.5", "10.0.0.6", Excludes},

		// Negation. The case a naive containment test answers exactly
		// backwards, which is the failure #274 is about.
		{"!10.0.0.0/8", "10.1.2.3", Excludes},
		{"!10.0.0.0/8", "192.168.1.1", Covers},
		{"!203.0.113.9", "203.0.113.9", Excludes},
		{"!10.0.0.1-10.0.0.5", "10.0.0.3", Excludes},
		{"!10.0.0.1-10.0.0.5", "10.0.0.9", Covers},

		// A list covers if any element does.
		{"10.0.0.0/8,192.168.0.0/16", "192.168.1.1", Covers},
		{"10.0.0.0/8,192.168.0.0/16", "172.16.0.1", Excludes},

		// IPv6, and cross-family, which never overlaps.
		{"2001:db8::/32", "2001:db8::1", Covers},
		{"2001:db8::/32", "2001:dbf::1", Excludes},
		{"10.0.0.0/8", "2001:db8::1", Excludes},

		// Anything unreadable must say so rather than guess. A rule
		// scoping by address-list name reaches here as a bare word.
		{"mgmt", "10.0.0.1", Unknown},
		{"10.0.0.0/999", "10.0.0.1", Unknown},
		{"10.0.0.0/8", "not-an-ip", Unknown},
	}

	for _, tt := range tests {
		if got := CoversAddress(tt.spec, tt.ip); got != tt.want {
			t.Errorf("CoversAddress(%q, %q) = %v, want %v", tt.spec, tt.ip, got, tt.want)
		}
	}
}

func TestCoversPort(t *testing.T) {
	tests := []struct {
		spec string
		port int
		want Coverage
	}{
		{"", 22, Covers},
		{"22", 22, Covers},
		{"22", 23, Excludes},
		{"22,23", 23, Covers},
		{"22,23", 24, Excludes},
		{"1000-2000", 1500, Covers},
		{"1000-2000", 1000, Covers},
		{"1000-2000", 2000, Covers},
		{"1000-2000", 2001, Excludes},
		{"22,1000-2000", 1500, Covers},
		{"22,1000-2000", 80, Excludes},

		{"ssh", 22, Unknown},
		{"22", 0, Unknown},
	}

	for _, tt := range tests {
		if got := CoversPort(tt.spec, tt.port); got != tt.want {
			t.Errorf("CoversPort(%q, %d) = %v, want %v", tt.spec, tt.port, got, tt.want)
		}
	}
}

// An unparseable element poisons the whole list rather than being
// skipped: the skipped element might have been the one that covers, and
// answering Excludes on that basis is a false "nothing watches this".
func TestUnparseableElementIsNotSilentlySkipped(t *testing.T) {
	if got := CoversAddress("mgmt,10.0.0.0/8", "192.168.1.1"); got != Unknown {
		t.Errorf("CoversAddress with an unreadable element = %v, want Unknown", got)
	}
	if got := CoversPort("ssh,80", 443); got != Unknown {
		t.Errorf("CoversPort with an unreadable element = %v, want Unknown", got)
	}
}
