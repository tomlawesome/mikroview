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
}

// tokenResponse mirrors auth.Token but never carries HashedValue --
// used for both the list endpoint and (with Value additionally set) the
// one-time creation response.
type tokenResponse struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
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

	raw, tok, err := s.Tokens.Create(req.Name, time.Now())
	if err != nil {
		status := http.StatusInternalServerError
		if err == auth.ErrTokenNotPersisted {
			status = http.StatusServiceUnavailable
		}
		authLog.Warn(err.Error())
		http.Error(w, "unable to create token", status)
		return
	}

	s.Audit.Record(auditActor(r), "token.create", tok.Name, "id="+tok.ID)
	writeJSON(w, http.StatusCreated, tokenResponse{
		ID:        tok.ID,
		Name:      tok.Name,
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
