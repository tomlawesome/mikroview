// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/blocklist"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/netclass"
	"github.com/tomlawesome/mikroview/internal/notify"
	"github.com/tomlawesome/mikroview/internal/oidc"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/routerstate"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/servertls"
	"github.com/tomlawesome/mikroview/internal/setup"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/suggest"
	"github.com/tomlawesome/mikroview/internal/syslog"
	"github.com/tomlawesome/mikroview/internal/tlssniff"
	"github.com/tomlawesome/mikroview/internal/watchlist"
	"github.com/tomlawesome/mikroview/web"
	"golang.org/x/term"
)

// engineTickInterval is how often the engine's tick driver runs (issue
// #405). Not itself a detector cadence: it is the granularity at which
// Engine.Tick asks "is anything due", and each Ticked definition still
// runs at its own declared TickInterval. Set to the finest cadence any
// shipped definition declares (global_spike's 10s, above) so nothing
// ever waits longer than its own interval for the driver to come round.
const engineTickInterval = 10 * time.Second

// deviceSilenceCheckInterval moved onto the definition that owns it
// (issue #405, internal/engine/shipped_device_silence.go): a cadence is
// part of what a definition means, so it is declared by the definition
// through Ticked.TickInterval rather than chosen by whatever drives it.

// suggestSyncInterval is how often internal/suggest re-scans routerState
// for new/changed candidates (#243 slice 5). Coarser than either check
// above on purpose: routerState itself only changes when RouterOS's own
// push script runs, which docs/routeros-setup.md's own example scheduler
// entry sets to every 20 minutes -- syncing more often than that finds
// nothing new, so this just needs to be well inside that window rather
// than tracking it exactly.
const suggestSyncInterval = 5 * time.Minute

// matchLogPurgeInterval is how often internal/matchlog.PostgresStore
// deletes matches older than watchlist.matchLogRetention (#243 slice
// 6). Coarser than suggestSyncInterval on purpose: retention is
// measured in days by default, so purging every few minutes buys
// nothing an hourly pass wouldn't -- this only needs to keep the table
// from growing meaningfully past its retention window between runs,
// not to enforce that window to the minute.
const matchLogPurgeInterval = 1 * time.Hour

// loginLimiter{Threshold,Window}: brute-force protection on
// POST /api/auth/login (see internal/auth.LoginLimiter) -- an internal
// hardening constant, not exposed via config, same tier as ws.go's
// wsPongTimeout/wsPingInterval.
const (
	loginLimiterThreshold = 5
	loginLimiterWindow    = 5 * time.Minute
)

// ingestLimiter{Threshold,Window}: rate-limits POST /api/ingest/routeros
// per ingest token (issue #186 step 3) -- see internal/api/ingest.go's
// handleIngestRouterOS doc comment for the reasoning behind these
// specific numbers. Same internal-hardening-constant tier as
// loginLimiterThreshold/Window above.
const (
	ingestLimiterThreshold = 120
	ingestLimiterWindow    = 15 * time.Minute
)

// version is stamped at build time via -ldflags "-X main.version=..."
// (see Dockerfile and .github/workflows/docker.yml).
//
// It carries the lane as well as the commit, because "which build is
// this" and "how much should I trust it" are the same question:
//
//	dev:<short-sha>      a local or dev-branch build; nothing published
//	preview:<short-sha>  the published release candidate
//	v1.2.3               a build from a release tag
//
// The commit, not a registry digest: an image digest is computed from
// the pushed image *after* the build, so a binary cannot know its own.
// The commit it was built from is the achievable stand-in, and is what
// the signing provenance binds to anyway.
//
// "dev:local" is the fallback for a plain `go build .` with no ldflags,
// so local development never shows a blank or misleading value.
var version = "dev:local"

// versionMarkerPath persists the last-seen version so a restart can
// tell a routine restart (same version) apart from a real upgrade (the
// image changed) -- worth telling the operator about, since "did my
// `docker compose pull` actually pick up the new image" is a common
// point of confusion otherwise.
var versionMarkerPath = config.DefaultDataDir + "/version"

// versionBootMessage returns the one line to log for this boot's
// version check, given the version string previously persisted (""
// if none, or unreadable) and the current build version -- a single
// line either way, not a routine "no upgrade" line on every ordinary
// restart plus a separate upgrade line, since only one of those is
// ever useful on a given boot.
func versionBootMessage(prev, current string) string {
	prev = strings.TrimSpace(prev)
	if prev != "" && prev != current {
		return fmt.Sprintf("upgraded from %s to %s", prev, current)
	}
	return fmt.Sprintf("version %s", current)
}

// logVersionAndMigration logs versionBootMessage's result and updates
// the persisted marker for next time. Like every other optional
// persistence in this codebase (see flags.Open's doc comment), a
// read/write failure is never fatal -- it just means upgrade detection
// silently doesn't work until the underlying path issue is fixed.
func logVersionAndMigration(logger *slog.Logger) {
	prev, err := os.ReadFile(versionMarkerPath)
	if err != nil && !os.IsNotExist(err) {
		logger.Warn(fmt.Sprintf("reading version marker: %v", err))
	}
	logger.Info(versionBootMessage(string(prev), version))
	if err := os.WriteFile(versionMarkerPath, []byte(version), 0o600); err != nil {
		logger.Warn(fmt.Sprintf("writing version marker: %v (upgrade detection won't work on the next restart)", err))
	}
}

// securityHeaders wraps next, setting baseline defense-in-depth headers
// on every response -- absent entirely before this fix. The frontend
// doesn't use {@html}/innerHTML anywhere (Svelte's default auto-
// escaping is intact), so there's no known current XSS path this CSP
// closes, but its absence removed a layer against any future
// regression; X-Frame-Options is independently load-bearing today,
// closing a real clickjacking gap -- without it, mikroview's UI could
// be iframed by any third-party page, and every mutating action a
// signed-in user's own browser performs (add user, clear a flag,
// toggle a detector, log out) correctly carries the session cookie and
// CSRF header regardless, since it's the *real* page's own JS issuing
// the request -- clickjacking tricks the user into clicking through an
// invisible overlay, it doesn't need to bypass either check. hsts is a
// separate opt-in -- see its call site's doc comment.
func securityHeaders(next http.Handler, hsts bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// staticCacheHeaders wraps the embedded frontend's file server, telling
// browsers how long each kind of file may be reused (#347).
//
// Two shapes, because the build produces two:
//
//   - /assets/* is content-hashed by Vite -- index-BShEGKey.js changes
//     its *name* whenever its contents change, so a copy can never go
//     stale and is safe to keep for as long as the browser likes.
//   - Everything else (index.html, sw.js, registerSW.js, the manifest,
//     the icons) keeps a fixed name across builds, so it gets no-cache:
//     store it, but check with the server before using it again.
//
// sw.js is the one that makes this load-bearing rather than tidy.
// mikroview is a PWA whose service worker precaches the whole app shell,
// so after an upgrade the first load is served the OLD shell from that
// precache. registerType: 'autoUpdate' is meant to make that transient
// -- the browser refetches sw.js, sees it changed, activates the new
// worker and reloads. But with no Cache-Control at all, a browser may
// reuse its cached sw.js for up to 24 hours before revalidating, and
// then the update is never noticed: an upgraded server keeps serving a
// days-old UI while /api/healthz correctly reports the new version.
// That is not hypothetical -- it cost an operator an hour of chasing an
// image tag that was never wrong.
//
// Files come from an embed.FS, whose entries have a zero modification
// time, so http.FileServer sends no Last-Modified and there are no
// conditional requests to answer with a 304. no-cache therefore means
// re-sending the body each time; at ~1.5KB for index.html and a few KB
// for sw.js that is not worth optimising. If it ever is, the answer is
// an ETag over the embedded bytes -- not a longer max-age, which is the
// thing that caused this.
func staticCacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// httpsRedirectTarget builds the Location for redirecting a plain-HTTP
// request to HTTPS -- strips any port off the request's Host header and
// assumes HTTPS is reachable on the browser-default 443 (see
// config.Listen.HTTPRedirect's doc comment for when that assumption
// doesn't hold), preserving the original path/query/method-relevant
// URI otherwise.
//
// A normal browser navigation's Host header always names wherever the
// user actually typed/clicked, so echoing it back is fine -- but a
// client connecting directly (curl -H "Host: evil.example.com" ...)
// controls it completely, turning an unvalidated echo into an open
// redirect. allowedHosts (cfg.TLS.Hosts, the operator-declared SAN
// list -- exactly the hostnames this deployment is meant to be reached
// as) is the known-good set to check against: a Host outside it falls
// back to allowedHosts[0], a real configured target, instead of
// whatever the request claimed. If allowedHosts is empty (TLS.Hosts
// left unconfigured, so servertls auto-detects instead -- see
// internal/servertls's own defaultHosts), there's no explicit ground
// truth available here to validate against, so this falls back to the
// prior echo-Host behavior.
func httpsRedirectTarget(r *http.Request, allowedHosts []string) string {
	if len(allowedHosts) == 0 {
		allowedHosts = localRedirectHosts()
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if len(allowedHosts) > 0 && !slices.Contains(allowedHosts, host) {
		host = allowedHosts[0]
	}
	return "https://" + host + r.URL.RequestURI()
}

// samePortRedirectHost is the host policy for the plaintext-on-the-TLS-
// port redirect (#325). Same allowlist reasoning as
// httpsRedirectTarget above -- never echo an arbitrary Host header back
// in a Location -- with one difference: the port is kept. That listener
// bounces to the browser default 443 because it lives on port 80; this
// one is the HTTPS listener, so https is reachable on exactly the
// address and port the client already dialled.
func samePortRedirectHost(requested string, allowedHosts []string, localAddr string) string {
	host, port, err := net.SplitHostPort(requested)
	if err != nil {
		host, port = requested, ""
	}
	if port == "" {
		if _, p, err := net.SplitHostPort(localAddr); err == nil {
			port = p
		}
	}
	allowed := allowedHosts
	if len(allowed) == 0 {
		allowed = localRedirectHosts()
	}
	if len(allowed) > 0 && !slices.Contains(allowed, host) {
		host = allowed[0]
	}
	if host == "" {
		return ""
	}
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

// localRedirectHosts is the fallback known-good set when cfg.TLS.Hosts
// is unset: this machine's own hostname and the addresses it holds.
//
// It exists because "unset" is the shipped default -- defaults() sets
// TLS.Enabled and Listen.HTTPRedirect but never TLS.Hosts, and
// deploy/docker-compose.yml maps host port 80 to the redirect listener.
// So the allowlist above was empty out of the box and the function fell
// back to echoing r.Host into a 308 Location: an unauthenticated
// Host-header reflection (CWE-601) in the default configuration, in a
// function whose own doc comment says it closes "a known vulnerability
// class".
//
// Exploitability is genuinely weak, and #272's two reviewers who found
// it disagreed about how weak -- a browser cannot be made to send a Host
// differing from the URL it is navigating to, so this needs someone able
// to put raw HTTP at the listener, which is mostly self-directed. That
// is why the fix is to make the guard work by default rather than to
// make TLS.Hosts mandatory: nobody has to configure anything, and
// reaching mikroview by bare IP keeps working. Owner decision on #283
// finding 2.
//
// Loopback is included last rather than first so a machine with a real
// address prefers it -- allowedHosts[0] is what an unrecognised Host is
// rewritten to, and redirecting a LAN client to https://127.0.0.1 would
// be useless. If nothing can be enumerated at all, this returns empty
// and the caller keeps the previous echo behaviour, which is strictly
// better than redirecting everyone to an address that does not work.
func localRedirectHosts() []string {
	var hosts []string
	seen := make(map[string]bool)
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}

	if name, err := os.Hostname(); err == nil {
		add(name)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
				continue
			}
			add(addr.String())
		}
	}
	add("localhost")
	add("127.0.0.1")
	return hosts
}

