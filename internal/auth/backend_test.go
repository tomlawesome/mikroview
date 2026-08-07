// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// The promise of issue #131 is that moving accounts onto Postgres
// changes where the bytes live and nothing about how the store behaves.
// These run the same assertions against both backends.
//
// The Postgres side skips itself when MIKROVIEW_TEST_POSTGRES is unset.
func eachAuthBackend(t *testing.T, run func(t *testing.T, open func() *Store)) {
	t.Helper()

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "users.json")
		run(t, func() *Store {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			return s
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
		if dsn == "" {
			t.Skip("MIKROVIEW_TEST_POSTGRES not set")
		}
		pool, err := persist.OpenPool(t.Context(), dsn)
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		t.Cleanup(pool.Close)
		if err := pool.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		// A store name unique to this test, so tests don't collide in a
		// shared database.
		name := "authtest_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
		b := persist.NewPostgresBackend(pool, name)

		// Reset to empty, unconditionally.
		//
		// This used to seed only when the store was absent, which made
		// the whole set pass exactly once against a given database and
		// fail on every run after: the second run inherited the first's
		// accounts ("registration is closed"), and the corrupt-document
		// test inherited the corruption it had deliberately written.
		//
		// It passed in CI regardless, because CI starts a fresh
		// database each run -- which is precisely what made it a trap
		// rather than a visible bug. A test that only passes the first
		// time is worse than no test: it teaches people to ignore it.
		empty := []byte(`{"disabled":false,"users":[]}`)
		snap, err := b.Load(t.Context())
		if err != nil {
			t.Fatalf("reading the store before reset: %v", err)
		}
		if _, err := b.Save(t.Context(), empty, snap.Version); err != nil {
			t.Fatalf("resetting the store: %v", err)
		}
		t.Cleanup(func() {
			_ = b.Close()
		})
		run(t, func() *Store {
			s, err := OpenWithBackend(b)
			if err != nil {
				t.Fatalf("OpenWithBackend: %v", err)
			}
			return s
		})
	})
}

func TestBackendRegisterAndAuthenticate(t *testing.T) {
	eachAuthBackend(t, func(t *testing.T, open func() *Store) {
		s := open()
		if _, err := s.Register("alice", "password123", time.Now()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := s.Authenticate("alice", "password123", time.Now()); err != nil {
			t.Errorf("Authenticate: %v", err)
		}
		if _, err := s.Authenticate("alice", "wrong", time.Now()); err != ErrInvalidCredentials {
			t.Errorf("wrong password: got %v", err)
		}
	})
}

// The whole point of persistence: a second Store opened against the same
// backend sees what the first wrote. This is also the cross-process case
// the CLI commands rely on.
func TestBackendSurvivesReopen(t *testing.T) {
	eachAuthBackend(t, func(t *testing.T, open func() *Store) {
		first := open()
		if _, err := first.Register("alice", "password123", time.Now()); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if _, err := first.CreateUser("bob", "password456", RoleUser, time.Now()); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}

		second := open()
		if second.Count() != 2 {
			t.Fatalf("reopened store has %d accounts, want 2", second.Count())
		}
		admin := second.Admin()
		if admin == nil || admin.Username != "alice" {
			t.Errorf("admin did not survive: %+v", admin)
		}
		if _, err := second.Authenticate("bob", "password456", time.Now()); err != nil {
			t.Errorf("bob's password did not survive: %v", err)
		}
	})
}

// A second Store standing in for a CLI command: it writes, and the
// first (standing in for the running server) must pick the change up
// without being restarted.
func TestBackendPicksUpAnotherProcessesWrite(t *testing.T) {
	eachAuthBackend(t, func(t *testing.T, open func() *Store) {
		server := open()
		if _, err := server.Register("alice", "password123", time.Now()); err != nil {
			t.Fatalf("Register: %v", err)
		}

		cli := open()
		if err := cli.SetPassword("alice", "newpassword999", time.Now()); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}

		// The running server must see it -- Authenticate reloads first.
		if _, err := server.Authenticate("alice", "newpassword999", time.Now()); err != nil {
			t.Errorf("the running store did not pick up the other process's change: %v", err)
		}
		if _, err := server.Authenticate("alice", "password123", time.Now()); err == nil {
			t.Error("the old password still works on the running store")
		}
	})
}

// Disable/EnableSetup cross the same process boundary -- that is exactly
// what `-enable-auth-setup` does to a live server.
func TestBackendDisabledStateCrossesProcesses(t *testing.T) {
	eachAuthBackend(t, func(t *testing.T, open func() *Store) {
		server := open()
		if server.Disabled() {
			t.Fatal("a fresh store reports disabled")
		}

		cli := open()
		if err := cli.Disable(); err != nil {
			t.Fatalf("Disable: %v", err)
		}
		if !server.Disabled() {
			t.Error("the running store did not see auth being disabled")
		}

		if err := cli.EnableSetup(); err != nil {
			t.Fatalf("EnableSetup: %v", err)
		}
		if server.Disabled() {
			t.Error("the running store did not see setup being re-enabled")
		}
	})
}

// An unreadable document must not read as an absent one -- that turns a
// corrupted accounts store into a fresh install, silently reopening
// registration. main.go refuses to start on this error.
func TestBackendCorruptDocumentIsAnError(t *testing.T) {
	eachAuthBackend(t, func(t *testing.T, open func() *Store) {
		s := open()
		if _, err := s.Register("alice", "password123", time.Now()); err != nil {
			t.Fatalf("Register: %v", err)
		}

		// Corrupt it through the backend, the way a bad disk or a
		// truncated write would.
		snap, err := s.backend.Load(context.Background())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if _, err := s.backend.Save(context.Background(), []byte("{not json"), snap.Version); err != nil {
			t.Fatalf("corrupting: %v", err)
		}

		reopened, err := OpenWithBackend(s.backend)
		if err == nil {
			t.Error("a corrupt accounts document opened cleanly -- it would present as a fresh install")
		}
		if reopened != nil && reopened.Count() != 0 {
			t.Errorf("expected an empty store alongside the error, got %d accounts", reopened.Count())
		}
	})
}
