// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestOIDCStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestFindOrCreateOIDCUserProvisionsFirstUserAsAdmin(t *testing.T) {
	s := newTestOIDCStore(t)

	u, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", time.Now())
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}
	if !created {
		t.Error("expected created=true for a brand-new identity")
	}
	if u.Role != RoleAdmin {
		t.Errorf("Role = %q, want admin (first-ever account)", u.Role)
	}
	if u.OIDCIssuer != "https://idp.example" || u.OIDCSubject != "sub-1" {
		t.Errorf("identity not recorded: %+v", u)
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want alice (hint was free)", u.Username)
	}
}

func TestFindOrCreateOIDCUserSecondIdentityIsRegularUser(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()

	if _, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", now); err != nil {
		t.Fatalf("first FindOrCreateOIDCUser: %v", err)
	}
	u, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-2", "bob", now)
	if err != nil {
		t.Fatalf("second FindOrCreateOIDCUser: %v", err)
	}
	if !created {
		t.Error("expected created=true")
	}
	if u.Role != RoleUser {
		t.Errorf("Role = %q, want user (not the first account)", u.Role)
	}
}

func TestFindOrCreateOIDCUserReusesExistingIdentity(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()

	first, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", now)
	if err != nil || !created {
		t.Fatalf("first call: user=%+v created=%v err=%v", first, created, err)
	}

	later := now.Add(time.Hour)
	second, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", later)
	if err != nil {
		t.Fatalf("second FindOrCreateOIDCUser: %v", err)
	}
	if created {
		t.Error("expected created=false on a repeat login for the same identity")
	}
	if second.ID != first.ID {
		t.Errorf("second call returned a different user: %+v vs %+v", second, first)
	}
	if !second.LastLogin.Equal(later) {
		t.Errorf("LastLogin = %v, want %v (repeat login should update it)", second.LastLogin, later)
	}
	if s.Count() != 1 {
		t.Errorf("Count() = %d, want 1 -- a repeat login must not create a second account", s.Count())
	}
}

func TestFindOrCreateOIDCUserNeverAutoLinksByUsernameHint(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()

	// A local password account already owns the username "alice".
	if _, err := s.Register("alice", "password12345", now); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// A completely unrelated OIDC identity that happens to present the
	// same preferred_username must NOT attach to that existing account
	// -- identity is (issuer, subject) only.
	u, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-999", "alice", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}
	if !created {
		t.Fatal("expected a new account to be provisioned, not attached to the existing 'alice'")
	}
	if u.Username == "alice" {
		t.Fatal("OIDC user was given the exact username of an unrelated existing local account")
	}
	if u.OIDCIssuer == "" {
		t.Error("expected the synthetic-username account to still carry the OIDC identity")
	}
	if s.Count() != 2 {
		t.Errorf("Count() = %d, want 2 (local alice + the new synthetic-username OIDC account)", s.Count())
	}
}

func TestFindOrCreateOIDCUserSyntheticUsernameIsStableAcrossRetries(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()

	if _, err := s.Register("bob", "password12345", now); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Same (issuer, subject) + colliding hint, called twice -- the
	// second call must resolve to the exact same account (via the
	// oidcIndex lookup), not generate a second synthetic username.
	a, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "bob", now)
	if err != nil || !created {
		t.Fatalf("first call: user=%+v created=%v err=%v", a, created, err)
	}
	b, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "bob", now)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Error("second call for the same identity should not create a new account")
	}
	if a.ID != b.ID || a.Username != b.Username {
		t.Errorf("retried provisioning diverged: %+v vs %+v", a, b)
	}
}

func TestFindOrCreateOIDCUserEmptyHintGetsSyntheticUsername(t *testing.T) {
	s := newTestOIDCStore(t)
	u, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "", time.Now())
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}
	if u.Username == "" {
		t.Fatal("expected a non-empty synthetic username when the hint is empty")
	}
}

func TestFindOrCreateOIDCUserNotGatedByClosedLocalRegistration(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()

	if _, err := s.Register("first-admin", "password12345", now); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Local self-registration is now closed (Count() > 0) -- JIT OIDC
	// provisioning must be unaffected, unlike Register.
	if _, err := s.Register("second-local", "password12345", now); err != ErrRegistrationClosed {
		t.Fatalf("expected local Register to be closed, got %v", err)
	}

	u, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "someone", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser after local registration closed: %v", err)
	}
	if !created || u == nil {
		t.Fatal("expected JIT provisioning to succeed regardless of Register's one-time gate")
	}
}

