// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// testPassphrase is long enough to clear MinPassphraseRunes.
const testPassphrase = "correct horse battery staple"

// openVaultAt is openVault for the tests that need to reopen the same
// directory, which is where the interesting claims live: what a fresh
// process can and cannot read off the disk.
func openVaultAt(t *testing.T, dir string, key *retention.Key) *Vault {
	t.Helper()
	v, err := Open(dir, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

func TestNoPassphraseLeavesTheVaultAsItWas(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	body := plainBackup(64)
	if err := v.Store("rb5009", KindBackup, body, now); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if v.PassphraseSet() {
		t.Fatal("PassphraseSet() = true on a vault with no passphrase")
	}
	if v.Locked() {
		t.Fatal("Locked() = true on a vault with no passphrase")
	}
	gen := v.Generations("rb5009")[0]
	got, err := v.Open("rb5009", gen.ID, KindBackup)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("the file did not round-trip")
	}
}

func TestSetPassphraseResealsWhatIsAlreadyStored(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultAt(t, dir, key)
	now := time.Now()
	body := plainBackup(64)
	if err := v.Store("rb5009", KindBackup, body, now); err != nil {
		t.Fatalf("Store: %v", err)
	}
	gen := v.Generations("rb5009")[0]
	path := filepath.Join(v.routerDir("rb5009"), v.fileName(gen.ID, KindBackup))

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the sealed file: %v", err)
	}
	if bytes.HasPrefix(before, []byte(hybridMagic)) {
		t.Fatal("a file stored with no passphrase is already sealed to a public key")
	}

	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the re-sealed file: %v", err)
	}
	if !bytes.HasPrefix(after, []byte(hybridMagic)) {
		t.Fatal("the file already in the vault was not re-sealed to the vault key -- turning the lock on left it readable")
	}
	// The point of the re-seal: the retention key alone no longer opens
	// it. This is the claim the whole feature rests on, so it is asserted
	// against the raw bytes on disk rather than through any vault method.
	info := sealInfoPrefix + "rb5009/" + gen.ID + "/" + KindBackup
	if _, err := key.OpenDocument(info, after); err == nil {
		t.Fatal("the retention key still opens the stored file after a passphrase was set")
	}

	// Still readable through the vault, which is unlocked from the set.
	got, err := v.Open("rb5009", gen.ID, KindBackup)
	if err != nil {
		t.Fatalf("Open while unlocked: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("the re-sealed file did not round-trip")
	}
}

func TestLockedVaultStillAcceptsPushesButRefusesReads(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	v.Lock()
	if !v.Locked() {
		t.Fatal("Locked() = false after Lock()")
	}

	// The scheduled push must still land: nobody is at the keyboard when
	// the router runs its script.
	arrived := plainBackup(128)
	if err := v.Store("rb5009", KindBackup, arrived, now); err != nil {
		t.Fatalf("Store while locked: %v", err)
	}
	gens := v.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("Generations while locked = %d, want 1", len(gens))
	}
	if _, err := v.Open("rb5009", gens[0].ID, KindBackup); !errors.Is(err, ErrLocked) {
		t.Fatalf("Open while locked = %v, want ErrLocked", err)
	}

	if err := v.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	got, err := v.Open("rb5009", gens[0].ID, KindBackup)
	if err != nil {
		t.Fatalf("Open after Unlock: %v", err)
	}
	if !bytes.Equal(got, arrived) {
		t.Fatal("the file stored while locked did not round-trip after unlocking")
	}
}

