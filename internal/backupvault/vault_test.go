// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
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
	// A roomy fake disk unless the test says otherwise: a test about
	// retention or pairing should behave the same whatever the host's
	// own filesystem happens to be doing (#1125).
	v.statfs = (&fakeDisk{free: testDiskRoomy, total: testDiskTotal}).statfs
	return v
}

// The fake filesystem the low-space tests drive. The floor for a 100GiB
// filesystem is 5% of it, 5GiB, which is far above 2 x MaxFileBytes;
// leaving the mode needs 25% more than that again, 6.25GiB.
const (
	testDiskTotal = 100 << 30
	testDiskRoomy = 50 << 30
	testDiskTight = 1 << 30
	// testDiskAboveFloor is over the floor but under the exit margin:
	// the mode should hold rather than flap.
	testDiskAboveFloor = 5<<30 + 1<<29
)

// fakeDisk is a free/total pair a test can move under the vault's feet,
// so low-space mode can be driven without filling a real disk.
type fakeDisk struct {
	mu    sync.Mutex
	free  int64
	total int64
}

func (d *fakeDisk) statfs(string) (int64, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.free, d.total, nil
}

func (d *fakeDisk) set(free int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.free = free
}

// openVaultOnDisk opens a vault whose free-space measurement is disk's.
func openVaultOnDisk(t *testing.T, dir string, key *retention.Key, disk *fakeDisk) *Vault {
	t.Helper()
	v, err := Open(dir, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	v.statfs = disk.statfs
	return v
}

func mustStore(t *testing.T, v *Vault, device string, n int, now time.Time) {
	t.Helper()
	if err := v.Store(device, KindBackup, plainBackup(n), now); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func generationIDs(v *Vault, device string) []string {
	var ids []string
	for _, g := range v.Generations(device) {
		ids = append(ids, g.ID)
	}
	return ids
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
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

// listNames walks dir and returns every regular file's base name, so a
// test can assert about what's actually on disk rather than what the
// vault's own index says is there.
func listNames(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, d.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return names
}

// TestStoreLeavesNoTempLitterAndArtifactsExist covers #1082: the sealed
// blob (vault.go's Store) and the vault index (persistMetaLocked) are now
// written via persist.WriteFileAtomic's temp-then-rename-then-fsync
// dance instead of a direct os.WriteFile (blob) or a rename with no
// fsync (index). After a successful Store, no "*.tmp*" artifact from
// that dance should remain anywhere under the vault directory, and both
// the blob files and the index must exist and be readable.
func TestStoreLeavesNoTempLitterAndArtifactsExist(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("rb5009", KindRsc, []byte("export text"), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	for _, name := range listNames(t, dir) {
		if strings.Contains(name, ".tmp") {
			t.Errorf("leftover temp artifact after Store: %s", name)
		}
	}

	gens := v.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("got %d generations, want 1", len(gens))
	}
	routerDir := v.routerDir("rb5009")
	for _, kind := range []string{KindBackup, KindRsc} {
		p := filepath.Join(routerDir, v.fileName(gens[0].ID, kind))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("blob file missing for kind %s: %v", kind, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, metaFileName)); err != nil {
		t.Errorf("vault index missing: %v", err)
	}
}

// TestLeftoverCrashTempFileNotTreatedAsGeneration seeds a router
// directory with a stray temp file of the shape persist.WriteFileAtomic
// would leave behind had a prior write crashed between the temp write
// and the rename, then drives Store and reopens the vault. Generations
// are read entirely from the sealed index (never by scanning the
// directory), so the stray file must never surface as a generation of
// its own, before or after a reopen.
func TestLeftoverCrashTempFileNotTreatedAsGeneration(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
		t.Fatal(err)
	}

	routerDir := v.routerDir("rb5009")
	if err := os.MkdirAll(routerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	strayName := "deadbeef.backup.enc.tmp-000000001"
	if err := os.WriteFile(filepath.Join(routerDir, strayName), []byte("partial, never renamed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := v.Store("rb5009", KindBackup, plainBackup(20), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	assertTwoRealGenerations := func(v *Vault) {
		t.Helper()
		gens := v.Generations("rb5009")
		if len(gens) != 2 {
			t.Fatalf("got %d generations, want 2 (stray temp file must not be counted)", len(gens))
		}
		for _, g := range gens {
			if g.ID == strayName {
				t.Fatalf("stray temp file surfaced as a generation ID: %s", g.ID)
			}
		}
	}
	assertTwoRealGenerations(v)

	v2, err := Open(dir, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	assertTwoRealGenerations(v2)

	if _, err := os.Stat(filepath.Join(routerDir, strayName)); err != nil {
		t.Errorf("stray temp file should still be untouched on disk: %v", err)
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

// TestFailedWriteLeavesTheIndexUnchanged covers #1125's founding
// defect: the generation used to be added to the in-memory index before
// the file was written, so a write that failed left a generation with
// no file behind it. The next successful push persisted that phantom --
// it counted towards MaxGenerations and evicted a real generation, and
// a download of it 404ed.
func TestFailedWriteLeavesTheIndexUnchanged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory would not refuse the write")
	}
	dir := t.TempDir()
	key := testKey(t)
	v, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
		t.Fatal(err)
	}

	// Take the write away: the router's directory still exists, but
	// nothing new may be created in it.
	routerDir := v.routerDir("rb5009")
	if err := os.Chmod(routerDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(routerDir, 0o700) })

	if err := v.Store("rb5009", KindBackup, plainBackup(20), now.Add(time.Hour)); err == nil {
		t.Fatal("Store into an unwritable router directory succeeded, want the write to fail")
	}
	if got := len(v.Generations("rb5009")); got != 1 {
		t.Fatalf("after a failed write the vault holds %d generations, want 1 (no phantom)", got)
	}

	if err := os.Chmod(routerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(30), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	assertEveryGenerationHasItsFile := func(v *Vault, want int) {
		t.Helper()
		gens := v.Generations("rb5009")
		if len(gens) != want {
			t.Fatalf("got %d generations, want %d", len(gens), want)
		}
		for _, g := range gens {
			if _, err := os.Stat(filepath.Join(routerDir, v.fileName(g.ID, KindBackup))); err != nil {
				t.Errorf("generation %s is in the index with no file behind it: %v", g.ID, err)
			}
		}
	}
	assertEveryGenerationHasItsFile(v, 2)

	v2, err := Open(dir, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	assertEveryGenerationHasItsFile(v2, 2)
}

// TestLowSpaceKeepsTheAnchorAndTheNewestArrival is #1125's own "done
// when": with a faked low-space signal the anchor -- the newest
// generation from before the trouble -- survives three further
// arrivals, the newest arrival is always present, and the set stops
// growing instead of the vault refusing anything.
func TestLowSpaceKeepsTheAnchorAndTheNewestArrival(t *testing.T) {
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), disk)
	base := time.Now()

	for i := 0; i < 3; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	before := generationIDs(v, "rb5009")
	if len(before) != 3 {
		t.Fatalf("got %d generations before the trouble, want 3", len(before))
	}
	anchor := before[2]

	disk.set(testDiskTight)
	for i := 3; i < 6; i++ {
		now := base.Add(time.Duration(i) * time.Hour)
		mustStore(t, v, "rb5009", 10+i, now)
		if !v.LowSpace() {
			t.Fatalf("arrival %d: LowSpace() = false on a disk under the floor", i)
		}
		ids := generationIDs(v, "rb5009")
		if len(ids) != 3 {
			t.Fatalf("arrival %d: the set grew to %d generations, want 3 (replace, not add)", i, len(ids))
		}
		if !contains(ids, anchor) {
			t.Fatalf("arrival %d: the anchor %s was evicted: %v", i, anchor, ids)
		}
		newest := v.Generations("rb5009")[len(ids)-1]
		if newest.BackupSize != int64(len(plainBackup(10+i))) {
			t.Fatalf("arrival %d: newest generation is not the one that just arrived", i)
		}
	}

	// The cycled-out generations left no files behind.
	routerDir := v.routerDir("rb5009")
	kept := generationIDs(v, "rb5009")
	for _, name := range listNames(t, routerDir) {
		held := false
		for _, id := range kept {
			if strings.HasPrefix(name, id+".") {
				held = true
				break
			}
		}
		if !held {
			t.Errorf("file %s is on disk for a generation the vault no longer holds", name)
		}
	}
}

// TestLowSpaceWithOnlyTheAnchorLeftWritesAlongsideIt pins the floor of
// two files per router: with nothing but the anchor to cycle, the
// arrival is kept beside it rather than refused.
func TestLowSpaceWithOnlyTheAnchorLeftWritesAlongsideIt(t *testing.T) {
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), disk)
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	anchor := generationIDs(v, "rb5009")[0]

	disk.set(testDiskTight)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	ids := generationIDs(v, "rb5009")
	if len(ids) != 2 || !contains(ids, anchor) {
		t.Fatalf("got %v, want the anchor %s plus the new arrival", ids, anchor)
	}

	mustStore(t, v, "rb5009", 12, base.Add(2*time.Hour))
	ids = generationIDs(v, "rb5009")
	if len(ids) != 2 || !contains(ids, anchor) {
		t.Fatalf("got %v, want the anchor %s plus the newest arrival only", ids, anchor)
	}
}

// TestLeavingLowSpaceClearsAnchorsAndResumesRetention covers the exit:
// free space back above the floor with margin ends the mode, the
// anchors go with it, and the set grows again.
func TestLeavingLowSpaceClearsAnchorsAndResumesRetention(t *testing.T) {
	disk := &fakeDisk{free: testDiskTight, total: testDiskTotal}
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), disk)
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	if !v.LowSpace() {
		t.Fatal("LowSpace() = false on a disk under the floor")
	}

	// Over the floor but inside the exit margin: the mode holds.
	disk.set(testDiskAboveFloor)
	mustStore(t, v, "rb5009", 12, base.Add(2*time.Hour))
	if !v.LowSpace() {
		t.Fatal("the mode was left inside the exit margin -- it will flap")
	}

	disk.set(testDiskRoomy)
	mustStore(t, v, "rb5009", 13, base.Add(3*time.Hour))
	if v.LowSpace() {
		t.Fatal("LowSpace() = true with the disk back above the floor and margin")
	}
	v.mu.Lock()
	anchor := v.meta.Routers["rb5009"].Anchor
	v.mu.Unlock()
	if anchor != "" {
		t.Errorf("anchor %q survived the mode it belongs to", anchor)
	}
	if got := len(v.Generations("rb5009")); got != 3 {
		t.Fatalf("got %d generations, want 3 (normal retention grows the set again)", got)
	}
}

