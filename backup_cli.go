// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// backedUpStores is every store the envelope carries, paired with where
// it lives on a JSON deployment.
//
// Everything, including the accounts file. Include-and-protect rather
// than exclude-by-default: a backup missing credentials cannot restore a
// working system, and the moment you find that out is a disaster
// recovery. The protection is on the output instead -- see writeBackup.
func backedUpStores(cfg config.Config) []struct{ Name, Path string } {
	return []struct{ Name, Path string }{
		{"auth", cfg.Auth.StorePath},
		{"tokens", cfg.Auth.TokensStorePath},
		{"recovery_keys", cfg.Auth.RecoveryKeysPath},
		{"flags", cfg.Flags.StorePath},
		{"rule_usage", cfg.Flags.RuleUsageStorePath},
		{"detector_settings", cfg.Flags.DetectorSettingsStorePath},
		{"entities", cfg.Entities.StorePath},
		{"mac_registry", cfg.DeviceMAC.StorePath},
		{"audit", cfg.Audit.StorePath},
	}
}

// refuseBackupOnPostgres is the deployment split the owner settled when
// the one-way migration was built: choosing Postgres is choosing its
// backup tooling.
//
// A second, partial mechanism is worse than none. It invites someone to
// restore an envelope over a live database and find the gap during a
// recovery, and a restore that could write JSON into Postgres is exactly
// the rollback path the one-way decision deliberately withholds.
func refuseBackupOnPostgres(cfg config.Config, cmd string) error {
	if cfg.Postgres.DSNFile == "" {
		return nil
	}
	return fmt.Errorf("%s is for JSON deployments. This one keeps its state in Postgres "+
		"(postgres.dsnFile is set), so back it up with your database's own tooling -- pg_dump or "+
		"your provider's snapshots. That is the expectation that came with choosing Postgres, and a "+
		"second half-mechanism covering only some of the state is worse than none", cmd)
}

func runBackup(args []string) int {
	logger := logging.New("backup")
	dest, ok := firstNonFlag(args)
	if !ok {
		logger.Error("usage: mikroview -backup <file> [--force]")
		return 2
	}

	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		logger.Error(fmt.Sprintf("config: %v", err))
		return 1
	}
	if err := refuseBackupOnPostgres(cfg, "-backup"); err != nil {
		logger.Error(err.Error())
		return 1
	}

	stores := map[string][]byte{}
	for _, s := range backedUpStores(cfg) {
		if s.Path == "" {
			continue
		}
		data, _, err := persist.LoadDocument(context.Background(), persist.NewFileBackend(s.Path))
		if err != nil {
			logger.Error(fmt.Sprintf("reading %s (%s): %v", s.Name, s.Path, err))
			return 1
		}
		if data == nil {
			continue // never written; nothing to carry
		}
		stores[s.Name] = data
	}
	if len(stores) == 0 {
		logger.Error("no store files exist yet -- nothing to back up")
		return 1
	}

	if err := writeBackup(dest, hasFlag(args, "--force"), stores); err != nil {
		logger.Error(err.Error())
		return 1
	}
	logger.Info(fmt.Sprintf("wrote %d store(s) to %s", len(stores), dest))
	fmt.Println("This file contains your accounts, API tokens and recovery-key digests.")
	fmt.Println("Treat it exactly as you would the data directory itself.")
	return 0
}

// writeBackup creates the file with O_EXCL and mode 0600.
//
// O_EXCL matters beyond refusing to clobber: opening an existing file
// ignores perm entirely, so a backup written over a world-readable
// placeholder would silently inherit it.
func writeBackup(dest string, force bool, stores map[string][]byte) error {
	if dir := filepath.Dir(dest); dir != "" && !force {
		if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm()&0o007 != 0 {
			return fmt.Errorf("refusing to write a backup into %s, which is world-readable -- "+
				"this file carries your accounts and recovery-key digests. Choose a private "+
				"directory, or pass --force if you are certain", dir)
		}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(dest, flags, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists -- refusing to overwrite it. "+
				"Choose another name, or pass --force", dest)
		}
		return err
	}
	defer f.Close()
	if err := os.Chmod(dest, 0o600); err != nil {
		return err
	}
	return backup.Write(f, version, stores)
}

func runRestore(args []string) int {
	logger := logging.New("restore")
	src, ok := firstNonFlag(args)
	if !ok {
		logger.Error("usage: mikroview -restore <file> [--force]")
		return 2
	}

	cfg, err := config.Load(os.Getenv("MIKROVIEW_CONFIG"), nil)
	if err != nil {
		logger.Error(fmt.Sprintf("config: %v", err))
		return 1
	}
	if err := refuseBackupOnPostgres(cfg, "-restore"); err != nil {
		logger.Error(err.Error())
		return 1
	}

	f, err := os.Open(src)
	if err != nil {
		logger.Error(err.Error())
		return 1
	}
	defer f.Close()

	// Parsed and fully validated before anything on disk is touched.
	env, err := backup.Read(f)
	if err != nil {
		logger.Error(fmt.Sprintf("%v -- nothing has been changed", err))
		return 1
	}

	known := map[string]string{}
	for _, s := range backedUpStores(cfg) {
		known[s.Name] = s.Path
	}
	for name := range env.Stores {
		if _, ok := known[name]; !ok {
			logger.Error(fmt.Sprintf("backup carries an unknown store %q -- refusing rather than "+
				"guessing where it belongs. Nothing has been changed", name))
			return 1
		}
		if known[name] == "" {
			logger.Error(fmt.Sprintf("backup carries store %q but this config has no path for it -- "+
				"nothing has been changed", name))
			return 1
		}
	}

	if !hasFlag(args, "--force") {
		for name := range env.Stores {
			if _, err := os.Stat(known[name]); err == nil {
				logger.Error(fmt.Sprintf("%s already exists (store %q) -- refusing to overwrite live "+
					"state. Re-run with --force once you are sure", known[name], name))
				return 1
			}
		}
	}

	// Write every store to a temp file first, then rename. A rename is
	// atomic, so a failure part-way leaves each original file whole --
	// which matters most for the accounts store, where the alternative to
	// "unchanged" is "locked out".
	for name, raw := range env.Stores {
		path := known[name]
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			logger.Error(err.Error())
			return 1
		}
		tmp := path + ".restore-tmp"
		if err := os.WriteFile(tmp, raw, 0o600); err != nil {
			logger.Error(err.Error())
			return 1
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			logger.Error(err.Error())
			return 1
		}
	}
	logger.Info(fmt.Sprintf("restored %d store(s) from %s (created %s by %s)",
		len(env.Stores), src, env.Created.Format("2006-01-02 15:04 MST"), env.AppVersion))
	return 0
}

// firstNonFlag picks the path argument out of args, so `-backup x --force`
// and `-backup --force x` both work.
func firstNonFlag(args []string) (string, bool) {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a, true
		}
	}
	return "", false
}