func TestReopenedVaultIsLockedAndTheKeyFileAloneDoesNotOpenIt(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultAt(t, dir, key)
	now := time.Now()
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	body := plainBackup(32)
	if err := v.Store("rb5009", KindBackup, body, now); err != nil {
		t.Fatalf("Store: %v", err)
	}
	gen := v.Generations("rb5009")[0]

	// A fresh process, holding the retention key exactly as `-backup`
	// and a restore do, and nothing else.
	reopened := openVaultAt(t, dir, key)
	if !reopened.PassphraseSet() {
		t.Fatal("the reopened vault does not know a passphrase is set")
	}
	if !reopened.Locked() {
		t.Fatal("the reopened vault came up unlocked")
	}
	if _, err := reopened.Open("rb5009", gen.ID, KindBackup); !errors.Is(err, ErrLocked) {
		t.Fatalf("Open on a reopened vault = %v, want ErrLocked", err)
	}
	// The index is deliberately still readable: an admin can see that
	// backups are arriving without being able to read one.
	if len(reopened.Generations("rb5009")) != 1 {
		t.Fatal("the reopened vault cannot list its generations")
	}
	if err := reopened.Unlock(testPassphrase); err != nil {
		t.Fatalf("Unlock on a reopened vault: %v", err)
	}
	got, err := reopened.Open("rb5009", gen.ID, KindBackup)
	if err != nil {
		t.Fatalf("Open after unlocking a reopened vault: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("the file did not round-trip across a reopen")
	}
}

func TestWrongPassphraseIsRefusedAndLeavesTheVaultLocked(t *testing.T) {
	v := openVault(t, testKey(t))
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(16), time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	gen := v.Generations("rb5009")[0]
	v.Lock()

	for _, wrong := range []string{
		"correct horse battery stapl",  // one character short of right
		"Correct horse battery staple", // one case away
		"",                             // empty
	} {
		if err := v.Unlock(wrong); !errors.Is(err, ErrWrongPassphrase) {
			t.Fatalf("Unlock(%q) = %v, want ErrWrongPassphrase", wrong, err)
		}
		if !v.Locked() {
			t.Fatalf("Unlock(%q) left the vault unlocked", wrong)
		}
		if _, err := v.Open("rb5009", gen.ID, KindBackup); !errors.Is(err, ErrLocked) {
			t.Fatalf("Open after a refused unlock = %v, want ErrLocked", err)
		}
	}
}

func TestPassphraseLengthFloorAndDoubleSetRefused(t *testing.T) {
	v := openVault(t, testKey(t))
	if err := v.SetPassphrase("short"); !errors.Is(err, ErrPassphraseTooShort) {
		t.Fatalf("SetPassphrase(short) = %v, want ErrPassphraseTooShort", err)
	}
	if v.PassphraseSet() {
		t.Fatal("a refused passphrase was set anyway")
	}
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	if err := v.SetPassphrase("another passphrase entirely"); !errors.Is(err, ErrPassphraseSet) {
		t.Fatalf("second SetPassphrase = %v, want ErrPassphraseSet", err)
	}
}

func TestUnlockAndRemoveNeedAPassphraseToExist(t *testing.T) {
	v := openVault(t, testKey(t))
	if err := v.Unlock(testPassphrase); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("Unlock with no passphrase set = %v, want ErrNoPassphrase", err)
	}
	if err := v.RemovePassphrase(testPassphrase); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("RemovePassphrase with none set = %v, want ErrNoPassphrase", err)
	}
}

func TestRemovePassphraseNeedsThePassphraseAndRestoresPlainReads(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultAt(t, dir, key)
	body := plainBackup(48)
	if err := v.Store("rb5009", KindBackup, body, time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	gen := v.Generations("rb5009")[0]

	if err := v.RemovePassphrase("not the passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("RemovePassphrase(wrong) = %v, want ErrWrongPassphrase", err)
	}
	if !v.PassphraseSet() {
		t.Fatal("a refused removal took the passphrase off anyway")
	}

	if err := v.RemovePassphrase(testPassphrase); err != nil {
		t.Fatalf("RemovePassphrase: %v", err)
	}
	if v.PassphraseSet() || v.Locked() {
		t.Fatal("the passphrase survived its own removal")
	}
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(err) {
		t.Fatalf("the lock file survived removal: %v", err)
	}

	// A fresh process with the retention key alone reads it again.
	reopened := openVaultAt(t, dir, key)
	got, err := reopened.Open("rb5009", gen.ID, KindBackup)
	if err != nil {
		t.Fatalf("Open after removal: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("the file did not round-trip after the passphrase was removed")
	}
}

func TestSealedBodyCannotBeMovedToAnotherSlot(t *testing.T) {
	v := openVault(t, testKey(t))
	now := time.Now()
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(24), now); err != nil {
		t.Fatalf("Store rb5009: %v", err)
	}
	if err := v.Store("hex-s", KindBackup, plainBackup(24), now); err != nil {
		t.Fatalf("Store hex-s: %v", err)
	}
	victim := v.Generations("rb5009")[0]
	attacker := v.Generations("hex-s")[0]

	// Move one router's sealed body into the other's slot. The seal binds
	// the file to its slot, so this must fail rather than decrypt as
	// somebody else's backup.
	src := filepath.Join(v.routerDir("hex-s"), v.fileName(attacker.ID, KindBackup))
	dst := filepath.Join(v.routerDir("rb5009"), v.fileName(victim.ID, KindBackup))
	moved, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading the sealed file: %v", err)
	}
	if err := os.WriteFile(dst, moved, 0o600); err != nil {
		t.Fatalf("writing the moved file: %v", err)
	}
	if _, err := v.Open("rb5009", victim.ID, KindBackup); err == nil {
		t.Fatal("a sealed body moved into another router's slot opened there")
	}
}

