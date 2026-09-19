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

	"golang.org/x/sys/unix"

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
	v, err := Open(t.TempDir(), key, nil)
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
	return openVaultOnDiskWithCandidates(t, dir, key, disk, nil)
}

// openVaultOnDiskWithCandidates is openVaultOnDisk plus the recovery
// candidates a lost index's rebuild is given to try (#1294) -- broken
// out only for the tests that care, so the other 29-odd call sites stay
// untouched.
func openVaultOnDiskWithCandidates(t *testing.T, dir string, key *retention.Key, disk *fakeDisk, candidates []string) *Vault {
	t.Helper()
	v, err := Open(dir, key, candidates)
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
	v1, err := Open(dir, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Store("rb5009", KindBackup, plainBackup(10), time.Now()); err != nil {
		t.Fatal(err)
	}

	v2, err := Open(dir, key, nil)
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
	v, err := Open(dir, key, nil)
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
	v, err := Open(dir, key, nil)
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

	v2, err := Open(dir, key, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	assertTwoRealGenerations(v2)

	// The reopen reconciles the directory against the index (#1125):
	// a file no generation refers to -- this half-written temp file, or
	// a blob whose index write never landed -- is dead weight on a disk
	// that is probably already full, so it goes.
	if _, err := os.Stat(filepath.Join(routerDir, strayName)); !os.IsNotExist(err) {
		t.Errorf("stray temp file survived the reopen, want it removed as unreferenced: %v", err)
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
	v, err := Open(dir, key, nil)
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

	v2, err := Open(dir, key, nil)
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

// TestLowSpaceWriteFailureKeepsEveryGenerationItAlreadyHad: a write
// that fails for any reason other than a full disk must cost the router
// nothing. The replacement is written first, so the generation it would
// have replaced is still there -- index entry and file both -- and the
// arrival is not in the index. The old order (drop, then write)
// destroyed a generation and stored nothing, eroding every router to
// its anchor one failed push at a time.
func TestLowSpaceWriteFailureKeepsEveryGenerationItAlreadyHad(t *testing.T) {
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
			return errors.New("the disk went away mid-write")
		}
		return persist.WriteFileAtomic(path, data, perm)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(99), base.Add(3*time.Hour)); err == nil {
		t.Fatal("Store with a failing writer succeeded, want the write error")
	}

	ids := generationIDs(v, "rb5009")
	if contains(ids, "") || len(ids) != len(before) {
		t.Fatalf("got %v, want the %d generations the vault already had", ids, len(before))
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

// TestFreeSpaceIsMeasuredWithoutHoldingTheIndexLock: the free-space
// check is a syscall against a filesystem that may already be sick, and
// every reader of the index -- the Settings list, a download -- would
// queue behind it if it ran under v.mu (#1125). It is taken before the
// lock and handed to the store path.
func TestFreeSpaceIsMeasuredWithoutHoldingTheIndexLock(t *testing.T) {
	v, err := Open(t.TempDir(), testKey(t), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var underLock bool
	v.statfs = func(string) (int64, int64, error) {
		if v.mu.TryLock() {
			v.mu.Unlock()
		} else {
			underLock = true
		}
		return testDiskRoomy, testDiskTotal, nil
	}
	mustStore(t, v, "rb5009", 10, time.Now())
	if underLock {
		t.Fatal("free space was measured with the vault index lock held")
	}
}

// TestLowSpaceFallsBackToDroppingFirstOnlyWhenTheDiskIsFull: the
// replacement is written before the generation it replaces is dropped,
// so a write that fails costs nothing. Only a write refused for want of
// space (ENOSPC) earns the dangerous order -- drop, then retry.
func TestLowSpaceFallsBackToDroppingFirstOnlyWhenTheDiskIsFull(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	before := generationIDs(v, "rb5009")
	oldest := before[0]

	disk.set(testDiskTight)
	routerDir := v.routerDir("rb5009")
	var attempts int
	v.writeFile = func(path string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(path, routerDir) {
			attempts++
			if attempts == 1 {
				return &os.PathError{Op: "write", Path: path, Err: unix.ENOSPC}
			}
		}
		return persist.WriteFileAtomic(path, data, perm)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(99), base.Add(3*time.Hour)); err != nil {
		t.Fatalf("Store on a disk that took the file after the drop: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("the vault made %d write attempts, want a write, a drop and a retry", attempts)
	}
	ids := generationIDs(v, "rb5009")
	if len(ids) != 3 {
		t.Fatalf("got %v, want three generations (the arrival replaced one)", ids)
	}
	if contains(ids, oldest) {
		t.Fatalf("got %v, want the oldest generation %s dropped to make room", ids, oldest)
	}
	for _, id := range ids {
		if _, err := os.Stat(filepath.Join(routerDir, v.fileName(id, KindBackup))); err != nil {
			t.Errorf("generation %s is in the index with no file behind it: %v", id, err)
		}
	}
}

// TestIndexWriteFailureLeavesNoOrphanFile: the file is written before
// the index that names it, so an index write that fails -- the likely
// failure on the full disk that got the vault here -- leaves up to
// 16MiB nothing will ever reference. It is removed instead (#1125).
func TestIndexWriteFailureLeavesNoOrphanFile(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultOnDisk(t, dir, key, &fakeDisk{free: testDiskRoomy, total: testDiskTotal})
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	kept := generationIDs(v, "rb5009")[0]

	metaPath := filepath.Join(dir, metaFileName)
	v.writeFile = func(path string, data []byte, perm os.FileMode) error {
		if path == metaPath {
			return errors.New("no space left on device")
		}
		return persist.WriteFileAtomic(path, data, perm)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(11), base.Add(time.Hour)); err == nil {
		t.Fatal("Store whose index write failed succeeded, want the error")
	}
	v.writeFile = nil

	routerDir := v.routerDir("rb5009")
	for _, name := range listNames(t, routerDir) {
		if !strings.HasPrefix(name, kept+".") {
			t.Errorf("%s was left behind for a generation the index never learned about", name)
		}
	}
	if ids := generationIDs(v, "rb5009"); len(ids) != 1 || ids[0] != kept {
		t.Fatalf("after a failed index write the vault holds %v, want only %s", ids, kept)
	}

	// The next push still works, and everything the index claims has a
	// file behind it.
	mustStore(t, v, "rb5009", 12, base.Add(2*time.Hour))
	for _, id := range generationIDs(v, "rb5009") {
		if _, err := os.Stat(filepath.Join(routerDir, v.fileName(id, KindBackup))); err != nil {
			t.Errorf("generation %s is in the index with no file behind it: %v", id, err)
		}
	}
}

// TestOpenAdoptsWellFormedOrphansInsteadOfDeletingThem covers #1294's
// narrowing of the sweep: a process killed between writing a
// generation's file and committing the index leaves a file nothing
// refers to, and it used to be deleted outright on the same terms as
// genuine junk -- which could just as well be an undamaged backup a
// crash never got to record. A name the sweep can recognise as one of
// this package's own is adopted into the cycling set instead, both for
// a router the index already knows (kept under its real name) and for
// a whole router directory nothing refers to (kept under the directory
// hash, same placement rebuildFromDisk gives a fully lost index).
func TestOpenAdoptsWellFormedOrphansInsteadOfDeletingThem(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	mustStore(t, v, "rb5009", 10, time.Now())
	routerDir := v.routerDir("rb5009")
	kept := generationIDs(v, "rb5009")[0]

	orphan := filepath.Join(routerDir, "20260101T000000.000000000Z-000042.backup.enc")
	if err := os.WriteFile(orphan, []byte("written, never indexed"), 0o600); err != nil {
		t.Fatal(err)
	}
	strayHash := dirNameFor("a router the index has forgotten")
	strayDir := filepath.Join(dir, strayHash)
	if err := os.MkdirAll(strayDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(strayDir, "20260101T000000.000000000Z-000043.rsc.enc")
	if err := os.WriteFile(stray, []byte("a whole router nothing refers to"), 0o600); err != nil {
		t.Fatal(err)
	}

	v2 := openVaultOnDisk(t, dir, key, disk)
	for _, path := range []string{orphan, stray} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s did not survive the reopen, want it adopted rather than deleted: %v", path, err)
		}
	}
	if ids := generationIDs(v2, "rb5009"); len(ids) != 2 || !contains(ids, kept) || !contains(ids, "20260101T000000.000000000Z-000042") {
		t.Fatalf("rb5009's generations after the reopen = %v, want %s (already known) plus the adopted orphan", ids, kept)
	}
	strayPlaceholder := recoveredDeviceUnknown + strayHash
	if ids := generationIDs(v2, strayPlaceholder); len(ids) != 1 || ids[0] != "20260101T000000.000000000Z-000043" {
		t.Fatalf("the stray directory's generations = %v, want the adopted orphan kept under its placeholder", ids)
	}
}

// TestOpenRemovesGenuineJunkNotShapedLikeOneOfOurs: the sweep only
// spares names it can read as a crash temp file or a well-formed
// `<id>.<kind>.enc`. Anything else -- a stray editor droppings, a typo
// -- is still deleted, exactly as before #1294 narrowed what else the
// sweep may touch.
func TestOpenRemovesGenuineJunkNotShapedLikeOneOfOurs(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	mustStore(t, v, "rb5009", 10, time.Now())
	routerDir := v.routerDir("rb5009")

	junk := filepath.Join(routerDir, "notes.txt")
	if err := os.WriteFile(junk, []byte("not one of ours"), 0o600); err != nil {
		t.Fatal(err)
	}

	openVaultOnDisk(t, dir, key, disk)
	if _, err := os.Stat(junk); !os.IsNotExist(err) {
		t.Errorf("%s survived the reopen, want genuine junk removed: %v", junk, err)
	}
}

// TestAZeroTotalMeasurementDoesNotEnterLowSpace: a filesystem reporting
// no blocks at all is a measurement that means nothing, not a full
// disk. Believing it would pin the vault in low-space mode for good --
// free=0 is under every floor and can never climb over the exit margin.
func TestAZeroTotalMeasurementDoesNotEnterLowSpace(t *testing.T) {
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), &fakeDisk{free: 0, total: 0})
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	if v.LowSpace() {
		t.Fatal("a filesystem reporting zero total bytes put the vault in low-space mode")
	}
	if got := len(v.Generations("rb5009")); got != 2 {
		t.Fatalf("got %d generations, want 2 (normal retention, the measurement said nothing)", got)
	}
}

// TestOpenDropsIndexEntriesWhoseFileIsMissing is the other half of the
// start-up reconcile: an index entry whose file is gone -- the far side
// of an index write that failed after a generation was cycled out -- is
// a generation the vault would list, offer and 404 on. The reopen drops
// it and keeps the repaired index.
func TestOpenDropsIndexEntriesWhoseFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	ids := generationIDs(v, "rb5009")
	if len(ids) != 2 {
		t.Fatalf("got %v, want two generations to start with", ids)
	}
	lost, kept := ids[0], ids[1]

	if err := os.Remove(filepath.Join(v.routerDir("rb5009"), v.fileName(lost, KindBackup))); err != nil {
		t.Fatal(err)
	}

	v2 := openVaultOnDisk(t, dir, key, disk)
	if got := generationIDs(v2, "rb5009"); len(got) != 1 || got[0] != kept {
		t.Fatalf("the reopened vault lists %v, want only %s -- the other has no file to serve", got, kept)
	}
	if got := v2.Stats().Generations; got != 1 {
		t.Errorf("Stats() counts %d generations, want 1", got)
	}

	// The repair was persisted, not just made in memory.
	v3 := openVaultOnDisk(t, dir, key, disk)
	if got := generationIDs(v3, "rb5009"); len(got) != 1 || got[0] != kept {
		t.Fatalf("the second reopen lists %v, want the repaired index to have been kept", got)
	}
}

// TestOpenRebuildsFromDiskMatchingACandidateName is #1294's Defect A
// end to end, for the common case: the vault keys and seals by the
// SFTP login name, and that name lives in the ingest token store, which
// an index loss does not touch. A directory whose hash matches
// dirNameFor(name) for a candidate the caller supplies recovers keyed
// by its real name straight away -- decryptable and downloadable now,
// no placeholder, no waiting for a push.
func TestOpenRebuildsFromDiskMatchingACandidateName(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v1 := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v1, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	preLoss := generationIDs(v1, "rb5009")
	if len(preLoss) != 3 {
		t.Fatalf("got %d generations before the loss, want 3", len(preLoss))
	}

	// The Defect A sequence: meta.enc gone, service restarted with
	// nothing else changed -- but this time the caller can offer
	// "rb5009" as a candidate, exactly as backups.go's
	// recoveryCandidates would from the still-intact token store.
	if err := os.Remove(filepath.Join(dir, metaFileName)); err != nil {
		t.Fatal(err)
	}
	v2 := openVaultOnDiskWithCandidates(t, dir, key, disk, []string{"unrelated-router", "rb5009"})

	if v2.RecoveredNameUnknown("rb5009") {
		t.Fatal(`RecoveredNameUnknown("rb5009") = true, want a matched candidate recovered under its real name`)
	}
	recovered := v2.ProtectedGenerations("rb5009")
	if len(recovered) != 3 {
		t.Fatalf(`got %d generations under "rb5009", want the 3 that predated the loss`, len(recovered))
	}
	for _, g := range recovered {
		if !contains(preLoss, g.ID) {
			t.Errorf("recovered generation %s was not one of the pre-loss generations %v", g.ID, preLoss)
		}
		if g.Comment == "" || g.ProtectedBy == "" || g.ProtectedAt.IsZero() {
			t.Errorf("recovered generation %s is missing its protection metadata: %+v", g.ID, g)
		}
	}

	// Matched means readable now, not just listed: a download of a
	// recovered generation must actually decrypt, the same round trip
	// any other backup gets.
	original := plainBackup(10)
	got, err := v2.Open("rb5009", preLoss[0], KindBackup)
	if err != nil {
		t.Fatalf("Open a matched-recovery generation: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("round-tripped bytes differ for a matched-recovery generation")
	}
}

// TestOpenRebuildsFromDiskWithNoMatchingCandidate covers the other half:
// no candidate explains the directory (a revoked token, or recovery
// running before the caller had any candidates at all). The generations
// are still recovered, protected, under a recoveredDeviceUnknown
// placeholder -- but sealed under a name this rebuild does not have, so
// Open refuses them with ErrRecoveredNameUnknown rather than attempting
// a decrypt that cannot succeed. The router's own next push, under its
// real name, re-associates the placeholder and makes them readable.
func TestOpenRebuildsFromDiskWithNoMatchingCandidate(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v1 := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v1, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	preLoss := generationIDs(v1, "rb5009")
	if len(preLoss) != 3 {
		t.Fatalf("got %d generations before the loss, want 3", len(preLoss))
	}

	if err := os.Remove(filepath.Join(dir, metaFileName)); err != nil {
		t.Fatal(err)
	}
	v2 := openVaultOnDiskWithCandidates(t, dir, key, disk, []string{"some-other-router"})

	placeholder := recoveredDeviceUnknown + dirNameFor("rb5009")
	if !v2.RecoveredNameUnknown(placeholder) {
		t.Fatal("RecoveredNameUnknown(placeholder) = false, want true for a directory no candidate explained")
	}
	recovered := v2.ProtectedGenerations(placeholder)
	if len(recovered) != 3 {
		t.Fatalf("got %d recovered generations under the placeholder, want the 3 that predated the loss", len(recovered))
	}

	// Refused with the specific error, not a decrypt failure and not a
	// 404 from the wrong directory.
	if _, err := v2.Open(placeholder, preLoss[0], KindBackup); !errors.Is(err, ErrRecoveredNameUnknown) {
		t.Fatalf("Open on an unmatched recovery = %v, want ErrRecoveredNameUnknown", err)
	}

	// A few ordinary pushes under the router's real name: the
	// placeholder re-associates with it rather than a second, empty
	// entry starting up beside it, and downloads work from that point.
	for i := 0; i < 2; i++ {
		mustStore(t, v2, "rb5009", 20+i, base.Add(time.Duration(10+i)*time.Hour))
	}
	if v2.RecoveredNameUnknown("rb5009") {
		t.Fatal(`RecoveredNameUnknown("rb5009") = true after re-association, want false`)
	}
	if got := len(v2.ProtectedGenerations("rb5009")); got != 3 {
		t.Fatalf("after re-association, ProtectedGenerations(rb5009) = %d, want the 3 recovered generations", got)
	}
	if got := len(v2.Generations("rb5009")); got != 2 {
		t.Fatalf("after re-association, Generations(rb5009) = %d, want the 2 new pushes", got)
	}
	if _, err := v2.Open("rb5009", preLoss[0], KindBackup); err != nil {
		t.Fatalf("Open after re-association: %v, want the pre-loss generation readable now", err)
	}

	// A further reopen: the rebuild and the re-association were both
	// persisted, not just held in memory, and the pre-loss files are
	// still on disk.
	v3 := openVaultOnDisk(t, dir, key, disk)
	if got := len(v3.ProtectedGenerations("rb5009")); got != 3 {
		t.Fatalf("after a second reopen, ProtectedGenerations(rb5009) = %d, want the 3 recovered generations kept", got)
	}
	routerDir := v3.routerDir("rb5009")
	for _, id := range preLoss {
		if _, err := os.Stat(filepath.Join(routerDir, v3.fileName(id, KindBackup))); err != nil {
			t.Errorf("pre-loss generation %s's file is gone after the missing-index recovery: %v", id, err)
		}
	}
}

// TestRecoveredRouterSurvivesAFurtherReopenBeforeItPushesAgain is the
// review catch on the first pass at #1294: a rebuilt index keyed a
// recovered router by its directory hash, but every directory helper
// still hashed the *key* unconditionally, so a hash-keyed entry
// resolved to dirNameFor(hash) -- a directory that does not exist. That
// was invisible as long as the very next thing to happen was the
// router's own push (which re-keys the entry back to its real name
// before anything else looks at the directory). It is not invisible
// across a second restart with no push in between -- an operator
// restarting the service more than once while recovering, which is a
// completely ordinary thing to do -- where reconcile would run again
// against the wrong directory, find both halves of every recovered
// generation "missing", and drop them; the commit message's claim that
// losing the index costs the notes and never the backups was false
// across two restarts. Still covers the no-matching-candidate case:
// that is the one still keyed by a placeholder, so it is the one this
// bug could reach.
func TestRecoveredRouterSurvivesAFurtherReopenBeforeItPushesAgain(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v1 := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v1, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	preLoss := generationIDs(v1, "rb5009")
	if len(preLoss) != 3 {
		t.Fatalf("got %d generations before the loss, want 3", len(preLoss))
	}

	if err := os.Remove(filepath.Join(dir, metaFileName)); err != nil {
		t.Fatal(err)
	}
	v2 := openVaultOnDisk(t, dir, key, disk)
	placeholder := recoveredDeviceUnknown + dirNameFor("rb5009")
	if got := len(v2.ProtectedGenerations(placeholder)); got != 3 {
		t.Fatalf("got %d recovered generations after the first reopen, want 3", got)
	}

	// No push in between: the router has not spoken up yet, so the
	// entry is still keyed by the placeholder when this second reopen's
	// reconcile runs against it.
	v3 := openVaultOnDisk(t, dir, key, disk)
	recovered := v3.ProtectedGenerations(placeholder)
	if len(recovered) != 3 {
		t.Fatalf("got %d recovered generations after a second reopen with no push in between, want the 3 still kept -- "+
			"reconcile must not mistake its own recovered directory for one whose files are gone", len(recovered))
	}
	routerDir := v3.routerDir(placeholder)
	for _, id := range preLoss {
		if _, err := os.Stat(filepath.Join(routerDir, v3.fileName(id, KindBackup))); err != nil {
			t.Errorf("pre-loss generation %s's file is gone after a second reopen with no push in between: %v", id, err)
		}
	}
}

// TestReconcileDemotesRatherThanDropsAHalfPresentProtectedGeneration is
// #1294's Defect B: presentOnDisk used to drop a generation's whole
// entry when either of its files went missing, applied to the
// protected pool on the same terms as the cycling set, and
// removeUnreferencedFiles then deleted the surviving, undamaged
// sibling -- for a protected generation, the very thing an admin asked
// to keep. Losing one file now demotes the entry to the half that
// survives; it is never dropped, and the surviving file is never swept.
func TestReconcileDemotesRatherThanDropsAHalfPresentProtectedGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		removeKind string
		survivor   string
	}{
		{"backup file lost, export survives", KindBackup, KindRsc},
		{"export file lost, backup survives", KindRsc, KindBackup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			key := testKey(t)
			disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
			v := openVaultOnDisk(t, dir, key, disk)
			now := time.Now()
			if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
				t.Fatal(err)
			}
			if err := v.Store("rb5009", KindRsc, []byte("export text"), now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			id := generationIDs(v, "rb5009")[0]
			if err := v.Protect("rb5009", id, "before the office move", "tom", now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}

			routerDir := v.routerDir("rb5009")
			if err := os.Remove(filepath.Join(routerDir, v.fileName(id, tc.removeKind))); err != nil {
				t.Fatal(err)
			}

			v2 := openVaultOnDisk(t, dir, key, disk)
			protected := v2.ProtectedGenerations("rb5009")
			if len(protected) != 1 || protected[0].ID != id {
				t.Fatalf("protected pool after repair = %v, want the same generation %s kept, demoted rather than dropped", protected, id)
			}
			g := protected[0]
			lostHas, survivorHas := g.HasRsc(), g.HasBackup()
			if tc.removeKind == KindBackup {
				lostHas, survivorHas = g.HasBackup(), g.HasRsc()
			}
			if lostHas {
				t.Errorf("the lost half (%s) still reports as present: %+v", tc.removeKind, g)
			}
			if !survivorHas {
				t.Errorf("the surviving half (%s) reports as absent: %+v", tc.survivor, g)
			}
			if _, err := os.Stat(filepath.Join(routerDir, v2.fileName(id, tc.survivor))); err != nil {
				t.Errorf("the surviving %s file was removed by reconcile, want it kept: %v", tc.survivor, err)
			}
		})
	}
}

