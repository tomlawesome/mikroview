// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"path/filepath"
	"testing"
	"time"
)

// Linking is a one-way, destructive conversion. The account becomes
// SSO-only in the same operation, because a dual-mode account -- local
// password still works, SSO also works -- keeps the weaker local attack
// surface alive on an account that is supposed to have moved past it.
//
// The invariant lives inside LinkOIDCIdentity rather than at the API
// call site, so these test the store directly.
func TestLinkingDestroysTheLocalPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)

	u, err := s.Register("alice", "password123", time.Now())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.Authenticate("alice", "password123", time.Now()); err != nil {
		t.Fatalf("the password should work before linking: %v", err)
	}

	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	if _, err := s.Authenticate("alice", "password123", time.Now()); err == nil {
		t.Error("the old password still works after linking -- the account is dual-mode")
	}
	linked, _ := s.ByUsername("alice")
	if linked.LocalPassword() {
		t.Error("the linked account still reports a local password")
	}
	if linked.OIDCIssuer != "https://idp.example" || linked.OIDCSubject != "subject-1" {
		t.Errorf("identity not attached: %+v", linked)
	}
}

// The replacement hash must be a real Argon2id hash, not "". An empty
// or malformed hash short-circuits VerifyPassword before Argon2id runs,
// so a login attempt against an SSO-only account would return
// measurably faster than one against a password account -- telling an
// attacker which accounts are worth attacking locally.
func TestLinkingLeavesARealUnmatchableHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	u, _ := s.Register("alice", "password123", time.Now())

	before, _ := s.Get(u.ID)
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}
	after, _ := s.Get(u.ID)

	if after.PasswordHash == "" {
		t.Fatal("password hash was blanked rather than replaced")
	}
	if after.PasswordHash == before.PasswordHash {
		t.Fatal("password hash was left untouched")
	}
	if VerifyPassword(after.PasswordHash, "anything-at-all") {
		t.Error("the replacement hash matched a guess")
	}
}

// Every session issued before the link has to die: the account's
// credentials just changed fundamentally.
func TestLinkingInvalidatesEarlierSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	u, _ := s.Register("alice", "password123", time.Now())

	linkedAt := time.Now()
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", linkedAt); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	after, _ := s.Get(u.ID)
	if !after.PasswordChangedAt.Equal(linkedAt) {
		t.Errorf("PasswordChangedAt = %v, want %v -- sessions issued before the link stay valid",
			after.PasswordChangedAt, linkedAt)
	}
}

// Refusing a taken identity is what stops someone attaching their own
// IdP account to a colleague's -- or to the admin's.
func TestLinkingRefusesAnIdentityHeldByAnotherAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	alice, _ := s.Register("alice", "password123", time.Now())
	bob, _ := s.CreateUser("bob", "password456", RoleUser, time.Now())

	if err := s.LinkOIDCIdentity(alice.ID, "https://idp.example", "shared-subject", time.Now()); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if err := s.LinkOIDCIdentity(bob.ID, "https://idp.example", "shared-subject", time.Now()); err != ErrOIDCIdentityTaken {
		t.Fatalf("expected ErrOIDCIdentityTaken, got %v", err)
	}

	// And the refusal must be total -- bob keeps his password rather
	// than being half-converted by a link that didn't happen.
	if _, err := s.Authenticate("bob", "password456", time.Now()); err != nil {
		t.Errorf("a refused link destroyed the account's password anyway: %v", err)
	}
	stillBob, _ := s.ByUsername("bob")
	if !stillBob.LocalPassword() || stillBob.OIDCIssuer != "" {
		t.Errorf("a refused link mutated the account: %+v", stillBob)
	}
}

func TestLinkingIsIdempotentForTheSameAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	u, _ := s.Register("alice", "password123", time.Now())

	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Errorf("re-linking the same identity to the same account failed: %v", err)
	}
}

// After linking, the account falls under -recover-admin-account's
// refusal: mikroview holds no credential for it, so recovery is the
// identity provider's job. Consistent with the CLI's behaviour, and
// worth pinning so a future change to LocalPassword() can't silently
// re-open local recovery for a linked account.
func TestALinkedAccountIsNotLocallyRecoverable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	u, _ := s.Register("alice", "password123", time.Now())
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	admin := s.Admin()
	if admin == nil {
		t.Fatal("no admin")
	}
	if admin.LocalPassword() {
		t.Error("a linked admin still reports as locally recoverable")
	}
}
