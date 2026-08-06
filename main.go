package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/geoip"
	"github.com/tomlawesome/mikroview/internal/hub"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/notify"
	"github.com/tomlawesome/mikroview/internal/oidc"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/rules"
	"github.com/tomlawesome/mikroview/internal/servertls"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/syslog"
	"github.com/tomlawesome/mikroview/web"
	"golang.org/x/term"
)

// globalSpikeCheckInterval is how often the global volume-spike detector
// re-samples the store's current events-per-second figure. Independent
// of STATS_REFRESH_MS on the frontend -- this only needs to be frequent
// enough for the detector's own EMA baseline to track real trends, not
// to feel "live" to a person.
const globalSpikeCheckInterval = 10 * time.Second

// deviceSilenceCheckInterval is how often DeviceSilenceDetector re-checks
// every configured device's LastSeen against Config.DeviceStaleAfter.
// Coarser than globalSpikeCheckInterval on purpose: DeviceStaleAfter's
// own default (15m) means a device going quiet is detected within this
// interval of crossing the threshold, which doesn't need EMA-baseline-
// tracking-grade freshness to be useful to an operator.
const deviceSilenceCheckInterval = 1 * time.Minute

// loginLimiter{Threshold,Window}: brute-force protection on
// POST /api/auth/login (see internal/auth.LoginLimiter) -- an internal
// hardening constant, not exposed via config, same tier as ws.go's
// wsPongTimeout/wsPingInterval.
const (
	loginLimiterThreshold = 5
	loginLimiterWindow    = 5 * time.Minute
)

// version is stamped at build time via -ldflags "-X main.version=<git-
// short-sha>" (see Dockerfile and .github/workflows/docker.yml) --
// "dev" is the fallback for a plain `go build .`/`go run .` with no
// ldflags, so local development never shows a blank or misleading
// value. A registry digest isn't something the binary can know about
// itself (it's computed from the pushed image after build), so the
// commit it was built from is the practical, achievable stand-in for
// "which build is this."
var version = "dev"

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
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if len(allowedHosts) > 0 && !slices.Contains(allowedHosts, host) {
		host = allowedHosts[0]
	}
	return "https://" + host + r.URL.RequestURI()
}

