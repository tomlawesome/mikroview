// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/settings"
)

// RouterBackupSwitch is the router-backup SFTP drop box's on/off control
// (#1361): whether it is listening right now and on which port, and the
// two actions that move it. Implemented by main.backupRuntime; nil on a
// Server built without one (older tests, and any build that omits the
// feature), in which case the endpoint refuses rather than pretends --
// same nil-means-unavailable convention as HistoryControl.
type RouterBackupSwitch interface {
	// State reports whether the drop box is listening right now, and on
	// which port. Port is "" while closed.
	State() (open bool, port string)
	// Start opens it, binding the port synchronously so "already in use"
	// comes back as this call's own error rather than a log line.
	Start() error
	// Stop closes it. Always succeeds.
	Stop()
}

// RouterBackupNotifier sends the switch's one-off change email (#1361:
// "an email to notify.smtp.to when configured"). A narrow interface of
// its own rather than notify.Notifier itself: that one is typed on a
// batch of behavioural flags, and this package has no business importing
// internal/flags just to describe an email a settings change sends.
// Nil means notify.smtp is not configured -- the same "empty means off"
// convention every other notify channel already uses.
type RouterBackupNotifier interface {
	SendNotice(subject, body string) error
}

// routerBackupSwitchBannerWindow is how long the admin banner
// (routerBackupSwitchChanged, below) keeps announcing a change after it
// happens -- the owner's ruling on question 4a: the banner clears seven
// days after each change.
const routerBackupSwitchBannerWindow = 7 * 24 * time.Hour

// routerBackupSwitchRequest is PUT /api/settings/router-backups' body.
// Password is only read, and only required, when Open is true --
// opening a second listening port from a browser is exactly the action a
// stolen session should not be able to take unattended; closing is the
// safe direction and needs no password, the same reasoning
// handleRouterBackupLock's vault lock uses for itself.
type routerBackupSwitchRequest struct {
	Open     bool   `json:"open"`
	Password string `json:"password,omitempty"`
}

// routerBackupSwitchResponse is what every call below returns, so the
// frontend never has to infer the new state from which call it made --
// read back from RouterBackupSwitch.State() itself, never from what was
// merely asked for.
type routerBackupSwitchResponse struct {
	Open bool   `json:"open"`
	Port string `json:"port,omitempty"`
}

// handleRouterBackupSwitchUpdate opens or closes the router-backup SFTP
// drop box (#1361).
//
// Order on open is bind-then-store: RouterBackupSwitch.Start's net.Listen
// runs first, so a port already taken by something else comes back as
// this request's own 409 with nothing written anywhere -- no stored
// position, no audit line, no email -- rather than a change that claims
// to have happened while the listener never came up. Stop cannot fail,
// so there is no equivalent ordering question on close.
//
// Admin-only, like every other control in this settings group.
func (s *Server) handleRouterBackupSwitchUpdate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.RouterBackupSwitch == nil || s.Settings == nil {
		http.Error(w, "the router-backup drop box is not adjustable on this instance", http.StatusServiceUnavailable)
		return
	}

	var req routerBackupSwitchRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	now := time.Now()
	actor := auditActor(r)

	if req.Open {
		user := userFromContext(r)
		if user == nil {
			writeUnauthorized(w, "sign in first")
			return
		}
		key := passwordRecheckLimiterKey(user.Username)
		if !s.LoginLimiter.Reserve(key, now) {
			http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
			return
		}
		if _, err := s.Auth.Authenticate(user.Username, req.Password, now); err != nil {
			writeUnauthorized(w, "incorrect password")
			return
		}
		s.LoginLimiter.Release(key, now)

		if err := s.RouterBackupSwitch.Start(); err != nil {
			http.Error(w, fmt.Sprintf("the drop box could not be opened: %v", err), http.StatusConflict)
			return
		}
	} else {
		s.RouterBackupSwitch.Stop()
	}

	open, port := s.RouterBackupSwitch.State()
	if err := s.Settings.SetBackup(settings.Backup{Enabled: open, ChangedAt: now, ChangedBy: actor}); err != nil {
		settingsLog.Error(fmt.Sprintf("storing the router-backup switch position failed: %v -- it will read as unset again at the next restart", err))
	}

	action, word, detail := "router_backup.closed", "closed", ""
	if open {
		action, word, detail = "router_backup.opened", "opened", fmt.Sprintf("port=%s", port)
	}
	s.Audit.Record(actor, action, "switch", detail)
	settingsLog.Info(fmt.Sprintf("router backups: %s by %s", word, actor))

	if s.RouterBackupNotifier != nil {
		subject := fmt.Sprintf("MikroView: router-backup drop box %s", word)
		body := fmt.Sprintf("The router-backup drop box was %s by %s.", word, actor)
		if open {
			body += fmt.Sprintf(" It is listening on port %s.", port)
		}
		if err := s.RouterBackupNotifier.SendNotice(subject, body); err != nil {
			settingsLog.Warn(fmt.Sprintf("router backups: could not email the switch change: %v", err))
		}
	}

	writeJSON(w, http.StatusOK, routerBackupSwitchResponse{Open: open, Port: port})
}

// routerBackupSwitchChanged is the live admin banner (#1361, read by
// configProblems.svelte.ts): every open or close is announced this way
// for seven days after it happens, on top of the audit line and the
// email above -- an admin who was not the one who moved it, and does not
// go looking at the audit log, still sees it the next time they open the
// app.
func (s *Server) routerBackupSwitchChanged() (ConfigProblem, bool) {
	if s.Settings == nil {
		return ConfigProblem{}, false
	}
	pos, ok := s.Settings.Backup()
	if !ok || pos.ChangedAt.IsZero() || time.Since(pos.ChangedAt) >= routerBackupSwitchBannerWindow {
		return ConfigProblem{}, false
	}
	word := "closed"
	if pos.Enabled {
		word = "opened"
	}
	by := "an admin"
	if pos.ChangedBy != "" {
		by = pos.ChangedBy
	}
	return ConfigProblem{
		Code:     "router-backup-switch-changed",
		Key:      "router-backups",
		Severity: "warn",
		Message: fmt.Sprintf("The router-backup drop box was %s by %s on %s.",
			word, by, pos.ChangedAt.Format("2 January 2006")),
	}, true
}
