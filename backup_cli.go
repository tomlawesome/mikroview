// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// backupBackendFor is the backend runBackup reads a store through and
// runRestore writes one back through -- deliberately not
// storage.backendFor's own policy, which also decides whether the *live
// server* persists a store at all with no key configured (#853). A
// backup's job is fidelity with whatever is actually on this disk: with a
// key, every store round-trips through the same encryption the server
// would apply to it; with none, every store is read and written as plain
// JSON, exactly as every mikroview release before #853 did -- including a
// file that predates the key, or one from a deployment that has since
// removed it.
func backupBackendFor(path string, key *retention.Key) persist.Backend {
	if key == nil {
		return persist.NewFileBackend(path)
	}
	return persist.NewEncryptedFileBackend(path, key)
}

// backedUpStores is every store the envelope carries, paired with where
// it lives on a JSON deployment.
//
// Everything except what excludedFromBackup documents, including the
// accounts file. Include-and-protect rather than exclude-by-default: a
// backup missing credentials cannot restore a working system, and the
// moment you find that out is a disaster recovery. The protection is on
// the output instead -- see writeBackup.
//
// This list drifted three fields behind config.Config once already
// (#372: Watchlist.StorePath, Watchlist.SuggestionsStorePath and
// Watchlist.MatchLogPath were all silently missing, so `-backup` dropped
// every operator's watchlist configuration with no error and no
// warning). TestBackupCoversAllConfigPathFields
// (backup_coverage_test.go) is the guard against that happening again:
// it fails the build if a new *Path field appears on config.Config
// without landing here or on excludedFromBackup.
func backedUpStores(cfg config.Config) []struct{ Name, Path string } {
	return []struct{ Name, Path string }{
		{"auth", cfg.Auth.StorePath},
		{"tokens", cfg.Auth.TokensStorePath},
		{"recovery_keys", cfg.Auth.RecoveryKeysPath},
		{"flags", cfg.Flags.StorePath},
		{"rule_usage", cfg.Flags.RuleUsageStorePath},
		{"entities", cfg.Entities.StorePath},
		{"coverage", cfg.Coverage.StorePath},
		{"mac_registry", cfg.DeviceMAC.StorePath},
		{"engine_state", cfg.Engine.StorePath},
		{"definitions", cfg.Engine.DefinitionsStorePath},
		{"audit", cfg.Audit.StorePath},
		{"setup", cfg.Setup.StorePath},
		{"settings", cfg.Store.SettingsStorePath},
		{"suggestions", cfg.Watchlist.SuggestionsStorePath},
		{"match_log", cfg.Watchlist.MatchLogPath},
	}
}

// excludedFromBackup is every *Path field on config.Config that
// backedUpStores deliberately does not carry, keyed by its dotted field
// path (Struct.Field, or Struct.Nested.Field), with the reason it stays
// out. TestBackupCoversAllConfigPathFields walks config.Config by
// reflection and requires every *Path field to appear either in
// backedUpStores or here -- so adding a new *Path field without deciding
// its backup coverage fails the build instead of silently drifting the
// way Watchlist's three fields did (#372).
//
// Two things #856 keeps out of a backup are absent from this map rather
// than listed in it, because the guard above walks *Path fields only and
// rejects an entry naming anything else. Recorded here so the omission
// reads as a decision:
//
//   - history.keyFile -- the key the retained event history is encrypted
//     with. Same reasoning as Auth.RecoveryPepperPath below, and the
//     reason the encryption is worth anything: a stolen backup should
//     carry nothing able to open what it holds.
//   - history.dir -- the retained files themselves.
//     docs/decisions/event-retention.md settled that they stay out of
//     any backup mikroview produces (#394) unless a later decision says
//     otherwise.
var excludedFromBackup = map[string]string{
	"Auth.RecoveryPepperPath": "the server-side secret mixed into every recovery-key digest (#97) " +
		"-- see that field's doc comment in internal/config/config.go. A stolen backup should carry " +
		"the digests and nothing able to verify them against.",
	"TLS.StorePath": "a directory of generated CA + certificate key material (#372), not a single " +
		"JSON document like every other entry in backedUpStores -- restoring it blind onto a new " +
		"host is more likely to be wrong than right (different hostname/IP SANs, a CA nothing has " +
		"trusted yet), and regenerating it is one restart away, so there is nothing here a restore " +
		"is actually saving.",
	"GeoIP.DBPath": "an external MaxMind database file the operator downloads themselves (#372), not " +
		"a store mikroview writes -- there is nothing here for a restore to reproduce that a fresh " +
		"download would not already give back.",
}

