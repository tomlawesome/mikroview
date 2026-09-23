// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/backup"
)

// TestBackupRestoreRoundTripCarriesPasskeys pins #1250's half of the
// promise backup_watchlist_roundtrip_test.go already pins for
// watchlist state: a passkey rides the accounts file with no envelope
// work of its own (the design's words -- "it rides the accounts file,
// so -backup carries it with no envelope work beyond a round-trip
// test"), so this exists only to prove that claim rather than to add
// any.
//
// It deliberately does not stop at comparing structs before and after
// restore -- the design calls that out by name as a test that would
// pass while proving nothing (a document that round-trips byte-for-byte
// but whose data nothing can actually use is not evidence the restore
// worked). This package has no WebAuthn ceremony to complete (that
// dependency belongs to a parallel #1250 slice, internal/api/webauthn.go),
// so instead it exercises the two operations that actually depend on
// the restored data being live and correct: RecordPasskeyAssertion
// (the store-level half of a passkey completing a login) advancing the
// restored credential's SignCount and LastUsedAt, and BurnRecoveryCode
// verifying a code that was minted -- and shown to the user -- before
// the backup was taken. Both would fail if the restore had silently
// dropped or corrupted the underlying hash/credential material, even
// if a naive struct comparison of the JSON would not have noticed.
func TestBackupRestoreRoundTripCarriesPasskeys(t *testing.T) {
	srcDir := t.TempDir()
	authPath := filepath.Join(srcDir, "users.json")

	now := time.Now().UTC().Truncate(time.Millisecond)
	s, err := auth.Open(authPath)
	if err != nil {
		t.Fatalf("auth.Open: %v", err)
	}
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	credID := []byte{0xAB, 0xCD, 0xEF, 0x01}
	added, err := s.AddPasskey(u.ID, auth.Passkey{
		ID:         credID,
		PublicKey:  []byte{1, 2, 3, 4, 5},
		SignCount:  3,
		Transports: []string{"internal", "hybrid"},
		Flags: auth.PasskeyFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		RPID:      "mikroview.example",
		Name:      "MacBook Touch ID",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("AddPasskey: %v", err)
	}
	// This is the account's first (and only) factor -- exactly the
	// point at which internal/api's registration-finish handler mints
	// recovery codes (see the design's mint-if-absent rule). Minted
	// directly here since that decision itself belongs to a different
	// slice; this test only needs codes to exist so it can prove they
	// survive the round trip.
	codes, err := s.GenerateRecoveryCodes(u.ID, now)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("test setup: GenerateRecoveryCodes returned %d codes, want 10", len(codes))
	}

	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", authPath)

	backupPath := filepath.Join(srcDir, "mikroview.backup")
	// --force: t.TempDir() is world-readable in some sandboxes, which
	// would otherwise trip writeBackup's own world-readable-directory
	// refusal -- unrelated to what this test is pinning, same reason
	// backup_watchlist_roundtrip_test.go's test does the same.
	if code := runBackup([]string{backupPath, "--force"}); code != 0 {
		t.Fatalf("runBackup = %d, want 0", code)
	}

	f, err := os.Open(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	env, err := backup.Read(f)
	f.Close()
	if err != nil {
		t.Fatalf("backup.Read: %v", err)
	}
	if _, ok := env.Stores["auth"]; !ok {
		t.Fatal(`backup envelope is missing the "auth" store`)
	}

	// Restore into a fresh directory -- a different path, the same way
	// a disaster recovery restores onto a new host.
	dstDir := t.TempDir()
	newAuthPath := filepath.Join(dstDir, "users.json")
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", newAuthPath)

	if code := runRestore([]string{backupPath}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}

	s2, err := auth.Open(newAuthPath)
	if err != nil {
		t.Fatalf("auth.Open after restore: %v", err)
	}
	got, ok := s2.Get(u.ID)
	if !ok {
		t.Fatal("restored store has no account for the registered user")
	}
	if len(got.Passkeys) != 1 {
		t.Fatalf("restored account has %d passkeys, want 1", len(got.Passkeys))
	}
	rp := got.Passkeys[0]
	if string(rp.ID) != string(credID) {
		t.Errorf("restored credential ID = %v, want %v", rp.ID, credID)
	}
	if rp.Name != added.Name {
		t.Errorf("restored Name = %q, want %q", rp.Name, added.Name)
	}
	if rp.RPID != "mikroview.example" {
		t.Errorf("restored RPID = %q, want %q", rp.RPID, "mikroview.example")
	}
	if rp.SignCount != 3 {
		t.Errorf("restored SignCount = %d, want 3", rp.SignCount)
	}
	if rp.Flags != (auth.PasskeyFlags{UserPresent: true, UserVerified: true, BackupEligible: true, BackupState: true}) {
		t.Errorf("restored Flags = %+v, did not round-trip", rp.Flags)
	}
	if len(got.RecoveryCodes) != 10 {
		t.Fatalf("restored account has %d recovery codes, want 10", len(got.RecoveryCodes))
	}

	// Prove the restored passkey is genuinely live, not just present in
	// the JSON: advance it past its pre-backup SignCount and confirm
	// the store's forward-only guard (RecordPasskeyAssertion) accepts
	// the advance against the restored data.
	later := now.Add(time.Hour)
	if err := s2.RecordPasskeyAssertion(u.ID, credID, 4, later); err != nil {
		t.Fatalf("RecordPasskeyAssertion against the restored store: %v", err)
	}
	got2, _ := s2.Get(u.ID)
	if got2.Passkeys[0].SignCount != 4 {
		t.Errorf("SignCount after a post-restore assertion = %d, want 4", got2.Passkeys[0].SignCount)
	}
	if !got2.Passkeys[0].LastUsedAt.Equal(later) {
		t.Errorf("LastUsedAt after a post-restore assertion = %v, want %v", got2.Passkeys[0].LastUsedAt, later)
	}

	// Prove a recovery code minted before the backup still verifies
	// after restore -- the hash, not just the count, survived.
	ok, err = s2.BurnRecoveryCode(u.ID, codes[0], later)
	if err != nil {
		t.Fatalf("BurnRecoveryCode against the restored store: %v", err)
	}
	if !ok {
		t.Error("a recovery code minted before the backup did not verify after restore")
	}
	// And a second attempt at the same code is refused -- proving the
	// burn itself persisted into the restored store, not just that the
	// hash matched once.
	ok, err = s2.BurnRecoveryCode(u.ID, codes[0], later)
	if err != nil {
		t.Fatalf("BurnRecoveryCode (replay) against the restored store: %v", err)
	}
	if ok {
		t.Error("a spent recovery code verified a second time after restore")
	}
}
