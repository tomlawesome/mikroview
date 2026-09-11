// SPDX-License-Identifier: AGPL-3.0-only

package backupvault

// The optional admin passphrase (#956): a second lock on the vault that
// mikroview itself does not hold the key to.
//
// The shape of the problem, which is what makes this more than a
// key-wrap. The router pushes on its own schedule, and nobody is at the
// keyboard when it does. So the vault must go on *accepting* backups
// while it is locked, and must not be able to *read* any of them until
// an admin unlocks it. A single symmetric key wrapped under the
// passphrase cannot do that: sealing a new arrival would need the same
// key reading one does, so either the passphrase is required to receive
// a backup -- and a locked vault silently loses every scheduled push --
// or the key sits in memory and the lock is decoration.
//
// So the lock is an X25519 key pair. The public half is stored beside
// the vault and is all that is needed to seal an arriving file; the
// private half is sealed under the passphrase and exists in memory only
// between an admin unlocking and the unlock ending. Writing needs the
// public half, reading needs the private half, and that asymmetry is
// exactly the asymmetry the feature describes.
//
// What this does and does not buy, stated plainly so nobody over-reads
// it, in the same terms internal/retention states its own limits.
// Copying the data directory while the vault is locked -- or restoring
// it from a `-backup` bundle -- yields file bodies nobody can open
// without the passphrase, mikroview's own operator included. While an
// admin has it unlocked, the private key is in this process's memory and
// root on the running host can reach it, because the process must hold
// it to serve a download. The lock narrows the window to the unlock; it
// does not abolish it.
//
// The cost is the one #394 deliberately removed from the router side and
// this feature knowingly puts back: a lost passphrase loses the backups.
// There is no recovery path here on purpose -- one that worked for the
// operator would work for anyone who reached the disk, which is the
// property the whole feature exists to provide. The setting is off by
// default, and there is no screen for it yet -- the controls are
// API-only, so docs/configuration.md is where an operator is told this
// before they turn it on.

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/retention"
)

var (
	// ErrLocked reports that a passphrase is configured and the vault is
	// not unlocked, so the file cannot be read. Stored files keep
	// arriving in this state -- it refuses reads, never writes.
	ErrLocked = errors.New("backupvault: the vault is locked -- an admin must unlock it with the passphrase")
	// ErrNoPassphrase reports that the caller asked to unlock, change or
	// remove a passphrase on a vault that has none set.
	ErrNoPassphrase = errors.New("backupvault: no vault passphrase is set")
	// ErrPassphraseSet reports an attempt to set a passphrase on a vault
	// that already has one. Removing it needs the current one, so this is
	// never a route around that.
	ErrPassphraseSet = errors.New("backupvault: a vault passphrase is already set")
	// ErrWrongPassphrase reports a passphrase that did not open the lock.
	// It is deliberately the only failure a caller can tell apart: a
	// malformed lock document and a wrong passphrase both surface here,
	// so nothing distinguishes "close" from "wrong".
	ErrWrongPassphrase = errors.New("backupvault: that passphrase does not open the vault")
	// ErrPassphraseTooShort reports a passphrase below MinPassphraseRunes.
	ErrPassphraseTooShort = fmt.Errorf("backupvault: the vault passphrase must be at least %d characters", MinPassphraseRunes)
	// ErrPassphraseBusy reports that another passphrase change is
	// already running. Setting, changing and removing a passphrase each
	// re-seal every stored file, so they cannot overlap: the loser is
	// refused outright rather than queued, because a caller waiting
	// behind a whole-vault conversion cannot tell that from a hang.
	ErrPassphraseBusy = errors.New("backupvault: another vault passphrase change is in progress -- try again when it has finished")
	// ErrResealIncomplete reports a passphrase change that was recorded
	// but did not convert every stored file -- a disk that filled, a
	// file that would not open. Every file still says for itself which
	// key seals it (see hybridMagic), so nothing is lost; the vault is
	// simply a mix of both schemes until the operation is retried, and
	// the caller is told rather than left to guess.
	ErrResealIncomplete = errors.New("backupvault: the passphrase change was recorded but not every stored backup was re-sealed")
)

