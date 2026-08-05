package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/tomlawesome/mikroview/internal/reputation"
	"github.com/tomlawesome/mikroview/internal/routeros"
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

// loginLimiter{Threshold,Window}: brute-force protection on
// POST /api/auth/login (see internal/auth.LoginLimiter) -- an internal
// hardening constant, not exposed via config, same tier as ws.go's
// wsPongTimeout/wsPingInterval.
const (
	loginLimiterThreshold = 5
	loginLimiterWindow    = 5 * time.Minute
)

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
	detectCfg := detect.Config{
		PortScanThreshold:      cfg.Flags.PortScanThreshold,
		PortScanWindow:         cfg.Flags.PortScanWindow,
		ActivitySpikeThreshold: cfg.Flags.ActivitySpikeThreshold,
		ActivitySpikeWindow:    cfg.Flags.ActivitySpikeWindow,
		CriticalPorts:          cfg.Flags.CriticalPorts,
		CriticalPortThreshold:  cfg.Flags.CriticalPortThreshold,
		CriticalPortWindow:     cfg.Flags.CriticalPortWindow,
		GlobalSpikeMultiplier:  cfg.Flags.GlobalSpikeMultiplier,
		GlobalSpikeMinEPS:      cfg.Flags.GlobalSpikeMinEPS,

		DistributedBruteForceThreshold: cfg.Flags.DistributedBruteForceThreshold,
		DistributedBruteForceWindow:    cfg.Flags.DistributedBruteForceWindow,

		OutboundAnomalyThreshold: cfg.Flags.OutboundAnomalyThreshold,
		OutboundAnomalyWindow:    cfg.Flags.OutboundAnomalyWindow,

		InternalReconThreshold: cfg.Flags.InternalReconThreshold,
		InternalReconWindow:    cfg.Flags.InternalReconWindow,

		RuleSpikeMultiplier: cfg.Flags.RuleSpikeMultiplier,
		RuleSpikeMinRate:    cfg.Flags.RuleSpikeMinRate,
		RuleSpikeWindow:     cfg.Flags.RuleSpikeWindow,

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Notify (issues #30/#31): alerting on newly-raised flags outside the
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

	go ingest(ctx, raw, st, devices, h, geo, detector, names)

	go func() {
		ticker := time.NewTicker(globalSpikeCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				globalSpike.Check(st.Stats().EventsPerSecond, time.Now())
			}
		}
	}()

	srv := &api.Server{
		Store:            st,
		Devices:          devices,
		Hub:              h,
		Reputation:       rep,
		Flags:            fs,
		DetectorSettings: detectorSettings,
		CriticalPorts:    cfg.Flags.CriticalPorts,
		Auth:             authStore,
		Sessions:         auth.NewSessionStore(cfg.Auth.SessionTTL),
		LoginLimiter:     auth.NewLoginLimiter(loginLimiterThreshold, loginLimiterWindow),
		SecureCookie:     cfg.Auth.SecureCookie,
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
		Addr:    cfg.Listen.HTTP,
		Handler: rootMux,
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
		cert, caCertPEM, err := servertls.Load(servertls.Config{
			CertFile:  cfg.TLS.CertFile,
			KeyFile:   cfg.TLS.KeyFile,
			Hosts:     cfg.TLS.Hosts,
			StorePath: cfg.TLS.StorePath,
		})
		if err != nil {
			tlsLog.Error(err.Error())
			os.Exit(1)
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
// Registry never need to arbitrate concurrent writers.
func ingest(ctx context.Context, raw <-chan syslog.RawMessage, st *store.Store, devices *device.Registry, h *hub.Hub, geo *geoip.Lookup, detector *detect.Detector, names naming.Resolver) {
	for {
		select {
		case <-ctx.Done():
			return
		case rm := <-raw:
			env := syslog.ParseEnvelope(rm.Data, rm.RecvTime)
			parsed := routeros.Parse(env.Message)
			deviceID := devices.Resolve(rm.SourceIP, rm.RecvTime)
			srcCountry, _ := geo.Country(parsed.SrcIP)
			dstCountry, _ := geo.Country(parsed.DstIP)

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
			detector.Observe(stored)
		}
	}
}
