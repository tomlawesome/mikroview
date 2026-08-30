// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
)

// handleFlagsList serves every known flag, active and cleared -- the
// frontend decides how much cleared history to keep showing (see
// docs/configuration.md) -- plus the last hour of newly-raised-episode
// counts by Type at 1-minute resolution (flags.Store.TimeSeries), for
// FlagsChart. Same shape convention as GET /api/stats's timeSeries
// field (internal/store/ring.go's Stats.TimeSeries) -- added alongside
// the existing flags array rather than as a new endpoint.
func (s *Server) handleFlagsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"flags":      s.Flags.List(),
		"timeSeries": s.Flags.TimeSeries(),
	})
}

// handleFlagsClear marks one flag as cleared. Clearing an unknown or
// already-cleared ID is not an error (see flags.Store.Clear's doc
// comment) -- it just reports which case applied, so the frontend can
// still refresh its view either way.
//
// User-tier, not viewer (#653): a viewer may watch what mikroview is
// seeing but must not be able to change what it is showing, and clearing
// a flag -- reversible as it is -- does that. Was open to any signed-in
// caller before viewer existed to exclude.
func (s *Server) handleFlagsClear(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	cleared := s.Flags.Clear(id, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"cleared": cleared})
}

// verdictRequest is POST /api/flags/{id}/verdict's body: one of the
// three bare verdict labels the owner ratified on #638 (2026-08-30) --
// "expected", "noise" or "real", no explanatory second line.
type verdictRequest struct {
	Verdict flags.Verdict `json:"verdict"`
}

// handleFlagsVerdict records an operator's judgement of one flag (#638).
// Same access tier as handleFlagsClear above -- not admin-gated, unlike
// handleFlagsClearPermanent -- because expected/noise are exactly as
// reversible as a plain clear (see flags.Store.SetVerdict, which routes
// them through the same clearLocked path Clear uses), and a real
// verdict is reversible too: it neither clears anything nor creates a
// permanent exclusion, so a mistaken "real" costs nothing an admin has
// to undo.
//
// User-tier, not viewer (#653), same reasoning as handleFlagsClear: a
// judgement is a change to what mikroview is showing, even though it's a
// reversible one, so a viewer may not make it.
//
// 400 for a body that doesn't parse or names anything other than the
// three recognised verdicts (flags.Verdict.Valid()), checked before the
// flag lookup so a malformed request never depends on the ID also being
// real. 404 for an id SetVerdict doesn't recognise. 200 with the
// updated flag otherwise -- verdict, verdictBy and verdictAt now set on
// it (verdictBy from auditActor(r), the same actor-resolution every
// other handler in this package uses).
func (s *Server) handleFlagsVerdict(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	var req verdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Verdict.Valid() {
		http.Error(w, fmt.Sprintf("unrecognised verdict %q", req.Verdict), http.StatusBadRequest)
		return
	}

	id := r.PathValue("id")
	f, ok := s.Flags.SetVerdict(id, req.Verdict, auditActor(r), time.Now())
	if !ok {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleFlagsVerdictUndo is the verdict row's undo affordance, and a
// real server call rather than the deferred-timer trick undo used to
// be -- see the #638 comment this replaces: a PWA service worker
// re-issues every fetch through itself, which strips the keepalive
// guarantee a deferred POST relied on to survive a reload, and
// live-check proved it (0 of 6 judged-then-reloaded verdicts reached
// the server). The verdict now POSTs immediately, so undo has to
// reverse a real write instead of just cancelling a timer.
//
// Registered at DELETE /api/flags/verdict/{id} -- deliberately NOT
// /api/flags/{id}/verdict, which would mirror handleFlagsVerdict's own
// POST path but is structurally ambiguous against the existing DELETE
// /api/flags/exclusions/{id} route under Go's net/http.ServeMux; see
// the registration table in server.go for the full reasoning.
//
// Same access tier as handleFlagsVerdict/handleFlagsClear -- undoing is
// no more dangerous than judging in the first place, and user-tier for
// the same #653 reason: it changes what mikroview is showing, so a
// viewer may not do it. 404 for an unknown id, same as the POST. 200
// with the updated flag otherwise; see flags.Store.UndoVerdict's doc
// comment for the one subtlety -- undoing must not re-open a flag that
// was already cleared before it was judged.
func (s *Server) handleFlagsVerdictUndo(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	f, ok := s.Flags.UndoVerdict(id)
	if !ok {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleFlagsClearAll clears every currently-active flag in one request
// (issue #198's "Clear all", with the frontend's click-again confirm as
// the safeguard against an accidental single click). Same access level
// as the per-flag handleFlagsClear -- authzMatrix's own comment already
// explains why that one is user-tier rather than admin-only: a plain
// clear is reversible, unlike the permanent variant below. User-tier
// rather than viewer for the same #653 reason as the per-flag handlers:
// this is a bulk version of the same change to what mikroview is
// showing.
//
// One audit entry for the whole call, not one per flag: "cleared N
// flags" is the meaningful record here, and N individual entries would
// bury the one action that actually happened under noise for anyone
// reading the log afterward. No exclusions are created -- see
// flags.Store.ClearAll's own doc comment for why a bulk permanent
// variant does not exist and is not planned.
func (s *Server) handleFlagsClearAll(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	n := s.Flags.ClearAll(time.Now())
	if n > 0 {
		s.Audit.Record(auditActor(r), "flag.clear_all", "", fmt.Sprintf("cleared %d flags", n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": n})
}

// handleFlagsClearPermanent is handleFlagsClear plus a permanent
// exclusion of the flag's (Type, Target) in the same step -- the
// "Clear and never flag this again" action (see flags.Store.
// ClearAndExclude).
//
// Admin-only, and audit-logged. This deliberately does NOT match
// handleFlagsClear's open-to-any-caller model, despite the two being
// adjacent buttons on the same UI row, because they differ in the one
// way that matters here: a plain clear is reversible (the flag can
// simply raise again on the next matching event), while an exclusion
// permanently suppresses detection for that (Type, Target) until an
// admin notices and undoes it. Leaving it open let any authenticated
// non-admin -- or a single compromised low-privilege account -- blind
// this deployment's detection for a target of their choosing, with no
// record of who did it. For a tool whose entire purpose is surfacing
// suspicious activity, silently losing that coverage is the more
// expensive failure than a bit of permission-model asymmetry.
//
// The exclusion's reviewability (handleExclusionsList) and its undo
// (handleExclusionRemove) were already admin-gated; this brings the
// action that creates one into line with them.
func (s *Server) handleFlagsClearPermanent(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	ok := s.Flags.ClearAndExclude(id, time.Now())
	if ok {
		s.Audit.Record(auditActor(r), "flag.clear_permanent", id, "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": ok, "excluded": ok})
}

// handleExclusionsList serves every permanently-excluded (Type, Target)
// pair -- admin-only (see callerIsAdmin), since this is the
// "undo a mistake" surface for handleFlagsClearPermanent.
func (s *Server) handleExclusionsList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exclusions": s.Flags.ListExclusions()})
}

// handleExclusionRemove reverses one exclusion, letting its (Type,
// Target) raise again going forward -- admin-only, same gate as
// handleExclusionsList. Removing an unknown exclusion ID is not an
// error, same "no-op, not an error" reasoning as handleFlagsClear --
// only logged to the audit trail when an exclusion was actually found
// and removed, since a no-op on an unknown ID isn't a meaningful action.
func (s *Server) handleExclusionRemove(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	removed := s.Flags.RemoveExclusionByID(id)
	if removed {
		s.Audit.Record(auditActor(r), "flag.exclusion_remove", id, "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}
