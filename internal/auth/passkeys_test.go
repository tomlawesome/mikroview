// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testPasskey builds a fixture Passkey with a distinct credential ID --
// id is folded into ID and PublicKey so every fixture in a test is
// trivially distinguishable in a failure message.
func testPasskey(id byte, name string) Passkey {
	return Passkey{
		ID:         []byte{id},
		PublicKey:  []byte{id, id, id},
		Transports: []string{"internal"},
		Flags: PasskeyFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    true,
		},
		RPID: "mikroview.example",
		Name: name,
	}
}

func TestAddPasskeyNormalisesAnEmptyNameToANumberedDefault(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.AddPasskey(u.ID, testPasskey(1, ""))
	if err != nil {
		t.Fatalf("AddPasskey: %v", err)
	}
	if got.Name != "Passkey 1" {
		t.Errorf("Name = %q, want %q", got.Name, "Passkey 1")
	}

	got2, err := s.AddPasskey(u.ID, testPasskey(2, "  "))
	if err != nil {
		t.Fatalf("AddPasskey (second): %v", err)
	}
	if got2.Name != "Passkey 2" {
		t.Errorf("Name = %q, want %q", got2.Name, "Passkey 2")
	}

	stored, ok := s.Get(u.ID)
	if !ok || len(stored.Passkeys) != 2 {
		t.Fatalf("expected 2 stored passkeys, got %+v", stored)
	}
}

func TestAddPasskeyTrimsAndBoundsAGivenName(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	pk := testPasskey(1, "  YubiKey  ")
	got, err := s.AddPasskey(u.ID, pk)
	if err != nil {
		t.Fatalf("AddPasskey: %v", err)
	}
	if got.Name != "YubiKey" {
		t.Errorf("Name = %q, want trimmed %q", got.Name, "YubiKey")
	}

	long := make([]rune, maxPasskeyNameLength+40)
	for i := range long {
		long[i] = 'x'
	}
	pk2 := testPasskey(2, string(long))
	got2, err := s.AddPasskey(u.ID, pk2)
	if err != nil {
		t.Fatalf("AddPasskey (long name): %v", err)
	}
	if len(got2.Name) != maxPasskeyNameLength {
		t.Errorf("Name length = %d, want %d", len(got2.Name), maxPasskeyNameLength)
	}
}

// TestAddPasskeyRefusesADuplicateCredentialID proves ErrPasskeyDuplicate
// can actually fire: the guard is deleted, the test is watched to fail,
// then the guard is restored (see this file's final report for which
// guards were proved this way).
func TestAddPasskeyRefusesADuplicateCredentialID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(9, "first")); err != nil {
		t.Fatalf("AddPasskey: %v", err)
	}

	if _, err := s.AddPasskey(u.ID, testPasskey(9, "second")); !errors.Is(err, ErrPasskeyDuplicate) {
		t.Errorf("AddPasskey with a duplicate credential ID = %v, want %v", err, ErrPasskeyDuplicate)
	}
	stored, _ := s.Get(u.ID)
	if len(stored.Passkeys) != 1 {
		t.Errorf("a duplicate add changed the stored count to %d, want 1", len(stored.Passkeys))
	}
}

// TestAddPasskeyRefusesAnEleventhCredential proves the cap can actually
// fire, the same way the duplicate test above does.
func TestAddPasskeyRefusesAnEleventhCredential(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPasskeysPerAccount; i++ {
		if _, err := s.AddPasskey(u.ID, testPasskey(byte(i), "")); err != nil {
			t.Fatalf("AddPasskey #%d: %v", i, err)
		}
	}

	if _, err := s.AddPasskey(u.ID, testPasskey(200, "eleventh")); !errors.Is(err, ErrPasskeyLimitReached) {
		t.Errorf("AddPasskey past the cap = %v, want %v", err, ErrPasskeyLimitReached)
	}
	stored, _ := s.Get(u.ID)
	if len(stored.Passkeys) != maxPasskeysPerAccount {
		t.Errorf("stored count after refusal = %d, want %d", len(stored.Passkeys), maxPasskeysPerAccount)
	}
}

func TestAddPasskeyUnknownUserReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey("no-such-user", testPasskey(1, "")); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("AddPasskey on an unknown user = %v, want %v", err, ErrUserNotFound)
	}
}

func TestAddPasskeyRefusesWhenNotPersisted(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey("whoever", testPasskey(1, "")); !errors.Is(err, ErrNotPersisted) {
		t.Errorf("AddPasskey on an unpersisted store = %v, want %v", err, ErrNotPersisted)
	}
}

func TestRenamePasskeyChangesTheStoredName(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "original")); err != nil {
		t.Fatal(err)
	}

	got, err := s.RenamePasskey(u.ID, []byte{1}, "  renamed  ")
	if err != nil {
		t.Fatalf("RenamePasskey: %v", err)
	}
	if got.Name != "renamed" {
		t.Errorf("Name = %q, want %q", got.Name, "renamed")
	}
	stored, _ := s.Get(u.ID)
	if stored.Passkeys[0].Name != "renamed" {
		t.Errorf("stored Name = %q, want %q", stored.Passkeys[0].Name, "renamed")
	}
}

func TestRenamePasskeyUnknownCredentialReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenamePasskey(u.ID, []byte{99}, "x"); !errors.Is(err, ErrPasskeyNotFound) {
		t.Errorf("RenamePasskey on an unknown credential = %v, want %v", err, ErrPasskeyNotFound)
	}
}

// TestDeletePasskeyKeepsRecoveryCodesWhileAnotherFactorRemains and
// TestDeletePasskeyClearsRecoveryCodesWhenItWasTheLastFactor are the two
// halves of #1250's clear-conditional rule -- the exact pair of tests a
// careless "always clear" or "never clear" implementation would still
// pass one of.
// TestConcurrentGetDuringRenameAndAssertionIsRaceFree proves Get's
// shallow *User copy (store.go) is safe to read concurrently with
// RenamePasskey and RecordPasskeyAssertion, which both write into the
// same account's Passkeys slice. Before both were changed to replace
// the whole slice wholesale rather than writing a field on
// u.Passkeys[idx] in place, -race caught a reader here touching a
// Passkey field through the copy's shared backing array at the same
// moment one of these wrote it. Run with -race -- the detector itself
// is the assertion; nothing this function checks by value would catch
// a regression on its own.
func TestConcurrentGetDuringRenameAndAssertionIsRaceFree(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "original")); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if got, ok := s.Get(u.ID); ok && len(got.Passkeys) > 0 {
				_ = got.Passkeys[0].Name
				_ = got.Passkeys[0].SignCount
			}
		}
	}()

	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		for i := 0; i < 200; i++ {
			if _, err := s.RenamePasskey(u.ID, []byte{1}, fmt.Sprintf("name-%d", i)); err != nil {
				t.Errorf("RenamePasskey: %v", err)
				return
			}
		}
	}()
	go func() {
		defer writers.Done()
		for i := 0; i < 200; i++ {
			if err := s.RecordPasskeyAssertion(u.ID, []byte{1}, uint32(i+1), time.Now()); err != nil {
				t.Errorf("RecordPasskeyAssertion: %v", err)
				return
			}
		}
	}()
	writers.Wait()
	close(stop)
	<-readerDone
}

func TestDeletePasskeyKeepsRecoveryCodesWhileAnotherFactorRemains(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "one")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(2, "two")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if _, err := s.DeletePasskey(u.ID, []byte{1}); err != nil {
		t.Fatalf("DeletePasskey: %v", err)
	}
	got, _ := s.Get(u.ID)
	if len(got.Passkeys) != 1 {
		t.Errorf("passkey count after delete = %d, want 1", len(got.Passkeys))
	}
	if len(got.RecoveryCodes) != 10 {
		t.Errorf("recovery codes after deleting one of two passkeys = %d, want 10 (kept)", len(got.RecoveryCodes))
	}
}

func TestDeletePasskeyClearsRecoveryCodesWhenItWasTheLastFactor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "only")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if _, err := s.DeletePasskey(u.ID, []byte{1}); err != nil {
		t.Fatalf("DeletePasskey: %v", err)
	}
	got, _ := s.Get(u.ID)
	if len(got.Passkeys) != 0 {
		t.Errorf("passkey count after deleting the only one = %d, want 0", len(got.Passkeys))
	}
	if len(got.RecoveryCodes) != 0 {
		t.Errorf("recovery codes after deleting the last factor = %d, want 0 (cleared)", len(got.RecoveryCodes))
	}
}