func TestTamperedEphemeralKeyIsRefused(t *testing.T) {
	v := openVault(t, testKey(t))
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	if err := v.Store("rb5009", KindBackup, plainBackup(24), time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	gen := v.Generations("rb5009")[0]
	path := filepath.Join(v.routerDir("rb5009"), v.fileName(gen.ID, KindBackup))
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the sealed file: %v", err)
	}
	sealed[len(hybridMagic)] ^= 0xff // flip a bit of the ephemeral key
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatalf("writing the tampered file: %v", err)
	}
	if _, err := v.Open("rb5009", gen.ID, KindBackup); err == nil {
		t.Fatal("a file with a tampered ephemeral key opened anyway")
	}
}

// The three races #1119 found, each driven from inside a re-seal pass
// through resealStep so the window is hit every run rather than when the
// scheduler happens to oblige.

func TestABackupArrivingDuringRemovePassphraseStaysReadable(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultAt(t, dir, key)
	now := time.Now()
	first := plainBackup(64)
	if err := v.Store("rb5009", KindBackup, first, now); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}

	// The router pushes while the passphrase is being taken off. Nothing
	// in the vault's API tells it that is happening, which is the point.
	arrived := plainBackup(128)
	var once sync.Once
	var storeErr error
	resealStep = func() {
		once.Do(func() { storeErr = v.Store("rb5009", KindBackup, arrived, now.Add(time.Minute)) })
	}
	t.Cleanup(func() { resealStep = nil })

	if err := v.RemovePassphrase(testPassphrase); err != nil {
		t.Fatalf("RemovePassphrase: %v", err)
	}
	if storeErr != nil {
		t.Fatalf("the push that landed mid-removal was refused: %v", storeErr)
	}

	gens := v.Generations("rb5009")
	if len(gens) != 2 {
		t.Fatalf("the vault holds %d generations, want 2", len(gens))
	}
	var found bool
	for _, g := range gens {
		body, err := v.Open("rb5009", g.ID, KindBackup)
		if err != nil {
			t.Fatalf("Open(%s) after the passphrase was removed: %v", g.ID, err)
		}
		if bytes.Equal(body, arrived) {
			found = true
		}
	}
	if !found {
		t.Fatal("the backup that arrived mid-removal did not read back")
	}
}

func TestConcurrentSetPassphraseLeavesEveryFileReadable(t *testing.T) {
	dir := t.TempDir()
	key := testKey(t)
	v := openVaultAt(t, dir, key)
	now := time.Now()
	bodies := map[string][]byte{}
	for i := range 3 {
		body := plainBackup(48 + i)
		if err := v.Store("rb5009", KindBackup, body, now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("Store: %v", err)
		}
		gens := v.Generations("rb5009")
		bodies[gens[len(gens)-1].ID] = body
	}

	phrases := []string{"the first passphrase here", "the second passphrase here"}
	errs := make([]error, len(phrases))
	var wg sync.WaitGroup
	for i, phrase := range phrases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = v.SetPassphrase(phrase)
		}()
	}
	wg.Wait()

	winner := ""
	for i, err := range errs {
		if err == nil {
			if winner != "" {
				t.Fatal("both concurrent SetPassphrase calls reported success")
			}
			winner = phrases[i]
			continue
		}
		if !errors.Is(err, ErrPassphraseSet) && !errors.Is(err, ErrPassphraseBusy) {
			t.Fatalf("the losing SetPassphrase returned %v, want ErrPassphraseSet or ErrPassphraseBusy", err)
		}
	}
	if winner == "" {
		t.Fatal("neither concurrent SetPassphrase call succeeded")
	}

	// A fresh process, reading only what is on disk: whatever the two
	// calls did between them, the passphrase that won must open every
	// file the vault holds.
	reopened := openVaultAt(t, dir, key)
	if err := reopened.Unlock(winner); err != nil {
		t.Fatalf("Unlock with the passphrase that won: %v", err)
	}
	for id, want := range bodies {
		got, err := reopened.Open("rb5009", id, KindBackup)
		if err != nil {
			t.Fatalf("Open(%s): %v", id, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s did not round-trip", id)
		}
	}
}

