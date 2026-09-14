// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/droplist"
)

// droplistEntryResponse is what one entry looks like over the wire --
// the CIDR rendered as its canonical string, never the netip.Prefix
// struct itself.
type droplistEntryResponse struct {
	CIDR    string    `json:"cidr"`
	AddedBy string    `json:"addedBy"`
	AddedAt time.Time `json:"addedAt"`
	Reason  string    `json:"reason"`
	FlagID  string    `json:"flagID,omitempty"`
}

func toDroplistEntryResponse(e droplist.Entry) droplistEntryResponse {
	return droplistEntryResponse{
		CIDR:    e.CIDR.String(),
		AddedBy: e.AddedBy,
		AddedAt: e.AddedAt,
		Reason:  e.Reason,
		FlagID:  e.FlagID,
	}
}

// droplistKeyStatus is the pull key's own status, folded into
// GET /api/droplist rather than a separate endpoint -- the settings
// group this backs shows the list and the key's state together. At most
// one droplist-pull token is ever meant to exist (see
// handleDroplistKeyCreate); Present is false and every other field is
// its zero value when none does.
type droplistKeyStatus struct {
	Present    bool      `json:"present"`
	CreatedAt  time.Time `json:"createdAt,omitzero"`
	CreatedBy  string    `json:"createdBy,omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
}

// droplistKeyStatus reads the single droplist-pull token, if one exists,
// via TokenStore.ByKind -- never the raw value, which this store never
// retains past the moment it was minted.
func (s *Server) droplistKeyStatus() droplistKeyStatus {
	toks := s.Tokens.ByKind(auth.TokenKindDroplistPull)
	if len(toks) == 0 {
		return droplistKeyStatus{}
	}
	t := toks[0]
	return droplistKeyStatus{
		Present:    true,
		CreatedAt:  t.CreatedAt,
		CreatedBy:  t.CreatedByUsername,
		LastUsedAt: t.LastUsedAt,
	}
}

// droplistRouterStatus is one device's drift against the drop list
// (issue #1225): how many of the store's own entries its last pushed
// address-list snapshot actually holds, alongside Total (the store's
// own entry count, the same for every device) so a caller can render
// "N of M" without a second request. Held/Total is a comparison against
// what the device says it has, not what the store has just resolved to
// be missing -- a device that has never pushed one at all is left out
// of the list entirely (see handleDroplistList), rather than reported
// here with a zero that would read as "confirmed empty".
type droplistRouterStatus struct {
	Device      string    `json:"device"`
	Held        int       `json:"held"`
	Total       int       `json:"total"`
	ConfirmedAt time.Time `json:"confirmedAt"`
}

type droplistListResponse struct {
	ListName string                  `json:"listName"`
	Entries  []droplistEntryResponse `json:"entries"`
	Key      droplistKeyStatus       `json:"key"`
	// Routers is one entry per device that has ever pushed an
	// address-list snapshot -- omitted (not zero-length) when none has,
	// same as every other router-derived slice in this API.
	Routers []droplistRouterStatus `json:"routers,omitempty"`
	// OwnRangesKnown mirrors Store.OwnRangesKnown(): whether Add's
	// own-range check has anything to check a candidate against at all,
	// surfaced here too so the Settings group can tell the operator the
	// check is still unproven even before they try adding anything (see
	// ownRangesUnknownWarning).
	OwnRangesKnown bool `json:"ownRangesKnown"`
	// Setup is the four RouterOS commands rendered for the address a
	// router would reach mikroview on -- the "address" query parameter,
	// falling back to the request's own Host. Key is always the literal
	// placeholder "<DROP-LIST-KEY>" here: the real value is never shown
	// on this route, only once, on the mint response (droplistKeyCreateResponse.Scheduler).
	Setup droplist.Setup `json:"setup"`
}

// droplistSetupKeyPlaceholder stands in for the real droplist-pull key
// on GET /api/droplist's rendered Setup.Scheduler -- that key is shown
// exactly once, on the mint response, never here.
const droplistSetupKeyPlaceholder = "<DROP-LIST-KEY>"

// droplistSetupAddress is the host[:port] Setup's rendered commands
// tell a router to fetch from: the caller-supplied "address" query
// parameter (the Settings group lets an operator override it, since
// mikroview cannot know which of its own names or addresses a given
// router can actually reach), falling back to the request's own Host
// header -- a reasonable default for the common case of one mikroview
// reachable at the address the admin is browsing it from right now.
func droplistSetupAddress(r *http.Request) string {
	return droplistAddressOr(r.URL.Query().Get("address"), r.Host)
}

// droplistAddressOr returns supplied when it is a plausible host[:port]
// and fallback otherwise. The rendered commands are pasted into a
// RouterOS terminal by hand, and QuoteScriptString cannot escape a line
// break: an address carrying one would let a second command ride into
// the paste (stage-3 security review). Rather than escape, refuse:
// only letters, digits, `.`, `-`, `:`, `[` and `]` (a bracketed IPv6
// literal) ever appear in a real host[:port], so anything else falls
// back to the Host header, which Go has already vetted.
func droplistAddressOr(supplied, fallback string) string {
	if supplied == "" || len(supplied) > 253 {
		return fallback
	}
	for _, c := range supplied {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return fallback
		}
	}
	return supplied
}

// handleDroplistList is the admin-only read backing the Settings group
// (#1224/#1225): every entry, the pull key's own status, each known
// router's drift against the list, whether the router's own ranges are
// known at all, and the setup commands to paste onto a router.
func (s *Server) handleDroplistList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	entries := s.Droplist.List()
	out := make([]droplistEntryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, toDroplistEntryResponse(e))
	}

	var routers []droplistRouterStatus
	if s.RouterState != nil {
		for _, device := range s.RouterState.Devices() {
			snapshot, updatedAt, ok := s.RouterState.AddressLists(device)
			if !ok {
				continue
			}
			routers = append(routers, droplistRouterStatus{
				Device:      device,
				Held:        droplist.Held(entries, snapshot),
				Total:       len(entries),
				ConfirmedAt: updatedAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, droplistListResponse{
		ListName:       droplist.ListName,
		Entries:        out,
		Key:            s.droplistKeyStatus(),
		Routers:        routers,
		OwnRangesKnown: s.Droplist.OwnRangesKnown(),
		Setup:          droplist.NewSetup(droplistSetupAddress(r), droplistSetupKeyPlaceholder),
	})
}

type droplistCreateRequest struct {
	CIDR   string `json:"cidr"`
	Reason string `json:"reason"`
	// FlagID is optional -- set when this entry is raised from a flag's
	// drawer rather than typed in from scratch. Checked against
	// s.Flags.Get before it ever reaches the store, so an entry can never
	// carry a flag id that names nothing.
	FlagID string `json:"flagID"`
}

// ownRangesUnknownWarning is handleDroplistCreate's Warning text for a
// 201 whose CIDR was not actually checked against the router's own
// ranges (security review): Store.Add stays fail-open (blocking would
// break first-time setup, before any router has ever reported in), but
// the operator should be told the check was skipped rather than assume
// it passed.
const ownRangesUnknownWarning = "No router has reported its addresses yet, so this range was not checked against the router's own ranges."

// droplistCreateResponse is handleDroplistCreate's 201 body: the stored
// entry, plus Warning when Store.OwnRangesKnown() was false at the time
// -- see ownRangesUnknownWarning.
type droplistCreateResponse struct {
	droplistEntryResponse
	Warning string `json:"warning,omitempty"`
}

// handleDroplistCreate adds one entry. Admin-only: this is an
// enforcement list, and #461 settled that a droplist entry is always
// operator-authored, never automatic.
func (s *Server) handleDroplistCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	var req droplistCreateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.FlagID != "" {
		if _, ok := s.Flags.Get(req.FlagID); !ok {
			http.Error(w, "no flag with that id", http.StatusBadRequest)
			return
		}
	}

	entry, err := s.Droplist.Add(auditActor(r), req.CIDR, req.Reason, req.FlagID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, droplist.ErrExists):
			status = http.StatusConflict
		case errors.Is(err, droplist.ErrInvalidCIDR),
			errors.Is(err, droplist.ErrNotIPv4),
			errors.Is(err, droplist.ErrTooBroad),
			errors.Is(err, droplist.ErrNotPublic),
			errors.Is(err, droplist.ErrRouterOwn),
			errors.Is(err, droplist.ErrBadText):
			status = http.StatusBadRequest
		}
		// err.Error() is safe to echo in every one of these cases: it
		// names a rule about the submitted range/reason (too broad, not
		// public, overlaps the router's own address, a duplicate), never
		// anything about other operators' entries.
		http.Error(w, err.Error(), status)
		return
	}
	resp := droplistCreateResponse{droplistEntryResponse: toDroplistEntryResponse(entry)}
	if !s.Droplist.OwnRangesKnown() {
		resp.Warning = ownRangesUnknownWarning
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleDroplistDelete removes one entry by its CIDR, taken from the
// path as a trailing wildcard ({cidr...}) since a CIDR's own "/" would
// otherwise be split across path segments.
func (s *Server) handleDroplistDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	cidr := r.PathValue("cidr")
	if err := s.Droplist.Remove(auditActor(r), cidr); err != nil {
		if errors.Is(err, droplist.ErrNotFound) {
			http.Error(w, "no entry for that range", http.StatusNotFound)
			return
		}
		http.Error(w, "unable to remove that entry", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// droplistKeyCreateRequest is handleDroplistKeyCreate's optional body:
// address to render Scheduler for, same meaning and same fallback to
// r.Host as GET /api/droplist's own "address" query parameter. Every
// field absent -- no body at all, an empty body, or the literal JSON
// null a bodyless POST from this package's own tests sends -- is the
// same as an empty Address, not a request error: this route took no
// body before #1225, and callers that still send none must keep
// working.
type droplistKeyCreateRequest struct {
	Address string `json:"address"`
}

type droplistKeyCreateResponse struct {
	// Key is the raw bearer value -- shown exactly once, the same
	// one-time contract every other token kind follows (see
	// auth.TokenStore.Create).
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"createdAt"`
	// Scheduler is the setup scheduler command (droplist.NewSetup) with
	// this response's own real key already filled in -- shown exactly
	// once, same one-time contract as Key: the list response only ever
	// renders this with the placeholder key.
	Scheduler string `json:"scheduler"`
}

