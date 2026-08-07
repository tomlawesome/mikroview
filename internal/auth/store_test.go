// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenEmptyPathIsUsableButNotPersisted(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	if s.Persisted() {
		t.Error("expected an empty path to leave the store unpersisted")
	}
	if s.Count() != 0 {
		t.Errorf("expected 0 users, got %d", s.Count())
	}
}

func TestRegisterRefusesWhenNotPersisted(t *testing.T) {
	s, _ := Open("")
	if _, err := s.Register("admin", "password123", time.Now()); err != ErrNotPersisted {
		t.Errorf("expected ErrNotPersisted, got %v", err)
	}
}

func TestRegisterCreatesAdminAndClosesAfterFirstUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Errorf("expected the first user to be RoleAdmin, got %v", u.Role)
	}
	if s.Count() != 1 {
		t.Errorf("expected 1 user, got %d", s.Count())
	}

	if _, err := s.Register("second", "password456", time.Now()); err != ErrRegistrationClosed {
		t.Errorf("expected ErrRegistrationClosed for a second Register call, got %v", err)
	}
}

func TestPasswordTooShortRejectedOnRegisterCreateAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)

	if _, err := s.Register("admin", "short1", time.Now()); err != ErrPasswordTooShort {
		t.Errorf("Register: expected ErrPasswordTooShort for a %d-char password, got %v", len("short1"), err)
	}
	if s.Count() != 0 {
		t.Fatalf("expected no account to have been created, got %d", s.Count())
	}

	if _, err := s.Register("admin", "password123", time.Now()); err != nil {
		t.Fatalf("Register with a long-enough password should succeed, got %v", err)
	}
	if _, err := s.CreateUser("second", "tiny", RoleUser, time.Now()); err != ErrPasswordTooShort {
		t.Errorf("CreateUser: expected ErrPasswordTooShort, got %v", err)
	}
	if err := s.SetPassword("admin", "abc", time.Now()); err != ErrPasswordTooShort {
		t.Errorf("SetPassword: expected ErrPasswordTooShort, got %v", err)
	}
}

func TestCreateUserAllowsAdditionalAccountsAtAnyRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "password123", time.Now())

	u, err := s.CreateUser("viewer", "password789", RoleUser, time.Now())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", u.Role)
	}
	if s.Count() != 2 {
		t.Errorf("expected 2 users, got %d", s.Count())
	}
}

func TestCreateUserRejectsDuplicateUsernameCaseInsensitive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("Admin", "password123", time.Now())

	if _, err := s.CreateUser("admin", "different", RoleUser, time.Now()); err != ErrUsernameTaken {
		t.Errorf("expected ErrUsernameTaken for a case-insensitive duplicate, got %v", err)
	}
}

func TestAuthenticateSucceedsAndFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "correct-password", time.Now())

	if _, err := s.Authenticate("admin", "correct-password", time.Now()); err != nil {
		t.Errorf("expected valid credentials to succeed, got %v", err)
	}
	if _, err := s.Authenticate("admin", "wrong-password", time.Now()); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for a wrong password, got %v", err)
	}
	if _, err := s.Authenticate("nobody", "anything", time.Now()); err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for an unknown username, got %v", err)
	}
}

func TestAuthenticateUpdatesLastLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "password123", time.Now())

	now := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	u, err := s.Authenticate("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	if !u.LastLogin.Equal(now) {
		t.Errorf("LastLogin = %v, want %v", u.LastLogin, now)
	}
}

func TestSetPasswordChangesCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "old-password", time.Now())

	if err := s.SetPassword("admin", "new-password", time.Now()); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, err := s.Authenticate("admin", "old-password", time.Now()); err == nil {
		t.Error("expected the old password to stop working")
	}
	if _, err := s.Authenticate("admin", "new-password", time.Now()); err != nil {
		t.Errorf("expected the new password to work, got %v", err)
	}
}

