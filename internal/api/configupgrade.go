// SPDX-License-Identifier: AGPL-3.0-only

package api

import "net/http"

// configUpgradeSetting is one MissingSetting (internal/config), reshaped
// for JSON -- key names of this package's own choosing rather than
// config.MissingSetting's exported Go field names leaking straight
// through.
type configUpgradeSetting struct {
	Key string `json:"key"`
	// Block is the ready-to-paste YAML -- comment lines and all,
	// already "#"-commented -- for this one setting, verbatim from
	// deploy/config.example.yaml. See config.MissingSetting.
	Block string `json:"block"`
}

// configUpgradeResponse is what Settings ▸ Upgrade renders.
type configUpgradeResponse struct {
	// Version is this build's own version string, so the frontend can
	// send it straight back on dismiss without asking again.
	Version  string                 `json:"version"`
	Settings []configUpgradeSetting `json:"settings"`
	// Dismissed is true once an admin has dismissed the notice for
	// Version specifically -- see internal/configdrift. A later version
	// with something new to say is never dismissed by an earlier
	// dismissal, so this is never true just because *some* version was
	// dismissed once.
	Dismissed bool `json:"dismissed"`
}

// handleConfigUpgrade serves the setup wizard's paste-block treatment
// (#1218), applied to whatever this build understands that the running
// config does not set, plus whether an admin has already dismissed the
// notice for this exact version.
//
// Admin-only, matching every other setup/config write and read this
// section gates the same way (config key names and filesystem paths are
// the same infrastructure-map disclosure GET /api/config/problems is
// admin-gated for).
func (s *Server) handleConfigUpgrade(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, s.configUpgradeResponse())
}

// handleConfigUpgradeDismiss records that this version's notice has
// been dealt with, so it stops appearing -- until a later version has
// something new to say, at which point it is not dismissed again by
// this same click. Admin-only, same gate as the GET beside it.
func (s *Server) handleConfigUpgradeDismiss(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.ConfigDrift == nil {
		http.Error(w, "config-drift state is not available", http.StatusServiceUnavailable)
		return
	}
	s.ConfigDrift.Dismiss(s.Version)
	s.Audit.Record(auditActor(r), "config.upgrade_dismissed", s.Version, "")
	writeJSON(w, http.StatusOK, s.configUpgradeResponse())
}

func (s *Server) configUpgradeResponse() configUpgradeResponse {
	settings := make([]configUpgradeSetting, 0, len(s.ConfigUpgradeSettings))
	for _, m := range s.ConfigUpgradeSettings {
		settings = append(settings, configUpgradeSetting{Key: m.Key, Block: m.Block})
	}
	dismissed := false
	if s.ConfigDrift != nil {
		dismissed = s.ConfigDrift.Dismissed(s.Version)
	}
	return configUpgradeResponse{Version: s.Version, Settings: settings, Dismissed: dismissed}
}
