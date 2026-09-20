// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// #1264 finding 5: a configured-but-unreadable retention key and no key
// at all both leave the vault's key nil, but they are not the same fact
// -- one is #394's ordinary disabled state, the other is a fault an
// operator must not be told to fix by minting a new key, since that
// overwrites the file every existing backup is encrypted under. These
// cases are openRouterBackupVault's own contract for keyUnreadable, the
// bit every downstream hop (api.SetupInstance.BackupKeyUnreadable,
// routerBackupsResponse.keyUnreadable, the wizard's blocked key) now
// carries forward instead of collapsing back into Vault.Enabled() alone.

func TestOpenRouterBackupVaultWithoutAKeyIsNotUnreadable(t *testing.T) {
	cfg := historyConfig(t, false, "")
	_, keyUnreadable := openRouterBackupVault(quietLog(), cfg, nil, nil)
	if keyUnreadable {
		t.Fatal("openRouterBackupVault reported keyUnreadable with no key configured at all -- that is #394's ordinary disabled state, not a fault")
	}
}

func TestOpenRouterBackupVaultReportsAMissingKeyFileAsUnreadable(t *testing.T) {
	cfg := historyConfig(t, false, filepath.Join(t.TempDir(), "absent.key"))
	_, keyUnreadable := openRouterBackupVault(quietLog(), cfg, nil, nil)
	if !keyUnreadable {
		t.Fatal("openRouterBackupVault did not report keyUnreadable for a configured key file that does not exist")
	}
}

func TestOpenRouterBackupVaultReportsATooShortKeyAsUnreadable(t *testing.T) {
	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("too short"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := historyConfig(t, false, short)
	_, keyUnreadable := openRouterBackupVault(quietLog(), cfg, nil, nil)
	if !keyUnreadable {
		t.Fatal("openRouterBackupVault did not report keyUnreadable for a key file below the length floor")
	}
}

func TestOpenRouterBackupVaultWithAGoodKeyIsNotUnreadable(t *testing.T) {
	cfg := historyConfig(t, false, writeKeyFile(t))
	v, keyUnreadable := openRouterBackupVault(quietLog(), cfg, nil, nil)
	if v == nil {
		t.Fatal("openRouterBackupVault returned no vault with a valid key")
	}
	if keyUnreadable {
		t.Fatal("openRouterBackupVault reported keyUnreadable for a key that loaded fine")
	}
}
