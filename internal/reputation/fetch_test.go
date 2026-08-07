// SPDX-License-Identifier: AGPL-3.0-only

package reputation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// clientReturning builds an *http.Client that answers every request
// with a fixed status/body, or fails with err, without touching the
// network. The fetchers take their client as a parameter, so this needs
// no production change -- threading a base URL through shipping code
// purely for testability would be the wrong trade.
func clientReturning(status int, body string, err error) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status,
			Body:       readCloser(body),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func readCloser(s string) *nopCloser { return &nopCloser{Reader: strings.NewReader(s)} }

type nopCloser struct{ *strings.Reader }

func (n *nopCloser) Close() error { return nil }

// TestFetchShodanParsesASuccessfulResponse pins the happy path.
func TestFetchShodanParsesASuccessfulResponse(t *testing.T) {
	c := clientReturning(http.StatusOK,
		`{"ports":[22,443],"hostnames":["a.example"],"vulns":["CVE-1"],"tags":["cdn"]}`, nil)

	got := fetchShodan(context.Background(), c, "203.0.113.9")
	if len(got.Ports) != 2 || got.Ports[0] != 22 {
		t.Errorf("Ports = %v, want [22 443]", got.Ports)
	}
	if len(got.Hostnames) != 1 || got.Hostnames[0] != "a.example" {
		t.Errorf("Hostnames = %v", got.Hostnames)
	}
	if len(got.Vulns) != 1 || len(got.Tags) != 1 {
		t.Errorf("Vulns/Tags = %v / %v", got.Vulns, got.Tags)
	}
}

// TestFetchersDegradeToZeroResultOnAnyFailure pins the contract the
// whole best-effort reputation design rests on: a source that errors,
// rate-limits, or returns junk contributes nothing and never surfaces
// an error to the caller. If this ever regressed into propagating a
// failure, a flaky third party would start breaking IP lookups for the
// operator instead of quietly degrading.
func TestFetchersDegradeToZeroResultOnAnyFailure(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"network error", 0, "", errors.New("dial tcp: connection refused")},
		{"rate limited", http.StatusTooManyRequests, `{"ports":[22]}`, nil},
		{"server error", http.StatusInternalServerError, "", nil},
		{"unauthorized (bad key)", http.StatusUnauthorized, "", nil},
		{"malformed JSON", http.StatusOK, `{"ports": [22`, nil},
		{"wrong shape", http.StatusOK, `"a bare string"`, nil},
		{"empty body", http.StatusOK, "", nil},
	}

	for _, tc := range cases {
		t.Run("shodan/"+tc.name, func(t *testing.T) {
			c := clientReturning(tc.status, tc.body, tc.err)
			if got := fetchShodan(context.Background(), c, "203.0.113.9"); len(got.Ports) != 0 || len(got.Hostnames) != 0 {
				t.Errorf("expected a zero Result, got %+v", got)
			}
		})
		t.Run("abuseipdb/"+tc.name, func(t *testing.T) {
			c := clientReturning(tc.status, tc.body, tc.err)
			if got := fetchAbuseIPDB(context.Background(), c, "key", "203.0.113.9"); got.AbuseScore != nil || got.CountryCode != "" {
				t.Errorf("expected a zero Result, got %+v", got)
			}
		})
	}
}

// TestFetchAbuseIPDBSendsTheKeyAsAHeaderNotAQueryParam: an API key in a
// query string leaks into proxy logs, browser history and Referer
// headers. It belongs in a header. This pins that.
func TestFetchAbuseIPDBSendsTheKeyAsAHeaderNotAQueryParam(t *testing.T) {
	var captured *http.Request
	c := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		captured = r
		return &http.Response{StatusCode: http.StatusOK, Body: readCloser(`{"data":{}}`), Header: make(http.Header), Request: r}, nil
	})}

	fetchAbuseIPDB(context.Background(), c, "super-secret-key", "203.0.113.9")

	if captured == nil {
		t.Fatal("no request was made")
	}
	if got := captured.Header.Get("Key"); got != "super-secret-key" {
		t.Errorf("Key header = %q, want the API key", got)
	}
	if strings.Contains(captured.URL.RawQuery, "super-secret-key") {
		t.Errorf("the API key leaked into the query string: %s", captured.URL.RawQuery)
	}
}

// TestFetchShodanEscapesTheIPInThePath: the IP reaches this function
// from a URL path parameter. It is validated as a public IP upstream,
// but the escaping here is the layer that keeps a malformed value from
// altering the request path shape.
//
// Asserts EscapedPath(), not Path: url.URL.Path is the *decoded*
// convenience field and still reads "/1.2.3.4/../../admin" even when
// the wire form is correctly escaped. Checking Path makes this test
// fail against perfectly good code -- which it did, on first write.
func TestFetchShodanEscapesTheIPInThePath(t *testing.T) {
	var captured *http.Request
	c := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		captured = r
		return &http.Response{StatusCode: http.StatusOK, Body: readCloser(`{}`), Header: make(http.Header), Request: r}, nil
	})}

	fetchShodan(context.Background(), c, "1.2.3.4/../../admin")

	if captured == nil {
		t.Fatal("no request was made")
	}
	if got := captured.URL.EscapedPath(); strings.Contains(got, "/../") {
		t.Errorf("path traversal survived escaping, wire path = %s", got)
	}
	if !strings.Contains(captured.URL.EscapedPath(), "%2F") {
		t.Errorf("expected separators to be percent-encoded, wire path = %s", captured.URL.EscapedPath())
	}
}
