// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/decommission"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/routerstate"
)

// This file is issue #460's surface: the offer, the answer, and what the
// map needs to draw a retiring segment.
//
// It is also the *narrow caller* the two isolation rules require. Pushed
// router state may not reach the evaluation machinery through a package
// edge, in either direction (internal/routerstate/isolation_test.go),
// which is why the enrichment a watch carries is copied here, once, at
// the moment the offer is answered -- from routerstate's frozen
// last-known snapshot into the watch's own record. After that the engine
// reads only plain data it was handed, and never the router's tables.

// decommissionOfferView is one pending offer plus the receipt that
// argues for it.
//
// The receipt travels with the offer rather than being fetched on demand
// because #485's ratified sentence is that the offer appears *with* it:
// "watch the dead range for stragglers?", with "in the last N hours this
// would have caught 3". A dialogue that asks first and justifies second
// is a different, weaker interaction.
type decommissionOfferView struct {
	Device     string                     `json:"device"`
	CIDR       string                     `json:"cidr"`
	Address    string                     `json:"address"`
	Interface  string                     `json:"interface"`
	Name       string                     `json:"name"`
	DepartedAt time.Time                  `json:"departedAt"`
	LastKnown  []routerstate.KnownHost    `json:"lastKnown,omitempty"`
	Coverage   engine.CoverageState       `json:"coverage"`
	Receipt    *decommissionReceiptView   `json:"receipt,omitempty"`
	Decline    *decommissionDeclineView   `json:"decline,omitempty"`
	Suggested  decommissionSuggestionView `json:"suggested"`
}

