// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
	"github.com/tomlawesome/gauntlet/gate"

	"github.com/tomlawesome/mikroview/internal/api"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// Tests for gauntlet_wiring.go: how mikroview opens gauntlet's stores and
// what start-up choices it makes for the sign-in gate (#1202).

func keyedConfig(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := cfgWithData(dir)
	cfg.Auth.TokensStorePath = filepath.Join(dir, "tokens.json")
	cfg.Auth.SignInsStorePath = filepath.Join(dir, "signins.json")
	cfg.Auth.RecoveryKeysPath = filepath.Join(dir, "recovery.json")
	cfg.History.KeyFile = writeKeyFile(t)
	return cfg
}

// The setup code line is what scripts/setup-code.sh and the live-check
// harness grep for, so the text is part of the contract.
func TestSetupCodeLineCarriesTheCodeInTheWordsScriptsMatch(t *testing.T) {
	got := setupCodeLine("abcd-efgh-ijkl-mnop")
	for _, want := range []string{"no account exists yet", "create the first admin with setup code abcd-efgh-ijkl-mnop"} {
		if !strings.Contains(got, want) {
			t.Errorf("setup code line %q lacks %q", got, want)
		}
	}
}

// The running server announces both codes on its log, and never sends a
// password anywhere (no breach check).
func TestServerStoreOptionsAnnounceCodesOnTheLogAndSkipTheBreachCheck(t *testing.T) {
	log, buf := logCapture()
	opts := serverStoreOptions(log, nil)

	opts.OnSetupCode.SetupCode("1111-2222-3333-4444")
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "1111-2222-3333-4444") {
		t.Errorf("setup code not announced as a Warn: %q", buf)
	}

	buf.Reset()
	opts.OnUnlockCode.UnlockCode("alice\x1b[31m", "5555-6666-7777-8888")
	out := buf.String()
	for _, want := range []string{"alice", "5555-6666-7777-8888", "unlock code"} {
		if !strings.Contains(out, want) {
			t.Errorf("unlock announcement lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("a control character in the username reached the log raw: %q", out)
	}
	if opts.BreachCheck != nil {
		t.Error("a breach check is set; passwords would leave the process")
	}
	if opts.ProductName != api.ProductName {
		t.Errorf("ProductName = %q", opts.ProductName)
	}
}

// The command-line tools and setup-only mode announce no code: the running
// server would not recognise one another process minted.
func TestQuietStoreOptionsSayNothing(t *testing.T) {
	opts := quietStoreOptions()
	opts.OnSetupCode.SetupCode("x") // must not panic, must be callable
	opts.OnUnlockCode.UnlockCode("u", "x")
	if opts.Log != nil || opts.BreachCheck != nil {
		t.Errorf("quiet options carry a logger or breach check: %+v", opts)
	}
}

// Without a refreshed copy the tools still get a list (the one built into
// the release); the directory they read is beside the accounts store.
func TestStoredPasswordListIsReadFromBesideTheAccountsStore(t *testing.T) {
	cfg := keyedConfig(t)
	if got, want := passwordListDir(cfg), filepath.Join(filepath.Dir(cfg.Auth.StorePath), "password-list"); got != want {
		t.Errorf("passwordListDir = %q, want %q", got, want)
	}
	if storedPasswordList(cfg) == nil {
		t.Error("no password list for the command-line tools")
	}
	if quietStoreOptionsFor(cfg).PasswordBlocklist == nil {
		t.Error("quietStoreOptionsFor left the password check on its default instead of the stored list")
	}
	// Reading must not create the directory: the server owns it.
	if _, err := os.Stat(passwordListDir(cfg)); !os.IsNotExist(err) {
		t.Errorf("reading the stored list created %s (stat err %v)", passwordListDir(cfg), err)
	}
}

func TestNewPasswordListMakesItsDirectoryOrRefusesToStart(t *testing.T) {
	cfg := keyedConfig(t)
	r, err := newPasswordList(cfg)
	if err != nil || r == nil {
		t.Fatalf("newPasswordList = %v, %v", r, err)
	}
	info, err := os.Stat(passwordListDir(cfg))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("password-list dir = %v, %v; want a directory only the owner can enter", info, err)
	}

	// A data directory that is really a file: the directory cannot be
	// made, which stops start-up (review R5) instead of failing every
	// refresh quietly.
	blocker := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newPasswordList(cfgWithData(blocker)); err == nil {
		t.Error("newPasswordList succeeded although its directory cannot be made")
	}
}

func TestSessionLimitsAreCutDownToGauntletsCeilings(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		idle, ceiling, wantIdle, want time.Duration
	}{
		{"within limits are kept", 10 * time.Minute, 2 * time.Hour, 10 * time.Minute, 2 * time.Hour},
		{"the old 24h defaults are cut", 24 * time.Hour, 24 * 7 * time.Hour, gauntlet.MaxSessionIdle, gauntlet.MaxSessionLifetime},
		{"zero idle never idles out, so is cut to the hour", 0, time.Hour, gauntlet.MaxSessionIdle, time.Hour},
		{"negative idle is cut", -time.Minute, time.Hour, gauntlet.MaxSessionIdle, time.Hour},
		{"zero ceiling meant none, so is cut to the day", time.Minute, 0, time.Minute, gauntlet.MaxSessionLifetime},
	} {
		var a config.Auth
		a.SessionTTL, a.SessionMaxLifetime = tc.idle, tc.ceiling
		idle, ceiling := sessionLimits(a)
		if idle != tc.wantIdle || ceiling != tc.want {
			t.Errorf("%s: sessionLimits = %v, %v; want %v, %v", tc.name, idle, ceiling, tc.wantIdle, tc.want)
		}
	}
}