// MinPassphraseRunes is the shortest passphrase accepted.
//
// Twelve, and the reasoning is different from a login password's. This
// one wraps a key on disk, so an attacker who copies the data directory
// can grind at it offline for as long as they like, with no login rate
// limiter in the way -- Argon2id's cost is the only thing slowing them
// down. Longer would be safer still, but a passphrase an admin cannot
// hold in their head gets written on something, and the failure mode
// here is total: see this file's header on why there is no recovery.
const MinPassphraseRunes = 12

// lockFileName holds the wrapped private key. It is sealed under the
// retention key like everything else here, which hides the Argon2id salt
// and parameters from a casual copy of the directory. That is depth, not
// the defence: the security of the lock rests on the passphrase, and the
// scheme assumes an attacker who has this file in the clear.
const lockFileName = "lock.enc"

// lockDocVersion is the on-disk shape's version. Anything else is
// refused rather than guessed at -- a vault written by a future version
// must not be opened by an older one that would misread the fields
// protecting it.
const lockDocVersion = 1

// hybridMagic prefixes a file body sealed to the vault public key,
// distinguishing it from one sealed directly under the retention key,
// which starts with internal/retention's own "MVS1". Per-file rather
// than recorded in the index, so a vault caught mid-migration -- the
// process died while turning the passphrase on or off -- still opens
// every file it holds: each one says for itself which key seals it.
const hybridMagic = "MVBL1"

// hybridHeaderBytes is the magic plus the ephemeral public key that
// follows it.
const hybridHeaderBytes = len(hybridMagic) + 32

// lockPrivateInfo namespaces the seal over the private key itself.
const lockPrivateInfo = sealInfoPrefix + "lock/private"

// lockDoc is the persisted lock: everything needed to seal a new arrival
// without the passphrase, and nothing that opens one without it.
type lockDoc struct {
	Version int `json:"version"`
	// PublicKey is the X25519 public half, in the raw 32-byte form
	// crypto/ecdh uses.
	PublicKey []byte `json:"publicKey"`
	// Salt and KDF are what the passphrase was stretched with. Recorded
	// rather than assumed from the current constants, for the reason
	// internal/auth's hash strings record theirs: raising the cost later
	// must not make an existing vault unopenable.
	Salt []byte         `json:"salt"`
	KDF  auth.KDFParams `json:"kdf"`
	// WrappedPrivate is the X25519 private half sealed under the
	// passphrase-derived key.
	WrappedPrivate []byte `json:"wrappedPrivate"`
}

// lockState is the in-memory half: the parsed document, plus the private
// key when and only when an admin has unlocked.
type lockState struct {
	doc  lockDoc
	pub  *ecdh.PublicKey
	priv *ecdh.PrivateKey
	// changing is set while a passphrase operation is converting the
	// vault, and means one thing to everything else: seal an arriving
	// backup under the retention key rather than to pub.
	//
	// It is what makes the two long operations safe to run while routers
	// keep pushing (#1119). Turning the lock on, it holds new arrivals
	// back until the lock document is on disk, because a file sealed to
	// a public key whose private half was never persisted is lost for
	// good. Turning it off, it puts new arrivals straight into the
	// scheme the whole vault is moving to, so the file that lands
	// half-way through the pass cannot be stranded by the delete at the
	// end of it.
	//
	// It doubles as the claim that keeps two passphrase operations from
	// overlapping: a caller that finds it set is refused with
	// ErrPassphraseBusy rather than queued.
	changing bool
}