func main() {
	// The runtime image is distroless (no shell, no curl/wget), so Docker's
	// HEALTHCHECK -- and any orchestrator's readiness probe -- can't shell
	// out to check the app; the binary has to check itself instead. Config
	// is loaded from file/env only here (not os.Args) since this runs as a
	// standalone HEALTHCHECK CMD with no other flags to parse.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(runHealthcheck())
	}
	// -list-users/-reset-password: the account-recovery path (see
	// docs/configuration.md's "Authentication" section) -- container/
	// host access is the trust anchor for these, deliberately outside
	// the web UI/API entirely, so a locked-out admin isn't dependent on
	// the very system they're locked out of.
	if len(os.Args) > 1 && os.Args[1] == "-list-users" {
		os.Exit(runListUsers())
	}
	if len(os.Args) > 1 && os.Args[1] == "-reset-password" {
		os.Exit(runResetPassword(os.Args[2:]))
	}
	// -enable-auth-setup: the only way to re-arm the web setup form after
	// a deployment has explicitly skipped auth (see auth.Store.Disable) --
	// deliberately CLI-only, not exposed via any API endpoint, so a UI
	// visitor can never unilaterally re-impose auth for everyone else.
	if len(os.Args) > 1 && os.Args[1] == "-enable-auth-setup" {
		os.Exit(runEnableAuthSetup())
	}

	configLog := logging.New("config")
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), os.Args[1:])
	if err != nil {
		configLog.Error(err.Error())
		os.Exit(1)
	}
	// Every component logger created before this point (configLog above)
	// still picks up the level -- SetLevel adjusts the shared threshold
	// in place, not a per-logger setting fixed at New() time.
	logging.SetLevel(cfg.Log.Level)

	logging.PrintBanner()
	logVersionAndMigration(logging.New("mikroview"))

	st := store.New(cfg.Store.MaxEvents, cfg.Store.Retention)
	devices := device.NewRegistry(cfg.Devices)
	h := hub.New()
	geoLog := logging.New("geoip")
	geo, err := geoip.Open(cfg.GeoIP.DBPath)
	if err != nil {
		geoLog.Warn(fmt.Sprintf("%v (country flags disabled)", err))
	}
	defer geo.Close()
	rep := reputation.New(cfg.Reputation.AbuseIPDBKey)

	flagsLog := logging.New("flags")
	fs, err := flags.Open(cfg.Flags.StorePath)
	if err != nil {
		flagsLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only flag state)", err))
	}

	// macRegistry backs the new-device/new-MAC detector (issue #103
	// phase 1) -- see internal/device.MACRegistry's doc comment for why
	// this needs its own persisted store distinct from devices above
	// (that one tracks router source IPs, not LAN client MACs).
	macLog := logging.New("device-mac")
	macRegistry, err := device.OpenMACRegistry(cfg.DeviceMAC.StorePath)
	if err != nil {
		macLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only MAC registry)", err))
	}

	// RuleUsage (issue #102): a long-lived, persisted per-rule
	// FirstSeen/LastSeen/Count record backing the stale-rule detector --
	// see internal/rules' doc comment for why this can't just reuse
	// internal/store's totalByRule (in-memory, windowed to the store's
	// short retention period).
	rulesLog := logging.New("rules")
	ru, err := rules.Open(cfg.Flags.RuleUsageStorePath)
	if err != nil {
		rulesLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only rule-usage state)", err))
	}

	authLog := logging.New("auth")
	authStore, err := auth.Open(cfg.Auth.StorePath)
	if err != nil {
		authLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only, unpersisted account state)", err))
	}
	switch {
	case authStore.Count() > 0:
		authLog.Info(fmt.Sprintf("%d account(s) registered -- authentication is active", authStore.Count()))
	case authStore.Disabled():
		authLog.Warn("explicitly disabled for this deployment -- mikroview is fully open (run -enable-auth-setup to reverse this)")
	default:
		authLog.Info("no decision made yet -- mikroview is showing the first-run choice screen (see docs/configuration.md)")
	}

	// Tokens (issue #101): read-only API bearer tokens for service-to-
	// service access -- optional persistence, same degrade-not-crash
	// contract as Flags/DetectorSettings above (a missing/unwritable path
	// just means token creation refuses with ErrTokenNotPersisted, not
	// that mikroview fails to start).
	tokensLog := logging.New("tokens")
	tokenStore, err := auth.OpenTokenStore(cfg.Auth.TokensStorePath)
	if err != nil {
		tokensLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only, unpersisted token state)", err))
	}
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

		DeviceStaleAfter: cfg.Flags.DeviceStaleAfter,
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
	detectorSettings, err := detect.OpenSettingsStore(cfg.Flags.DetectorSettingsStorePath, seed)
	if err != nil {
		detectorsLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only detector toggle state)", err))
	}
	detector := detect.NewWithSettings(detectCfg, fs, detectorSettings).WithReputation(rep)
	globalSpike := detect.NewGlobalSpikeDetectorWithSettings(detectCfg, fs, detectorSettings)
	deviceSilence := detect.NewDeviceSilenceDetectorWithSettings(detectCfg, fs, detectorSettings, devices)
	staleRule := detect.NewStaleRuleDetector(ru, fs, time.Duration(cfg.Flags.StaleRuleDays)*24*time.Hour)

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

	go func() {
		if err := syslog.ListenUDP(ctx, cfg.Listen.SyslogUDP, raw); err != nil && ctx.Err() == nil {
			logging.New("syslog-udp").Error(err.Error())
			os.Exit(1)
		}
	}()
	go func() {
		if err := syslog.ListenTCP(ctx, cfg.Listen.SyslogTCP, raw); err != nil && ctx.Err() == nil {
			logging.New("syslog-tcp").Error(err.Error())
			os.Exit(1)
		}
	}()

	names := naming.Resolver{Rules: cfg.RuleNames, Hosts: cfg.HostNames}

	go ingest(ctx, raw, st, devices, macRegistry, fs, h, geo, detector, ru, names)
	go detector.Run(ctx)

	go func() {
		spikeLog := logging.New("global-spike")
		ticker := time.NewTicker(globalSpikeCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer logging.Recover(spikeLog)
					globalSpike.Check(st.Stats().EventsPerSecond, time.Now())
				}()
			}
		}
	}()

	go func() {
		silenceLog := logging.New("device-silence")
		ticker := time.NewTicker(deviceSilenceCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer logging.Recover(silenceLog)
					deviceSilence.Check(time.Now())
				}()
			}
		}
	}()

	// Stale-rule sweep (issue #102): coarse by design (see
	// StaleRuleCheckInterval's doc comment) -- staleness is judged in
	// days, so there's no benefit to checking anywhere near as often as
	// the global-spike ticker above.
	go func() {
		staleRuleLog := logging.New("stale-rule")
		ticker := time.NewTicker(cfg.Flags.StaleRuleCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer logging.Recover(staleRuleLog)
					staleRule.Check(time.Now())
				}()
			}
		}
	}()

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
	switch {
	case cfg.OIDC.IssuerURL == "":
		// Not configured -- no log line, same as every other disabled-
		// by-default optional integration (GeoIP, Reputation, Notify).
	case cfg.OIDC.PublicBaseURL == "":
		oidcLog.Error("oidc.issuerUrl is set but oidc.publicBaseUrl is not -- SSO login is unavailable until it's configured (see docs/configuration.md)")
	case cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "":
		oidcLog.Error("oidc.issuerUrl is set but oidc.clientId/oidc.clientSecret are not -- SSO login is unavailable until both are configured")
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
			oidcLog.Info(fmt.Sprintf("SSO login active against %s", cfg.OIDC.IssuerURL))
		}
	}

	srv := &api.Server{
		Store:            st,
		Devices:          devices,
		Hub:              h,
		Reputation:       rep,
		Flags:            fs,
		DetectorSettings: detectorSettings,
		CriticalPorts:    cfg.Flags.CriticalPorts,
		DeviceStaleAfter: cfg.Flags.DeviceStaleAfter,
		Auth:             authStore,
		Sessions:         auth.NewSessionStore(cfg.Auth.SessionTTL),
		LoginLimiter:     auth.NewLoginLimiter(loginLimiterThreshold, loginLimiterWindow),
		SecureCookie:     cfg.Auth.SecureCookie,
		Tokens:           tokenStore,
		OIDC:             oidcClient,
		OIDCState:        oidcState,
		StartTime:        time.Now(),
	}

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", srv.Routes())
	if frontend, err := web.DistFS(); err != nil {
		logging.New("frontend").Warn(fmt.Sprintf("%v (serving API only)", err))
	} else {
		rootMux.Handle("/", http.FileServer(http.FS(frontend)))
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
		ErrorLog: slog.NewLogLogger(logging.New("http").Handler(), slog.LevelWarn),
	}

	// TLS (on by default -- see internal/config.TLS's doc comment for
	// why, and the one documented reason to disable it). /ca.crt is
	// registered directly on rootMux, not routed through api.Server,
	// since it's not an API concern -- and only when mikroview generated
	// its own CA, never for an operator-supplied cert.
	tlsLog := logging.New("tls")
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
		cert, caCertPEM, persistErr, err := servertls.Load(servertls.Config{
			CertFile:  cfg.TLS.CertFile,
			KeyFile:   cfg.TLS.KeyFile,
			Hosts:     cfg.TLS.Hosts,
			StorePath: cfg.TLS.StorePath,
		})
		if err != nil {
			tlsLog.Error(err.Error())
			os.Exit(1)
		}
		if persistErr != nil {
			tlsLog.Warn(fmt.Sprintf("%v (continuing with an unpersisted certificate -- every restart will generate a fresh, untrusted-again CA)", persistErr))
		}
		if caCertPEM != nil {
			fingerprint := sha256.Sum256(cert.Certificate[0])
			tlsLog.Info(fmt.Sprintf("generated a local CA (leaf fingerprint %x) -- served at /ca.crt for your browser or reverse proxy to trust", fingerprint))
			rootMux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-pem-file")
				w.Write(caCertPEM)
			})
		}
		httpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
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

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	logging.New("mikroview").Info(fmt.Sprintf("%s on %s, syslog udp/tcp on %s/%s", scheme, cfg.Listen.HTTP, cfg.Listen.SyslogUDP, cfg.Listen.SyslogTCP))
	var serveErr error
	if cfg.TLS.Enabled {
		serveErr = httpServer.ListenAndServeTLS("", "")
	} else {
		serveErr = httpServer.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		logging.New("http").Error(serveErr.Error())
		os.Exit(1)
	}
}