func main() {
	// -version: prints the build-time-stamped commit SHA (see the
	// `version` var above) and exits -- no config load, no network,
	// nothing else needed, so it's checked before every other mode
	// below. The intended use is `docker exec <container> mikroview
	// -version` against a running deployment, so the output is the bare
	// string only (no "version:" prefix, no trailing punctuation) --
	// easy to capture in a script without trimming anything first.
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		os.Exit(runVersion())
	}
	// The runtime image is distroless (no shell, no curl/wget), so Docker's
	// HEALTHCHECK -- and any orchestrator's readiness probe -- can't shell
	// out to check the app; the binary has to check itself instead. Config
	// is loaded from file/env only here (not os.Args) since this runs as a
	// standalone HEALTHCHECK CMD with no other flags to parse.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(runHealthcheck())
	}
	// -recover-admin-account: the account-recovery path (see
	// docs/configuration.md's "Authentication" section) -- container/
	// host access is the trust anchor for these, deliberately outside
	// the web UI/API entirely, so a locked-out admin isn't dependent on
	// the very system they're locked out of.
	if len(os.Args) > 1 && os.Args[1] == "-recover-admin-account" {
		os.Exit(runRecoverAdminAccount(os.Args[2:]))
	}
	// -validate-config: check a config before deploying it. Same rules
	// the server enforces at startup (one config.Validate, two entry
	// points) so this can never pass something startup would reject, or
	// vice versa.
	if len(os.Args) > 1 && os.Args[1] == "-validate-config" {
		os.Exit(runValidateConfig(os.Args[2:]))
	}
	// Recovery-key tooling: the second factor beyond host access on the
	// commands that change authentication state (issue #134).
	if len(os.Args) > 1 && os.Args[1] == "-generate-recovery-keys" {
		os.Exit(runGenerateRecoveryKeys(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "-backup" {
		os.Exit(runBackup(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "-restore" {
		os.Exit(runRestore(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "-transfer-admin" {
		os.Exit(runTransferAdmin(os.Args[2:]))
	}

	configLog := logging.New("config")
	cfg, configResult, err := config.LoadWithProblems(os.Getenv("MIKROVIEW_CONFIG"), os.Args[1:])
	if err != nil {
		// The structured report rather than err.Error(): this is the
		// message that stops the server, so the line telling the
		// operator what to change should not be the tail of a long
		// sentence. Falls back to the error's own text when the failure
		// was reading or parsing the file, which produces no Problems to
		// render.
		if len(configResult.Fatal) > 0 {
			configLog.Error("invalid configuration:\n" + config.Report(configResult.Fatal) + config.CheckHint)
		} else {
			configLog.Error(err.Error() + "\n" + config.CheckHint)
		}
		os.Exit(1)
	}
	// Every component logger created before this point (configLog above)
	// still picks up the level -- SetLevel adjusts the shared threshold
	// in place, not a per-logger setting fixed at New() time.
	logging.SetLevel(cfg.Log.Level)

	logging.PrintBanner()
	logVersionAndMigration(logging.New("mikroview"))

	storeCapacity := cfg.Store.Capacity()
	logging.New("store").Info(fmt.Sprintf(
		"event buffer: %s reserved for up to %d events (store.maxMemory) -- once traffic arrives, GET /api/stats reports how full it is and how far back it actually reaches",
		cfg.Store.MaxMemory, storeCapacity))
	st := store.New(storeCapacity, cfg.Store.Retention)
	devices := device.NewRegistry(cfg.Devices)
	// Tell the syslog listener which sources are the operator's declared
	// routers, so a flood of undeclared ones cannot take every
	// connection slot and lock them out -- see syslog.reservedFraction.
	// Set here, before any listener starts, which is the contract
	// SetConfiguredSources documents.
	configuredSources := make([]string, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		if d.SourceIP != "" {
			configuredSources = append(configuredSources, d.SourceIP)
		}
	}
	syslog.SetConfiguredSources(configuredSources)
	h := hub.New()
	geoLog := logging.New("geoip")
	geo, err := geoip.Open(cfg.GeoIP.DBPath)
	if err != nil {
		geoLog.Warn(fmt.Sprintf("%v (country flags disabled)", err))
	}
	defer geo.Close()
	// rep: always built (AbuseIPDBKey empty just means that one source
	// inside it stays inert; Shodan InternetDB is free/keyless and
	// always queried).
	rep := reputation.New(cfg.Reputation.AbuseIPDBKey)

	flagsLog := logging.New("flags")
	// Boot-time context: the signal-aware ctx below is created after
	// the stores exist, and connecting to the database has to happen
	// before any of them.
	bootCtx := context.Background()
	persistence, err := openStorage(bootCtx, cfg)
	if err != nil {
		logging.New("storage").Error(err.Error())
		os.Exit(1)
	}
	defer persistence.Close()

	flagsBackend, err := persistence.backendFor(bootCtx, "flags", cfg.Flags.StorePath)
	if err != nil {
		flagsLog.Warn(err.Error())
	}
	fs, err := flags.OpenWithBackend(flagsBackend)
	mustOpenStore(flagsLog, err)

	// macRegistry backs the new-device/new-MAC detector (issue #103
	// phase 1) -- see internal/device.MACRegistry's doc comment for why
	// this needs its own persisted store distinct from devices above
	// (that one tracks router source IPs, not LAN client MACs).
	macLog := logging.New("device-mac")
	macBackend, err := persistence.backendFor(bootCtx, "mac_registry", cfg.DeviceMAC.StorePath)
	if err != nil {
		macLog.Warn(err.Error())
	}
	macRegistry, err := device.OpenMACRegistryWithBackend(macBackend)
	mustOpenStore(macLog, err)

	// RuleUsage (issue #102): a long-lived, persisted per-rule
	// FirstSeen/LastSeen/Count record backing the stale-rule detector --
	// see internal/rules' doc comment for why this can't just reuse
	// internal/store's totalByRule (in-memory, windowed to the store's
	// short retention period).
	rulesLog := logging.New("rules")
	rulesBackend, err := persistence.backendFor(bootCtx, "rule_usage", cfg.Flags.RuleUsageStorePath)
	if err != nil {
		rulesLog.Warn(err.Error())
	}
	ru, err := rules.OpenWithBackend(rulesBackend)
	mustOpenStore(rulesLog, err)

	// Storage is resolved before any store opens: it decides whether
	// this deployment persists to JSON files or Postgres, and performs
	// the one-time JSON adoption when Postgres is newly configured.
	// A configured-but-unreachable database is fatal -- see openStorage.
	authLog := logging.New("auth")
	authBackend, err := persistence.backendFor(bootCtx, "auth", cfg.Auth.StorePath)
	if err != nil {
		authLog.Error(fmt.Sprintf("preparing the accounts store: %v -- refusing to start with authentication in an unknown state", err))
		os.Exit(1)
	}
	authStore, err := auth.OpenWithBackend(authBackend)
	// A non-nil error here, when persistence is actually configured,
	// ALWAYS means "the accounts file exists but couldn't be
	// read/parsed" -- OpenWithBackend returns (store, nil) for both "no
	// persistence configured" and "file genuinely doesn't exist yet"
	// (see its own doc comment), so this is never reached by a true
	// fresh install. This used to be the one store in this boot
	// sequence that failed closed at all: falling through with
	// Count()==0 is exactly the state requireAuth treats as "no
	// decision made yet," silently presenting a stranger with the
	// first-run setup wizard on a previously-authenticated instance. It
	// no longer needs to be a special case -- mustOpenStore now applies
	// the same refusal to every persisted store below, for the same
	// underlying reason (see #378): an unreadable-but-present document
	// must never be treated as a blank one to build a live-backed store
	// around.
	mustOpenStore(authLog, err)
	switch {
	case authStore.Count() > 0:
		authLog.Info(fmt.Sprintf("%d account(s) registered -- authentication is active", authStore.Count()))
	default:
		authLog.Info("no account yet -- mikroview is showing the create-account screen (see docs/configuration.md)")
	}

	// entities (issue #107): the persisted, admin-manageable (type, key)
	// -> label/tags store backing GET/POST/DELETE /api/entities -- the
	// shared foundation a future mail-sender allowlist and UI-managed
	// IP/port/rule aliasing both build on. Seed is the one-time upgrade
	// path: an existing deployment's YAML-only cfg.RuleNames/HostNames
	// become UI-editable Entity records the first time it boots against
	// an empty store, and never re-import again afterward (even if a
	// user later deletes every one of them) -- see Store.Seed's own doc
	// comment.
	entitiesLog := logging.New("entities")
	entityBackend, err := persistence.backendFor(bootCtx, "entities", cfg.Entities.StorePath)
	if err != nil {
		entitiesLog.Warn(err.Error())
	}
	entityStore, err := entities.OpenWithBackend(entityBackend)
	mustOpenStore(entitiesLog, err)
	if n := entityStore.Seed(cfg.RuleNames, cfg.HostNames); n > 0 {
		entitiesLog.Info(fmt.Sprintf("imported %d entries from config.yaml's ruleNames/hostNames (now UI-editable)", n))
	}

	// Tokens (issue #101): read-only API bearer tokens for service-to-
	// service access. Persistence itself is optional -- a missing/
	// unconfigured path just means token creation refuses with
	// ErrTokenNotPersisted, not that mikroview fails to start -- but a
	// document that exists and can't be loaded is not that case; see
	// mustOpenStore.
	tokensLog := logging.New("tokens")
	tokensBackend, err := persistence.backendFor(bootCtx, "tokens", cfg.Auth.TokensStorePath)
	if err != nil {
		tokensLog.Warn(err.Error())
	}
	tokenStore, err := auth.OpenTokenStoreWithBackend(tokensBackend)
	mustOpenStore(tokensLog, err)

	// Audit (issue #112): the persisted admin-action accountability log.
	// Persistence itself is optional -- a missing/unconfigured path just
	// means entries don't survive a restart -- but a document that
	// exists and can't be loaded is not that case; see mustOpenStore.
	auditLog := logging.New("audit")
	auditBackend, err := persistence.backendFor(bootCtx, "audit", cfg.Audit.StorePath)
	if err != nil {
		auditLog.Warn(err.Error())
	}
	auditStore, err := audit.OpenWithBackend(auditBackend)
	mustOpenStore(auditLog, err)

	// The watchlist entry set (#243). Persistence itself is optional; a
	// document that exists and can't be loaded is not that case -- see
	// mustOpenStore.
	watchlistLog := logging.New("watchlist")
	watchlistBackend, err := persistence.backendFor(bootCtx, "watchlist", cfg.Watchlist.StorePath)
	if err != nil {
		watchlistLog.Warn(err.Error())
	}
	watchlistStore, err := watchlist.OpenWithBackend(watchlistBackend)
	mustOpenStore(watchlistLog, err)

	// The suggestion candidate pool (#243 slice 5): watchlist entries
	// suggested from data RouterOS has already pushed. Persistence
	// itself is optional -- losing this on restart just means every
	// candidate regenerates at Off, which loses an operator's
	// Hide/accept-in-progress state but never anything RunPeriodicSync
	// (started below, once routerState exists) can't rebuild -- but a
	// document that exists and can't be loaded is not that case; see
	// mustOpenStore.
	suggestLog := logging.New("suggest")
	suggestBackend, err := persistence.backendFor(bootCtx, "suggestions", cfg.Watchlist.SuggestionsStorePath)
	if err != nil {
		suggestLog.Warn(err.Error())
	}
	suggestStore, err := suggest.OpenWithBackend(suggestBackend)
	mustOpenStore(suggestLog, err)

	// The watchlist's match log has no in-memory-only mode (durability
	// is the entire point of it, see internal/matchlog's package doc
	// comment), so a failure here is handled differently from every
	// store above: not fatal (the rest of mikroview -- the event ring,
	// live view, flags, detectors -- is completely unaffected by this),
	// but not silently degraded either. matchLog stays nil and every
	// event skips watchlist evaluation entirely for this run, which
	// ingestOneRecovered's nil check makes an explicit, visible no-op
	// rather than a mysteriously empty watchlist page.
	//
	// On Postgres, this is a dedicated table (internal/matchlog.
	// PostgresStore), not a row in store_blob -- see
	// docs/decisions/postgres-backend.md §1a -- bounded by
	// MatchLogRetention (age) rather than MatchLogCapacity (count), and
	// already migrated by persistence.pool.Migrate above alongside every
	// other store's schema. The file backend is unaffected either way.
	var matchLog matchlog.Store
	// Nil unless the Postgres backend is in use -- RunPeriodicPurge is
	// started later, alongside every other periodic background task,
	// once the shutdown-aware ctx exists (see below).
	var matchLogPostgres *matchlog.PostgresStore
	if persistence.pool != nil {
		matchLogPostgres = matchlog.OpenPostgres(persistence.pool, cfg.Watchlist.MatchLogRetention)
		matchLog = matchLogPostgres
		// Unlike every other store, there is no adoption path from the
		// file backend's JSONL format into the Postgres table -- every
		// other store's JSON blob round-trips byte-identically (see
		// persistence.backendFor/persist.AdoptFile), but the match log's
		// append-only line format doesn't fit that migration, and a
		// backfill migrator was judged not worth building for what is,
		// by design, bounded evidence rather than durable account state.
		// Said plainly rather than left to be discovered as "why is my
		// match history gone": the old file is untouched and still
		// readable by reverting Postgres, it just isn't carried forward.
		if fi, err := os.Stat(cfg.Watchlist.MatchLogPath); err == nil && fi.Size() > 0 {
			logging.New("matchlog").Warn(fmt.Sprintf("an existing match log at %s (%d bytes) will NOT be migrated into Postgres -- "+
				"unlike every other store, the match log has no file-to-Postgres adoption path. It starts empty on Postgres; "+
				"the old file is untouched and still readable if you revert postgres.dsnFile",
				cfg.Watchlist.MatchLogPath, fi.Size()))
		}
	} else if ml, err := matchlog.Open(cfg.Watchlist.MatchLogPath, cfg.Watchlist.MatchLogCapacity); err != nil {
		logging.New("matchlog").Error(fmt.Sprintf("opening the match log at %s failed: %v -- watchlist entries will not record any matches until this is fixed and mikroview is restarted", cfg.Watchlist.MatchLogPath, err))
	} else {
		matchLog = ml
		defer ml.Close()
	}
	// Evaluates the watchlist asynchronously off the ingest goroutine --
	// see Evaluator's doc comment for why, and detector.Run just below
	// for the identical existing pattern this mirrors. A nil matchLog
	// (opened above) makes every Enqueue call a documented no-op rather
	// than queuing work that could never complete.
	watchlistEval := watchlist.NewEvaluator(watchlistStore, matchLog)

	// eng is the evaluation chassis (issue #398, part of the v0.3.0
	// unification -- see docs/decisions/evaluation-engine.md): one ingest
	// queue, one backpressure policy, one lifecycle, one panic boundary.
	// It holds no definitions yet (that starts with #401), so wiring it
	// in alongside detector/watchlistEval is a no-behaviour-change
	// change -- it evaluates nothing, it just now also receives every
	// stored event. detect.Detector and watchlist.Evaluator collapse
	// onto it in later issues; this one only adds it.
	eng := engine.New()

	// engineState (#399/#400) persists every definition's per-key
	// Baseline state -- opened here, under the same fail-closed
	// persist.Open contract every other store uses, even though nothing
	// registers a definition against it yet (#401/#405's job). Opening
	// it now, rather than waiting for the first real definition, means
	// mikroview refuses to start on a corrupted engine-state document
	// from day one instead of only once something actually depends on
	// reading it correctly.
	engineStateLog := logging.New("engine-state")
	engineStateBackend, err := persistence.backendFor(bootCtx, "engine_state", cfg.Engine.StorePath)
	if err != nil {
		engineStateLog.Warn(err.Error())
	}
	engineState, err := engine.OpenStateStoreWithBackend(engineStateBackend)
	mustOpenStore(engineStateLog, err)

	// definitions (#404) is the one document holding every definition --
	// shipped detectors, watchlist expectations, and eventually
	// builder-authored custom ones. On a not-yet-existing document, it is
	// seeded once from internal/detect's settings store and
	// internal/watchlist's entries store (engine.MigrateDefinitions),
	// fail-closed and non-destructive: neither old store's document is
	// touched, and both keep working exactly as before until #405/#406
	// port their evaluation logic onto this chassis and retire them. A
	// second backend handle for the detector-settings store is opened
	// here purely to read its document for migration -- watchlistBackend
	// above is reused as-is; detect.OpenSettingsStoreWithBackend below
	// opens its own handle for live use the same way it always has.
	// persistence.backendFor is safe to call more than once for the same
	// store name (its one-time Postgres adoption step is itself
	// idempotent -- see storage.backendFor's own doc comment).
	definitionsLog := logging.New("definitions")
	definitionsBackend, err := persistence.backendFor(bootCtx, "definitions", cfg.Engine.DefinitionsStorePath)
	if err != nil {
		definitionsLog.Warn(err.Error())
	}
	migrationDetectorBackend, err := persistence.backendFor(bootCtx, "detector_settings", cfg.Flags.DetectorSettingsStorePath)
	if err != nil {
		definitionsLog.Warn(err.Error())
	}
	if _, err := engine.MigrateDefinitions(bootCtx, definitionsBackend, migrationDetectorBackend, watchlistBackend); err != nil {
		if errors.Is(err, engine.ErrMigrationWriteFailed) {
			// Nothing was lost -- see ErrMigrationWriteFailed's own doc
			// comment: neither source was touched, and the definitions
			// document still does not exist either way, so this is
			// retried automatically on the next restart once whatever
			// blocked the write (a missing/unwritable data directory, a
			// momentarily unreachable Postgres) is fixed. Same
			// log-and-continue severity every other store here gives an
			// ordinary "can't currently reach my backend" failure.
			definitionsLog.Warn(err.Error() + " -- continuing without a migrated definitions store; this is retried automatically on the next restart")
		} else {
			// An unreadable/corrupt source, or a conversion that could
			// not be trusted to be complete -- issue #404's fail-closed
			// contract: refuse to start rather than risk ever writing a
			// partial or wrong definitions document.
			mustOpenStore(definitionsLog, err)
		}
	}
	definitions, err := engine.OpenDefinitionsStoreWithBackend(definitionsBackend)
	mustOpenStore(definitionsLog, err)

	detectCfg := detect.Config{
		PortScanThreshold:        cfg.Flags.PortScanThreshold,
		PortScanWindow:           cfg.Flags.PortScanWindow,
		ActivitySpikeThreshold:   cfg.Flags.ActivitySpikeThreshold,
		ActivitySpikeWindow:      cfg.Flags.ActivitySpikeWindow,
		CriticalPorts:            cfg.Flags.CriticalPorts,
		CriticalPortThreshold:    cfg.Flags.CriticalPortThreshold,
		CriticalPortWindow:       cfg.Flags.CriticalPortWindow,
		GlobalSpikeMultiplier:    cfg.Flags.GlobalSpikeMultiplier,
		GlobalSpikeMinEPS:        cfg.Flags.GlobalSpikeMinEPS,
		GlobalSpikeWarmupSamples: cfg.Flags.GlobalSpikeWarmupSamples,

		DistributedBruteForceThreshold: cfg.Flags.DistributedBruteForceThreshold,
		DistributedBruteForceWindow:    cfg.Flags.DistributedBruteForceWindow,

		OutboundAnomalyThreshold: cfg.Flags.OutboundAnomalyThreshold,
		OutboundAnomalyWindow:    cfg.Flags.OutboundAnomalyWindow,

		InternalReconThreshold: cfg.Flags.InternalReconThreshold,
		InternalReconWindow:    cfg.Flags.InternalReconWindow,

		RuleSpikeMultiplier:    cfg.Flags.RuleSpikeMultiplier,
		RuleSpikeMinRate:       cfg.Flags.RuleSpikeMinRate,
		RuleSpikeWindow:        cfg.Flags.RuleSpikeWindow,
		RuleSpikeWarmupSamples: cfg.Flags.RuleSpikeWarmupSamples,

		RepeatedDropsThreshold: cfg.Flags.RepeatedDropsThreshold,
		RepeatedDropsWindow:    cfg.Flags.RepeatedDropsWindow,

		HostActivityMultiplier:    cfg.Flags.HostActivityMultiplier,
		HostActivityWarmupSamples: cfg.Flags.HostActivityWarmupSamples,

		LowSlowScanWindow:             cfg.Flags.LowSlowScanWindow,
		LowSlowScanPortThreshold:      cfg.Flags.LowSlowScanPortThreshold,
		LowSlowScanHostThreshold:      cfg.Flags.LowSlowScanHostThreshold,
		LowSlowScanMinObservation:     cfg.Flags.LowSlowScanMinObservation,
		LowSlowScanDropRatio:          cfg.Flags.LowSlowScanDropRatio,
		LowSlowScanBaselineMultiplier: cfg.Flags.LowSlowScanBaselineMultiplier,

		OffHoursStartHour:     cfg.Flags.OffHoursStartHour,
		OffHoursEndHour:       cfg.Flags.OffHoursEndHour,
		OffHoursMinSampleDays: cfg.Flags.OffHoursMinSampleDays,
		OffHoursMinCount:      cfg.Flags.OffHoursMinCount,

		DeviceStaleAfter: cfg.Flags.DeviceStaleAfter,

		VPNInterfaces:           cfg.Flags.VPNInterfaces,
		VPNConfidenceMultiplier: cfg.Flags.VPNConfidenceMultiplier,
	}
	seed := detect.DefaultSettingsMap()
	for name, ds := range cfg.Flags.Detectors {
		seed[detect.DetectorName(name)] = detect.Settings{
			Enabled: ds.Enabled,
			Scope: detect.Scope{
				Hosts:          ds.Scope.Hosts,
				HostsMode:      detect.ListMode(ds.Scope.HostsMode),
				Ports:          ds.Scope.Ports,
				PortsMode:      detect.ListMode(ds.Scope.PortsMode),
				Classification: store.Scope(ds.Scope.Classification),
				Rules:          ds.Scope.Rules,
				RulesMode:      detect.ListMode(ds.Scope.RulesMode),
			},
		}
	}
	detectorsLog := logging.New("detectors")
	detectorBackend, err := persistence.backendFor(bootCtx, "detector_settings", cfg.Flags.DetectorSettingsStorePath)
	if err != nil {
		detectorsLog.Warn(err.Error())
	}
	detectorSettings, err := detect.OpenSettingsStoreWithBackend(detectorBackend, seed)
	mustOpenStore(detectorsLog, err)
	// Every shipped definition this binary evaluates has to actually
	// exist, whatever the persistence situation -- see
	// engine.SeedShippedDefinitions' own doc comment for why this runs
	// every boot and is not the same thing as MigrateDefinitions running
	// once. Anything already in the store (a migration's output, an
	// operator's edits) is left untouched; only genuinely missing
	// definitions are added, using this deployment's live detector
	// settings for enabled/scope so a detector switched off before the
	// port stays off after it.
	shippedDefaults := engine.ShippedDefaults{
		Config:                 detectCfg,
		StaleRuleMaxAge:        time.Duration(cfg.Flags.StaleRuleDays) * 24 * time.Hour,
		StaleRuleCheckInterval: cfg.Flags.StaleRuleCheckInterval,
	}
	if err := engine.SeedShippedDefinitions(definitions, detectorSettings.List(), shippedDefaults); err != nil {
		definitionsLog.Warn(err.Error())
	}
	// bl (issue #113 Part B): always constructed, even with zero enabled
	// sources (cfg.Blocklist.Sources == []) -- Match/Refresh are both
	// harmless no-ops in that case (see internal/blocklist.Blocklist's
	// doc comment), same "always non-nil, off means inert" convention
	// Reputation/Auth/Entities already use elsewhere in this file. Only
	// the refresh goroutines below are actually skipped when disabled
	// (see bl.HasFeeds()), so a fully-disabled deployment starts zero
	// extra goroutines for this feature.
	blocklistLog := logging.New("blocklist")
	bl := blocklist.New(cfg.Blocklist.Sources, blocklistLog)

	// netclass (issue #114): local IP attribution for the manual lookup
	// popover -- Tor exit / VPN / datacenter / privacy relay. The
	// netclass package itself stays display-only by design (see its own
	// doc comment) and is attached to the API server for that. It is
	// also handed to the engine's netclass definition below, but narrowly:
	// only that definition ever reads it, and only to reinforce confidence
	// on an already-active flag for the two high-precision categories
	// (Tor, VPN), direction-gated to inbound traffic only -- never to
	// raise a flag on its own. Nil-safe when no sources are enabled, same
	// as bl.
	netclassLog := logging.New("netclass")
	nc := netclass.New(cfg.NetClass.Sources, netclassLog)

	// Every optional input internal/detect used to consult -- the
	// trusted-mail-sender allowlist (#108), the local blocklist match
	// (#113 Part B), the direction-aware VPN/Tor reinforcement (#114) --
	// now reaches its definition through ShippedDeps below (issue #405).
	// The detector itself evaluates nothing as of this commit; it is
	// deleted, along with the rest of internal/detect's engine machinery,
	// once the last port lands.
	detector := detect.NewWithSettings(detectCfg, fs, detectorSettings)

	// Shipped declarative definitions (issue #405): built from whatever
	// the definitions store currently holds for a shipped, available,
	// declarative-kind definition, wrapped in one DeclarativeSet (its own
	// dispatch pre-index, see internal/engine/dispatch.go) and registered
	// on the engine -- port_scan, critical_port, repeated_drops and
	// distributed_brute_force are ported this way so far
	// (docs/decisions/evaluation-engine.md section 2,
	// internal/engine/shipped_declarative.go's shippedDeclarativeBuilders);
	// every other shipped detector still runs through internal/detect
	// below until its own #405 port lands. An empty/not-yet-migrated
	// definitions store (definitions.List() returns nothing) is a valid,
	// common state -- see MigrateDefinitions's own doc comment -- and
	// simply means this DeclarativeSet starts out evaluating nothing,
	// same as registering an empty one on a freshly-constructed Engine.
	// Each definition's sink raises into fs and, for a newly-raised
	// episode, kicks off the same best-effort async reputation lookup
	// internal/detect's WithReputation-configured detectors have always
	// had -- single-address or group-sampling, chosen per definition by
	// engine.ShippedDeclarativeSink, mirroring internal/detect's own
	// maybeCheckReputation/maybeCheckGroupReputation split. A definition
	// with no address to look up (a rule-label or "global" target) is
	// simply never a lookup candidate.
	//
	// The lookup policy (pool size, timeout, group sampling) comes from
	// the shipped reputation definition's own params rather than from a
	// literal here -- see engine.ReputationPolicyFrom. This file used to
	// carry a hand-synced copy of internal/detect's unexported
	// concurrency constant; the definition is what replaces it.
	reputationPolicy := engine.ReputationPolicyFrom(definitions)
	var shippedDeclDefs []*engine.DeclarativeDefinition
	for _, sd := range definitions.List() {
		if !sd.Available || sd.Definition.Kind != engine.KindDeclarative || sd.Definition.Provenance.Origin != engine.ProvenanceShipped {
			continue
		}
		dd, err := engine.BuildShippedDeclarativeDefinition(sd.Definition)
		if err != nil {
			// Not every shipped-provenance declarative definition
			// necessarily has a registered builder yet (a stale/future
			// entry outside this binary's current shipped catalogue) --
			// logged and skipped, not fatal: the rest of the shipped set,
			// and every programmatic/legacy definition still running
			// through internal/detect below, keeps working.
			detectorsLog.Warn(fmt.Sprintf("skipping shipped declarative definition %q: %v", sd.Definition.ID, err))
			continue
		}
		dd.OnRoutedEmission = engine.ShippedDeclarativeSink(sd.Definition, fs, rep, reputationPolicy)
		shippedDeclDefs = append(shippedDeclDefs, dd)
	}
	eng.Register(engine.NewDeclarativeSet("shipped-declarative", shippedDeclDefs))

	// Shipped programmatic definitions (issue #405): built-in Go wearing
	// the same envelope, for the detectors that cannot honestly be a
	// form -- statistical baselines, absence-of-events checks,
	// external-data lookups (see internal/engine/programmatic.go).
	// Registered one at a time rather than behind a set: unlike the
	// declarative kind there is no dispatch pre-index to share, and one
	// registration per definition is what gives each its own panic
	// boundary and its own fault report.
	//
	// Everything a programmatic definition may need beyond the event
	// stream arrives through ShippedDeps as a narrow interface, so the
	// concrete stores stay behind adapters this file owns (see
	// blocklistLookup and friends at the bottom of this file). Every
	// field is optional: a deployment with no blocklist sources, no
	// netclass sources and no entity store still builds the whole
	// catalogue, with the definitions that need those simply never
	// firing.
	deps := engine.ShippedDeps{
		Flags:    engine.FlagsConfidenceFloorRaiser(fs),
		Entities: entityTagLookup{es: entityStore},
		KnownBad: blocklistLookup{bl: bl},
		NetClass: netClassLookup{nc: nc},
		Devices:  deviceLister{reg: devices},
		Rules:    staleRuleLister{ru: ru},
		Rate:     eventRateSource{st: st},
		State:    engineState,
	}
	for _, sd := range definitions.List() {
		if !sd.Available || sd.Definition.Kind != engine.KindProgrammatic || sd.Definition.Provenance.Origin != engine.ProvenanceShipped {
			continue
		}
		pd, err := engine.BuildShippedProgrammaticDefinition(sd.Definition, deps)
		if err != nil {
			// Expected, and not a warning, for every detector #405 has
			// not ported yet: it has a shipped definition (the migration
			// created one for all twelve) but no Go logic registered on
			// this chassis, because internal/detect below is still the
			// thing evaluating it. Logged at info so the shrinking list
			// is visible during the port without reading as a fault.
			detectorsLog.Info(fmt.Sprintf("shipped programmatic definition %q is not evaluated by the engine: %v", sd.Definition.ID, err))
			continue
		}
		if sink, ok := pd.(interface {
			SetSink(func(engine.RoutedEmission))
		}); ok {
			sink.SetSink(engine.ShippedDeclarativeSink(sd.Definition, fs, rep, reputationPolicy))
		}
		eng.Register(pd)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Notify (issues #30/#31/#96): alerting on newly-raised flags outside the
	// UI, through whichever channels are configured -- each independently
	// enabled by its own identifying field being set (same "empty means
	// off" convention as Reputation.AbuseIPDBKey/GeoIP.DBPath), sharing
	// one Dispatcher/BatchWindow. No dispatcher goroutine is started at
	// all if nothing is configured.
	var notifiers []notify.Notifier
	if cfg.Notify.SMTP.Host != "" {
		notifiers = append(notifiers, notify.NewSMTPNotifier(notify.SMTPConfig{
			Host:     cfg.Notify.SMTP.Host,
			Port:     cfg.Notify.SMTP.Port,
			Username: cfg.Notify.SMTP.Username,
			Password: cfg.Notify.SMTP.Password,
			TLSMode:  notify.TLSMode(cfg.Notify.SMTP.TLSMode),
			From:     cfg.Notify.SMTP.From,
			To:       cfg.Notify.SMTP.To,
		}))
	}
	if cfg.Notify.Pushover.Token != "" {
		notifiers = append(notifiers, notify.NewPushoverNotifier(notify.PushoverConfig{
			Token: cfg.Notify.Pushover.Token,
			User:  cfg.Notify.Pushover.User,
		}))
	}
	if cfg.Notify.Webhook.URL != "" {
		notifiers = append(notifiers, notify.NewWebhookNotifier(notify.WebhookConfig{
			URL:     cfg.Notify.Webhook.URL,
			Headers: cfg.Notify.Webhook.Headers,
		}))
	}
	if len(notifiers) > 0 {
		dispatcher := notify.NewDispatcher(cfg.Notify.BatchWindow, notifiers)
		go dispatcher.Run(ctx)
		fs.WithOnRaise(dispatcher.Enqueue)
	}

	raw := make(chan syslog.RawMessage, 4096)

	// Entities takes precedence over Rules/Hosts for any key it has a
	// label for -- see naming.Resolver's doc comment and issue #107's
	// migration/precedence design.
	// routerState (issue #186 step 4): each device's most recent pushed
	// state, in-memory only by that package's design. Wired into the
	// naming resolver first (RouterOS always wins on host names -- the
	// owner's 4c decision) and into the API server for the ingest
	// endpoint to write and the table endpoints to read.
	routerState := routerstate.New()

	// What the guided setup wizard (#320) has actually observed of each
	// router's setup. Hooked into the syslog accept path here rather
	// than inside internal/syslog, which has no business knowing what a
	// wizard is.
	setupStore := setup.New()
	syslog.SetOnConnection(func(host string) { setupStore.NoteSyslogConnection(host, time.Now()) })
	// What makes an entry scoped to a router's address list resolvable
	// at match time (#274 item 2). Wired here rather than at
	// construction because routerState does not exist yet up there --
	// and safe to do late because the evaluator does not start
	// consuming until Run below.
	watchlistEval.WithAddressLists(routerState)
	names := naming.Resolver{Rules: cfg.RuleNames, Hosts: cfg.HostNames, Entities: entityStore, RouterHosts: routerState}

	go ingest(ctx, raw, st, devices, macRegistry, fs, h, geo, detector, ru, names, watchlistEval, eng, setupStore)
	go detector.Run(ctx)
	go watchlistEval.Run(ctx)
	go eng.Run(ctx)
	// One driver for every Ticked definition (issue #405). Deliberately
	// one goroutine at the finest cadence any shipped definition
	// declares, not one goroutine per definition: Engine.Tick honours
	// each definition's own TickInterval, so internal/detect's three
	// separate tickers (10s global spike, 1m device silence, an
	// operator-configured stale-rule sweep) keep their individual rates
	// while sharing one timer and one ordering.
	go func() {
		ticker := time.NewTicker(engineTickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				eng.Tick(time.Now())
			}
		}
	}()
	go suggestStore.RunPeriodicSync(ctx, routerState, suggestSyncInterval)
	if matchLogPostgres != nil {
		go matchLogPostgres.RunPeriodicPurge(ctx, matchLogPurgeInterval)
	}

	// The global-spike ticker moved onto the engine (issue #405): it is a
	// shipped programmatic definition now, driven by Engine.Tick at its
	// own declared TickInterval alongside every other Ticked definition.

	// The device-silence ticker moved onto the engine with the
	// global-spike one (issue #405): device_silence is a shipped
	// programmatic definition now, driven by Engine.Tick at its own
	// declared TickInterval.

	// The stale-rule sweep moved onto the engine with the global-spike and
	// device-silence tickers (issue #102/#405): stale_rule is a shipped
	// programmatic definition now, and its cadence -- which was always
	// operator-set, config.Flags.StaleRuleCheckInterval -- is a param it
	// declares through Ticked.TickInterval.

	// Local blocklist refresh (issue #113 Part B): a fixed daily cycle
	// (blocklist.RefreshInterval, not configurable -- see that const's
	// doc comment), same ticker/select/recover shape as the stale-rule
	// sweep just above. Skipped entirely if no source is enabled
	// (bl.HasFeeds() false), so a deployment that's disabled this
	// feature via `blocklist.sources: []` starts zero extra goroutines
	// for it. The first Refresh runs immediately, in its own goroutine,
	// rather than blocking startup on Spamhaus/ET's reachability --
	// same "never block startup on an optional external integration"
	// contract GeoIP/Auth/Rules above already have; until it completes,
	// Blocklist.Match just reports no matches (see that method's own
	// doc comment), not an error.
	if bl.HasFeeds() {
		go func() {
			defer logging.Recover(blocklistLog)
			bl.Refresh(ctx)
		}()
		go func() {
			ticker := time.NewTicker(blocklist.RefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					func() {
						defer logging.Recover(blocklistLog)
						bl.Refresh(ctx)
					}()
				}
			}
		}()
	}

	// netclass refresh (issue #114): same shape as the blocklist sweep
	// above, with one addition -- a per-install random jitter before the
	// first fetch. Thousands of self-hosted instances all refreshing at
	// 00:00 UTC would be a thundering herd against raw.githubusercontent.
	// com and would get collectively rate-limited; a random offset up to
	// an hour spreads them out. The daily ticker then keeps that offset.
	if nc.HasSources() {
		go func() {
			defer logging.Recover(netclassLog)
			jitter := time.Duration(rand.Int64N(int64(time.Hour)))
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter):
			}
			nc.Refresh(ctx)
			ticker := time.NewTicker(netclass.RefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					func() {
						defer logging.Recover(netclassLog)
						nc.Refresh(ctx)
					}()
				}
			}
		}()
	}

	// SSO (issue #43): additive on top of local auth, never a
	// replacement -- see internal/oidc and auth.Store.
	// FindOrCreateOIDCUser. A misconfigured or unreachable provider must
	// never take down local login, so every failure path here just
	// leaves srv.OIDC nil (SSO unavailable, 404 on its routes) rather
	// than exiting -- the same degrade-not-crash contract GeoIP/Flags/
	// Auth/DetectorSettings already have above for their own optional
	// persistence/integrations.
	var oidcClient *oidc.Client
	var oidcState *oidc.StateCodec
	oidcLog := logging.New("oidc")
	oidcPolicy := oidc.Policy{
		AllowedGroups:       cfg.OIDC.AllowedGroups,
		GroupsClaim:         cfg.OIDC.GroupsClaim,
		AllowedEmails:       cfg.OIDC.AllowedEmails,
		AllowedEmailDomains: cfg.OIDC.AllowedEmailDomains,
		RequiredClaims:      cfg.OIDC.RequiredClaims,
	}

	switch {
	case cfg.OIDC.IssuerURL == "":
		// Not configured -- no log line, same as every other disabled-
		// by-default optional integration (GeoIP, Reputation, Notify).
	case cfg.OIDC.PublicBaseURL == "":
		oidcLog.Error("oidc.issuerUrl is set but oidc.publicBaseUrl is not -- SSO login is unavailable until it's configured (see docs/configuration.md)")
	case cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "":
		oidcLog.Error("oidc.issuerUrl is set but oidc.clientId/oidc.clientSecret are not -- SSO login is unavailable until both are configured")
	case oidc.AllowIssuer(cfg.OIDC.IssuerURL) != nil:
		// Refused outright, not warned about, and deliberately not
		// rescuable by configuration -- see oidc.AllowIssuer. Leaving SSO
		// off is the fail-closed outcome; local login is unaffected.
		oidcLog.Error(fmt.Sprintf(
			"%s is a multi-tenant provider and is not supported -- mikroview only supports self-hosted identity providers "+
				"(Authentik, Keycloak, Zitadel, or an Entra single-tenant issuer URL), where the issuer itself restricts who can "+
				"sign in. SSO login is unavailable; local login is unaffected. See docs/configuration.md",
			cfg.OIDC.IssuerURL))
	default:
		client, err := oidc.New(ctx, oidc.Config{
			IssuerURL:    cfg.OIDC.IssuerURL,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			// PublicBaseURL, not a request's Host header -- see
			// config.OIDC.PublicBaseURL's doc comment for why deriving
			// this from client-influenced input would be a real
			// redirect_uri-confusion vulnerability.
			RedirectURL: strings.TrimRight(cfg.OIDC.PublicBaseURL, "/") + "/api/auth/oidc/callback",
			Scopes:      cfg.OIDC.Scopes,
		})
		if err != nil {
			oidcLog.Error(fmt.Sprintf("%v (SSO login is unavailable)", err))
		} else if state, err := oidc.NewStateCodec(); err != nil {
			oidcLog.Error(fmt.Sprintf("%v (SSO login is unavailable)", err))
		} else {
			oidcClient, oidcState = client, state
			if oidcPolicy.Restricted() {
				oidcLog.Info(fmt.Sprintf("SSO login active against %s, restricted to permitted accounts", cfg.OIDC.IssuerURL))
			} else {
				oidcLog.Info(fmt.Sprintf("SSO login active against %s for any account that issuer vouches for", cfg.OIDC.IssuerURL))
			}
		}
	}

	// A bad trusted-proxy entry is a security-relevant misconfiguration,
	// not a typo to paper over: silently ignoring it would leave the
	// operator believing forwarded addresses are being honoured when they
	// aren't. Refusing to start is the same stance the TLS and OIDC
	// blocks already take for their own required values.
	proxyLog := logging.New("proxy")
	trustedProxies, err := config.ParseTrustedProxies(cfg.Listen.TrustedProxies)
	if err != nil {
		proxyLog.Error(fmt.Sprintf("listen.trustedProxies: %v", err))
		os.Exit(1)
	}
	if len(trustedProxies) > 0 {
		header := cfg.Listen.ClientIPHeader
		if header == "" {
			header = "X-Forwarded-For"
		}
		proxyLog.Info(fmt.Sprintf("trusting %s from %d proxy range(s) for login rate limiting", header, len(trustedProxies)))
	}

	// Logged here and surfaced in the admin UI (see api.Server.
	// ConfigProblems). The log line alone is not enough: it is seen once,
	// by whoever ran `docker compose up`, and never again -- which is not
	// good enough for a value the operator believes is in effect.
	var configProblems []api.ConfigProblem
	for _, p := range configResult.Warnings {
		msg := fmt.Sprintf("%s: %s", p.Key, p.Message)
		if p.Applied != "" {
			msg += fmt.Sprintf(" -- using %s instead", p.Applied)
		}
		configLog.Warn(msg)
		configProblems = append(configProblems, api.ConfigProblem{
			Code: p.Code, Key: p.Key, Message: p.Message,
			Applied: p.Applied, Remediation: p.Remediation,
		})
	}

	srv := &api.Server{
		Store:             st,
		Devices:           devices,
		Setup:             setupStore,
		Hub:               h,
		Reputation:        rep,
		NetClass:          nc,
		Flags:             fs,
		DetectorSettings:  detectorSettings,
		Entities:          entityStore,
		Rules:             ru,
		Audit:             auditStore,
		Watchlist:         watchlistStore,
		Suggest:           suggestStore,
		DefaultWatchPorts: cfg.Flags.CriticalPorts,
		MatchLog:          matchLog,
		DeviceStaleAfter:  cfg.Flags.DeviceStaleAfter,
		Auth:              authStore,
		Sessions:          auth.NewSessionStoreWithMaxLifetime(cfg.Auth.SessionTTL, cfg.Auth.SessionMaxLifetime),
		LoginLimiter:      auth.NewLoginLimiter(loginLimiterThreshold, loginLimiterWindow),
		SecureCookie:      cfg.Auth.SecureCookie,
		TrustedProxies:    trustedProxies,
		ClientIPHeader:    cfg.Listen.ClientIPHeader,
		Tokens:            tokenStore,
		IngestLimiter:     auth.NewLoginLimiter(ingestLimiterThreshold, ingestLimiterWindow),
		RouterState:       routerState,
		SetupInstance: api.SetupInstance{
			TLSEnabled: cfg.TLS.Enabled,
			Hosts:      cfg.TLS.Hosts,
			SyslogPort: cfg.Listen.SyslogTLS,
		},
		OIDC:              oidcClient,
		OIDCState:         oidcState,
		OIDCPolicy:        oidcPolicy,
		StartTime:         time.Now(),
		Version:           version,
		ThirdPartyNotices: thirdPartyNotices,
		ConfigProblems:    configProblems,
	}

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", srv.Routes())
	if frontend, err := web.DistFS(); err != nil {
		logging.New("frontend").Warn(fmt.Sprintf("%v (serving API only)", err))
	} else {
		// A binary can compile with an empty dist/ -- that is what the
		// committed .gitkeep is for -- and http.FileServer would then
		// answer / with a directory listing of that one placeholder,
		// which reads as a broken install rather than a build step that
		// was skipped. Say which it is, in the log and in the response
		// (#353). The API is mounted above and keeps working either way.
		//
		// Either way it goes out through staticCacheHeaders (#347), so
		// the "no frontend" page is itself revalidated rather than kept
		// by a browser after a proper build is deployed.
		var ui http.Handler = http.FileServer(http.FS(frontend))
		if !web.HasUI() {
			logging.New("frontend").Warn("no frontend was built into this binary (run `make build`) -- serving API only")
			ui = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "no frontend was built into this binary -- the API is available under /api/", http.StatusServiceUnavailable)
			})
		}
		rootMux.Handle("/", staticCacheHeaders(ui))
	}

	httpServer := &http.Server{
		Addr: cfg.Listen.HTTP,
		// HSTS is opt-in, not tied to tls.enabled alone: it commits a
		// browser to HTTPS-only for this exact host for the whole
		// max-age, which is a worse failure mode than usual for a
		// self-hosted appliance if the operator later changes hostname
		// or drops back to plain HTTP -- only worth that trade-off once
		// they've supplied their own real certificate (cfg.TLS.CertFile
		// set), not for the self-generated default every fresh install
		// starts with.
		Handler: securityHeaders(rootMux, cfg.TLS.Enabled && cfg.TLS.CertFile != ""),
		// Bounds a slow client trickling headers/body in to tie up a
		// connection indefinitely (the WS listener, syslog listeners, and
		// hub already have their own backpressure/deadline handling --
		// this was the one place in the request path without any). None
		// of these continue to apply once /api/ws hijacks a connection:
		// the WS handler manages its own read/write deadlines from that
		// point on (see internal/api/ws.go), and Go's server stops
		// enforcing these on a connection once it's been hijacked.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Routes Go's own internal server diagnostics (TLS handshake
		// errors from misbehaving clients, etc.) through the same
		// formatted/leveled output as everything mikroview logs itself,
		// rather than the stdlib default logger's unformatted stderr
		// lines being the one exception.
		// Translated and de-duplicated rather than passed straight
		// through -- see logging.HTTPErrorLog (#321/#322).
		ErrorLog: logging.HTTPErrorLog(logging.New("http")),
	}

	// TLS (on by default -- see internal/config.TLS's doc comment for
	// why, and the one documented reason to disable it). /ca.crt is
	// registered directly on rootMux, not routed through api.Server,
	// since it's not an API concern -- and only when mikroview generated
	// its own CA, never for an operator-supplied cert.
	//
	// The certificate is loaded whenever *either* the HTTPS listener or
	// syslog TLS (issue #188) needs one, not only when cfg.TLS.Enabled --
	// a deployment that disables mikroview's own HTTP TLS because its
	// own reverse proxy terminates TLS for real clients would otherwise
	// lose syslog ingest entirely once #189 removed the plaintext
	// fallback: RouterOS connects to the syslog port directly, never
	// through that proxy, so it needs a certificate to trust either way.
	tlsLog := logging.New("tls")
	scheme := "http"
	var cert tls.Certificate
	var certReloader *servertls.Reloader
	if cfg.TLS.Enabled || cfg.Listen.SyslogTLS != "" {
		tlsCfg := servertls.Config{
			CertFile:  cfg.TLS.CertFile,
			KeyFile:   cfg.TLS.KeyFile,
			Hosts:     cfg.TLS.Hosts,
			StorePath: cfg.TLS.StorePath,
		}
		c, caCertPEM, persistErr, err := servertls.Load(tlsCfg)
		if err != nil {
			tlsLog.Error(err.Error())
			os.Exit(1)
		}
		cert = c
		// Both listeners read through this, so a renewal reaches the
		// syslog port as well as HTTPS -- see servertls.Reloader.
		certReloader = servertls.NewReloader(tlsCfg, cert)
		go watchForCertificateReload(ctx, certReloader, tlsLog, cfg.TLS.CertFile != "")
		if persistErr != nil {
			tlsLog.Warn(fmt.Sprintf("%v (continuing with an unpersisted certificate -- every restart will generate a fresh, untrusted-again CA)", persistErr))
		}
		if caCertPEM != nil {
			fingerprint := sha256.Sum256(cert.Certificate[0])
			tlsLog.Info(fmt.Sprintf("generated a local CA (leaf fingerprint %x) -- served at /ca.crt for your browser, reverse proxy, or router to trust", fingerprint))
			rootMux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
				// Recorded so the wizard can confirm the router reached
				// mikroview and took the CA -- the first step whose
				// success is otherwise invisible from this side (#320).
				setupStore.NoteCAFetch(srv.ClientIP(r), time.Now())
				w.Header().Set("Content-Type", "application/x-pem-file")
				w.Write(caCertPEM)
			})
		}
	}
	if cfg.TLS.Enabled {
		scheme = "https"
		// MinVersion pinned rather than left to the Go release that
		// built this binary, matching internal/syslog's TLS listener and
		// its reasoning: the implicit server default has shifted across
		// Go versions before, and what this listener will accept should
		// not depend on which toolchain produced it. This is the
		// listener carrying login credentials and session cookies, so if
		// either listener deserves the pin it is this one.
		//
		// Not a live vulnerability: probed on this repo's Go 1.26.5, the
		// unpinned config refused TLS 1.0 and 1.1 identically to the
		// pinned one. Two of #272's phase 2 reviewers raised the
		// asymmetry independently and one tested it rather than assuming
		// -- recorded so the pin reads as consistency, not as a fix for
		// something that was exploitable. See #282, #284.
		httpServer.TLSConfig = &tls.Config{
			// GetCertificate rather than a fixed Certificates list, so
			// SIGHUP swaps what this serves without a restart (#294
			// item 5).
			GetCertificate: certReloader.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		if cfg.Listen.HTTPRedirect != "" {
			redirectLog := logging.New("http-redirect")
			redirectServer := &http.Server{
				Addr: cfg.Listen.HTTPRedirect,
				// The only job here is bouncing a client that guessed
				// plain HTTP over to the real HTTPS listener -- strips
				// any port off the request's Host header and assumes
				// HTTPS is reachable on the browser-default 443 (see
				// Listen.HTTPRedirect's doc comment for when that
				// assumption doesn't hold).
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, httpsRedirectTarget(r, cfg.TLS.Hosts), http.StatusPermanentRedirect)
				}),
				ReadHeaderTimeout: 10 * time.Second,
				ErrorLog:          slog.NewLogLogger(redirectLog.Handler(), slog.LevelWarn),
			}
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				redirectServer.Shutdown(shutdownCtx)
			}()
			go func() {
				redirectLog.Info(fmt.Sprintf("redirecting plain HTTP on %s -> https", cfg.Listen.HTTPRedirect))
				if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					redirectLog.Error(err.Error())
				}
			}()
		}
	} else {
		tlsLog.Warn(fmt.Sprintf("disabled (tls.enabled=false) -- mikroview is serving plain HTTP on %s. Safe ONLY if this listener is unreachable except from your own reverse proxy over an isolated network -- never expose this port to a LAN or the internet in this mode.", cfg.Listen.HTTP))
	}
	if cfg.Listen.SyslogTLS != "" {
		// RouterOS's remote-protocol=tls (issue #188), presenting the
		// same certificate loaded above -- started independently of
		// cfg.TLS.Enabled, see that block's comment for why.
		go func() {
			if err := syslog.ListenTLS(ctx, cfg.Listen.SyslogTLS, certReloader, raw); err != nil && ctx.Err() == nil {
				logging.New("syslog-tls").Error(err.Error())
				os.Exit(1)
			}
		}()
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	syslogSummary := "syslog disabled (listen.syslogTls is empty)"
	if cfg.Listen.SyslogTLS != "" {
		syslogSummary = fmt.Sprintf("syslog tls on %s", cfg.Listen.SyslogTLS)
	}
	logging.New("mikroview").Info(fmt.Sprintf("%s on %s, %s", scheme, cfg.Listen.HTTP, syslogSummary))
	var serveErr error
	if cfg.TLS.Enabled {
		// One published port has to answer both a TLS client and a
		// browser given "host:8080", which tries http:// first -- see
		// internal/tlssniff (#325).
		ln, lnErr := net.Listen("tcp", httpServer.Addr)
		if lnErr != nil {
			logging.New("http").Error(lnErr.Error())
			os.Exit(1)
		}
		sniffLog := logging.New("http")
		serveErr = httpServer.ServeTLS(
			tlssniff.Listener(ln, sniffLog, func(requested string) string {
				return samePortRedirectHost(requested, cfg.TLS.Hosts, ln.Addr().String())
			}),
			"", "")
	} else {
		serveErr = httpServer.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		logging.New("http").Error(serveErr.Error())
		os.Exit(1)
	}

	// httpServer.Shutdown has returned by this point (that's what let
	// ServeTLS/ListenAndServe return above), so ingest and every
	// evaluation goroutine downstream of it have stopped submitting new
	// mutations. Flush every write-behind-backed store's final dirty
	// state now, within a bounded deadline, so a change made right
	// before shutdown is not silently dropped the way an ordinary
	// MinInterval-debounced write could be (issue #400). Best-effort:
	// each store already logs its own save failures, so a Close error
	// here is just the shutdown-budget case, worth one line, not fatal.
	closeStoreOnShutdown(fs, macRegistry, ru, detectorSettings, watchlistStore, engineState, definitions)
}

