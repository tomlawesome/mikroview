// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// callerIsAdmin (shared, defined in auth.go) gates every token-
// management endpoint here the same way it gates handleAuthCreateUser
// and internal/entities' admin-only CRUD.
type createTokenRequest struct {
	Name string `json:"name"`
	// Kind is optional, and omitting it means a read-only API token.
	// The default is the less privileged of the two: an ingest token
	// has to be asked for by name, so a caller can never end up with
	// one by leaving a field out.
	Kind string `json:"kind"`
	// Device is required for kind "ingest" and rejected otherwise --
	// see auth.Token.Device for why an unscoped ingest token is not an
	// option.
	Device string `json:"device"`
}

// tokenResponse mirrors auth.Token but never carries HashedValue --
// used for both the list endpoint and (with Value additionally set) the
// one-time creation response.
type tokenResponse struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Kind       auth.TokenKind `json:"kind"`
	Device     string         `json:"device,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	LastUsedAt time.Time      `json:"lastUsedAt,omitzero"`
	// Value is the raw bearer token -- set only by handleTokensCreate's
	// response, never by handleTokensList. This is the one and only time
	// it is ever transmitted; it cannot be recovered afterward, only
	// reissued as a brand new token (see auth.TokenStore.Create's doc
	// comment).
	Value string `json:"value,omitempty"`
}

// handleTokensCreate issues a new read-only bearer API token (issue
// #101) -- admin-only, mirroring handleAuthCreateUser's gate. The raw
// value is returned exactly once, in this response; the store itself
// never retains it, only its SHA-256 hash (see auth.TokenStore).
func (s *Server) handleTokensCreate(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req createTokenRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Name) == 0 {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	kind := auth.TokenKind(req.Kind)
	if req.Kind == "" {
		kind = auth.TokenKindAPI
	}

	raw, tok, err := s.Tokens.Create(req.Name, kind, req.Device, userFromContext(r), time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		switch err {
		case auth.ErrTokenNotPersisted:
			status = http.StatusServiceUnavailable
		case auth.ErrTokenKindInvalid, auth.ErrTokenDeviceRequired, auth.ErrTokenDeviceNotAllowed, auth.ErrTokenDeviceInvalid:
			// The caller's request is wrong, not the deployment's state,
			// and the message is safe to hand back: it names a field, not
			// anything about existing tokens.
			authLog.Warn(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		authLog.Warn(err.Error())
		http.Error(w, "unable to create token", status)
		return
	}

	// The audit entry records kind and device because "an ingest token
	// was issued for router X" is a materially different event from "a
	// read-everything token was issued", and an audit log that renders
	// them identically cannot answer the question afterwards.
	detail := "id=" + tok.ID + " kind=" + string(tok.Kind)
	if tok.Device != "" {
		detail += " device=" + tok.Device
	}
	s.Audit.Record(auditActor(r), "token.create", tok.Name, detail)
	writeJSON(w, http.StatusCreated, tokenResponse{
		ID:        tok.ID,
		Name:      tok.Name,
		Kind:      tok.Kind,
		Device:    tok.Device,
		CreatedAt: tok.CreatedAt,
		Value:     raw,
	})
}

// handleTokensList returns every token's metadata -- name/created/last-
// used, never the hash or raw value -- admin-only.
func (s *Server) handleTokensList(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	tokens := s.Tokens.List()
	out := make([]tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenResponse{
			ID:         t.ID,
			Name:       t.Name,
			Kind:       t.Kind,
			Device:     t.Device,
			CreatedAt:  t.CreatedAt,
			LastUsedAt: t.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// handleTokensRevoke permanently deletes a token by ID -- admin-only.
// Idempotent-feeling from the caller's perspective (a 404 either means
// "already revoked" or "never existed," indistinguishable and both
// fine, same as internal/auth.Store.Revoke's error handling elsewhere).
func (s *Server) handleTokensRevoke(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	if err := s.Tokens.Revoke(id); err != nil {
		http.Error(w, "no such token", http.StatusNotFound)
		return
	}
	s.Audit.Record(auditActor(r), "token.revoke", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}
