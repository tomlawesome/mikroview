// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/routeros"
	"github.com/tomlawesome/mikroview/internal/settings"
)

// #1361: the router-backup drop box's switch, backupRuntime. These
// tests cover the lifecycle newBackupRuntime/Start/Stop/State own; the
// endpoint that drives it (PUT /api/settings/router-backups) is tested
// against a fake in internal/api/routerbackupswitch_test.go, the same
// split history_runtime_test.go and internal/api's history_settings_test.go
// already draw for history.

// backupSwitchConfig builds a Config whose TLS.StorePath (the SFTP host
// key's home, see backups.go's own doc comment) lives under this test's
// own temp dir, with Backup.Listen set to addr.
func backupSwitchConfig(t *testing.T, addr string) config.Config {
	t.Helper()
	var cfg config.Config
	cfg.TLS.StorePath = filepath.Join(t.TempDir(), "tls")
	cfg.Backup.Listen = addr
	return cfg
}

func TestNewBackupRuntimeStartsClosedWithNothingStoredAndNoLegacyValue(t *testing.T) {
	cfg := backupSwitchConfig(t, "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	r := newBackupRuntime(ctx, quietLog(), cfg, unpersistedSettings(t), nil, nil)
	t.Cleanup(r.Stop)

	if open, port := r.State(); open || port != "" {
		t.Fatalf("State() = (%v, %q), want closed with nothing stored and no legacy value", open, port)
	}
}

// The retired backup.enabled/MIKROVIEW_BACKUP_ENABLED config value seeds
// the settings store's starting position exactly once, on the first run
// that finds nothing stored yet (main.newBackupRuntime's own doc
// comment) -- an existing deployment that already had it on must not
// silently come up closed after this switch moved out of the config
// file.
func TestNewBackupRuntimeSeedsFromTheRetiredConfigValueOnce(t *testing.T) {
	cfg := backupSwitchConfig(t, "127.0.0.1:0")
	cfg.Backup.Enabled = true
	set := unpersistedSettings(t)

	r := newBackupRuntime(context.Background(), quietLog(), cfg, set, nil, nil)
	t.Cleanup(r.Stop)

	if open, _ := r.State(); !open {
		t.Fatal("a legacy backup.enabled: true did not seed the drop box open")
	}
	pos, ok := set.Backup()
	if !ok || !pos.Enabled {
		t.Fatalf("the seeded position was not stored: (%+v, %v)", pos, ok)
	}
}

// Once a position is stored, a leftover legacy config value must never
// be consulted again -- an admin who closed the drop box from Settings
// must not have it reopened by a stale backup.enabled: true nobody got
// around to deleting from config.yaml.
func TestNewBackupRuntimeNeverConsultsTheLegacyValueOnceSomethingIsStored(t *testing.T) {
	cfg := backupSwitchConfig(t, "127.0.0.1:0")
	cfg.Backup.Enabled = true
	set := unpersistedSettings(t)
	if err := set.SetBackup(settings.Backup{Enabled: false, ChangedBy: "alice"}); err != nil {
		t.Fatal(err)
	}

	r := newBackupRuntime(context.Background(), quietLog(), cfg, set, nil, nil)
	t.Cleanup(r.Stop)

	if open, _ := r.State(); open {
		t.Fatal("a stored closed position was overridden by the legacy config value")
	}
}

func TestNewBackupRuntimeStartsOpenWhenStoredPositionSaysOpen(t *testing.T) {
	cfg := backupSwitchConfig(t, "127.0.0.1:0")
	set := unpersistedSettings(t)
	if err := set.SetBackup(settings.Backup{Enabled: true, ChangedBy: "alice"}); err != nil {
		t.Fatal(err)
	}

	r := newBackupRuntime(context.Background(), quietLog(), cfg, set, nil, nil)
	t.Cleanup(r.Stop)

	open, port := r.State()
	if !open {
		t.Fatal("a stored open position came up closed")
	}
	if port != routeros.PortOf(cfg.Backup.Listen) {
		t.Errorf("port = %q, want %q", port, routeros.PortOf(cfg.Backup.Listen))
	}
}

func TestBackupRuntimeStartStopAreIdempotentAndStateReflectsBoth(t *testing.T) {
	cfg := backupSwitchConfig(t, "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := newBackupRuntime(ctx, quietLog(), cfg, unpersistedSettings(t), nil, nil)
	t.Cleanup(r.Stop)

	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if open, port := r.State(); !open || port == "" {
		t.Fatalf("State() after Start = (%v, %q), want open with a port", open, port)
	}
	// Idempotent: calling it again while already open is a no-op, not a
	// second bind attempt that would itself fail with "address in use".
	if err := r.Start(); err != nil {
		t.Fatalf("a second Start on an already-open runtime errored: %v", err)
	}

	r.Stop()
	if open, port := r.State(); open || port != "" {
		t.Fatalf("State() after Stop = (%v, %q), want closed", open, port)
	}
	// Idempotent the other way too.
	r.Stop()
}

// The one failure worth telling an admin about: the configured port is
// already taken by something else. Start must return that as a plain
// error -- handleRouterBackupSwitchUpdate turns it into a 409 -- not
// merely log it from an unwatched goroutine.
func TestBackupRuntimeStartReportsAPortAlreadyInUse(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { occupied.Close() })

	cfg := backupSwitchConfig(t, occupied.Addr().String())
	r := newBackupRuntime(context.Background(), quietLog(), cfg, unpersistedSettings(t), nil, nil)
	t.Cleanup(r.Stop)

	if err := r.Start(); err == nil {
		t.Fatal("Start on an already-occupied port returned no error")
	}
	if open, _ := r.State(); open {
		t.Fatal("a failed Start still reports the drop box as open")
	}
}
