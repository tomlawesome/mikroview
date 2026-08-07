// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// eachRecoveryBackend runs a contract against both places the digests can
// live. The Postgres leg is skipped without MIKROVIEW_TEST_POSTGRES; CI
// sets it (see .github/workflows/ci.yml's postgres job).
func eachRecoveryBackend(t *testing.T, run func(t *testing.T, open func() *RecoveryStore, pepperPath string)) {
	t.Helper()

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		keyPath := filepath.Join(dir, "recovery.json")
		pepperPath := filepath.Join(dir, "pepper")
		run(t, func() *RecoveryStore {
			s, err := OpenRecovery(keyPath, pepperPath)
			if err != nil {
				t.Fatalf("OpenRecovery: %v", err)
			}
			return s
		}, pepperPath)
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
		if dsn == "" {
			t.Skip("MIKROVIEW_TEST_POSTGRES not set")
		}
		pool, err := persist.OpenPool(context.Background(), dsn)
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		t.Cleanup(pool.Close)
		if err := pool.Migrate(context.Background()); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		pepperPath := filepath.Join(t.TempDir(), "pepper")
		run(t, func() *RecoveryStore {
			s, err := OpenRecoveryWithBackend(persist.NewPostgresBackend(pool, "recovery_keys_test"), pepperPath)
			if err != nil {
				t.Fatalf("OpenRecoveryWithBackend: %v", err)
			}
			return s
		}, pepperPath)
	})
}

// A key generated against one backend has to verify against a store
// reopened on the same backend -- that is the whole point of persisting
// the digests, and it is what a locked-out admin depends on.
func TestRecoveryKeysSurviveAReopenOnEitherBackend(t *testing.T) {
	eachRecoveryBackend(t, func(t *testing.T, open func() *RecoveryStore, _ string) {
		s := open()
		if s.Exists() {
			// Postgres is shared between subtests in a run; start clean.
			if _, err := s.Redeem("PURGE"); err == nil {
				t.Fatal("unexpected")
			}
		}
		keys, err := s.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if err := s.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		reopened := open()
		if _, err := reopened.Redeem(keys[0]); err != nil {
			t.Errorf("a committed key did not verify after reopening: %v", err)
		}
	})
}

// The pepper must never follow the digests into the backend.
//
// This is the entire value of the split: a database dump yields digests
// that cannot be tested against anything, and the mikroview host yields a
// pepper with no digests to apply it to. Writing the pepper alongside the
// digests would make it decorative -- whoever reads one reads both.
func TestThePepperNeverReachesTheDigestBackend(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "recovery.json")
	pepperPath := filepath.Join(dir, "pepper")

	s, err := OpenRecovery(keyPath, pepperPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Generate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	pepper, err := os.ReadFile(pepperPath)
	if err != nil {
		t.Fatalf("pepper was not written to its own file: %v", err)
	}
	digests, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(digests), strings.TrimSpace(string(pepper))) {
		t.Error("the pepper was written into the digest store; a single stolen copy would then carry both halves")
	}
	// And it is a local file in the first place, not something the
	// backend was asked to hold.
	if fi, err := os.Stat(pepperPath); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("pepper file missing or too permissive: %v", err)
	}
}

// Without a pepper there is nothing to verify against, so opening must
// fail rather than quietly producing a store that rejects every key --
// which would look identical to "your keys are wrong" at the exact moment
// someone is locked out.
func TestOpeningWithoutAPepperPathFails(t *testing.T) {
	if _, err := OpenRecoveryWithBackend(persist.NewFileBackend(filepath.Join(t.TempDir(), "r.json")), ""); err == nil {
		t.Error("opened a recovery store with no pepper configured")
	}
}