// An SSO-provisioned account starts with HasLocalPassword explicitly
// false. If it is ever given a real password, that flag has to move with
// it -- otherwise the account holds a working mikroview password while
// still reporting itself SSO-only, and -recover-admin-account refuses to
// recover an account it actually could.
//
// Nothing reaches this state today (recovery refuses SSO-only accounts
// up front), so this guards the invariant ahead of account linking
// rather than a live bug.
func TestSetPasswordMarksTheAccountAsHavingALocalPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)

	u, created, err := s.FindOrCreateOIDCUser("https://idp.example", "subject-1", "sso-user", time.Now())
	if err != nil || !created {
		t.Fatalf("FindOrCreateOIDCUser: created=%v err=%v", created, err)
	}
	if u.LocalPassword() {
		t.Fatal("an SSO-provisioned account reports a local password before the test even starts")
	}

	if err := s.SetPassword(u.Username, "new-password", time.Now()); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	got, _ := s.ByUsername(u.Username)
	if !got.LocalPassword() {
		t.Error("an account that was just given a password reports no local password")
	}
}

func TestSetPasswordUnknownUserReturnsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	if err := s.SetPassword("nobody", "irrelevant", time.Now()); err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestListNeverIncludesPasswordHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "password123", time.Now())
	s.CreateUser("viewer", "password456", RoleUser, time.Now())

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
	for _, u := range list {
		if u.PasswordHash != "" {
			t.Errorf("expected List() to never include a password hash, got one for %s", u.Username)
		}
	}
	if list[0].Username != "admin" || list[1].Username != "viewer" {
		t.Errorf("expected alphabetical order, got %s, %s", list[0].Username, list[1].Username)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "users.json")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s1.Register("admin", "password123", now)
	s1.CreateUser("viewer", "password456", RoleUser, now)

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-opening the persisted store failed: %v", err)
	}
	if s2.Count() != 2 {
		t.Fatalf("expected 2 persisted users, got %d", s2.Count())
	}
	if _, err := s2.Authenticate("admin", "password123", time.Now()); err != nil {
		t.Errorf("expected the persisted admin's password to still verify, got %v", err)
	}
}

func TestGetReturnsCopyNotSharedPointer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	registered, _ := s.Register("admin", "password123", time.Now())

	got, ok := s.Get(registered.ID)
	if !ok {
		t.Fatal("expected Get to find the registered user")
	}
	got.Username = "tampered"

	got2, _ := s.Get(registered.ID)
	if got2.Username == "tampered" {
		t.Error("expected Get to return an independent copy, not a shared pointer into the store")
	}
}

// TestSeparateProcessPasswordResetIsPickedUpByRunningStore reproduces
// the cross-process scenario the CLI recovery tool (`-recover-admin-account`)
// depends on: two independent Store instances (standing in for two
// separate process invocations) opened against the same file. A change
// made through one must be visible through the other on its next read,
// without requiring a restart -- otherwise the recovery tool would
// silently have no effect on an already-running server.
func TestSeparateProcessPasswordResetIsPickedUpByRunningStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	serverStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	serverStore.Register("admin", "old-password", time.Now())

	if _, err := serverStore.Authenticate("admin", "old-password", time.Now()); err != nil {
		t.Fatalf("expected the old password to work before the reset: %v", err)
	}

	// A second, independent Store against the same file -- standing in
	// for the CLI tool's own separate process.
	cliStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Filesystem mtime resolution can be coarse (1s on some systems) --
	// a short real sleep guarantees the reset's write gets a strictly
	// later mtime than serverStore's initial load, same as a real CLI
	// invocation running at least that long after the server started.
	time.Sleep(10 * time.Millisecond)
	if err := cliStore.SetPassword("admin", "new-password", time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := serverStore.Authenticate("admin", "new-password", time.Now()); err != nil {
		t.Errorf("expected the running store to pick up the externally-reset password, got %v", err)
	}
	if _, err := serverStore.Authenticate("admin", "old-password", time.Now()); err == nil {
		t.Error("expected the old password to stop working after the external reset")
	}
}

func TestDisableOnlyWhenNoAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)

	if s.Disabled() {
		t.Fatal("expected a fresh store to not be disabled")
	}
	if err := s.Disable(); err != nil {
		t.Fatalf("expected Disable to succeed with zero accounts, got %v", err)
	}
	if !s.Disabled() {
		t.Error("expected Disabled() to report true after Disable()")
	}

	// Once an account exists, Disable must refuse -- disabling auth out
	// from under an existing account isn't this method's job.
	s2, _ := Open(filepath.Join(t.TempDir(), "users2.json"))
	s2.Register("admin", "password123", time.Now())
	if err := s2.Disable(); err != ErrRegistrationClosed {
		t.Errorf("expected Disable to refuse once an account exists, got %v", err)
	}
}

func TestDisableRefusesWhenNotPersisted(t *testing.T) {
	s, _ := Open("")
	if err := s.Disable(); err != ErrNotPersisted {
		t.Errorf("expected ErrNotPersisted for an unconfigured store, got %v", err)
	}
}

func TestEnableSetupClearsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	if err := s.Disable(); err != nil {
		t.Fatal(err)
	}
	if !s.Disabled() {
		t.Fatal("expected the store to be disabled before EnableSetup")
	}

	if err := s.EnableSetup(); err != nil {
		t.Fatal(err)
	}
	if s.Disabled() {
		t.Error("expected EnableSetup to clear the disabled flag")
	}

	// The setup form should be usable again -- Register must not be
	// refused by anything left over from the prior Disable.
	if _, err := s.Register("admin", "password123", time.Now()); err != nil {
		t.Errorf("expected Register to succeed after EnableSetup, got %v", err)
	}
}

// TestSeparateProcessEnableSetupIsPickedUpByRunningStore mirrors
// TestSeparateProcessPasswordResetIsPickedUpByRunningStore, but for
// -enable-auth-setup -- the CLI tool's whole point is taking effect on
// an already-running server without a restart.
func TestSeparateProcessEnableSetupIsPickedUpByRunningStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	serverStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverStore.Disable(); err != nil {
		t.Fatal(err)
	}
	if !serverStore.Disabled() {
		t.Fatal("expected the server's store to be disabled")
	}

	cliStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // see the password-reset test's identical reasoning
	if err := cliStore.EnableSetup(); err != nil {
		t.Fatal(err)
	}

	if serverStore.Disabled() {
		t.Error("expected the running server's store to pick up the externally-cleared disabled flag")
	}
}

func TestOpenReadsLegacyBareArrayFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	legacy := `[{"id":"u1","username":"admin","passwordHash":"$argon2id$fake","role":"admin","createdAt":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("expected a legacy bare-array file to still load, got %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("expected 1 user loaded from the legacy format, got %d", s.Count())
	}
	if s.Disabled() {
		t.Error("expected a legacy file (no disabled key at all) to load as Disabled: false")
	}
	if u, ok := s.ByUsername("admin"); !ok || u.ID != "u1" {
		t.Errorf("expected the legacy user to be readable by username, got %+v, %v", u, ok)
	}
}

// A JSON array containing null is syntactically valid, so it unmarshals
// without error into a slice with a nil *User element -- before the
// fix, the next line (indexing u.ID) paniced, and since nothing in
// this codebase recovers a panic from every goroutine (see
// internal/logging.Recover), that meant crashing the entire process on
// mikroview startup, not a graceful degrade.
func TestOpenSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	data := `{"disabled":false,"users":[null,{"id":"u1","username":"admin","passwordHash":"$argon2id$fake","role":"admin","createdAt":"2026-01-01T00:00:00Z"},null]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path) // must not panic
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	if s.Count() != 1 {
		t.Fatalf("expected the one real user to survive, got %d", s.Count())
	}
	if u, ok := s.ByUsername("admin"); !ok || u.ID != "u1" {
		t.Errorf("expected the real user's data to be intact, got %+v, %v", u, ok)
	}
}