// handleDroplistKeyCreate mints the droplist-pull key, rotating rather
// than accumulating: at most one is ever meant to exist, since every
// router fetches the same feed with the same credential, so minting
// again revokes whatever was there first in the same call. Admin-only,
// like every other token-issuing endpoint (see handleTokensCreate).
func (s *Server) handleDroplistKeyCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req droplistKeyCreateRequest
	if err := decodeJSONBody(w, r, &req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	address := droplistAddressOr(req.Address, r.Host)

	// Create first, revoke second. The other order is the remove-then-set
	// shape #1222 took out of the vault: a Create that failed after the
	// revoke would leave the router with no key at all, when the admin
	// asked for a new one.
	//
	// droplistKeyMintMu serializes the whole sequence (security review):
	// without it, two concurrent mint requests could each create a token
	// before either reached its revoke loop below, leaving two live keys
	// where at most one is ever meant to exist.
	s.droplistKeyMintMu.Lock()
	defer s.droplistKeyMintMu.Unlock()

	now := time.Now()
	raw, tok, err := s.Tokens.Create("droplist-pull", auth.TokenKindDroplistPull, "", userFromContext(r), now)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrTokenNotPersisted) {
			status = http.StatusServiceUnavailable
		}
		authLog.Warn(err.Error())
		http.Error(w, "unable to create the droplist pull key", status)
		return
	}
	replaced := false
	for _, t := range s.Tokens.ByKind(auth.TokenKindDroplistPull) {
		if t.ID == tok.ID {
			continue
		}
		if err := s.Tokens.Revoke(t.ID); err == nil {
			replaced = true
		}
	}
	detail := ""
	if replaced {
		detail = "replaced the previous key"
	}
	s.Audit.Record(auditActor(r), "droplist.key_minted", tok.Name, detail)
	// The raw key is shown exactly once, in this response -- no-store so
	// no cache along the way keeps a copy of it, the same header the pull
	// handler below already sets on the feed itself.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, droplistKeyCreateResponse{
		Key:       raw,
		CreatedAt: tok.CreatedAt,
		Scheduler: droplist.NewSetup(address, raw).Scheduler,
	})
}

