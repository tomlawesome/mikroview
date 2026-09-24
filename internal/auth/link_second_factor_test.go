// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"testing"
	"time"
)

// testTOTPSecret is opaque to the store -- SetPendingTOTPSecret takes
// whatever the caller encoded and never reads it back apart from
// comparing it to "" -- so the shape here only has to look like the
// base32 internal/api actually stores.
const testTOTPSecret = "JBSWY3DPEHPK3PXP"

// enrolEverySecondFactor gives userID one of each kind of local second
// factor -- confirmed authenticator app, recovery codes and a passkey
// -- so a test asserting that linking clears "every" factor is not
// quietly only checking TOTP.
func enrolEverySecondFactor(t *testing.T, s *Store, userID string, now time.Time) {
	t.Helper()
	if err := s.SetPendingTOTPSecret(userID, testTOTPSecret); err != nil {
		t.Fatalf("SetPendingTOTPSecret: %v", err)
	}
	if err := s.ConfirmTOTP(userID, now, 42); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	if _, err := s.GenerateRecoveryCodes(userID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if _, err := s.AddPasskey(userID, testPasskey(1, "YubiKey")); err != nil {
		t.Fatalf("AddPasskey: %v", err)
	}
}

// TestLinkOIDCIdentityClearsEverySecondFactorForANonAdmin covers the
// gap #1249 shipped and #1253 (note 22375) caught. Linking already
// replaced a non-admin's password with an unmatchable hash, but left
// its second factor standing: an account whose SSO sign-in never asks
// for that factor, and whose owner could no longer remove it either,
// because DELETE /api/auth/totp is password-gated and the password was
// already gone.
//
// Asserts on the raw fields rather than HasSecondFactor alone, so a
// factor that is merely hidden from the predicate -- a pending secret,
// an unspent recovery code -- still fails this.
func TestLinkOIDCIdentityClearsEverySecondFactorForANonAdmin(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	if _, err := s.Register("alice", "password12345", now); err != nil {
		t.Fatalf("Register alice (the admin): %v", err)
	}
	bob, err := s.CreateUser("bob", "password12345", RoleUser, now)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	enrolEverySecondFactor(t, s, bob.ID, now)

	if err := s.LinkOIDCIdentity(bob.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	linked, ok := s.Get(bob.ID)
	if !ok {
		t.Fatal("the account vanished")
	}
	if linked.TOTPSecret != "" {
		t.Errorf("TOTPSecret = %q after linking, want it cleared", linked.TOTPSecret)
	}
	if !linked.TOTPConfirmedAt.IsZero() {
		t.Errorf("TOTPConfirmedAt = %v after linking, want the zero time", linked.TOTPConfirmedAt)
	}
	if linked.TOTPLastCounter != 0 {
		t.Errorf("TOTPLastCounter = %d after linking, want 0", linked.TOTPLastCounter)
	}
	if len(linked.RecoveryCodes) != 0 {
		t.Errorf("%d recovery codes survived linking, want none", len(linked.RecoveryCodes))
	}
	if len(linked.Passkeys) != 0 {
		t.Errorf("%d passkeys survived linking, want none", len(linked.Passkeys))
	}
	if linked.HasSecondFactor() {
		t.Error("a linked non-admin still reports as holding a second factor")
	}
	if linked.LocalPassword() {
		t.Error("a linked non-admin still reports as holding a local password")
	}
}

// TestLinkOIDCIdentityKeepsTheAdminsSecondFactor is the other half of
// the rule above. The admin keeps its local password permanently
// (owner, 2026-09-18, #1252: it must be able to sign in with the
// identity provider down), and for the same reason it has to keep the
// factor that password is paired with: #1253's forced-enrolment door
// checks every local account on every request, so an admin stripped of
// its factor here would be sent back to enrolment the moment its
// linked session ended.
func TestLinkOIDCIdentityKeepsTheAdminsSecondFactor(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	admin, err := s.Register("alice", "password12345", now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if admin.Role != RoleAdmin {
		t.Fatalf("the first registered account is %q, want the admin -- fixture assumption broken", admin.Role)
	}
	enrolEverySecondFactor(t, s, admin.ID, now)

	if err := s.LinkOIDCIdentity(admin.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	linked, ok := s.Get(admin.ID)
	if !ok {
		t.Fatal("the account vanished")
	}
	if linked.TOTPSecret != testTOTPSecret {
		t.Errorf("TOTPSecret = %q after linking, want the admin's factor untouched", linked.TOTPSecret)
	}
	if linked.TOTPLastCounter != 42 {
		t.Errorf("TOTPLastCounter = %d after linking, want the replay guard left at 42", linked.TOTPLastCounter)
	}
	if len(linked.Passkeys) != 1 {
		t.Errorf("%d passkeys after linking, want the admin's 1 kept", len(linked.Passkeys))
	}
	if len(linked.RecoveryCodes) == 0 {
		t.Error("the admin's recovery codes were cleared by linking")
	}
	if !linked.HasActiveTOTP() {
		t.Error("the admin's authenticator app stopped being active after linking")
	}
	if !linked.LocalPassword() {
		t.Error("the admin lost its local password to linking")
	}
}

// TestLinkOIDCIdentityRestoresTheSecondFactorWhenPersistFails extends
// the R6 rollback (TestLinkOIDCIdentityLeavesThePasswordWorkingWhenPersistFails
// in link_test.go) to the fields linking now also clears. A link that
// cannot be saved must put the factor back with the password: half a
// rollback would leave the operator with a working password and no way
// to pass the second-factor door it is paired with.
func TestLinkOIDCIdentityRestoresTheSecondFactorWhenPersistFails(t *testing.T) {
	// Register, CreateUser and each of the four enrolment writes below
	// persist as well, so the budget has to cover all six and fail only
	// on the link itself.
	s, err := OpenWithBackend(&saveBudgetBackend{left: 6})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	now := time.Now()
	if _, err := s.Register("alice", "password12345", now); err != nil {
		t.Fatalf("Register alice (the admin): %v", err)
	}
	bob, err := s.CreateUser("bob", "password12345", RoleUser, now)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	enrolEverySecondFactor(t, s, bob.ID, now)

	if err := s.LinkOIDCIdentity(bob.ID, "https://idp.example", "sub-1", now); err == nil {
		t.Fatal("LinkOIDCIdentity against a backend that cannot save = nil error, want one")
	}

	got, ok := s.Get(bob.ID)
	if !ok {
		t.Fatal("the account vanished")
	}
	if got.TOTPSecret != testTOTPSecret {
		t.Errorf("TOTPSecret = %q after a failed link, want it restored", got.TOTPSecret)
	}
	if got.TOTPLastCounter != 42 {
		t.Errorf("TOTPLastCounter = %d after a failed link, want the replay guard restored to 42", got.TOTPLastCounter)
	}
	if got.TOTPConfirmedAt.IsZero() {
		t.Error("TOTPConfirmedAt was left zeroed by a link that never saved")
	}
	if len(got.RecoveryCodes) == 0 {
		t.Error("the recovery codes were left cleared by a link that never saved")
	}
	if len(got.Passkeys) != 1 {
		t.Errorf("%d passkeys after a failed link, want the 1 restored", len(got.Passkeys))
	}
	if !got.HasSecondFactor() {
		t.Error("the account was left with no second factor by a link that never saved")
	}
}

// TestLinkOIDCIdentityClearsAPendingEnrolmentToo guards the case
// HasSecondFactor cannot see: a secret set by SetPendingTOTPSecret but
// never confirmed. Left behind, it is a live enrolment against an
// account that no longer has a password to protect it, and the next
// ConfirmTOTP would activate a factor nobody linked.
func TestLinkOIDCIdentityClearsAPendingEnrolmentToo(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	if _, err := s.Register("alice", "password12345", now); err != nil {
		t.Fatalf("Register alice (the admin): %v", err)
	}
	bob, err := s.CreateUser("bob", "password12345", RoleUser, now)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	if err := s.SetPendingTOTPSecret(bob.ID, testTOTPSecret); err != nil {
		t.Fatalf("SetPendingTOTPSecret: %v", err)
	}

	if err := s.LinkOIDCIdentity(bob.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	linked, ok := s.Get(bob.ID)
	if !ok {
		t.Fatal("the account vanished")
	}
	if linked.TOTPSecret != "" {
		t.Errorf("an unconfirmed TOTPSecret (%q) survived linking", linked.TOTPSecret)
	}
}
