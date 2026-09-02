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

// verdictRequest is POST /api/flags/{id}/verdict's body: one of the
// four bare verdict labels the owner ratified on #640 (2026-09-02) --
// "expected", "checked", "investigate" or "resolved", no explanatory
// second line.
type verdictRequest struct {
	Verdict flags.Verdict `json:"verdict"`
}

// handleFlagsVerdict records an operator's judgement of one flag (#640).
// It is now the only way a single flag leaves the inbox: the plain clear
// and the admin-only "clear and never flag this again" are both gone, and
// an expected verdict records the sized expectation the second of those
// used to (see flags.Store.SetVerdict).
//
// User-tier for all four verdicts, per #640's ratified design, and not
// viewer (#653): a judgement changes what mikroview is showing, so a
// viewer may not make one. The suppression an expected verdict records
// is bounded by the firing the operator just looked at and reversible by
// undo, which is what makes it a user-tier action where the old
// unbounded exclude-forever was admin-only.
//
// Audit-logged, carrying the verdict as its detail. That is what keeps
// the record clear-permanent's admin gate used to guarantee: an
// expectation suppresses future detection for a (detector, target) pair,
// and "who decided this stopped being flagged" must stay answerable now
// that any user can decide it.
//
// 400 for a body that doesn't parse or names anything other than the
// four recognised verdicts (flags.Verdict.Valid()), checked before the
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
	actor := auditActor(r)

	// One verdict at a time past this point: the watchlist half of an
	// expected verdict is a read-modify-write across two stores (this
	// flag's expectation, and the device's inverted entry), and two
	// verdicts on the same device interleaving would let one's promotion
	// land inside the other's withdrawal. Held for the whole handler, not
	// per store call, because it is the compound operation that has to be
	// atomic -- the same reasoning definitionsEnabledScopeMu records for
	// its own pair of calls.
	s.verdictWatchlistMu.Lock()
	defer s.verdictWatchlistMu.Unlock()

	prior, known := s.Flags.Get(id)
	if !known {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	// Changing one's mind away from expected takes back the destinations
	// that verdict permitted, exactly as undoing it would -- the
	// watchlist counterpart of the expectation withdrawal SetVerdict
	// already does. Before the verdict changes, since the record lives on
	// the expectation the change may delete.
	if prior.Verdict == flags.VerdictExpected && req.Verdict != flags.VerdictExpected {
		if !s.withdrawPermittedFor(w, id, actor) {
			return
		}
	}

	now := time.Now()
	f, ok := s.Flags.SetVerdict(id, req.Verdict, actor, now)
	if !ok {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	if req.Verdict == flags.VerdictExpected {
		rec, wrote, err := s.permitFlagEvidence(f, actor, now)
		if err != nil {
			// The two land together or not at all, the same rule #640
			// applied to the clear and the expectation that justifies it:
			// a flag cleared as expected while the destinations it
			// declared normal never reached the watchlist would be a
			// judgement half-recorded, with nothing on screen saying
			// which half.
			s.Flags.UndoVerdict(id)
			http.Error(w, "recording this flag's destinations on the watchlist failed, so the verdict was not kept: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if wrote {
			s.Flags.RecordPermitted(id, rec)
		}
	}
	s.Audit.Record(actor, "flag.verdict", id, string(req.Verdict))
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
// POST path but would be structurally ambiguous against a wildcard-
// then-literal sibling under Go's net/http.ServeMux; see the
// registration table in server.go for the full reasoning.
//
// Same access tier as handleFlagsVerdict -- undoing is no more dangerous
// than judging in the first place, and user-tier for the same #653
// reason: it changes what mikroview is showing, so a viewer may not do
// it. Audit-logged for the same reason the verdict itself is: undoing an
// expected verdict withdraws its expectation (flags.Store.UndoVerdict),
// which is as much a change to what mikroview will flag as making it
// was. 404 for an unknown id, same as the POST. 200 with the updated
// flag otherwise; see flags.Store.UndoVerdict's doc comment for the one
// subtlety -- undoing must not re-open a flag that was already cleared
// before it was judged.
func (s *Server) handleFlagsVerdictUndo(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	actor := auditActor(r)

	// Same lock, same reason, as handleFlagsVerdict above.
	s.verdictWatchlistMu.Lock()
	defer s.verdictWatchlistMu.Unlock()

	prior, known := s.Flags.Get(id)
	if !known {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	// Undoing an expected verdict takes back the destinations it
	// permitted as well as the expectation it recorded (#641). An undo
	// that reopened the flag while leaving the device permitted would be
	// the watchlist version of the half-undo UndoVerdict's own doc
	// comment rules out.
	if prior.Verdict == flags.VerdictExpected {
		if !s.withdrawPermittedFor(w, id, actor) {
			return
		}
	}

	f, ok := s.Flags.UndoVerdict(id)
	if !ok {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	s.Audit.Record(actor, "flag.verdict_undo", id, "")
	writeJSON(w, http.StatusOK, f)
}

// handleFlagsClearAll clears every currently-active flag in one request
// (issue #198's "Clear all", with the frontend's click-again confirm as
// the safeguard against an accidental single click). User-tier, not
// viewer (#653), same as the per-flag verdict above: this is a bulk
// change to what mikroview is showing. It records no judgement and no
// expectation, so nothing it does is irreversible -- a cleared flag
// raises again on the next matching event.
//
// One audit entry for the whole call, not one per flag: "cleared N
// flags" is the meaningful record here, and N individual entries would
// bury the one action that actually happened for anyone reading the log
// afterward. No expectations are recorded -- see flags.Store.ClearAll's
// own doc comment for why a bulk suppressing variant does not exist and
// is not planned.
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
