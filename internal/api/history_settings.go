// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"

	"github.com/tomlawesome/mikroview/internal/config"
)

// HistorySettings is the disk group's whole state on the wire: what is
// set, what is actually held, and which of the two caps is deciding.
//
// The setting and the window held are separate fields rather than one
// number because they are routinely different and the difference is the
// thing an operator must not be misled about -- history.days says 30,
// the disk holds whatever survived the byte cap, and reporting the
// setting as if it were the window would state a month of history this
// deployment does not have. See docs/decisions/event-retention.md and
// #910.
type HistorySettings struct {
	// Keyed reports that a usable key file is mounted. False means the
	// feature cannot run at all on this instance -- there is no
	// unencrypted mode -- so Enabled is reported false whatever is
	// stored, and the PUT refuses.
	Keyed bool `json:"keyed"`
	// Enabled is whether retention is actually running right now, not
	// merely what was asked for: a stored "on" with no key mounted
	// reads false here, because nothing is being written.
	Enabled bool `json:"enabled"`
	// Days and MaxBytes are the two caps in effect -- what is allowed,
	// not what is held.
	Days     int   `json:"days"`
	MaxBytes int64 `json:"maxBytes"`
	// Held is the window actually on disk, or null when nothing is.
	Held *HistoryHeld `json:"held"`
	// Capped says the byte cap, rather than the day count, is what last
	// dropped a day -- the difference between "this is as far back as
	// you asked for" and "this is as far back as the cap allows". False
	// when it cannot be known: an unsure claim here would be worse than
	// none.
	Capped bool `json:"capped"`
	// BytesPerDay is today's rate, taken as the newest *complete* day's
	// file size. Zero when no complete day exists yet -- the day still
	// being written is a partial figure, and multiplying it out would
	// tell an operator their month fits when it does not.
	BytesPerDay int64 `json:"bytesPerDay"`
}

// HistoryHeld is the window of days actually on disk.
type HistoryHeld struct {
	Days   int    `json:"days"`
	Oldest string `json:"oldest"` // YYYY-MM-DD, UTC
	Newest string `json:"newest"` // YYYY-MM-DD, UTC
	Bytes  int64  `json:"bytes"`
}

// HistoryControl is the running on-disk history, as these two
// endpoints see it: something that can report its state and be turned
// on, off or re-capped.
//
// An interface for the same reason engine.RetainedDays is one -- this
// package deliberately knows nothing about keys, day files or the
// in-memory ring's contents. main owns the lifecycle (see
// history_runtime.go); a test wires a stand-in.
type HistoryControl interface {
	// HistorySettings reports the current state, reading the directory
	// for what is actually held.
	HistorySettings() HistorySettings
	// ApplyHistory stores the three settings and makes them true of the
	// running instance: opening the store and handing it what memory
	// holds, or closing it and deleting what is on disk. It returns
	// only when that has happened, so the caller's response describes a
	// finished act rather than an intention.
	ApplyHistory(enabled bool, days int, maxBytes int64) error
}

// historySettingsRequest is PUT /api/settings/history's body.
type historySettingsRequest struct {
	Enabled  bool  `json:"enabled"`
	Days     int   `json:"days"`
	MaxBytes int64 `json:"maxBytes"`
}

// handleHistorySettings reports the on-disk history's state.
//
// Admin-only, and the read as well as the write -- unlike the memory
// group, whose figure rides GET /api/stats for every tier. Two reasons,
// either sufficient: the answer names how much custody data this
// deployment is keeping and for how long, which is exactly the
// infrastructure disclosure /api/config/problems and /api/persistence
// are already admin-gated for; and the whole group is drawn on an admin
// surface, so no other tier has anywhere to put it.
func (s *Server) handleHistorySettings(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.HistoryControl == nil {
		http.Error(w, "the on-disk event history is not adjustable on this instance", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, s.HistoryControl.HistorySettings())
}

// handleHistorySettingsUpdate turns the on-disk history on or off and
// sets its two caps.
//
// Order is store-then-apply, the same way handleStoreSettingsUpdate
// resizes the ring only after the figure is safely written: an operator
// whose change took visible effect but was never stored would be told
// nothing and find it undone at the next restart. ApplyHistory holds
// both halves so this handler cannot get the order wrong.
//
// Turning it off purges before this returns, rather than scheduling it.
// Off has to mean the events are gone (see the ADR and #853), and a
// response that says "off" while last month is still on disk is the
// lie the whole setting exists to avoid.
//
// Admin-only for the reason the memory slider is, only more so: this
// one deletes up to a month of retained evidence in a single call.
func (s *Server) handleHistorySettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.HistoryControl == nil {
		http.Error(w, "the on-disk event history is not adjustable on this instance", http.StatusServiceUnavailable)
		return
	}

	var req historySettingsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	before := s.HistoryControl.HistorySettings()
	if !before.Keyed {
		// 409 rather than 400: the request is well formed and the
		// setting is a real one, but this deployment cannot honour it
		// until somebody mounts a key. A plain sentence, because the
		// operator reading it has to know what to go and do.
		http.Error(w, "no history key file is mounted, and there is no unencrypted mode -- mount a key file and restart before turning the history on", http.StatusConflict)
		return
	}
	if req.Days < 1 {
		http.Error(w, fmt.Sprintf("%d days is not a history to keep -- ask for at least one day", req.Days), http.StatusBadRequest)
		return
	}
	if req.MaxBytes < config.MinRetentionBytes {
		http.Error(w, fmt.Sprintf("%d bytes is smaller than a single day at any realistic rate -- set a cap of at least %d bytes, e.g. 1073741824 for 1 GiB",
			req.MaxBytes, int64(config.MinRetentionBytes)), http.StatusBadRequest)
		return
	}

	if err := s.HistoryControl.ApplyHistory(req.Enabled, req.Days, req.MaxBytes); err != nil {
		settingsLog.Error(fmt.Sprintf("changing the on-disk event history failed: %v", err))
		http.Error(w, "the on-disk event history could not be changed, so nothing was changed", http.StatusInternalServerError)
		return
	}

	after := s.HistoryControl.HistorySettings()
	detail := fmt.Sprintf("%s -> %s", describeHistory(before), describeHistory(after))
	s.Audit.Record(auditActor(r), "settings.history", "history", detail)
	settingsLog.Info("on-disk event history: " + detail)

	writeJSON(w, http.StatusOK, after)
}

// describeHistory is one half of the audit trail's "from what to what".
// It names the setting, not the window held, because that is what the
// operator changed -- what is on disk afterwards is a consequence, and
// is reported separately on the screen they are looking at.
func describeHistory(h HistorySettings) string {
	if !h.Enabled {
		return "off"
	}
	return fmt.Sprintf("on, %d days, %s", h.Days, config.ByteSize(h.MaxBytes))
}
