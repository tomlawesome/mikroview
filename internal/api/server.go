// SPDX-License-Identifier: AGPL-3.0-only

// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/coverage"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/oidc"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routerstate"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/setup"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/suggest"
)

type Server struct {
	Store      *store.Store
	Devices    *device.Registry
	Hub        *hub.Hub
	Reputation *reputation.Client
	// MACRegistry is the persisted MAC-address first/last-seen history
	// (internal/device.MACRegistry) that already backs the new-device
	// detector -- read-only here, for GET /api/devices/macs (issue #675):
	// the Entities page's named-things table joins a host entity (keyed
	// on IP) against this by MACEntry.LastIP to show its MAC and how long
	// mikroview has known it, without inventing a second persisted
	// per-host store. Nil-guarded in handleDeviceMACs like Reputation/
	// NetClass above -- a Server built without one (an older test) simply
	// answers an empty list rather than panicking.
	MACRegistry *device.MACRegistry
	// NetClass attributes an IP to a Tor exit / VPN / datacenter /
	// privacy relay for the manual lookup popover (issue #114). Nil means
	// no sources were enabled, and every use is nil-guarded -- the same
	// nil-means-disabled convention as Reputation. Deliberately display-
	// only: it is read in handleIPLookup and nowhere near flag scoring.
	NetClass *netclass.Classifier
	Flags    *flags.Store
	// Definitions is the one document holding every definition the engine
	// evaluates -- shipped detectors, the operator's expectations, and
	// anything a builder UI authors from scratch (issue #404). It backs
	// the whole /api/definitions surface (issue #407), which replaced
	// /api/detectors and /api/watchlist/entries wholesale: a definition's
	// envelope is where enabled, scope and params live, so there is one
	// answer to "is this on" rather than two documents that can disagree.
	// Always non-nil (engine.OpenDefinitionsStore("") returns a usable,
	// empty, unpersisted store), same always-usable convention as Flags
	// above.
	Definitions *engine.DefinitionsStore
	// Entities is the persisted, admin-manageable (type, key) -> label/
	// tags store backing GET/POST/DELETE /api/entities (issue #107) --
	// the shared foundation for a future mail-sender allowlist and
	// UI-managed IP/port/rule aliasing. Always non-nil (internal/entities.
	// Open("") returns a usable, empty, unpersisted store), same
	// always-usable convention as Flags/Definitions above.
	Entities *entities.Store
	// Naming is the same resolver the ingest path uses to stamp friendly
	// names onto events (see internal/naming and main.go, which builds
	// one and hands it to both). Held here so GET /api/naming/provenance
	// can answer with the precedence that actually applies, rather than
	// re-deriving it from Entities alone and getting the router layer
	// wrong -- which for issue #413's editor is the whole question. The
	// zero value is usable and simply names nothing, so a test Server
	// that leaves it unset still works.
	Naming naming.Resolver
	// MatchLog answers GET /api/matches, the query surface
	// #243 section 3 exists for -- birdcage-style correlation by source
	// IP over a time range. Unlike every store field above, this can be
	// nil: internal/matchlog has no in-memory-only fallback (durability
	// is the entire reason it exists), so a boot-time failure to open it
	// leaves this nil rather than degrading to an unpersisted store that
	// would silently lose every match. Every handler that reads it must
	// nil-check first.
	MatchLog matchlog.Store
	// Learning answers a definition's live baseline warm-up state (issue
	// #639) -- purpose-named and narrow rather than a *engine.Engine
	// field, following #407's own definitions-API precedent of never
	// handing this package evaluation internals it does not need. Nil
	// like MatchLog is a valid, common state (most tests, and any Server
	// built before the engine exists): every reader treats a nil Learning
	// exactly like a definition this method reports false for, so the
	// "learning" field is simply omitted rather than requiring one.
	Learning interface {
		Learning(id string, now time.Time) (engine.LearningState, bool)
	}
	// Suggest is the persisted pool of watchlist entries suggested from
	// data RouterOS has already pushed (#243 slice 5) -- backing GET/POST
	// /api/suggestions/... (see suggest.go). Always non-nil
	// (internal/suggest.Open("") returns a usable, empty, unpersisted
	// store), same always-usable convention as Watchlist/Entities above.
	// Kept synced with RouterState in the background by
	// suggest.Store.RunPeriodicSync (see main.go), never by a handler in
	// this package -- see internal/suggest's own doc comment for why
	// there is deliberately no manual "refresh" endpoint.
	Suggest *suggest.Store
	// DefaultWatchPorts is what an accepted address-list suggestion
	// watches, since such a candidate deliberately carries no ports of
	// its own (#274 item 2): a rule scoping by address list says which
	// hosts matter, not which ports. The operator's own
	// flags.criticalPorts is the honest default -- the same set the
	// critical_port detector already treats as worth noticing -- and
	// the entry is editable the moment it exists.
	DefaultWatchPorts []int
	// Rules is the persisted, long-lived per-rule-label usage record
	// (issue #103's internal/rules.Store) -- exposed read-only via GET
	// /api/rules (issue #109) as the "discovered but unnamed rules"
	// source for the Entities admin panel: every rule label ever seen
	// firing, independent of the store's retention window, mirroring
	// device.Registry's own "auto-discovered, shown even before
	// configured/labeled" pattern. Always non-nil (internal/rules.
	// Open("") returns a usable, empty, unpersisted store), same
	// always-usable convention as Entities/Flags/Definitions above.
	Rules *rules.Store
	// Coverage is the persisted set of coverage-gap declarations (issue
	// #630/#392): an admin's on-record statement that a given boundary-
	// direction pair is intentionally, not accidentally, quiet. Backs
	// GET/PUT/DELETE /api/coverage/declarations (see coverage.go).
	// Always non-nil (internal/coverage.Open("") returns a usable,
	// empty, unpersisted store), same always-usable convention as
	// Entities/Flags/Definitions above.
	Coverage *coverage.Store
	// Audit is the persisted, admin-only accountability log of every
	// admin-privileged mutation (issue #112) -- who created a user,
	// changed a detector setting, upserted/deleted an entity, created or
	// revoked an API token, or removed a permanent flag exclusion (see
	// flags.go's handleExclusionRemove; the flag-clear handlers
	// deliberately don't record here -- see their own doc comments).
	// Deliberately separate from Flags: this is about actions taken *in*
	// mikroview, not behavior mikroview observes on the network. Always
	// non-nil (internal/audit.Open("") returns a usable, empty,
	// unpersisted store), same always-usable convention as Entities/
	// Flags/Definitions above.
	Audit *audit.Store
	// DeviceStaleAfter (issue #98) is how long a device's LastSeen may go
	// without updating before GET /api/devices reports it as "stale" --
	// same threshold detect.DeviceSilenceDetector uses to raise an actual
	// flag (see internal/detect/device_silence.go), duplicated here
	// purely so this read-time status computation doesn't need to import
	// internal/detect for one number. Zero means "not configured": every
	// device with at least one event is always reported "live".
	DeviceStaleAfter time.Duration
	StartTime        time.Time
	// Version is main.version (the build-time-stamped short commit SHA,
	// "dev" for a plain local build) -- passed in rather than read
	// directly since internal/api can't import main. Surfaced on
	// GET /api/healthz, the one endpoint reachable with no auth and no
	// session regardless of deployment state, so "which build am I
	// running" is checkable without any special access.
	Version string
	// ThirdPartyNotices is THIRD-PARTY-NOTICES.md, embedded in the
	// binary at build time (see notices.go) and served verbatim by
	// handleThirdPartyNotices. Every dependency compiled into this
	// binary ships under a licence (MIT, BSD-3-Clause, ISC,
	// Apache-2.0) requiring its copyright notice and licence text to
	// accompany a binary distribution -- serving it here is how a user
	// of a running instance receives them without having to go and find
	// the source separately.
	ThirdPartyNotices string
	// ConfigProblems are non-fatal configuration problems found at
	// startup, where a safe default was substituted for a bad value.
	// Surfaced to admins in the UI because a startup log line is seen
	// once, by whoever ran `docker compose up`, and never again -- which
	// is not good enough for a setting the operator believes is in
	// effect. See config_problems.go.
	ConfigProblems []ConfigProblem
	// Persistence reports which backend this deployment's persisted
	// stores (flags, definitions, watchlist entries, entities, tokens/
	// accounts -- internal/persist's own package doc) actually use right
	// now -- set once at boot from main.go's storage decision. See
	// persistence.go.
	Persistence PersistenceInfo

	// Auth/Sessions/LoginLimiter/SecureCookie: see auth.go. Auth is
	// always non-nil (internal/auth.Open("") returns a usable, empty,
	// unpersisted store) -- mikroview stays fully open as long as it has
	// zero users, exactly like every other request path today.
	Auth         *auth.Store
	Sessions     *auth.SessionStore
	LoginLimiter *auth.LoginLimiter
	SecureCookie bool

	// TrustedProxies/ClientIPHeader control how the login rate limiter
	// attributes a request to a source address when mikroview sits behind
	// a reverse proxy -- see clientip.go, and config.Listen's fields of
	// the same names for why the empty default ignores forwarding headers
	// rather than trusting them.
	TrustedProxies []netip.Prefix
	ClientIPHeader string

	// Tokens holds read-only API and ingest bearer tokens (issues #101,
	// #186) -- always non-nil (internal/auth.OpenTokenStore("") returns a
	// usable, empty, unpersisted store), same nil-never convention as
	// Auth above.
	Tokens *auth.TokenStore
	// IngestLimiter bounds how often one ingest token may call POST
	// /api/ingest/routeros (issue #186 step 3). Reuses auth.LoginLimiter
	// rather than a second rate-limiting primitive -- see handleIngest
	// RouterOS's doc comment for the threshold/window reasoning. Keyed by
	// token ID, never the raw token value, the same never-store-the-
	// secret convention LoginLimiter itself follows by keying on
	// username/IP rather than a password.
	IngestLimiter *auth.LoginLimiter
	// RouterState holds each device's most recent pushed state (issue
	// #186 step 4) -- written by handleIngestRouterOS, read by the
	// /api/routeros/{device}/... table endpoints. Always non-nil
	// (routerstate.New() needs no configuration); in-memory only, by
	// that package's design.
	RouterState *routerstate.Store

	// Setup holds what has been observed of each router's setup, for the
	// guided wizard (#320). Nil in tests that do not exercise it, which
	// handleSetupStatus tolerates.
	Setup *setup.Store
	// SetupInstance is the running configuration the wizard writes
	// commands from -- the address a router should be pointed at, and
	// whether the certificate covers it.
	SetupInstance SetupInstance

	// OIDC/OIDCState: see oidc.go. Both nil unless cfg.OIDC.IssuerURL was
	// set and provider discovery succeeded at startup -- every OIDC
	// handler checks for nil and 404s, so a misconfigured or absent OIDC
	// block never affects local auth, the same nil-means-disabled
	// convention Reputation already uses elsewhere on this struct.
	OIDC      *oidc.Client
	OIDCState *oidc.StateCodec
	// OIDCPolicy restricts which accounts at the issuer may sign in. The
	// zero value permits everyone the issuer vouches for, which is the
	// correct answer for a self-hosted IdP and refused at startup for a
	// multi-tenant one -- see internal/oidc.Policy and main.go.
	OIDCPolicy oidc.Policy

	// ingestAudit remembers, per (device, kind), when that combination
	// last produced an audit row and whether it succeeded, so a routine
	// push does not write one. See noteIngest for why -- unqualified
	// per-push auditing let one ingest token roll the whole admin audit
	// trail in about a day. Unexported and lazily built: it is internal
	// bookkeeping, not configuration, so a zero-valued Server (which
	// every test constructs) needs no extra setup.
	ingestAuditMu sync.Mutex
	ingestAudit   map[ingestAuditKey]ingestAuditState

	// definitionsEnabledScopeMu serializes handleDefinitionsUpdate's
	// read-merge-write of a definition's Enabled/Scope fields (issue
	// #494): those two are the only fields on Definition this handler
	// fills in from the existing stored value for whichever the request
	// left unset, and engine.DefinitionsStore.SetEnabledAndScope writes
	// both unconditionally, so it cannot tell a caller-supplied value
	// from a stale one read before the client-paced decodeJSONBody call.
	// Holding this for the fresh read through the write closes that
	// window for the one production caller of SetEnabledAndScope (this
	// handler); zero value is ready to use, same as ingestAuditMu above.
	definitionsEnabledScopeMu sync.Mutex

	// verdictWatchlistMu serializes the verdict handlers' compound work
	// (issue #641): an expected verdict writes an expectation into the
	// flags store *and* permitted destinations onto the device's inverted
	// watchlist entry, and undoing or re-judging it takes both back. Two
	// verdicts about the same device interleaving could let one's
	// promotion land inside the other's withdrawal, leaving the device
	// permitted somewhere no record still claims. Zero value is ready to
	// use, same as the two above.
	verdictWatchlistMu sync.Mutex
}