// Same bug, reached through the other code path that parses a
// storeFile: reloadIfStale, which a live server calls on every read
// once a separate process (a CLI recovery tool) has touched the file.
func TestReloadIfStaleSkipsNilArrayElements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	data := `{"disabled":false,"users":[null,{"id":"u1","username":"admin","passwordHash":"$argon2id$fake","role":"admin","createdAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // see other mtime-staleness tests' identical reasoning

	if u, ok := s.ByUsername("admin"); !ok || u.ID != "u1" { // triggers reloadIfStale; must not panic
		t.Errorf("expected the externally-written user to be picked up, got %+v, %v", u, ok)
	}
}

func TestOpenReadsNewObjectFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	// Round-trip through the store's own writer -- the true contract is
	// "whatever Store.persistLocked writes, Store.Open can read back."
	s1, _ := Open(path)
	if err := s1.Disable(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Disabled() {
		t.Error("expected the object-format file to round-trip Disabled: true")
	}
}

// TestConcurrentRegisterCreatesExactlyOneAdmin is the regression test
// for the first-run registration race. Before the fix, Register checked
// Count()/Disabled() outside the lock and createLocked only re-checked
// for a username collision under it -- so N concurrent registrations
// with distinct usernames all succeeded, every one of them landing
// RoleAdmin. Measured 8/8 succeeding, reproducibly.
//
// The window was wide, not theoretical: HashPassword (Argon2id, ~100ms
// by design) runs before the lock is taken, so a real attacker racing
// the operator through the unauthenticated first-run screen had a
// comfortable margin, not a microsecond one.
func TestConcurrentRegisterCreatesExactlyOneAdmin(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	names := []string{"alice", "bob", "carol", "dave", "eve", "frank", "grace", "heidi"}
	results := make([]error, len(names))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			<-start
			_, results[i] = s.Register(name, "correct horse battery staple", time.Now())
		}(i, name)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for i, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrRegistrationClosed):
			// expected for every loser of the race
		default:
			t.Errorf("Register(%q) failed with an unexpected error: %v", names[i], err)
		}
	}
	if succeeded != 1 {
		t.Errorf("%d concurrent Register calls succeeded, want exactly 1", succeeded)
	}
	if got := s.Count(); got != 1 {
		t.Errorf("store holds %d accounts after the race, want exactly 1", got)
	}
}

// TestConcurrentRegisterAndDisableAreMutuallyExclusive is the
// regression test for the worse half of the same root cause: Register
// and Disable could both succeed, leaving an admin account in a store
// that also reports Disabled() == true. internal/api's requireAuth
// checks Disabled() first, so that state serves every request with no
// authentication at all while an admin account quietly exists -- the
// operator's own registration returns success, giving them no reason to
// suspect the deployment is wide open. Measured at 79/200 attempts.
//
// Exactly one of the two must win, whichever it is: either auth is on
// with one admin, or it's deliberately disabled with no accounts.
//
// 40 attempts rather than the 200 used to characterize the bug: each
// one pays for a real Argon2id hash (~100ms, deliberately), so the
// loop is kept to what still reliably catches a regression without
// dominating the suite's runtime. At the pre-fix ~40% interleaving
// rate, 40 attempts miss a reintroduced race with probability well
// under one in a million.
func TestConcurrentRegisterAndDisableAreMutuallyExclusive(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		s, err := Open(filepath.Join(t.TempDir(), "users.json"))
		if err != nil {
			t.Fatal(err)
		}

		var regErr, disErr error
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, regErr = s.Register("operator", "correct horse battery staple", time.Now())
		}()
		go func() {
			defer wg.Done()
			<-start
			disErr = s.Disable()
		}()
		close(start)
		wg.Wait()

		if regErr == nil && disErr == nil {
			t.Fatalf("attempt %d: Register and Disable both succeeded -- store now has %d account(s) with Disabled()=%v, "+
				"which requireAuth serves as fully open despite an admin existing", attempt, s.Count(), s.Disabled())
		}
		if regErr != nil && disErr != nil {
			t.Fatalf("attempt %d: neither Register nor Disable succeeded (register: %v, disable: %v) -- "+
				"one of them must always win", attempt, regErr, disErr)
		}
		// Whichever won, the resulting state must be self-consistent.
		if s.Disabled() && s.Count() > 0 {
			t.Fatalf("attempt %d: inconsistent end state -- Disabled()=true with %d account(s)", attempt, s.Count())
		}
	}
}