// closeStoreOnShutdown flushes every write-behind-backed store passed to
// it, in parallel (one slow backend must not delay the others) and
// under one shared deadline -- see persist.WriteBehind.Close. Each
// argument is a *T with a Close(context.Context) error method; a nil
// store (persistence never configured, or the caller has none to offer)
// is a safe no-op via that method's own nil-receiver contract.
func closeStoreOnShutdown(stores ...interface{ Close(context.Context) error }) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log := logging.New("shutdown")
	var wg sync.WaitGroup
	for _, s := range stores {
		wg.Add(1)
		go func(s interface{ Close(context.Context) error }) {
			defer wg.Done()
			if err := s.Close(ctx); err != nil {
				log.Warn(fmt.Sprintf("flushing a store's final state on shutdown: %v", err))
			}
		}(s)
	}
	wg.Wait()
}

// runVersion backs the `-version` mode -- prints the build-time-stamped
// commit SHA (the `version` var above, "dev" for a plain local build)
// and nothing else, so `docker exec <container> mikroview -version`
// output is directly usable in a script without trimming a prefix.
func runVersion() int {
	fmt.Println(version)
	return 0
}

// runHealthcheck backs the `-healthcheck` mode used by the container's
// HEALTHCHECK (see Dockerfile). It hits the app's own /api/healthz over
// loopback and returns a process exit code, rather than opening any
// listeners itself.
// validateConfigExit* are the exit codes -validate-config reports.
// Distinct rather than a plain 0/1 because CI needs to tell "your config
// is imperfect" apart from "I couldn't read your config at all" -- the
// second is a broken pipeline, not a broken config.
const (
	validateConfigExitOK       = 0
	validateConfigExitProblems = 1
	validateConfigExitUnusable = 2
)

