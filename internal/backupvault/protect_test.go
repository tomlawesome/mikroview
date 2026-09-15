// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

// The protected pool (#1126): what "kept" has to mean for the promise
// to hold -- out of the ten, out of retention's reach, out of low-space
// cycling's reach, still there after a restart, and still on disk after
// the unreferenced-file sweep that runs on the way back up.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// protectedIDs is ProtectedGenerations' ids, oldest first.
func protectedIDs(v *Vault, device string) []string {
	var ids []string
	for _, g := range v.ProtectedGenerations(device) {
		ids = append(ids, g.ID)
	}
	return ids
}

func fileIsThere(t *testing.T, v *Vault, device, id, kind string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(v.routerDir(device), v.fileName(id, kind)))
	return err == nil
}

func TestProtectMovesAGenerationIntoItsOwnPool(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	ids := generationIDs(v, "rb5009")

	at := base.Add(4 * time.Hour)
	if err := v.Protect("rb5009", ids[0], "  before the 7.16 upgrade  ", "tom", at); err != nil {
		t.Fatalf("Protect: %v", err)
	}

	if got := generationIDs(v, "rb5009"); len(got) != 2 || contains(got, ids[0]) {
		t.Fatalf("cycling set = %v, want the other two without %s", got, ids[0])
	}
	kept := v.ProtectedGenerations("rb5009")
	if len(kept) != 1 {
		t.Fatalf("kept pool = %d, want 1", len(kept))
	}
	if kept[0].ID != ids[0] {
		t.Errorf("kept id = %s, want %s", kept[0].ID, ids[0])
	}
	if kept[0].Comment != "before the 7.16 upgrade" {
		t.Errorf("comment = %q, want it trimmed and kept", kept[0].Comment)
	}
	if kept[0].ProtectedBy != "tom" || !kept[0].ProtectedAt.Equal(at) {
		t.Errorf("kept by %q at %v, want tom at %v", kept[0].ProtectedBy, kept[0].ProtectedAt, at)
	}
	// Still readable: keeping a backup has never meant hiding it.
	if _, err := v.Open("rb5009", ids[0], KindBackup); err != nil {
		t.Errorf("Open on a kept generation: %v", err)
	}
}

func TestProtectRefusesWhatItShould(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	mustStore(t, v, "rb5009", 10, now)
	id := generationIDs(v, "rb5009")[0]

	for name, comment := range map[string]string{
		"empty":            "",
		"whitespace only":  "   ",
		"over the cap":     strings.Repeat("x", MaxCommentRunes+1),
		"a second line":    "before the upgrade\nand the move",
		"an escape of any": "before\x1b[31m the upgrade",
	} {
		if err := v.Protect("rb5009", id, comment, "tom", now); !errors.Is(err, ErrBadComment) {
			t.Errorf("Protect with a %s comment = %v, want ErrBadComment", name, err)
		}
	}
	// The cap itself is allowed -- 1 to 120 inclusive.
	if err := v.Protect("rb5009", id, strings.Repeat("x", MaxCommentRunes), "tom", now); err != nil {
		t.Fatalf("Protect with a comment at the cap: %v", err)
	}
	if err := v.Protect("rb5009", id, "again", "tom", now); !errors.Is(err, ErrAlreadyProtected) {
		t.Errorf("Protect on a kept generation = %v, want ErrAlreadyProtected", err)
	}
	if err := v.Protect("rb5009", "no-such-generation", "why", "tom", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("Protect on an unknown generation = %v, want ErrNotFound", err)
	}
	if err := v.Protect("no-such-router", id, "why", "tom", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("Protect on an unknown router = %v, want ErrNotFound", err)
	}
}

// TestKeptGenerationsDoNotCountTowardsTheTen is the pool's first
// promise: ordinary retention never evicts one, and holding one does
// not cost the router a slot in the cycling set.
func TestKeptGenerationsDoNotCountTowardsTheTen(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	keptID := generationIDs(v, "rb5009")[0]
	if err := v.Protect("rb5009", keptID, "before the office move", "tom", base); err != nil {
		t.Fatalf("Protect: %v", err)
	}

	for i := 1; i <= MaxGenerations+5; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}

	if got := len(generationIDs(v, "rb5009")); got != MaxGenerations {
		t.Errorf("cycling set = %d, want %d", got, MaxGenerations)
	}
	if got := protectedIDs(v, "rb5009"); len(got) != 1 || got[0] != keptID {
		t.Fatalf("kept pool = %v, want just %s", got, keptID)
	}
	if !fileIsThere(t, v, "rb5009", keptID, KindBackup) {
		t.Error("the kept generation's .backup was deleted by ordinary retention")
	}
	// The totals count it: it is a real pair of files on this disk.
	if got := v.Stats().Generations; got != MaxGenerations+1 {
		t.Errorf("Stats().Generations = %d, want %d (the ten plus the kept one)", got, MaxGenerations+1)
	}
}