// handleDroplistKeyDelete revokes the droplist-pull key -- every router
// that was fetching with it starts failing its next scheduled fetch and
// keeps serving whatever it imported last (see docs/configuration.md's
// "How the router fetches it").
func (s *Server) handleDroplistKeyDelete(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	toks := s.Tokens.ByKind(auth.TokenKindDroplistPull)
	if len(toks) == 0 {
		http.Error(w, "no droplist pull key exists", http.StatusNotFound)
		return
	}
	for _, t := range toks {
		if err := s.Tokens.Revoke(t.ID); err != nil {
			http.Error(w, "unable to revoke the droplist pull key", http.StatusInternalServerError)
			return
		}
	}
	s.Audit.Record(auditActor(r), "droplist.key_revoked", "droplist-pull", "")
	w.WriteHeader(http.StatusNoContent)
}

// handleDroplistPull serves the generated .rsc feed (issue #1224) --
// reachable only through droplistPullRoutes, the bearer mux a
// droplist-pull token dispatches to (see requireAuth in auth.go). No
// audit entry is recorded per pull: this is hit on the router's own
// fetch schedule and would flood the admin trail the same way an
// unqualified per-push ingest audit once did (see noteIngest's doc
// comment in ingest.go) -- the token's own LastUsedAt is the record of
// "is this still being fetched", already updated by requireAuth's
// Authenticate call before this handler ever runs (security review: the
// separate TokenStore.Touch this used to call here was redundant with
// that and has been removed).
func (s *Server) handleDroplistPull(w http.ResponseWriter, r *http.Request) {
	tok := droplistTokenFromContext(r)
	if tok == nil {
		// Unreachable in practice, same reasoning as
		// handleIngestRouterOS's identical guard: droplistPullRoutes is
		// only ever dispatched to from requireAuth's droplist-pull-token
		// branch, which always sets this first.
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now()
	if !s.IngestLimiter.Reserve(tok.ID, now) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(droplist.Script(s.Droplist.List(), now))
}
