// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// historyDirectory is where this deployment's retained event files live.
//
// The configured value wins; empty means beside mikroview's other state,
// resolved through dataDir exactly as snapshotDirectory does, so an
// operator who moved the data directory does not end up with a history
// stranded on the default volume. #795 learned that one the hard way --
// snapshot.dir defaulted to a literal path, so a moved data directory
// wrote nothing and everything still reported success.
func historyDirectory(cfg config.Config) string {
	if dir := strings.TrimSpace(cfg.History.Dir); dir != "" {
		return dir
	}
	return filepath.Join(dataDir(cfg), "history")
}

// openHistory prepares on-disk event retention, or reports why it is
// off.
//
// The three settings it reads -- Enabled, Days, MaxBytes -- are the
// *effective* ones, not necessarily config.yaml's: since #910 an admin
// can change them from inside the app and the stored figures win. The
// caller resolves that and hands the result in (newHistoryRuntime,
// which is the only caller); keyFile and dir stay config-only.
//
// Off is the default and a first-class outcome, not a degraded one:
// nil, nil means memory-only and nothing is wrong. What must never
// happen is retention silently not running while the operator believes
// it is, so every path that ends without a store says so in the log,
// once, at startup -- while the operator is still watching.
//
// Three states, deliberately distinct:
//
//   - No key file configured: memory-only. Logged at info, because it
//     is the ordinary default rather than a fault.
//   - A key file configured but unusable (missing, unreadable, too
//     short): retention stays off and this is a warning. There is no
//     unencrypted mode to fall back to, so the alternative would be
//     writing events in the clear, which the ADR rules out.
//   - Key present but the switch off: retention stays off.
//
// Neither off path deletes what an earlier run retained -- see
// leaveHistoryOff.
func openHistory(log *slog.Logger, cfg config.Config) *retention.Store {
	dir := historyDirectory(cfg)

	key, err := retention.LoadKey(cfg.History.KeyFile)
	switch {
	case err == retention.ErrNoKey:
		if cfg.History.Enabled {
			// Validate already turns this pair off (CFG-0080); reaching
			// here means it was set some other way, so say it again
			// rather than assume.
			log.Warn("on-disk event history is switched on but no key file is configured -- there is no unencrypted mode, so nothing is being retained")
		} else {
			log.Info("on-disk event history is off: no key file configured, so the corpus is the in-memory ring only")
		}
		leaveHistoryOff(log, dir)
		return nil
	case err != nil:
		log.Warn("on-disk event history is off: the key file could not be used -- MikroView runs normally and retains nothing", "keyFile", cfg.History.KeyFile, "err", err)
		return nil
	}

	if key.GroupOrWorldReadable {
		// Not a refusal: a secret mounted by an orchestrator commonly
		// arrives 0644, and refusing would break deployments that are
		// otherwise doing the right thing. Visible, though, because a
		// key anyone on the host can read protects rather less than the
		// documentation implies.
		log.Warn("the event history key file is readable by more than its owner -- tighten it to 0600 if you can", "keyFile", cfg.History.KeyFile)
	}

	if !cfg.History.Enabled {
		log.Info("on-disk event history is switched off")
		leaveHistoryOff(log, dir)
		return nil
	}

	st, err := retention.Open(retention.Options{
		Dir:      dir,
		Key:      key,
		Days:     cfg.History.Days,
		MaxBytes: cfg.History.MaxBytes,
	})
	if err != nil {
		log.Warn("on-disk event history is off: it could not be opened -- MikroView runs normally and retains nothing", "dir", dir, "err", err)
		return nil
	}
	log.Info("on-disk event history is on", "dir", dir, "days", cfg.History.Days, "maxBytes", cfg.History.MaxBytes)
	return st
}

// leaveHistoryOff deals with a history an earlier run left behind while
// retention is off: always kept and reported.
//
// Kept because "off" is also what a missing history block, a typo or a
// copied example reads as, and deleting a month of retained events on
// that reading cannot be undone. Deleting is only ever an admin choice
// made in the UI (#1354), never something a config setting can trigger.
// Needs no key: see retention.DaysHeld.
func leaveHistoryOff(log *slog.Logger, dir string) {
	files, err := retention.DaysHeld(dir)
	if err != nil {
		log.Warn("could not read what the on-disk event history holds", "dir", dir, "err", err)
		return
	}
	if len(files) == 0 {
		return
	}
	log.Warn(fmt.Sprintf(
		"on-disk event history is off, but %d day(s) retained by an earlier run are still in %s -- nothing is being deleted and nothing new is retained. "+
			"To use them again set history.enabled: true with the history.keyFile they were written under; "+
			"to delete them, an admin can do so from Settings",
		len(files), dir), "dir", dir, "days", len(files))
}
