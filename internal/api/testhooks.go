// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/suggest"
)

// This file is the whole of mikroview's test-hook surface: two routes
// that exist only when the process was started with MV_TEST_HOOKS=1, and
// that a shipped image therefore does not serve at all.
//
// Absent rather than refused, on purpose. A route registered and then
// gated is a route an attacker can find, probe and hold an opinion about;
// one that was never registered answers 404 exactly like a typo, and the
// difference between "off here" and "does not exist" stops being visible
// from the outside. It also means the switch is checkable by reading
// routes() rather than by trusting every handler in this file to have
// remembered its own guard.
//
// Both are admin tier as well. The flag is the real gate -- these can
// destroy an instance's whole record of what it has seen -- but a harness
// signs in as an admin anyway, so nothing is bought by making the second
// lock weaker than the locks on the endpoints they are more dangerous
// than.
//
// MV_TEST_HOOKS is read once, in main.go, with os.Getenv rather than
// through internal/config. It is not an operator setting: it does not
// appear in docs/configuration.md, has no YAML key and no flag, because
// an option an operator can find is an option somebody will turn on.

// testHookRoutes is appended to the route table by routes() when
// TestHooks is set -- see this file's own comment for why they are added
// rather than gated.
func (s *Server) testHookRoutes() []route {
	return []route{
		{http.MethodPost, "/api/test/clock", s.handleTestClockAdvance},
		{http.MethodPost, "/api/test/reset", s.handleTestReset},
	}
}

// testClockRequest is POST /api/test/clock's body: a Go duration string
// ("3m", "90s"), the amount to move the definitions clock forward by.
type testClockRequest struct {
	Advance string `json:"advance"`
}

// handleTestClockAdvance moves this process's definitions clock forward
// (see Server.now) and reports where it now stands.
//
// Forward only. The nightly watch fill is idempotent because a night is
// keyed by the instant its window opened and written once, so winding the
// clock back would leave a store holding nights from a future that has
// been un-happened -- a state no real deployment can reach and one no
// code here is written to survive. A zero advance is allowed and useful:
// it is how a scenario asks what the server thinks the time is before
// computing a window against it.
func (s *Server) handleTestClockAdvance(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	var req testClockRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	advance := time.Duration(0)
	if req.Advance != "" {
		d, err := time.ParseDuration(req.Advance)
		if err != nil {
			http.Error(w, fmt.Sprintf("advance %q is not a Go duration (e.g. \"3m\")", req.Advance), http.StatusBadRequest)
			return
		}
		if d < 0 {
			http.Error(w, "advance must not be negative -- the test clock only moves forward", http.StatusBadRequest)
			return
		}
		advance = d
	}

	offset := time.Duration(s.testClockOffset.Add(int64(advance)))
	writeJSON(w, http.StatusOK, map[string]any{
		"now":    s.now().UTC().Format(time.RFC3339Nano),
		"offset": offset.String(),
	})
}

// handleTestReset puts the instance back to what it looked like before
// any scenario ran, without signing anybody out.
//
// What it clears is everything derived from traffic and from what the
// routers pushed: the event ring and its counters, flags (including the
// expectations a verdict recorded), the match log, every pushed
// router-state table, every definition -- re-seeded straight afterwards
// with this binary's shipped catalogue, since an empty definitions store
// evaluates nothing -- and the watchlist suggestions generated from
// router state.
//
// What it keeps is identity and configuration: accounts, live sessions
// (the caller's own included, or this would sign the harness out
// mid-scenario), the device registry, ingest tokens, settings and the
// setup wizard's ledger. That list is what makes the reset usable between
// scenarios at all: a token live-env.sh issued keeps working, and the
// wizard stays shut instead of reappearing over every scenario's first
// screenshot.
//
// The hub is deliberately untouched. It holds connected clients, not a
// backlog -- there is no cached snapshot for a reconnecting browser to
// re-read, so there is nothing here to clear, and dropping the clients
// would disconnect the very page the scenario is driving.
func (s *Server) handleTestReset(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}

	s.Store.Reset()
	s.Flags.Reset()
	s.RouterState.Reset()
	definitions := s.Definitions.Reset()
	if s.Reseed != nil {
		if err := s.Reseed(); err != nil {
			// Not fatal to the request: the stores above are already
			// empty, so refusing now would leave the instance in a state
			// no retry improves. Say so loudly instead -- a scenario
			// running against a catalogue-less instance fails in ways
			// that look like anything but this.
			apiLog.Error(fmt.Sprintf("re-seeding the shipped definitions after a test reset failed: %v -- this instance now evaluates nothing", err))
		}
	}
	if s.MatchLog != nil {
		if err := s.MatchLog.Reset(); err != nil {
			http.Error(w, fmt.Sprintf("clearing the match log: %v", err), http.StatusInternalServerError)
			return
		}
	}
	if s.Suggest != nil {
		s.Suggest.Reset()
		s.Suggest.Sync(suggest.Generate(s.RouterState))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cleared":     []string{"events", "flags", "matches", "router-state", "definitions", "suggestions"},
		"kept":        []string{"accounts", "sessions", "devices", "tokens", "settings", "setup"},
		"definitions": definitions,
	})
}
