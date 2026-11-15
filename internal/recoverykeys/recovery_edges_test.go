// SPDX-License-Identifier: AGPL-3.0-only

package recoverykeys

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// A store with nowhere to keep its digests refuses to hand out keys that
// would not survive a restart, and has nothing to commit.
func TestStoreWithoutAPathRefusesToGenerateOrCommit(t *testing.T) {
	s, err := OpenRecovery("", filepath.Join(t.TempDir(), "pepper"))
	if err != nil {
		t.Fatal(err)
	}
	if keys, err := s.Generate(); !errors.Is(err, ErrNotPersisted) || keys != nil {
		t.Errorf("Generate = %v, %v; want ErrNotPersisted and no keys", keys, err)
	}
	if err := s.Commit(); err == nil {
		t.Error("Commit with nothing pending succeeded")
	}
	if s.Exists() {
		t.Error("a store with no backend reports recovery keys")
	}
}

func TestOpenRecoveryFailsWhereTheKeyDirectoryCannotBeMade(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRecovery(filepath.Join(blocker, "sub", "recovery.json"), filepath.Join(t.TempDir(), "pepper")); err == nil {
		t.Error("OpenRecovery succeeded under a path component that is a file")
	}
}

// The pepper is created once with owner-only permission and then read
// back unchanged; a damaged pepper fails closed rather than being
// replaced (a new one would orphan every stored digest).
func TestPepperIsCreatedPrivateReusedAndNeverReplacedWhenDamaged(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "recovery.json")
	pepperPath := filepath.Join(dir, "nested", "pepper")

	if _, err := OpenRecovery(keyPath, pepperPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pepperPath)
	if err != nil {
		t.Fatalf("pepper not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("pepper mode = %v, want 0600", info.Mode().Perm())
	}
	first, _ := os.ReadFile(pepperPath)
	if _, err := OpenRecovery(keyPath, pepperPath); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(pepperPath); string(again) != string(first) {
		t.Error("a second open replaced the pepper")
	}

	for name, damage := range map[string]string{"not hex": "zz-not-hex", "too short": "abcd"} {
		if err := os.WriteFile(pepperPath, []byte(damage), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenRecovery(keyPath, pepperPath); !errors.Is(err, ErrInvalidRecoveryKey) {
			t.Errorf("%s: OpenRecovery = %v, want ErrInvalidRecoveryKey", name, err)
		}
		if got, _ := os.ReadFile(pepperPath); string(got) != damage {
			t.Errorf("%s: the damaged pepper was overwritten with %q", name, got)
		}
	}
}

// failSave lets the real file backend load, and fails its saves on demand.
type failSave struct {
	persist.Backend
	fail bool
}

func (f *failSave) Save(ctx context.Context, p []byte, expect int64) (int64, error) {
	if f.fail {
		return 0, errors.New("disk full")
	}
	return f.Backend.Save(ctx, p, expect)
}

// A rotation whose save fails must leave the previous keys valid -- not
// neither set -- and stay pending so the operator can try again.
func TestFailedCommitKeepsThePreviousKeysValidAndRetryable(t *testing.T) {
	dir := t.TempDir()
	pepper := filepath.Join(dir, "pepper")
	fb := &failSave{Backend: persist.NewFileBackend(filepath.Join(dir, "recovery.json"))}
	s, err := OpenRecoveryWithBackend(fb, pepper)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	fresh, err := s.Redeem(old[0])
	if err != nil {
		t.Fatal(err)
	}
	fb.fail = true
	if err := s.Commit(); err == nil {
		t.Fatal("Commit succeeded although the save failed")
	}
	if _, err := s.Redeem(old[1]); err != nil {
		t.Errorf("the previous keys stopped working after a failed rotation: %v", err)
	}
	if _, err := s.Redeem(fresh[0]); !errors.Is(err, ErrInvalidRecoveryKey) {
		t.Errorf("a key from the uncommitted set verified: %v", err)
	}

	// Retry once the disk is back.
	fb.fail = false
	if _, err := s.Redeem(old[2]); err != nil { // regenerates the pending set
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("retry Commit: %v", err)
	}
	if _, err := s.Redeem(old[0]); !errors.Is(err, ErrInvalidRecoveryKey) {
		t.Errorf("an old key still verifies after the rotation committed: %v", err)
	}
}
