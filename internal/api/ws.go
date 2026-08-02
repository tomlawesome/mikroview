package api

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomlawesome/mikroview/internal/store"
)

const (
	wsBatchInterval = 50 * time.Millisecond
	wsBatchMaxSize  = 100
	wsWriteTimeout  = 10 * time.Second
	wsPongTimeout   = 60 * time.Second
	wsPingInterval  = 30 * time.Second
)

// CheckOrigin always allows: this is a trusted-LAN, no-auth deployment by
// explicit design (see docs/configuration.md), so origin checking would
// add friction without adding real protection.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	events, dropped, unregister := s.Hub.Register()
	defer unregister()

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