func TestFindOrCreateOIDCUserRefusesWhenNotPersisted(t *testing.T) {
	s, _ := Open("")
	if _, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "someone", time.Now()); err != ErrNotPersisted {
		t.Errorf("expected ErrNotPersisted, got %v", err)
	}
}

func TestFindOrCreateOIDCUserRefusesWhenAuthDisabled(t *testing.T) {
	s := newTestOIDCStore(t)
	if err := s.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "someone", time.Now()); err != ErrAuthDisabled {
		t.Errorf("expected ErrAuthDisabled, got %v", err)
	}
}

// TestOIDCOnlyUserPasswordHashIsUnmatchableNotEmpty guards against a
// real timing side-channel found during design review: an empty
// PasswordHash would make VerifyPassword's malformed-hash guard return
// false *before* running Argon2id at all, responding measurably faster
// than either a real password mismatch or an unknown username (both of
// which always pay the Argon2id cost) -- letting a local-login attempt
// distinguish "this username is SSO-only" purely from response time.
func TestOIDCOnlyUserPasswordHashIsUnmatchableNotEmpty(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	u, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "sso-only", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}

	stored, ok := s.byID[u.ID]
	if !ok {
		t.Fatal("provisioned user not found in store")
	}
	if stored.PasswordHash == "" {
		t.Fatal("OIDC-only user has an empty PasswordHash -- this is the timing side-channel the design review flagged")
	}

	// A local-login attempt against this username must go through the
	// same real Argon2id comparison path as any other account (and
	// fail, since there's no real password) -- not the fast malformed-
	// hash rejection.
	if _, err := s.Authenticate(u.Username, "whatever-password", now); err != ErrInvalidCredentials {
		t.Errorf("Authenticate against an OIDC-only user = %v, want ErrInvalidCredentials", err)
	}
}

func TestByOIDCIdentityFindsProvisionedUser(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	created, _, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}

	found, ok := s.ByOIDCIdentity("https://idp.example", "sub-1")
	if !ok {
		t.Fatal("ByOIDCIdentity did not find the provisioned user")
	}
	if found.ID != created.ID {
		t.Errorf("found ID %q, want %q", found.ID, created.ID)
	}

	if _, ok := s.ByOIDCIdentity("https://idp.example", "sub-does-not-exist"); ok {
		t.Error("ByOIDCIdentity found a user for an identity that was never provisioned")
	}
}

func TestLinkOIDCIdentityAttachesToExistingLocalUser(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	u, err := s.Register("alice", "password12345", now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	found, ok := s.ByOIDCIdentity("https://idp.example", "sub-1")
	if !ok || found.ID != u.ID {
		t.Fatalf("ByOIDCIdentity after linking = %+v, %v -- want user %q", found, ok, u.ID)
	}

	relogin, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser after linking: %v", err)
	}
	if created {
		t.Error("logging in with a linked identity must reuse the linked account, not create a new one")
	}
	if relogin.ID != u.ID {
		t.Errorf("linked login resolved to %q, want the local account %q", relogin.ID, u.ID)
	}
}

func TestLinkOIDCIdentityIsIdempotentForSameUser(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	u, err := s.Register("alice", "password12345", now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("first LinkOIDCIdentity: %v", err)
	}
	if err := s.LinkOIDCIdentity(u.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("second LinkOIDCIdentity (same user, should be idempotent): %v", err)
	}
}

func TestLinkOIDCIdentityRefusesWhenTakenByDifferentUser(t *testing.T) {
	s := newTestOIDCStore(t)
	now := time.Now()
	a, err := s.Register("alice", "password12345", now)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	b, err := s.CreateUser("bob", "password12345", RoleUser, now)
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	if err := s.LinkOIDCIdentity(a.ID, "https://idp.example", "sub-1", now); err != nil {
		t.Fatalf("LinkOIDCIdentity(a): %v", err)
	}
	if err := s.LinkOIDCIdentity(b.ID, "https://idp.example", "sub-1", now); err != ErrOIDCIdentityTaken {
		t.Errorf("LinkOIDCIdentity(b, already taken) = %v, want ErrOIDCIdentityTaken", err)
	}
}

func TestOIDCIdentityPersistsAndReloadsAcrossStoreOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now()
	created, _, err := s1.FindOrCreateOIDCUser("https://idp.example", "sub-1", "alice", now)
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	found, ok := s2.ByOIDCIdentity("https://idp.example", "sub-1")
	if !ok {
		t.Fatal("OIDC identity did not survive a Store re-open")
	}
	if found.ID != created.ID {
		t.Errorf("reloaded ID %q, want %q", found.ID, created.ID)
	}
}
