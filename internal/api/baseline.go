// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/baseline"
	"github.com/tomlawesome/mikroview/internal/config"
)

// baselineExpectedRequest is the wire shape PUT
// /api/baseline/{key}/expected accepts. The key comes from the URL and
// the actor from the session, the same "identity from the route/session,
// not the body" convention hostMarkRequest documents next door.
type baselineExpectedRequest struct {
	Reason string `json:"reason"`
}

// offBaselineLine is one line in the off-baseline answer.
//
// A deliberately narrower shape than baseline.Line: the recurrence
// bitmap, the anchor day and the all-time first-seen are how the
// register decides, not what the map draws, and shipping them would
// invite a caller to re-derive establishment in the browser against a
// threshold the server did not use.
type offBaselineLine struct {
	Key   string `json:"key"`
	SrcIP string `json:"srcIp"`
	DstIP string `json:"dstIp"`
	// Port is a JSON number, or null when the event carried no
	// destination port -- null rather than 0, because 0 is not a port and
	// a caller formatting `:0` into a label would be stating something
	// untrue.
	Port  *int   `json:"port"`
	Proto string `json:"proto"`
	// Count and FirstSeenToday are today's, not all time: the card on a
	// bright element lists exactly these two (DESIGN.md, "Saying it is
	// expected").
	Count          uint64 `json:"count"`
	FirstSeenToday int64  `json:"firstSeenToday"`
	// Outcome is "accept" or "drop" -- the worst verdict this line drew
	// today, since colour is the verdict and a line refused once has been
	// refused.
	Outcome string `json:"outcome"`
}

// offBaselineResponse is the whole answer to GET /api/baseline/off.
//
// Config travels with the lines rather than being fetched separately so
// the browser can say "3 of the last 14 days" using the numbers the
// server actually applied, and cannot drift into describing a threshold
// nobody used.
type offBaselineResponse struct {
	Config      baseline.Config   `json:"config"`
	GeneratedAt int64             `json:"generatedAt"`
	Count       int               `json:"count"`
	Lines       []offBaselineLine `json:"lines"`
	// HostQuietAfterMs is how long a host may be silent before the map
	// draws it quiet (config baseline.hostQuietAfter, 24h by default).
	//
	// It rides along here rather than on GET /api/hosts because it is a
	// threshold, not a host: it belongs with the other two thresholds
	// this endpoint already carries, and the alternative was a second
	// request on every page load to fetch one number. It is a sibling of
	// the pinned fields above, never inside config, so a caller reading
	// config.days/config.of sees exactly the two-field object the design
	// specified.
	HostQuietAfterMs int64 `json:"hostQuietAfterMs"`
}

// handleBaselineOff serves today's off-baseline lines (issue #1016,
// round 49): the lines seen today that are not on the established
// pattern, plus the threshold that judged them and how many there are.
//
// Only those. An established line is deliberately unreachable through
// this endpoint, and that is the design rather than an optimisation: on
// a busy network the established set *is* the entire traffic set, so
// returning it would make the payload proportional to volume, when the
// whole claim of the feature is that it is proportional to novelty. The
// header's `off-baseline today · N` count and the roll-up that brightens
// one road among a thousand both need the short list, not the long one.
//
// Viewer tier, the same asymmetry GET /api/hosts and GET
// /api/coverage/declarations already carry relative to their own
// user-tier writes: a non-admin looking at the map is exactly who needs
// to see what is off pattern today.
//
// Deliberately not on readOnlyRoutes, for the same reason GET /api/hosts
// is not: this is a partial inventory of the operator's private address
// space -- with destinations and ports attached, so if anything a
// sharper one -- and no bearer token has ever been able to read that.
func (s *Server) handleBaselineOff(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	lines := s.Baseline.OffToday(now)

	out := make([]offBaselineLine, 0, len(lines))
	for _, l := range lines {
		var port *int
		if l.Port != 0 {
			p := l.Port
			port = &p
		}
		// A line reaches this list only by having been seen today, so
		// FirstSeenToday is always set; LastSeen is the honest fallback
		// rather than a zero epoch if that ever stops being true.
		first := l.FirstSeenToday
		if first.IsZero() {
			first = l.LastSeen
		}
		outcome := string(l.OutcomeToday)
		if outcome == "" {
			outcome = string(baseline.OutcomeAccept)
		}
		out = append(out, offBaselineLine{
			Key:            l.Key,
			SrcIP:          l.SrcIP,
			DstIP:          l.DstIP,
			Port:           port,
			Proto:          l.Proto,
			Count:          l.CountToday,
			FirstSeenToday: first.UnixMilli(),
			Outcome:        outcome,
		})
	}

	// A Server built without the threshold -- every test that does not
	// care about it -- still answers with the ratified default rather
	// than zero, which the browser would read as "every host is quiet".
	quietAfter := s.HostQuietAfter
	if quietAfter <= 0 {
		quietAfter = config.DefaultHostQuietAfter
	}

	writeJSON(w, http.StatusOK, offBaselineResponse{
		Config:           s.Baseline.Config(),
		GeneratedAt:      now.UnixMilli(),
		Count:            len(out),
		Lines:            out,
		HostQuietAfterMs: quietAfter.Milliseconds(),
	})
}

// handleBaselineExpectedPut records an operator's statement that the
// line at {key} is meant to be there, which makes it established from
// then on -- "a reason that stays said" (DESIGN.md). It is the only way
// a line leaves the bright state early.
//
// User tier and above, the same gate and the same reasoning as
// handleHostMarkPut: saying that traffic is expected changes how the
// next human reads the map, so it carries the weight of any other
// authored explanation and is audit-logged like one. It also exempts the
// line from eviction, which is a second reason it is not a viewer's to
// make.
//
// The reason is required and is refused when empty. A bare "expected"
// flag with nothing behind it would answer the question the sieve exists
// to ask -- why is this here? -- with silence, and would still be
// sitting on the map months later with nobody able to say who decided
// what.
//
// 404 rather than 400 for a key no event has ever registered, the same
// stale-page reasoning handleHostMarkPut documents: marking a line the
// feed has never shown would invent traffic rather than record it.
func (s *Server) handleBaselineExpectedPut(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	// The key is caller-controlled input from the URL and reaches the
	// audit trail below, so it is checked before it is used for anything.
	key := r.PathValue("key")
	if err := baseline.ValidateKey(key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req baselineExpectedRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	l, err := s.Baseline.Expect(key, req.Reason, auditActor(r))
	if errors.Is(err, baseline.ErrUnknownLine) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The stored reason, not the request's: what went on the record is
	// what the audit line should say.
	s.Audit.Record(auditActor(r), "baseline.expected", l.Key, "reason="+l.Expected.Reason)
	writeJSON(w, http.StatusOK, l)
}

// handleBaselineExpectedDelete takes the statement back, putting the
// line back to whatever its own recurrence says it is -- which may well
// be off-baseline again on the next read. User tier and above, same gate
// as handleBaselineExpectedPut: withdrawing an explanation is the same
// class of decision as making one.
//
// 404 when there is no mark to remove, the same "the caller is working
// from a list this endpoint served" reasoning handleHostMarkDelete
// documents.
func (s *Server) handleBaselineExpectedDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}

	key := r.PathValue("key")
	if err := baseline.ValidateKey(key); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.Baseline.Unexpect(key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "baseline.unexpected", key, "")
	w.WriteHeader(http.StatusNoContent)
}