// runValidateConfig backs `mikroview -validate-config [-strict]`.
//
// Exits non-zero for warnings as well as fatals, which makes the checker
// deliberately stricter than the server: the server starts on a clamped
// default because losing all monitoring over a bad retention value would
// be worse, but a pipeline has no such excuse and should be told.
// -strict exists for the opposite preference and is currently a no-op
// flag reserved for it; see the issue.
//
// Deliberately offline. Nothing here dials the OIDC issuer, the SMTP
// host, or anything else -- a checker that reaches out to production
// from a build agent is a surprise, and an egress finding in its own
// right. Network checks belong behind their own explicit flag.
func runValidateConfig(args []string) int {
	path := os.Getenv("MIKROVIEW_CONFIG")

	cfg, result, err := config.LoadWithProblems(path, nil)
	if err != nil {
		// Either the file is unreadable/unparseable, or validation found
		// something fatal. Only the exit code distinguishes them, and
		// only when we can tell -- but a fatal gets the same block
		// treatment warnings do below, rather than one dense line with
		// the fix on the end.
		if len(result.Fatal) > 0 {
			fmt.Fprint(os.Stderr, config.Report(result.Fatal))
			fmt.Fprintf(os.Stderr, "\n%d problem(s). mikroview would refuse to start.\n", len(result.Fatal))
			return validateConfigExitProblems
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if strings.Contains(err.Error(), "invalid configuration") {
			return validateConfigExitProblems
		}
		return validateConfigExitUnusable
	}
	_ = args
	_ = cfg

	if !result.HasProblems() {
		if path == "" {
			fmt.Println("No config file set (MIKROVIEW_CONFIG is empty) -- built-in defaults are valid.")
		} else {
			fmt.Printf("%s: no problems found.\n", path)
		}
		return validateConfigExitOK
	}

	for _, p := range result.Warnings {
		fmt.Printf("warning  %s  %s: %s\n", p.Code, p.Key, p.Message)
		if p.Applied != "" {
			fmt.Printf("         using %s instead\n", p.Applied)
		}
		if p.Remediation != "" {
			fmt.Printf("         %s\n", p.Remediation)
		}
	}
	fmt.Printf("\n%d warning(s). mikroview would start, using the values shown above.\n", len(result.Warnings))
	return validateConfigExitProblems
}

// openRecoveryStoreForCLI opens the recovery-key store the gated CLI
// commands verify against.
// openRecoveryStoreForCLI resolves the same backend the server would, so
// the digests follow the accounts into Postgres (issue #172).
//
// The pepper stays a local file in both cases. That is the whole design:
// a database dump yields digests nothing can test, and this host yields a
// pepper with no digests to apply it to.
func openRecoveryStoreForCLI() (*auth.RecoveryStore, func(), error) {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Auth.RecoveryKeysPath == "" && cfg.Postgres.DSNFile == "" {
		return nil, nil, fmt.Errorf("auth.recoveryKeysPath is not configured")
	}

	st, err := openStorage(context.Background(), cfg)
	if err != nil {
		return nil, nil, err
	}
	backend, err := st.backendFor(context.Background(), "recovery_keys", cfg.Auth.RecoveryKeysPath)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	store, err := auth.OpenRecoveryWithBackend(backend, cfg.Auth.RecoveryPepperPath)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return store, st.Close, nil
}

// isContainerMainProcess reports whether this process is the container's
// entrypoint, whose stdout the log driver captures.
//
// PID 1 is the discriminator: `docker run` starts mikroview as PID 1 and
// logs its output, while `docker exec` starts it as an ordinary child and
// does not (measured against a real daemon, not assumed). On a host
// install PID 1 is the system's init, so this is false and printing is
// allowed, which is the behaviour we want there.
//
// It is a heuristic in one direction only: a container that wrapped
// mikroview in an init shim like tini would make this false while stdout
// is still logged. This project's image has no such shim, so the case
// cannot arise for the image we ship.
func isContainerMainProcess(pid int) bool { return pid == 1 }

// refuseIfContainerMainProcess stops a key-printing command before it
// does any work.
//
// Checked up front rather than at the point of printing: -transfer-admin
// and -recover-admin-account both change state before they have keys to
// show, and refusing halfway leaves the operator having done the
// dangerous half of an operation they were told they could not do.
func refuseIfContainerMainProcess(cmd string) error {
	if !isContainerMainProcess(os.Getpid()) {
		return nil
	}
	return containerMainProcessRefusal(cmd)
}

// containerMainProcessRefusal is split out so the wording can be tested
// without forking a PID-1 process. The wording is the load-bearing part:
// an operator told "no" and not told what to run instead reaches for
// whatever works, which is the thing that was just refused.
func containerMainProcessRefusal(cmd string) error {
	return fmt.Errorf("refusing to run %s as the container's main process, because everything it "+
		"prints -- including your recovery keys -- goes into the container log, which is kept on "+
		"disk and is usually shipped off the host. Run it with 'docker compose exec', not "+
		"'docker compose run': exec output is not captured by the log driver", cmd)
}

// printRecoveryKeys shows a freshly generated set, once.
//
// Straight to stdout, with no file in between. Writing them to a file
// first was tried and was worse: the operator has to read the file to
// use it, so the keys end up on a terminal regardless, and the file adds
// an exposure the terminal does not have -- plaintext keys sitting on
// the data volume, included in any backup taken during that window, and
// left behind entirely if the process is killed before it can clean up.
// It moved the problem and charged a disk copy for it.
//
// What actually matters is which stream this is. In a container, stdout
// is the container log; under `docker exec` it is the operator's own
// terminal and nothing else. refuseIfContainerMainProcess enforces that
// distinction before any of this runs.
func printRecoveryKeys(keys []string) {
	fmt.Println()
	fmt.Println("Recovery keys -- shown once, and never again:")
	fmt.Println()
	for _, k := range keys {
		fmt.Printf("    %s\n", k)
	}
	fmt.Println()
	fmt.Println("Store these somewhere safe, such as a password manager. Any one of")
	fmt.Println("them works; using one replaces all three.")
	fmt.Println()
}

// confirmSaved is the acknowledgement gate. Nothing rotates until the
// operator says they have the new keys -- otherwise a successful
// recovery whose output was lost leaves zero valid keys behind.
func confirmSaved() bool {
	fmt.Print("Type 'saved' once you have stored them: ")
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(answer), "saved")
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// runGenerateRecoveryKeys backs `-generate-recovery-keys`.
//
// A one-time backfill for deployments predating recovery keys, not a
// regeneration tool. It refuses once keys exist: otherwise anyone with
// host access mints a fresh set and satisfies the very gate they were
// meant to be stopped by. Rotation happens automatically after each use.
func runGenerateRecoveryKeys(args []string) int {
	logger := logging.New("recovery-keys")
	if err := refuseIfContainerMainProcess("-generate-recovery-keys"); err != nil {
		logger.Error(err.Error())
		return 1
	}
	store, closeStorage, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer closeStorage()

	keys, err := store.Generate()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	printRecoveryKeys(keys)
	if !confirmSaved() {
		logger.Error("not confirmed -- no keys were stored, nothing has changed")
		return 1
	}
	if err := store.Commit(); err != nil {
		logger.Error(fmt.Sprintf("storing recovery keys: %v", err))
		return 1
	}
	logger.Info("recovery keys stored")
	return 0
}

// runTransferAdmin backs `-transfer-admin <username>`.
//
// CLI-only and recovery-key gated on purpose. If an authenticated admin
// could transfer the role from the web UI, anyone reaching that session
// -- a compromised identity-provider account, a stolen cookie -- could
// grant themselves durable ownership and demote the real admin out of
// their own deployment. Requiring host access plus a recovery key means
// an IdP compromise buys the ability to log in, and nothing more.
func runTransferAdmin(args []string) int {
	logger := logging.New("transfer-admin")
	if err := refuseIfContainerMainProcess("-transfer-admin"); err != nil {
		logger.Error(err.Error())
		return 1
	}

	// The username is optional. Someone locked out of the UI often can't
	// look up the exact spelling of a colleague's account, and guessing
	// at 3am is a bad place to be -- so with no argument this offers a
	// list to pick from. Supplying one keeps the flow scriptable.
	var target string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
	}

	recovery, closeStorage, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer closeStorage()
	store, closeAuth, err := openAuthStoreForCLI("-transfer-admin")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer closeAuth()

	current := store.Admin()
	if current == nil {
		logger.Error("this deployment has no admin account -- nothing to transfer")
		return 1
	}
	// The key is asked for BEFORE any account is named or listed, so
	// nothing about who holds an account is disclosed to someone without
	// one. That reordering is only affordable because Redeem below
	// prepares a rotation without persisting it -- see its call.
	//
	// This comment described the intent and the code did the opposite:
	// the current admin's username was printed here, above this line,
	// before the key was ever asked for. So anyone able to run the
	// binary learned who the admin is by starting the command and
	// pressing Ctrl-C. Flagged as an Uncertain lead on #267 and passed
	// to the security track as out of that audit's scope, where it was
	// not picked up -- it fell between the two.
	key, err := readRecoveryKey()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	// Redeem verifies the key and prepares a replacement set, but does
	// not persist the rotation -- that waits for Commit at the end. So
	// abandoning this command after seeing the list below costs nothing:
	// the existing keys stay valid, and no key has been spent.
	fresh, err := recovery.Redeem(key)
	if err != nil {
		// Deliberately one message for a wrong key and a corrupt store:
		// the distinction is only useful to someone probing.
		logger.Error(err.Error())
		return 1
	}

	// Now that the key has been proven, naming the account is fine --
	// and still useful, since the operator is about to choose what to
	// transfer it to.
	fmt.Printf("Admin is currently %q.\n", current.Username)

	next, code := resolveTransferTarget(store, current, target)
	if next == nil {
		// Nothing was written, and no key was consumed -- Commit never
		// ran. Said out loud so an operator who backed out isn't left
		// wondering whether they just burned a key.
		fmt.Println("\nNothing was changed, and your recovery keys are unchanged.")
		return code
	}
	target = next.Username

	// Stated before the transfer, not after: an SSO-only admin cannot be
	// recovered by -recover-admin-account, by design, so the operator
	// needs this while they can still back out.
	if !next.LocalPassword() {
		fmt.Println()
		fmt.Printf("WARNING: %q signs in through your identity provider and has no local\n", next.Username)
		fmt.Println("password. After this transfer, admin recovery goes through your identity")
		fmt.Println("provider -- mikroview will not be able to recover that account itself.")
		fmt.Println()
		if !confirmYes(fmt.Sprintf("Transfer admin to %q anyway?", next.Username)) {
			fmt.Println("Nothing was changed, and your recovery keys are unchanged.")
			return 1
		}
	}

	from, to, err := store.TransferAdmin(target, time.Now())
	if err != nil {
		logger.Error(fmt.Sprintf("transfer failed, no keys were consumed: %v", err))
		return 1
	}

	printRecoveryKeys(fresh)
	if !confirmSaved() {
		// The transfer stands; only the rotation is abandoned, leaving
		// the previous keys valid. Better than rotating into a set the
		// operator never captured.
		logger.Warn(fmt.Sprintf("admin transferred from %s to %s, but the new keys were not confirmed -- "+
			"your previous recovery keys remain valid", logging.Printable(from.Username), logging.Printable(to.Username)))
		return 1
	}
	if err := recovery.Commit(); err != nil {
		logger.Warn(fmt.Sprintf("admin transferred from %s to %s, but storing the new keys failed: %v -- "+
			"your previous recovery keys remain valid", logging.Printable(from.Username), logging.Printable(to.Username), err))
		return 1
	}
	logger.Info(fmt.Sprintf("admin transferred from %s to %s", logging.Printable(from.Username), logging.Printable(to.Username)))
	return 0
}