// PassphraseSet reports whether a vault passphrase is configured at all.
// A vault with none behaves exactly as it did before this feature.
func (v *Vault) PassphraseSet() bool {
	if v == nil {
		return false
	}
	v.lockMu.RLock()
	defer v.lockMu.RUnlock()
	return v.lock != nil
}

// Locked reports whether reads are currently refused: a passphrase is
// set and nobody has unlocked. Stores succeed either way.
func (v *Vault) Locked() bool {
	if v == nil {
		return false
	}
	v.lockMu.RLock()
	defer v.lockMu.RUnlock()
	return v.lock != nil && v.lock.priv == nil
}

// loadLock reads the lock document, if there is one. Called by Open.
func (v *Vault) loadLock() error {
	sealed, err := os.ReadFile(filepath.Join(v.dir, lockFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("backupvault: reading %s: %w", lockFileName, err)
	}
	plain, err := v.key.OpenDocument(lockPrivateInfo, sealed)
	if err != nil {
		return fmt.Errorf("backupvault: opening the vault lock: %w", err)
	}
	var doc lockDoc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return fmt.Errorf("backupvault: parsing the vault lock: %w", err)
	}
	if doc.Version != lockDocVersion {
		return fmt.Errorf("backupvault: the vault lock is version %d, this build understands %d", doc.Version, lockDocVersion)
	}
	if !doc.KDF.Valid() {
		return errors.New("backupvault: the vault lock records unusable key-derivation parameters")
	}
	pub, err := ecdh.X25519().NewPublicKey(doc.PublicKey)
	if err != nil {
		return fmt.Errorf("backupvault: the vault lock's public key is unusable: %w", err)
	}
	v.lockMu.Lock()
	v.lock = &lockState{doc: doc, pub: pub}
	v.lockMu.Unlock()
	return nil
}

// writeLockDoc persists doc.
func (v *Vault) writeLockDoc(doc lockDoc) error {
	plain, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("backupvault: encoding the vault lock: %w", err)
	}
	sealed, err := v.key.SealDocument(lockPrivateInfo, plain)
	if err != nil {
		return fmt.Errorf("backupvault: sealing the vault lock: %w", err)
	}
	if err := persist.WriteFileAtomic(filepath.Join(v.dir, lockFileName), sealed, 0o600); err != nil {
		return fmt.Errorf("backupvault: committing the vault lock: %w", err)
	}
	return nil
}

// wrapKeyFor stretches passphrase with the recorded salt and parameters
// and returns the retention.Key the private half is sealed under.
func wrapKeyFor(passphrase string, salt []byte, params auth.KDFParams) (*retention.Key, error) {
	material := auth.DeriveKey(passphrase, salt, params)
	k, err := retention.NewKeyFromMaterial(material)
	if err != nil {
		return nil, fmt.Errorf("backupvault: deriving the passphrase key: %w", err)
	}
	return k, nil
}