// route is one registered endpoint. Routes are declared as data rather
// than as direct mux.HandleFunc calls so the full set is enumerable at
// runtime -- http.ServeMux keeps its patterns in unexported fields, so
// without this there is no way for a test to ask "what endpoints exist?"
// and therefore no way to assert that every one of them has a
// deliberate access level. internal/api's authorization-matrix test
// depends on that enumeration; see authz_matrix_test.go for why (a real
// permission gap shipped precisely because nothing forced that question
// to be answered for a new route).
type route struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// routes returns every /api/* endpoint. Order is irrelevant to
// ServeMux's matching (it is longest-pattern-wins, not first-match), so
// these stay grouped by area for readability.
func (s *Server) routes() []route {
	return []route{
		{http.MethodGet, "/api/healthz", s.handleHealthz},
		{http.MethodGet, "/api/events", s.handleEvents},
		{http.MethodGet, "/api/devices", s.handleDevices},
		{http.MethodGet, "/api/devices/macs", s.handleDeviceMACs},
		{http.MethodGet, "/api/rules", s.handleRules},
		// The pushed rule/NAT tables (issue #186 step 4) -- session-gated
		// reads over RouterState, entirely separate from the push
		// endpoint itself, which lives on ingestRoutes' own mux and is
		// deliberately absent from this table.
		{http.MethodGet, "/api/routeros/{device}/rules", s.handleRouterOSRules},
		{http.MethodGet, "/api/routeros/{device}/nat", s.handleRouterOSNAT},
		{http.MethodGet, "/api/routeros/{device}/addresses", s.handleRouterOSAddresses},
		{http.MethodGet, "/api/stats", s.handleStats},
		// #644 round 21's top port/top talker table columns -- see
		// handleStatsTops' own doc comment for why this is a separate
		// route rather than a field on /api/stats above.
		{http.MethodGet, "/api/stats/tops", s.handleStatsTops},
		{http.MethodGet, "/api/ws", s.handleWS},
		{http.MethodGet, "/api/lookup/ip/{ip}", s.handleIPLookup},
		{http.MethodGet, "/api/flags", s.handleFlagsList},
		{http.MethodPost, "/api/flags/clear-all", s.handleFlagsClearAll},
		{http.MethodPost, "/api/flags/{id}/verdict", s.handleFlagsVerdict},
		// Not "/{id}/verdict" (which would mirror the POST above): that
		// shape is structurally ambiguous against any literal-then-
		// wildcard sibling under /api/flags/ in Go's net/http.ServeMux
		// (both would be 4 segments, one wildcard-then-literal and one
		// literal-then-wildcard, so a path matching both makes neither
		// pattern more specific and ServeMux panics at registration
		// rather than pick one -- as it did against the exclusions
		// DELETE this table used to carry). "verdict/{id}" instead puts
		// the wildcard last, the same shape every other DELETE-by-id
		// route in this table already uses (definitions/{id},
		// tokens/{id}, users/{id}).
		{http.MethodDelete, "/api/flags/verdict/{id}", s.handleFlagsVerdictUndo},

		// The one definitions surface (issue #407), replacing
		// /api/detectors and /api/watchlist/entries wholesale. A shipped
		// detector and a watchlist expectation are the same thing to the
		// engine, so they are the same thing here.
		{http.MethodGet, "/api/definitions", s.handleDefinitionsList},
		{http.MethodPost, "/api/definitions", s.handleDefinitionsCreate},
		// Registered before the {id} pattern purely for readability --
		// ServeMux matches longest-pattern-wins, so a literal "schema"
		// segment always beats the wildcard regardless of order here.
		{http.MethodGet, "/api/definitions/schema", s.handleDefinitionsSchema},
		{http.MethodGet, "/api/definitions/{id}", s.handleDefinitionsGet},
		{http.MethodPut, "/api/definitions/{id}", s.handleDefinitionsUpdate},
		{http.MethodDelete, "/api/definitions/{id}", s.handleDefinitionsDelete},
		{http.MethodPost, "/api/definitions/{id}/clone", s.handleDefinitionsClone},
		{http.MethodPost, "/api/definitions/{id}/reset", s.handleDefinitionsReset},
		{http.MethodPost, "/api/definitions/{id}/replay", s.handleDefinitionsReplay},
		{http.MethodPost, "/api/definitions/{id}/promote", s.handleDefinitionsPromote},
		{http.MethodPost, "/api/definitions/{id}/observing", s.handleDefinitionsSetObserving},

		// Where the name shown for one row token comes from, and
		// whether labelling it here would change anything (issue
		// #413). Sits beside /api/entities because it is the question
		// that has to be answered before writing one.
		{http.MethodGet, "/api/naming/provenance", s.handleNameProvenance},

		{http.MethodGet, "/api/entities", s.handleEntitiesList},
		{http.MethodPost, "/api/entities", s.handleEntitiesUpsert},
		{http.MethodDelete, "/api/entities", s.handleEntitiesDelete},

		// Coverage-gap declarations (issue #630/#392) -- see coverage.go.
		{http.MethodGet, "/api/coverage/declarations", s.handleCoverageList},
		{http.MethodPut, "/api/coverage/declarations/{key}", s.handleCoveragePut},
		{http.MethodDelete, "/api/coverage/declarations/{key}", s.handleCoverageDelete},

		// The match log query -- a read over evidence already collected,
		// and the one thing on the retired /api/watchlist prefix the
		// engine does not replace. Renamed with the noun rather than left
		// behind on a prefix nothing else uses; see handleMatchesQuery.
		{http.MethodGet, "/api/matches", s.handleMatchesQuery},

		// Suggested watchlist entries (#243 slice 5), generated in the
		// background from pushed router data -- see suggest.go.
		{http.MethodGet, "/api/suggestions", s.handleSuggestionsList},
		{http.MethodPost, "/api/suggestions/reset", s.handleSuggestionsReset},
		{http.MethodPost, "/api/suggestions/{id}/accept", s.handleSuggestionsAccept},
		{http.MethodPost, "/api/suggestions/{id}/hide", s.handleSuggestionsHide},
		{http.MethodPost, "/api/suggestions/{id}/unhide", s.handleSuggestionsUnhide},

		{http.MethodGet, "/api/third-party-notices", s.handleThirdPartyNotices},

		{http.MethodGet, "/api/audit", s.handleAuditList},

		// The guided setup wizard's view of what has actually landed
		// (#320) -- open to any signed-in user, see handleSetupStatus.
		{http.MethodGet, "/api/setup/status", s.handleSetupStatus},
		// The claim ledger's own marks (#487): a step skipped or forced
		// past. Admin-only, matching the modal it is written from.
		{http.MethodPost, "/api/setup/mark", s.handleSetupMark},
		{http.MethodGet, "/api/config/problems", s.handleConfigProblems},
		{http.MethodGet, "/api/persistence", s.handlePersistence},

		{http.MethodGet, "/api/auth/session", s.handleAuthSession},
		{http.MethodPost, "/api/auth/register", s.handleAuthRegister},
		{http.MethodPost, "/api/auth/login", s.handleAuthLogin},
		{http.MethodPost, "/api/auth/password", s.handleAuthChangePassword},
		{http.MethodPost, "/api/auth/logout", s.handleAuthLogout},
		{http.MethodPost, "/api/auth/logout-all", s.handleAuthLogoutAll},
		{http.MethodPost, "/api/auth/users", s.handleAuthCreateUser},
		{http.MethodGet, "/api/auth/users", s.handleAuthListUsers},
		{http.MethodDelete, "/api/auth/users/{id}", s.handleAuthDeleteUser},

		// Admin-only token management (issue #101) -- gated the same way
		// POST /api/auth/users is (see handleTokensCreate/
		// handleTokensList/handleTokensRevoke). The tokens themselves
		// grant access through a completely separate, deliberately
		// minimal mux -- see requireAuth's bearer-token branch in
		// auth.go -- not through anything registered here.
		{http.MethodPost, "/api/tokens", s.handleTokensCreate},
		{http.MethodGet, "/api/tokens", s.handleTokensList},
		{http.MethodDelete, "/api/tokens/{id}", s.handleTokensRevoke},

		{http.MethodGet, "/api/auth/oidc/login", s.handleOIDCLogin},
		{http.MethodPost, "/api/auth/oidc/link", s.handleOIDCLinkStart},
		{http.MethodGet, "/api/auth/oidc/callback", s.handleOIDCCallback},
	}
}

// Routes builds the /api/* handler. Static frontend asset serving is
// mounted separately once the embedded build exists. requireAuth wraps
// every route except the ones that must work before a session exists
// (healthz, and the specific auth endpoints that are unauthenticated by
// nature -- see auth.go's exemptPaths).
func (s *Server) Routes() http.Handler {
	return s.requireAuth(s.mux())
}

// mux is Routes without the authentication gate in front of it.
//
// It exists for the tests that exercise a handler's own behaviour rather
// than who is allowed to reach it. Those used to get an ungated API by
// standing the fixture up with authentication disabled, which is not a
// state that exists any more. Reaching for the inner mux says what those
// tests actually mean, and keeps the gate itself covered in one place --
// auth_test.go and the authzMatrix guard, which both mount Routes.
func (s *Server) mux() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.HandleFunc(r.method+" "+r.path, r.handler)
	}
	return mux
}
