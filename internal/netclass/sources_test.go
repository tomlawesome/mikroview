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

// parseAWS and parseGCP were two of the six registered feed parsers with
// no test at all (#267 finding 9). Nothing constructed an ip-ranges.json
// or cloud.json body, so an upstream field rename would have passed CI
// silently while that feed quietly stopped attributing anything --
// which is the failure worth catching, since a feed returning zero
// usable prefixes looks identical to a quiet network.
//
// Bodies below are the real documents' shape, trimmed to the fields
// each parser reads.
func TestParseAWS(t *testing.T) {
	body := []byte(`{
	  "syncToken": "1700000000",
	  "prefixes": [
	    {"ip_prefix": "3.5.140.0/22", "region": "ap-northeast-2", "service": "AMAZON", "network_border_group": "ap-northeast-2"},
	    {"ip_prefix": "13.34.37.64/27", "region": "eu-west-2", "service": "AMAZON", "network_border_group": "eu-west-2"},
	    {"ip_prefix": "not-a-cidr", "region": "nowhere", "service": "AMAZON"},
	    {"ip_prefix": "10.0.0.0/8", "region": "private", "service": "AMAZON"},
	    {"ip_prefix": "0.0.0.0/0", "region": "everything", "service": "AMAZON"}
	  ],
	  "ipv6_prefixes": [
	    {"ipv6_prefix": "2600:1f13:d8f:d200::/56", "region": "us-west-2", "service": "AMAZON"}
	  ]
	}`)

	got := parseAWS(body)
	if len(got) != 3 {
		t.Fatalf("parseAWS returned %d prefixes, want 3 (two v4, one v6; the malformed, private and 0.0.0.0/0 entries must be dropped): %+v", len(got), got)
	}

	byPrefix := map[string]string{}
	for _, p := range got {
		byPrefix[p.prefix.String()] = p.detail
	}
	// The region is what ends up shown against an address, so it has to
	// survive, not just the prefix.
	if byPrefix["3.5.140.0/22"] != "ap-northeast-2" {
		t.Errorf("v4 region not carried through: %+v", byPrefix)
	}
	if byPrefix["2600:1f13:d8f:d200::/56"] != "us-west-2" {
		t.Errorf("the ipv6_prefixes block was not read, or lost its region: %+v", byPrefix)
	}
}

func TestParseGCP(t *testing.T) {
	// Entries carry either ipv4Prefix or ipv6Prefix, never both -- the
	// fallback between them is the part worth pinning.
	body := []byte(`{
	  "syncToken": "1700000000",
	  "prefixes": [
	    {"ipv4Prefix": "34.80.0.0/15", "service": "Google Cloud", "scope": "asia-east1"},
	    {"ipv6Prefix": "2600:1900:4000::/44", "service": "Google Cloud", "scope": "us-central1"},
	    {"ipv4Prefix": "not-a-cidr", "service": "Google Cloud", "scope": "nowhere"},
	    {"ipv4Prefix": "192.168.0.0/16", "service": "Google Cloud", "scope": "private"},
	    {"service": "Google Cloud", "scope": "neither-prefix-field"}
	  ]
	}`)

	got := parseGCP(body)
	if len(got) != 2 {
		t.Fatalf("parseGCP returned %d prefixes, want 2 (one v4, one v6): %+v", len(got), got)
	}

	byPrefix := map[string]string{}
	for _, p := range got {
		byPrefix[p.prefix.String()] = p.detail
	}
	if byPrefix["34.80.0.0/15"] != "asia-east1" {
		t.Errorf("v4 scope not carried through: %+v", byPrefix)
	}
	if byPrefix["2600:1900:4000::/44"] != "us-central1" {
		t.Errorf("the ipv6Prefix fallback was not taken, or lost its scope: %+v", byPrefix)
	}
}

// Both parsers return nil rather than panicking on a body that is not
// the document they expect -- an upstream serving an error page, say.
func TestCloudParsersRejectGarbage(t *testing.T) {
	for _, body := range [][]byte{[]byte(""), []byte("<html>503</html>"), []byte(`{"prefixes": "not-an-array"}`)} {
		if got := parseAWS(body); got != nil {
			t.Errorf("parseAWS(%q) = %+v, want nil", body, got)
		}
		if got := parseGCP(body); got != nil {
			t.Errorf("parseGCP(%q) = %+v, want nil", body, got)
		}
	}
}