// SetPassphrase turns the lock on.
//
// Every file already in the vault is re-sealed to the new public key, so
// that turning the lock on protects the backups already held rather than
// only the next one to arrive. The lock document is written first: a
// re-sealed file whose key was never persisted would be unreadable by
// anyone, which is data loss rather than security. Interrupted part-way
// the vault is a mix of both schemes, which reads correctly -- see
// hybridMagic.
//
// The vault is left unlocked. The admin has just typed the passphrase;
// making them type it again to see the effect of what they set would
// teach nothing.
func (v *Vault) SetPassphrase(passphrase string) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	if utf8.RuneCountInString(passphrase) < MinPassphraseRunes {
		return ErrPassphraseTooShort
	}

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("backupvault: generating the vault key pair: %w", err)
	}

	// Claim the vault before any of the slow work, in one step that both
	// tests and takes (#1119). Asking PassphraseSet() and then setting
	// the lock a tenth of a second later -- which is what stretching a
	// passphrase costs -- let two callers both pass the test, and the
	// loser's re-seal then ran against the winner's key, leaving files
	// nobody could ever open.
	st := &lockState{pub: priv.PublicKey(), priv: priv, changing: true}
	v.lockMu.Lock()
	switch {
	case v.lock == nil:
		v.lock = st
		v.lockMu.Unlock()
	case v.lock.changing:
		v.lockMu.Unlock()
		return ErrPassphraseBusy
	default:
		v.lockMu.Unlock()
		return ErrPassphraseSet
	}

	salt, err := auth.NewKDFSalt()
	if err != nil {
		return v.abandonChange(st, fmt.Errorf("backupvault: %w", err))
	}
	params := auth.DefaultKDFParams()
	wrapKey, err := wrapKeyFor(passphrase, salt, params)
	if err != nil {
		return v.abandonChange(st, err)
	}
	wrapped, err := wrapKey.SealDocument(lockPrivateInfo, priv.Bytes())
	if err != nil {
		return v.abandonChange(st, fmt.Errorf("backupvault: sealing the vault private key: %w", err))
	}

	doc := lockDoc{
		Version:        lockDocVersion,
		PublicKey:      priv.PublicKey().Bytes(),
		Salt:           salt,
		KDF:            params,
		WrappedPrivate: wrapped,
	}
	if err := v.writeLockDoc(doc); err != nil {
		return v.abandonChange(st, err)
	}

	// The document is on disk, so the pair can be relied on now: an
	// arriving backup sealed to it from here can still be opened with
	// the passphrase that was just set.
	v.lockMu.Lock()
	st.doc = doc
	st.changing = false
	v.lockMu.Unlock()

	if err := v.resealAllTo(st.pub); err != nil {
		// The passphrase is set and recorded; some stored files are
		// still sealed under the retention key. Every file says for
		// itself which key seals it, so all of them still open -- but
		// the caller is told the conversion did not finish, rather than
		// being handed a bare failure for an operation that did take
		// effect (#1120).
		v.log.Warn(fmt.Sprintf("the vault passphrase was set but not every stored backup was re-sealed: %v", err))
		return fmt.Errorf("%w: %w", ErrResealIncomplete, err)
	}
	v.log.Info("a vault passphrase was set -- stored backups are now unreadable without it")
	return nil
}

// abandonChange gives up a claim SetPassphrase could not carry through,
// leaving the vault exactly as it was before the claim: no passphrase,
// no key in memory, and nothing for the next caller to collide with.
func (v *Vault) abandonChange(st *lockState, err error) error {
	v.lockMu.Lock()
	if v.lock == st {
		v.lock = nil
	}
	v.lockMu.Unlock()
	return err
}

// RemovePassphrase turns the lock off, which requires the passphrase
// that is currently on it: an admin who cannot open the vault cannot
// decide to stop protecting it.
//
// Order mirrors SetPassphrase and is equally deliberate. Every file is
// re-sealed under the retention key *first* and the lock document is
// deleted last, so an interruption leaves files whose key is still on
// disk. Deleting first would strand any file not yet converted.
func (v *Vault) RemovePassphrase(passphrase string) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	v.lockMu.RLock()
	st := v.lock
	var wasLocked, busy bool
	if st != nil {
		wasLocked, busy = st.priv == nil, st.changing
	}
	v.lockMu.RUnlock()
	if st == nil {
		return ErrNoPassphrase
	}
	if busy {
		return ErrPassphraseBusy
	}

	// The passphrase is checked before anything is claimed, and the
	// order matters. Claiming first would mean a wrong guess -- which
	// anyone who can reach the route can make, repeatedly -- diverting
	// every backup arriving in the meantime to the retention key, which
	// is precisely the protection the passphrase is there to give.
	if err := v.Unlock(passphrase); err != nil {
		return err
	}

	v.lockMu.Lock()
	if v.lock != st || st.changing || st.priv == nil {
		v.lockMu.Unlock()
		return ErrPassphraseBusy
	}
	st.changing = true
	v.lockMu.Unlock()

	if err := v.resealAllTo(nil); err != nil {
		v.restoreAfterFailedChange(st, wasLocked)
		v.log.Warn(fmt.Sprintf("the vault passphrase was not removed -- not every stored backup could be re-sealed: %v", err))
		return fmt.Errorf("%w: %w", ErrResealIncomplete, err)
	}
	if err := os.Remove(filepath.Join(v.dir, lockFileName)); err != nil && !os.IsNotExist(err) {
		v.restoreAfterFailedChange(st, wasLocked)
		return fmt.Errorf("backupvault: removing the vault lock: %w", err)
	}
	v.lockMu.Lock()
	if v.lock == st {
		v.lock = nil
	}
	v.lockMu.Unlock()
	v.log.Info("the vault passphrase was removed -- stored backups are readable with the retention key again")
	return nil
}

