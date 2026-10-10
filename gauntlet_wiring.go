// SPDX-License-Identifier: AGPL-3.0-only

package main

// Authentication on gauntlet (#1202, docs/decisions/gauntlet-adoption.md
// decision 3): how the server, setup-only mode and the command-line
// tools open gauntlet's stores, and the start-up choices main makes for
// its gate. Kept out of main() so the three entry points share one
// answer to each question rather than three copies of it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/blocklist"
	"github.com/tomlawesome/gauntlet/gate"
	"github.com/tomlawesome/gauntlet/passkey"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// setupCodeLine is the setup code's log line, in mikroview's own words:
// scripts/setup-code.sh, the live-check harness and docs/configuration.md
// all match this text, so it is mikroview's to keep even though
// gauntlet's default wording is the same today.
func setupCodeLine(code string) string {
	return fmt.Sprintf("no account exists yet -- create the first admin with setup code %s (valid until an account exists or this process restarts)", code)
}

// serverStoreOptions is how the running server opens the accounts store:
// the setup code and the lone admin's unlock code each go to the log as
// one Warn line on the auth logger, where an operator with access to the
// machine reads them (gauntlet ADR-0003), and new passwords are checked
// against the refreshed common-password list. BreachCheck is nil: no
// password ever leaves the process for Have I Been Pwned (ruling 7).
// AllowPlaintextAtRest is never set -- see authBackendFor.
func serverStoreOptions(log *slog.Logger, passwords gauntlet.PasswordList) gauntlet.Options {
	return gauntlet.Options{
		Log:         log,
		ProductName: api.ProductName,
		OnSetupCode: gauntlet.SetupCodeFunc(func(code string) { log.Warn(setupCodeLine(code)) }),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(username, code string) {
			log.Warn(fmt.Sprintf("sign-in for the admin account %q is disabled after %d failed attempts in a row, and no other admin can unlock it -- "+
				"lift the disable with unlock code %s (valid until the account is unlocked or this process restarts; "+
				"the admin then signs in with their existing password and second factor). Without it the disable lifts itself %s after it began",
				logging.Printable(username), gauntlet.MaxConsecutiveLoginFailures, code, gauntlet.LoginDisableDuration))
		}),
		PasswordBlocklist: passwords,
	}
}

// quietStoreOptions is how everything but the running server opens the
// accounts store: the command-line tools and setup-only mode. Neither
// announces a code. The running server does not know a code another
// process minted and would refuse it, and setup-only mode refuses an
// empty store outright (gauntlet ADR-0003 decision 6, #1415).
func quietStoreOptions() gauntlet.Options {
	return gauntlet.Options{
		ProductName:  api.ProductName,
		OnSetupCode:  gauntlet.SetupCodeFunc(func(string) {}),
		OnUnlockCode: gauntlet.UnlockCodeFunc(func(string, string) {}),
	}
}

// quietStoreOptionsFor is quietStoreOptions with the common-password
// list a password set here is checked against: the newer copy the
// server keeps, if it has one (storedPasswordList), not only the one
// built into this release (#1202 review R6).
func quietStoreOptionsFor(cfg config.Config) gauntlet.Options {
	opts := quietStoreOptions()
	if list := storedPasswordList(cfg); list != nil {
		opts.PasswordBlocklist = list
	}
	return opts
}

// storedPasswordList is the common-password list as the server last
// kept it, for the command-line tools and setup-only mode: the newer
// verified copy in passwordListDir, or the list built into this release
// when there is none. Read only -- NewRefresher makes no request and
// writes nothing, and Run, which would fetch, is never called here: the
// server owns the fetch and the directory. nil only if the refresher
// refuses its own configuration, which is the built-in list again
// (gauntlet uses it when Options.PasswordBlocklist is nil).
func storedPasswordList(cfg config.Config) *blocklist.Refresher {
	r, err := blocklist.NewRefresher(blocklist.RefreshConfig{Dir: passwordListDir(cfg), Log: logging.New("passwordlist")})
	if err != nil {
		return nil
	}
	return r
}

// openTokens opens the token store with every kind mikroview issues
// (api.TokenKinds) -- a kind left out would leave the drop-list keys
// routers already hold in the document but unable to authenticate.
func openTokens(ctx context.Context, st *storage, cfg config.Config, log *slog.Logger) (*gauntlet.TokenStore, error) {
	b, err := st.authBackendFor(ctx, authStoreTokens, cfg.Auth.TokensStorePath)
	if err != nil {
		return nil, err
	}
	return gauntlet.OpenTokenStore(b, gauntlet.TokenOptions{Log: log, Kinds: api.TokenKinds})
}

