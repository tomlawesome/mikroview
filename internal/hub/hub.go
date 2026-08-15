// SPDX-License-Identifier: AGPL-3.0-only

// Package hub fans newly inserted events out to connected WebSocket
// clients for the live-tail view.
package hub

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/tomlawesome/mikroview/internal/store"
)

// clientQueueSize bounds how many pending events a single slow client can
// accumulate before newer events start evicting older queued ones — a
// stalled browser tab must never be able to push back on ingestion.
//
// Generous on purpose: a backgrounded/throttled browser tab (very common
// in practice -- mikroview left open in a background tab while working
// elsewhere) can stall its WS read loop for well beyond what a small
// buffer covers, and the resulting "N events dropped" banner reads as
// alarming even though nothing is actually wrong.
//
// It used to be 20,000, justified as "a few MB per connected client at
// worst" against "~a few hundred bytes per store.Event". That arithmetic
// was wrong by about 3x: store.Event is 464 bytes, so the channel's
// buffer alone was 8.85 MiB per client, allocated in full the moment the
// connection registered rather than as events arrived. Roughly fifteen
// connections exceeded the 128 MiB the CI smoke test runs mikroview
// under. /api/ws needs a valid session, so this is not an
// unauthenticated path -- but "a signed-in non-admin can open fifteen
// tabs" is not much of a barrier. See #285 finding 17.
//
// 5,000 is 2.21 MiB per client, still eight minutes of stall at a
// sustained 10 events/sec, which is well past what tab throttling
// produces. The real bound is maxClients below: the two together cap the
// hub's fan-out memory at a number that fits the documented container,
// which one alone cannot do.
const clientQueueSize = 5_000

// maxClients caps concurrent WebSocket subscribers.
//
// ClientCount() already existed and was reported on /api/stats; nothing
// acted on it, so the number of clients -- and therefore the hub's total
// memory -- was bounded only by how many tabs someone opened. With
// clientQueueSize above, this holds the worst case to about 35 MiB.
//
// 16 is far more than this tool's shape calls for (a handful of
// concurrent viewers, and a browser opens one connection per tab, not
// per view). A refused connection is told why, rather than being
// dropped, so the sixteenth tab shows a reason instead of appearing
// broken. A var so tests need not open 16 real connections.
var maxClients = 16

// ErrTooManyClients is returned by Register when maxClients is already
// reached. The caller should refuse the upgrade and say so.
var ErrTooManyClients = errors.New("hub: too many live connections")

type client struct {
	id   uint64
	send chan store.Event
	// dropped counts events evicted from this client's queue because it
	// fell behind (see Broadcast). Exposed via Register's dropped
	// accessor so the WS handler can tell the browser it's missing
	// events, rather than that silently never showing up anywhere.
	dropped atomic.Uint64
}

// Hub fans out newly inserted events to connected WebSocket clients. Every
// event goes to every client unfiltered — the frontend applies filters
// locally against its own buffer — so there is no per-connection filter
// state to manage here, keeping Broadcast a single cheap fan-out loop.
type Hub struct {
	mu      sync.Mutex
	clients map[uint64]*client
	nextID  uint64
}

func New() *Hub {
	return &Hub{clients: make(map[uint64]*client)}
}

// Register adds a new client and returns its event channel, a function
// reporting how many events have been dropped for it so far (see
// Broadcast), and an unregister function the caller must invoke exactly
// once when the connection ends.
//
// It returns ErrTooManyClients once maxClients are already connected --
// each client costs clientQueueSize * sizeof(store.Event) immediately,
// so "one more is free" is not true here.
func (h *Hub) Register() (events <-chan store.Event, dropped func() uint64, unregister func(), err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.clients) >= maxClients {
		return nil, nil, nil, ErrTooManyClients
	}

	h.nextID++
	id := h.nextID
	c := &client{id: id, send: make(chan store.Event, clientQueueSize)}
	h.clients[id] = c

	var once sync.Once
	unreg := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.clients, id)
			h.mu.Unlock()
		})
	}

	return c.send, c.dropped.Load, unreg, nil
}

// Broadcast delivers e to every connected client's queue. If a client's
// queue is full (a slow/stalled consumer), the oldest queued event for
// that client is dropped to make room — Broadcast never blocks on a slow
// reader, since it's called from the same goroutine that writes to Store.
func (h *Hub) Broadcast(e store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, c := range h.clients {
		select {
		case c.send <- e:
		default:
			select {
			case <-c.send:
				c.dropped.Add(1)
			default:
			}
			select {
			case c.send <- e:
			default:
			}
		}
	}
}

// ClientCount reports how many clients are currently registered.
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
