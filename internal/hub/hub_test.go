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
	sub, _ := h.Register()
	defer sub.Unregister()

	h.Broadcast(store.Event{ID: 1})

	select {
	case e := <-sub.Events:
		if e.ID != 1 {
			t.Errorf("ID = %d, want 1", e.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}

func TestBroadcastFanOutToMultipleClients(t *testing.T) {
	h := New()
	s1, _ := h.Register()
	s2, _ := h.Register()
	defer s1.Unregister()
	defer s2.Unregister()

	h.Broadcast(store.Event{ID: 42})

	for _, ch := range []<-chan store.Event{s1.Events, s2.Events} {
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
	sub, _ := h.Register() // never drained
	defer sub.Unregister()

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
	sub, _ := h.Register() // never drained
	defer sub.Unregister()

	for i := 0; i < clientQueueSize+50; i++ {
		h.Broadcast(store.Event{ID: uint64(i)})
	}

	if got := sub.Dropped(); got != 50 {
		t.Errorf("dropped() = %d, want 50 (queue holds %d, so the next 50 each evict one)", got, clientQueueSize)
	}
}

func TestUnregisterStopsDelivery(t *testing.T) {
	h := New()
	sub, _ := h.Register()
	sub.Unregister()

	h.Broadcast(store.Event{ID: 1})

	select {
	case e, ok := <-sub.Events:
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

// TestNotifyReachesEveryClient pins the fan-out half of Notify: a change
// notice is what lets a screen refetch when a table is pushed, instead of
// waiting out its own poll, so every open tab has to get it -- not just
// whichever one happens to be reading events at the time.
func TestNotifyReachesEveryClient(t *testing.T) {
	h := New()
	s1, _ := h.Register()
	s2, _ := h.Register()
	defer s1.Unregister()
	defer s2.Unregister()

	h.Notify(ChangeRouterState)

	for i, ch := range []<-chan Change{s1.Notices, s2.Notices} {
		select {
		case got := <-ch:
			if got != ChangeRouterState {
				t.Errorf("client %d got change %q, want %q", i, got, ChangeRouterState)
			}
		case <-time.After(time.Second):
			t.Fatalf("client %d never received the change notice", i)
		}
	}
}

// TestNotifyNeverBlocksOnAFullNoticeQueue is the property that makes it
// safe to call from a request handler: the caller is the ingest endpoint,
// which must not be held up by a browser tab that has stopped reading.
// Notices are hints with a poll behind them, so dropping one costs
// latency and nothing else -- see Notify.
func TestNotifyNeverBlocksOnAFullNoticeQueue(t *testing.T) {
	h := New()
	sub, _ := h.Register() // never drained
	defer sub.Unregister()

	done := make(chan struct{})
	go func() {
		for i := 0; i < noticeQueueSize+50; i++ {
			h.Notify(ChangeRouterState)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a full, undrained notice queue")
	}

	// And the ones that did fit are still there: dropping the overflow
	// must not cost the notices already queued.
	if got := len(sub.Notices); got != noticeQueueSize {
		t.Errorf("the notice queue holds %d, want %d", got, noticeQueueSize)
	}
}

// TestNoticesAndEventsDoNotShareAQueue is the reason notices have a
// channel of their own. A slow client has events evicted from its queue
// by design; a notice evicted the same way would be a screen left showing
// a stale answer with nothing to tell it so.
func TestNoticesAndEventsDoNotShareAQueue(t *testing.T) {
	h := New()
	sub, _ := h.Register() // never drained
	defer sub.Unregister()

	h.Notify(ChangeDefinitions)
	for i := 0; i < clientQueueSize+50; i++ {
		h.Broadcast(store.Event{ID: uint64(i)})
	}

	select {
	case got := <-sub.Notices:
		if got != ChangeDefinitions {
			t.Errorf("change = %q, want %q", got, ChangeDefinitions)
		}
	default:
		t.Error("the notice was lost to an event flood -- it must not share the event queue")
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
		sub, err := h.Register()
		if err != nil {
			t.Fatalf("client %d was refused below the cap: %v", i, err)
		}
		unregisters = append(unregisters, sub.Unregister)
	}

	if _, err := h.Register(); !errors.Is(err, ErrTooManyClients) {
		t.Fatalf("Register past the cap returned %v, want ErrTooManyClients", err)
	}
	if got := h.ClientCount(); got != maxClients {
		t.Errorf("ClientCount = %d, want %d -- a refused client must not be registered", got, maxClients)
	}

	// A slot must come back when a client leaves, or the cap becomes a
	// permanent lockout after enough reconnects.
	unregisters[0]()
	if _, err := h.Register(); err != nil {
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
