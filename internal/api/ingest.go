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
// A validated push lands in RouterState (issue #186 step 4), keyed by
// the token's own Device -- never by anything the payload claims about
// itself, so a router cannot report state for any device but the one
// its credential is scoped to. What that data then feeds -- host names
// via naming.Resolver, the rule/NAT table endpoints -- is display and
// attribution only; internal/routerstate's isolation test guarantees
// pushed data cannot reach a suspicion signal in either direction.
// ingestAuditInterval is how long a device may keep pushing the same
// kind with the same outcome before it is worth another audit row. A
// heartbeat, not a record of each push.
const ingestAuditInterval = 24 * time.Hour

// ingestAuditKey is one device's pushes of one kind. Both halves are
// bounded: device comes from an admin-issued token, and kind is checked
// against a fixed set by ingest.DecodePayload before it reaches here (an
// empty kind stands for a payload that never decoded).
type ingestAuditKey struct {
	device string
	kind   string
}

type ingestAuditState struct {
	lastAt time.Time
	lastOK bool
}

// noteIngest reports whether this push is worth an audit row.
//
// Every push used to write one. A scheduled RouterOS push runs every
// 15-30 minutes, and the audit log is pruned FIFO at 10,000 entries, so
// at the shipped rate limit (120 per 15 minutes) one ingest token
// produced 11,520 rows a day and rolled the *entire* admin trail --
// token.create, user.create, admin transfers -- in about 21 hours. Write
// cost at the cap was measured at 52.2 ms per record under the store's
// write lock, blocking GET /api/audit, for roughly 25.7 GiB of rewrites
// a day. #186 already documents that any RouterOS user holding the
// built-in read policy can print an ingest token out of a script.
//
// A successful, scheduled push is not an accountability event; what it
// erases is. So a row is written only when something changed:
//
//   - the first push of a kind from a device, which is genuinely new
//     information about the deployment;
//   - the outcome flipping, so a device that starts being refused (or
//     recovers) is on the record;
//   - ingestAuditInterval elapsing, so a long-running device still
//     leaves a periodic trace rather than falling silent in the log.
//
// Refusals go through the same gate deliberately. Auditing every refusal
// would just move the flood one branch over: a decode error is entirely
// caller-controlled and far cheaper to produce than a valid push.
//
// Rows are therefore bounded by devices x kinds x (1 + 1/day), and both
// factors are outside an attacker's control. Owner decision recorded on
// #285.
func (s *Server) noteIngest(device, kind string, ok bool, now time.Time) bool {
	s.ingestAuditMu.Lock()
	defer s.ingestAuditMu.Unlock()
	if s.ingestAudit == nil {
		s.ingestAudit = make(map[ingestAuditKey]ingestAuditState)
	}

	key := ingestAuditKey{device: device, kind: kind}
	prev, seen := s.ingestAudit[key]
	notable := !seen || prev.lastOK != ok || now.Sub(prev.lastAt) >= ingestAuditInterval
	if notable {
		s.ingestAudit[key] = ingestAuditState{lastAt: now, lastOK: ok}
	}
	return notable
}

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
		if s.noteIngest(tok.Device, "", false, now) {
			s.Audit.Record("device:"+tok.Device, "ingest.routeros.refused", tok.Device,
				fmt.Sprintf("payload rejected: %v", err))
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.RouterState.Apply(tok.Device, payload, now); err != nil {
		// A cap refusal (too many records/devices) -- the caller's own
		// push is what's oversized, and the message names only numbers
		// and the kind, nothing about other devices' state.
		if s.noteIngest(tok.Device, string(payload.Kind), false, now) {
			s.Audit.Record("device:"+tok.Device, "ingest.routeros.refused", tok.Device,
				fmt.Sprintf("kind=%s: %v", payload.Kind, err))
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// #186 step 5: never persist a raw payload wholesale. RouterState
	// above is in-memory only by design, and nothing here logs the
	// decoded records themselves either -- only their shape -- for the
	// same reason: this is reconnaissance-grade router config, and the
	// audit log is readable by every admin, not scoped to the one that
	// issued this token.
	if s.noteIngest(tok.Device, string(payload.Kind), true, now) {
		s.Audit.Record("device:"+tok.Device, "ingest.routeros", tok.Device,
			fmt.Sprintf("kind=%s page=%d/%d records=%d", payload.Kind, payload.Page, payload.Pages, payload.RecordCount()))
	}

	writeJSON(w, http.StatusOK, ingestAckResponse{
		Kind:    payload.Kind,
		Page:    payload.Page,
		Pages:   payload.Pages,
		Records: payload.RecordCount(),
	})
}

// routerTableResponse is the shape both table endpoints return.
// Available distinguishes "this device has never pushed this table"
// from "it pushed an empty one" -- the UI renders the former as "no
// data pushed yet" rather than an empty table pretending to be real,
// the same absence-is-not-evidence framing internal/routerstate's own
// accessors use.
type routerTableResponse struct {
	Available bool       `json:"available"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Rules     any        `json:"rules"`
}

// handleRouterOSRules serves device's pushed firewall filter table in
// RouterOS's own display order (issue #186 step 4c: "here is the table,
// go look at rule 7 in RouterOS"). Reads only mikroview's own
// RouterState store -- nothing contacts the router on request, which is
// the entire point of the push model (#110's rejected pull design would
// have needed a live RouterOS credential right here).
func (s *Server) handleRouterOSRules(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	rules, updatedAt, ok := s.RouterState.FilterRules(device)
	resp := routerTableResponse{Available: ok, Rules: rules}
	if ok {
		resp.UpdatedAt = &updatedAt
	}
	if rules == nil {
		resp.Rules = []ingest.FilterRule{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRouterOSNAT is handleRouterOSRules for the NAT table -- the
// display-table-only shape #186 step 4c settled on for NAT, since a log
// line carries a translation result, never which rule performed it.
func (s *Server) handleRouterOSNAT(w http.ResponseWriter, r *http.Request) {
	device := r.PathValue("device")
	rules, updatedAt, ok := s.RouterState.NATRules(device)
	resp := routerTableResponse{Available: ok, Rules: rules}
	if ok {
		resp.UpdatedAt = &updatedAt
	}
	if rules == nil {
		resp.Rules = []ingest.NATRule{}
	}
	writeJSON(w, http.StatusOK, resp)
}
