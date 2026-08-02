package hub

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func TestBroadcastDeliversToRegisteredClient(t *testing.T) {
	h := New()
	events, unregister := h.Register()
	defer unregister()

	h.Broadcast(store.Event{ID: 1})

	select {
	case e := <-events:
		if e.ID != 1 {
			t.Errorf("ID = %d, want 1", e.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}

func TestBroadcastFanOutToMultipleClients(t *testing.T) {
	h := New()
	e1, u1 := h.Register()
	e2, u2 := h.Register()
	defer u1()
	defer u2()

	h.Broadcast(store.Event{ID: 42})

	for _, ch := range []<-chan store.Event{e1, e2} {
		select {
		case e := <-ch:
			if e.ID != 42 {
				t.Errorf("ID = %d, want 42", e.ID)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for fan-out event")
		}
	}
}

func TestBroadcastNeverBlocksOnFullSlowClient(t *testing.T) {
	h := New()
	_, unregister := h.Register() // never drained
	defer unregister()

	done := make(chan struct{})
	go func() {
		for i := 0; i < clientQueueSize+50; i++ {
			h.Broadcast(store.Event{ID: uint64(i)})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked on a full, undrained client queue")
	}
}

func TestUnregisterStopsDelivery(t *testing.T) {
	h := New()
	events, unregister := h.Register()
	unregister()

	h.Broadcast(store.Event{ID: 1})

	select {
	case e, ok := <-events:
		if ok {
			t.Errorf("expected no further delivery after unregister, got %+v", e)
		}
	case <-time.After(100 * time.Millisecond):
		// no delivery within a short window is the expected outcome
	}

	if h.ClientCount() != 0 {
		t.Errorf("ClientCount() = %d, want 0", h.ClientCount())
	}
}