// Ruling 1: admins must hold a passkey over https on a domain name; the
// config setting wins either way; a plain-http or IP address is optional.
func TestAdminPasskeyRule(t *testing.T) {
	mk := func(publicURL, override string) config.Config {
		var c config.Config
		c.PublicURL = publicURL
		c.Auth.AdminPasskey = override
		return c
	}
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want gate.AdminPasskeyRule
	}{
		{"https domain, unset", mk("https://mikroview.example.com", ""), gate.AdminPasskeyRequired},
		{"http localhost, unset: a developer's address", mk("http://localhost:8080", ""), gate.AdminPasskeyOptional},
		{"https IP address, unset: no passkey can be made", mk("https://192.0.2.10", ""), gate.AdminPasskeyOptional},
		{"no public URL, unset", mk("", ""), gate.AdminPasskeyOptional},
		{"https domain, explicitly optional", mk("https://mikroview.example.com", config.AdminPasskeyOptional), gate.AdminPasskeyOptional},
		{"http localhost, explicitly required", mk("http://localhost:8080", config.AdminPasskeyRequired), gate.AdminPasskeyRequired},
	} {
		rp, err := newRelyingParty(tc.cfg)
		if err != nil {
			t.Fatalf("%s: newRelyingParty: %v", tc.name, err)
		}
		if got := adminPasskeyRule(tc.cfg, rp); got != tc.want {
			t.Errorf("%s: adminPasskeyRule = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// passkeysFor hands gate nothing when passkeys are off, so every passkey
// route answers 404 rather than refusing each ceremony.
func TestPasskeysForIsNilWhenPasskeysAreOff(t *testing.T) {
	off, err := newRelyingParty(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := passkeysFor(off); got != nil {
		t.Errorf("passkeysFor(unset public URL) = %v, want nil", got)
	}
	var c config.Config
	c.PublicURL = "https://mikroview.example.com"
	on, err := newRelyingParty(c)
	if err != nil {
		t.Fatal(err)
	}
	if passkeysFor(on) == nil {
		t.Error("passkeysFor(https domain) = nil, passkeys would be off although they can work")
	}
}

// Every kind mikroview issues must survive being opened: a drop-list key a
// router holds must authenticate again after a restart (api.TokenKinds).
func TestOpenTokensKeepsEveryKindAcrossARestart(t *testing.T) {
	ctx := context.Background()
	cfg := keyedConfig(t)
	log, _ := logCapture()
	st, err := openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	toks, err := openTokens(ctx, st, cfg, log)
	if err != nil {
		t.Fatalf("openTokens: %v", err)
	}
	raw, _, err := toks.Create("router", api.TokenKindDroplistPull, "", nil, time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	again, err := openTokens(ctx, st, cfg, log)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, ok := again.Authenticate(raw, api.TokenKindDroplistPull, time.Now()); !ok {
		t.Error("a drop-list token no longer authenticates after reopening the store")
	}
	if onDisk, _ := os.ReadFile(cfg.Auth.TokensStorePath); strings.Contains(string(onDisk), "router") {
		t.Error("the tokens file holds token names in the clear; it must be sealed")
	}
}

func TestOpenTokensAndSignInsNeedAKey(t *testing.T) {
	ctx := context.Background()
	cfg := cfgWithData(t.TempDir())
	cfg.Auth.TokensStorePath = filepath.Join(t.TempDir(), "t.json")
	cfg.Auth.SignInsStorePath = filepath.Join(t.TempDir(), "s.json")
	st := &storage{cfg: cfg}
	log, _ := logCapture()
	if _, err := openTokens(ctx, st, cfg, log); err == nil {
		t.Error("openTokens opened without a key")
	}
	if _, err := openSignIns(ctx, st, cfg, log); err == nil {
		t.Error("openSignIns opened without a key")
	}
}

func TestOpenSignInsKeepsHistorySealedAcrossARestart(t *testing.T) {
	ctx := context.Background()
	cfg := keyedConfig(t)
	log, _ := logCapture()
	st, err := openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, err := openSignIns(ctx, st, cfg, log)
	if err != nil {
		t.Fatalf("openSignIns: %v", err)
	}
	h.Record(gauntlet.SignInEvent{Username: "mallory-the-visible", Outcome: gauntlet.SignInSuccess, Method: gauntlet.SignInMethodPassword}, time.Now())
	if err := h.Close(); err != nil { // saves its last rows
		t.Fatalf("Close: %v", err)
	}

	again, err := openSignIns(ctx, st, cfg, log)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	if total, _ := again.Summary(); total != 1 {
		t.Errorf("sign-ins after restart = %d, want the 1 recorded", total)
	}
	if onDisk, _ := os.ReadFile(cfg.Auth.SignInsStorePath); len(onDisk) == 0 || strings.Contains(string(onDisk), "mallory-the-visible") {
		t.Errorf("sign-in history missing or in the clear on disk (%d bytes)", len(onDisk))
	}
}

// The recovery-key document is opened sealed at start-up so a plaintext
// one left by a keyless release refuses to start (ruling 19), while an
// absent one or a path of "" is nothing to check.
func TestOpenRecoveryKeysSealedRefusesPlaintextAndNonJSON(t *testing.T) {
	ctx := context.Background()
	cfg := keyedConfig(t)
	st, err := openStorage(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := openRecoveryKeysSealed(ctx, st, cfg); err != nil {
		t.Errorf("an absent recovery-key store should open as empty: %v", err)
	}

	empty := cfg
	empty.Auth.RecoveryKeysPath = ""
	if err := openRecoveryKeysSealed(ctx, st, empty); err != nil {
		t.Errorf("persistence switched off should not be an error: %v", err)
	}

	// A sealed JSON document opens.
	b := persist.NewEncryptedFileBackend(cfg.Auth.RecoveryKeysPath, authStoreRecoveryKeys, st.key)
	if _, err := b.Save(ctx, []byte(`{"keys":[]}`), 0); err != nil {
		t.Fatal(err)
	}
	if err := openRecoveryKeysSealed(ctx, st, cfg); err != nil {
		t.Errorf("a sealed JSON document was refused: %v", err)
	}

	// A sealed document that is not JSON is refused by name.
	notJSON := cfg
	notJSON.Auth.RecoveryKeysPath = filepath.Join(t.TempDir(), "r.json")
	if _, err := persist.NewEncryptedFileBackend(notJSON.Auth.RecoveryKeysPath, authStoreRecoveryKeys, st.key).Save(ctx, []byte("not json at all"), 0); err != nil {
		t.Fatal(err)
	}
	if err := openRecoveryKeysSealed(ctx, st, notJSON); err == nil || !strings.Contains(err.Error(), "recovery-key store") {
		t.Errorf("non-JSON recovery store = %v, want a refusal naming the recovery-key store", err)
	}

	// A plaintext file an earlier keyless release left is refused.
	plain := cfg
	plain.Auth.RecoveryKeysPath = filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(plain.Auth.RecoveryKeysPath, []byte(`{"keys":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err = openRecoveryKeysSealed(ctx, st, plain)
	if err == nil {
		t.Fatal("a plaintext recovery-key file was accepted")
	}
	var se *persist.StartupError
	if !errors.As(err, &se) {
		t.Errorf("plaintext recovery file: %T %v, want a *persist.StartupError", err, err)
	}
	if onDisk, _ := os.ReadFile(plain.Auth.RecoveryKeysPath); string(onDisk) != `{"keys":[]}` {
		t.Errorf("the refused file was altered: %q", onDisk)
	}
}

// A sweep goroutine stops when its context ends, without sweeping.
func TestRunTokenSweepStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	log, _ := logCapture()
	done := make(chan struct{})
	go func() {
		runTokenSweep(ctx, nil, log) // a nil gate would panic if it ever swept
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runTokenSweep did not return after its context ended")
	}
}

// The command-line tools refuse on a missing key before opening or
// prompting for anything, and on data a newer build wrote.
func TestCLIRequireKey(t *testing.T) {
	writeConfig := func(t *testing.T, cfg config.Config) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		body := "auth:\n  storePath: " + cfg.Auth.StorePath + "\nhistory:\n  keyFile: " + cfg.History.KeyFile + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MIKROVIEW_CONFIG", path)
	}
	log, buf := logCapture()

	cfg := keyedConfig(t)
	writeConfig(t, cfg)
	if !cliRequireKey(log, "-recover-admin-account") {
		t.Fatalf("refused with a key and a fresh data directory: %s", buf)
	}

	// No key: the same refusal as start-up, naming the command.
	noKey := cfg
	noKey.History.KeyFile = ""
	writeConfig(t, noKey)
	log, buf = logCapture()
	if cliRequireKey(log, "-recover-admin-account") {
		t.Fatal("ran with no key")
	}
	if !strings.Contains(buf.String(), "run -recover-admin-account") || !strings.Contains(buf.String(), "CFG-0083") {
		t.Errorf("refusal %q does not name the command and CFG-0083", buf)
	}

	// Data from a newer build: refused, and nothing rewritten.
	writeConfig(t, cfg)
	doc := `{"schema":9999,"version":"v99.0.0"}`
	schemaPath := filepath.Join(filepath.Dir(cfg.Auth.StorePath), persist.SchemaDocumentName)
	if err := os.WriteFile(schemaPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	log, _ = logCapture()
	if cliRequireKey(log, "-transfer-admin") {
		t.Error("ran against a data directory a newer build wrote")
	}
	if got, _ := os.ReadFile(schemaPath); string(got) != doc {
		t.Errorf("the schema document was rewritten: %q", got)
	}
}