// openSignIns opens the sign-in history (ruling 8), sealed like the
// other auth stores. Closed at shutdown, which saves its last rows.
func openSignIns(ctx context.Context, st *storage, cfg config.Config, log *slog.Logger) (*gauntlet.SignInHistory, error) {
	b, err := st.authBackendFor(ctx, authStoreSignIns, cfg.Auth.SignInsStorePath)
	if err != nil {
		return nil, err
	}
	return gauntlet.OpenSignInHistory(b, gauntlet.SignInHistoryOptions{Log: log})
}

// sessionLimits is auth.sessionTTL and auth.sessionMaxLifetime cut down
// to what gauntlet allows (ruling 3): at most an hour idle and a day in
// total -- NIST SP 800-63B-4's AAL2 limits, which gate.New refuses to
// exceed. Silently: no Warn, because every existing config carries the
// old 24-hour default and would warn on every start; the CHANGELOG
// says so instead. A ceiling of 0 used to mean none at all, and is cut
// to the day like any other value above it. An idle limit of 0 or less
// (CFG-0020 refuses it at a normal start; setup-only mode may still meet
// it) is a session that never idles out, and is cut to the hour the
// same way.
func sessionLimits(a config.Auth) (idle, ceiling time.Duration) {
	idle, ceiling = a.SessionTTL, a.SessionMaxLifetime
	if idle <= 0 || idle > gauntlet.MaxSessionIdle {
		idle = gauntlet.MaxSessionIdle
	}
	if ceiling <= 0 || ceiling > gauntlet.MaxSessionLifetime {
		ceiling = gauntlet.MaxSessionLifetime
	}
	return idle, ceiling
}

// newRelyingParty builds the passkey relying party from publicUrl, which
// config validation has already checked (CFG-0100..0103). An unusable
// address is not an error: the relying party reports why passkeys are
// off in its Status. An error means the WebAuthn library refused a
// configuration this code believed well-formed -- a bug, which the
// caller refuses to start on rather than ship passkeys missing with
// nothing said.
func newRelyingParty(cfg config.Config) (*passkey.RelyingParty, error) {
	return passkey.New(passkey.Config{PublicURL: cfg.PublicURL, DisplayName: api.ProductName})
}

// adminPasskeyRule is ruling 1: every admin must hold a passkey when
// MikroView is served over https on a domain name and passkeys work
// there; optional otherwise. auth.adminPasskey overrides either way --
// config validation has already refused "required" where no passkey
// could be made (CFG-0023), so gate.New's own refusal of that is never
// what an operator meets.
//
// "https", not merely a ready relying party: plain http on localhost is
// ready too (browsers count it as a secure context), but it is a
// developer's address, and the ruling names https.
func adminPasskeyRule(cfg config.Config, rp *passkey.RelyingParty) gate.AdminPasskeyRule {
	switch cfg.Auth.AdminPasskey {
	case config.AdminPasskeyRequired:
		return gate.AdminPasskeyRequired
	case config.AdminPasskeyOptional:
		return gate.AdminPasskeyOptional
	}
	u, err := url.Parse(cfg.PublicURL)
	if err == nil && u.Scheme == "https" && rp.Status() == gauntlet.PasskeyStatusReady {
		return gate.AdminPasskeyRequired
	}
	return gate.AdminPasskeyOptional
}

// passkeysFor is the relying party as gate takes it: nil when passkeys
// are off, so every passkey route answers 404 and the session body
// leaves them out, rather than a relying party that refuses every
// ceremony.
func passkeysFor(rp *passkey.RelyingParty) gauntlet.PasskeyCeremony {
	if rp.Status() != gauntlet.PasskeyStatusReady {
		return nil
	}
	return rp
}

// passwordListDir is where the refreshed common-password list is kept:
// beside the accounts store, in the data directory. The list is public,
// so it is not sealed.
func passwordListDir(cfg config.Config) string {
	return filepath.Join(dataDir(cfg), "password-list")
}

