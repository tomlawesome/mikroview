// SPDX-License-Identifier: AGPL-3.0-only

package api

import "net/http"

// ConfigProblem is one thing wrong with the running configuration,
// surfaced so an operator sees it in the app rather than only in a
// startup log line that scrolled past weeks ago.
//
// Deliberately declared here rather than imported from internal/config:
// internal packages stay leaves (same reasoning internal/detect gives
// for duplicating config's thresholds rather than importing it). main.go
// converts across at wiring time.
type ConfigProblem struct {
	Code string `json:"code"`
	Key  string `json:"key"`
	// Message never contains a secret's value -- internal/config's
	// validation is responsible for that, and has a test asserting it.
	Message string `json:"message"`
	// Applied is the safe default substituted for a bad value. This is
	// what makes clamping defensible rather than a silent override, so
	// dropping it from the response would change the ethics of the
	// feature, not just its presentation.
	Applied     string `json:"applied,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// handleConfigProblems reports configuration problems found at startup.
//
// Admin-gated, and the gate is here rather than in the frontend. Hiding
// a banner client-side while the endpoint still answers everyone is an
// information disclosure with extra steps -- the payload sits in
// devtools regardless. A non-admin gets an empty list, not a list they
// are trusted not to look at.
//
// The disclosure matters because of what these messages contain: config
// key names, filesystem paths, the OIDC issuer URL, SMTP hosts, and a
// database endpoint once #131 lands. That is an infrastructure map, and
// the same recon value already judged worth protecting when -list-users
// was scoped and when the clear-permanent gate was fixed.
//
// callerIsAdminOrOpen, not a bare admin check: on a fresh install with
// no users yet there is no admin to be, and the operator standing in
// front of it is exactly who needs to know their store path isn't
// writable.
func (s *Server) handleConfigProblems(w http.ResponseWriter, r *http.Request) {
	if !s.callerIsAdminOrOpen(r) {
		// 403, not 200-with-an-empty-list. The empty-list form was tried
		// first on the reasoning that "problems exist" is itself
		// information -- but it makes the route-authorization matrix
		// unable to tell a correct refusal from a handler that has
		// started leaking, since both look like "allowed through". An
		// unambiguous refusal keeps that guard meaningful, and leaks
		// nothing a non-admin doesn't already know about their own role.
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	problems := s.ConfigProblems
	if problems == nil {
		problems = []ConfigProblem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"problems": problems})
}
