// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

// #1448 (the /api/ws keep-alive re-check must not count as activity), end
// to end through the real binary. The re-check runs on a real 30 second
// tick the harness cannot shorten, so each test moves the server's test
// clock in steps shorter than the idle limit and waits out a tick between
// steps (wsTickWait). The idle limit is auth.sessionTTL; 10m is what these
// tests use because the wall-clock waits count towards it too, and the
// tests already take a few minutes each (they run in parallel). The
// harness gives no way to shorten the tick itself.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// wsIdleLimit is the idle limit the tests configure (auth.sessionTTL).
	wsIdleLimit = "10m"
	// wsTickWait is a little over one re-check tick (30 s), so a wait
	// always contains at least one tick.
	wsTickWait = 35 * time.Second
)

// wsWatch is an open /api/ws connection and a reader that notes when it
// closes.
type wsWatch struct {
	conn *websocket.Conn
	done chan struct{}
	err  error
}

// dialWS opens /api/ws with c's session cookie, as a browser tab would.
func dialWS(t *testing.T, a *app, c *client) *wsWatch {
	t.Helper()
	u, _ := url.Parse(a.base)
	var cookies []string
	for _, ck := range c.jar.Cookies(u) {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	if len(cookies) == 0 {
		t.Fatal("test setup: the client holds no session cookie")
	}
	hdr := http.Header{"Cookie": []string{strings.Join(cookies, "; ")}, "Origin": []string{a.base}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.base, "http")+"/api/ws", hdr)
	if err != nil {
		t.Fatalf("dialing /api/ws: %v", err)
	}
	w := &wsWatch{conn: conn, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for {
			if _, _, w.err = conn.ReadMessage(); w.err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return w
}

func (w *wsWatch) isClosed() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// wsWorld is an instance with the short idle limit, an admin to drive the
// clock, and a way to make signed-in viewers with a socket each.
func wsWorld(t *testing.T) *world {
	t.Helper()
	return bootstrap(t, Opts{SessionTTL: wsIdleLimit})
}

// viewerWithSocket signs a fresh enrolled viewer in and opens its socket.
func viewerWithSocket(t *testing.T, w *world, name string) (*client, *wsWatch) {
	t.Helper()
	u := w.enrolled(t, name, "viewer")
	c, r := w.signIn(t, u, u.code(t, w.a, 1))
	wantStatus(t, "sign-in of "+name, r, 200)
	if !c.session(t).Authenticated {
		t.Fatalf("%s is not signed in straight after sign-in", name)
	}
	return c, dialWS(t, w.a, c)
}

// refusedSignIn: a plain API request and the session check both say the
// session is gone.
func refusedSignIn(t *testing.T, what string, c *client) {
	t.Helper()
	if r := c.get("/api/stats"); r.Status == 200 {
		t.Errorf("%s: GET /api/stats = %s, want it refused (sign-in required)", what, r)
	}
	if c.session(t).Authenticated {
		t.Errorf("%s: the session still reads as signed in", what)
	}
}

// #1448 (a) and (b): a socket held open with no other requests is closed
// once the session's idle limit has passed, and the keep-alive re-checks
// that ran meanwhile did not extend the session: a plain API request made
// just past the idle limit (about 10m35s after sign-in, the last re-check
// having run at about 4m30s) is refused. Two viewers, one for each question.
func TestAnIdleSocketIsClosedAtTheIdleLimitAndTheRecheckNeverExtendsTheSession(t *testing.T) {
	t.Parallel()
	w := wsWorld(t)
	_, watched := viewerWithSocket(t, w, "ws-watched")
	probe, _ := viewerWithSocket(t, w, "ws-probe")

	w.advance(t, 4*time.Minute)
	time.Sleep(wsTickWait) // a re-check, at about 4m
	if watched.isClosed() {
		t.Fatalf("the socket closed at 4 minutes, inside the 10 minute idle limit: %v", watched.err)
	}
	w.advance(t, 6*time.Minute) // about 10m35s since sign-in: just past the idle limit
	// (b) no request has been made on the probe's session since sign-in.
	refusedSignIn(t, "a request just past the idle limit after only a socket was open", probe)

	// (a) the other socket closes by the next re-check or the one after,
	// not at the one-day maximum. The clock keeps moving in short steps so
	// that a re-check that counted as activity would keep the session.
	for i := 0; i < 3 && !watched.isClosed(); i++ {
		time.Sleep(wsTickWait)
		w.advance(t, 2*time.Minute)
	}
	if !watched.isClosed() {
		t.Error("a socket held open with no other requests was still open 15 minutes of server time after its last real request; the idle limit is 10 minutes")
	}
}

// #1448 (c), paired: real requests within the idle limit keep both the
// session and the socket alive, across more than two idle limits.
func TestRealRequestsWithinTheIdleLimitKeepTheSessionAndTheSocketAlive(t *testing.T) {
	t.Parallel()
	w := wsWorld(t)
	c, watched := viewerWithSocket(t, w, "ws-busy")

	for i := 1; i <= 5; i++ {
		w.advance(t, 4*time.Minute)
		if !c.session(t).Authenticated { // a real request, inside the idle limit
			t.Fatalf("the session ended on round %d of use every 4 minutes (idle limit 10 minutes)", i)
		}
		time.Sleep(wsTickWait) // a re-check
		if watched.isClosed() {
			t.Fatalf("the socket closed on round %d although its session was in use: %v", i, watched.err)
		}
	}
	if !c.session(t).Authenticated {
		t.Error("the session ended although it was in use every 4 minutes")
	}
}

// #1448 (d), #375 still holds: a session revoked (here: the user signs
// out from another tab, a second request carrying the same cookie) closes
// its socket at the next re-check.
func TestARevokedSessionStillClosesItsSocket(t *testing.T) {
	t.Parallel()
	w := wsWorld(t)
	c, watched := viewerWithSocket(t, w, "ws-revoked")
	time.Sleep(time.Second)
	if watched.isClosed() {
		t.Fatalf("test premise: the socket closed on its own: %v", watched.err)
	}
	wantStatus(t, "sign out", c.post("/api/auth/logout", map[string]any{}), 200)
	select {
	case <-watched.done:
	case <-time.After(wsTickWait + 5*time.Second):
		t.Error("the socket of a signed-out session was still open after a re-check")
	}
}