// newPasswordList builds the common-password list's refresher (ruling
// 7, gauntlet ADR-0007). It starts from the signed list built into this
// release, or the newer verified copy kept in passwordListDir, so it is
// usable at once; NewRefresher makes no request, and Run -- started once
// the server's context exists -- fetches gauntlet's published list,
// checksum first, about daily, adopting only one whose checksum and
// signature verify. A failed fetch is one Warn and the list in use
// stays; only a directory that cannot be made fails start-up.
//
// MV_TEST_PASSWORDLIST_URL moves the fetch to a test's own host, honoured
// only alongside MV_TEST_HOOKS=1 -- the same off-unless-asked rule as
// every other test hook, and never an operator setting.
func newPasswordList(cfg config.Config) (*blocklist.Refresher, error) {
	// Made now, not at the first fetch that adopts a list: a directory
	// that cannot be made stops start-up here, naming it, rather than
	// leaving every refresh to fail with a Warn nobody reads (#1202
	// review R5).
	if err := os.MkdirAll(passwordListDir(cfg), 0o700); err != nil {
		return nil, err
	}
	rc := blocklist.RefreshConfig{Dir: passwordListDir(cfg), Log: logging.New("passwordlist")}
	if os.Getenv("MV_TEST_HOOKS") == "1" {
		rc.URL = os.Getenv("MV_TEST_PASSWORDLIST_URL")
	}
	return blocklist.NewRefresher(rc)
}

// openRecoveryKeysSealed opens the recovery-key digests' document at
// start-up, through the same sealed backend the command-line tools use,
// so a plaintext one an earlier keyless release left refuses start-up,
// as the other auth stores do (#1202, ruling 19). It only reads: the store itself, and
// its pepper, are the command-line tools' (recoverykeys). A document
// that will not open, or is not JSON, is a *persist.StartupError.
func openRecoveryKeysSealed(ctx context.Context, st *storage, cfg config.Config) error {
	b, err := st.authBackendFor(ctx, authStoreRecoveryKeys, cfg.Auth.RecoveryKeysPath)
	if err != nil || b == nil {
		return err
	}
	_, _, err = persist.Open(ctx, b, "the recovery-key store", func(data []byte) error {
		if !json.Valid(data) {
			return errors.New("not a JSON document")
		}
		return nil
	})
	return err
}

// firstTokenSweepDelay is how long after start-up the first token sweep
// runs. Soon rather than a day in: a process that restarts more often
// than daily would otherwise never sweep at all.
const firstTokenSweepDelay = 5 * time.Second

// tokenSweepInterval is the sweep's cadence after that (ruling 5).
const tokenSweepInterval = 24 * time.Hour

// runTokenSweep is the daily token sweep (ruling 5): tokens unused for a
// year are removed, and those whose creating account is gone, each
// audited by gate. Returns when ctx ends.
func runTokenSweep(ctx context.Context, g *gate.Gate, log *slog.Logger) {
	defer logging.Recover(log)
	wait := firstTokenSweepDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = tokenSweepInterval
		res, err := g.SweepTokens(ctx, time.Now())
		if err != nil {
			log.Warn(fmt.Sprintf("token sweep: %v -- what could not be saved is tried again at the next sweep", err))
		}
		if res.Removed > 0 || res.Orphaned > 0 {
			log.Info(fmt.Sprintf("token sweep removed %d token(s) unused for a year and %d whose creator is gone", res.Removed, res.Orphaned))
		}
	}
}

// cliRequireKey is requireKey for the four commands that open the
// account stores: the same refusal as start-up, before anything is
// opened or prompted for. It also refuses a data directory a newer
// build wrote (persist.CheckFileSchema, #1202 review R25), as start-up
// does, so a command never rewrites newer data in this build's older
// shapes; checking writes nothing.
func cliRequireKey(log *slog.Logger, cmd string) bool {
	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		// Reported, with the config's own words, by the store opener
		// that runs next.
		return true
	}
	if !requireKey(log, cfg, "run "+cmd) {
		return false
	}
	if err := persist.CheckFileSchema(dataDir(cfg)); err != nil {
		log.Error(err.Error())
		return false
	}
	return true
}

// openStorageForCLI is openStorage for the four commands that open the
// account stores. On Postgres it also writes the sealed tokens row if
// there is none, as the first start does (#1202, ruling 20): a command
// may be the first thing v0.7 runs on the database, and once it has put
// a sealed accounts row there a v0.6.x build must not be able to start
// on it.
func openStorageForCLI(cfg config.Config) (*storage, error) {
	ctx := context.Background()
	st, err := openStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := st.sealTokensRow(ctx); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}
