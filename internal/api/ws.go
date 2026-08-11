// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/store"
)

var wsLogger = logging.New("ws")

const (
	wsBatchInterval = 50 * time.Millisecond
	wsBatchMaxSize  = 100
	wsWriteTimeout  = 10 * time.Second
	wsPongTimeout   = 60 * time.Second
	wsPingInterval  = 30 * time.Second
)

// CheckOrigin always allows at the gorilla/websocket-upgrader level --
// the actual origin check happens in handleWS below, where s (and so
// s.Auth.Count()) is in scope. Splitting it out to a standalone function
// wouldn't have access to that without awkward global state.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// checkOrigin requires a cross-site WebSocket upgrade to come from this
// origin. A session cookie is a real credential, and cookies are
// attached to cross-site requests regardless of CORS/fetch-origin rules
// -- SameSite=Lax alone isn't a guaranteed defence for a WebSocket
// upgrade specifically, so this check is required, not redundant with
// requireAuth's cookie check: same-origin is what actually stops a
// malicious page opening a WS connection with a victim's cookie and
// streaming every firewall event out of it.
//
// This used to open with `if s.Auth.Count() == 0 { return true }`,
// justified as "mikroview's default, fully-open, trusted-LAN
// deployment" -- a deployment shape that no longer exists. It was also
// unreachable: /api/ws is not in bootstrapExemptPaths, so requireAuth
// returns 503 before handleWS runs while Count()==0.
//
// Unreachable is not the same as harmless, and this codebase has
// already made that argument about itself once: callerIsAdmin's doc
// comment records deleting a callerIsAdminOrOpen bypass that was
// likewise unreachable, because it "read as 'anonymous callers are
// admins under some condition', and would have become live again the
// moment requireAuth was loosened." Identical reasoning, so it goes the
// same way. See #282.
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No Origin header at all is normal for a same-origin
		// non-browser client (curl, server-to-server) -- browsers always
		// send one on a cross-site request, so its absence isn't the
		// attack this check defends against.
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

type wsEnvelope struct {
	Type    string        `json:"type"`
	Events  []store.Event `json:"events,omitempty"`
	Dropped uint64        `json:"dropped,omitempty"`
}

// handleWS serves the live-tail feed: after the client has loaded a
// snapshot via GET /api/events, this pushes every subsequently inserted
// event, unfiltered, batched every wsBatchInterval (or wsBatchMaxSize,
// whichever comes first) into a single WS frame. The frontend applies
// filters client-side — see docs/configuration.md for the rationale.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(r) {
		http.Error(w, "cross-origin WebSocket connections are not allowed", http.StatusForbidden)
		return
	}
	// Registered before the upgrade, so a refusal is a plain HTTP 503
	// the browser can read, rather than a WebSocket that opens and then
	// closes for no stated reason. Each subscriber costs several MiB the
	// moment it registers (see hub.clientQueueSize), so this is a real
	// limit rather than a formality.
	events, dropped, unregister, err := s.Hub.Register()
	if err != nil {
		http.Error(w, "too many live connections are already open; close another tab and retry", http.StatusServiceUnavailable)
		return
	}
	defer unregister()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
		return nil
	})

	// gorilla/websocket requires a dedicated reader to process control
	// frames (pong) and to detect the client going away; we don't expect
	// any application messages from the client.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		defer logging.Recover(wsLogger)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	batchTicker := time.NewTicker(wsBatchInterval)
	defer batchTicker.Stop()
	pingTicker := time.NewTicker(wsPingInterval)
	defer pingTicker.Stop()

	batch := make([]store.Event, 0, wsBatchMaxSize)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		// Dropped is the cumulative total for this connection, not a delta
		// -- the client just needs to know "have I ever missed events,"
		// not track deltas itself.
		if err := conn.WriteJSON(wsEnvelope{Type: "events", Events: batch, Dropped: dropped()}); err != nil {
			return false
		}
		batch = batch[:0]
		return true
	}

	for {
		select {
		case <-closed:
			return
		case e := <-events:
			batch = append(batch, e)
			if len(batch) >= wsBatchMaxSize && !flush() {
				return
			}
		case <-batchTicker.C:
			if !flush() {
				return
			}
		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
