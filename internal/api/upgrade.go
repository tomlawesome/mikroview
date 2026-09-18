// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/setup"
)

// The upgrade notice (#1240, docs/upgrades.md): after a build crosses a
// version, the one thing the operator still has to do by hand is the
// router -- the wizard's pasted script changes between versions and the
// router does not update itself.
//
// What is served here is the crossing (internal/setup, persisted, so it
// outlives a restart and is the instance's rather than a browser's) and
// the fleet's standing against the current wizard (#1241's per-router
// reports). The frontend decides from those two whether to draw the
// line; this endpoint states facts and never a verdict, so the same
// answer serves the notice, a future settings row and anything else
// that asks.

// upgradeRouters is the fleet's standing against the current wizard.
//
// Behind counts every declared router that is not reporting the current
// setup -- including one that has never reported at all, which is what
// a router still running a pre-#1241 script looks like. That is the
// count the notice shows, and the right one: "never said" is not
// evidence of being up to date.
//
// Reported is carried beside it because those two cases are the same to
// an operator and not at all the same to the notice. A fleet where
// every router has reported can be trusted to clear the notice on its
// own as each one catches up, so `done` has nothing to add; a fleet
// where some router has never said anything cannot be, and the
// operator's own `done` stays the only way to settle it. Behind alone
// cannot tell those apart.
type upgradeRouters struct {
	Behind   int `json:"behind"`
	Total    int `json:"total"`
	Reported int `json:"reported"`
}

// upgradeResponse is what GET /api/upgrade serves.
//
// Previous is empty when this instance has never crossed a version -- a
// first install, and the case the notice must stay silent for. Current
// is this build either way, so a caller always learns what it is
// talking to.
type upgradeResponse struct {
	Previous  string    `json:"previous"`
	Current   string    `json:"current"`
	NoticedAt time.Time `json:"noticedAt,omitzero"`
	// Acknowledged is true once an admin has pressed `done` on this
	// crossing. A later upgrade is never acknowledged by an earlier
	// admin's click: the record is replaced when the pair of versions
	// changes (setup.Store.NoteUpgrade).
	Acknowledged bool           `json:"acknowledged"`
	Routers      upgradeRouters `json:"routers"`
}

// handleUpgrade serves the upgrade and the fleet's standing.
//
// Open to any signed-in caller, unlike GET /api/config/upgrade beside
// it: what it discloses is two version strings and a count of the
// operator's own declared routers, not config keys or filesystem paths,
// and the header already shows every session the current version. The
// notice itself is an admin's -- only an admin can act on it, and
// UpgradeNotice.svelte draws it for nobody else.
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.upgradeResponse())
}

// handleUpgradeAcknowledge is the notice's `done`: the admin's statement
// that the routers have been dealt with. Admin-only, matching the
// wizard's own writes, and audited like every other admin-privileged
// mutation.
func (s *Server) handleUpgradeAcknowledge(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	if s.Setup == nil {
		http.Error(w, "the setup ledger is not available", http.StatusServiceUnavailable)
		return
	}
	u, ok := s.Setup.AcknowledgeUpgrade(auditActor(r), time.Now())
	if !ok {
		// Nothing to acknowledge: a first install, or a click that
		// raced a restart onto a version with no crossing behind it. A
		// genuine conflict, not a caller mistake, so 409 rather than a
		// 4xx that would suggest the request itself was malformed --
		// but still an error response, not the 200 upgradeResponse
		// shape a GET returns: the frontend treats any non-2xx here
		// alike (upgrade.svelte.ts's acknowledge), and no audit entry
		// is written for a click against nothing.
		http.Error(w, "there is no upgrade to acknowledge", http.StatusConflict)
		return
	}
	s.Audit.Record(auditActor(r), "upgrade.acknowledged", u.Current, "upgraded from "+u.Previous)
	writeJSON(w, http.StatusOK, s.upgradeResponse())
}

func (s *Server) upgradeResponse() upgradeResponse {
	out := upgradeResponse{Current: s.Version}
	if s.Setup == nil {
		return out
	}
	u, ok := s.Setup.Upgrade()
	if !ok {
		return out
	}
	return upgradeResponse{
		Previous:     u.Previous,
		Current:      u.Current,
		NoticedAt:    u.NoticedAt,
		Acknowledged: u.Acknowledged(),
		Routers:      s.upgradeRouterStanding(),
	}
}

// upgradeRouterStanding counts the declared fleet against what the
// current wizard would leave on a router for this instance -- the same
// comparison GET /api/devices makes per device (#1241), asked of the
// whole list at once so the notice does not have to fetch every router
// to count them.
func (s *Server) upgradeRouterStanding() upgradeRouters {
	if s.Setup == nil || s.Devices == nil {
		return upgradeRouters{}
	}
	want := routeros.WizardLogging(s.Setup.Address(), s.SetupInstance.SyslogPort, defaultDialect())
	var out upgradeRouters
	for _, info := range s.Devices.List() {
		out.Total++
		switch s.Setup.RouterSetup(info.ID, want).Standing {
		case setup.StandingCurrent:
			out.Reported++
		case setup.StandingBehind:
			out.Reported++
			out.Behind++
		default:
			out.Behind++
		}
	}
	return out
}
