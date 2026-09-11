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
// default and the unlock screen says so.

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if v.PassphraseSet() {
		return ErrPassphraseSet
	}

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("backupvault: generating the vault key pair: %w", err)
	}
	salt, err := auth.NewKDFSalt()
	if err != nil {
		return fmt.Errorf("backupvault: %w", err)
	}
	params := auth.DefaultKDFParams()
	wrapKey, err := wrapKeyFor(passphrase, salt, params)
	if err != nil {
		return err
	}
	wrapped, err := wrapKey.SealDocument(lockPrivateInfo, priv.Bytes())
	if err != nil {
		return fmt.Errorf("backupvault: sealing the vault private key: %w", err)
	}

	doc := lockDoc{
		Version:        lockDocVersion,
		PublicKey:      priv.PublicKey().Bytes(),
		Salt:           salt,
		KDF:            params,
		WrappedPrivate: wrapped,
	}
	if err := v.writeLockDoc(doc); err != nil {
		return err
	}

	v.lockMu.Lock()
	v.lock = &lockState{doc: doc, pub: priv.PublicKey(), priv: priv}
	v.lockMu.Unlock()

	if err := v.resealAll(); err != nil {
		return err
	}
	v.log.Info("a vault passphrase was set -- stored backups are now unreadable without it")
	return nil
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
	if !v.PassphraseSet() {
		return ErrNoPassphrase
	}
	if err := v.Unlock(passphrase); err != nil {
		return err
	}

	v.lockMu.Lock()
	unlocked := v.lock
	v.lockMu.Unlock()
	if unlocked == nil || unlocked.priv == nil {
		return ErrWrongPassphrase
	}

	if err := v.resealAllTo(nil); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(v.dir, lockFileName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backupvault: removing the vault lock: %w", err)
	}
	v.lockMu.Lock()
	v.lock = nil
	v.lockMu.Unlock()
	v.log.Info("the vault passphrase was removed -- stored backups are readable with the retention key again")
	return nil
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

	v.lockMu.Lock()
	v.lock.priv = priv
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
	v.lockMu.RUnlock()
	if st == nil {
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
	out := make([]byte, 0, hybridHeaderBytes+len(sealed))
	out = append(out, hybridMagic...)
	out = append(out, eph.PublicKey().Bytes()...)
	out = append(out, sealed...)
	return out, nil
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

// resealAll re-seals every stored file under the scheme currently in
// force. Used when the passphrase is turned on.
func (v *Vault) resealAll() error {
	return v.resealAllTo(v.sealBody)
}

// resealAllTo re-seals every stored file with sealWith, or -- when
// sealWith is nil -- directly under the retention key, which is how the
// passphrase is turned off.
//
// Each file is read, opened under whichever scheme sealed it, re-sealed
// and written atomically in place before the next is started, so an
// interruption leaves whole files in one scheme or the other and never a
// half-written one. Bounded work: ten generations per router, each at
// most MaxFileBytes.
func (v *Vault) resealAllTo(sealWith func(string, []byte) ([]byte, error)) error {
	if sealWith == nil {
		sealWith = v.key.SealDocument
	}
	type slot struct {
		device, generation, kind string
	}
	var slots []slot
	v.mu.Lock()
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
	v.mu.Unlock()

	for _, s := range slots {
		path := filepath.Join(v.routerDir(s.device), v.fileName(s.generation, s.kind))
		sealed, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				// The index knows of a file the disk does not. Not this
				// operation's business to repair.
				continue
			}
			return fmt.Errorf("backupvault: reading %s while re-sealing: %w", path, err)
		}
		info := sealInfoPrefix + s.device + "/" + s.generation + "/" + s.kind
		plain, err := v.openBody(info, sealed)
		if err != nil {
			return fmt.Errorf("backupvault: opening %s while re-sealing: %w", path, err)
		}
		next, err := sealWith(info, plain)
		if err != nil {
			return fmt.Errorf("backupvault: re-sealing %s: %w", path, err)
		}
		if err := persist.WriteFileAtomic(path, next, 0o600); err != nil {
			return fmt.Errorf("backupvault: writing %s while re-sealing: %w", path, err)
		}
	}
	return nil
}