// restoreAfterFailedChange puts the vault back the way a removal that
// could not finish found it: nothing in flight, and the private key
// dropped again if nobody had the vault unlocked when the removal
// started (#1120). Removing a passphrase unlocks the vault to do it, and
// a key left in memory that no admin holds -- with nothing that would
// ever clear it -- is the one thing this feature promises not to leave
// behind.
func (v *Vault) restoreAfterFailedChange(st *lockState, wasLocked bool) {
	v.lockMu.Lock()
	st.changing = false
	if wasLocked {
		st.priv = nil
	}
	v.lockMu.Unlock()
}

// Unlock opens the vault for reading until Lock is called. It is
// idempotent: unlocking an already-unlocked vault with the right
// passphrase succeeds and changes nothing.
func (v *Vault) Unlock(passphrase string) error {
	if v == nil || v.key == nil {
		return ErrDisabled
	}
	v.lockMu.RLock()
	st := v.lock
	v.lockMu.RUnlock()
	if st == nil {
		return ErrNoPassphrase
	}
	if st.doc.Version != lockDocVersion {
		// A passphrase is being set this moment and its document is not
		// on disk yet. There is nothing here to unlock against.
		return ErrPassphraseBusy
	}

	wrapKey, err := wrapKeyFor(passphrase, st.doc.Salt, st.doc.KDF)
	if err != nil {
		return err
	}
	plain, err := wrapKey.OpenDocument(lockPrivateInfo, st.doc.WrappedPrivate)
	if err != nil {
		// Every failure below the passphrase -- a truncated document, a
		// tampered one -- arrives here too, and is reported the same way
		// on purpose. See ErrWrongPassphrase.
		return ErrWrongPassphrase
	}
	priv, err := ecdh.X25519().NewPrivateKey(plain)
	if err != nil {
		return ErrWrongPassphrase
	}
	if !priv.PublicKey().Equal(st.pub) {
		// The wrapped key opened but is not the pair this vault's files
		// were sealed to. Nothing good comes of continuing with it.
		return ErrWrongPassphrase
	}

	// Re-check what was read at the top before writing into it. Deriving
	// the key takes about a tenth of a second with the lock released,
	// which is long enough for the passphrase to have been removed or
	// replaced underneath (#1119): the old code dereferenced whatever
	// was there by then, which panicked when the answer was nothing and
	// wrote this pair's private key into another pair's state when it
	// was something else.
	v.lockMu.Lock()
	switch {
	case v.lock == nil:
		v.lockMu.Unlock()
		return ErrNoPassphrase
	case v.lock != st:
		v.lockMu.Unlock()
		return ErrWrongPassphrase
	}
	st.priv = priv
	v.lockMu.Unlock()
	return nil
}

// Lock drops the private key, so reads are refused again. Called when
// the unlocking admin's session ends and by the explicit lock control.
func (v *Vault) Lock() {
	if v == nil {
		return
	}
	v.lockMu.Lock()
	if v.lock != nil {
		v.lock.priv = nil
	}
	v.lockMu.Unlock()
}