// TestReconcileLeavesAGenerationWhoseExportNeverArrivedAlone is the
// regression guard for the rule presentOnDisk and repairIndexAgainstDisk
// already state in their own comments: a generation whose `.rsc` never
// came is complete as it stands, and repair must not read that as a
// lost half.
func TestReconcileLeavesAGenerationWhoseExportNeverArrivedAlone(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	now := time.Now()
	if err := v.Store("rb5009", KindBackup, plainBackup(10), now); err != nil {
		t.Fatal(err)
	}
	id := generationIDs(v, "rb5009")[0]
	if err := v.Protect("rb5009", id, "before the 7.16 upgrade", "tom", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	v2 := openVaultOnDisk(t, dir, key, disk)
	protected := v2.ProtectedGenerations("rb5009")
	if len(protected) != 1 || protected[0].ID != id {
		t.Fatalf("protected pool after repair = %v, want the untouched generation %s", protected, id)
	}
	if !protected[0].HasBackup() || protected[0].HasRsc() {
		t.Errorf("a generation whose export never arrived changed shape across repair: %+v", protected[0])
	}
	routerDir := v.routerDir("rb5009")
	if _, err := os.Stat(filepath.Join(routerDir, v2.fileName(id, KindBackup))); err != nil {
		t.Errorf("backup file missing after repair: %v", err)
	}
}

// TestUnreadableIndexErrorNamesWhatDeletingCosts covers the other half
// of #1294's plan: meta.enc present but unreadable must still refuse to
// start -- consistent with storage_preflight.go's rule that a file
// present but not trusted is a hard stop -- but the refusal must say
// what removing the file costs, so an operator is not left to
// rediscover the hard way that it is not the backups themselves.
func TestUnreadableIndexErrorNamesWhatDeletingCosts(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultOnDisk(t, dir, key, &fakeDisk{free: testDiskRoomy, total: testDiskTotal})
	if err := v.Store("rb5009", KindBackup, plainBackup(10), time.Now()); err != nil {
		t.Fatal(err)
	}

	metaPath := filepath.Join(dir, metaFileName)
	if err := os.WriteFile(metaPath, []byte("not a sealed document"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(dir, key, nil)
	if err == nil {
		t.Fatal("Open with a corrupted index succeeded, want the refusal")
	}
	msg := err.Error()
	for _, want := range []string{"protection", "anchor", "backup files themselves", "recovers"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}