// TestLowSpaceModeAndAnchorsSurviveAReopen: a restart in the middle of
// the trouble must not forget which copy was the safe one.
func TestLowSpaceModeAndAnchorsSurviveAReopen(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskTight, total: testDiskTotal}
	v1 := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	mustStore(t, v1, "rb5009", 10, base)
	mustStore(t, v1, "rb5009", 11, base.Add(time.Hour))
	anchor := generationIDs(v1, "rb5009")[0]

	v2 := openVaultOnDisk(t, dir, key, disk)
	if !v2.LowSpace() {
		t.Fatal("the reopened vault forgot it was in low-space mode")
	}
	v2.mu.Lock()
	got := v2.meta.Routers["rb5009"].Anchor
	v2.mu.Unlock()
	if got != anchor {
		t.Fatalf("reopened anchor = %q, want %q", got, anchor)
	}
	mustStore(t, v2, "rb5009", 12, base.Add(2*time.Hour))
	if ids := generationIDs(v2, "rb5009"); len(ids) != 2 || !contains(ids, anchor) {
		t.Fatalf("after the reopen the vault holds %v, want the anchor %s plus the newest arrival", ids, anchor)
	}
}

// TestLowSpaceChangeIsReportedToTheCaller: the vault has no audit log
// of its own, so entering and leaving the mode is reported to whoever
// wired one up.
func TestLowSpaceChangeIsReportedToTheCaller(t *testing.T) {
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), disk)
	var changes []bool
	v.OnLowSpaceChange(func(low bool, detail string) {
		if detail == "" {
			t.Error("low-space change reported with no detail to audit")
		}
		changes = append(changes, low)
	})

	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	disk.set(testDiskTight)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	mustStore(t, v, "rb5009", 12, base.Add(2*time.Hour))
	disk.set(testDiskRoomy)
	mustStore(t, v, "rb5009", 13, base.Add(3*time.Hour))

	if len(changes) != 2 || changes[0] != true || changes[1] != false {
		t.Fatalf("low-space changes reported = %v, want one entry and one exit", changes)
	}
}

