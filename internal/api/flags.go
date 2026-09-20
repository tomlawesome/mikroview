// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/flags"
)

// handleFlagsList serves every known flag, active and cleared -- the
// frontend decides how much cleared history to keep showing (see
// docs/configuration.md) -- plus the last hour of newly-raised-episode
// counts by Type at 1-minute resolution (flags.Store.TimeSeries), for
// FlagsChart. Same shape convention as GET /api/stats's timeSeries
// field (internal/store/ring.go's Stats.TimeSeries) -- added alongside
// the existing flags array rather than as a new endpoint.
//
// Since #768 it also carries "baselinesWarming": whether any enabled
// detection is still warming, for the learning shelf's "why is
// mikroview silent" line. Additive and omitted when it cannot be
// answered -- see baselinesWarming below.
func (s *Server) handleFlagsList(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"flags":      s.Flags.List(),
		"timeSeries": s.Flags.TimeSeries(),
	}
	if warming := s.baselinesWarming(s.now()); warming != nil {
		body["baselinesWarming"] = *warming
	}
	writeJSON(w, http.StatusOK, body)
}

// baselinesWarming answers the learning shelf's one question (#768): is
// any enabled detection still holding an observed key below its history
// floor -- the state engine.Snapshot.ProvisionalFire fires in, and so
// the state in which the shelf's "a spike seen now appears here as a
// provisional flag" claim is true.
//
// It rides on GET /api/flags rather than being inferred from
// /api/definitions (the owner's decision on #768, 2026-09-02): the shelf
// is a flags surface, re-rendering on the flags poll, and two polls on
// different cadences feeding one component let it disagree with the
// flags beside it. Viewer-readable, because this route is (#653) --
// the same fact, on the surface that shows it.
//
// nil means "cannot say", and the field is then omitted from the
// response entirely rather than sent as false: with no live engine
// wired there is no warm-up state to report, and false would be a claim
// this server cannot make. Same silence-over-guess rule the definitions
// surface's own learning field follows (learningView's doc comment).
//
// The three exclusions are #642's ruling, amendment 2, unchanged --
// they were the frontend's anyBaselineWarming and are now this:
//   - a definition with no warm-up concept (Learning's own ok=false);
//   - a disabled detection, however warm its baseline;
//   - "no traffic seen yet" (Keys 0) -- nothing observed can be
//     provisional, so the shelf's claim would be false.
func (s *Server) baselinesWarming(now time.Time) *bool {
	if s.Learning == nil || s.Definitions == nil {
		return nil
	}
	warming := false
	for _, sd := range s.Definitions.List() {
		d := sd.Definition
		// An unavailable definition is preserved but never evaluated
		// (engine.StoredDefinition.Available), so it can warm nothing.
		if !sd.Available || !d.Enabled || d.Intent != engine.IntentDetection {
			continue
		}
		state, ok := s.Learning.Learning(d.ID, now)
		if !ok {
			continue
		}
		if state.Keys > state.Ready {
			warming = true
			break
		}
	}
	return &warming
}

// verdictRequest is POST /api/flags/{id}/verdict's body: one of the
// four bare verdict labels the owner ratified on #640 (2026-09-02) --
// "expected", "checked", "investigate" or "resolved" -- and, since
// #1232, the operator's optional note explaining it.
//
// The note is not #640's retired "clear with a note": that was a prompt
// after the decision, this is what was already written in the drawer
// before the verdict was clicked, and it is always optional.
type verdictRequest struct {
	Verdict flags.Verdict `json:"verdict"`
	Note    string        `json:"note"`
}

// noteRequest is PUT /api/flags/{id}/note's body: a replacement note
// for a flag that already carries a verdict (#1232's "we should be able
// to edit"). Empty is allowed and means "take what I wrote back" --
// the same state as never having written one.
type noteRequest struct {
	Note string `json:"note"`
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
// that any user can decide it. A verdict that arrived with a note says
// so -- "checked, with note" -- and never carries the words themselves:
// the flag is the note's one home (#1232), so undoing the verdict can
// discard it without an audit entry being deleted or left quoting text
// that has gone.
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
	// Through decodeJSONBody rather than json.NewDecoder(r.Body)
	// directly, since #1232 put free operator text in this body:
	// maxJSONBodyBytes is what bounds how much of it one request can
	// make this process hold.
	var req verdictRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
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
	f, ok, err := s.Flags.SetVerdict(id, req.Verdict, actor, req.Note, now)
	if err != nil {
		// R6: a verdict that only exists in memory must not be reported
		// as kept. SetVerdict has already put its own state back on this
		// path -- see its own doc comment -- so there is nothing here to
		// roll back beyond not claiming success. The backend's own words
		// stay server-side (apiLog), same as every other persistence
		// failure in this handler group.
		apiLog.Error("saving a flag verdict failed: " + err.Error())
		http.Error(w, "the verdict could not be saved, so nothing was changed", http.StatusInternalServerError)
		return
	}
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
			if _, _, uerr := s.Flags.UndoVerdict(id); uerr != nil {
				apiLog.Warn("rolling back a verdict after its watchlist write failed: " + uerr.Error())
			}
			http.Error(w, "recording this flag's destinations on the watchlist failed, so the verdict was not kept: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if wrote {
			if _, err := s.Flags.RecordPermitted(id, rec); err != nil {
				// The watchlist write already landed and the verdict is
				// already saved; only the ledger record tying them
				// together failed. Take both back rather than leave a
				// verdict standing with no record of what it permitted --
				// the same all-or-nothing rule this handler just applied
				// above when the watchlist write itself failed.
				if uerr := s.unpermitFlagEvidence(rec, actor); uerr != nil {
					apiLog.Warn("rolling back a verdict's watchlist write failed: " + uerr.Error())
				}
				if _, _, uerr := s.Flags.UndoVerdict(id); uerr != nil {
					apiLog.Warn("rolling back a verdict after its permitted-destinations record failed to save: " + uerr.Error())
				}
				apiLog.Error("saving a flag's permitted-destinations record failed: " + err.Error())
				http.Error(w, "recording this flag's permitted destinations failed, so the verdict was not kept", http.StatusInternalServerError)
				return
			}
		}
	}
	detail := string(req.Verdict)
	if req.Note != "" {
		detail += ", with note"
	}
	s.Audit.Record(actor, "flag.verdict", id, detail)
	writeJSON(w, http.StatusOK, f)
}

