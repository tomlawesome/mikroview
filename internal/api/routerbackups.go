// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
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
	// Comment/ProtectedAt/ProtectedBy are carried only by an entry in
	// a router's `protected` array (#1126) -- the admin's note saying
	// why this one is kept, when they said so and who they were. Absent
	// from every entry in `generations`, which is the cycling set.
	Comment     string    `json:"comment,omitempty"`
	ProtectedAt time.Time `json:"protectedAt,omitzero"`
	ProtectedBy string    `json:"protectedBy,omitempty"`
}

// routerBackupRouter is one router's block (round 44's per-router
// strip). Missed/MissedIntervalSeconds/LastArrival together carry the
// owner's 2026-09-05 decision: the interval is learned from arrivals,
// not the scheduler line, and a router with one push has neither.
type routerBackupRouter struct {
	Device      string                   `json:"device"`
	Generations []routerBackupGeneration `json:"generations"` // oldest first
	// Protected is this router's kept pool (#1126), oldest first like
	// Generations: generations an admin marked with a comment saying
	// why. They are not in Generations, do not count towards the ten,
	// and nothing but an admin releasing one removes them.
	Protected       []routerBackupGeneration `json:"protected"`
	IntervalKnown   bool                     `json:"intervalKnown"`
	IntervalSeconds float64                  `json:"intervalSeconds,omitempty"`
	LastArrival     time.Time                `json:"lastArrival,omitzero"`
	Missed          int                      `json:"missed"`
}

type routerBackupsResponse struct {
	// Enabled reports whether a retention key is open and usable -- #394's
	// "no key, no backups": with this false the drop box refuses every
	// login and Routers is always empty. False covers two different
	// situations -- see KeyUnreadable, which says which one.
	Enabled bool `json:"enabled"`
	// KeyUnreadable is #1264 finding 5: true when history.keyFile names a
	// file that exists but could not be read (unreadable, truncated,
	// wrong), as opposed to Enabled being false because no key was
	// configured at all. The frontend must never render the two the same
	// way -- a broken key told to "mint a new one" strands every backup
	// already encrypted under the old one, since minting overwrites the
	// file rather than repairing it.
	KeyUnreadable    bool                 `json:"keyUnreadable"`
	Routers          []routerBackupRouter `json:"routers"`
	TotalGenerations int                  `json:"totalGenerations"`
	TotalRouters     int                  `json:"totalRouters"`
	TotalBytes       int64                `json:"totalBytes"`
	// LowSpace reports that the vault's filesystem has dropped below
	// its free-space floor (#1125), so each new arrival now replaces
	// the oldest ordinary generation instead of adding one. Nothing is
	// refused while this is true -- it is a warning that older
	// generations are being cycled out sooner than usual.
	LowSpace bool `json:"lowSpace"`
	// Port is the SFTP drop box's own listening port (round 44's "arrive
	// by" row), empty when backup.enabled is false -- the same
	// SetupInstance.BackupPort the wizard's step 6 already reads, not a
	// second copy of the configured value.
	// Lock is the optional admin passphrase's state (#956), always
	// present so the group can render "locked" without a second call.
	Lock vaultLockStatusResponse `json:"lock"`
	Port string                  `json:"port,omitempty"`
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
	out.Comment = g.Comment
	out.ProtectedAt = g.ProtectedAt
	out.ProtectedBy = g.ProtectedBy
	return out
}

// routerBackupRow assembles one router's block -- both lists and the
// missed-push arithmetic -- so the list and the three keep controls
// all answer with the same shape, and a control's caller never has to
// re-read the whole list to find out what it just changed.
func (s *Server) routerBackupRow(device string, now time.Time) routerBackupRouter {
	row := routerBackupRouter{
		Device:      device,
		Generations: toRouterBackupGenerations(s.Vault.Generations(device)),
		Protected:   toRouterBackupGenerations(s.Vault.ProtectedGenerations(device)),
	}
	missed := s.Vault.Missed(device, now)
	row.IntervalKnown = missed.IntervalKnown
	row.Missed = missed.Count
	if missed.IntervalKnown {
		row.IntervalSeconds = missed.Interval.Seconds()
	}
	if !missed.LastArrival.IsZero() {
		row.LastArrival = missed.LastArrival
	}
	return row
}

