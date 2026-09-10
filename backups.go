// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backupsftp"
	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/retention"
	"github.com/tomlawesome/mikroview/internal/routeros"
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
func openRouterBackupVault(log *slog.Logger, cfg config.Config) *backupvault.Vault {
	key, err := retention.LoadKey(cfg.History.KeyFile)
	switch {
	case err == retention.ErrNoKey:
		log.Info("router backups: no retention key configured (history.keyFile) -- the drop box refuses every login until one is mounted")
		key = nil
	case err != nil:
		log.Warn(fmt.Sprintf("router backups: the retention key could not be used -- the drop box refuses every login until it can be: %v", err))
		key = nil
	default:
		if key.GroupOrWorldReadable {
			log.Warn(fmt.Sprintf("%s is readable by more than its owner -- tighten its permissions", cfg.History.KeyFile))
		}
	}

	v, err := backupvault.Open(backupVaultDirectory(cfg), key)
	if err != nil {
		log.Error(fmt.Sprintf("router backups: %v -- the drop box will refuse every login", err))
		return nil
	}
	return v
}

// routerBackupPort is the drop box's port, for SetupInstance.BackupPort
// -- "" when backups are switched off, so the wizard's step 6 knows not
// to render a script pointing at a listener that is not running. Pure
// and cheap: it does not need the listener to actually be up, only the
// config to say what port it would be on.
func routerBackupPort(cfg config.Config) string {
	if !cfg.Backup.Enabled {
		return ""
	}
	return routeros.PortOf(cfg.Backup.Listen)
}

// startRouterBackupServer starts the SFTP drop box when backups are
// switched on. Tied to ctx exactly like syslog.ListenTLS just above it
// in run() -- no separate shutdown join, since ListenAndServe already
// returns once ctx is cancelled.
//
// The host key lives beside the TLS material (cfg.TLS.StorePath), not
// the data directory: both are generated-on-first-run secrets a restore
// should not carry, and TLS.StorePath is already excluded from -backup
// for exactly that reason (see excludedFromBackup in backup_cli.go) --
// putting the SFTP host key there means it inherits that exclusion for
// free rather than needing one of its own.
func startRouterBackupServer(ctx context.Context, cfg config.Config, vault *backupvault.Vault, tokens *auth.TokenStore) {
	log := logging.New("backupsftp")
	if !cfg.Backup.Enabled {
		log.Info("router backups: off (backup.enabled is false)")
		return
	}
	hostKey, err := backupsftp.LoadOrGenerateHostKey(cfg.TLS.StorePath)
	if err != nil {
		log.Error(fmt.Sprintf("router backups: could not prepare the SFTP host key, the drop box is not starting: %v", err))
		return
	}
	srv := backupsftp.New(vault, tokens, hostKey)
	go func() {
		if err := srv.ListenAndServe(ctx, cfg.Backup.Listen); err != nil && ctx.Err() == nil {
			log.Error(err.Error())
			os.Exit(1)
		}
	}()
}
