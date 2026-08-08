// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// ingestLimiterThreshold/Window bound how often one ingest token may call
// this endpoint. Sized around a full push, not a single request: #186
// step 0 measured pagination as whole records per page (e.g. 4 pages for
// 200 filter rules), and a real push can cover up to eight independent
// kinds (address lists, filter/NAT rules, DNS, DHCP, ARP, WireGuard
// interfaces/peers) in the same scheduler run, each potentially
// paginated -- dozens of requests arriving within seconds of each other
// is the expected shape of ONE push, not abuse. 120 requests per 15
// minutes gives a large push generous headroom while still meaningfully
// bounding a token used for anything else: #186 step 3 itself notes a
// 15-30 minute scheduler cadence is ample, so a sustained triple-digit
// request rate has no legitimate source.
const (
	ingestLimiterThreshold = 120
	ingestLimiterWindow    = 15 * time.Minute
)

// ingestAckResponse is deliberately minimal -- it echoes back what was
// accepted (never the payload's own content), which is enough for a
// router-side script to log "pushed N records of kind K, page P/M"
// without mikroview handing back anything an attacker holding the token
// could use to learn about mikroview's own state.
type ingestAckResponse struct {
	Kind    ingest.Kind `json:"kind"`
	Page    int         `json:"page"`
	Pages   int         `json:"pages"`
	Records int         `json:"records"`
}

// handleIngestRouterOS is issue #186 step 3: the push endpoint a
// router-side script POSTs to. Reachable only via an ingest bearer token
// -- see requireAuth's bearer-token branch and ingestRoutes -- there is
// no session-based path to it at all, matching #186's design: mikroview
// never holds RouterOS credentials, and a router pushing its own state is
// the only way data arrives here.
//
// This handler validates and accounts for a push; it does not yet act on
// one. Applying pushed data -- aliasing hosts/rules, populating the rule
// lookup table, raising (never lowering) a security signal -- is #186
// step 4, a deliberately separate change: this step's job is the
// endpoint's own mechanics (auth, rate limit, audit, strict decoding),
// independently shippable and testable per the issue's own step
// ordering. Until step 4 lands, a valid push is accepted, accounted for,
// and otherwise has no effect -- inert by design, not a partial
// implementation of step 4.
func (s *Server) handleIngestRouterOS(w http.ResponseWriter, r *http.Request) {
	tok := ingestTokenFromContext(r)
	if tok == nil {
		// Unreachable in practice: ingestRoutes is only ever dispatched to
		// from requireAuth's ingest-token branch, which always sets this
		// first. Guarded anyway rather than trusting that invariant
		// silently -- the alternative is a nil-pointer panic on tok.ID
		// below.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now()
	if !s.IngestLimiter.Reserve(tok.ID, now) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Same 64KiB bound every other JSON body on this API is held to (see
	// maxJSONBodyBytes) -- it also happens to be the number RouterOS's
	// own /tool fetch enforces client-side, so this can never be the
	// tighter of the two limits a real push runs into.
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	payload, err := ingest.DecodePayload(r.Body)
	if err != nil {
		// err is safe to echo: it describes a problem with the caller's
		// own request (an unknown field, an oversized value, a bad page
		// number), never anything about mikroview's existing state --
		// the same reasoning handleTokensCreate's validation-error branch
		// already relies on.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// #186 step 5: never persist a raw payload wholesale. Nothing here
	// logs the decoded records themselves either -- only their shape --
	// for the same reason: this is reconnaissance-grade router config,
	// and the audit log is readable by every admin, not scoped to the
	// one that issued this token.
	s.Audit.Record("device:"+tok.Device, "ingest.routeros", tok.Device,
		fmt.Sprintf("kind=%s page=%d/%d records=%d", payload.Kind, payload.Page, payload.Pages, payload.RecordCount()))

	writeJSON(w, http.StatusOK, ingestAckResponse{
		Kind:    payload.Kind,
		Page:    payload.Page,
		Pages:   payload.Pages,
		Records: payload.RecordCount(),
	})
}
