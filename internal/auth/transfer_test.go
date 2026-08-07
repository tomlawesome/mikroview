// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func storeWithAdminAndUser(t *testing.T) (*Store, *User, *User) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.Register("admin", "correct-horse-battery-staple", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser("second", "correct-horse-battery-staple", RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s, admin, user
}

func TestCreateUserCannotMintASecondAdmin(t *testing.T) {
	s, _, _ := storeWithAdminAndUser(t)
	if _, err := s.CreateUser("other", "correct-horse-battery-staple", RoleAdmin, time.Now()); !errors.Is(err, ErrSingleAdmin) {
		t.Errorf("a second admin was created (err=%v) -- the single-admin invariant is the whole model", err)
	}
}

func TestTransferAdminMovesTheRole(t *testing.T) {
	s, admin, user := storeWithAdminAndUser(t)

	from, to, err := s.TransferAdmin("second", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if from.ID != admin.ID || to.ID != user.ID {
		t.Errorf("transfer reported the wrong accounts: from=%s to=%s", from.Username, to.Username)
	}

	got := s.Admin()
	if got == nil || got.Username != "second" {
		t.Fatalf("admin is %v, want second", got)
	}
	old, ok := s.ByUsername("admin")
	if !ok {
		t.Fatal("previous admin vanished")
	}
	if old.Role != RoleUser {
		t.Errorf("previous admin still has role %q -- there are now two admins", old.Role)
	}
}

// TestExactlyOneAdminUnderConcurrentTransfers is the TOCTOU case. Two
// simultaneous transfers must not produce zero admins or two.
func TestExactlyOneAdminUnderConcurrentTransfers(t *testing.T) {
	s, _, _ := storeWithAdminAndUser(t)
	if _, err := s.CreateUser("third", "correct-horse-battery-staple", RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, target := range []string{"second", "third", "second", "third"} {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			<-start
			_, _, _ = s.TransferAdmin(u, time.Now())
		}(target)
	}
	close(start)
	wg.Wait()

	admins := 0
	for _, u := range s.List() {
		if u.Role == RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Errorf("after concurrent transfers there are %d admins, want exactly 1", admins)
	}
}

func TestTransferRejections(t *testing.T) {
	s, _, _ := storeWithAdminAndUser(t)

	if _, _, err := s.TransferAdmin("admin", time.Now()); !errors.Is(err, ErrTransferToSelf) {
		t.Errorf("transfer to the current admin returned %v, want ErrTransferToSelf", err)
	}
	if _, _, err := s.TransferAdmin("nobody", time.Now()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("transfer to an unknown account returned %v, want ErrUserNotFound", err)
	}

	empty, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := empty.TransferAdmin("anyone", time.Now()); !errors.Is(err, ErrNoAdmin) {
		t.Errorf("transfer with no admin returned %v, want ErrNoAdmin", err)
	}
}

func TestTransferSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register("admin", "correct-horse-battery-staple", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("second", "correct-horse-battery-staple", RoleUser, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TransferAdmin("second", time.Now()); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Admin()
	if got == nil || got.Username != "second" {
		t.Errorf("after reload the admin is %v, want second -- the transfer wasn't persisted", got)
	}
}
