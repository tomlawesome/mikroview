// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomlawesome/mikroview/internal/store"
)

func TestHandleWSBroadcastsInsertedEvents(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Give the server a moment to register the client before broadcasting,
	// since Register() happens inside the handler goroutine after Upgrade.
	time.Sleep(50 * time.Millisecond)

	s.Hub.Broadcast(store.Event{ID: 99, Action: store.ActionAccept, SrcIP: "10.0.0.1"})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env wsEnvelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if env.Type != "events" || len(env.Events) != 1 || env.Events[0].ID != 99 {
		t.Errorf("unexpected envelope: %+v", env)
	}
}

func TestHandleWSBatchesMultipleEvents(t *testing.T) {
	s, _ := newTestServer(t)
	ts := httptest.NewServer(s.mux())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 5; i++ {
		s.Hub.Broadcast(store.Event{ID: uint64(i)})
	}

	// Batching means these 5 rapid broadcasts should arrive in very few
	// frames -- but not necessarily exactly one. This test's own setup
	// sleep (above) is the same duration as wsBatchInterval, so the
	// server's flush ticker can legitimately fire in the middle of the
	// broadcast loop and split it across two frames; that's a real,
	// valid outcome of the ticker/channel race, not a bug, so asserting
	// on a single frame here was flaky by construction. Accumulate
	// across frames instead, and only fail if batching isn't happening
	// at all (i.e. one frame per event).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	seen := map[uint64]bool{}
	frames := 0
	for len(seen) < 5 && frames < 5 {
		var env wsEnvelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("ReadJSON failed after %d/5 events in %d frames: %v", len(seen), frames, err)
		}
		frames++
		for _, e := range env.Events {
			seen[e.ID] = true
		}
	}

	if len(seen) != 5 {
		t.Errorf("expected all 5 events to arrive, got %d across %d frames", len(seen), frames)
	}
	if frames > 2 {
		t.Errorf("expected batching to deliver 5 rapid events in at most 2 frames, took %d", frames)
	}
}

// --- #375: a revoked session's socket must close, not keep streaming ----

// shrinkWSPingInterval swaps wsPingInterval (see its own doc comment) down
// to d for the duration of the calling test, so a test proving the
// revalidation-on-ping behaviour doesn't have to wait out a real 30s tick.
func shrinkWSPingInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := wsPingInterval
	wsPingInterval = d
	t.Cleanup(func() { wsPingInterval = prev })
}

// dialWSAs opens a live-tail connection carrying sessionID as its cookie,
// the same way a browser's WebSocket upgrade would -- gorilla's dialer
// doesn't share a cookie jar with an *http.Client, so the value has to be
// attached to the upgrade request by hand.
func dialWSAs(t *testing.T, ts *httptest.Server, sessionID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	header := http.Header{"Cookie": []string{sessionCookieName + "=" + sessionID}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dialing /api/ws: %v", err)
	}
	return conn
}

// assertWSAlive proves events are still reaching conn -- the "before" half
// of every test below, so a later close is known to be the revocation
// firing rather than the connection having never worked.
func assertWSAlive(t *testing.T, s *Server, conn *websocket.Conn, eventID uint64) {
	t.Helper()
	s.Hub.Broadcast(store.Event{ID: eventID})
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env wsEnvelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("expected the connection to still be delivering events: %v", err)
	}
}

// assertWSClosesCleanly waits up to within for conn to close, and requires
// the close to have arrived as a real WebSocket close frame (a
// *websocket.CloseError) rather than the connection merely going dead --
// see closeRevoked's doc comment for why that distinction matters: a clean
// close is what makes the browser's onclose (and so ws.ts's reconnect
// loop) fire the same way a normal disconnect does.
func assertWSClosesCleanly(t *testing.T, conn *websocket.Conn, within time.Duration) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(within))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("expected the connection to close, but it kept delivering")
	}
	if _, ok := err.(*websocket.CloseError); !ok {
		t.Errorf("expected a clean WebSocket close frame, got %v (%T)", err, err)
	}
}

