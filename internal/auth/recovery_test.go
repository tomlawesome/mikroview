// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRecovery(t *testing.T) (*RecoveryStore, string, string) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "recovery-keys.json")
	pepperPath := filepath.Join(dir, "recovery-pepper.key")
	s, err := OpenRecovery(keyPath, pepperPath)
	if err != nil {
		t.Fatalf("OpenRecovery: %v", err)
	}
	return s, keyPath, pepperPath
}

func TestGenerateAndRedeem(t *testing.T) {
	s, _, _ := newRecovery(t)

	keys, err := s.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != recoveryKeyCount {
		t.Fatalf("got %d keys, want %d", len(keys), recoveryKeyCount)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	// Any of the set works.
	for i, k := range keys {
		s2, _ := OpenRecovery(s.path, s.pepperPath)
		if _, err := s2.Redeem(k); err != nil {
			t.Errorf("key %d did not verify: %v", i, err)
		}
	}
}

// TestRedeemRotatesTheWholeSet: using one key invalidates all of them,
// which is what makes three keys "one key with two spares".
func TestRedeemRotatesTheWholeSet(t *testing.T) {
	s, keyPath, pepperPath := newRecovery(t)
	keys, _ := s.Generate()
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	fresh, err := s.Redeem(keys[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	reopened, _ := OpenRecovery(keyPath, pepperPath)
	for i, old := range keys {
		if _, err := reopened.Redeem(old); !errors.Is(err, ErrInvalidRecoveryKey) {
			t.Errorf("old key %d still works after rotation (err=%v)", i, err)
		}
	}
	reopened2, _ := OpenRecovery(keyPath, pepperPath)
	if _, err := reopened2.Redeem(fresh[0]); err != nil {
		t.Errorf("a freshly issued key does not work: %v", err)
	}
}

// TestRotationIsNotCommittedWithoutAcknowledgement is the gate that stops
// a successful recovery from leaving the operator with zero valid keys
// when the printed output is lost.
func TestRotationIsNotCommittedWithoutAcknowledgement(t *testing.T) {
	s, keyPath, pepperPath := newRecovery(t)
	keys, _ := s.Generate()
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Redeem(keys[0]); err != nil {
		t.Fatal(err)
	}
	// Deliberately no Commit -- simulating the process dying, or the
	// operator not confirming, after the new set was displayed.

	reopened, _ := OpenRecovery(keyPath, pepperPath)
	if _, err := reopened.Redeem(keys[1]); err != nil {
		t.Errorf("the old keys were invalidated despite no acknowledgement -- "+
			"a lost printout would leave this deployment unrecoverable (err=%v)", err)
	}
}

// TestRegenerationIsRefused: without this, anyone with host access mints
// a fresh set and satisfies the gate they were meant to be stopped by.
func TestRegenerationIsRefused(t *testing.T) {
	s, _, _ := newRecovery(t)
	if _, err := s.Generate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Generate(); !errors.Is(err, ErrRecoveryKeysExist) {
		t.Errorf("regeneration was allowed (err=%v) -- this bypasses the gate entirely", err)
	}
}

// TestPepperIsRequiredToVerify: the point of the pepper is that a stolen
// backup, which excludes it, cannot be used to check keys offline.
func TestPepperIsRequiredToVerify(t *testing.T) {
	s, keyPath, pepperPath := newRecovery(t)
	keys, _ := s.Generate()
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	// Simulate a backup that carries the digests but not the pepper.
	stolenDir := t.TempDir()
	digests, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	stolenKeys := filepath.Join(stolenDir, "recovery-keys.json")
	if err := os.WriteFile(stolenKeys, digests, 0o600); err != nil {
		t.Fatal(err)
	}

	thief, err := OpenRecovery(stolenKeys, filepath.Join(stolenDir, "recovery-pepper.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thief.Redeem(keys[0]); err == nil {
		t.Error("a real key verified against digests restored without the pepper -- " +
			"the pepper is buying nothing")
	}
	_ = pepperPath
}

func TestNoKeysFailsClosed(t *testing.T) {
	s, _, _ := newRecovery(t)
	if _, err := s.Redeem("ANYTHING"); !errors.Is(err, ErrNoRecoveryKeys) {
		t.Errorf("a store with no keys returned %v -- absent must never read as 'no gate'", err)
	}
}

func TestCorruptKeyFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "recovery-keys.json")
	if err := os.WriteFile(keyPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenRecovery(keyPath, filepath.Join(dir, "recovery-pepper.key"))
	if err == nil {
		t.Error("a corrupt key file loaded cleanly -- truncating the file would be the bypass")
	}
}

// Transcription forgiveness must not weaken the key: case and separators
// aren't part of the secret, but the characters are.
func TestKeyNormalisation(t *testing.T) {
	s, _, _ := newRecovery(t)
	keys, _ := s.Generate()
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	k := keys[0]

	for _, variant := range []string{
		strings.ToLower(k),
		k[:8] + "-" + k[8:],
		" " + k + " ",
	} {
		s2, _ := OpenRecovery(s.path, s.pepperPath)
		if _, err := s2.Redeem(variant); err != nil {
			t.Errorf("variant %q was rejected: %v", variant, err)
		}
	}

	// One character different must still fail.
	wrong := []byte(k)
	if wrong[0] == 'A' {
		wrong[0] = 'B'
	} else {
		wrong[0] = 'A'
	}
	s3, _ := OpenRecovery(s.path, s.pepperPath)
	if _, err := s3.Redeem(string(wrong)); !errors.Is(err, ErrInvalidRecoveryKey) {
		t.Error("a key differing by one character was accepted")
	}
}

func TestKeyEntropy(t *testing.T) {
	s, _, _ := newRecovery(t)
	keys, _ := s.Generate()
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Error("duplicate key in one set -- keys must be independently random")
		}
		seen[k] = true
		// 160 bits in unpadded base32 is 32 characters.
		if len(k) != 32 {
			t.Errorf("key %q is %d chars, want 32 (160 bits) -- below 112 bits a "+
				"salted KDF would be required instead of a fast HMAC", k, len(k))
		}
	}
}
