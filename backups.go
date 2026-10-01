// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupsftp"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/settings"
)

// This file is main's half of #394: internal/backupvault knows how to
// keep and encrypt what arrives, internal/backupsftp knows how to run
// the drop box; what is left is deciding where the vault lives, which
// key it opens under, and when the listener actually starts.

// backupVaultDirectory is where router-backup generations live on disk.
// The configured value wins; empty means beside the data directory --
// the same contract snapshotDirectory and historyDirectory already use,
// so a moved data directory does not strand this one on the default
// volume the way #795 found snapshot.dir doing before that was fixed.
func backupVaultDirectory(cfg config.Config) string {
	if dir := strings.TrimSpace(cfg.Backup.VaultDir); dir != "" {
		return dir
	}
	return filepath.Join(dataDir(cfg), "router-backups")
}

// recoveryCandidates is every device name a lost vault index's rebuild
// can try a router's directory hash against to recover it by name
// rather than by placeholder (#1294): the SFTP login name a push must
// carry equals its ingest token's Device field (Server.authenticate,
// above the fold in internal/backupsftp/server.go), and that field
// survives a vault index loss untouched -- it lives in tokens, a wholly
// separate store. The device registry's own ids are folded in too, a
// useful superset when a token has since been revoked but the registry
// row has not. Order does not matter: rebuildFromDisk tries every
// candidate against every directory.
func recoveryCandidates(tokens *auth.TokenStore, devices *device.Registry) []string {
	var names []string
	if tokens != nil {
		for _, tok := range tokens.ByKind(auth.TokenKindIngest) {
			if tok.Device != "" {
				names = append(names, tok.Device)
			}
		}
	}
	if devices != nil {
		for _, info := range devices.List() {
			names = append(names, info.ID)
		}
	}
	return names
}

// openRouterBackupVault loads the retention key (the same one
// history.keyFile names -- #394's vault and #853's state store share
// one key, not two) and opens the vault under it.
//
// A missing key is not a fault: it is #394's own "no key, no backups"
// rule, the vault's first-class disabled state, exactly as memory-only
// is openHistory's first-class disabled state for the same reason. A
// key that is configured but unusable (unreadable, too short) is a
// fault, because the operator asked for encrypted backups and is not
// getting them -- logged loudly rather than silently falling back to an
// unencrypted vault, which does not exist as an option here.
//
// keyUnreadable, the second return, is #1264 finding 5: both cases above
// leave the vault disabled the same way (key = nil), which used to be
// the end of the story -- RouterBackupsResponse.enabled and the
// wizard's step-6 message could not tell "nothing configured" from "was
// configured, could not be read" apart, so an operator whose key merely
// failed to load was told the same thing as one who had never set one:
// mint a new key. Minting overwrites history.keyFile, and every backup
// already encrypted under the old one -- unreadable, not missing --
// becomes unreadable for good. keyUnreadable is true only for the
// configured-but-broken case; the caller (main.go) carries it into
// api.SetupInstance so every hop that currently reads Vault.Enabled()
// to decide what to tell the operator can also ask this.
func openRouterBackupVault(log *slog.Logger, cfg config.Config, tokens *auth.TokenStore, devices *device.Registry) (v *backupvault.Vault, keyUnreadable bool) {
	key, err := retention.LoadKey(cfg.History.KeyFile)
	switch {
	case err == retention.ErrNoKey:
		log.Info("router backups: no retention key configured (history.keyFile) -- the drop box refuses every login until one is mounted")
		key = nil
	case err != nil:
		log.Warn(fmt.Sprintf("router backups: the retention key could not be used -- the drop box refuses every login until it can be: %v", err))
		key = nil
		keyUnreadable = true
	default:
		if key.GroupOrWorldReadable {
			log.Warn(fmt.Sprintf("%s is readable by more than its owner -- tighten its permissions", cfg.History.KeyFile))
		}
	}

	vault, err := backupvault.Open(backupVaultDirectory(cfg), key, recoveryCandidates(tokens, devices))
	if err != nil {
		log.Error(fmt.Sprintf("router backups: %v -- the drop box will refuse every login", err))
		return nil, keyUnreadable
	}
	return vault, keyUnreadable
}