// TestHandleWSClosesOnLogout is the first of #375's three revocation
// paths: SessionStore.Revoke, called by handleAuthLogout for exactly the
// session the caller is using.
func TestHandleWSClosesOnLogout(t *testing.T) {
	shrinkWSPingInterval(t, 20*time.Millisecond)
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := registerAdmin(t, ts)
	sessionID := sessionIDFromJar(t, client, ts.URL)

	conn := dialWSAs(t, ts, sessionID)
	defer conn.Close()
	assertWSAlive(t, s, conn, 1)

	// The same session logging out from "another tab" -- a second request
	// carrying the identical cookie, which is what a second tab in the
	// same browser context actually sends.
	postJSON(t, client, ts.URL+"/api/auth/logout", map[string]any{}).Body.Close()

	assertWSClosesCleanly(t, conn, 2*time.Second)
}

// TestHandleWSClosesOnPasswordChange is #375's second path:
// SessionStore.RevokeAllForUser, called by handleAuthChangePassword. It
// revokes every session for the account -- including the caller's own,
// which a freshly minted cookie papers over for ordinary requests (see
// TestChangePasswordRotatesTheSessionAndEndsOthers) but not for a socket
// that already has the old, now-dead session ID baked into its request.
// This proves the *other* session's socket -- the one the "sign out
// everywhere" promise is actually about -- closes.
func TestHandleWSClosesOnPasswordChange(t *testing.T) {
	shrinkWSPingInterval(t, 20*time.Millisecond)
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	admin := registerAdmin(t, ts)
	other := loggedInClient(t, ts.URL, "admin", "password123")
	otherSessionID := sessionIDFromJar(t, other, ts.URL)

	conn := dialWSAs(t, ts, otherSessionID)
	defer conn.Close()
	assertWSAlive(t, s, conn, 1)

	resp := postJSON(t, admin, ts.URL+"/api/auth/password", changePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "a-brand-new-password",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change password returned %d, want 200", resp.StatusCode)
	}

	assertWSClosesCleanly(t, conn, 2*time.Second)
}

// TestHandleWSClosesOnAccountDeletion is #375's third path:
// SessionStore.RevokeAllForUser, called by handleAuthDeleteUser -- "Sessions
// and tokens go with it" (auth.go:690).
func TestHandleWSClosesOnAccountDeletion(t *testing.T) {
	shrinkWSPingInterval(t, 20*time.Millisecond)
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	admin := registerAdmin(t, ts)
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{
		Username: "viewer", Password: "password456", Role: "user",
	}).Body.Close()
	viewer, ok := s.Auth.ByUsername("viewer")
	if !ok {
		t.Fatal("the account under test was not created")
	}

	viewerClient := loggedInClient(t, ts.URL, "viewer", "password456")
	viewerSessionID := sessionIDFromJar(t, viewerClient, ts.URL)

	conn := dialWSAs(t, ts, viewerSessionID)
	defer conn.Close()
	assertWSAlive(t, s, conn, 1)

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/users/"+viewer.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(csrfHeaderName, csrfHeaderValue)
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete user returned %d, want 200", resp.StatusCode)
	}

	assertWSClosesCleanly(t, conn, 2*time.Second)
}

// TestHandleWSStaysOpenForAValidSession guards against the revalidation
// itself becoming the bug -- a socket must survive any number of ping
// cycles while its session is untouched, not just fail to survive a
// revoked one.
func TestHandleWSStaysOpenForAValidSession(t *testing.T) {
	shrinkWSPingInterval(t, 20*time.Millisecond)
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := registerAdmin(t, ts)
	sessionID := sessionIDFromJar(t, client, ts.URL)

	conn := dialWSAs(t, ts, sessionID)
	defer conn.Close()

	// Outlive several revalidation ticks before checking anything.
	time.Sleep(150 * time.Millisecond)

	assertWSAlive(t, s, conn, 42)
}