// sealBody seals one file body under whichever scheme is in force.
// Sealing never needs the passphrase -- that is the point of the key
// pair -- so a locked vault still stores what a router pushes.
func (v *Vault) sealBody(info string, plain []byte) ([]byte, error) {
	v.lockMu.RLock()
	st := v.lock
	toPublic := st != nil && !st.changing
	v.lockMu.RUnlock()
	if !toPublic {
		// No passphrase, or one being turned on or off right now. Either
		// way the retention key is the scheme this file belongs in: see
		// lockState.changing.
		return v.key.SealDocument(info, plain)
	}
	return sealToPublic(st.pub, info, plain)
}

// openBody reverses sealBody, reading the scheme off the body itself
// rather than off the index. A file sealed to the public key cannot be
// read while locked, whatever the index says.
func (v *Vault) openBody(info string, sealed []byte) ([]byte, error) {
	if !bytes.HasPrefix(sealed, []byte(hybridMagic)) {
		return v.key.OpenDocument(info, sealed)
	}
	v.lockMu.RLock()
	st := v.lock
	v.lockMu.RUnlock()
	if st == nil {
		// A file sealed to a public key whose lock document is gone.
		// Unrecoverable, and worth saying so precisely rather than
		// reporting a decryption failure.
		return nil, ErrNoPassphrase
	}
	if st.priv == nil {
		return nil, ErrLocked
	}
	return openFromPrivate(st.priv, info, sealed)
}

// sealToPublic seals plain so that only the holder of pub's private half
// can open it: a throwaway key pair, X25519 to pub, and the shared
// secret used as the key material for this codebase's one AEAD scheme.
// The ephemeral public key travels in the clear ahead of the ciphertext,
// and is bound into the info string -- and so into the additional
// authenticated data -- alongside the file's own slot, so a body cannot
// be replayed into a different slot or re-headed with another ephemeral
// key.
func sealToPublic(pub *ecdh.PublicKey, info string, plain []byte) ([]byte, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("backupvault: generating an ephemeral key: %w", err)
	}
	shared, err := eph.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("backupvault: agreeing a file key: %w", err)
	}
	fileKey, err := fileKeyFromShared(shared, eph.PublicKey().Bytes(), pub.Bytes())
	if err != nil {
		return nil, err
	}
	sealed, err := fileKey.SealDocument(hybridInfo(info, eph.PublicKey().Bytes()), plain)
	if err != nil {
		return nil, fmt.Errorf("backupvault: sealing to the vault key: %w", err)
	}
	return hybridBody(eph.PublicKey().Bytes(), sealed), nil
}

// hybridBody lays out one sealed file: the magic, the ephemeral public
// key, then the ciphertext.
//
// One allocation, exactly the size of the result, and one copy of the
// body into it -- a backup can be 16MiB, so an accidental second copy is
// 16MiB of garbage per stored file (#1121). The copy that remains is the
// floor for this shape: retention.Key.SealDocument hands back a fresh
// slice with no room in front of it for a header, and putting one there
// without moving the bytes would need an append-style API in
// internal/retention.
func hybridBody(ephPub, sealed []byte) []byte {
	out := make([]byte, hybridHeaderBytes+len(sealed))
	copy(out, hybridMagic)
	copy(out[len(hybridMagic):], ephPub)
	copy(out[hybridHeaderBytes:], sealed)
	return out
}

