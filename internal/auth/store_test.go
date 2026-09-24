// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// failingSaveBackend lets Open/Register succeed (nothing stored yet) but
// fails every Save -- the v0.6.0 audit's R6 fix needs a backend that can
// never durably record the change a scoped call is about to make.
type failingSaveBackend struct{}

func (failingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (failingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, errors.New("backend unavailable")
}
func (failingSaveBackend) Close() error     { return nil }
func (failingSaveBackend) Describe() string { return "failing test backend" }

// saveBudgetBackend allows a fixed number of Saves and then fails every
// one after, for the R6 cases where the change under test has to land
// on a store that already holds something.
type saveBudgetBackend struct{ left int }

func (b *saveBudgetBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (b *saveBudgetBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	if b.left <= 0 {
		return 0, errors.New("backend unavailable")
	}
	b.left--
	return expect + 1, nil
}
func (b *saveBudgetBackend) Close() error     { return nil }
func (b *saveBudgetBackend) Describe() string { return "save-budget test backend" }

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

func TestCreateUserAddsAdditionalAccounts(t *testing.T) {
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

// TestCreateUserAcceptsViewer is TestCreateUserAddsAdditionalAccounts'
// #653 counterpart: RoleViewer, the new lowest tier, is a valid role for
// CreateUser, same as RoleUser.
func TestCreateUserAcceptsViewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "password123", time.Now())

	u, err := s.CreateUser("watcher", "password789", RoleViewer, time.Now())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != RoleViewer {
		t.Errorf("expected RoleViewer, got %v", u.Role)
	}
}

// TestCreateUserRejectsUnknownRole covers the branch ErrSingleAdmin
// doesn't: a role that is neither RoleAdmin (refused separately as
// ErrSingleAdmin, see transfer_test.go) nor one of the two CreateUser
// actually grants.
func TestCreateUserRejectsUnknownRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("admin", "password123", time.Now())

	if _, err := s.CreateUser("someone", "password789", Role("owner"), time.Now()); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("expected ErrInvalidRole for an unrecognized role, got %v", err)
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

// TestSetPasswordLeavesTheOldPasswordWorkingWhenPersistFails is the
// v0.6.0 audit's R6 fix: a password change that cannot be saved must not
// take effect in memory either, or a restart before the next good write
// would silently restore a credential the operator was told was already
// dead.
func TestSetPasswordLeavesTheOldPasswordWorkingWhenPersistFails(t *testing.T) {
	// Register below persists too (createLocked is R6-converted as
	// well), so the fixture needs a backend that saves once before
	// failing, not one that fails outright.
	s, err := OpenWithBackend(&saveBudgetBackend{left: 1})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	if _, err := s.Register("admin", "old-password", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := s.SetPassword("admin", "new-password", time.Now()); err == nil {
		t.Fatal("SetPassword against a backend that cannot save = nil error, want one")
	}

	if _, err := s.Authenticate("admin", "old-password", time.Now()); err != nil {
		t.Errorf("expected the old password to still work after a failed persist, got %v", err)
	}
	if _, err := s.Authenticate("admin", "new-password", time.Now()); err == nil {
		t.Error("expected the new password to not have taken effect after a failed persist")
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
	if _, err := s1.Register("admin", "password123", time.Now()); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := s2.ByUsername("admin"); !ok || u.Role != RoleAdmin {
		t.Errorf("expected the object-format file to round-trip the admin account, got %+v, %v", u, ok)
	}
}

// TestOpenLeavesAnEmptyRoleFailingClosed pins what replaced #653's
// pre-roles default: an accounts file whose account carries no "role"
// key can now only have been hand-edited, so the empty Role is loaded
// as-is and rank() denies it every gate -- not even viewer. Silently
// promoting an unassigned role to RoleUser is the wrong direction for a
// value nobody legitimately wrote.
func TestOpenLeavesAnEmptyRoleFailingClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	data := `{"users":[{"id":"u1","username":"someone","passwordHash":"$argon2id$fake","createdAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	u, ok := s.ByUsername("someone")
	if !ok {
		t.Fatal("expected the roleless account to load")
	}
	if u.Role != "" {
		t.Errorf("expected the empty on-disk role to be preserved, got %q", u.Role)
	}
	for _, min := range []Role{RoleViewer, RoleUser, RoleAdmin} {
		if u.Role.AtLeast(min) {
			t.Errorf("expected a roleless account to be denied %q, but AtLeast granted it", min)
		}
	}
}

// TestReloadIfStaleLeavesAnEmptyRoleFailingClosed is the same behaviour
// reached through the other code path that parses a storeFile --
// reloadIfStale, which a live server calls on every read once a separate
// process has touched the file. Mirrors TestReloadIfStaleSkipsNilArrayElements
// above.
func TestReloadIfStaleLeavesAnEmptyRoleFailingClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	data := `{"users":[{"id":"u1","username":"someone","passwordHash":"$argon2id$fake","createdAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // see other mtime-staleness tests' identical reasoning

	u, ok := s.ByUsername("someone") // triggers reloadIfStale
	if !ok {
		t.Fatal("expected the externally-written account to be picked up")
	}
	if u.Role != "" {
		t.Errorf("expected the empty on-disk role to be preserved, got %q", u.Role)
	}
	for _, min := range []Role{RoleViewer, RoleUser, RoleAdmin} {
		if u.Role.AtLeast(min) {
			t.Errorf("expected a roleless account to be denied %q, but AtLeast granted it", min)
		}
	}
}

// A deployment that took the removed no-auth option has "disabled": true
// in its accounts file and no accounts. The key is no longer read at
// all, so it must load as an ordinary undecided store -- setup required,
// which is the safe direction -- rather than failing to parse.
func TestAStoreLeftByTheRemovedNoAuthModeRequiresSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte(`{"disabled":true,"users":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("a store left by the no-auth mode must still load, got %v", err)
	}
	if s.Count() != 0 {
		t.Fatalf("expected no accounts, got %d", s.Count())
	}
	// Registration has to be open, or the deployment is stranded: there
	// is no account to sign in with and no way to make one.
	if _, err := s.Register("admin", "password123", time.Now()); err != nil {
		t.Fatalf("expected setup to be available on a previously-disabled store, got %v", err)
	}

	// And the marker must not survive the write -- nothing reads it, so
	// leaving it on disk is just a misleading artefact.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "disabled") {
		t.Errorf("the no-auth marker was written back out:\n%s", body)
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

func TestDeleteUserRefusesTheAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	admin, _ := s.Register("alice", "password123", time.Now())

	if _, err := s.DeleteUser(admin.ID); err != ErrCannotDeleteAdmin {
		t.Fatalf("expected ErrCannotDeleteAdmin, got %v", err)
	}
	if s.Admin() == nil {
		t.Error("the admin account is gone after a refused delete")
	}
}

func TestDeleteUserRemovesTheAccountAndFreesItsIdentifiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("alice", "password123", time.Now())
	bob, err := s.CreateUser("bob", "password456", RoleUser, time.Now())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.LinkOIDCIdentity(bob.ID, "https://idp.example", "sub-bob", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}

	deleted, err := s.DeleteUser(bob.ID)
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if deleted.PasswordHash != "" {
		t.Error("DeleteUser returned the account's password hash")
	}
	if _, ok := s.ByUsername("bob"); ok {
		t.Error("the deleted account is still resolvable by username")
	}

	// The username and the SSO identity must both be reusable, or a
	// deleted account silently blocks re-creating the person's access.
	if _, err := s.CreateUser("bob", "password789", RoleUser, time.Now()); err != nil {
		t.Errorf("expected the freed username to be reusable, got %v", err)
	}
	replacement, created, err := s.FindOrCreateOIDCUser("https://idp.example", "sub-bob", "bob2", time.Now())
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}
	if !created || replacement.ID == bob.ID {
		t.Error("the deleted account's SSO identity still maps to the old account")
	}
}

func TestDeleteUserUnknownIDReturnsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Register("alice", "password123", time.Now())

	if _, err := s.DeleteUser("no-such-id"); err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

// TestDeleteUserLeavesTheAccountInPlaceWhenPersistFails is the v0.6.0
// audit's R6 fix: a deletion that cannot be saved must not remove the
// account from memory either, or a restart before the next good write
// would bring it back while the caller has already revoked its sessions
// and tokens on the strength of a deletion that never took hold.
func TestDeleteUserLeavesTheAccountInPlaceWhenPersistFails(t *testing.T) {
	// Register and CreateUser below each persist too (createLocked is
	// R6-converted as well), so the fixture needs a backend that saves
	// twice before failing, not one that fails outright.
	s, err := OpenWithBackend(&saveBudgetBackend{left: 2})
	if err != nil {
		t.Fatalf("OpenWithBackend: %v", err)
	}
	if _, err := s.Register("alice", "password123", time.Now()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := s.CreateUser("bob", "password456", RoleUser, time.Now())
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := s.DeleteUser(bob.ID); err == nil {
		t.Fatal("DeleteUser against a backend that cannot save = nil error, want one")
	}

	got, ok := s.ByUsername("bob")
	if !ok || got.ID != bob.ID {
		t.Error("expected the account to still be there after a failed persist")
	}
}

// TestRegisterAndCreateUserReportPersistFailure is the v0.6.0 audit's
// R6 fix: an account that cannot be saved must not exist in memory
// either, or a restart before the next good write would erase it while
// the caller has already handed out a session or told an operator it
// was created.
func TestRegisterAndCreateUserReportPersistFailure(t *testing.T) {
	t.Run("Register", func(t *testing.T) {
		s, err := OpenWithBackend(failingSaveBackend{})
		if err != nil {
			t.Fatalf("OpenWithBackend: %v", err)
		}
		if _, err := s.Register("alice", "password123", time.Now()); err == nil {
			t.Fatal("Register against a backend that cannot save = nil error, want one")
		}
		if s.Count() != 0 {
			t.Errorf("Count() = %d after a failed persist, want 0", s.Count())
		}
		if _, ok := s.ByUsername("alice"); ok {
			t.Error("the account is resolvable after a failed persist")
		}
	})

	t.Run("CreateUser", func(t *testing.T) {
		// Register below persists too, so the fixture needs a backend
		// that saves once before failing, not one that fails outright.
		s, err := OpenWithBackend(&saveBudgetBackend{left: 1})
		if err != nil {
			t.Fatalf("OpenWithBackend: %v", err)
		}
		if _, err := s.Register("alice", "password123", time.Now()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := s.CreateUser("bob", "password456", RoleUser, time.Now()); err == nil {
			t.Fatal("CreateUser against a backend that cannot save = nil error, want one")
		}
		if _, ok := s.ByUsername("bob"); ok {
			t.Error("bob is resolvable after a failed persist")
		}
		if s.Count() != 1 {
			t.Errorf("Count() = %d after a failed persist, want 1 (alice only)", s.Count())
		}
	})
}

// TestRoleAtLeastStacksTheThreeTiers pins #653's ordering: admin implies
// user implies viewer, an unrecognized or empty role outranks nothing --
// not even itself as a min, which is deliberate: there is no legitimate
// call site that ever passes one as min, so what it denies doesn't
// matter, only that it never grants.
func TestRoleAtLeastStacksTheThreeTiers(t *testing.T) {
	cases := []struct {
		role Role
		min  Role
		want bool
	}{
		{RoleAdmin, RoleAdmin, true},
		{RoleAdmin, RoleUser, true},
		{RoleAdmin, RoleViewer, true},
		{RoleUser, RoleAdmin, false},
		{RoleUser, RoleUser, true},
		{RoleUser, RoleViewer, true},
		{RoleViewer, RoleUser, false},
		{RoleViewer, RoleViewer, true},
		{RoleViewer, RoleAdmin, false},
		{Role(""), RoleViewer, false},
		{Role("bogus"), RoleViewer, false},
	}
	for _, c := range cases {
		if got := c.role.AtLeast(c.min); got != c.want {
			t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", c.role, c.min, got, c.want)
		}
	}
}

// setTOTPForTest sets userID's TOTPSecret/TOTPConfirmedAt/TOTPLastCounter
// directly and persists them. SetPendingTOTPSecret and ConfirmTOTP are
// the real way in, and the tests for those use them; this exists for
// the fixture states they deliberately refuse to produce -- an
// unconfirmed secret carrying a counter, or a confirmed factor set up
// in one step -- which the tests below need as a starting point rather
// than as the thing under test.
func setTOTPForTest(t *testing.T, s *Store, userID, secret string, confirmedAt time.Time, counter uint64) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		t.Fatalf("setTOTPForTest: no such user %q", userID)
	}
	u.TOTPSecret = secret
	u.TOTPConfirmedAt = confirmedAt
	u.TOTPLastCounter = counter
	if err := s.tryPersistLocked(); err != nil {
		t.Fatalf("setTOTPForTest: persisting fixture: %v", err)
	}
}

// TestUnconfirmedTOTPSecretIsNotAnActiveFactor pins the distinction the
// design calls out explicitly: a secret generated mid-setup (e.g. to
// show a QR code) and never confirmed must not count as an active
// factor, on either the User predicate or the store-level one login and
// the admin UI use.
func TestUnconfirmedTOTPSecretIsNotAnActiveFactor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if u.HasActiveTOTP() {
		t.Error("a freshly registered user claims an active TOTP factor")
	}
	if s.HasActiveTOTP(u.ID) {
		t.Error("Store.HasActiveTOTP is true before any secret exists")
	}

	// A secret with no confirmation -- mid-setup, or an abandoned one.
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", time.Time{}, 0)
	got, ok := s.Get(u.ID)
	if !ok {
		t.Fatal("expected the user to still exist")
	}
	if got.HasActiveTOTP() {
		t.Error("an unconfirmed secret counts as an active factor")
	}
	if s.HasActiveTOTP(u.ID) {
		t.Error("Store.HasActiveTOTP is true for an unconfirmed secret")
	}

	// Confirming it is what activates it.
	now := time.Now().UTC().Truncate(time.Millisecond)
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 5)
	if !s.HasActiveTOTP(u.ID) {
		t.Error("Store.HasActiveTOTP is false for a confirmed secret")
	}
}

// TestHasActiveTOTPUnknownUserIsFalse: the predicate answers a yes/no
// gating question, not a lookup, so an unknown ID gets the same false
// answer a real account with no factor would give rather than a panic
// or a distinguishable zero value.
func TestHasActiveTOTPUnknownUserIsFalse(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.HasActiveTOTP("no-such-user") {
		t.Error("HasActiveTOTP is true for a user that does not exist")
	}
}

// TestClearTOTPRemovesEveryPart is the "disable 2FA" path: it must leave
// nothing behind that a stale recovery code could still redeem, or that
// a later re-enrollment could accidentally inherit (the old counter, in
// particular, would let a replay-guard check pass against a fresh
// secret's early codes).
func TestClearTOTPRemovesEveryPart(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 7)
	codes, err := s.GenerateRecoveryCodes(u.ID, now)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if !s.HasActiveTOTP(u.ID) {
		t.Fatal("test setup: expected an active factor before clearing it")
	}

	if err := s.ClearTOTP(u.ID); err != nil {
		t.Fatalf("ClearTOTP: %v", err)
	}

	got, ok := s.Get(u.ID)
	if !ok {
		t.Fatal("expected the user to still exist after ClearTOTP")
	}
	if got.TOTPSecret != "" {
		t.Error("ClearTOTP left the secret behind")
	}
	if !got.TOTPConfirmedAt.IsZero() {
		t.Error("ClearTOTP left the confirmation timestamp behind")
	}
	if got.TOTPLastCounter != 0 {
		t.Error("ClearTOTP left the replay counter behind")
	}
	if len(got.RecoveryCodes) != 0 {
		t.Error("ClearTOTP left recovery codes behind")
	}
	if s.HasActiveTOTP(u.ID) {
		t.Error("HasActiveTOTP is still true after ClearTOTP")
	}
	// A code from before the clear must not still work afterward.
	if ok, err := s.BurnRecoveryCode(u.ID, codes[0], now); err != nil || ok {
		t.Errorf("a pre-clear recovery code still worked after ClearTOTP: ok=%v err=%v", ok, err)
	}
}

func TestClearTOTPUnknownUserReturnsNotFound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ClearTOTP("no-such-user"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ClearTOTP on an unknown user = %v, want %v", err, ErrUserNotFound)
	}
}

// TestClearTOTPLeavesStateWhenPersistFails follows the same
// restore-on-failure contract every other credential-changing method in
// this package documents (SetPassword, IssueResetCode, ...): a clear
// that cannot be durably saved must not be reported as done, and must
// not leave the in-memory state ahead of what's on disk.
func TestClearTOTPLeavesStateWhenPersistFails(t *testing.T) {
	// Budget covers exactly Register (1) and setTOTPForTest's fixture
	// write (1); GenerateRecoveryCodes and ClearTOTP each get their own
	// budget set just before they run, below.
	budget := &saveBudgetBackend{left: 2}
	s, err := OpenWithBackend(budget)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Register("admin", "password123", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 3)

	budget.left = 1
	if _, err := s.GenerateRecoveryCodes(u.ID, now); err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	budget.left = 0

	if err := s.ClearTOTP(u.ID); err == nil {
		t.Fatal("ClearTOTP against a backend that cannot save = nil error, want one")
	}
	got, ok := s.Get(u.ID)
	if !ok {
		t.Fatal("expected the user to still exist")
	}
	if got.TOTPSecret == "" || got.TOTPConfirmedAt.IsZero() || len(got.RecoveryCodes) == 0 {
		t.Error("ClearTOTP's in-memory state changed even though the write failed")
	}
}

// TestEnrolThenConfirmActivatesTheFactor walks the two store writes the
// enrolment routes make, in order, and pins the thing that separates
// them: the secret exists after the first call but must not gate a
// sign-in until the second.
func TestEnrolThenConfirmActivatesTheFactor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetPendingTOTPSecret(u.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("SetPendingTOTPSecret: %v", err)
	}
	got, ok := s.Get(u.ID)
	if !ok {
		t.Fatal("expected the user to still exist")
	}
	if got.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("TOTPSecret = %q after enrolment, want the pending secret", got.TOTPSecret)
	}
	if s.HasActiveTOTP(u.ID) {
		t.Error("a pending, unconfirmed secret counts as an active factor -- it must not gate a sign-in")
	}

	if err := s.ConfirmTOTP(u.ID, now, 57); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	if !s.HasActiveTOTP(u.ID) {
		t.Error("the factor is not active after ConfirmTOTP")
	}
	got, _ = s.Get(u.ID)
	if !got.TOTPConfirmedAt.Equal(now) {
		t.Errorf("TOTPConfirmedAt = %v, want %v", got.TOTPConfirmedAt, now)
	}
	if got.TOTPLastCounter != 57 {
		t.Errorf("TOTPLastCounter = %d after confirming, want the counter that proved the enrolment (57)", got.TOTPLastCounter)
	}
}

// TestSetPendingTOTPSecretRefusesToReplaceAnActiveFactor covers the
// lockout ErrTOTPAlreadyActive exists to prevent: if enrolling again
// overwrote a live secret, abandoning that enrolment would leave
// HasActiveTOTP true against a secret no authenticator app holds.
func TestSetPendingTOTPSecretRefusesToReplaceAnActiveFactor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 9)

	if err := s.SetPendingTOTPSecret(u.ID, "MZXW6YTBOI======"); !errors.Is(err, ErrTOTPAlreadyActive) {
		t.Errorf("enrolling over an active factor = %v, want %v", err, ErrTOTPAlreadyActive)
	}
	got, _ := s.Get(u.ID)
	if got.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("the live secret was replaced anyway: %q", got.TOTPSecret)
	}
	if got.TOTPLastCounter != 9 {
		t.Errorf("TOTPLastCounter = %d, want the live factor's 9 left alone", got.TOTPLastCounter)
	}
}

// TestSetPendingTOTPSecretResetsTheReplayCounter covers the abandoned
// enrolment: a counter left over from an earlier secret would refuse
// the new one's early codes, which reads as an authenticator app that
// simply does not work.
func TestSetPendingTOTPSecretResetsTheReplayCounter(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	// An enrolment that got a secret and a counter but was never
	// confirmed -- so it is replaceable, unlike the test above.
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", time.Time{}, 12345)

	if err := s.SetPendingTOTPSecret(u.ID, "MZXW6YTBOI======"); err != nil {
		t.Fatalf("SetPendingTOTPSecret over an abandoned enrolment: %v", err)
	}
	got, _ := s.Get(u.ID)
	if got.TOTPSecret != "MZXW6YTBOI======" {
		t.Errorf("TOTPSecret = %q, want the new pending secret", got.TOTPSecret)
	}
	if got.TOTPLastCounter != 0 {
		t.Errorf("TOTPLastCounter = %d, want 0 -- the old secret's counter must not carry over", got.TOTPLastCounter)
	}
}

// TestConfirmTOTPNeedsSomethingPending pins both halves of
// ErrNoPendingTOTP: nothing started, and already finished.
func TestConfirmTOTPNeedsSomethingPending(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ConfirmTOTP(u.ID, now, 1); !errors.Is(err, ErrNoPendingTOTP) {
		t.Errorf("confirming with no enrolment = %v, want %v", err, ErrNoPendingTOTP)
	}

	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 40)
	if err := s.ConfirmTOTP(u.ID, now.Add(time.Hour), 1); !errors.Is(err, ErrNoPendingTOTP) {
		t.Errorf("confirming an already-confirmed factor = %v, want %v", err, ErrNoPendingTOTP)
	}
	got, _ := s.Get(u.ID)
	if got.TOTPLastCounter != 40 {
		t.Errorf("TOTPLastCounter = %d, want the confirmed factor's 40 -- a rejected confirm must not wind the guard back", got.TOTPLastCounter)
	}
	if !got.TOTPConfirmedAt.Equal(now) {
		t.Error("a rejected confirm moved TOTPConfirmedAt")
	}
}

// TestRecordTOTPCounterOnlyMovesForward is the replay guard's own
// contract. Winding the counter back is the one outcome the method
// exists to prevent, so a stale value is a no-op and not an error:
// nothing has gone wrong, another request for the same account simply
// got there first.
func TestRecordTOTPCounterOnlyMovesForward(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}
	setTOTPForTest(t, s, u.ID, "JBSWY3DPEHPK3PXP", now, 100)

	if err := s.RecordTOTPCounter(u.ID, 101); err != nil {
		t.Fatalf("RecordTOTPCounter forward: %v", err)
	}
	if got, _ := s.Get(u.ID); got.TOTPLastCounter != 101 {
		t.Errorf("TOTPLastCounter = %d after advancing, want 101", got.TOTPLastCounter)
	}

	for _, stale := range []uint64{101, 100, 0} {
		if err := s.RecordTOTPCounter(u.ID, stale); err != nil {
			t.Errorf("RecordTOTPCounter(%d) = %v, want no error -- a stale counter is a no-op", stale, err)
		}
		if got, _ := s.Get(u.ID); got.TOTPLastCounter != 101 {
			t.Fatalf("RecordTOTPCounter(%d) wound the replay guard back to %d", stale, got.TOTPLastCounter)
		}
	}
}

// TestTheEnrolmentCodeCannotAlsoSignYouIn is the seam between this
// package's two halves: totp.go verifies a code, the store remembers
// which counter was spent. It is written with real generated codes
// rather than fixture counters because the failure it guards against --
// the code someone typed to finish enrolment still working as their
// first sign-in -- only appears when both halves are wired together.
func TestTheEnrolmentCodeCannotAlsoSignYouIn(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	u, err := s.Register("admin", "password123", now)
	if err != nil {
		t.Fatal(err)
	}

	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	encoded := EncodeTOTPSecret(secret)
	if err := s.SetPendingTOTPSecret(u.ID, encoded); err != nil {
		t.Fatal(err)
	}

	// Enrolment: the user reads a code off their app and confirms.
	stored, _ := s.Get(u.ID)
	code := GenerateTOTPCode(secret, totpCounter(now, totpStep))
	matched, ok := VerifyTOTP(stored.TOTPSecret, code, now, stored.TOTPLastCounter)
	if !ok {
		t.Fatal("the enrolment code did not verify against the pending secret")
	}
	if err := s.ConfirmTOTP(u.ID, now, matched); err != nil {
		t.Fatal(err)
	}

	// Sign-in, moments later, with the same code still on screen.
	stored, _ = s.Get(u.ID)
	if _, ok := VerifyTOTP(stored.TOTPSecret, code, now, stored.TOTPLastCounter); ok {
		t.Error("the code used to enrol was accepted again at sign-in")
	}

	// The next window's code still works, and spending it advances the
	// guard -- the point of the counter is to move on, not to wedge.
	later := now.Add(totpStep)
	next := GenerateTOTPCode(secret, totpCounter(later, totpStep))
	matched, ok = VerifyTOTP(stored.TOTPSecret, next, later, stored.TOTPLastCounter)
	if !ok {
		t.Fatal("the next window's code was refused, so the account is wedged")
	}
	if err := s.RecordTOTPCounter(u.ID, matched); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.Get(u.ID)
	if _, ok := VerifyTOTP(stored.TOTPSecret, next, later, stored.TOTPLastCounter); ok {
		t.Error("the sign-in code was accepted a second time")
	}
}

// TestTOTPWritesLeaveStateWhenPersistFails holds the three new writes
// to the same restore-on-failure contract as ClearTOTP above: a change
// that cannot be durably saved is not reported as done, and does not
// leave memory ahead of disk.
func TestTOTPWritesLeaveStateWhenPersistFails(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	newStore := func(t *testing.T) (*Store, *saveBudgetBackend, string) {
		t.Helper()
		// Budget covers Register (1); each case sets its own budget
		// before the write it is testing.
		budget := &saveBudgetBackend{left: 1}
		s, err := OpenWithBackend(budget)
		if err != nil {
			t.Fatal(err)
		}
		u, err := s.Register("admin", "password123", now)
		if err != nil {
			t.Fatal(err)
		}
		return s, budget, u.ID
	}

	t.Run("SetPendingTOTPSecret", func(t *testing.T) {
		s, budget, id := newStore(t)
		budget.left = 0
		if err := s.SetPendingTOTPSecret(id, "JBSWY3DPEHPK3PXP"); err == nil {
			t.Fatal("SetPendingTOTPSecret against a backend that cannot save = nil error, want one")
		}
		if got, _ := s.Get(id); got.TOTPSecret != "" {
			t.Errorf("TOTPSecret = %q in memory even though the write failed", got.TOTPSecret)
		}
	})

	t.Run("ConfirmTOTP", func(t *testing.T) {
		s, budget, id := newStore(t)
		budget.left = 1
		if err := s.SetPendingTOTPSecret(id, "JBSWY3DPEHPK3PXP"); err != nil {
			t.Fatal(err)
		}
		budget.left = 0
		if err := s.ConfirmTOTP(id, now, 5); err == nil {
			t.Fatal("ConfirmTOTP against a backend that cannot save = nil error, want one")
		}
		if s.HasActiveTOTP(id) {
			t.Error("the factor reads as active even though the confirmation could not be saved")
		}
		if got, _ := s.Get(id); got.TOTPLastCounter != 0 {
			t.Errorf("TOTPLastCounter = %d after a failed confirm, want 0", got.TOTPLastCounter)
		}
	})

	t.Run("RecordTOTPCounter", func(t *testing.T) {
		s, budget, id := newStore(t)
		budget.left = 1
		setTOTPForTest(t, s, id, "JBSWY3DPEHPK3PXP", now, 30)
		budget.left = 0
		if err := s.RecordTOTPCounter(id, 31); err == nil {
			t.Fatal("RecordTOTPCounter against a backend that cannot save = nil error, want one")
		}
		if got, _ := s.Get(id); got.TOTPLastCounter != 30 {
			t.Errorf("TOTPLastCounter = %d after a failed write, want the stored 30", got.TOTPLastCounter)
		}
	})
}
