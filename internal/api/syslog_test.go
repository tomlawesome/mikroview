// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/syslog"
)

// TestHandleSyslogLossClearAvailableToUserNotViewer mirrors
// TestHandleFlagsClearAllAvailableToUserNotViewer: #1015's clear-all
// copies POST /api/flags/clear-all's gate exactly (same "user role
// required" 403 for a viewer, same CSRF header requirement on every
// non-GET route, enforced centrally -- see
// TestMutatingRequestWithoutCSRFHeaderIsRejectedOnceAuthActive).
func TestHandleSyslogLossClearAvailableToUserNotViewer(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()
	postJSON(t, adminClient, ts.URL+"/api/auth/users", createUserRequest{Username: "watcher", Password: "password789", Role: "viewer"}).Body.Close()

	userClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, userClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()

	viewerClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, viewerClient, ts.URL+"/api/auth/login", credentialsRequest{Username: "watcher", Password: "password789"}).Body.Close()

	viewerReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/syslog/loss/clear", nil)
	viewerReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	viewerResp, err := viewerClient.Do(viewerReq)
	if err != nil {
		t.Fatal(err)
	}
	defer viewerResp.Body.Close()
	if viewerResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a viewer clear to be forbidden, got %d", viewerResp.StatusCode)
	}

	userReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/syslog/loss/clear", nil)
	userReq.Header.Set(csrfHeaderName, csrfHeaderValue)
	userResp, err := userClient.Do(userReq)
	if err != nil {
		t.Fatal(err)
	}
	defer userResp.Body.Close()
	if userResp.StatusCode != http.StatusOK {
		t.Errorf("expected a user clear to succeed, got %d", userResp.StatusCode)
	}

	noHeaderReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/syslog/loss/clear", nil)
	noHeaderResp, err := userClient.Do(noHeaderReq)
	if err != nil {
		t.Fatal(err)
	}
	defer noHeaderResp.Body.Close()
	if noHeaderResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected a request missing the CSRF header to be rejected with 403, got %d", noHeaderResp.StatusCode)
	}
}

// TestHandleSyslogLossClearZeroesCountersAndAudits drives real loss
// through a real syslog.ServeTCP listener -- the same technique
// internal/syslog's own tests use (see e.g.
// TestPerSourceConnectionCap and the oversized-message tests) -- rather
// than a synthetic shortcut, since syslog.ClearLoss has no test-only
// backdoor for setting counters from outside its package. That proves
// the clear endpoint really does reach and reset the listener's global
// state, not just that it returns 200.
//
// Two independent counters are driven: Rejected, by opening more than
// the (default, unconfigured) 8-connection per-source cap from one
// address without completing a line, and Oversized, by sending one
// message past the 64KiB per-message limit with no terminator. Both
// must read zero after the clear, and the audit entry must carry both
// totals as they stood immediately before it.
func TestHandleSyslogLossClearZeroesCountersAndAudits(t *testing.T) {
	// However this test ends, leave the package-global syslog counters
	// zeroed for whatever test runs after it in this binary (they are
	// process-wide state, not per-server) -- see syslog.ClearLoss.
	t.Cleanup(func() { syslog.ClearLoss() })

	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	adminClient := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, adminClient, ts.URL+"/api/auth/register", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	out := make(chan syslog.RawMessage, 32)
	go syslog.ServeTCP(t.Context(), ln, out)

	// Rejected: more than the per-source cap (8, unconfigured) from one
	// address, held open without a line -- exactly what a locked-out
	// sender looks like to the accept loop.
	var held []net.Conn
	t.Cleanup(func() {
		for _, c := range held {
			c.Close()
		}
	})
	for i := 0; i < 12; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			held = append(held, c)
		}
	}

	// Oversized: one message past the 64KiB cap, no newline anywhere in
	// it. Dialed from a distinct loopback address (127.0.0.2, not
	// 127.0.0.1) so it isn't itself turned away by the per-source cap
	// the 12 held connections above have already filled -- the listener
	// keys rejection on source host, and 127.0.0.2 is a different one.
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
	oversizedConn, err := dialer.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// The counter only increments once the listener is *continuing* to
	// discard an already-over-cap message (internal/syslog's
	// handleTCPConn counts the tail, not the first truncated delivery
	// that crosses the cap) -- so this has to be two writes: one to
	// cross the 64KiB cap, a second to give it something to discard and
	// count. Neither carries a newline. The pause between them matters:
	// written back to back with no gap, the kernel can hand both to the
	// listener's very first Read of the pair, so the cap-crossing and
	// the "more still arriving" it needs to count land in the same
	// read and nothing is ever left to discard separately -- observed
	// concretely as this test failing reliably without -race (which
	// slows the goroutine down just enough to separate them by
	// accident) while passing reliably with it.
	if _, err := oversizedConn.Write([]byte(strings.Repeat("A", 70000))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := oversizedConn.Write([]byte(strings.Repeat("B", 100))); err != nil {
		t.Fatal(err)
	}
	// Close now, rather than leaving the connection open: while it stays
	// open the listener can keep delivering more discard reads (each its
	// own increment) at its own pace, which raced the "before" snapshot
	// below against the clear-all call actually landing -- the Oversized
	// total was still climbing between the two under -race's slower
	// scheduling. Closing bounds that: at most one more increment for
	// any already-buffered tail arriving with EOF, and none after.
	oversizedConn.Close()

	deadline := time.Now().Add(3 * time.Second)
	var before syslog.ListenerStats
	stableSince := time.Time{}
	lastOversized := ^uint64(0)
	for time.Now().Before(deadline) {
		s := syslog.Stats()
		if s.Rejected != 0 && s.Oversized != 0 {
			if s.Oversized != lastOversized {
				lastOversized = s.Oversized
				stableSince = time.Now()
			} else if time.Since(stableSince) >= 100*time.Millisecond {
				before = s
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.Rejected == 0 {
		t.Fatal("expected at least one rejection before testing the clear -- the driving traffic did not land")
	}
	if before.Oversized == 0 {
		t.Fatal("expected at least one oversized message before testing the clear -- the driving traffic did not land, or never stabilised")
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/syslog/loss/clear", nil)
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", resp.StatusCode)
	}

	after := syslog.Stats()
	if after.Rejected != 0 || after.Dropped != 0 || after.RejectedConfigured != 0 || after.Oversized != 0 {
		t.Errorf("totals after clear = %+v, want all zero", after)
	}
	if after.Loss.Rejected.Recent != 0 || after.Loss.Rejected.Active || after.Loss.Rejected.LastAt != nil {
		t.Errorf("loss.rejected after clear = %+v, want a zeroed, inactive, never-moved entry", after.Loss.Rejected)
	}
	if after.Loss.Oversized.Recent != 0 || after.Loss.Oversized.Active || after.Loss.Oversized.LastAt != nil || after.Loss.Oversized.Host != "" {
		t.Errorf("loss.oversized after clear = %+v, want a zeroed, inactive, never-moved, hostless entry", after.Loss.Oversized)
	}

	var matches []audit.Entry
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "ingest_loss.clear_all" {
			matches = append(matches, e)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 ingest_loss.clear_all audit entry, got %d: %+v", len(matches), matches)
	}
	wantSubstr := []string{
		"rejected=" + strconv.FormatUint(before.Rejected, 10),
		"oversized=" + strconv.FormatUint(before.Oversized, 10),
	}
	for _, want := range wantSubstr {
		if !strings.Contains(matches[0].Detail, want) {
			t.Errorf("audit detail %q does not contain %q (the pre-clear total)", matches[0].Detail, want)
		}
	}
}
