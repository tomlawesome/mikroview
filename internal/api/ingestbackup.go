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

// backupBusyMessage is what a push refused for capacity is told, in
// place of the refusal's own text: mikroview's occupancy -- how many
// devices have a transfer in flight, how much of the shared byte budget
// is spoken for, whether this token has spent its window -- is not
// something a device-scoped token learns from a reply (#1122). The
// router does the same thing whatever the reason: its next scheduled run
// starts the transfer over.
const backupBusyMessage = "the router-backup channel is busy; try again later"

// backupSinkFailedMessage is what a push is told when mikroview itself
// could not keep its side of the bargain. The sink's own error names
// mikroview's filesystem -- "no space left on device", with the vault's
// absolute path -- and that goes to the server log, not to a router
// (#1122).
const backupSinkFailedMessage = "mikroview could not store this backup; try again later"

// auditActorServer is the actor for an entry recording mikroview's own
// failure rather than something a caller did. Device pushes are audited
// as "device:<name>"; a full disk is not the router's act, and auditing
// it against the device would read in the trail as that router
// misbehaving. The device stays the entry's target -- whose backup was
// lost is still the useful fact.
const auditActorServer = "system"

// errIngestBudgetSpent is the ingest limiter refusing to start a new
// transfer. A sentinel of its own so the refusal takes the same path as
// the receiver's own capacity refusals -- one fixed 429 to the router,
// the real reason in the audit trail, which is what #1123 had no way of
// telling an operator.
var errIngestBudgetSpent = errors.New("the device's ingest allowance for this window is spent")

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

	// A transfer whose router stopped mid-loop holds its buffer until
	// something clears it. The receiver sweeps on its own ticker
	// (backupslice.Receiver.RunPeriodicSweep, started in main.go);
	// sweeping here as well costs a map scan over at most a handful of
	// entries and reclaims the buffer at the next push rather than at
	// the next tick.
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
		// The ingest limiter is charged once per transfer, here, rather
		// than once per request (#1123). A slice used to spend a token
		// from the same 120-per-15-minutes allowance as the device's
		// RouterOS pushes, so anything needing more than 120 slices --
		// about 3.8MB, an ordinary backup plus its export -- was refused
		// around slice 118 and could never land, every night, with
		// nothing but a refusal in the audit log to show for it.
		//
		// What bounds the slices that follow is not the limiter: one
		// transfer per device at a time, 32KiB a slice, a slice count
		// fixed at begin, and a declared total the receiver refuses to
		// let the accumulated bytes pass. A token cannot buy more work
		// here than the one transfer it has just paid for.
		if !s.IngestLimiter.Reserve(tok.ID, now) {
			s.refuseBackupSlice(w, tok.Device, errIngestBudgetSpent, now)
			return
		}
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
		res, err := s.BackupSlices.Slice(tok.Device, req.TransferID, req.Index, req.Data, now)
		switch {
		case errors.Is(err, backupslice.ErrSink):
			// mikroview's fault, not the device's: a different status, a
			// different audit entry, and nothing about this server's
			// disks in the reply.
			s.failBackupSlice(w, tok.Device, err, now)
			return
		case err != nil:
			s.refuseBackupSlice(w, tok.Device, err, now)
			return
		}
		if res.Done {
			// One audit entry per completed backup, matching what the
			// SFTP drop box records: a configuration arriving is worth
			// the same line however it travelled. Kind and size come
			// from the receiver, which counted the bytes it actually
			// reassembled -- the push script sends neither on a slice,
			// so building this from the request recorded every backup as
			// "kind= bytes=0" (#1122).
			//
			// Through the same gate as the refusal below, and with the
			// same key, so the trail keeps the shape noteIngest
			// documents: a device that starts being refused, or
			// recovers, is on the record either way.
			if s.noteIngest(tok.Device, "router-backup", true, now) {
				s.Audit.Record("device:"+tok.Device, "ingest.router_backup", tok.Device,
					fmt.Sprintf("kind=%s bytes=%d over the ingest channel", res.Kind, res.Bytes))
			}
		}
		writeJSON(w, http.StatusOK, backupSliceResponse{Accepted: true, Done: res.Done})
	default:
		http.Error(w, `op must be "begin" or "slice"`, http.StatusBadRequest)
	}
}

// refuseBackupSlice turns a receiver refusal into a status code and a
// rate-limited audit entry.
//
// The audit entry carries the refusal's own text, which an admin reading
// the trail needs; the reply does not always, because two of these
// refusals describe mikroview's occupancy rather than the request (see
// backupBusyMessage). Everything else here describes the caller's own
// push -- a slice out of order, a file over the cap -- in terms of the
// JSON fields it sent, which is the same judgement handleIngestRouterOS
// makes about echoing its decode errors.
func (s *Server) refuseBackupSlice(w http.ResponseWriter, device string, err error, now time.Time) {
	if s.noteIngest(device, "router-backup", false, now) {
		s.Audit.Record("device:"+device, "ingest.router_backup.refused", device, err.Error())
	}
	switch {
	case errors.Is(err, backupslice.ErrBusy), errors.Is(err, errIngestBudgetSpent):
		// Capacity, not a fault in the request: the router's next
		// scheduled run starts over, which is what it does after any
		// refusal anyway.
		http.Error(w, backupBusyMessage, http.StatusTooManyRequests)
	case errors.Is(err, backupslice.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// failBackupSlice answers a push mikroview could not complete itself.
// The device did nothing wrong -- it delivered a whole, verified file --
// so this is a 503 rather than a 400, and it is recorded against the
// server rather than against the router.
func (s *Server) failBackupSlice(w http.ResponseWriter, device string, err error, now time.Time) {
	apiLog.Error(fmt.Sprintf("storing a router backup from %s failed: %v", device, err))
	// Gated on a key of its own: a vault that is failing should leave a
	// trace without flooding the log the way an ungated entry per push
	// would, and sharing the refusal key would let a server fault mask
	// the device's own next refusal (see noteIngest).
	if s.noteIngest(device, "router-backup-sink", false, now) {
		s.Audit.Record(auditActorServer, "ingest.router_backup.failed", device,
			"the vault could not store a backup that arrived over the ingest channel; see the server log")
	}
	http.Error(w, backupSinkFailedMessage, http.StatusServiceUnavailable)
}
