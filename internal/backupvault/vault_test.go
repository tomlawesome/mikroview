// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/retention"
)

func testKey(t *testing.T) *retention.Key {
	t.Helper()
	k, err := retention.NewKeyFromMaterial([]byte(strings.Repeat("k", retention.MinKeyBytes)))
	if err != nil {
		t.Fatalf("NewKeyFromMaterial: %v", err)
	}
	return k
}

func openVault(t *testing.T, key *retention.Key) *Vault {
	t.Helper()
	v, err := Open(t.TempDir(), key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

func plainBackup(n int) []byte {
	body := append([]byte{}, plainMagic...)
	body = append(body, bytes.Repeat([]byte("x"), n)...)
	return body
}

func TestNoKeyMeansDisabled(t *testing.T) {
	v := openVault(t, nil)
	if v.Enabled() {
		t.Fatal("Enabled() = true with no key")
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(10), time.Now()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Store with no key = %v, want ErrDisabled", err)
	}
}

func TestHeaderCheckAcceptsPlainAndEncryptedRefusesJunk(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()

	if err := v.Store("rb5009", KindBackup, plainBackup(100), now); err != nil {
		t.Fatalf("plain backup refused: %v", err)
	}
	encrypted := append([]byte{0xef, 0xa8, 0x91, 0x73}, []byte("ciphertext")...)
	if err := v.Store("rb5009", KindBackup, encrypted, now.Add(time.Hour)); err != nil {
		t.Fatalf("encrypted backup refused: %v", err)
	}
	junk := []byte("not a backup at all")
	if err := v.Store("rb5009", KindBackup, junk, now.Add(2*time.Hour)); !errors.Is(err, ErrNotABackup) {
		t.Fatalf("junk .backup = %v, want ErrNotABackup", err)
	}
	// .rsc is text: no header check.
	if err := v.Store("rb5009", KindRsc, []byte("# export\n/interface print\n"), now); err != nil {
		t.Fatalf(".rsc refused: %v", err)
	}

	gens := v.Generations("rb5009")
	if len(gens) != 2 {
		t.Fatalf("got %d generations, want 2 (junk refused, nothing kept)", len(gens))
	}
	if gens[0].Header != HeaderPlain {
		t.Errorf("first generation header = %q, want plain", gens[0].Header)
	}
	if gens[1].Header != HeaderEncrypted {
		t.Errorf("second generation header = %q, want encrypted", gens[1].Header)
	}
}

func TestOverCapRefusedAndNothingKept(t *testing.T) {
	v := openVault(t, testKey(t))
	big := plainBackup(MaxFileBytes + 1)
	if err := v.Store("rb5009", KindBackup, big, time.Now()); !errors.Is(err, ErrOverCap) {
		t.Fatalf("over-cap store = %v, want ErrOverCap", err)
	}
	if got := len(v.Generations("rb5009")); got != 0 {
		t.Fatalf("got %d generations after a refused push, want 0", got)
	}
}

func TestBackupThenRscPairIntoOneGeneration(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("rb5009", KindRsc, []byte("export text"), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	gens := v.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("got %d generations, want 1 (backup+rsc paired)", len(gens))
	}
	if !gens[0].HasBackup() || !gens[0].HasRsc() {
		t.Fatalf("generation missing a half: %+v", gens[0])
	}
}

func TestRetentionKeepsTenNewestPerRouter(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	for i := 0; i < MaxGenerations+3; i++ {
		now := base.Add(time.Duration(i) * time.Hour)
		if err := v.Store("rb5009", KindBackup, plainBackup(10+i), now); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	gens := v.Generations("rb5009")
	if len(gens) != MaxGenerations {
		t.Fatalf("got %d generations kept, want %d", len(gens), MaxGenerations)
	}
	// Oldest first: the surviving oldest should be push index 3 (0,1,2 evicted).
	if want := int64(len(plainBackup(10 + 3))); gens[0].BackupSize != want {
		t.Errorf("oldest surviving generation has size %d, want %d (the 3 oldest should have been evicted)",
			gens[0].BackupSize, want)
	}
	if want := int64(len(plainBackup(10 + MaxGenerations + 2))); gens[len(gens)-1].BackupSize != want {
		t.Errorf("newest generation has size %d, want %d (the last push's size)", gens[len(gens)-1].BackupSize, want)
	}
}

func TestRetentionIsPerRouter(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(1), now); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("hap-ax2", KindBackup, plainBackup(2), now); err != nil {
		t.Fatal(err)
	}
	if got := v.Routers(); len(got) != 2 {
		t.Fatalf("Routers() = %v, want 2 routers", got)
	}
}

func TestDownloadRoundTripsAndUnknownGenerationIsRefused(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	backup := plainBackup(123)
	if err := v.Store("rb5009", KindBackup, backup, now); err != nil {
		t.Fatal(err)
	}
	gen := v.Generations("rb5009")[0]

	got, err := v.Open("rb5009", gen.ID, KindBackup)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, backup) {
		t.Fatalf("round-tripped bytes differ")
	}

	if _, err := v.Open("rb5009", "no-such-generation", KindBackup); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open unknown generation = %v, want ErrNotFound", err)
	}
	if _, err := v.Open("no-such-router", gen.ID, KindBackup); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open unknown router = %v, want ErrNotFound", err)
	}
}

