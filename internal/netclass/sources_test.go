// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"strings"
	"testing"
)

// TestParseApplePrivateRelay exercises the CSV shape directly against
// real sample lines (see sources.go's comment: verified against the live
// feed, 287,715 lines, every one exactly four commas). Not fetched over
// the network here -- fetch_test.go covers the HTTP path generically via
// parseTorList; this is specifically about the comma-splitting logic
// this parser has that none of the others do.
func TestParseApplePrivateRelay(t *testing.T) {
	body := strings.Join([]string{
		"172.224.226.0/27,GB,GB-EN,London,",
		"146.75.253.246/31,US,US-MA,BOSTON,",
		"", // blank line, must be skipped
		"malformed line with no commas",
		"10.0.0.0/8,US,US-CA,San Francisco,", // reserved -- must be rejected by acceptablePrefix
	}, "\n")

	got := parseApplePrivateRelay([]byte(body))
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (the reserved and malformed lines must be dropped): %+v", len(got), got)
	}

	want := map[string]string{
		"172.224.226.0/27":  "London",
		"146.75.253.246/31": "BOSTON",
	}
	for _, cp := range got {
		key := cp.prefix.String()
		wantDetail, ok := want[key]
		if !ok {
			t.Errorf("unexpected prefix %s in result", key)
			continue
		}
		if cp.detail != wantDetail {
			t.Errorf("prefix %s: detail = %q, want %q -- the trailing comma from the CSV's empty 5th field must be stripped", key, cp.detail, wantDetail)
		}
	}
}

func TestParseApplePrivateRelayIgnoresTooFewFields(t *testing.T) {
	got := parseApplePrivateRelay([]byte("172.224.226.0/27,GB,GB-EN\n"))
	if len(got) != 0 {
		t.Errorf("a line with only 3 fields (missing city) was accepted: %+v", got)
	}
}
