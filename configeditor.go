// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/gate"
	goidc "github.com/tomlawesome/gauntlet/oidc"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/configsnap"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/prefs"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/servertls"
	"github.com/tomlawesome/mikroview/internal/tlssniff"
)

// releaseFile is the repo's VERSION file: the release this source tree
// is, or is on the way from. The config editor (#1347) needs a release
// number for the header it writes and the download's name, and
// main.version only carries one for a tagged build -- see
// runningRelease.
//
//go:embed VERSION
var releaseFile string

var releaseTagPattern = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

// runningRelease is this build's release as the config editor names it,
// without a leading "v": the tag itself for a release build ("0.6.1"),
// and VERSION plus the lane and commit for anything else
// ("0.6.1-dev.abc1234"), so a file written by a development build never
// claims to be from the release it merely follows. Only characters a
// file name can carry.
func runningRelease(buildVersion string) string {
	if m := releaseTagPattern.FindStringSubmatch(buildVersion); m != nil {
		return m[1]
	}
	lane, commit, _ := strings.Cut(buildVersion, ":")
	out := strings.TrimSpace(releaseFile) + "-" + lane
	if commit != "" {
		out += "." + commit
	}
	return regexp.MustCompile(`[^A-Za-z0-9.\-]`).ReplaceAllString(out, "")
}

// configSnapshotsPath is where the config editor's snapshots live:
// beside the accounts store in the data directory, the same way the
// Postgres adoption marker is placed, rather than at a configurable path
// of its own -- a new config key would have been one more setting in a
// file this very feature exists to make smaller, for a file nobody has a
// reason to move. Empty when the accounts store itself is not persisted
// (auth.storePath set to ""): there is then no data directory this
// deployment has said it keeps, and snapshots follow the accounts.
func configSnapshotsPath(cfg config.Config) string {
	if cfg.Auth.StorePath == "" {
		return ""
	}
	return filepath.Join(dataDir(cfg), "config-snapshots.json")
}

// configSnapshotsStore is the snapshot store's name: its name in a
// backup, and the name its sealed file is bound to (#1450).
const configSnapshotsStore = "config_snapshots"

// openConfigSnapshots opens the snapshot store sealed under the
// retention key, the way router backups are sealed (see
// openRouterBackupVault), and always as a file: a snapshot is a whole
// config.yaml, secrets included, and the Postgres backend stores its
// documents in the clear. With no usable key it returns nil and the
// reason, in words for the admin -- there is no unencrypted mode.
func openConfigSnapshots(st *storage, cfg config.Config) (*configsnap.Store, string) {
	path := configSnapshotsPath(cfg)
	switch {
	case path == "":
		return nil, "auth.storePath is empty, so this deployment keeps no data directory for them"
	case st.keyErr != nil:
		return nil, fmt.Sprintf("history.keyFile is set but could not be used (%v); snapshots hold the config's secrets, so they are only ever kept sealed under that key", st.keyErr)
	case st.key == nil:
		return nil, "snapshots hold the config's secrets, so they are only ever kept sealed under the retention key, and history.keyFile is not set"
	}
	store, err := configsnap.Open(context.Background(), persist.NewEncryptedFileBackend(path, configSnapshotsStore, st.key))
	if err != nil {
		return nil, err.Error()
	}
	return store, ""
}

// newConfigEditor gathers what the config editor needs: the file
// start-up read, the release, and the snapshot store.
func newConfigEditor(log *slog.Logger, st *storage, cfg config.Config, configPath string) *api.ConfigEditor {
	snaps, why := openConfigSnapshots(st, cfg)
	if snaps == nil {
		log.Info("config editor snapshots are off: " + why)
	}
	return &api.ConfigEditor{
		Path:                 configPath,
		StartupText:          readRawConfigYAML(configPath),
		RunningVersion:       runningRelease(version),
		Snapshots:            snaps,
		SnapshotsUnavailable: why,
	}
}

