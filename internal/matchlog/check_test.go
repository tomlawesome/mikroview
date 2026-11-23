// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// #1470 review: Check says what is at the path without writing, so the
// Postgres backend's start-up warning can describe a leftover file
// accurately.
func TestCheckTellsSealedFromPlaintextAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if err := Check(filepath.Join(dir, "absent.jsonl")); err != nil {
		t.Errorf("absent: %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "absent.jsonl")); err == nil {
		t.Error("Check created a file")
	}

	sealed, _ := writeN(t, testKey(t), 1)
	if err := Check(sealed); err != nil {
		t.Errorf("sealed: %v, want nil", err)
	}

	plain := filepath.Join(dir, "plain.jsonl")
	if err := os.WriteFile(plain, []byte(`{"kind":"record","id":"aa","lastSeen":"2026-08-09T12:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(plain); !errors.Is(err, persist.ErrPlaintextDocument) {
		t.Errorf("plaintext: %v, want persist.ErrPlaintextDocument", err)
	}

	other := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(other, []byte("not a match log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(other); err == nil || errors.Is(err, persist.ErrPlaintextDocument) {
		t.Errorf("other: %v, want a not-a-match-log error", err)
	}
}
