// SPDX-License-Identifier: AGPL-3.0-only

package hub

import (
	"errors"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func TestBroadcastDeliversToRegisteredClient(t *testing.T) {
	h := New()
	events, _, unregister, _ := h.Register()
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
	e1, _, u1, _ := h.Register()
	e2, _, u2, _ := h.Register()
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
	_, _, unregister, _ := h.Register() // never drained
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

func TestBroadcastReportsDroppedCount(t *testing.T) {
	h := New()
	_, dropped, unregister, _ := h.Register() // never drained
	defer unregister()

	for i := 0; i < clientQueueSize+50; i++ {
		h.Broadcast(store.Event{ID: uint64(i)})
	}

	if got := dropped(); got != 50 {
		t.Errorf("dropped() = %d, want 50 (queue holds %d, so the next 50 each evict one)", got, clientQueueSize)
	}
}

func TestUnregisterStopsDelivery(t *testing.T) {
	h := New()
	events, _, unregister, _ := h.Register()
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

// Each subscriber's channel buffer is allocated in full the moment it
// registers, so "one more client is free" was never true. The old
// clientQueueSize of 20,000 was justified as "a few MB per connected
// client at worst" -- arithmetic that was out by about 3x, since
// store.Event is 464 bytes and 20,000 of them is 8.85 MiB. Roughly
// fifteen connections exceeded the 128 MiB the CI smoke test runs
// mikroview under, and nothing capped the client count even though
// ClientCount() existed and was already reported on /api/stats.
//
// /api/ws needs a valid session, so this is not an unauthenticated
// path -- but "a signed-in non-admin can open fifteen tabs" is not much
// of a barrier. See #285 finding 17.
func TestRegisterRefusesBeyondMaxClients(t *testing.T) {
	prev := maxClients
	maxClients = 3
	t.Cleanup(func() { maxClients = prev })

	h := New()
	var unregisters []func()
	for i := 0; i < maxClients; i++ {
		_, _, unreg, err := h.Register()
		if err != nil {
			t.Fatalf("client %d was refused below the cap: %v", i, err)
		}
		unregisters = append(unregisters, unreg)
	}

	if _, _, _, err := h.Register(); !errors.Is(err, ErrTooManyClients) {
		t.Fatalf("Register past the cap returned %v, want ErrTooManyClients", err)
	}
	if got := h.ClientCount(); got != maxClients {
		t.Errorf("ClientCount = %d, want %d -- a refused client must not be registered", got, maxClients)
	}

	// A slot must come back when a client leaves, or the cap becomes a
	// permanent lockout after enough reconnects.
	unregisters[0]()
	if _, _, _, err := h.Register(); err != nil {
		t.Errorf("Register after a disconnect was refused: %v", err)
	}
}

// The two bounds together are what makes the worst case finite; either
// alone does not. Asserted as a product rather than as two magic
// numbers, so raising one deliberately still forces a look at the other.
func TestFanOutMemoryWorstCaseFitsTheDocumentedContainer(t *testing.T) {
	const eventBytes = 464 // store.Event on 64-bit; pinned by internal/store's own test
	worst := maxClients * clientQueueSize * eventBytes
	if limit := 64 << 20; worst > limit {
		t.Errorf("maxClients(%d) * clientQueueSize(%d) * %d bytes = %d bytes, over the %d-byte budget for fan-out alone",
			maxClients, clientQueueSize, eventBytes, worst, limit)
	}
}