// runSetupOnly is what a refused config starts instead of exiting
// (#1347, owner's "13a"): the UI shell, /api/auth/*, /api/healthz and the
// config editor, and nothing else -- no ingest, no syslog, no other
// listener, no scheduler. It returns only when the server stops, with
// the process's exit code: 1 when it could not start at all, which is
// the same refusal a refused config always was.
//
// Kept apart from main's own start-up on purpose: that path assumes a
// valid config at every step, and a flag threaded through it would be a
// flag every later change there has to remember.
func runSetupOnly(result config.Result, problems int) int {
	log := logging.New("setup-only")
	salvage := config.SetupOnlyConfig(readRawConfigYAML(result.ConfigPath), os.Args[1:])
	cfg := salvage.Config

	// The same refusal as a normal start (#1202, ruling 12), and first:
	// the accounts this mode signs the admin in with are sealed under
	// the key, found here as at a normal start -- history.keyFile if the
	// file's history block survived, MIKROVIEW_HISTORY_KEY_FILE, or the
	// app folder's keys/history.key.
	//
	// With the history section unreadable, history.keyFile is unknown
	// rather than unset, and saying "no key is mounted" would send the
	// operator looking in the wrong place (#1202 review R13).
	if salvage.Broken["history"] || salvage.Broken["(whole file)"] {
		if _, err := retention.LoadKey(cfg.History.KeyFile); errors.Is(err, retention.ErrNoKey) {
			p := config.KeyRequiredHistoryUnreadable(config.AppFolderHistoryKeyPath())
			log.Error(fmt.Sprintf("refusing to start the config editor:\n%s", config.Report([]config.Problem{p})))
			return 1
		}
	}
	if !requireKey(log, cfg, "start the config editor") {
		return 1
	}

	// ui.allow limits who may reach the UI at all. If the refusal is in
	// that list, the operator's restriction cannot be honoured, and
	// serving the editor to everyone instead would widen exactly what
	// they narrowed -- so that case refuses as a refused config always
	// did.
	if salvage.Broken["ui"] || salvage.Broken["(whole file)"] {
		log.Error("the ui section (or the whole file) could not be read, so ui.allow cannot be honoured -- not starting the config editor either")
		return 1
	}
	uiAllow, err := config.ParseUIAllow(cfg.UI.Allow)
	if err != nil {
		log.Error(fmt.Sprintf("ui.allow: %v -- not starting the config editor either, since who may reach it cannot be honoured", err))
		return 1
	}
	trustedProxies, err := config.ParseTrustedProxies(cfg.Listen.TrustedProxies)
	if err != nil {
		trustedProxies = nil // forwarding headers are then ignored: the safe reading
	}

	// Accounts that live in Postgres are out of reach here: this mode
	// reads a file accounts store only, and on a deployment that moved
	// to Postgres the file left behind is stale -- signing in against it
	// could admit a password changed since. And a deployment only now
	// being pointed at Postgres must not make that one-way move from
	// here, adopting a few stores and stranding the rest (see
	// storage.backendFor).
	if cfg.Postgres.DSNFile != "" {
		if postgresAlreadyAdopted(cfg) {
			log.Error("this deployment keeps its accounts in Postgres, and setup-only mode reads only a file accounts store -- not starting the config editor; check the config with -validate-config instead")
			return 1
		}
		cfg.Postgres.DSNFile = ""
	}
	// Never migrate here: a rollback to the previous release starts it
	// on this same data with the old config, and data stamped by this
	// build would be refused. Checking touches nothing: the sealed files
	// are opened, but with their re-seal deferred (PROP-R1) they are not
	// rewritten.
	if err := persist.CheckFileSchema(dataDir(cfg)); err != nil {
		log.Error(err.Error())
		return 1
	}
	if err := checkNoRestoreInProgress(cfg); err != nil {
		log.Error(err.Error())
		return 1
	}

	ctx := context.Background()
	st, err := openStorage(ctx, cfg)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	defer st.Close()

	authBackend, err := st.authBackendFor(ctx, authStoreAccounts, cfg.Auth.StorePath)
	if err != nil {
		log.Error(fmt.Sprintf("preparing the accounts store: %v", err))
		return 1
	}
	// quietStoreOptions: this editor refuses an empty store just below,
	// so a setup code announced here could never be used (#1415).
	storeOpts := quietStoreOptionsFor(cfg)
	storeOpts.Log = logging.New("auth")
	authStore, err := gauntlet.OpenStore(authBackend, storeOpts)
	// A plaintext accounts file is refused here as at a normal start
	// (#1202, ruling 19; review R23): nothing seals it, at any schema.
	if errors.Is(err, persist.ErrPlaintextDocument) {
		log.Error(fmt.Sprintf("%s holds the accounts in the clear: it was written without a key before v0.7.0, and "+
			"MikroView does not seal plaintext in place. Not starting the config editor: restore a sealed copy from a "+
			"backup taken with the key mounted, or move it aside and start fresh", authBackend.Describe()))
		return 1
	}
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	// No account means nobody to sign in and fix the file -- and the
	// first-run screen would hand the admin account to whoever reached
	// this port first, on an instance that is not otherwise running.
	// Refuse, as a refused config always did.
	if authStore.Count() == 0 {
		log.Error(fmt.Sprintf("no account in %s to sign in with -- the config editor needs an admin, so not starting it", authBackend.Describe()))
		return 1
	}
	// Whether admins must sign in with a passkey is worked out from
	// publicUrl unless auth.adminPasskey says (ruling 1). With either
	// unreadable the rule is unknown, and the worked-out answer would
	// fall to "optional" -- letting an admin who holds a passkey in on
	// a password alone. Fail closed when one does (ruling 16, #1202
	// review R17); with no admin holding a passkey nothing is loosened.
	if field, unknown := passkeyRuleUnreadable(salvage, cfg); unknown && anyAdminHoldsPasskey(authStore) {
		log.Error(fmt.Sprintf("the config file's %s could not be read, so setup-only mode cannot tell whether admins must "+
			"sign in with a passkey, and an admin holds one -- not starting the config editor, rather than let that "+
			"admin in without it. Fix the file on disk and check it with -validate-config", field))
		return 1
	}
	// No token reaches anything in this mode (SetupOnlyRoutes registers
	// no bearer mux), so the token store is an empty one, never the
	// deployment's own.
	tokens, err := gauntlet.OpenTokenStore(nil, gauntlet.TokenOptions{Kinds: api.TokenKinds})
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	// Sign-ins here are recorded like any other (ruling 8): this mode is
	// the only process running, so it owns the history.
	signIns, err := openSignIns(ctx, st, cfg, logging.New("signins"))
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	defer func() { _ = signIns.Close() }()
	auditStore := openOptionalStore(ctx, log, st, "audit", cfg.Audit.StorePath, audit.OpenWithBackend)
	prefsStore := openOptionalStore(ctx, log, st, "prefs", cfg.Prefs.StorePath, prefs.OpenWithBackend)
	if auditStore == nil || prefsStore == nil {
		return 1
	}
	loginLimiter, err := gauntlet.NewLoginLimiter(loginLimiterThreshold, loginLimiterWindow)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	loginLimiter.SetLog(storeOpts.Log) // #1202 review R4, as at a normal start
	idle, ceiling := sessionLimits(cfg.Auth)

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &api.Server{
		Auth:           authStore,
		Sessions:       gauntlet.NewSessionStore(idle, ceiling),
		LoginLimiter:   loginLimiter,
		Tokens:         tokens,
		Audit:          auditStore,
		Prefs:          prefsStore,
		TrustedProxies: trustedProxies,
		ClientIPHeader: cfg.Listen.ClientIPHeader,
		UIAllow:        uiAllow,
		StartTime:      time.Now(),
		Version:        version,
		ConfigEditor:   newConfigEditor(log, st, cfg, result.ConfigPath),
	}
	var oidcClient *goidc.Client
	var oidcState *goidc.StateCodec
	var oidcPolicy goidc.Policy
	if salvage.OIDCUsable {
		oidcClient, oidcState, oidcPolicy = startOIDC(sigCtx, cfg, authStore)
	} else if cfg.OIDC.IssuerURL != "" {
		log.Warn("single sign-on is off: the refusal is inside the oidc block -- sign in with a local account")
	}
	// The relying party and the admin passkey rule are decided exactly as
	// a normal start decides them. A relying party the WebAuthn library
	// refused is a bug, and stops this mode as it stops a normal start:
	// carrying on would decide the passkey rule without it.
	rp, err := newRelyingParty(cfg)
	if err != nil {
		log.Error(fmt.Sprintf("building the WebAuthn relying party from publicUrl: %v", err))
		return 1
	}
	passkeys := passkeysFor(rp)
	rule := adminPasskeyRule(cfg, rp)
	// Ruling 17 (#1202 review R18): auth.adminPasskey: required with a
	// publicUrl that cannot offer passkeys (an IP address) is the very
	// combination CFG-0023 refuses, so it is what starts this mode. An
	// admin held to a passkey that cannot be made here would be held at
	// that door with no way through it, editor and all. When no admin
	// holds a passkey (and an IP address can never have issued one)
	// nothing is lost by treating the rule as optional, so it is, and
	// the log says so. When an admin does hold one, the rule is not
	// loosened: this mode refuses and the file is fixed on disk, as in
	// ruling 16. A normal start is unaffected and still refuses
	// (CFG-0023).
	if rule == gate.AdminPasskeyRequired && passkeys == nil {
		if anyAdminHoldsPasskey(authStore) {
			log.Error("auth.adminPasskey is required, but passkeys cannot be offered at the configured publicUrl -- not starting the config editor; fix the file on disk and check it with -validate-config")
			return 1
		}
		log.Warn("auth.adminPasskey is required, but passkeys cannot be made at the configured publicUrl and no admin holds one -- the admin passkey rule is relaxed (treated as optional) for setup-only mode; fix the file on disk and restart")
		rule = gate.AdminPasskeyOptional
	}
	g, err := srv.NewGate(api.GateOptions{
		SecureCookie: cfg.Auth.SecureCookie,
		AdminPasskey: rule,
		Passkeys:     passkeys,
		OIDC:         oidcClient,
		OIDCState:    oidcState,
		OIDCPolicy:   oidcPolicy,
		SignIns:      signIns,
		Log:          logging.New("gate"),
	})
	if err != nil {
		log.Error(fmt.Sprintf("building the sign-in layer: %v", err))
		return 1
	}
	srv.Gate = g

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", srv.SetupOnlyRoutes())
	if ui := frontendHandler(); ui != nil {
		rootMux.Handle("/", ui)
	}
	httpServer := &http.Server{
		Addr:              cfg.Listen.HTTP,
		Handler:           securityHeaders(srv.RestrictToAllowList(rootMux), cfg.TLS.Enabled && cfg.TLS.CertFile != ""),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          logging.HTTPErrorLog(logging.New("http")),
	}
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
		cert, _, _, err := servertls.Load(servertls.Config{
			CertFile: cfg.TLS.CertFile, KeyFile: cfg.TLS.KeyFile, Hosts: cfg.TLS.Hosts, StorePath: cfg.TLS.StorePath,
		})
		if err != nil {
			log.Error(err.Error())
			return 1
		}
		httpServer.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Warn(fmt.Sprintf("config refused (%d problems) -- serving the config editor only at %s; nothing else is running until the config is fixed and MikroView restarted",
		problems, setupOnlyURL(scheme, cfg)))

	var serveErr error
	if cfg.TLS.Enabled {
		ln, err := net.Listen("tcp", httpServer.Addr)
		if err != nil {
			log.Error(err.Error())
			return 1
		}
		serveErr = httpServer.ServeTLS(tlssniff.Listener(ln, logging.New("http"), func(requested string) string {
			return samePortRedirectHost(requested, cfg.TLS.Hosts, ln.Addr().String())
		}), "", "")
	} else {
		serveErr = httpServer.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Error(serveErr.Error())
		return 1
	}
	// Stopped by a signal. Still a refused config: exit non-zero, so
	// nothing watching the exit code reads this as a healthy stop.
	return 1
}

