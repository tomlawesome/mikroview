// SPDX-License-Identifier: AGPL-3.0-only

package api

// The HTTPS route a router backup can take when SFTP cannot be opened
// (#955): the router reads its own backup in <=32KiB slices and POSTs
// them here, and internal/backupslice puts the file back together.
//
// This file is the boundary, not the algorithm. It answers who is
// asking -- the ingest token names exactly one device, and a transfer
// belongs to the device that began it -- and it holds the same body cap
// and rate limit every other ingest push is held to. Reassembly, the
// caps on size and order, and the header check all live in
// internal/backupslice, where they can be tested without an HTTP server
// in the way.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupslice"
)

// backupSliceRequest is one POST from the router's push loop. `op` says
// which of the two shapes it is; the fields for the other are ignored,
// which keeps the router script to a single request builder.
type backupSliceRequest struct {
	Op string `json:"op"`

	// begin
	Kind        string `json:"kind"`
	TotalBytes  int64  `json:"totalBytes"`
	TotalSlices int    `json:"totalSlices"`
	SHA256      string `json:"sha256"`

	// slice. Data is base64 in the JSON: encoding/json decodes a string
	// into []byte that way, so the wire format is the one RouterOS can
	// actually produce without a second encoder here.
	TransferID string `json:"transferId"`
	Index      int    `json:"index"`
	Data       []byte `json:"data"`
}

type backupSliceResponse struct {
	TransferID string `json:"transferId,omitempty"`
	Accepted   bool   `json:"accepted"`
	Done       bool   `json:"done"`
}

// handleIngestRouterBackup receives one slice, or the declaration that
// opens a transfer.
func (s *Server) handleIngestRouterBackup(w http.ResponseWriter, r *http.Request) {
	tok := ingestTokenFromContext(r)
	if tok == nil {
		// Same unreachable-but-guarded case as handleIngestRouterOS: the
		// ingest mux is only dispatched to with a token in context.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.BackupSlices == nil || !s.Vault.Enabled() {
		// #394's "no key, no backups": the drop box is closed, and
		// saying so is better than accepting 460KB and discarding it.
		http.Error(w, "the router-backup vault is not enabled", http.StatusServiceUnavailable)
		return
	}

	now := time.Now()
	if !s.IngestLimiter.Reserve(tok.ID, now) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// A transfer whose router stopped mid-loop holds its buffer until
	// something clears it. Sweeping here rather than from a ticker means
	// the next push -- from any router -- pays a map scan over at most a
	// handful of entries and reclaims it, with no goroutine to own.
	s.BackupSlices.Sweep(now)

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req backupSliceRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "the request body is not a router-backup slice", http.StatusBadRequest)
		return
	}

	switch req.Op {
	case "begin":
		id, err := s.BackupSlices.Begin(tok.Device, backupslice.Begin{
			Kind:        req.Kind,
			TotalBytes:  req.TotalBytes,
			TotalSlices: req.TotalSlices,
			SHA256:      req.SHA256,
		}, now)
		if err != nil {
			s.refuseBackupSlice(w, tok.Device, err, now)
			return
		}
		writeJSON(w, http.StatusOK, backupSliceResponse{TransferID: id, Accepted: true})
	case "slice":
		done, err := s.BackupSlices.Slice(tok.Device, req.TransferID, req.Index, req.Data, now)
		if err != nil {
			s.refuseBackupSlice(w, tok.Device, err, now)
			return
		}
		if done {
			// One audit entry per completed backup, matching what the
			// SFTP drop box records: a configuration arriving is worth
			// the same line however it travelled.
			s.Audit.Record("device:"+tok.Device, "router_backup.received", tok.Device,
				fmt.Sprintf("kind=%s bytes=%d over the ingest channel", req.Kind, req.TotalBytes))
		}
		writeJSON(w, http.StatusOK, backupSliceResponse{Accepted: true, Done: done})
	default:
		http.Error(w, `op must be "begin" or "slice"`, http.StatusBadRequest)
	}
}

// refuseBackupSlice turns a receiver refusal into a status code and a
// rate-limited audit entry.
//
// The message is the refusal's own text, which describes the caller's
// request -- a slice out of order, a file over the cap -- and never
// anything about mikroview's state or another device's transfers. That
// is the same judgement handleIngestRouterOS makes about echoing its
// decode errors.
func (s *Server) refuseBackupSlice(w http.ResponseWriter, device string, err error, now time.Time) {
	if s.noteIngest(device, "router-backup", false, now) {
		s.Audit.Record("device:"+device, "ingest.router_backup.refused", device, err.Error())
	}
	switch {
	case errors.Is(err, backupslice.ErrBusy):
		// Capacity, not a fault in the request: the router's next
		// scheduled run starts over, which is what it does after any
		// refusal anyway.
		http.Error(w, err.Error(), http.StatusTooManyRequests)
	case errors.Is(err, backupslice.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