// jsonLinesStore is the one backedUpStores entry whose on-disk shape is
// not a single JSON document: internal/matchlog's append-only
// newline-delimited JSON file (#372). internal/backup.Envelope holds
// each store as json.RawMessage, which requires exactly one JSON value --
// fine for every other store here, which is a single JSON object, but a
// match log with more than one line is many JSON values concatenated,
// and embedding that directly makes backup.Write fail outright the
// moment a second match is ever recorded (confirmed: "invalid character
// '{' after top-level value"). Wrapped as a base64 JSON string instead
// (see wrapForEnvelope/unwrapFromEnvelope below), which keeps the
// envelope's per-store value a single valid JSON value either way and
// needs no format-version bump -- the envelope's own doc comment already
// promises it never has to understand a store's shape.
const jsonLinesStore = "match_log"

// wrapForEnvelope prepares one store's raw bytes for backup.Write. Every
// store except jsonLinesStore is already a single JSON document and is
// carried as-is; jsonLinesStore is base64-wrapped into a JSON string
// (encoding/json's built-in []byte handling) since it is not.
func wrapForEnvelope(name string, data []byte) ([]byte, error) {
	if name != jsonLinesStore {
		return data, nil
	}
	wrapped, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encoding %s for the backup: %w", name, err)
	}
	return wrapped, nil
}