func TestUnlockRacingRemovePassphraseDoesNotRevive(t *testing.T) {
	dir := t.TempDir()
	v := openVaultAt(t, dir, testKey(t))
	if err := v.Store("rb5009", KindBackup, plainBackup(64), time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	v.Lock()

	// The unlock is started from inside the removal's re-seal pass, so it
	// is still stretching the passphrase when the lock document goes.
	unlocked := make(chan error, 1)
	var once sync.Once
	resealStep = func() {
		once.Do(func() {
			go func() { unlocked <- v.Unlock(testPassphrase) }()
		})
	}
	t.Cleanup(func() { resealStep = nil })

	if err := v.RemovePassphrase(testPassphrase); err != nil {
		t.Fatalf("RemovePassphrase: %v", err)
	}
	if err := <-unlocked; !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("an unlock that outlived the lock it read returned %v, want ErrNoPassphrase", err)
	}
	if v.PassphraseSet() || v.Locked() {
		t.Fatal("the removed passphrase came back")
	}
}

func TestARemovalThatCannotFinishLeavesTheVaultLocked(t *testing.T) {
	dir := t.TempDir()
	v := openVaultAt(t, dir, testKey(t))
	if err := v.Store("rb5009", KindBackup, plainBackup(64), time.Now()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := v.SetPassphrase(testPassphrase); err != nil {
		t.Fatalf("SetPassphrase: %v", err)
	}
	v.Lock()

	// One file the conversion cannot open, which is what a half-written
	// disk or a bad sector looks like from here.
	gen := v.Generations("rb5009")[0]
	path := filepath.Join(v.routerDir("rb5009"), v.fileName(gen.ID, KindBackup))
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := hybridHeaderBytes; i < len(sealed); i++ {
		sealed[i] ^= 0xff
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}

	err = v.RemovePassphrase(testPassphrase)
	if !errors.Is(err, ErrResealIncomplete) {
		t.Fatalf("RemovePassphrase over an unreadable file = %v, want ErrResealIncomplete", err)
	}
	if !v.PassphraseSet() {
		t.Fatal("a removal that could not finish took the passphrase off anyway")
	}
	// Removing unlocks the vault to do its work. When it cannot finish,
	// the key has to go back: the vault was locked when this started, and
	// no admin is holding it open (#1120).
	if !v.Locked() {
		t.Fatal("a removal that could not finish left the private key in memory")
	}
}

// TestSealingToTheVaultKeyDoesNotCopyTheBody measures bytes rather than
// counting allocations: the interesting number here is not how many
// times this path allocates -- key agreement and hashing account for
// most of that -- but whether a 16MiB backup passes through memory once
// or twice (#1121).
func TestSealingToTheVaultKeyDoesNotCopyTheBody(t *testing.T) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const info = sealInfoPrefix + "rb5009/gen/backup"
	plain := bytes.Repeat([]byte("x"), 4<<20)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	sealed, err := sealToPublic(priv.PublicKey(), info, plain)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("sealToPublic: %v", err)
	}

	allocated := after.TotalAlloc - before.TotalAlloc
	if limit := uint64(len(plain)) * 3 / 2; allocated > limit {
		t.Fatalf("sealing a %d-byte backup allocated %d bytes, want under %d -- the body is being copied a second time",
			len(plain), allocated, limit)
	}
	if want := hybridHeaderBytes + retention.SealOverheadBytes + len(plain); len(sealed) != want {
		t.Fatalf("the sealed file is %d bytes, want %d", len(sealed), want)
	}
	if len(sealed) != cap(sealed) {
		t.Fatalf("a %d-byte file was assembled in a %d-byte buffer", len(sealed), cap(sealed))
	}
	if !bytes.HasPrefix(sealed, []byte(hybridMagic)) {
		t.Fatal("the sealed file does not start with the hybrid magic")
	}
	ephPub := sealed[len(hybridMagic):hybridHeaderBytes]
	if _, err := ecdh.X25519().NewPublicKey(ephPub); err != nil {
		t.Fatalf("the header does not hold a usable ephemeral key: %v", err)
	}
	if bytes.Equal(ephPub, priv.PublicKey().Bytes()) {
		t.Fatal("the header holds the vault's own public key, not a throwaway one")
	}

	got, err := openFromPrivate(priv, info, sealed)
	if err != nil {
		t.Fatalf("openFromPrivate: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("the body did not round-trip")
	}
}
