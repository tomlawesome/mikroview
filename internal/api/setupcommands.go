// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

// setupCommandsRequest is what the wizard sends to render RouterOS
// commands (#436): only Address is required, since every other field is
// either optional in the commands it feeds (SyslogPort falls back to the
// running configuration, Token/Kinds together gate whether a push script
// is worth rendering at all) or is itself optional input (Version, the
// operator's pick from the dialect table's rows).
type setupCommandsRequest struct {
	Address    string   `json:"address"`
	SyslogPort string   `json:"syslogPort"`
	Token      string   `json:"token"`
	Kinds      []string `json:"kinds"`
	Version    string   `json:"version"`
}

// routerosTable is the dialect table itself, so the wizard can quote its
// bounds ("commands were written for 7.18 and later ... last checked
// against 7.24.1") without hard-coding them a second time.
type routerosTable struct {
	Minimum string         `json:"minimum"`
	Newest  string         `json:"newest"`
	Rows    []routeros.Row `json:"rows"`
}

// pickedVersion is what the operator's Version selection (or its
// absence) resolved to: the standing that version holds, and the dialect
// used to render every step below.
type pickedVersion struct {
	Version  string `json:"version"`
	Standing string `json:"standing"`
	Dialect  string `json:"dialect"`
}

// setupCommandsRouter is one router whose version mikroview actually
// knows -- from a push, or #436 step 3's /ca.crt?ros= hint -- with where
// it stands and any per-row note that concerns it.
type setupCommandsRouter struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	RouterOSVersion string `json:"routerosVersion"`
	Standing        string `json:"standing"`
	Note            string `json:"note"`
}

// commandStep is one rendered block: the commands themselves, and any
// note that belongs beside this specific step (currently only
// ruleTagging ever carries one -- a row's Note, when the selected
// version's row has one).
type commandStep struct {
	Commands string `json:"commands"`
	Note     string `json:"note"`
}

type setupCommandsSteps struct {
	CaTrust     commandStep `json:"caTrust"`
	Syslog      commandStep `json:"syslog"`
	RuleTagging commandStep `json:"ruleTagging"`
	Push        commandStep `json:"push"`
	Schedule    commandStep `json:"schedule"`
}

type setupCommandsResponse struct {
	RouterOS routerosTable         `json:"routeros"`
	Picked   *pickedVersion        `json:"picked"`
	Routers  []setupCommandsRouter `json:"routers"`
	Steps    setupCommandsSteps    `json:"steps"`
}

// handleSetupCommands renders the RouterOS commands the setup wizard
// shows an operator (#436): the dialect table itself so the wizard can
// quote its bounds, what an operator-picked version resolves to, every
// router whose version mikroview actually knows and where it stands, and
// the five command blocks rendered for the selected dialect.
//
// Same access gate as GET /api/setup/status beside it: open to any
// signed-in user, not admin-gated (see that handler's doc comment for
// why -- this only re-renders what a viewer can already read off the
// setup-status and devices endpoints as copy-paste commands).
func (s *Server) handleSetupCommands(w http.ResponseWriter, r *http.Request) {
	var req setupCommandsRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Address == "" {
		http.Error(w, "address is required", http.StatusBadRequest)
		return
	}

	// Dialect selection (#436): the picked version's own row when it has
	// one; otherwise, since mikroview holds exactly one dialect today,
	// that one. This is the seam a second dialect would use -- nothing
	// else here needs to change the day one appears.
	dialect := defaultDialect()
	var picked *pickedVersion
	if req.Version != "" {
		d := dialect
		if row, ok := routeros.RowFor(req.Version); ok {
			d = row.Dialect
		}
		picked = &pickedVersion{
			Version:  req.Version,
			Standing: routeros.VersionStanding(req.Version).String(),
			Dialect:  d,
		}
		dialect = d
	}

	routers := make([]setupCommandsRouter, 0)
	if s.Devices != nil {
		for _, info := range s.Devices.List() {
			version, ok := s.effectiveRouterOSVersion(info)
			if !ok {
				continue
			}
			note := ""
			if row, ok := routeros.RowFor(version); ok {
				note = row.Note
			}
			routers = append(routers, setupCommandsRouter{
				ID:              info.ID,
				Name:            info.Name,
				RouterOSVersion: version,
				Standing:        routeros.VersionStanding(version).String(),
				Note:            note,
			})
		}
	}

	// syslogPort falls back to the instance's own running configuration
	// when the wizard didn't send one -- the same value
	// GET /api/setup/status's Instance.SyslogPort already reports.
	syslogPort := req.SyslogPort
	if syslogPort == "" {
		syslogPort = s.SetupInstance.SyslogPort
	}

	// The rule-tagging note is the picked version's own row note, when
	// there is a picked version and its row has one -- e.g. 7.24.0's
	// find-lookup warning. No picked version means nothing to quote here;
	// a per-router note about the same thing is carried on that router's
	// own entry in Routers above.
	ruleTaggingNote := ""
	if picked != nil {
		if row, ok := routeros.RowFor(picked.Version); ok {
			ruleTaggingNote = row.Note
		}
	}

	// A push script only means something with both a token and at least
	// one kind to push; either missing leaves it blank rather than
	// rendering an empty or half-formed script.
	pushCommands := ""
	if req.Token != "" && len(req.Kinds) > 0 {
		pushCommands = routeros.PushScript(req.Address, req.Token, req.Kinds, dialect)
	}

	writeJSON(w, http.StatusOK, setupCommandsResponse{
		RouterOS: routerosTable{
			Minimum: routeros.MinimumVersion,
			Newest:  routeros.NewestVersion(),
			Rows:    append([]routeros.Row(nil), routeros.Rows...),
		},
		Picked:  picked,
		Routers: routers,
		Steps: setupCommandsSteps{
			CaTrust:     commandStep{Commands: routeros.CaTrustCommands(req.Address, dialect)},
			Syslog:      commandStep{Commands: routeros.SyslogCommands(req.Address, syslogPort, dialect)},
			RuleTagging: commandStep{Commands: routeros.RuleTaggingCommands(dialect), Note: ruleTaggingNote},
			Push:        commandStep{Commands: pushCommands},
			Schedule:    commandStep{Commands: routeros.ScheduleCommands(dialect)},
		},
	})
}

// defaultDialect is the dialect used when nothing else picks one -- see
// handleSetupCommands' dialect-selection comment. Reads Rows[0] rather
// than a hard-coded "a" so a second dialect landing in dialects.go
// cannot silently disagree with what this falls back to.
func defaultDialect() string {
	if len(routeros.Rows) == 0 {
		return ""
	}
	return routeros.Rows[0].Dialect
}