// openFromPrivate reverses sealToPublic.
func openFromPrivate(priv *ecdh.PrivateKey, info string, sealed []byte) ([]byte, error) {
	if len(sealed) < hybridHeaderBytes {
		return nil, errors.New("backupvault: the sealed file is too short to hold its header")
	}
	ephBytes := sealed[len(hybridMagic):hybridHeaderBytes]
	eph, err := ecdh.X25519().NewPublicKey(ephBytes)
	if err != nil {
		return nil, fmt.Errorf("backupvault: the sealed file's ephemeral key is unusable: %w", err)
	}
	shared, err := priv.ECDH(eph)
	if err != nil {
		return nil, fmt.Errorf("backupvault: agreeing the file key: %w", err)
	}
	fileKey, err := fileKeyFromShared(shared, ephBytes, priv.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	return fileKey.OpenDocument(hybridInfo(info, ephBytes), sealed[hybridHeaderBytes:])
}

// fileKeyFromShared turns a raw X25519 shared secret into key material
// for one file. The secret is hashed together with both public keys
// rather than used as-is: a bare ECDH output is not uniformly random and
// carries no context, so binding the sender's and recipient's keys into
// it is what stops the same material meaning two different things.
// internal/retention's HKDF then does the actual derivation.
func fileKeyFromShared(shared, ephPub, vaultPub []byte) (*retention.Key, error) {
	sum := sha256.New()
	sum.Write([]byte("mikroview/backupvault/x25519/v1"))
	sum.Write(shared)
	sum.Write(ephPub)
	sum.Write(vaultPub)
	k, err := retention.NewKeyFromMaterial(sum.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("backupvault: deriving the file key: %w", err)
	}
	return k, nil
}

// hybridInfo is the info string -- and so the additional authenticated
// data -- a hybrid-sealed body is bound to: the slot it belongs in, plus
// the ephemeral key it was sealed with.
func hybridInfo(info string, ephPub []byte) string {
	return info + "/x25519/" + hex.EncodeToString(ephPub)
}

// resealStep, when it is not nil, is called once per file a re-seal pass
// is about to convert. A seam for this package's own tests, which have
// to put a concurrent Store or Unlock *inside* a conversion pass to
// prove the races this file is written to prevent; nil in every build
// that is not running them.
var resealStep func()

// slot names one stored file: a router, one of its generations, and
// which of the two files that generation holds.
type slot struct {
	device, generation, kind string
}

// storedSlots is every file the index currently lists.
func (v *Vault) storedSlots() []slot {
	var slots []slot
	v.mu.Lock()
	defer v.mu.Unlock()
	for device, rm := range v.meta.Routers {
		for _, g := range rm.Generations {
			if !g.BackupArrivedAt.IsZero() {
				slots = append(slots, slot{device, g.ID, KindBackup})
			}
			if !g.RscArrivedAt.IsZero() {
				slots = append(slots, slot{device, g.ID, KindRsc})
			}
		}
	}
	return slots
}

// resealProgressEvery is how often a conversion pass says where it has
// got to. A whole vault -- fifty routers, ten generations each, two
// files apiece -- is a thousand files and a couple of thousand fsyncs,
// which is minutes inside the one request that asked for it (#1121). It
// stays synchronous, because the handler must not answer before the
// vault is in one state or the other (#1119), but a log that moves is
// the difference between "slow" and "hung".
const resealProgressEvery = 100

// resealMaxPasses bounds the re-scan below. Two is the most that can do
// any work -- see resealAllTo -- and the rest is headroom, so a busy
// vault can never turn this into an unbounded loop inside a request.
const resealMaxPasses = 8

// resealAllTo re-seals every stored file to pub, or -- when pub is nil --
// directly under the retention key, which is how the passphrase is
// turned off.
//
// Each file is read, opened under whichever scheme sealed it, re-sealed
// and written atomically in place before the next is started, so an
// interruption leaves whole files in one scheme or the other and never a
// half-written one. A file already in the target scheme is left alone.
// Bounded work: ten generations per router, each at most MaxFileBytes.
//
// The index is re-scanned until a pass finds no file it has not already
// seen (#1119). Backups arrive while this runs -- the router pushes on
// its own schedule -- and the pass that deletes the lock document must
// not leave behind a file it never looked at. The second pass converts
// nothing in practice, because lockState.changing already puts an
// arrival into the scheme this is moving to; it is the proof of that,
// not a substitute for it.
func (v *Vault) resealAllTo(pub *ecdh.PublicKey) error {
	started := time.Now()
	seen := map[slot]bool{}
	// One read buffer for the whole pass rather than one per file:
	// nothing holds on to a file's sealed bytes once it has been opened,
	// and at up to MaxFileBytes each that is the largest allocation here
	// by a wide margin.
	var buf []byte
	var examined, announced int
	for pass := 0; pass < resealMaxPasses; pass++ {
		var pending []slot
		for _, s := range v.storedSlots() {
			if !seen[s] {
				seen[s] = true
				pending = append(pending, s)
			}
		}
		if len(pending) == 0 {
			break
		}
		if pass == 0 && len(pending) >= resealProgressEvery {
			announced = len(pending)
			v.log.Info(fmt.Sprintf("re-sealing %d stored backup files -- backups keep arriving while this runs", announced))
		}
		for _, s := range pending {
			if resealStep != nil {
				resealStep()
			}
			var err error
			if buf, err = v.resealSlot(s, pub, buf); err != nil {
				return err
			}
			examined++
			if announced > 0 && examined%resealProgressEvery == 0 {
				v.log.Info(fmt.Sprintf("re-sealing stored backup files: %d done", examined))
			}
		}
	}
	if announced > 0 {
		v.log.Info(fmt.Sprintf("re-sealed %d stored backup files in %s", examined, time.Since(started).Round(time.Millisecond)))
	}
	return nil
}

// resealSlot converts one file, or reports why it could not. buf is the
// caller's read buffer, returned grown to whatever this file needed so
// the next one can use it again.
func (v *Vault) resealSlot(s slot, pub *ecdh.PublicKey, buf []byte) ([]byte, error) {
	path := filepath.Join(v.routerDir(s.device), v.fileName(s.generation, s.kind))
	sealed, buf, err := readFileInto(path, buf)
	if err != nil {
		if os.IsNotExist(err) {
			// The index knows of a file the disk does not. Not this
			// operation's business to repair.
			return buf, nil
		}
		return buf, fmt.Errorf("backupvault: reading %s while re-sealing: %w", path, err)
	}
	if bytes.HasPrefix(sealed, []byte(hybridMagic)) == (pub != nil) {
		// Already in the target scheme: a file that arrived after this
		// operation claimed the vault, or one an interrupted earlier
		// attempt had already converted. Re-sealing it would be work for
		// no change.
		return buf, nil
	}
	info := sealInfoPrefix + s.device + "/" + s.generation + "/" + s.kind
	plain, err := v.openBody(info, sealed)
	if err != nil {
		return buf, fmt.Errorf("backupvault: opening %s while re-sealing: %w", path, err)
	}
	var next []byte
	if pub == nil {
		next, err = v.key.SealDocument(info, plain)
	} else {
		next, err = sealToPublic(pub, info, plain)
	}
	if err != nil {
		return buf, fmt.Errorf("backupvault: re-sealing %s: %w", path, err)
	}
	if err := persist.WriteFileAtomic(path, next, 0o600); err != nil {
		return buf, fmt.Errorf("backupvault: writing %s while re-sealing: %w", path, err)
	}
	return buf, nil
}

// readFileInto reads path into buf, growing it when the file does not
// fit. It returns the file's bytes and the buffer to hand to the next
// call -- the bytes are a window onto the buffer and stay valid only
// until then, which is all a conversion pass needs.
func readFileInto(path string, buf []byte) (data, next []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, buf, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, buf, err
	}
	size := info.Size()
	if size > MaxFileBytes {
		return nil, buf, fmt.Errorf("backupvault: %s is %d bytes, over the %d-byte cap", path, size, MaxFileBytes)
	}
	if int64(cap(buf)) < size {
		buf = make([]byte, size)
	}
	buf = buf[:size]
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, buf, err
	}
	return buf, buf, nil
}