// decommissionReceiptView is the replay answer, count and span together.
// Never the count alone: "would have caught 3" means nothing without the
// period it was counted over, and a receipt that omits its span invites
// the reader to supply a flattering one.
type decommissionReceiptView struct {
	EmissionCount int       `json:"emissionCount"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	Duration      string    `json:"duration"`
	EventCount    int       `json:"eventCount"`
	Truncated     bool      `json:"truncated"`
}

// decommissionDeclineView is what is shown instead when there is nothing
// to replay against -- an honest refusal rather than a zero.
type decommissionDeclineView struct {
	Reason string `json:"reason"`
}

// decommissionSuggestionView carries the defaults the offer is
// pre-filled with, so the surface does not hard-code a number the server
// owns.
type decommissionSuggestionView struct {
	CleanWindow string `json:"cleanWindow"`
}

// decommissionWatchView is one live watch, painted.
type decommissionWatchView struct {
	decommission.Watch
	// State is the watch's state at the moment of the request. Derived,
	// never stored, so it cannot go stale against the record -- see
	// decommission.Watch.StateAt.
	State decommission.State `json:"state"`
	// RetiresIn is how much longer the range must stay silent. Rendered
	// as a duration string beside State, from the same computation, so
	// the countdown and the paint can never disagree.
	RetiresIn string `json:"retiresIn"`
	// Coverage is the live answer to "could a straggler be seen at all",
	// alongside the stored Covered flag the state machine reads. Both are
	// reported: the flag is what retirement is decided on, and this is
	// the evidence for it.
	Coverage engine.CoverageState `json:"coverage"`
}

// decommissionResponse is one call for the whole surface: what is being
// offered, and what is already being watched.
//
// One endpoint rather than two because the map needs both on every
// paint, and because an offer and a ghost are the same object one
// decision apart -- splitting them would mean two requests that can
// disagree about a segment mid-answer.
type decommissionResponse struct {
	Offers  []decommissionOfferView `json:"offers"`
	Watches []decommissionWatchView `json:"watches"`
	// EvidenceComplete carries #367's caveat outward: false means at
	// least one router feeds events but never pushed its filter table,
	// so a coverage answer of "nothing is logging this" cannot be
	// trusted as a definite negative.
	EvidenceComplete bool `json:"evidenceComplete"`
}

// handleDecommission serves every pending offer and every live watch.
//
// Viewer tier: this is the map's own data. A viewer already sees the
// zones, the hosts on them and the watches over them, so a retiring zone
// is nothing new to them -- and hiding a ghost from a viewer would make
// the map lie about the network rather than protect anything.
func (s *Server) handleDecommission(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	rulesByDevice, evidence := s.definitionsCoverage()

	resp := decommissionResponse{
		Offers:           []decommissionOfferView{},
		Watches:          []decommissionWatchView{},
		EvidenceComplete: evidence.Complete,
	}

	if s.RouterState != nil {
		corpus := s.replayCorpus()
		for _, d := range s.RouterState.Departures() {
			resp.Offers = append(resp.Offers, s.offerViewFor(d, rulesByDevice, corpus))
		}
	}

	if s.Decommissions != nil {
		// Coverage is refreshed here as well as on a filter-rule push,
		// so a watch created before any router pushed a table stops
		// reporting broken as soon as one does, without waiting for the
		// next push to arrive.
		for _, wt := range s.Decommissions.List() {
			covered := engine.DecommissionCoverage(wt.CIDR, rulesByDevice) == engine.CoverageOK
			if covered != wt.Covered {
				if err := s.Decommissions.SetCovered(wt.ID, covered); err == nil {
					wt.Covered = covered
				}
			}
			resp.Watches = append(resp.Watches, decommissionWatchView{
				Watch:     wt,
				State:     wt.StateAt(now),
				RetiresIn: wt.Remaining(now).Round(time.Minute).String(),
				Coverage:  engine.DecommissionCoverage(wt.CIDR, rulesByDevice),
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// offerViewFor builds one offer, replaying the candidate range over the
// corpus to produce its receipt.
func (s *Server) offerViewFor(d routerstate.Departure, rulesByDevice map[string][]ingest.FilterRule, corpus engine.Corpus) decommissionOfferView {
	v := decommissionOfferView{
		Device:     d.Device,
		CIDR:       d.CIDR,
		Address:    d.Address,
		Interface:  d.Interface,
		Name:       d.Name,
		DepartedAt: d.DepartedAt,
		LastKnown:  d.LastKnown,
		Coverage:   engine.DecommissionCoverage(d.CIDR, rulesByDevice),
		Suggested:  decommissionSuggestionView{CleanWindow: s.decommissionCleanWindow().String()},
	}
	if corpus == nil {
		v.Decline = &decommissionDeclineView{Reason: "no event corpus is available to replay this against"}
		return v
	}
	result, err := engine.ReplayDecommission(d.CIDR, corpus)
	if err != nil {
		v.Decline = &decommissionDeclineView{Reason: err.Error()}
		return v
	}
	if result.Decline != nil {
		v.Decline = &decommissionDeclineView{Reason: result.Decline.Reason}
		return v
	}
	if rec := result.Receipt; rec != nil {
		win := rec.Window()
		v.Receipt = &decommissionReceiptView{
			EmissionCount: rec.EmissionCount(),
			Start:         win.Start(),
			End:           win.End(),
			Duration:      win.Duration().String(),
			EventCount:    win.EventCount(),
			Truncated:     rec.CorpusTruncated(),
		}
	}
	return v
}

// decommissionCleanWindow is the configured default, clamped the same way
// the config validator clamps it, so a Server constructed in a test
// without config still offers a usable window rather than zero.
func (s *Server) decommissionCleanWindow() time.Duration {
	if s.DecommissionCleanWindow < decommission.MinCleanWindow || s.DecommissionCleanWindow > decommission.MaxCleanWindow {
		return decommission.DefaultCleanWindow
	}
	return s.DecommissionCleanWindow
}

// createDecommissionRequest is the wire shape for POST
// /api/decommission/watches -- the "yes" answer to an offer.
type createDecommissionRequest struct {
	Device string `json:"device"`
	CIDR   string `json:"cidr"`
	// CleanWindow overrides the configured default for this watch alone,
	// as a Go duration string. Per watch because a range that hosted a
	// nightly job honestly needs longer than a range of desk phones, and
	// the operator answering the offer is the one who knows which.
	CleanWindow string `json:"cleanWindow,omitempty"`
}

// handleDecommissionCreate answers an offer with yes: the zone stays on
// the map as a ghost, painted by its watch state, until the watch
// retires.
//
// User tier, matching POST /api/definitions: this creates server-side
// traffic surveillance over a range, which is the same kind of change to
// what the instance watches.
func (s *Server) handleDecommissionCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	if s.Decommissions == nil {
		http.Error(w, "decommission watches are not available on this deployment", http.StatusServiceUnavailable)
		return
	}
	var req createDecommissionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// The offer is the only way in. A watch is created from a departure
	// mikroview actually observed, never from an arbitrary range typed at
	// the API: the whole feature is "the operator retired this and we
	// noticed", and a range nobody retired has no ghost to draw, no
	// last-known names to enrich with, and no honest zone to sit in.
	departure, ok := s.findDeparture(req.Device, req.CIDR)
	if !ok {
		http.Error(w, "no segment departure is pending for that range", http.StatusNotFound)
		return
	}

	window := s.decommissionCleanWindow()
	if strings.TrimSpace(req.CleanWindow) != "" {
		parsed, err := time.ParseDuration(req.CleanWindow)
		if err != nil {
			http.Error(w, "cleanWindow is not a duration", http.StatusBadRequest)
			return
		}
		window = parsed
	}

	rulesByDevice, _ := s.definitionsCoverage()
	corpus := s.replayCorpus()

	watch := decommission.Watch{
		CIDR:        departure.CIDR,
		Device:      departure.Device,
		Interface:   departure.Interface,
		Name:        departure.Name,
		CreatedAt:   time.Now(),
		CleanWindow: window,
		Covered:     engine.DecommissionCoverage(departure.CIDR, rulesByDevice) == engine.CoverageOK,
		LastKnown:   stragglersFrom(departure.LastKnown),
	}
	// The receipt is stored with the watch, not just shown once: the
	// number is the argument the operator said yes to, and an operator
	// revisiting the ghost a day later deserves to see what convinced
	// them rather than a fresh number measured over a different corpus.
	if corpus != nil {
		if result, err := engine.ReplayDecommission(departure.CIDR, corpus); err == nil && result.Receipt != nil {
			watch.ReplayCount = result.Receipt.EmissionCount()
			watch.ReplaySpan = result.Receipt.Window().Duration()
		}
	}

	stored, err := s.Decommissions.Add(watch)
	if err != nil {
		writeDecommissionError(w, err)
		return
	}
	s.RouterState.AckDeparture(departure.Device, departure.CIDR)
	s.Audit.Record(auditActor(r), "decommission.watch", stored.ID, stored.CIDR)
	writeJSON(w, http.StatusCreated, s.watchViewFor(stored, rulesByDevice, time.Now()))
}

// dismissDecommissionRequest is the wire shape for the "no" answer.
type dismissDecommissionRequest struct {
	Device string `json:"device"`
	CIDR   string `json:"cidr"`
}

// handleDecommissionDismiss answers an offer with no: the zone leaves at
// once and no watch is created (#485's ratified "on no" branch).
//
// The offer is dropped rather than remembered as declined. The next push
// does not carry the range either, and re-offering it every fifteen
// minutes would turn a considered answer into nagging -- while a genuine
// change of mind is served by the range reappearing and departing again,
// which is a real second decommission with its own clock.
func (s *Server) handleDecommissionDismiss(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	if s.RouterState == nil {
		http.Error(w, "no router state is available", http.StatusServiceUnavailable)
		return
	}
	var req dismissDecommissionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cidr, err := decommission.NormaliseCIDR(req.CIDR)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.RouterState.AckDeparture(req.Device, cidr) {
		http.Error(w, "no segment departure is pending for that range", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "decommission.dismiss", req.Device, cidr)
	w.WriteHeader(http.StatusNoContent)
}

// forceDecommissionRequest carries the recorded override.
type forceDecommissionRequest struct {
	// Reason is what the operator typed. Required: the #385 pattern is a
	// heavy warning plus a *recorded* override, and an override with no
	// stated reason records only that someone clicked through.
	Reason string `json:"reason"`
}

// handleDecommissionForce is the force-remove escape hatch (owner ruling,
// 2026-08-17).
//
// The segment leaves the map immediately. The watch does not end: it
// detaches and finishes the same job from the watchlist, retiring on the
// same clean window. So the two paths converge on the same end state --
// patient removal keeps the draining segment on the map until it is
// quiet; force-remove takes it off the map and keeps watching. Nothing is
// silently dropped either way, which is the property the ruling turns on
// and the reason this is not simply a delete.
func (s *Server) handleDecommissionForce(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	if s.Decommissions == nil {
		http.Error(w, "decommission watches are not available on this deployment", http.StatusServiceUnavailable)
		return
	}
	var req forceDecommissionRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		http.Error(w, "a reason is required: forcing a segment off the map while its traffic persists is a recorded override, and an override with no reason records only that someone clicked through", http.StatusBadRequest)
		return
	}
	actor := auditActor(r)
	stored, err := s.Decommissions.ForceRemove(r.PathValue("id"), actor, strings.TrimSpace(req.Reason), time.Now())
	if err != nil {
		writeDecommissionError(w, err)
		return
	}
	s.Audit.Record(actor, "decommission.force", stored.ID, stored.CIDR+": "+stored.ForcedReason)
	rulesByDevice, _ := s.definitionsCoverage()
	writeJSON(w, http.StatusOK, s.watchViewFor(stored, rulesByDevice, time.Now()))
}

// handleDecommissionDelete abandons a watch outright -- "I no longer want
// to be asked about this range". Distinct from retirement, which is the
// watch finishing its job, and from force-remove, which only takes it off
// the map.
func (s *Server) handleDecommissionDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsUser(r) {
		http.Error(w, "user role required", http.StatusForbidden)
		return
	}
	if s.Decommissions == nil {
		http.Error(w, "decommission watches are not available on this deployment", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.Decommissions.Delete(id); err != nil {
		writeDecommissionError(w, err)
		return
	}
	s.Audit.Record(auditActor(r), "decommission.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// findDeparture locates one pending offer by device and range, accepting
// the range in whatever form the caller wrote it.
func (s *Server) findDeparture(device, cidr string) (routerstate.Departure, bool) {
	if s.RouterState == nil {
		return routerstate.Departure{}, false
	}
	normalised, err := decommission.NormaliseCIDR(cidr)
	if err != nil {
		return routerstate.Departure{}, false
	}
	for _, d := range s.RouterState.Departures() {
		if d.Device == device && d.CIDR == normalised {
			return d, true
		}
	}
	return routerstate.Departure{}, false
}

// stragglersFrom copies routerstate's frozen last-known snapshot into the
// watch's own record.
//
// A copy, not a reference: this is the import-edge crossing the package
// comment describes, and it is also what makes the enrichment survive the
// router's tables draining away over the pushes that follow.
func stragglersFrom(known []routerstate.KnownHost) []decommission.Straggler {
	if len(known) == 0 {
		return nil
	}
	out := make([]decommission.Straggler, 0, len(known))
	for _, k := range known {
		out = append(out, decommission.Straggler{
			Address: k.Address,
			Name:    k.Name,
			Source:  k.Source,
			SeenAt:  k.SeenAt,
		})
	}
	return out
}

func (s *Server) watchViewFor(wt decommission.Watch, rulesByDevice map[string][]ingest.FilterRule, now time.Time) decommissionWatchView {
	return decommissionWatchView{
		Watch:     wt,
		State:     wt.StateAt(now),
		RetiresIn: wt.Remaining(now).Round(time.Minute).String(),
		Coverage:  engine.DecommissionCoverage(wt.CIDR, rulesByDevice),
	}
}

// refreshDecommissionCoverage re-answers "could a straggler be seen at
// all" for every live watch.
//
// Called from the ingest path when a router pushes a filter table, which
// is the only thing that can change the answer. Doing it there rather
// than on a timer means a rule an operator just switched logging on for
// clears a broken ghost on the next push, not at some arbitrary later
// moment.
func (s *Server) refreshDecommissionCoverage() {
	if s.Decommissions == nil {
		return
	}
	rulesByDevice, _ := s.definitionsCoverage()
	for _, wt := range s.Decommissions.List() {
		if !wt.RetiredAt.IsZero() {
			continue
		}
		covered := engine.DecommissionCoverage(wt.CIDR, rulesByDevice) == engine.CoverageOK
		if covered != wt.Covered {
			_ = s.Decommissions.SetCovered(wt.ID, covered)
		}
	}
}

// writeDecommissionError maps this package's refusals onto status codes,
// so a caller can tell "no such watch" from "that range is already
// watched" without parsing prose.
func writeDecommissionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, decommission.ErrNoSuchWatch):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, decommission.ErrAlreadyEnded):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