// runHealthcheck backs the `-healthcheck` mode used by the container's
// HEALTHCHECK (see Dockerfile). It hits the app's own /api/healthz over
// loopback and returns a process exit code, rather than opening any
// listeners itself.
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

// openAuthStoreForCLI is shared by runListUsers/runResetPassword: both
// need a real, persisted auth.Store and both refuse identically if one
// isn't configured -- an ephemeral store would make either command
// pointless (list nothing meaningful, or reset a password that vanishes
// on the next restart).
func openAuthStoreForCLI(cmd string) (*auth.Store, error) {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Auth.StorePath == "" {
		return nil, fmt.Errorf("auth.storePath is not configured -- %s has nothing persisted to work with", cmd)
	}
	return auth.Open(cfg.Auth.StorePath)
}

// runListUsers backs `-list-users` -- usernames and roles only, no
// password hashes, to help an operator pick which account to reset.
func runListUsers() int {
	store, err := openAuthStoreForCLI("-list-users")
	if err != nil {
		logging.New("list-users").Error(err.Error())
		return 1
	}

	users := store.List()
	if len(users) == 0 {
		fmt.Println("No accounts exist yet.")
		return 0
	}
	for _, u := range users {
		fmt.Printf("%s\t%s\n", u.Username, u.Role)
	}
	return 0
}