// TestDeletePasskeyWithActiveTOTPKeepsRecoveryCodes covers the same rule
// from ClearTOTP's ledger: TOTP is still active, so removing the only
// passkey does not clear the codes it also backs.
func TestDeletePasskeyWithActiveTOTPKeepsRecoveryCodes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "only")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if _, err := s.DeletePasskey(u.ID, []byte{1}); err != nil {
		t.Fatalf("DeletePasskey: %v", err)
	}
	got, _ := s.Get(u.ID)
	if len(got.RecoveryCodes) != 10 {
		t.Errorf("recovery codes after removing the only passkey while TOTP stays active = %d, want 10 (kept)", len(got.RecoveryCodes))
	}
}

func TestDeletePasskeyUnknownCredentialReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeletePasskey(u.ID, []byte{99}); !errors.Is(err, ErrPasskeyNotFound) {
		t.Errorf("DeletePasskey on an unknown credential = %v, want %v", err, ErrPasskeyNotFound)
	}
}

// TestClearTOTPKeepsRecoveryCodesWhileAPasskeyRemains pins #1250's
// change to #1249's ClearTOTP: before this, RecoveryCodes was always
// dropped. With a passkey still active, dropping the codes here would
// orphan that passkey's own fallback.
func TestClearTOTPKeepsRecoveryCodesWhileAPasskeyRemains(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "backup key")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if err := s.ClearTOTP(u.ID); err != nil {
		t.Fatalf("ClearTOTP: %v", err)
	}
	got, _ := s.Get(u.ID)
	if got.HasActiveTOTP() {
		t.Error("ClearTOTP left the authenticator-app factor active")
	}
	if len(got.RecoveryCodes) != 10 {
		t.Errorf("recovery codes after ClearTOTP with a passkey still active = %d, want 10 (kept)", len(got.RecoveryCodes))
	}
}

// TestClearTOTPWithNoPasskeysStillClearsRecoveryCodes pins that #1249's
// original behaviour survives for the case #1250 doesn't change: no
// passkey ever existed, so clearing TOTP is clearing the account's last
// factor, exactly as before.
func TestClearTOTPWithNoPasskeysStillClearsRecoveryCodes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if err := s.ClearTOTP(u.ID); err != nil {
		t.Fatalf("ClearTOTP: %v", err)
	}
	got, _ := s.Get(u.ID)
	if len(got.RecoveryCodes) != 0 {
		t.Errorf("recovery codes after ClearTOTP with no passkeys = %d, want 0 (cleared)", len(got.RecoveryCodes))
	}
}

func TestClearPasskeysRemovesAllAndAppliesTheSameConditionalRule(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "one")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(2, "two")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	if err := s.ClearPasskeys(u.ID); err != nil {
		t.Fatalf("ClearPasskeys: %v", err)
	}
	got, _ := s.Get(u.ID)
	if len(got.Passkeys) != 0 {
		t.Errorf("passkeys after ClearPasskeys = %d, want 0", len(got.Passkeys))
	}
	// TOTP is still active, so the shared codes must survive.
	if len(got.RecoveryCodes) != 10 {
		t.Errorf("recovery codes after ClearPasskeys with TOTP still active = %d, want 10 (kept)", len(got.RecoveryCodes))
	}

	if err := s.ClearTOTP(u.ID); err != nil {
		t.Fatalf("ClearTOTP: %v", err)
	}
	got, _ = s.Get(u.ID)
	if len(got.RecoveryCodes) != 0 {
		t.Errorf("recovery codes after clearing the last remaining factor = %d, want 0", len(got.RecoveryCodes))
	}
}

func TestClearPasskeysUnknownUserReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClearPasskeys("no-such-user"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ClearPasskeys on an unknown user = %v, want %v", err, ErrUserNotFound)
	}
}