// passkeyRuleUnreadable reports whether setup-only mode cannot work out
// the admin passkey rule (ruling 16), and names the part of the file
// that is to blame: the auth section (auth.adminPasskey itself), or
// publicUrl when auth.adminPasskey is not set to settle it.
func passkeyRuleUnreadable(salvage config.SetupOnly, cfg config.Config) (string, bool) {
	switch {
	case salvage.Broken["auth"]:
		return "auth section", true
	case salvage.Broken["publicUrl"] && cfg.Auth.AdminPasskey == "":
		return "publicUrl", true
	}
	return "", false
}

// anyAdminHoldsPasskey reports whether an admin account holds at least
// one passkey.
func anyAdminHoldsPasskey(store *gauntlet.Store) bool {
	for _, a := range store.Admins() {
		if store.PasskeyCount(a.ID) > 0 {
			return true
		}
	}
	return false
}

// openOptionalStore opens one of setup-only mode's two small stores
// through the same backend policy the server uses -- memory-only where
// that policy persists nothing (no retention key), since both stores
// treat a nil backend that way. nil means the store exists but could not
// be read: refused, as the server refuses (see mustOpenStore).
func openOptionalStore[T any](ctx context.Context, log *slog.Logger, st *storage, name, path string, open func(persist.Backend) (*T, error)) *T {
	b, err := st.backendFor(ctx, name, path)
	if err != nil {
		log.Error(fmt.Sprintf("preparing the %s store: %v", name, err))
		return nil
	}
	s, err := open(b)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return s
}

// setupOnlyURL is where the log line tells the operator to go.
func setupOnlyURL(scheme string, cfg config.Config) string {
	if cfg.PublicURL != "" {
		return cfg.PublicURL
	}
	host, port, err := net.SplitHostPort(cfg.Listen.HTTP)
	if err != nil {
		return scheme + "://" + cfg.Listen.HTTP
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
		if len(cfg.TLS.Hosts) > 0 {
			host = cfg.TLS.Hosts[0]
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}