// TestLowSpaceWriteFailureKeepsTheIndexHonest: if even the
// replace-oldest write fails, the arrival is not added to the index and
// the generation whose files were already deleted to make room does not
// linger in it either.
func TestLowSpaceWriteFailureKeepsTheIndexHonest(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	before := generationIDs(v, "rb5009")
	anchor := before[2]

	disk.set(testDiskTight)
	routerDir := v.routerDir("rb5009")
	v.writeFile = func(path string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(path, routerDir) {
			return errors.New("no space left on device")
		}
		return persist.WriteFileAtomic(path, data, perm)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(99), base.Add(3*time.Hour)); err == nil {
		t.Fatal("Store with a failing writer succeeded, want the write error")
	}

	ids := generationIDs(v, "rb5009")
	if contains(ids, "") || len(ids) != 2 {
		t.Fatalf("got %v, want the two generations whose files are still there", ids)
	}
	if !contains(ids, anchor) {
		t.Fatalf("got %v, want the anchor %s kept", ids, anchor)
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(routerDir, v.fileName(id, KindBackup))); err != nil {
			t.Errorf("generation %s is in the index with no file behind it: %v", id, err)
		}
	}

	// The reopened vault agrees: nothing phantom was persisted.
	v.writeFile = nil
	v2 := openVaultOnDisk(t, dir, key, disk)
	if got := generationIDs(v2, "rb5009"); len(got) != len(ids) {
		t.Fatalf("reopened vault holds %v, want %v", got, ids)
	}
}

// TestStatfsBytesReadsTheRealFilesystem covers the measurement the
// low-space tests fake: on a real directory it reports a plausible
// free/total pair rather than an error.
func TestStatfsBytesReadsTheRealFilesystem(t *testing.T) {
	free, total, err := statfsBytes(t.TempDir())
	if err != nil {
		t.Fatalf("statfsBytes: %v", err)
	}
	if total <= 0 || free < 0 || free > total {
		t.Fatalf("statfsBytes = free %d, total %d, which is not a plausible filesystem", free, total)
	}
	if got, want := lowSpaceFloor(total), total/100*lowSpaceFloorPercent; got < want {
		t.Fatalf("lowSpaceFloor(%d) = %d, want at least %d%% of the filesystem (%d)",
			total, got, lowSpaceFloorPercent, want)
	}
}
