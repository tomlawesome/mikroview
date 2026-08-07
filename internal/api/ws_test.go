// SPDX-License-Identifier: AGPL-3.0-only

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
	s, _ := newTestServer(t)
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
	s, _ := newTestServer(t)
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