func runHealthcheck() int {
	logger := logging.New("healthcheck")
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		logger.Error(fmt.Sprintf("loading config: %v", err))
		return 1
	}
	addr := cfg.Listen.HTTP
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	scheme := "http"
	client := http.Client{Timeout: 3 * time.Second}
	if cfg.TLS.Enabled {
		scheme = "https"
		// Checking itself, from inside the same container -- there's no
		// trust boundary being crossed by skipping verification of its
		// own (possibly self-signed) certificate here.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := client.Get(scheme + "://" + addr + "/api/healthz")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Error(fmt.Sprintf("unexpected status %d", resp.StatusCode))
		return 1
	}
	return 0
}

// openAuthStoreForCLI is shared by the account-management CLI commands.
// They need a real, persisted auth.Store -- an ephemeral one would let
// -recover-admin-account report success over a password that vanishes on
// the next restart.
//
// It resolves the same backend the server would, so these work against
// Postgres as well as the JSON files. They used to refuse outright on a
// Postgres deployment, which left it with no way to transfer or recover
// admin at all: those two operations cannot go through the web UI (a
// compromised session must not be able to grant itself admin) and cannot
// go through an identity provider (an IdP account is a login, not an
// authorisation to escalate). CLI is the only route, so it has to work
// in every deployment shape.
func openAuthStoreForCLI(cmd string) (*auth.Store, func(), error) {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Auth.StorePath == "" && cfg.Postgres.DSNFile == "" {
		return nil, nil, fmt.Errorf("auth.storePath is not configured -- %s has nothing persisted to work with", cmd)
	}

	st, err := openStorage(context.Background(), cfg)
	if err != nil {
		return nil, nil, err
	}
	backend, err := st.backendFor(context.Background(), "auth", cfg.Auth.StorePath)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	store, err := auth.OpenWithBackend(backend)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return store, st.Close, nil
}

