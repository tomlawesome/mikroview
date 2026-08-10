// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSRFGuardBlocksPrivateAndCGNAT is the load-bearing fetch test. A
// feed URL that resolves to a non-public address must be refused before
// connect -- covering the ranges Go's own predicates miss, which is the
// whole reason the guard is an explicit list.
func TestSSRFGuardBlocksPrivateAndCGNAT(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1:80",       // loopback
		"10.0.0.1:80",        // RFC1918
		"169.254.169.254:80", // cloud metadata, link-local
		"100.64.0.1:80",      // CGNAT -- Go's Is* predicates all return false
		"192.0.0.1:80",       // IETF protocol assignments
		"[::1]:80",           // v6 loopback
	} {
		if err := guardDial("tcp", host, nil); err == nil {
			t.Errorf("guardDial permitted %s, want refusal", host)
		}
	}
}

func TestSSRFGuardAllowsPublic(t *testing.T) {
	for _, host := range []string{"8.8.8.8:443", "1.1.1.1:443", "[2606:4700:4700::1111]:443"} {
		if err := guardDial("tcp", host, nil); err != nil {
			t.Errorf("guardDial refused public %s: %v", host, err)
		}
	}
}

// TestConditionalGET proves a 304 is reported as not-modified and the
// stored ETag drives the next request's If-None-Match.
func TestConditionalGET(t *testing.T) {
	const etag = `"abc123"`
	var sawINM string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawINM = r.Header.Get("If-None-Match")
		if sawINM == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write([]byte("1.2.3.0/24\n"))
	}))
	defer srv.Close()

	// The guard would block the test server's 127.0.0.1 address, so run
	// the fetch through a client without it -- the guard has its own
	// test above.
	c := New(nil, testLog())
	c.client = &fetchClient{http: srv.Client()}

	body, notModified, err := c.client.fetch(context.Background(), c, SourceTor, srv.URL)
	if err != nil || notModified || !strings.Contains(string(body), "1.2.3.0/24") {
		t.Fatalf("first fetch: body=%q notModified=%v err=%v", body, notModified, err)
	}
	if c.etag[SourceTor] != etag {
		t.Fatalf("ETag not stored: %q", c.etag[SourceTor])
	}

	_, notModified, err = c.client.fetch(context.Background(), c, SourceTor, srv.URL)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !notModified {
		t.Error("second fetch should have been a 304")
	}
	if sawINM != etag {
		t.Errorf("If-None-Match on the second request = %q, want %q", sawINM, etag)
	}
}

// TestRefreshKeepsLastGoodOnFailure covers fail-to-last-known-good: a
// source that errors on refresh keeps serving what it had.
func TestRefreshKeepsLastGoodOnFailure(t *testing.T) {
	var serve200 = true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serve200 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("1.2.3.0/24\n"))
	}))
	defer srv.Close()

	c := New([]string{string(SourceTor)}, testLog())
	c.client = &fetchClient{http: srv.Client()}
	c.sources[SourceTor] = feedDef{Source: SourceTor, Category: CategoryTor, Label: "Tor", URL: srv.URL, Parse: parseTorList}

	c.Refresh(context.Background())
	if !c.Lookup("1.2.3.4").Matched {
		t.Fatal("first refresh did not load the feed")
	}

	serve200 = false
	c.Refresh(context.Background())
	if !c.Lookup("1.2.3.4").Matched {
		t.Error("a failed refresh dropped the last-good data instead of keeping it")
	}
}

// TestRefreshRejectsPoisonedDelta covers the coverage-delta guard: a feed
// that suddenly claims far more space than before is rejected, and the
// prior data kept.
func TestRefreshRejectsPoisonedDelta(t *testing.T) {
	payload := "1.2.3.0/24\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := New([]string{string(SourceTor)}, testLog())
	c.client = &fetchClient{http: srv.Client()}
	c.sources[SourceTor] = feedDef{Source: SourceTor, Category: CategoryTor, Label: "Tor", URL: srv.URL, Parse: parseTorList}

	c.Refresh(context.Background())

	// Now the feed balloons to a /8 -- 65536x the address space.
	payload = "9.0.0.0/8\n"
	c.Refresh(context.Background())

	if c.Lookup("9.0.0.1").Matched {
		t.Error("a poisoned oversized delta was adopted instead of rejected")
	}
	if !c.Lookup("1.2.3.4").Matched {
		t.Error("rejecting the poisoned delta also dropped the last-good data")
	}
}