func TestMissedNeedsTwoArrivalsBeforeReportingAnInterval(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(1), now); err != nil {
		t.Fatal(err)
	}
	m := v.Missed("rb5009", now.Add(48*time.Hour))
	if m.IntervalKnown {
		t.Fatalf("Missed with one arrival reports an interval: %+v", m)
	}
	if m.Count != 0 {
		t.Fatalf("Missed with one arrival reports a count: %+v", m)
	}
}

func TestMissedFlagsAfterOneMissedInterval(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	// Three nightly pushes, 24h apart, establish a 24h interval.
	for i := 0; i < 3; i++ {
		if err := v.Store("hap-ax2", KindBackup, plainBackup(1), base.Add(time.Duration(i)*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	last := base.Add(2 * 24 * time.Hour)

	// Just under one interval later: not yet missed.
	m := v.Missed("hap-ax2", last.Add(23*time.Hour))
	if m.Count != 0 {
		t.Fatalf("Missed just under one interval = %+v, want Count 0", m)
	}

	// Three intervals (72h) later: three missed, as round 44's own data
	// story reads ("none since 30 Aug -- 3 missed").
	m = v.Missed("hap-ax2", last.Add(3*24*time.Hour))
	if !m.IntervalKnown || m.Count != 3 {
		t.Fatalf("Missed after 3 intervals = %+v, want Count 3", m)
	}
}

func TestStatsCountsGenerationsRoutersAndBytes(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(100), now); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("rb5009", KindRsc, []byte("0123456789"), now); err != nil {
		t.Fatal(err)
	}
	s := v.Stats()
	if s.Routers != 1 || s.Generations != 1 {
		t.Fatalf("Stats() = %+v, want 1 router, 1 generation", s)
	}
	wantBytes := int64(len(plainBackup(100))) + 10
	if s.Bytes != wantBytes {
		t.Fatalf("Stats().Bytes = %d, want %d", s.Bytes, wantBytes)
	}
}

func TestReopenLoadsPersistedMeta(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v1, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Store("rb5009", KindBackup, plainBackup(10), time.Now()); err != nil {
		t.Fatal(err)
	}

	v2, err := Open(dir, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := v2.Generations("rb5009"); len(got) != 1 {
		t.Fatalf("reopened vault has %d generations, want 1", len(got))
	}
}

func TestDeviceDirNamesDoNotLeakPathTraversal(t *testing.T) {
	for _, evil := range []string{"..", "../../etc", "a/b", "."} {
		name := dirNameFor(evil)
		if name == ".." || name == "." || strings.Contains(name, "/") {
			t.Errorf("dirNameFor(%q) = %q, which is not a safe single path segment", evil, name)
		}
	}
}