// mustOpenStore is main()'s boot-sequence policy for every persisted
// store's OpenWithBackend/OpenSettingsStoreWithBackend call (issue
// #378): err is nil for both "persistence not configured" and "no
// document written yet" (a real fresh install), and non-nil exactly
// when a document exists but could not be loaded or parsed -- see
// internal/persist.Open, which every one of those constructors now
// funnels through.
//
// That second case is refused outright rather than logged and
// continued. The store returned on that path used to keep its live
// backend attached (see #378): the process ran with near-empty
// in-memory state and a warning claiming "in-memory-only," and the next
// persist call silently overwrote the operator's actual on-disk
// document with that near-empty state. Every OpenWithBackend now
// returns (nil, err) instead, so there is no half-built store left to
// fall through with -- refusing to start is the only option left, and
// the error's own message (built by persist.StartupError) already names
// the store, its location, the cause, and the remedy: restore from a
// backup, or deliberately move the document aside to start fresh.
// log is the exact logger the call site already built for this store,
// so the message keeps that store's own component tag.
func mustOpenStore(log *slog.Logger, err error) {
	if err == nil {
		return
	}
	log.Error(err.Error())
	os.Exit(1)
}

// runRecoverAdminAccount backs `-recover-admin-account` -- the way back
// in when the admin cannot sign in.
//
// Two deliberate narrowings from the `-recover-admin-account` it replaces.
//
// It recovers the admin account only. The old command could rewrite any
// account's password from host access alone, which made every user
// account a route to a working login; other accounts are the admin's to
// manage from the UI, where the action is authenticated and logged.
//
// It requires a recovery key. Host access alone is no longer sufficient,
// so a lower-privileged local account or a container exec that can run
// the binary but cannot read the key file gets nowhere -- and neither
// does someone holding a stolen backup, since the pepper is not in it.
//
// The new password is prompted for twice, without echo, rather than
// taken as an argument or env var, so it never reaches shell history,
// process args, or `docker inspect`. SetPassword records
// PasswordChangedAt, which invalidates every session issued before the
// recovery -- a stolen cookie must not survive it.
func runRecoverAdminAccount(args []string) int {
	logger := logging.New("recover-admin-account")
	if err := refuseIfContainerMainProcess("-recover-admin-account"); err != nil {
		logger.Error(err.Error())
		return 1
	}

	recovery, closeStorage, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer closeStorage()
	store, closeAuth, err := openAuthStoreForCLI("-recover-admin-account")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer closeAuth()

	admin := store.Admin()
	if admin == nil {
		logger.Error("this deployment has no admin account -- nothing to recover")
		return 1
	}
	// Checked before anything is prompted for. mikroview does not hold
	// the credential for an SSO-only admin, so there is nothing here to
	// reset; sending the operator to their identity provider immediately
	// beats letting them type a recovery key and a new password first
	// and only then learning it was never going to work.
	if !admin.LocalPassword() {
		logger.Error(fmt.Sprintf("%q signs in through your identity provider and has no local password "+
			"-- mikroview cannot recover it. Reset it at your identity provider, or use "+
			"-transfer-admin to move admin to an account that does have one", logging.Printable(admin.Username)))
		return 1
	}

	// Named before the key is asked for, unlike -transfer-admin, and
	// deliberately so rather than by oversight: the SSO-only check just
	// above already has to name the account to explain why this command
	// cannot help, so withholding it here would buy nothing. The
	// operator also needs to know which account they are about to reset
	// before typing a key and a new password for it.
	fmt.Printf("Recover the admin account %q.\n", admin.Username)
	key, err := readRecoveryKey()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	// Redeem verifies the key and prepares a replacement set without
	// persisting the rotation -- Commit below does that, once the
	// operator confirms they captured the new keys.
	fresh, err := recovery.Redeem(key)
	if err != nil {
		// One message for a wrong key and for a corrupt store: the
		// difference is only useful to someone probing.
		logger.Error(err.Error())
		return 1
	}

	password, err := readPasswordTwice()
	if err != nil {
		logger.Error(fmt.Sprintf("%v -- no key was consumed, nothing has changed", err))
		return 1
	}
	if err := store.SetPassword(admin.Username, password, time.Now()); err != nil {
		logger.Error(fmt.Sprintf("%v -- no key was consumed, nothing has changed", err))
		return 1
	}
	fmt.Printf("Password for %q updated. Existing sessions for that account are now invalid.\n", admin.Username)

	// Past this point the recovery itself has succeeded. Every remaining
	// failure leaves the previous keys valid and says so -- rotating
	// into a set the operator never captured is the one outcome worse
	// than not rotating at all.
	printRecoveryKeys(fresh)
	if !confirmSaved() {
		logger.Warn("admin account recovered, but the new keys were not confirmed -- " +
			"your previous recovery keys remain valid")
		return 1
	}
	if err := recovery.Commit(); err != nil {
		logger.Warn(fmt.Sprintf("admin account recovered, but storing the new keys failed: %v -- "+
			"your previous recovery keys remain valid", err))
		return 1
	}
	logger.Info("admin account recovered, recovery keys rotated")
	return 0
}

