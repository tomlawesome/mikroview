package auth

import (
	"path/filepath"
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
// the cross-process scenario the CLI recovery tool (`-reset-password`)
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