// runEnableAuthSetup backs `-enable-auth-setup` -- clears a prior
// explicit "skip auth" decision (see auth.Store.Disable) so the web
// setup form becomes reachable again on next load. It does not create
// an account itself; the operator (or whoever loads the UI next) still
// completes setup through the normal create-account form.
func runEnableAuthSetup() int {
	logger := logging.New("enable-auth-setup")
	store, err := openAuthStoreForCLI("-enable-auth-setup")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	if err := store.EnableSetup(); err != nil {
		logger.Error(err.Error())
		return 1
	}
	fmt.Println("Auth setup re-enabled -- the create-account form will be shown again on next load.")
	return 0
}

// runResetPassword backs `-reset-password <username>` -- the account-
// recovery path. Prompts for the new password twice (no echo) rather
// than accepting it as a CLI argument or env var, so it never touches
// shell history, process args, or `docker inspect` output; container/
// host access (the ability to exec this at all) is the trust anchor.
// Revokes every existing session for that user, so a stolen session
// cookie doesn't survive a deliberate credential reset.
func runResetPassword(args []string) int {
	logger := logging.New("reset-password")
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: mikroview -reset-password <username>")
		return 1
	}
	username := args[0]

	store, err := openAuthStoreForCLI("-reset-password")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	if _, ok := store.ByUsername(username); !ok {
		logger.Error(fmt.Sprintf("no such user %q -- run -list-users to see existing accounts", username))
		return 1
	}

	password, err := readPasswordTwice()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	if err := store.SetPassword(username, password, time.Now()); err != nil {
		logger.Error(err.Error())
		return 1
	}
	fmt.Printf("Password for %q updated.\n", username)
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
func ingest(ctx context.Context, raw <-chan syslog.RawMessage, st *store.Store, devices *device.Registry, macRegistry *device.MACRegistry, fs *flags.Store, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector, ru *rules.Store, names naming.Resolver) {
	ingestLog := logging.New("ingest")
	for {
		select {
		case <-ctx.Done():
			return
		case rm := <-raw:
			ingestOneRecovered(ingestLog, rm, st, devices, macRegistry, fs, h, geo, detector, ru, names)
		}
	}
}

// ingestOneRecovered isolates panic recovery to a single message
// rather than ingest's whole lifetime -- recover only unwinds as far as
// the nearest deferring function, so a defer in ingest itself would
// still end the entire ingest goroutine for good on the first bad
// message (silently stopping all future event processing) rather than
// just dropping that one message. See logging.Recover's doc comment.
func ingestOneRecovered(logger *slog.Logger, rm syslog.RawMessage, st *store.Store, devices *device.Registry, macRegistry *device.MACRegistry, fs *flags.Store, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector, ru *rules.Store, names naming.Resolver) {
	defer logging.Recover(logger)

	env := syslog.ParseEnvelope(rm.Data, rm.RecvTime)
	parsed := routeros.Parse(env.Message)
	deviceID := devices.Resolve(rm.SourceIP, rm.RecvTime)
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
		SrcHostName:  names.Host(parsed.SrcIP),
		DstHostName:  names.Host(parsed.DstIP),
		SrcCountry:   srcCountry,
		DstCountry:   dstCountry,
		NatIP:        parsed.NatIP,
		NatPort:      parsed.NatPort,
		NatRaw:       parsed.NatRaw,
		Length:       parsed.Length,
		Flags:        parsed.Flags,
		Raw:          parsed.Raw,
	}

	stored := st.Insert(e)
	h.Broadcast(stored)
	detector.Enqueue(stored)
	// Keeps internal/rules' long-lived per-rule usage record in sync with
	// internal/store/ring.go's own totalByRule bump inside Insert above --
	// same per-event trigger, so RuleUsage never drifts out of step with
	// what the store itself just counted (see internal/rules.Store.Touch's
	// doc comment for why this lives here rather than as a separate pass).
	ru.Touch(stored.RuleLabel, stored.ReceivedAt)
}
