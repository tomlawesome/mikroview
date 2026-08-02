// Package hub fans newly inserted events out to connected WebSocket
// clients for the live-tail view.
package hub

import (
	"sync"

	"github.com/tomlawesome/mikroview/internal/store"
)

// clientQueueSize bounds how many pending events a single slow client can
// accumulate before newer events start evicting older queued ones — a
// stalled browser tab must never be able to push back on ingestion.
const clientQueueSize = 2000

type client struct {
	id   uint64
	send chan store.Event
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

// Register adds a new client and returns its event channel plus an
// unregister function the caller must invoke exactly once when the
// connection ends.
func (h *Hub) Register() (events <-chan store.Event, unregister func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

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

	return c.send, unreg
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