// TestUnprotectPutsItBackWhereTheCapAppliesAgain is the sentence the
// release control says out loud: it goes back into the ten, and the
// oldest may go.
func TestUnprotectPutsItBackWhereTheCapAppliesAgain(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	// A full cycling set with the newest generation kept out of it, so
	// what the release costs is somebody else's slot rather than its
	// own: released into a set of ten, it is not the oldest.
	for i := 0; i <= MaxGenerations; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	held := generationIDs(v, "rb5009")
	keptID := held[len(held)-1]
	if err := v.Protect("rb5009", keptID, "before the office move", "tom", base); err != nil {
		t.Fatalf("Protect: %v", err)
	}
	mustStore(t, v, "rb5009", 99, base.Add(time.Duration(MaxGenerations+1)*time.Hour))
	before := generationIDs(v, "rb5009")
	if len(before) != MaxGenerations {
		t.Fatalf("cycling set before the release = %d, want %d", len(before), MaxGenerations)
	}
	oldest := before[0]

	if err := v.Unprotect("rb5009", keptID); err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if got := protectedIDs(v, "rb5009"); len(got) != 0 {
		t.Errorf("kept pool = %v, want empty", got)
	}
	after := generationIDs(v, "rb5009")
	if len(after) != MaxGenerations {
		t.Fatalf("cycling set = %d, want %d", len(after), MaxGenerations)
	}
	// The released one is back among the ten, in its own place by age,
	// and the generation that used to be oldest has gone with its files.
	if !contains(after, keptID) {
		t.Errorf("cycling set = %v, want the released %s among them", after, keptID)
	}
	if after[len(after)-2] != keptID {
		t.Errorf("released generation sits at %v, want it second-newest, in id order", after)
	}
	if contains(after, oldest) {
		t.Errorf("%s is still held: releasing a kept backup should have let the oldest go", oldest)
	}
	if fileIsThere(t, v, "rb5009", oldest, KindBackup) {
		t.Errorf("%s's .backup is still on disk after it was evicted", oldest)
	}
	// Its comment went with the keeping.
	for _, g := range v.Generations("rb5009") {
		if g.ID == keptID && (g.Comment != "" || g.ProtectedBy != "") {
			t.Errorf("released generation still carries %q by %q", g.Comment, g.ProtectedBy)
		}
	}
	if err := v.Unprotect("rb5009", keptID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Unprotect on a generation that is not kept = %v, want ErrNotFound", err)
	}
}

// TestLowSpaceCyclingNeverReachesTheKeptPool is #1125 meeting #1126:
// the mode replaces the oldest ordinary generation on every arrival,
// and a kept one is not an ordinary generation.
func TestLowSpaceCyclingNeverReachesTheKeptPool(t *testing.T) {
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, t.TempDir(), testKey(t), disk)
	base := time.Now()
	for i := 0; i < 3; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
	}
	ids := generationIDs(v, "rb5009")
	keptID := ids[0]
	if err := v.Protect("rb5009", keptID, "before the 7.16 upgrade", "tom", base); err != nil {
		t.Fatalf("Protect: %v", err)
	}

	disk.set(testDiskTight)
	for i := 3; i < 9; i++ {
		mustStore(t, v, "rb5009", 10+i, base.Add(time.Duration(i)*time.Hour))
		if !v.LowSpace() {
			t.Fatalf("arrival %d: LowSpace() = false on a disk under the floor", i)
		}
		if got := protectedIDs(v, "rb5009"); len(got) != 1 || got[0] != keptID {
			t.Fatalf("arrival %d: kept pool = %v, want just %s", i, got, keptID)
		}
		if !fileIsThere(t, v, "rb5009", keptID, KindBackup) {
			t.Fatalf("arrival %d: the kept generation's .backup was cycled out", i)
		}
	}

	// Releasing one while the mode is on does not evict anything: the
	// set does not grow past the cap, and low-space cycling is not
	// retention.
	if err := v.Unprotect("rb5009", keptID); err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if !contains(generationIDs(v, "rb5009"), keptID) {
		t.Error("the released generation is not back in the cycling set")
	}
}