// TestClearAllSecondFactorsClearsEverythingUnconditionally is the CLI's
// path: unlike ClearTOTP/DeletePasskey/ClearPasskeys, there is no
// factor-remaining case to keep codes for -- everything goes together.
func TestClearAllSecondFactorsClearsEverythingUnconditionally(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "one")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(2, "two")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if !s.HasActiveTOTP(u.ID) {
		t.Fatal("test setup: expected an active TOTP factor")
	}

	if err := s.ClearAllSecondFactors(u.ID); err != nil {
		t.Fatalf("ClearAllSecondFactors: %v", err)
	}
	got, _ := s.Get(u.ID)
	if got.HasActiveTOTP() {
		t.Error("ClearAllSecondFactors left TOTP active")
	}
	if len(got.Passkeys) != 0 {
		t.Errorf("passkeys after ClearAllSecondFactors = %d, want 0", len(got.Passkeys))
	}
	if len(got.RecoveryCodes) != 0 {
		t.Errorf("recovery codes after ClearAllSecondFactors = %d, want 0", len(got.RecoveryCodes))
	}
	if got.HasSecondFactor() {
		t.Error("HasSecondFactor is still true after ClearAllSecondFactors")
	}
}

func TestClearAllSecondFactorsUnknownUserReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClearAllSecondFactors("no-such-user"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ClearAllSecondFactors on an unknown user = %v, want %v", err, ErrUserNotFound)
	}
}

// TestRecordPasskeyAssertionOnlyMovesSignCountForward mirrors
// TestRecordTOTPCounterOnlyMovesForward: a stale or equal sign count is
// a no-op on the count, not an error, and LastUsedAt still advances --
// the 0-to-0 case is the ordinary one for a platform authenticator that
// never reports a nonzero counter, and must not be refused.
func TestRecordPasskeyAssertionOnlyMovesSignCountForward(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "")); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordPasskeyAssertion(u.ID, []byte{1}, 5, now); err != nil {
		t.Fatalf("RecordPasskeyAssertion: %v", err)
	}
	got, _ := s.Get(u.ID)
	if got.Passkeys[0].SignCount != 5 {
		t.Fatalf("SignCount = %d after first assertion, want 5", got.Passkeys[0].SignCount)
	}

	later := now.Add(time.Hour)
	for _, stale := range []uint32{5, 3, 0} {
		if err := s.RecordPasskeyAssertion(u.ID, []byte{1}, stale, later); err != nil {
			t.Errorf("RecordPasskeyAssertion(%d) = %v, want no error -- a stale count is a no-op", stale, err)
		}
		got, _ = s.Get(u.ID)
		if got.Passkeys[0].SignCount != 5 {
			t.Fatalf("RecordPasskeyAssertion(%d) wound SignCount back to %d", stale, got.Passkeys[0].SignCount)
		}
		// LastUsedAt still moves even when the count doesn't -- a
		// zero-reporting authenticator must still show as recently
		// used.
		if !got.Passkeys[0].LastUsedAt.Equal(later) {
			t.Errorf("LastUsedAt after a stale-count assertion = %v, want %v", got.Passkeys[0].LastUsedAt, later)
		}
	}
}

func TestRecordPasskeyAssertionUnknownCredentialReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPasskeyAssertion(u.ID, []byte{99}, 1, time.Now()); !errors.Is(err, ErrPasskeyNotFound) {
		t.Errorf("RecordPasskeyAssertion on an unknown credential = %v, want %v", err, ErrPasskeyNotFound)
	}
}

func TestHasSecondFactorCoversEveryCombination(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get(u.ID)
	if got.HasSecondFactor() {
		t.Error("a freshly registered account reports a second factor it doesn't have")
	}

	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	got, _ = s.Get(u.ID)
	if !got.HasSecondFactor() {
		t.Error("an active TOTP factor is not reported by HasSecondFactor")
	}

	if err := s.ClearTOTP(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "")); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(u.ID)
	if !got.HasSecondFactor() {
		t.Error("a passkey alone is not reported by HasSecondFactor")
	}

	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 1)
	got, _ = s.Get(u.ID)
	if !got.HasSecondFactor() {
		t.Error("TOTP and a passkey together are not reported by HasSecondFactor")
	}
}