// backupRuntime owns the router-backup SFTP drop box's lifecycle: whether
// it is listening, on which port, and what happens the moment an admin
// moves the switch (#1361, "Settings -> router backups").
//
// Simpler than historyRuntime beside it in history_runtime.go: there is
// no ring to backfill and no seam to protect, only a listener to open or
// close. Start's net.Listen runs synchronously and its error is returned
// straight to the caller, so the one failure mode worth telling an admin
// about -- the configured port is already taken by something else --
// reaches handleRouterBackupSwitchUpdate as a plain error it turns into a
// 409, never merely a log line from a goroutine nobody asked to watch.
//
// The host key lives beside the TLS material (cfg.TLS.StorePath), not the
// data directory: both are generated-on-first-run secrets a restore
// should not carry, and TLS.StorePath is already excluded from -backup
// for exactly that reason (see excludedFromBackup in backup_cli.go) --
// putting the SFTP host key there means it inherits that exclusion for
// free rather than needing one of its own.
type backupRuntime struct {
	log         *slog.Logger
	ctx         context.Context
	vault       *backupvault.Vault
	tokens      *auth.TokenStore
	set         *settings.Store
	hostKeyPath string
	addr        string

	mu     sync.Mutex
	ln     net.Listener
	cancel context.CancelFunc
}

// newBackupRuntime brings the drop box up as this instance's stored
// switch position leaves it. ctx is main's own shutdown context: a
// listener this opens is tied to it exactly like syslog.ListenTLS is, so
// it closes on an ordinary shutdown with no separate join needed, on top
// of whatever an admin's own Stop does later.
//
// backup.enabled and MIKROVIEW_BACKUP_ENABLED are retired (#1361): the
// switch lives in the settings store now, beside history.enabled. Read
// here only once, to seed that store's starting position the first time
// an instance runs with nothing stored yet -- logged so an operator who
// never opens Settings still sees why the drop box came up open. Never
// consulted again after that: a leftover value in an old config.yaml is
// harmless, and a fresh install has none, so it starts closed.
func newBackupRuntime(ctx context.Context, log *slog.Logger, cfg config.Config, set *settings.Store, vault *backupvault.Vault, tokens *auth.TokenStore) *backupRuntime {
	r := &backupRuntime{
		log:         log,
		ctx:         ctx,
		vault:       vault,
		tokens:      tokens,
		set:         set,
		hostKeyPath: cfg.TLS.StorePath,
		addr:        cfg.Backup.Listen,
	}

	pos, ok := set.Backup()
	if !ok {
		if cfg.Backup.Enabled {
			log.Info("router backups: no stored switch position -- seeding it open from the retired backup.enabled/MIKROVIEW_BACKUP_ENABLED config value; that key is not read again after this")
		}
		pos = settings.Backup{Enabled: cfg.Backup.Enabled}
		if err := set.SetBackup(pos); err != nil {
			log.Warn(fmt.Sprintf("router backups: could not store the seeded switch position -- it will be seeded again at the next restart: %v", err))
		}
	}

	if pos.Enabled {
		if err := r.Start(); err != nil {
			log.Error(fmt.Sprintf("router backups: could not open the drop box on %s at startup: %v", r.addr, err))
		}
	} else {
		log.Info("router backups: closed")
	}
	return r
}

// Start opens the drop box, binding the port synchronously so a caller
// gets "already in use" back as an error from this call rather than a log
// line nobody is watching. Idempotent: calling it while already open is a
// no-op.
func (r *backupRuntime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln != nil {
		return nil
	}
	hostKey, err := backupsftp.LoadOrGenerateHostKey(r.hostKeyPath)
	if err != nil {
		return fmt.Errorf("preparing the SFTP host key: %w", err)
	}
	ln, err := net.Listen("tcp", r.addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(r.ctx)
	srv := backupsftp.New(r.vault, r.tokens, hostKey)
	r.ln = ln
	r.cancel = cancel
	go func() {
		if err := srv.Serve(ctx, ln); err != nil && ctx.Err() == nil {
			r.log.Error(err.Error())
		}
	}()
	return nil
}

// Stop closes the drop box. Closing only stops new connections -- an
// upload already under way keeps writing on its own already-accepted
// connection until it finishes or drops (internal/backupsftp's
// pendingWrite: an interrupted transfer commits nothing, exactly as if
// the router's own end had dropped it); nothing here waits for that or
// cuts it short. Idempotent: calling it while already closed is a no-op.
func (r *backupRuntime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln == nil {
		return
	}
	r.cancel()
	r.ln.Close()
	r.ln = nil
	r.cancel = nil
}

// State reports whether the drop box is listening right now, and on
// which port -- read from the listener itself, never from the stored
// setting, so a startup bind failure (the stored position says open, but
// the port turned out to be taken by something else) is reported as
// closed rather than repeating a claim that is not true. Port is "" when
// closed.
func (r *backupRuntime) State() (open bool, port string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln == nil {
		return false, ""
	}
	return true, routeros.PortOf(r.addr)
}
