// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
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

type droplistListResponse struct {
	ListName string                  `json:"listName"`
	Entries  []droplistEntryResponse `json:"entries"`
	Key      droplistKeyStatus       `json:"key"`
}

// handleDroplistList is the admin-only read backing the Settings group
// (#1224): every entry, plus the pull key's own status.
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
	writeJSON(w, http.StatusOK, droplistListResponse{
		ListName: droplist.ListName,
		Entries:  out,
		Key:      s.droplistKeyStatus(),
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
	writeJSON(w, http.StatusCreated, toDroplistEntryResponse(entry))
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

type droplistKeyCreateResponse struct {
	// Key is the raw bearer value -- shown exactly once, the same
	// one-time contract every other token kind follows (see
	// auth.TokenStore.Create).
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"createdAt"`
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

	// Create first, revoke second. The other order is the remove-then-set
	// shape #1222 took out of the vault: a Create that failed after the
	// revoke would leave the router with no key at all, when the admin
	// asked for a new one.
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
	writeJSON(w, http.StatusCreated, droplistKeyCreateResponse{Key: raw, CreatedAt: tok.CreatedAt})
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
// comment in ingest.go) -- the token's own LastUsedAt, touched below, is
// the record of "is this still being fetched".
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
	s.Tokens.Touch(tok.ID, now)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(droplist.Script(s.Droplist.List(), now))
}