// TestKeptPoolSurvivesAReopenAndTheUnreferencedSweep covers both halves
// of reconcile: the index remembers the pool and its comments, and the
// sweep that deletes files nothing refers to leaves them alone.
func TestKeptPoolSurvivesAReopenAndTheUnreferencedSweep(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	if err := v.Store("rb5009", KindRsc, []byte("export text"), base.Add(time.Second)); err != nil {
		t.Fatalf("Store .rsc: %v", err)
	}
	keptID := generationIDs(v, "rb5009")[0]
	if err := v.Protect("rb5009", keptID, "last config before the office move", "tom", base); err != nil {
		t.Fatalf("Protect: %v", err)
	}
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))

	reopened := openVaultOnDisk(t, dir, key, disk)
	kept := reopened.ProtectedGenerations("rb5009")
	if len(kept) != 1 || kept[0].ID != keptID {
		t.Fatalf("kept pool after a reopen = %v, want just %s", kept, keptID)
	}
	if kept[0].Comment != "last config before the office move" || kept[0].ProtectedBy != "tom" {
		t.Errorf("kept pool lost its comment across the reopen: %q by %q", kept[0].Comment, kept[0].ProtectedBy)
	}
	for _, k := range []string{KindBackup, KindRsc} {
		if !fileIsThere(t, reopened, "rb5009", keptID, k) {
			t.Errorf("the kept generation's .%s was swept as unreferenced on the way back up", k)
		}
	}
	if _, err := reopened.Open("rb5009", keptID, KindBackup); err != nil {
		t.Errorf("Open on a kept generation after a reopen: %v", err)
	}
}

// TestKeptPoolAloneKeepsTheRouterListed: a router whose only stored
// backup is a kept one still appears, or the pool would vanish from
// the screen the moment the cycling set emptied.
func TestKeptPoolAloneKeepsTheRouterListed(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	disk := &fakeDisk{free: testDiskRoomy, total: testDiskTotal}
	v := openVaultOnDisk(t, dir, key, disk)
	now := time.Now()
	mustStore(t, v, "rb5009", 10, now)
	keptID := generationIDs(v, "rb5009")[0]
	if err := v.Protect("rb5009", keptID, "the only one", "tom", now); err != nil {
		t.Fatalf("Protect: %v", err)
	}

	reopened := openVaultOnDisk(t, dir, key, disk)
	routers := reopened.Routers()
	if len(routers) != 1 || routers[0] != "rb5009" {
		t.Fatalf("Routers() = %v, want rb5009 -- its only backup is a kept one", routers)
	}
	if got := reopened.Stats().Routers; got != 1 {
		t.Errorf("Stats().Routers = %d, want 1", got)
	}
	// The learned interval reads the pool too, or a router whose newest
	// push was kept would look silent (#1126).
	if reopened.Missed("rb5009", now).LastArrival.IsZero() {
		t.Error("Missed() reports no last arrival for a router whose only backup is kept")
	}
}

func TestSetCommentRewritesOnlyAKeptGenerationsNote(t *testing.T) {
	v := openVault(t, testKey(t))
	base := time.Now()
	mustStore(t, v, "rb5009", 10, base)
	mustStore(t, v, "rb5009", 11, base.Add(time.Hour))
	ids := generationIDs(v, "rb5009")
	if err := v.Protect("rb5009", ids[0], "before the upgrade", "tom", base); err != nil {
		t.Fatalf("Protect: %v", err)
	}

	if err := v.SetComment("rb5009", ids[0], "before the 7.16 upgrade"); err != nil {
		t.Fatalf("SetComment: %v", err)
	}
	if got := v.ProtectedGenerations("rb5009")[0].Comment; got != "before the 7.16 upgrade" {
		t.Errorf("comment = %q, want the rewritten one", got)
	}
	if err := v.SetComment("rb5009", ids[0], ""); !errors.Is(err, ErrBadComment) {
		t.Errorf("SetComment with an empty comment = %v, want ErrBadComment", err)
	}
	if got := v.ProtectedGenerations("rb5009")[0].Comment; got != "before the 7.16 upgrade" {
		t.Errorf("a refused comment changed the stored one to %q", got)
	}
	if err := v.SetComment("rb5009", ids[1], "why"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetComment on a cycling generation = %v, want ErrNotFound", err)
	}
}

func TestKeepControlsRefuseWithNoRetentionKey(t *testing.T) {
	v := openVault(t, nil)
	now := time.Now()
	if err := v.Protect("rb5009", "g0", "why", "tom", now); !errors.Is(err, ErrDisabled) {
		t.Errorf("Protect with no key = %v, want ErrDisabled", err)
	}
	if err := v.Unprotect("rb5009", "g0"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Unprotect with no key = %v, want ErrDisabled", err)
	}
	if err := v.SetComment("rb5009", "g0", "why"); !errors.Is(err, ErrDisabled) {
		t.Errorf("SetComment with no key = %v, want ErrDisabled", err)
	}
}
