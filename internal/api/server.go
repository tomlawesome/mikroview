// SPDX-License-Identifier: AGPL-3.0-only

// Package api exposes the HTTP/WebSocket surface: historical event
// queries, device/stat snapshots, and the live-tail WebSocket feed.
package api

import (
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupslice"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/baseline"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/coverage"
	"github.com/tomlawesome/mikroview/internal/decommission"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/droplist"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/hosts"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/oidc"
	"github.com/tomlawesome/mikroview/internal/oui"
	"github.com/tomlawesome/mikroview/internal/prefs"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routerstate"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/seen"
	"github.com/tomlawesome/mikroview/internal/settings"
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
	// OUI turns a hardware address into the organisation IEEE assigned
	// its prefix to, for the device dossier (issue #410). Nil means the
	// feed is switched off, and every use is nil-guarded -- the same
	// nil-means-disabled convention as NetClass above. The package's own
	// methods are nil-safe too, so a disabled feed answers "no vendor
	// data" rather than needing a check at each call site.
	OUI *oui.Registry
	// History is the on-disk event history a replay reads before it
	// reaches the ring (#856). Nil means memory-only, which is the
	// default and a first-class mode, not a missing dependency -- same
	// nil-means-disabled convention as NetClass and Reputation above.
	// It is read for replays and nowhere else; ingest writes to it
	// through main, not through here.
	History engine.RetainedDays
	// HistoryControl is that same history's switch and caps, as
	// /api/settings/history reads and writes them (#910). Nil in tests
	// that do not exercise it and on any instance built without one, in
	// which case both endpoints refuse rather than pretend -- same
	// nil-means-unavailable convention as Settings above.
	//
	// Separate from History rather than folded into it because they are
	// different questions asked by different callers: History is read
	// on the replay path and must stay the narrow "give me days" shape
	// internal/engine defines, while this one is a settings surface.
	HistoryControl HistoryControl
	Flags          *flags.Store
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
	// Decommissions holds every retiring network segment (#460): the
	// range, its clean-window clock, and the names the router last knew
	// inside it. Its own store rather than a corner of Definitions,
	// because a decommission watch is not a stored Definition -- see
	// engine.DecommissionWatches for why its logic has to be Go. nil is
	// valid and disables the whole surface with a 503 rather than a
	// panic, matching the optional-store convention below.
	Decommissions *decommission.Store
	// DecommissionCleanWindow is the configured default offered when a
	// watch is created (config engine.decommissionCleanWindow). Each
	// watch keeps the window it was created with, so changing this never
	// retunes a decommission already under way.
	DecommissionCleanWindow time.Duration
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
	// Evaluation reports how far behind the engine's cursor is, how old
	// the oldest thing it has not checked yet is, and how many events the
	// store evicted before it reached them at all (#1109). Narrow and
	// purpose-named for the same reason as Learning above. Nil is valid
	// and common (tests, and any Server built before the engine exists),
	// and the field is then omitted rather than reported as zeros: all
	// zeros is a real answer -- "caught up, nothing missed" -- and a
	// Server with no engine cannot honestly give it.
	Evaluation interface {
		Lag() (behind uint64, behindSeconds float64, outrun uint64)
		// Forget is for handleTestReset only: the store was emptied on
		// purpose, so the engine starts level with it and carries no
		// outrun from before -- see engine.Engine.Forget.
		Forget()
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
	// Hosts is the host presence register (issue #1016): every host the
	// syslog feed has shown, so the map can grey out one that has gone
	// quiet instead of silently dropping it, plus whatever an operator
	// has said about a quiet host. Backs GET /api/hosts and the mark
	// endpoints (see hosts.go). Always non-nil (internal/hosts.Open("")
	// returns a usable, empty, unpersisted register), same
	// always-usable convention as Coverage above.
	Hosts *hosts.Register
	// Baseline is the line register (issue #1016, round 49): which
	// source/destination/port/protocol lines the feed has shown and on
	// which of the last few days, so the map can draw a line that is off
	// the established pattern brightly and let every settled one recede.
	// Backs GET /api/baseline/off and the expected endpoints (see
	// baseline.go). Always non-nil (internal/baseline.Open("", cfg)
	// returns a usable, empty, unpersisted register), same always-usable
	// convention as Hosts above.
	Baseline *baseline.Register
	// SeenValues is the register of values the feed has actually shown
	// for the two filter fields with no option list anywhere else --
	// protocol and interface (issue #1226). Backs GET /api/seen-values
	// (see seen.go), which is what turns the stream's Proto and
	// Interface free-text boxes into pickers. Always non-nil
	// (internal/seen.Open("") returns a usable, empty, unpersisted
	// register), same always-usable convention as Baseline above.
	SeenValues *seen.Register
	// HostQuietAfter is how long a host may be silent before the map
	// draws it quiet (config baseline.hostQuietAfter, 24 hours by owner
	// ratification on 2026-09-07). Served to the browser on
	// GET /api/baseline/off, which is where the other two thresholds
	// already travel -- see offBaselineResponse for why it rides there
	// rather than on GET /api/hosts. The zero value is treated as the
	// default by that handler's caller, so a Server built without it in a
	// test is still coherent.
	HostQuietAfter time.Duration
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
	// Droplist is the operator-authored drop list entry store (issue
	// #1223, stage 1 of the design ratified on #461): ranges an admin
	// has explicitly decided to block, distinct from the fetched
	// threat-intel feeds internal/blocklist's own doc comment describes
	// (that half is wired directly into the engine, not here). Always
	// non-nil (internal/droplist.Open("") returns a usable, empty,
	// unpersisted store), same always-usable convention as Audit above.
	// #1224 wired it up: GET/POST /api/droplist, DELETE
	// /api/droplist/{cidr...}, the key routes and the RouterOS feed
	// below all read and write through this store.
	Droplist *droplist.Store
	// Prefs is the per-user preferences store (issue #1283): one
	// versioned JSON record per account, read on sign-in and written on
	// change, replacing what used to live in the browser's localStorage.
	// Always non-nil (internal/prefs.Open("") returns a usable, empty,
	// unpersisted store), same always-usable convention as Droplist
	// above. GET/PUT /api/me/preferences (preferences.go) are the only
	// routes that touch it; handleAuthDeleteUser clears a user's record
	// when the account itself is deleted.
	Prefs *prefs.Store
	// DeviceStaleAfter (issue #98) is how long a device's LastSeen may go
	// without updating before GET /api/devices reports it as "stale" --
	// same threshold detect.DeviceSilenceDetector uses to raise an actual
	// flag (see internal/detect/device_silence.go), duplicated here
	// purely so this read-time status computation doesn't need to import
	// internal/detect for one number. Zero means "not configured": every
	// device with at least one event is always reported "live".
	DeviceStaleAfter time.Duration
	StartTime        time.Time
	// Now is where the definitions read path takes the current time from
	// -- the nightly watch fill and the ring/coverage view it renders
	// (see s.now and handleDefinitionsList). Nil means time.Now, which is
	// every deployment: this is a seam for tests, not a setting.
	//
	// Deliberately narrow. It is not a process-wide fake clock: ingest
	// stamps, session expiry, retention and everything else keep reading
	// time.Now directly, because a whole-process clock that can be moved
	// from an HTTP request is a much larger thing to get wrong than the
	// one read path #1063 needed to stop waiting on.
	Now func() time.Time
	// TestHooks turns on the test-only routes (POST /api/test/clock and
	// POST /api/test/reset) and nothing else. Off unless main saw
	// MV_TEST_HOOKS=1; when off the routes are not registered at all, so
	// they 404 rather than 403 -- see testhooks.go.
	TestHooks bool
	// Reseed re-applies this binary's shipped catalogue after
	// POST /api/test/reset empties the definitions store. Set by main
	// alongside TestHooks; nil means the reset leaves the store empty,
	// which is only ever a test fixture's situation.
	Reseed func() error
	// testClockOffset is how far POST /api/test/clock has moved this
	// process's definitions clock forward, in nanoseconds. Zero unless
	// that route exists and something called it.
	testClockOffset atomic.Int64
	// Version is main.version (the build-time-stamped short commit SHA,
	// "dev" for a plain local build) -- passed in rather than read
	// directly since internal/api can't import main. Surfaced on
	// GET /api/healthz, the one endpoint reachable with no auth and no
	// session regardless of deployment state, so "which build am I
	// running" is checkable without any special access.
	Version string
	// GeoIP reports whether a country database was successfully opened
	// (main sets this from geoip.Lookup.Configured()), surfaced on
	// GET /api/healthz as `geoip` (#1198). Country flags degrade silently
	// to blank when there is no database -- indistinguishable, from the
	// UI's side, from "no public traffic yet" -- so this is the one fact
	// that lets the country filter and the ingest settings card tell a
	// reader which case they are looking at instead of staying quiet
	// about it.
	GeoIP bool
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

	// ConfigUpgradeSettings is #1218's offer: every optional top-level
	// setting this build understands that the running config does not
	// set, each paired with deploy/config.example.yaml's own ready-to-
	// paste YAML for it. Computed once at boot from the fixed inputs it
	// depends on (this binary, this process's config) -- see main.go and
	// config.MissingSettings -- and served as-is by handleConfigUpgrade.
	ConfigUpgradeSettings []config.MissingSetting

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
	// Vault is the router-backup store (#394) -- nil in tests that do
	// not exercise it and on any instance built without one, same
	// nil-means-disabled convention as History/NetClass above. The SFTP
	// drop box (internal/backupsftp) writes to it directly; the HTTP
	// handlers here only ever read it back and download from it.
	Vault *backupvault.Vault
	// BackupSlices reassembles a backup pushed over the ingest channel
	// in slices (#955). Nil where no vault is configured -- the handler
	// refuses rather than accepting a file it has nowhere to put.
	BackupSlices *backupslice.Receiver
	// vaultUnlock is which session, if any, currently holds the vault's
	// optional passphrase open (#956, routerbackupslock.go).
	vaultUnlock vaultUnlockState
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

	// Settings holds the small set of configuration an admin may change
	// from inside the running app rather than in config.yaml -- today,
	// the event buffer's size (#796). Backs PUT /api/settings/store; see
	// settings.go. Nil is a valid state (a Server built without one, as
	// most tests are): the PUT then refuses rather than pretending to
	// store anything.
	Settings *settings.Store
	// memory is the event-buffer figure in effect and the range it may
	// move within -- see settings.go. Populated by InitMemory at boot;
	// unexported because it carries a mutex and an atomic, which a
	// struct literal cannot safely fill in.
	memory memoryState

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

	// droplistKeyMintMu serializes handleDroplistKeyCreate's create-then-
	// revoke sequence (#1224 hardening, security review): at most one
	// droplist-pull token is ever meant to exist, but Create and the
	// revoke loop that follows it are two separate steps, so two
	// concurrent mint requests could otherwise each create a token before
	// either reaches its revoke loop and leave two live keys instead of
	// one. Held for the whole create-then-revoke sequence, in that order
	// -- create first, same as an unserialized request -- so a request
	// that failed after create still leaves no worse than an extra
	// revocable token, never zero. Zero value is ready to use, same as
	// the mutexes above.
	droplistKeyMintMu sync.Mutex
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

// now is the current time as the definitions read path sees it: the
// process clock (or Server.Now, where a test supplied one) plus whatever
// POST /api/test/clock has been asked to add.
//
// The offset is separate from the Now field on purpose. Now is the base
// clock a unit test substitutes wholesale; the offset is the movable part
// the test-hooks route drives, and keeping them apart means a test can
// pin the base to a fixed instant and still exercise the endpoint.
func (s *Server) now() time.Time {
	base := time.Now
	if s.Now != nil {
		base = s.Now
	}
	return base().Add(time.Duration(s.testClockOffset.Load()))
}

// routes returns every /api/* endpoint. Order is irrelevant to
// ServeMux's matching (it is longest-pattern-wins, not first-match), so
// these stay grouped by area for readability.
func (s *Server) routes() []route {
	rs := s.apiRoutes()
	// Appended rather than declared inline so the ordinary table stays
	// exactly the set a shipped image serves: with MV_TEST_HOOKS unset
	// these two patterns are never registered, so they 404 like any
	// unknown path instead of existing and refusing (see testhooks.go).
	if s.TestHooks {
		rs = append(rs, s.testHookRoutes()...)
	}
	return rs
}

func (s *Server) apiRoutes() []route {
	return []route{
		{http.MethodGet, "/api/healthz", s.handleHealthz},
		{http.MethodGet, "/api/events", s.handleEvents},
		{http.MethodGet, "/api/devices", s.handleDevices},
		{http.MethodGet, "/api/devices/macs", s.handleDeviceMACs},
		// Issue #1281: declaring, enrolling and deleting a syslog-only
		// device, and the addresses the listener gate has refused a line
		// from. Admin-only writes beside the viewer-tier read above --
		// see devices.go's own doc comments for why each is gated where
		// it is.
		{http.MethodPost, "/api/devices", s.handleDeviceCreate},
		{http.MethodDelete, "/api/devices/{id}", s.handleDeviceDelete},
		{http.MethodPost, "/api/devices/{id}/registration", s.handleDeviceRegister},
		{http.MethodPost, "/api/devices/{id}/enrolment", s.handleDeviceEnrolmentCreate},
		{http.MethodPost, "/api/devices/{id}/enrolment/address", s.handleDeviceEnrolmentRebind},
		{http.MethodDelete, "/api/devices/{id}/enrolment", s.handleDeviceEnrolmentDelete},
		{http.MethodGet, "/api/devices/refused", s.handleDevicesRefused},
		{http.MethodGet, "/api/rules", s.handleRules},
		// The pushed rule/NAT tables (issue #186 step 4) -- session-gated
		// reads over RouterState, entirely separate from the push
		// endpoint itself, which lives on ingestRoutes' own mux and is
		// deliberately absent from this table.
		{http.MethodGet, "/api/routeros/{device}/rules", s.handleRouterOSRules},
		{http.MethodGet, "/api/routeros/{device}/nat", s.handleRouterOSNAT},
		{http.MethodGet, "/api/routeros/{device}/addresses", s.handleRouterOSAddresses},
		// Per-tunnel state (issue #874, City 9's ingest side): WireGuard
		// handshake-derived up/down and the /ppp/active table backing
		// L2TP/PPTP/SSTP/OVPN alike. Same session-gated, read-only shape
		// as the three routes above.
		{http.MethodGet, "/api/routeros/{device}/wireguard", s.handleRouterOSWireguard},
		{http.MethodGet, "/api/routeros/{device}/ppp-active", s.handleRouterOSPPPActive},
		{http.MethodGet, "/api/stats", s.handleStats},
		// #644 round 21's top port/top talker table columns -- see
		// handleStatsTops' own doc comment for why this is a separate
		// route rather than a field on /api/stats above.
		{http.MethodGet, "/api/stats/tops", s.handleStatsTops},
		// #1018's two tools on the living topology: where a port is used
		// (seen traffic, and the pushed rules that name it), and the one
		// hop one logged line took through the router. Reads over the
		// event buffer and RouterState; nothing here touches the network.
		{http.MethodGet, "/api/ports", s.handlePorts},
		{http.MethodGet, "/api/trace", s.handleTrace},
		// Ingest-loss "Clear all" (#1015): zeroes the four monotonic
		// syslog-listener loss counters /api/stats' "syslog.loss" field
		// reads, so a transient loss stops permanently marking the
		// instance once the operator has seen it. See syslog.go.
		{http.MethodPost, "/api/syslog/loss/clear", s.handleSyslogLossClear},
		// The one setting an admin may change from inside the app (#796).
		// No matching GET: the memory group's whole state rides on
		// /api/stats' "memory" object, which every open tab is already
		// polling for the count and capacity beside it -- see
		// handleStats for why one payload rather than two.
		{http.MethodPut, "/api/settings/store", s.handleStoreSettingsUpdate},
		// The on-disk history's switch and caps (#910), which sit one
		// storey under the memory group on the same screen. This pair
		// does have its own GET, unlike the memory group above: what it
		// reports -- the days actually held, the oldest and newest of
		// them, the bytes -- is read off the retention directory, which
		// is far too much work to hang off /api/stats' few-second poll.
		{http.MethodGet, "/api/settings/history", s.handleHistorySettings},
		{http.MethodPut, "/api/settings/history", s.handleHistorySettingsUpdate},
		{http.MethodGet, "/api/ws", s.handleWS},
		{http.MethodGet, "/api/lookup/ip/{ip}", s.handleIPLookup},
		{http.MethodGet, "/api/flags", s.handleFlagsList},
		{http.MethodPost, "/api/flags/clear-all", s.handleFlagsClearAll},
		{http.MethodPost, "/api/flags/{id}/verdict", s.handleFlagsVerdict},
		// Editing the note a verdict already carries (#1232). Free to
		// take the wildcard-then-literal shape the POST above uses --
		// the ambiguity the next comment describes is between patterns
		// that could match the same request, and no other PUT is
		// registered under /api/flags/.
		{http.MethodPut, "/api/flags/{id}/note", s.handleFlagNote},
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
		// #640's ledger. Same literal-then-wildcard shape as the DELETE
		// route above it, for the reason the comment above gives: a
		// wildcard-then-literal fourth segment under /api/flags/ cannot
		// be registered alongside it.
		{http.MethodGet, "/api/flags/expectations", s.handleExpectationsList},
		{http.MethodDelete, "/api/flags/expectations/{id}", s.handleExpectationForget},

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

		// Retiring a network segment (#460). One GET for the whole
		// surface, because an offer and a ghost are the same object one
		// decision apart and the map paints both together.
		{http.MethodGet, "/api/decommission", s.handleDecommission},
		{http.MethodPost, "/api/decommission/watches", s.handleDecommissionCreate},
		{http.MethodPost, "/api/decommission/dismiss", s.handleDecommissionDismiss},
		{http.MethodPost, "/api/decommission/watches/{id}/force", s.handleDecommissionForce},
		{http.MethodPost, "/api/decommission/watches/{id}/undo", s.handleDecommissionUndo},
		{http.MethodDelete, "/api/decommission/watches/{id}", s.handleDecommissionDelete},

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

		// The host presence register (issue #1016) -- see hosts.go.
		{http.MethodGet, "/api/hosts", s.handleHostsList},
		{http.MethodPut, "/api/hosts/{key}/mark", s.handleHostMarkPut},
		{http.MethodDelete, "/api/hosts/{key}/mark", s.handleHostMarkDelete},
		{http.MethodGet, "/api/hosts/{ip}/dossier", s.handleHostDossier},

		// The baseline line register (issue #1016, round 49). Only
		// today's off-baseline lines are reachable -- there is
		// deliberately no endpoint serving the established ones, see
		// handleBaselineOff.
		// The seen-values register (issue #1226) -- see seen.go. One
		// route for both fields; the response is keyed by field name.
		{http.MethodGet, "/api/seen-values", s.handleSeenValues},

		{http.MethodGet, "/api/baseline/off", s.handleBaselineOff},
		{http.MethodPut, "/api/baseline/{key}/expected", s.handleBaselineExpectedPut},
		{http.MethodDelete, "/api/baseline/{key}/expected", s.handleBaselineExpectedDelete},

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

		// The caller's own preferences record (issue #1283): presets,
		// widgets and layout, held on the server instead of the
		// browser's localStorage. Open to any signed-in user, same
		// reasoning as /api/auth/password above -- it acts only on the
		// session's own account, with no id in the request that could
		// point it at someone else's.
		{http.MethodGet, "/api/me/preferences", s.handlePreferencesGet},
		{http.MethodPut, "/api/me/preferences", s.handlePreferencesPut},

		{http.MethodGet, "/api/audit", s.handleAuditList},

		// The guided setup wizard's view of what has actually landed
		// (#320) -- open to any signed-in user, see handleSetupStatus.
		{http.MethodGet, "/api/setup/status", s.handleSetupStatus},
		// Renders the wizard's RouterOS commands for a router or an
		// operator-picked version (#436) -- same tier as the status GET
		// beside it, see handleSetupCommands.
		{http.MethodPost, "/api/setup/commands", s.handleSetupCommands},
		// The claim ledger's own marks (#487): a step skipped or forced
		// past. Admin-only, matching the modal it is written from.
		{http.MethodPost, "/api/setup/mark", s.handleSetupMark},
		// The wizard header field's answer (#1213): what address a
		// router can reach this instance on. Admin-only, same gate as
		// the mark endpoint above.
		{http.MethodPost, "/api/setup/address", s.handleSetupAddress},
		// How step 6's script delivers its backup (#955): over SFTP to
		// the drop box, or in slices through the ingest channel for an
		// HTTPS-only install. A property of the deployment, stored
		// beside the address above and admin-only for the same reason.
		{http.MethodPut, "/api/setup/backup-transport", s.handleSetupBackupTransport},

		// "Log every rule" (#435, named "Tune logging" until #1134,
		// which left these two paths alone): upload a RouterOS export,
		// get back the
		// filter rules that cross a dark boundary with their pushed
		// counters as the cost of watching them, then render logging
		// switched on for whichever the operator picks. Same tier as the
		// operational writes above -- user, not admin -- since this
		// changes a config file the operator downloads and applies
		// themselves; mikroview never touches the router (see
		// tunelogging.go).
		{http.MethodPost, "/api/tune-logging/analyse", s.handleTuneLoggingAnalyse},
		{http.MethodPost, "/api/tune-logging/render", s.handleTuneLoggingRender},

		{http.MethodGet, "/api/config/problems", s.handleConfigProblems},
		{http.MethodGet, "/api/persistence", s.handlePersistence},

		// The "N new settings are available" notice (#1218) -- the setup
		// wizard's paste-block treatment, applied to whatever this
		// version understands that config.yaml does not set. See
		// configupgrade.go.
		{http.MethodGet, "/api/config/upgrade", s.handleConfigUpgrade},

		// The upgrade notice (#1240): which build this data directory
		// last ran, how much of the fleet is still on the old setup, and
		// the admin's "done". See upgrade.go.
		{http.MethodGet, "/api/upgrade", s.handleUpgrade},
		{http.MethodPost, "/api/upgrade/acknowledge", s.handleUpgradeAcknowledge},

		// Router-backup vault (#394): the Settings group's list and the
		// download an admin uses to actually restore a dead router.
		{http.MethodGet, "/api/router-backups", s.handleRouterBackupsList},
		{http.MethodGet, "/api/router-backups/{device}/{generation}/{kind}", s.handleRouterBackupDownload},

		// Reading and comparing the redacted text export (#895). The
		// `text` route is more specific than the `{kind}` download
		// above it, so ServeMux picks it first; diff takes its pair as
		// query parameters because neither generation owns the other.
		{http.MethodGet, "/api/router-backups/{device}/{generation}/text", s.handleRouterBackupText},
		{http.MethodGet, "/api/router-backups/{device}/diff", s.handleRouterBackupDiff},

		// The kept pool (#1126): an admin marks one stored backup as
		// one to hold on to, with a comment saying why, and it stops
		// counting towards the ten the vault cycles.
		{http.MethodPost, "/api/router-backups/{device}/{generation}/protect", s.handleRouterBackupProtect},
		{http.MethodDelete, "/api/router-backups/{device}/{generation}/protect", s.handleRouterBackupUnprotect},
		{http.MethodPatch, "/api/router-backups/{device}/{generation}/protect", s.handleRouterBackupComment},

		// The vault's optional admin passphrase (#956). Reading a backup
		// needs the passphrase once one is set; a backup still arrives
		// without it.
		{http.MethodPost, "/api/router-backups/unlock", s.handleRouterBackupUnlock},
		{http.MethodPost, "/api/router-backups/lock", s.handleRouterBackupLock},
		{http.MethodPost, "/api/router-backups/passphrase", s.handleRouterBackupSetPassphrase},
		{http.MethodDelete, "/api/router-backups/passphrase", s.handleRouterBackupRemovePassphrase},
		{http.MethodPut, "/api/router-backups/passphrase", s.handleRouterBackupChangePassphrase},

		// The drop list's admin API (issue #1224): the entry list/add/
		// remove routes, and the pull key that lets a router fetch the
		// generated .rsc feed. The pull route itself
		// (GET /api/droplist.rsc) is bearer-only and lives on its own
		// mux -- see droplistPullRoutes in auth.go -- deliberately absent
		// from this session-gated table.
		{http.MethodGet, "/api/droplist", s.handleDroplistList},
		{http.MethodPost, "/api/droplist", s.handleDroplistCreate},
		// Registered before the {cidr...} pattern purely for readability,
		// same as /api/definitions/schema above it: ServeMux matches the
		// literal segment regardless of declaration order.
		{http.MethodPost, "/api/droplist/key", s.handleDroplistKeyCreate},
		{http.MethodDelete, "/api/droplist/key", s.handleDroplistKeyDelete},
		{http.MethodDelete, "/api/droplist/{cidr...}", s.handleDroplistDelete},

		{http.MethodGet, "/api/auth/session", s.handleAuthSession},
		{http.MethodPost, "/api/auth/register", s.handleAuthRegister},
		{http.MethodPost, "/api/auth/login", s.handleAuthLogin},
		{http.MethodPost, "/api/auth/password", s.handleAuthChangePassword},
		{http.MethodPost, "/api/auth/logout", s.handleAuthLogout},
		{http.MethodPost, "/api/auth/logout-all", s.handleAuthLogoutAll},
		{http.MethodPost, "/api/auth/users", s.handleAuthCreateUser},
		{http.MethodGet, "/api/auth/users", s.handleAuthListUsers},
		{http.MethodDelete, "/api/auth/users/{id}", s.handleAuthDeleteUser},
		{http.MethodPost, "/api/auth/users/{id}/reset-password", s.handleAuthResetUserPassword},

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
