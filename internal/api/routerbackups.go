// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// routerBackupGeneration is one kept generation -- the shape round 44's
// strip and newest-pair line are drawn from. Sizes/header are omitted
// (via omitempty/omitzero) for whichever half of the pair has not
// arrived yet, so the frontend can tell "not here" from "zero bytes".
type routerBackupGeneration struct {
	ID              string    `json:"id"`
	BackupArrivedAt time.Time `json:"backupArrivedAt,omitzero"`
	RscArrivedAt    time.Time `json:"rscArrivedAt,omitzero"`
	BackupBytes     int64     `json:"backupBytes,omitempty"`
	RscBytes        int64     `json:"rscBytes,omitempty"`
	// Header is the .backup's header label ("plain" or "encrypted"),
	// empty until the .backup half of this generation has arrived.
	Header string `json:"header,omitempty"`
}

// routerBackupRouter is one router's block (round 44's per-router
// strip). Missed/MissedIntervalSeconds/LastArrival together carry the
// owner's 2026-09-05 decision: the interval is learned from arrivals,
// not the scheduler line, and a router with one push has neither.
type routerBackupRouter struct {
	Device          string                   `json:"device"`
	Generations     []routerBackupGeneration `json:"generations"` // oldest first
	IntervalKnown   bool                     `json:"intervalKnown"`
	IntervalSeconds float64                  `json:"intervalSeconds,omitempty"`
	LastArrival     time.Time                `json:"lastArrival,omitzero"`
	Missed          int                      `json:"missed"`
}

type routerBackupsResponse struct {
	// Enabled reports whether a retention key is configured at all --
	// #394's "no key, no backups": with this false the drop box refuses
	// every login and Routers is always empty.
	Enabled          bool                 `json:"enabled"`
	Routers          []routerBackupRouter `json:"routers"`
	TotalGenerations int                  `json:"totalGenerations"`
	TotalRouters     int                  `json:"totalRouters"`
	TotalBytes       int64                `json:"totalBytes"`
	// Port is the SFTP drop box's own listening port (round 44's "arrive
	// by" row), empty when backup.enabled is false -- the same
	// SetupInstance.BackupPort the wizard's step 6 already reads, not a
	// second copy of the configured value.
	Port string `json:"port,omitempty"`
}

func toRouterBackupGeneration(g backupvault.Generation) routerBackupGeneration {
	out := routerBackupGeneration{ID: g.ID}
	if g.HasBackup() {
		out.BackupArrivedAt = g.BackupArrivedAt
		out.BackupBytes = g.BackupSize
		out.Header = string(g.Header)
	}
	if g.HasRsc() {
		out.RscArrivedAt = g.RscArrivedAt
		out.RscBytes = g.RscSize
	}
	return out
}

// handleRouterBackupsList is Settings' "router backups" group (round
// 44) and the wizard step 6's observation line (round 45): admin-only,
// like the disk group's own state and key rows beside it -- a viewer
// never sees this group at all.
func (s *Server) handleRouterBackupsList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	resp := routerBackupsResponse{Enabled: s.Vault.Enabled(), Routers: []routerBackupRouter{}, Port: s.SetupInstance.BackupPort}
	if !s.Vault.Enabled() {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	now := time.Now()
	stats := s.Vault.Stats()
	resp.TotalGenerations = stats.Generations
	resp.TotalRouters = stats.Routers
	resp.TotalBytes = stats.Bytes

	for _, device := range s.Vault.Routers() {
		gens := s.Vault.Generations(device)
		out := make([]routerBackupGeneration, 0, len(gens))
		for _, g := range gens {
			out = append(out, toRouterBackupGeneration(g))
		}
		missed := s.Vault.Missed(device, now)
		row := routerBackupRouter{
			Device:        device,
			Generations:   out,
			IntervalKnown: missed.IntervalKnown,
			Missed:        missed.Count,
		}
		if missed.IntervalKnown {
			row.IntervalSeconds = missed.Interval.Seconds()
		}
		if !missed.LastArrival.IsZero() {
			row.LastArrival = missed.LastArrival
		}
		resp.Routers = append(resp.Routers, row)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRouterBackupDownload streams one generation's file back,
// decrypted, and writes an audit entry with the admin's name -- #394's
// requirement that a download of a router's whole configuration
// (credentials included) is never unaccountable. kind is "backup" or
// "rsc" (routerbackups' own vocabulary, not a file extension the caller
// gets to invent).
func (s *Server) handleRouterBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	device := r.PathValue("device")
	generation := r.PathValue("generation")
	kind := r.PathValue("kind")
	if kind != backupvault.KindBackup && kind != backupvault.KindRsc {
		http.Error(w, "kind must be \"backup\" or \"rsc\"", http.StatusBadRequest)
		return
	}

	data, err := s.Vault.Open(device, generation, kind)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// The audit entry names who, which router, which generation and
	// which half of the pair -- everything an operator investigating
	// "who has seen this router's credentials" would need, without
	// carrying any of the file's own content into the log.
	s.Audit.Record(auditActor(r), "router_backup.download", device,
		fmt.Sprintf("generation=%s kind=%s", generation, kind))

	ext := "backup"
	if kind == backupvault.KindRsc {
		ext = "rsc"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, device, ext))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
