// SPDX-License-Identifier: AGPL-3.0-only

package configsnap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

func openMemory(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	return s
}

func add(t *testing.T, s *Store, reason, note string) Meta {
	t.Helper()
	m, err := s.Add(Snapshot{Meta: Meta{Reason: reason, Note: note, By: "admin"}, Text: "listen: {}\n# " + note + "\n"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func notes(s *Store) []string {
	var out []string
	for _, m := range s.List() {
		out = append(out, m.Note)
	}
	return out
}

func TestKeepsTheLastFive(t *testing.T) {
	s := openMemory(t)
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		add(t, s, ReasonManual, n)
	}
	if got := strings.Join(notes(s), ","); got != "7,6,5,4,3" {
		t.Errorf("kept %s, want the newest five, newest first", got)
	}
}

// The newest before-carry-forward snapshot is the copy of the file as
// it was before the last rewrite -- the one a rollback needs -- so five
// manual snapshots taken after it must not push it out.
func TestNeverEvictsTheNewestBeforeCarryForward(t *testing.T) {
	s := openMemory(t)
	add(t, s, ReasonBeforeCarryForward, "old-cf")
	add(t, s, ReasonBeforeCarryForward, "cf")
	for _, n := range []string{"m1", "m2", "m3", "m4", "m5", "m6"} {
		add(t, s, ReasonManual, n)
	}
	got := notes(s)
	if len(got) != Keep {
		t.Fatalf("kept %d, want %d", len(got), Keep)
	}
	if got[len(got)-1] != "cf" {
		t.Errorf("kept %v -- the newest before-carry-forward snapshot was evicted", got)
	}
	for _, n := range got {
		if n == "old-cf" {
			t.Errorf("kept %v -- an older before-carry-forward snapshot is not protected", got)
		}
	}
	if strings.Join(got[:4], ",") != "m6,m5,m4,m3" {
		t.Errorf("kept %v, want the four newest manual ones beside it", got)
	}
}

func TestGetDeleteAndLimits(t *testing.T) {
	s := openMemory(t)
	m := add(t, s, "", "x")
	if m.Reason != ReasonManual {
		t.Errorf("an unlabelled snapshot got reason %q, want manual", m.Reason)
	}
	snap, err := s.Get(m.ID)
	if err != nil || !strings.Contains(snap.Text, "# x") {
		t.Fatalf("Get = %+v, %v", snap, err)
	}
	if err := s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: %v", err)
	}
	if err := s.Delete(m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if _, err := s.Add(Snapshot{Text: strings.Repeat("a", MaxTextBytes+1)}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized text: %v", err)
	}
}

// Snapshots hold whole config files, secrets included, so what reaches
// the disk is sealed -- and reads back after a restart.
func TestSealedAtRestAndReloaded(t *testing.T) {
	dir := t.TempDir()
	key, err := retention.NewKeyFromMaterial(bytes.Repeat([]byte{0x42}, retention.MinKeyBytes))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config-snapshots.json")
	s, err := Open(context.Background(), persist.NewEncryptedFileBackend(path, key))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "clientSecret: sealed-marker-1347"
	m, err := s.Add(Snapshot{Meta: Meta{By: "admin", Schema: 7, Version: "v0.6.1"}, Text: "oidc:\n  " + secret + "\n"})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte("sealed-marker-1347")) {
		t.Fatal("the snapshot file holds the config text in the clear")
	}

	again, err := Open(context.Background(), persist.NewEncryptedFileBackend(path, key))
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Get(m.ID)
	if err != nil || !strings.Contains(got.Text, secret) || got.Schema != 7 || got.Version != "v0.6.1" || got.By != "admin" {
		t.Errorf("reloaded snapshot = %+v, %v", got, err)
	}
}
