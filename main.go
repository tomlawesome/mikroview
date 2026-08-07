// SPDX-License-Identifier: AGPL-3.0-only

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
	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/blocklist"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/detect"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/entities"
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
	// -list-users/-recover-admin-account: the account-recovery path (see
	// docs/configuration.md's "Authentication" section) -- container/
	// host access is the trust anchor for these, deliberately outside
	// the web UI/API entirely, so a locked-out admin isn't dependent on
	// the very system they're locked out of.
	if len(os.Args) > 1 && os.Args[1] == "-list-users" {
		os.Exit(runListUsers())
	}
	if len(os.Args) > 1 && os.Args[1] == "-recover-admin-account" {
		os.Exit(runRecoverAdminAccount(os.Args[2:]))
	}
	// -reset-password was the previous name, and reset *any* account's
	// password with host access alone. It is not kept as an alias: the
	// replacement is narrower (admin only) and gated on a recovery key,
	// so silently forwarding would misrepresent what now happens.
	if len(os.Args) > 1 && os.Args[1] == "-reset-password" {
		os.Exit(runRetiredResetPassword())
	}
	// -enable-auth-setup: the only way to re-arm the web setup form after
	// a deployment has explicitly skipped auth (see auth.Store.Disable) --
	// deliberately CLI-only, not exposed via any API endpoint, so a UI
	// visitor can never unilaterally re-impose auth for everyone else.
	if len(os.Args) > 1 && os.Args[1] == "-enable-auth-setup" {
		os.Exit(runEnableAuthSetup())
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
	if len(os.Args) > 1 && os.Args[1] == "-transfer-admin" {
		os.Exit(runTransferAdmin(os.Args[2:]))
	}

	configLog := logging.New("config")
	cfg, configResult, err := config.LoadWithProblems(os.Getenv("MIKROVIEW_CONFIG"), os.Args[1:])
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
	// rep: always built (AbuseIPDBKey empty just means that one source
	// inside it stays inert; Shodan InternetDB is free/keyless and
	// always queried).
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
	// A non-nil error from auth.Open, when persistence is actually
	// configured, ALWAYS means "the accounts file exists but couldn't be
	// read/parsed" -- Open returns (store, nil) for both "no persistence
	// configured" and "file genuinely doesn't exist yet" (see its own
	// doc comment), so this is never reached by a true fresh install.
	// Falling through to the normal boot sequence here used to mean the
	// in-memory store's Count() reads 0, which is *exactly* the state
	// requireAuth treats as "no decision made yet" -- silently
	// presenting a stranger with the first-run setup wizard on a
	// previously-authenticated instance, indistinguishable in the logs
	// from a genuine fresh install. That's a fail-OPEN on a security
	// control, not an acceptable degrade-and-continue case like every
	// other optional store above. Refuse to start instead: recovering
	// requires an explicit, conscious operator action, and container/
	// host access (the same trust anchor as every other CLI recovery
	// path) is already sufficient to take it directly -- move or delete
	// the broken file and restart, no dedicated CLI mode needed for
	// something `mv`/`rm` already does.
	if authShouldFailClosed(err, cfg.Auth.StorePath) {
		authLog.Error(fmt.Sprintf(
			"accounts file at %q exists but could not be loaded: %v -- refusing to start with authentication in an unknown state. "+
				"If this deployment previously had accounts configured, this is NOT a fresh install: restore the file from a backup, "+
				"or move/delete %q and restart to consciously re-arm the first-run setup screen (container/host access is required either way).",
			cfg.Auth.StorePath, err, cfg.Auth.StorePath,
		))
		os.Exit(1)
	}
	switch {
	case authStore.Count() > 0:
		authLog.Info(fmt.Sprintf("%d account(s) registered -- authentication is active", authStore.Count()))
	case authStore.Disabled():
		authLog.Warn("explicitly disabled for this deployment -- mikroview is fully open (run -enable-auth-setup to reverse this)")
	default:
		authLog.Info("no decision made yet -- mikroview is showing the first-run choice screen (see docs/configuration.md)")
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
	entityStore, err := entities.Open(cfg.Entities.StorePath)
	if err != nil {
		entitiesLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only entity state)", err))
	}
	if n := entityStore.Seed(cfg.RuleNames, cfg.HostNames); n > 0 {
		entitiesLog.Info(fmt.Sprintf("imported %d entries from config.yaml's ruleNames/hostNames (now UI-editable)", n))
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

	// Audit (issue #112): the persisted admin-action accountability log --
	// same optional-persistence, degrade-not-crash contract as every other
	// store above (a missing/unwritable path just means entries don't
	// survive a restart, not that mikroview fails to start).
	auditLog := logging.New("audit")
	auditStore, err := audit.Open(cfg.Audit.StorePath)
	if err != nil {
		auditLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only, unpersisted audit log)", err))
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
	detectorSettings, err := detect.OpenSettingsStore(cfg.Flags.DetectorSettingsStorePath, seed)
	if err != nil {
		detectorsLog.Warn(fmt.Sprintf("%v (continuing with in-memory-only detector toggle state)", err))
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

	// Both optional inputs are attached in one chain: entities backs the
	// trusted-mail-sender allowlist (#108), knownBad backs the local
	// blocklist match (#113 Part B). Each is independently a valid
	// no-op when unconfigured.
	detector := detect.NewWithSettings(detectCfg, fs, detectorSettings).
		WithReputation(rep).
		WithEntities(entityStore).
		WithKnownBadIPs(bl)
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

	// Entities takes precedence over Rules/Hosts for any key it has a
	// label for -- see naming.Resolver's doc comment and issue #107's
	// migration/precedence design.
	names := naming.Resolver{Rules: cfg.RuleNames, Hosts: cfg.HostNames, Entities: entityStore}

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
		Store:            st,
		Devices:          devices,
		Hub:              h,
		Reputation:       rep,
		Flags:            fs,
		DetectorSettings: detectorSettings,
		Entities:         entityStore,
		Rules:            ru,
		Audit:            auditStore,
		CriticalPorts:    cfg.Flags.CriticalPorts,
		DeviceStaleAfter: cfg.Flags.DeviceStaleAfter,
		Auth:             authStore,
		Sessions:         auth.NewSessionStore(cfg.Auth.SessionTTL),
		LoginLimiter:     auth.NewLoginLimiter(loginLimiterThreshold, loginLimiterWindow),
		SecureCookie:     cfg.Auth.SecureCookie,
		TrustedProxies:   trustedProxies,
		ClientIPHeader:   cfg.Listen.ClientIPHeader,
		Tokens:           tokenStore,
		OIDC:             oidcClient,
		OIDCState:        oidcState,
		OIDCPolicy:       oidcPolicy,
		StartTime:        time.Now(),
		Version:          version,
		ConfigProblems:   configProblems,
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
		// something fatal. Both are reported the same way to the operator;
		// only the exit code distinguishes them, and only when we can tell.
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
func openRecoveryStoreForCLI() (*auth.RecoveryStore, error) {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Auth.RecoveryKeysPath == "" {
		return nil, fmt.Errorf("auth.recoveryKeysPath is not configured")
	}
	return auth.OpenRecovery(cfg.Auth.RecoveryKeysPath, cfg.Auth.RecoveryPepperPath)
}

// printRecoveryKeys writes a freshly generated set to stdout, exactly
// once, and refuses when stdout is not a terminal.
//
// These commands run inside a container, so "printed once" can land
// somewhere durable and broadly readable -- the Docker log driver, a
// shipped log aggregator, the journal on a non-interactive run, or a
// pipe into a file nobody thought about. Keys written to a non-TTY are
// keys written to storage with weaker permissions than the file they
// protect. --i-will-capture-this exists for genuine scripted
// provisioning, but it has to be asked for.
func printRecoveryKeys(keys []string, allowNonTTY bool) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) && !allowNonTTY {
		return fmt.Errorf("refusing to print recovery keys because stdout is not a terminal -- " +
			"they would be written wherever this output is going (container logs, a file, a pipe). " +
			"Re-run interactively, or pass --i-will-capture-this if you are provisioning from a script")
	}
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
	return nil
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
	store, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	keys, err := store.Generate()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	if err := printRecoveryKeys(keys, hasFlag(args, "--i-will-capture-this")); err != nil {
		logger.Error(err.Error())
		return 1
	}
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
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		logger.Error("usage: mikroview -transfer-admin <username>")
		return 2
	}
	target := args[0]

	recovery, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	store, err := openAuthStoreForCLI("-transfer-admin")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

	current := store.Admin()
	if current == nil {
		logger.Error("this deployment has no admin account -- nothing to transfer")
		return 1
	}
	next, ok := store.ByUsername(target)
	if !ok {
		logger.Error(fmt.Sprintf("no such account: %s", target))
		return 1
	}
	fmt.Printf("Transfer admin from %q to %q.\n", current.Username, next.Username)
	// Stated before the key is asked for, not after: an SSO-only admin
	// cannot be recovered by -recover-admin-account, by design, so the
	// operator needs this before committing to it.
	if !next.LocalPassword() {
		fmt.Println()
		fmt.Printf("WARNING: %q signs in through your identity provider and has no local\n", next.Username)
		fmt.Println("password. After this transfer, admin recovery goes through your identity")
		fmt.Println("provider -- mikroview will not be able to recover that account itself.")
		fmt.Println()
	}

	fmt.Print("Recovery key: ")
	var key string
	if _, err := fmt.Scanln(&key); err != nil {
		logger.Error("no recovery key supplied")
		return 1
	}

	// Redeem verifies the key and prepares a replacement set, but does
	// not persist the rotation -- that waits for Commit below, so a lost
	// printout can't leave the operator with no valid keys.
	fresh, err := recovery.Redeem(key)
	if err != nil {
		// Deliberately one message for a wrong key and a corrupt store:
		// the distinction is only useful to someone probing.
		logger.Error(err.Error())
		return 1
	}

	from, to, err := store.TransferAdmin(target, time.Now())
	if err != nil {
		logger.Error(fmt.Sprintf("transfer failed, no keys were consumed: %v", err))
		return 1
	}

	if err := printRecoveryKeys(fresh, hasFlag(args, "--i-will-capture-this")); err != nil {
		logger.Warn(fmt.Sprintf("admin transferred from %s to %s, but the new keys could not be shown: %v -- "+
			"your previous recovery keys remain valid", from.Username, to.Username, err))
		return 1
	}
	if !confirmSaved() {
		// The transfer stands; only the rotation is abandoned, leaving
		// the previous keys valid. Better than rotating into a set the
		// operator never captured.
		logger.Warn(fmt.Sprintf("admin transferred from %s to %s, but the new keys were not confirmed -- "+
			"your previous recovery keys remain valid", from.Username, to.Username))
		return 1
	}
	if err := recovery.Commit(); err != nil {
		logger.Warn(fmt.Sprintf("admin transferred from %s to %s, but storing the new keys failed: %v -- "+
			"your previous recovery keys remain valid", from.Username, to.Username, err))
		return 1
	}
	logger.Info(fmt.Sprintf("admin transferred from %s to %s", from.Username, to.Username))
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

// openAuthStoreForCLI is shared by runListUsers/runRecoverAdminAccount: both
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

// authShouldFailClosed reports whether main()'s boot sequence should
// refuse to start rather than continue with an unauthenticated,
// zero-account in-memory auth.Store. err is auth.Open's own return
// value; storePath is the configured auth.storePath. auth.Open only
// ever returns a non-nil error when a persistence path IS configured
// and the file at it exists but couldn't be read/parsed -- both "no
// persistence configured" (storePath == "") and "file genuinely
// doesn't exist yet" (a real fresh install) return a nil error, so
// requiring storePath != "" here is belt-and-braces rather than the
// actual distinguishing signal, which is err itself.
func authShouldFailClosed(err error, storePath string) bool {
	return err != nil && storePath != ""
}

// runRetiredResetPassword explains where `-recover-admin-account` went.
//
// A removed recovery command is the worst thing to hit an operator who
// is already locked out, so it exits with a pointer rather than an
// unrecognised-flag error. Exit 2 (usage), not 1: nothing failed, the
// command simply no longer exists.
func runRetiredResetPassword() int {
	logger := logging.New("reset-password")
	logger.Error("-reset-password has been replaced by -recover-admin-account, which recovers " +
		"the admin account only and requires a recovery key. Other accounts are managed by the " +
		"admin from the web UI. See docs/configuration.md, \"Recovering the admin account\"")
	return 2
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

	recovery, err := openRecoveryStoreForCLI()
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	store, err := openAuthStoreForCLI("-recover-admin-account")
	if err != nil {
		logger.Error(err.Error())
		return 1
	}

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
			"-transfer-admin to move admin to an account that does have one", admin.Username))
		return 1
	}

	fmt.Printf("Recover the admin account %q.\n", admin.Username)
	fmt.Print("Recovery key: ")
	var key string
	if _, err := fmt.Scanln(&key); err != nil {
		logger.Error("no recovery key supplied")
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
	if err := printRecoveryKeys(fresh, hasFlag(args, "--i-will-capture-this")); err != nil {
		logger.Warn(fmt.Sprintf("admin account recovered, but the new keys could not be shown: %v -- "+
			"your previous recovery keys remain valid", err))
		return 1
	}
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
		SrcPortName:  names.Port(parsed.SrcPort),
		DstPortName:  names.Port(parsed.DstPort),
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
