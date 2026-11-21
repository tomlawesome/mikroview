// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"
)

// Issue #1552: the confirm/factor routes hash recovery codes with
// Argon2id and can outlast the server-wide 15 s WriteTimeout. A
// per-route write deadline (http.ResponseController) must give them
// longer, and leave every other route on the server default.

func slowDeadlineServer(t *testing.T) *httptest.Server {
	t.Helper()
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	ts := httptest.NewUnstartedServer(slowAuthWriteDeadline(slow, 2*time.Second))
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

func noKeepAliveClient() *http.Client {
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

func TestWriteDeadline_ListedPathGetsFullReply(t *testing.T) {
	t.Parallel()
	ts := slowDeadlineServer(t)
	for _, p := range []string{"/api/auth/totp/confirm", "/api/auth/login/factor", "/api/auth/users/abc/reset-password"} {
		resp, err := noKeepAliveClient().Post(ts.URL+p, "application/json", nil)
		if err != nil {
			t.Fatalf("POST %s: want a reply, got %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Errorf("POST %s: got %d %q, want 200 \"ok\"", p, resp.StatusCode, body)
		}
	}
}

func TestWriteDeadline_UnlistedPathKeepsServerDeadline(t *testing.T) {
	t.Parallel()
	ts := slowDeadlineServer(t)
	resp, err := noKeepAliveClient().Get(ts.URL + "/api/events")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("GET /api/events: got a reply (%d), want the connection dropped by WriteTimeout", resp.StatusCode)
	}
}

// Every route under /api/auth/ gets the long deadline (#1554: password
// reset ran Argon2id and was missing from the old per-route list).
func TestWriteDeadline_SlowAuthPathsCoverEveryAuthRoute(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path string
		want bool
	}{
		{"/api/auth/users/abc/reset-password", true},
		{"/api/auth/users/abc", true},
		{"/api/auth/totp/confirm", true},
		{"/api/auth/login/factor", true},
		{"/api/events", false},
		{"/api/authx", false},
		{"/", false},
	} {
		if got := isSlowAuthPath(c.path); got != c.want {
			t.Errorf("isSlowAuthPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// main()'s http.Server is built inline and cannot be reached from a
// test, so this checks the source: the Handler given to the production
// http.Server must pass through slowAuthWriteDeadline.
func TestWriteDeadline_ProductionServerHandlerIsWrapped(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)&http\.Server\{.*?Handler:[^\n]*slowAuthWriteDeadline\(`)
	if !re.Match(src) {
		t.Error("main.go: the http.Server Handler is not wrapped by slowAuthWriteDeadline")
	}
}