func toRouterBackupGenerations(gens []backupvault.Generation) []routerBackupGeneration {
	out := make([]routerBackupGeneration, 0, len(gens))
	for _, g := range gens {
		out = append(out, toRouterBackupGeneration(g))
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

	resp := routerBackupsResponse{
		Enabled:       s.Vault.Enabled(),
		KeyUnreadable: s.SetupInstance.BackupKeyUnreadable,
		Routers:       []routerBackupRouter{},
		Port:          s.SetupInstance.BackupPort,
		LowSpace:      s.Vault.LowSpace(),
	}
	resp.Lock = s.vaultLockStatus(r, time.Now())
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
		resp.Routers = append(resp.Routers, s.routerBackupRow(device, now))
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

	// The passphrase gate sits ahead of the read (#956): with a
	// passphrase set, only the session that unlocked may download, and
	// an unlock that has gone idle or lost its session is dropped here
	// rather than merely refused. Shared with routerbackuptext.go's
	// readBackupText (#1262) -- see requireVaultUnlocked's own doc
	// comment for why this used to be two hand-copied blocks.
	if !s.requireVaultUnlocked(w, r, "download") {
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

// The keep controls (#1126, owner ruling 2026-09-11). An admin marks a
// stored backup as one to hold on to, with a comment saying why, and it
// moves into a pool of its own that ordinary retention and low-space
// cycling both leave alone. Admin-only on the same terms as the
// passphrase controls beside them in routerbackupslock.go: this is
// admin-only because the whole group is, and no unlock is needed --
// keeping a backup is a decision about the index, not a read of the
// file, and an admin who cannot open the vault can still say which copy
// must not go.
//
// Each returns the router's whole block, both lists included, so the
// screen renders what the vault now holds rather than what the caller
// assumed its call would do.
type routerBackupKeepRequest struct {
	Comment string `json:"comment"`
}

// handleRouterBackupProtect moves one generation into the kept pool.
func (s *Server) handleRouterBackupProtect(w http.ResponseWriter, r *http.Request) {
	device, generation, ok := s.keepRequest(w, r)
	if !ok {
		return
	}
	var req routerBackupKeepRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now()
	if err := s.Vault.Protect(device, generation, req.Comment, auditActor(r), now); err != nil {
		writeRouterBackupKeepError(w, err)
		return
	}
	// The generation, never the comment: the audit log is read by
	// whoever can read this server's logs, and the comment is an
	// operator's note about their own network, sealed in the vault
	// index with everything else about the backup it describes.
	s.Audit.Record(auditActor(r), "router_backup.protected", device, fmt.Sprintf("generation=%s", generation))
	writeJSON(w, http.StatusOK, s.routerBackupRow(device, now))
}

// handleRouterBackupUnprotect releases one, back into the ten, where
// the oldest may then go.
func (s *Server) handleRouterBackupUnprotect(w http.ResponseWriter, r *http.Request) {
	device, generation, ok := s.keepRequest(w, r)
	if !ok {
		return
	}
	if err := s.Vault.Unprotect(device, generation); err != nil {
		writeRouterBackupKeepError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "router_backup.unprotected", device, fmt.Sprintf("generation=%s", generation))
	writeJSON(w, http.StatusOK, s.routerBackupRow(device, time.Now()))
}

// handleRouterBackupComment rewrites a kept generation's comment.
func (s *Server) handleRouterBackupComment(w http.ResponseWriter, r *http.Request) {
	device, generation, ok := s.keepRequest(w, r)
	if !ok {
		return
	}
	var req routerBackupKeepRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.Vault.SetComment(device, generation, req.Comment); err != nil {
		writeRouterBackupKeepError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "router_backup.comment_changed", device, fmt.Sprintf("generation=%s", generation))
	writeJSON(w, http.StatusOK, s.routerBackupRow(device, time.Now()))
}

// keepRequest is the gate and the path values the three share.
func (s *Server) keepRequest(w http.ResponseWriter, r *http.Request) (device, generation string, ok bool) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", "", false
	}
	device = r.PathValue("device")
	generation = r.PathValue("generation")
	if device == "" || generation == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return "", "", false
	}
	return device, generation, true
}

// writeRouterBackupKeepError maps the vault's refusals onto status
// codes, in the shape writeVaultLockError already uses.
func writeRouterBackupKeepError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backupvault.ErrBadComment):
		http.Error(w, fmt.Sprintf("say why you are keeping it -- 1 to %d characters, on one line", backupvault.MaxCommentRunes), http.StatusBadRequest)
	case errors.Is(err, backupvault.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, backupvault.ErrAlreadyProtected):
		http.Error(w, "that backup is already kept", http.StatusConflict)
	case errors.Is(err, backupvault.ErrDisabled):
		http.Error(w, "the router-backup vault is not enabled", http.StatusConflict)
	default:
		http.Error(w, "the vault refused that", http.StatusInternalServerError)
	}
}