// readPasswordTwice prompts for a password without echoing it to the
// terminal, and again to confirm -- a mistyped new password on a
// recovery tool is nearly as bad as staying locked out.
func readPasswordTwice() (string, error) {
	fmt.Print("New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	fmt.Print("Confirm new password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}

	if !bytes.Equal(first, second) {
		return "", fmt.Errorf("passwords did not match")
	}
	if len(first) == 0 {
		return "", fmt.Errorf("password cannot be empty")
	}
	return string(first), nil
}

// ingest is the single store-writer goroutine: it parses each raw syslog
// message, resolves device identity, inserts the resulting Event into the
// store, and hands the stored (ID-assigned) event to the hub for
// broadcast. Keeping this on one goroutine means Store and the device
// Registry never need to arbitrate concurrent writers. Detection is
// handed off via detector.Enqueue rather than run inline here -- a slow
// or backed-up detection pass must never delay store insertion or
// WebSocket broadcast (see detect.Detector.Enqueue/Run, and the
// dedicated detection-worker goroutine main() starts alongside this
// one).
func ingest(ctx context.Context, raw <-chan syslog.RawMessage, st *store.Store, devices *device.Registry, macRegistry *device.MACRegistry, fs *flags.Store, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector, ru *rules.Store, names naming.Resolver, watchlistEval *watchlist.Evaluator, eng *engine.Engine, setupStore *setup.Store) {
	ingestLog := logging.New("ingest")
	for {
		select {
		case <-ctx.Done():
			return
		case rm := <-raw:
			ingestOneRecovered(ingestLog, rm, st, devices, macRegistry, fs, h, geo, detector, ru, names, watchlistEval, eng, setupStore)
		}
	}
}