// unwrapFromEnvelope reverses wrapForEnvelope on restore.
func unwrapFromEnvelope(name string, raw []byte) ([]byte, error) {
	if name != jsonLinesStore {
		return raw, nil
	}
	var decoded []byte
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decoding %s from the backup: %w", name, err)
	}
	return decoded, nil
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

	// #853: decrypt under history.keyFile when one is configured, so the
	// envelope this produces stays plain, readable JSON either way -- the
	// backup runs with the key available, same as the server. This is
	// deliberately its own key load rather than persistence.backendFor's
	// (storage.go's) policy: that policy also decides whether the *live
	// server* persists a store at all with no key configured, which is a
	// different question from "read whatever is actually on this disk",
	// the one -backup answers. A file predating #853, or written by a
	// deployment that has since removed its key, is still backed up as
	// the plain JSON it actually is.
	ctx := context.Background()
	ourKey, keyErr := retention.LoadKey(cfg.History.KeyFile)
	if keyErr != nil && keyErr != retention.ErrNoKey {
		logger.Error(fmt.Sprintf("history.keyFile is set but could not be used: %v -- refusing rather than guessing whether stores are encrypted", keyErr))
		return 1
	}

	stores := map[string][]byte{}
	for _, s := range backedUpStores(cfg) {
		if s.Path == "" {
			continue
		}
		data, _, err := persist.LoadDocument(ctx, backupBackendFor(s.Path, ourKey))
		if err != nil {
			logger.Error(fmt.Sprintf("reading %s (%s): %v", s.Name, s.Path, err))
			return 1
		}
		if data == nil {
			continue // never written; nothing to carry
		}
		wrapped, err := wrapForEnvelope(s.Name, data)
		if err != nil {
			logger.Error(err.Error())
			return 1
		}
		stores[s.Name] = wrapped
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
		// #nosec G703 -- operator-supplied CLI path; see the note on firstNonFlag.
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
	// #nosec G703 -- operator-supplied CLI path; see the note on firstNonFlag.
	f, err := os.OpenFile(dest, flags, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists -- refusing to overwrite it. "+
				"Choose another name, or pass --force", dest)
		}
		return err
	}
	defer f.Close()
	// #nosec G703 -- operator-supplied CLI path; see the note on firstNonFlag.
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

	// #853: the same key runBackup would have decrypted under, so a store
	// gets written back encrypted exactly when the deployment restoring
	// it has a key configured -- matching what the running server would
	// then expect to find there.
	ctx := context.Background()
	ourKey, keyErr := retention.LoadKey(cfg.History.KeyFile)
	if keyErr != nil && keyErr != retention.ErrNoKey {
		logger.Error(fmt.Sprintf("history.keyFile is set but could not be used: %v -- nothing has been changed", keyErr))
		return 1
	}

	// #nosec G703 -- operator-supplied CLI path; see the note on firstNonFlag.
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
	// decoded holds the bytes each store will actually be written with --
	// unwrapped up front, in the same fully-validated-before-anything-is-
	// touched pass as the known-store checks below, so a corrupt
	// jsonLinesStore entry is refused before any file is touched rather
	// than after some other store has already been restored.
	decoded := map[string][]byte{}
	for name, raw := range env.Stores {
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
		data, err := unwrapFromEnvelope(name, raw)
		if err != nil {
			logger.Error(fmt.Sprintf("%v -- nothing has been changed", err))
			return 1
		}
		decoded[name] = data
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

	// Write every store through its backend -- persist.Backend.Save
	// already does the atomic temp-file/fsync/rename dance, encrypting on
	// the way when the store is one #853 covers. This never goes through
	// OpenWithBackend/persist.Open (issue #378's read-and-decode path):
	// Save only ever writes the bytes it is given, so a document that
	// would make a normal boot refuse to start (an unreadable accounts
	// file, say) is still one --restore can overwrite.
	for name, data := range decoded {
		path := known[name]
		backend := backupBackendFor(path, ourKey)
		// The version to overwrite with, read without needing the
		// existing document to actually decrypt -- see
		// persist.EncryptedFileBackend.Version's doc comment. Backends
		// that do not implement VersionReader (plain FileBackend) fall
		// back to Load, which never fails on an undecryptable document
		// either, since it never attempts to decrypt anything.
		var expect int64
		if vr, ok := backend.(persist.VersionReader); ok {
			expect, _, err = vr.Version(ctx)
		} else {
			var snap persist.Snapshot
			snap, err = backend.Load(ctx)
			expect = snap.Version
		}
		if err != nil {
			logger.Error(fmt.Sprintf("reading the current version of %s (store %q): %v", path, name, err))
			return 1
		}
		if _, err := backend.Save(ctx, data, expect); err != nil {
			logger.Error(fmt.Sprintf("writing %s (store %q): %v", path, name, err))
			return 1
		}
	}
	logger.Info(fmt.Sprintf("restored %d store(s) from %s (created %s by %s)",
		len(env.Stores), src, env.Created.Format("2006-01-02 15:04 MST"), env.AppVersion))
	return 0
}

// firstNonFlag picks the path argument out of args, so `-backup x --force`
// and `-backup --force x` both work.
//
// This is the single point every path-taking CLI command reads its
// argument through -- -backup, -restore and -migrate-data -- and it is
// what the "operator-supplied path" note on the #nosec G703 annotations
// in this file and migrate_cli.go refers to. gosec's taint analysis
// follows the value from here to each os.* call and reports a path
// traversal at every one.
//
// It is not one. Path traversal is crossing a boundary you were meant to
// stay inside; there is no boundary here. The value is a path the
// operator typed into their own shell, and the process runs as them, so
// every path they can reach this way is one they could already reach
// with cp. Cleaning the path was tried and changes nothing -- gosec does
// not treat filepath.Clean as a sanitiser -- so it would have been
// obfuscation rather than a fix.
//
// The rule stays enabled because the shape it is looking for would be
// real if a path ever arrived from an HTTP request instead.
func firstNonFlag(args []string) (string, bool) {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a, true
		}
	}
	return "", false
}
