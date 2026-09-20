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
	// Version is this build's own version string.
	Version  string                 `json:"version"`
	Settings []configUpgradeSetting `json:"settings"`
}

// handleConfigUpgrade serves the setup wizard's paste-block treatment
// (#1218), applied to whatever this build understands that the running
// config does not set.
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

func (s *Server) configUpgradeResponse() configUpgradeResponse {
	settings := make([]configUpgradeSetting, 0, len(s.ConfigUpgradeSettings))
	for _, m := range s.ConfigUpgradeSettings {
		settings = append(settings, configUpgradeSetting{Key: m.Key, Block: m.Block})
	}
	return configUpgradeResponse{Version: s.Version, Settings: settings}
}