// handleFlagNote replaces the note on an already-judged flag (#1232's
// "we should be able to edit"). A note written *before* the verdict
// travels with the verdict itself, on the POST above; this is only the
// edit afterwards, which the drawer sends on blur.
//
// Same user tier as the verdict it explains, for the same #653 reason:
// a viewer may not change what mikroview is showing, and the note is
// part of the record a returning flag reads back.
//
// 404 for an id the store does not know. 409 where it does but the flag
// carries no verdict: the note belongs to the decision, so there is
// nothing for it to be the reason for and nothing for an undo to
// discard it with -- storing it anyway would leave text in a place
// nothing on screen ever reads back. 200 with the updated flag
// otherwise.
//
// Audit-logged as flag.note_edit, with no text (#1232's ratified §3 --
// the flag is the note's only home, so an edit never leaves an older
// wording standing in a second one). Empty is a legitimate edit: it is
// how the operator takes back what they wrote.
func (s *Server) handleFlagNote(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	var req noteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	id := r.PathValue("id")
	f, known, judged, err := s.Flags.SetNote(id, req.Note)
	if err != nil {
		// R6: an edit that only exists in memory must not be reported as
		// kept -- SetNote has already put the old note back.
		apiLog.Error("saving a flag note failed: " + err.Error())
		http.Error(w, "the note could not be saved, so nothing was changed", http.StatusInternalServerError)
		return
	}
	if !known {
		http.Error(w, "flag not found", http.StatusNotFound)
		return
	}
	if !judged {
		http.Error(w, "this flag has no verdict for a note to belong to", http.StatusConflict)
		return
	}
	s.Audit.Record(auditActor(r), "flag.note_edit", id, "")
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

	f, ok, err := s.Flags.UndoVerdict(id)
	if err != nil {
		// R6: same reasoning as handleFlagsVerdict's own SetVerdict
		// check -- an undo that only exists in memory must not be
		// reported as done, and UndoVerdict has already put its own
		// state back.
		apiLog.Error("undoing a flag verdict failed: " + err.Error())
		http.Error(w, "the undo could not be saved, so nothing was changed", http.StatusInternalServerError)
		return
	}
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
	n, err := s.Flags.ClearAll(time.Now())
	if err != nil {
		// R6: a bulk clear that only exists in memory must not be
		// reported as done -- ClearAll has already put every flag it
		// touched back to active.
		apiLog.Error("clearing all flags failed: " + err.Error())
		http.Error(w, "the flags could not be cleared, so nothing was changed", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		s.Audit.Record(auditActor(r), "flag.clear_all", "", fmt.Sprintf("cleared %d flags", n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": n})
}

// handleExpectationsList serves the ledger (#640 part C): every
// expectation this deployment has recorded -- what the operator has
// told mikroview is normal here -- with each entry's recorded size,
// how many firings it has absorbed and when it was first made (see
// flags.Exclusion).
//
// Viewer tier, the same "core read" as GET /api/flags. An expectation
// is the reason a firing that would otherwise be on the flags card is
// not, so a caller who may read the flags but not the expectations
// behind them is reading half the story -- and reading the ledger
// changes nothing, which is the line #653 drew for the viewer tier.
//
// It reads the same entries handleExclusionsList above serves. The two
// differ in who they are for, not in what they read: that one is the
// admin's undo surface for the admin-only clear-permanent, this one is
// the operator's own record of what they have taught this instance.
// #640's part B retires exclude-forever and the admin pair with it,
// leaving this pair as the only view of them; splitting the delivery
// is why both exist at once here rather than either being a shim.
func (s *Server) handleExpectationsList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"expectations": s.Flags.ListExclusions()})
}

// handleExpectationForget removes one expectation, so its (Type,
// Target) raises again from its next firing -- the ledger's per-row
// Forget control.
//
// User tier, matching POST /api/flags/{id}/verdict rather than the
// admin gate on handleExclusionRemove above: the operator who can say
// "expected" can take it back, and an undo must not be harder to reach
// than the thing it undoes. The asymmetry on the admin pair is not
// duplicated here because it follows from *its* creating action being
// admin-only, and because forgetting only ever re-arms detection --
// the safe direction, unlike the exclusion that created it.
//
// 204 with no body on success, 404 when no expectation has that id.
// Deliberately not handleExclusionRemove's "no-op, not an error" 200:
// there the caller may be a stale affordance racing a page that moved
// on, while here the operator clicked a row they can see, so a silent
// success on an unknown id would leave the ledger looking pruned when
// nothing was.
func (s *Server) handleExpectationForget(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	ok, err := s.Flags.RemoveExclusionByID(id)
	if err != nil {
		// R6: a forget that only exists in memory must not be reported as
		// done -- RemoveExclusionByID has already put the expectation back.
		apiLog.Error("removing an expectation failed: " + err.Error())
		http.Error(w, "the expectation could not be removed, so nothing was changed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "expectation not found", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "flag.expectation_forget", id, "")
	w.WriteHeader(http.StatusNoContent)
}
