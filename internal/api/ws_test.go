package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tomlawesome/mikroview/internal/store"
)

func TestHandleWSBroadcastsInsertedEvents(t *testing.T) {
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
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
	s, _ := newTestServer()
	ts := httptest.NewServer(s.Routes())
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

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var env wsEnvelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}

	if len(env.Events) != 5 {
		t.Errorf("expected all 5 events batched into one frame, got %d", len(env.Events))
	}
}
