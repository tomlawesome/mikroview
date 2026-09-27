// SPDX-License-Identifier: AGPL-3.0-only

package api

// Setup-only mode (#1347, owner's "13a"): what MikroView serves when its
// config is refused. Before this a refused config meant the container
// exited, and the only way to find out why was the log; now it starts
// just enough to sign in and fix the file -- the UI shell, /api/auth/*,
// /api/healthz and the config editor -- and nothing else. No ingest, no
// syslog, no stores beyond the accounts, and every other route answers
// 503 saying why.
//
// A separate route table rather than a flag threaded through the normal
// one: the normal table's handlers assume stores and an engine that do
// not exist here, and a route that is simply not registered cannot be
// reached by mistake.

import (
	"net/http"
	"strings"
	"time"
)

// SetupOnlyMessage is what every route outside setup-only mode's small
// set answers, with a 503.
const SetupOnlyMessage = "MikroView is in setup-only mode: fix the config and restart"

// setupOnlyRoutes is setup-only mode's whole route table: the health
// check, every /api/auth/* route (sign-in, including SSO when its own
// settings validated), and the config editor. Every one of these is a
// row of authzMatrix at the same pattern, and enforces the same access
// there -- TestSetupOnlyRoutesAreInTheAuthorizationMatrix holds that.
func (s *Server) setupOnlyRoutes() []route {
	rs := []route{{http.MethodGet, "/api/healthz", s.handleSetupOnlyHealthz}}
	for _, r := range s.apiRoutes() {
		if strings.HasPrefix(r.path, "/api/auth/") {
			rs = append(rs, r)
		}
	}
	return append(rs, s.configEditorRoutes()...)
}

// SetupOnlyRoutes builds the /api/ handler for setup-only mode. The
// refusal sits in front of authentication, so every other route answers
// 503 to everyone, signed in or not: the question "why is nothing
// working" gets its answer before "who are you".
func (s *Server) SetupOnlyRoutes() http.Handler {
	inner := http.NewServeMux()
	for _, r := range s.setupOnlyRoutes() {
		inner.HandleFunc(r.method+" "+r.path, r.handler)
	}
	gated := s.requireAuth(inner)

	outer := http.NewServeMux()
	seen := map[string]bool{}
	for _, r := range s.setupOnlyRoutes() {
		if !seen[r.path] {
			seen[r.path] = true
			outer.Handle(r.path, gated)
		}
	}
	outer.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": SetupOnlyMessage})
	})
	return outer
}

// handleSetupOnlyHealthz is /api/healthz in setup-only mode: the same
// liveness answer, plus "mode":"setup-only" so the UI knows to go
// straight to the editor with a banner saying why, and a container
// health check still sees a process that is up and serving.
func (s *Server) handleSetupOnlyHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"mode":          "setup-only",
		"time":          time.Now().UTC(),
		"uptime":        time.Since(s.StartTime).String(),
		"uptimeSeconds": int64(time.Since(s.StartTime).Seconds()),
		"version":       s.Version,
	})
}