// ingestOneRecovered isolates panic recovery to a single message
// rather than ingest's whole lifetime -- recover only unwinds as far as
// the nearest deferring function, so a defer in ingest itself would
// still end the entire ingest goroutine for good on the first bad
// message (silently stopping all future event processing) rather than
// just dropping that one message. See logging.Recover's doc comment.
func ingestOneRecovered(logger *slog.Logger, rm syslog.RawMessage, st *store.Store, devices *device.Registry, macRegistry *device.MACRegistry, fs *flags.Store, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector, ru *rules.Store, names naming.Resolver, watchlistEval *watchlist.Evaluator, eng *engine.Engine, setupStore *setup.Store) {
	defer logging.Recover(logger)

	env := syslog.ParseEnvelope(rm.Data, rm.RecvTime)
	parsed := routeros.Parse(env.Message)
	deviceID := devices.Resolve(rm.SourceIP, rm.RecvTime)
	// Whether the rule's log-prefix decoded (#320 step 3): a router
	// logging without the <A|D|R|L|M|N>|slug| convention sends events
	// that look healthy on every other measure and carry no action at
	// all.
	//
	// ActionFromPrefix rather than "action != unknown": since #437 the
	// parser classifies some untagged lines on its own (a NAT chain
	// carrying a translation), and counting those would tell an operator
	// their prefixes were working when none are configured.
	if setupStore != nil {
		setupStore.NoteEvent(deviceID, parsed.ActionFromPrefix, rm.RecvTime)
	}
	srcCountry, _ := geo.Country(parsed.SrcIP)
	dstCountry, _ := geo.Country(parsed.DstIP)

	// New-device/new-MAC detection (issue #103 phase 1): a deterministic,
	// once-per-MAC check, so it's raised directly here rather than
	// through detect.Detector's async worker like every other flag type
	// -- there's no rolling-window state to maintain, just a "seen
	// before" lookup against macRegistry's persisted history. Skips
	// events with no SrcMAC (MACRegistry.Seen already no-ops for those,
	// but checking here first avoids taking its lock at all on the
	// common WAN-side-rule case where SrcMAC is never present). No
	// confidence score attached (plain Add, not AddWithDetail/
	// AddWithConfidence): "is this MAC new" is a deterministic yes/no,
	// but that's a different question from "is this a threat" -- a truly
	// new device is very often entirely benign (a phone joining the
	// Wi-Fi), so fabricating a numeric confidence here would misleadingly
	// imply a threat judgment this detector doesn't make. Same "not
	// scored" contract as Flag.Confidence's nil case. The target is a
	// MAC, not an IP, so -- same as TypeRuleSpike's rule-label target --
	// there's no meaningful Country to attach either.
	if parsed.SrcMAC != "" && macRegistry.Seen(parsed.SrcMAC, rm.RecvTime) {
		detail := fmt.Sprintf("first traffic seen from MAC %s", parsed.SrcMAC)
		if parsed.SrcIP != "" {
			detail = fmt.Sprintf("first traffic seen from MAC %s (source IP %s)", parsed.SrcMAC, parsed.SrcIP)
		}
		fs.Add(flags.TypeNewDevice, parsed.SrcMAC, detail, rm.RecvTime)
	}

	e := store.Event{
		Time:         env.Timestamp,
		DeviceID:     deviceID,
		SourceIP:     rm.SourceIP,
		Action:       parsed.Action,
		RuleLabel:    parsed.RuleLabel,
		RuleName:     names.Rule(parsed.RuleLabel),
		Chain:        parsed.Chain,
		InInterface:  parsed.InInterface,
		OutInterface: parsed.OutInterface,
		ConnState:    parsed.ConnState,
		Protocol:     parsed.Protocol,
		SrcMAC:       parsed.SrcMAC,
		SrcIP:        parsed.SrcIP,
		SrcPort:      parsed.SrcPort,
		DstIP:        parsed.DstIP,
		DstPort:      parsed.DstPort,
		SrcHostName:  names.Host(deviceID, parsed.SrcIP),
		DstHostName:  names.Host(deviceID, parsed.DstIP),
		SrcPortName:  names.Port(parsed.SrcPort),
		DstPortName:  names.Port(parsed.DstPort),
		SrcCountry:   srcCountry,
		DstCountry:   dstCountry,
		NatIP:        parsed.NatIP,
		NatPort:      parsed.NatPort,
		NatRaw:       parsed.NatRaw,
		Length:       parsed.Length,
		Flags:        parsed.Flags,
	}
	// Applied here rather than in the parser: internal/routeros deals in
	// one line at a time and has no view of the ring's memory budget,
	// which is what this bounds. See store.MaxRawBytes.
	e.Raw, e.RawTruncated = store.ClampRaw(parsed.Raw)

	stored := st.Insert(e)
	h.Broadcast(stored)
	detector.Enqueue(stored)
	watchlistEval.Enqueue(stored)
	// eng holds no definitions yet (#398 -- see New's call site above),
	// so this evaluates nothing today; fanning every stored event to it
	// now is what makes the later collapse of detector/watchlistEval
	// onto it (#399 onward) a swap rather than a rewire.
	eng.Enqueue(stored)
	// Keeps internal/rules' long-lived per-rule usage record in sync with
	// internal/store/ring.go's own totalByRule bump inside Insert above --
	// same per-event trigger, so RuleUsage never drifts out of step with
	// what the store itself just counted (see internal/rules.Store.Touch's
	// doc comment for why this lives here rather than as a separate pass).
	ru.Touch(stored.RuleLabel, stored.ReceivedAt)
}

// resolveTransferTarget works out which account admin is moving to,
// either from the username supplied on the command line or by offering
// a numbered list.
//
// The list is what replaces the old `-list-users` command. That command
// existed almost entirely to answer "what is my colleague's username
// again?" while locked out, and answering it here means the disclosure
// sits behind the recovery key that transfer already requires, instead
// of behind nothing.
//
// Gating a standalone list on a key was considered and rejected: it
// would teach the operator to type a recovery key for a routine read.
// The key is echo-suppressed (see readRecoveryKey), so this is not
// about shell history -- it is that every use is a chance for the key
// to reach a screen share, a support call, or a recorded session, and
// a key used routinely stops being treated as precious.
//
// Returns (nil, exitCode) when there is nothing to transfer to or the
// operator backs out. Callers must treat that as "change nothing".
func resolveTransferTarget(store *auth.Store, current *auth.User, target string) (*auth.User, int) {
	logger := logging.New("transfer-admin")

	if target != "" {
		next, ok := store.ByUsername(target)
		if !ok {
			logger.Error(fmt.Sprintf("no such account: %s", logging.Printable(target)))
			return nil, 1
		}
		if next.ID == current.ID {
			logger.Error("that account is already the admin")
			return nil, 1
		}
		return next, 0
	}

	candidates := make([]auth.User, 0)
	for _, u := range store.List() {
		if u.ID != current.ID {
			candidates = append(candidates, u)
		}
	}
	if len(candidates) == 0 {
		logger.Error("there is no other account to transfer admin to -- create one from the web UI first")
		return nil, 1
	}

	fmt.Println("\nTransfer admin to:")
	fmt.Println()
	for i, u := range candidates {
		note := ""
		if !u.LocalPassword() {
			// Flagged in the list, not only after choosing, so the
			// consequence is visible while choosing rather than as a
			// surprise afterwards.
			note = "   (signs in via SSO -- mikroview cannot recover this account)"
		}
		fmt.Printf("  %2d) %s%s\n", i+1, logging.Printable(u.Username), note)
	}
	fmt.Println()
	fmt.Printf("Choose 1-%d, or anything else to cancel: ", len(candidates))

	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return nil, 1
	}
	choice, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil || choice < 1 || choice > len(candidates) {
		return nil, 1
	}
	picked := candidates[choice-1]
	return &picked, 0
}

// confirmYes asks a yes/no question, defaulting to no on anything that
// isn't an explicit yes -- including a read error, so a closed stdin
// can never be mistaken for consent.
func confirmYes(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}

// readRecoveryKey prompts for a recovery key without echoing it.
//
// Passwords have always been read this way; recovery keys were not, and
// that was an inconsistency rather than a decision. A recovery key is at
// least as sensitive as the password -- it is the second factor on admin
// transfer and account recovery -- and echoing it put it in terminal
// scrollback, and in anything capturing the session (tmux logging,
// `script`, screen sharing, a support call). Measured before the fix:
// the key appeared verbatim in a captured session.
//
// Note this was never a *shell* history exposure for interactive use --
// the shell never sees it, because the prompt belongs to this process.
// It reaches shell history only in the piped form
// (`echo KEY | mikroview -transfer-admin`), which is the caller's
// choice and their exposure to manage.
//
// A non-terminal stdin falls back to a plain read rather than refusing:
// automation that pipes a key has already exposed it by whatever put it
// in the pipe, so refusing here protects nothing and breaks a
// legitimate workflow.
func readRecoveryKey() (string, error) {
	fmt.Print("Recovery key: ")

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		var key string
		if _, err := fmt.Scanln(&key); err != nil {
			return "", fmt.Errorf("no recovery key supplied")
		}
		return key, nil
	}

	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("no recovery key supplied")
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("no recovery key supplied")
	}
	return string(raw), nil
}

// watchForCertificateReload swaps in a renewed certificate on SIGHUP,
// for both the HTTPS and syslog listeners at once (#294 item 5).
//
// SIGHUP rather than watching the files: mikroview cannot tell a
// finished renewal from a half-written one, and serving half a
// certificate is worse than serving an old one. The signal is the
// operator (or certbot's --deploy-hook, or a cert-manager reloader)
// saying the new files are complete. That is also the convention every
// other long-running server uses for this, so it needs no explaining.
//
// operatorSupplied only affects the log line. The reload works either
// way, but for mikroview's own generated certificate there is normally
// nothing new on disk to pick up, so saying so avoids an operator
// concluding the signal did nothing when it did exactly what it should.
func watchForCertificateReload(ctx context.Context, reloader *servertls.Reloader, log *slog.Logger, operatorSupplied bool) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			cert, err := reloader.Reload()
			if err != nil {
				// Deliberately not fatal, and the previous certificate
				// stays in service: an operator who sent this expecting
				// an improvement must not get an outage out of it, and
				// would have little reason to connect the two.
				log.Error(fmt.Sprintf("reloading the certificate failed, continuing with the one already loaded: %v", err))
				continue
			}
			fingerprint := sha256.Sum256(cert.Certificate[0])
			if operatorSupplied {
				log.Info(fmt.Sprintf("certificate reloaded (leaf fingerprint %x) -- new connections to both the https and syslog listeners use it from now on", fingerprint))
			} else {
				log.Info(fmt.Sprintf("certificate reloaded (leaf fingerprint %x) -- this deployment uses mikroview's own generated certificate, so this only re-reads what is already on disk", fingerprint))
			}
		}
	}
}

// --- ShippedDeps adapters (issue #405) -------------------------------
//
// internal/engine deliberately depends on narrow interfaces rather than
// on internal/blocklist, internal/netclass, internal/entities,
// internal/device or internal/rules -- so a definition's test can supply
// a fake without standing up a real blocklist download or device
// registry, and so the chassis does not accumulate a dependency on every
// package a detector happens to consult. These adapters are where the
// concrete stores meet those interfaces, and process wiring is exactly
// what this file is for.

type entityTagLookup struct{ es *entities.Store }

func (a entityTagLookup) HasTag(entityType, id, tag string) bool {
	if a.es == nil {
		return false
	}
	return a.es.HasTag(entityType, id, tag)
}

type blocklistLookup struct{ bl *blocklist.Blocklist }

func (a blocklistLookup) MatchIP(ip string) (label, cidr string, ok bool) {
	if a.bl == nil {
		return "", "", false
	}
	m, matched := a.bl.Match(ip)
	if !matched {
		return "", "", false
	}
	return m.Label, m.Range, true
}

type netClassLookup struct{ nc *netclass.Classifier }

func (a netClassLookup) LookupClass(ip string) (matched bool, category, label string) {
	if a.nc == nil {
		return false, "", ""
	}
	c := a.nc.Lookup(ip)
	if !c.Matched {
		return false, "", ""
	}
	return true, string(c.Category), c.Label
}

type deviceLister struct{ reg *device.Registry }

func (a deviceLister) ListDevices() []engine.DeviceInfo {
	if a.reg == nil {
		return nil
	}
	list := a.reg.List()
	out := make([]engine.DeviceInfo, 0, len(list))
	for _, d := range list {
		out = append(out, engine.DeviceInfo{ID: d.ID, Name: d.Name, LastSeen: d.LastSeen, Configured: d.Configured})
	}
	return out
}

// staleRuleLister adapts a *rules.Store. It honours the maxAge the
// definition passes rather than carrying its own copy: the staleness
// threshold is stale_rule's own maxAge param now (issue #405), seeded
// from cfg.Flags.StaleRuleDays, so this adapter's job is purely the
// type conversion internal/rules and internal/engine need between them.
type staleRuleLister struct {
	ru *rules.Store
}

func (a staleRuleLister) StaleRules(maxAge time.Duration, now time.Time) []engine.RuleUsage {
	if a.ru == nil {
		return nil
	}
	stale := a.ru.Stale(maxAge, now)
	out := make([]engine.RuleUsage, 0, len(stale))
	for _, u := range stale {
		out = append(out, engine.RuleUsage{Rule: u.Rule, FirstSeen: u.FirstSeen, LastSeen: u.LastSeen, Count: int(u.Count)})
	}
	return out
}

type eventRateSource struct{ st *store.Store }

func (a eventRateSource) EventsPerSecond() float64 {
	if a.st == nil {
		return 0
	}
	return a.st.EventsPerSecond()
}
