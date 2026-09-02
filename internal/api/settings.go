// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"runtime/metrics"
	"sync"
	"sync/atomic"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
)

var settingsLog = logging.New("settings")

// memoryState is the event-buffer figure currently in effect, the range
// it may move within, and the lock that serialises a change to it.
//
// Held on the Server rather than re-derived from the ring's capacity
// because the two are not the same number: capacity is
// maxMemory/AssumedBytesPerEvent rounded down, so reading it back would
// hand an operator a figure a few bytes short of the one they set, and
// the row under the slider would disagree with the handle above it.
type memoryState struct {
	// current is the store.maxMemory in effect, in bytes. Atomic because
	// GET /api/stats reads it on every poll from every open tab while a
	// PUT may be applying.
	current atomic.Int64
	bounds  config.MemoryBounds
	// mu serialises the whole read-validate-persist-resize sequence, so
	// two admins dragging at once cannot interleave into a stored figure
	// that does not match the ring that is running.
	mu sync.Mutex
}

// InitMemory records the event-buffer figure this instance came up on
// and the range it may be moved within. Called once at boot, before the
// server starts serving -- see main.go, which is also where the decision
// between the config file's figure and the stored one is made and
// logged.
func (s *Server) InitMemory(current config.ByteSize, bounds config.MemoryBounds) {
	s.memory.current.Store(int64(current))
	s.memory.bounds = bounds
}

// StoreSettings is the memory group's whole state on the wire: what the
// buffer is set to, what it may be set to, and what the process is
// actually costing the host right now.
type StoreSettings struct {
	// MaxMemory is store.maxMemory in effect, in bytes.
	MaxMemory int64 `json:"maxMemory"`
	// Min and Max are the ends of the slider, in bytes. See
	// config.MaxMemoryCeiling for the headroom rule that produced Max.
	Min int64 `json:"min"`
	Max int64 `json:"max"`
	// HostTotal is what Max is a share of -- the cgroup limit if this
	// process is in one, otherwise the machine's RAM. Zero when nothing
	// could be read, in which case the UI says the ceiling is a
	// conservative default rather than naming a total it does not know.
	HostTotal int64 `json:"hostTotal"`
	// BytesPerEvent is config.AssumedBytesPerEvent, so the frontend can
	// turn a proposed budget into an event count without a second
	// constant to keep in step with this one.
	BytesPerEvent int64 `json:"bytesPerEvent"`
	// Resident is what this process currently holds from the operating
	// system. It is the "and what is it actually costing" half of the
	// trade-off: a budget is a promise, this is the bill.
	Resident int64 `json:"resident"`
	// Stored says the figure came from the settings store rather than
	// the config file -- i.e. somebody set it here. The UI does not draw
	// this today; it is what makes the JSON self-explanatory to anyone
	// reading it from a script or a log.
	Stored bool `json:"stored"`
}

// storeSettings assembles the current state.
func (s *Server) storeSettings() StoreSettings {
	stored := false
	if s.Settings != nil {
		_, stored = s.Settings.MaxMemory()
	}
	return StoreSettings{
		MaxMemory:     s.memory.current.Load(),
		Min:           int64(s.memory.bounds.Min),
		Max:           int64(s.memory.bounds.Max),
		HostTotal:     int64(s.memory.bounds.HostTotal),
		BytesPerEvent: config.AssumedBytesPerEvent,
		Resident:      residentBytes(),
		Stored:        stored,
	}
}

// storeSettingsRequest is PUT /api/settings/store's body. Bytes, not a
// "480MiB" string: the slider works in exact byte counts and a unit
// suffix would only add a parse that can disagree with the figure the
// operator was shown.
type storeSettingsRequest struct {
	MaxMemory int64 `json:"maxMemory"`
}

// handleStoreSettingsUpdate sets store.maxMemory on the running
// instance: it stores the figure, resizes the ring to match, and records
// who did it.
//
// Order matters, and it is store-then-resize. The resize cannot fail --
// Resize has no error path, it either grows or drops the oldest -- while
// the store write can, and an operator whose change took visible effect
// on the running buffer but was never written would be told nothing and
// find it gone at the next restart. Storing first means the only failure
// is the honest one: nothing changed, and the response says so.
//
// Admin-only. This is an instance-wide setting with a real cost to the
// host, and shrinking it destroys held history -- squarely the tier
// authzMatrix calls admin, alongside accounts, tokens and the other
// settings that change what the instance *is* rather than what it is
// watching.
func (s *Server) handleStoreSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Settings == nil || s.Store == nil {
		http.Error(w, "the event buffer is not adjustable on this instance", http.StatusServiceUnavailable)
		return
	}

	var req storeSettingsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	proposed := config.ByteSize(req.MaxMemory)
	if err := s.memory.bounds.ValidateMaxMemory(proposed); err != nil {
		// The operator's own words back to them: the slider cannot reach
		// an out-of-range figure, so anything arriving here came from a
		// script or a stale page and deserves to be told which end it
		// hit rather than a bare 400.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.memory.mu.Lock()
	defer s.memory.mu.Unlock()

	previous := config.ByteSize(s.memory.current.Load())
	if err := s.Settings.SetMaxMemory(int64(proposed)); err != nil {
		settingsLog.Error(fmt.Sprintf("storing the event-buffer budget failed: %v -- the buffer was left at %s", err, previous))
		http.Error(w, "the new event-buffer size could not be stored, so nothing was changed", http.StatusInternalServerError)
		return
	}

	capacity := config.Store{MaxMemory: proposed}.Capacity()
	kept, evicted := s.Store.Resize(capacity)
	s.memory.current.Store(int64(proposed))

	detail := fmt.Sprintf("%s -> %s (%d events; %d held, %d let go)", previous, proposed, capacity, kept, evicted)
	s.Audit.Record(auditActor(r), "settings.store_max_memory", "store.maxMemory", detail)
	settingsLog.Info("event buffer resized: " + detail)

	writeJSON(w, http.StatusOK, s.storeSettings())
}

// residentBytes reports what this process currently holds from the
// operating system.
//
// Read through runtime/metrics rather than runtime.ReadMemStats: this is
// on GET /api/stats, which every open tab polls every few seconds, and
// ReadMemStats stops the world for the duration. The two classes below
// are the runtime's own definition of "mapped and not yet handed back",
// which is what an operator comparing this against `docker stats` is
// looking at.
//
// It is the Go runtime's view, not the kernel's RSS -- close enough for
// a trade-off readout, and it needs no /proc, so it is the same number
// on every platform the binary builds for.
func residentBytes() int64 {
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	metrics.Read(samples)
	for _, s := range samples {
		if s.Value.Kind() != metrics.KindUint64 {
			return 0 // a runtime that does not publish these; say nothing rather than guess
		}
	}
	total := samples[0].Value.Uint64()
	released := samples[1].Value.Uint64()
	if released > total {
		return 0
	}
	return int64(total - released)
}