// TestListBlanksPasskeysAndPasskeyCountReadsTheLiveData is the trap this
// slice's brief named explicitly: a naive len() on List()'s output would
// read zero for every account, because List() blanks Passkeys the same
// way it blanks TOTPSecret. PasskeyCount exists precisely so a caller
// doesn't have to make that mistake.
func TestListBlanksPasskeysAndPasskeyCountReadsTheLiveData(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPasskey(u.ID, testPasskey(2, "")); err != nil {
		t.Fatal(err)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("List() returned %d users, want 1", len(list))
	}
	if list[0].Passkeys != nil {
		t.Errorf("List()'s copy carries Passkeys (%v), want it blanked to nil", list[0].Passkeys)
	}

	if got := s.PasskeyCount(u.ID); got != 2 {
		t.Errorf("PasskeyCount = %d, want 2 -- it must read live data, not a List() copy", got)
	}
	if got := s.PasskeyCount("no-such-user"); got != 0 {
		t.Errorf("PasskeyCount for an unknown user = %d, want 0", got)
	}
}

// TestPasskeyWritesLeaveStateWhenPersistFails is this slice's version of
// TestTOTPWritesLeaveStateWhenPersistFails: every write in passkeys.go
// follows the restore-on-persist-failure contract, checked here with a
// backend whose Save always fails after setup.
func TestPasskeyWritesLeaveStateWhenPersistFails(t *testing.T) {
	budget := &saveBudgetBackend{left: 1}
	s, err := OpenWithBackend(budget)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}

	budget.left = 1
	if _, err := s.AddPasskey(u.ID, testPasskey(1, "keeper")); err != nil {
		t.Fatalf("AddPasskey (fixture): %v", err)
	}
	budget.left = 1
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes (fixture): %v", err)
	}

	budget.left = 0
	if _, err := s.AddPasskey(u.ID, testPasskey(2, "should not stick")); err == nil {
		t.Fatal("AddPasskey against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); len(got.Passkeys) != 1 {
		t.Errorf("AddPasskey's in-memory state changed even though the write failed: %d passkeys, want 1", len(got.Passkeys))
	}

	if _, err := s.RenamePasskey(u.ID, []byte{1}, "renamed"); err == nil {
		t.Fatal("RenamePasskey against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); got.Passkeys[0].Name != "keeper" {
		t.Errorf("RenamePasskey's in-memory state changed even though the write failed: Name = %q", got.Passkeys[0].Name)
	}

	if err := s.RecordPasskeyAssertion(u.ID, []byte{1}, 99, now.Add(time.Hour)); err == nil {
		t.Fatal("RecordPasskeyAssertion against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); got.Passkeys[0].SignCount != 0 || !got.Passkeys[0].LastUsedAt.IsZero() {
		t.Errorf("RecordPasskeyAssertion's in-memory state changed even though the write failed: %+v", got.Passkeys[0])
	}

	if _, err := s.DeletePasskey(u.ID, []byte{1}); err == nil {
		t.Fatal("DeletePasskey against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); len(got.Passkeys) != 1 || len(got.RecoveryCodes) != 10 {
		t.Errorf("DeletePasskey's in-memory state changed even though the write failed: %d passkeys, %d codes", len(got.Passkeys), len(got.RecoveryCodes))
	}

	if err := s.ClearPasskeys(u.ID); err == nil {
		t.Fatal("ClearPasskeys against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); len(got.Passkeys) != 1 || len(got.RecoveryCodes) != 10 {
		t.Errorf("ClearPasskeys' in-memory state changed even though the write failed: %d passkeys, %d codes", len(got.Passkeys), len(got.RecoveryCodes))
	}

	if err := s.ClearAllSecondFactors(u.ID); err == nil {
		t.Fatal("ClearAllSecondFactors against a backend that cannot save = nil error, want one")
	}
	if got, _ := s.Get(u.ID); len(got.Passkeys) != 1 || len(got.RecoveryCodes) != 10 {
		t.Errorf("ClearAllSecondFactors' in-memory state changed even though the write failed: %d passkeys, %d codes", len(got.Passkeys), len(got.RecoveryCodes))
	}
}
