// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/netip"
	"time"
)

// uiAllowExemptPaths are the paths config.yaml's ui.allow never applies
// to (issue #1287). The allow list exists to keep strangers off the
// *browser* surface; a router is not a browser, and it does not read
// the config file it would have to be listed in.
//
// Every entry is router-facing, and every entry already has its own,
// narrower gate -- which is the test for being on this list:
//
//   - "/ca.crt" -- the CA certificate the router (or a browser, or a
//     reverse proxy) imports so it can verify MikroView at all. Served
//     from main.go's root mux, deliberately public and deliberately
//     unauthenticated: it is a public certificate, and refusing it
//     would break the very step that makes the rest of the setup
//     trustworthy.
//   - "/api/ingest/routeros" -- the RouterOS state push. Needs an
//     ingest bearer token scoped to one device, and since #1281 the
//     request must also arrive from that device's own enrolled address.
//     That is a tighter allow list than ui.allow, maintained per
//     device, so applying ui.allow on top would only refuse routers the
//     operator has already named.
//   - "/api/ingest/router-backup" -- the sliced HTTPS backup push, on
//     the same ingest token and the same enrolled-address gate.
//   - "/api/droplist.rsc" -- the drop-list feed a router's scheduled
//     `/tool fetch` pulls (#1224). Bearer-only, on its own single-route
//     mux, and the whole blast radius of that key is this one file.
//   - "/api/healthz" -- the liveness probe. The container's own
//     HEALTHCHECK (main.go's runHealthcheck) fetches it from loopback,
//     and an orchestrator's readiness probe fetches it from wherever
//     the orchestrator lives; neither is a browser, and an operator who
//     listed only their workstation would otherwise have Docker mark
//     the container unhealthy and restart it on a loop. It was already
//     the one endpoint answering with no auth and no session
//     (server.go, Version), so exempting it discloses nothing new.
//
// The enrolment flow is not on this list because none of it is served
// over HTTP: a router enrols by logging the marker line
// `mikroview-enrol <token>` to the syslog listener
// (internal/routeros.SyslogCommands, internal/device.TryEnrol), which
// never passes through this middleware. Its only HTTP step is fetching
// /ca.crt, already exempt above. The endpoints that *mint* and rebind
// an enrolment token are admin actions taken from a browser, so they
// are governed by ui.allow like every other admin screen -- exempting
// them would hand the setting's whole point away.
//
// TestUIAllowExemptsEveryRouterFacingRoute holds this list against the
// bearer muxes it mirrors, so a new router-facing route cannot be added
// without deciding about it here.
var uiAllowExemptPaths = map[string]bool{
	"/ca.crt":                   true,
	"/api/ingest/routeros":      true,
	"/api/ingest/router-backup": true,
	"/api/droplist.rsc":         true,
	"/api/healthz":              true,
}

// uiAllowAuditInterval is how long one refused address may keep being
// refused before it is worth another audit row. A record that this
// address is being turned away, not a record of every attempt.
const uiAllowAuditInterval = time.Hour

// uiAllowAuditMaxAddresses caps how many distinct addresses the
// throttle remembers at once, and so how many refusal rows an hour can
// produce.
//
// This is the one place noteUIRefusal has to go further than the
// noteIngest throttle it copies. noteIngest's key is (device, kind):
// devices come from admin-issued tokens and kinds from a fixed set, so
// both factors are outside an attacker's control. The key here is a
// client address -- and behind a declared trusted proxy that address
// comes from a forwarding header, which is text the client chose. A
// caller varying it per request would otherwise mint a fresh audit row
// every time and roll the whole admin trail, which is the exact failure
// #285 fixed for ingest.
const uiAllowAuditMaxAddresses = 1024

// RestrictToAllowList is issue #1287's gate: it refuses a request from
// any address config.yaml's ui.allow does not list.
//
// Wrapped around the whole root mux in main.go rather than around
// Routes(), for two reasons. The static UI is served from that mux, and
// a refusal has to be a plain 403 -- never the login page, which would
// invite the caller to guess at credentials on a screen they are not
// allowed to see at all. And /ca.crt, one of the exempt paths, is
// registered on the root mux too, so a gate below it could not exempt
// it.
//
// Nothing here touches CSRF: the address list is not a replacement for
// it, because a cross-site request rides the admin's own browser at the
// admin's own -- allowed -- address.
func (s *Server) RestrictToAllowList(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty means everyone, so a deployment that never sets the key
		// behaves exactly as it did before it existed. Checked per
		// request rather than once at wrap time so the middleware's
		// behaviour does not depend on the order main.go builds things
		// in.
		if len(s.UIAllow) == 0 || uiAllowExemptPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// ClientIP, not the peer address: it already honours
		// listen.trustedProxies, walking the forwarded chain only for a
		// connection that came from a declared proxy and falling back
		// to the observed peer otherwise (clientip.go). Re-reading the
		// forwarding header here would be a second, unverified answer
		// to a question that already has one.
		ip := s.ClientIP(r)
		if s.uiAllowed(ip) {
			next.ServeHTTP(w, r)
			return
		}

		if s.noteUIRefusal(ip, time.Now()) && s.Audit != nil {
			s.Audit.Record(auditActorServer, "ui.address_refused", ip,
				"ui.allow does not list this address")
		}
		// Plain text, and nothing about what is behind the wall.
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// uiAllowed reports whether addr is in ui.allow. An address that will
// not parse is refused: clientIP only ever returns something it parsed
// or the raw RemoteAddr, so the unparseable case is a peer address Go
// itself could not split -- not something to admit on a guess.
func (s *Server) uiAllowed(addr string) bool {
	parsed, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	parsed = parsed.Unmap()
	for _, p := range s.UIAllow {
		if p.Contains(parsed) {
			return true
		}
	}
	return false
}

// noteUIRefusal reports whether this refusal is worth an audit row --
// the same throttle shape as noteIngest, for the same reason: the 403
// is the enforcement, the audit row is only the record, and a record
// that pushes every other entry out of a FIFO-pruned trail is worse
// than a missing one.
//
// One row per address per uiAllowAuditInterval, and at most
// uiAllowAuditMaxAddresses distinct addresses tracked at a time (see
// that constant for why this needs a cap where noteIngest does not).
// At the cap, expired entries are dropped first; if the map is still
// full, the refusal goes unrecorded rather than displacing the trail.
func (s *Server) noteUIRefusal(addr string, now time.Time) bool {
	s.uiAllowAuditMu.Lock()
	defer s.uiAllowAuditMu.Unlock()
	if s.uiAllowAudit == nil {
		s.uiAllowAudit = make(map[string]time.Time)
	}

	if last, seen := s.uiAllowAudit[addr]; seen {
		if now.Sub(last) < uiAllowAuditInterval {
			return false
		}
		s.uiAllowAudit[addr] = now
		return true
	}

	if len(s.uiAllowAudit) >= uiAllowAuditMaxAddresses {
		for k, t := range s.uiAllowAudit {
			if now.Sub(t) >= uiAllowAuditInterval {
				delete(s.uiAllowAudit, k)
			}
		}
		if len(s.uiAllowAudit) >= uiAllowAuditMaxAddresses {
			return false
		}
	}
	s.uiAllowAudit[addr] = now
	return true
}
