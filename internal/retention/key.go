// SPDX-License-Identifier: AGPL-3.0-only

// Package retention writes the events mikroview holds in memory to
// encrypted, compressed daily files, so a replay can reach further back
// than the ring does (#856).
//
// The whole package is off unless the operator mounts a key file. That
// is the decision docs/decisions/event-retention.md records, and the
// reason is worth restating where the code is: thirty days of
// who-talked-to-whom in plaintext is exactly what an attacker who
// reaches the box is looking for, and it would sit there whether or not
// anyone ever replayed it. Memory-only is the default and stays a
// first-class mode -- an operator who wants nothing on disk is choosing
// that, not failing to finish a setup step.
//
// What encryption here does and does not buy, stated plainly so nobody
// over-reads it: copying the data directory, or restoring a backup of
// it, yields nothing readable. Root on the running host can still read
// what the process itself can read, because the process must hold the
// key to append to the files. Only not retaining at all avoids that,
// which is why not retaining remains the default.
package retention

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// MinKeyBytes is the shortest key file accepted.
//
// Thirty-two bytes because that is the size of the AES-256 key every
// file is ultimately sealed with: accepting less would let a short
// passphrase in a file stand in for it, and the resulting key would be
// only as strong as whatever the operator typed while believing the
// documented "AES-256" applied to it. Rejecting outright is better than
// stretching a weak secret, which would produce something that looks
// like a working deployment and is not.
//
// Key material is read as raw bytes, not decoded: 32 random bytes, a
// base64 or hex string of them, or a long passphrase all satisfy this,
// and HKDF turns any of them into the key actually used. See
// docs/configuration.md for the generation command.
const MinKeyBytes = 32

var (
	// ErrNoKey reports that no key file is configured. It is the
	// ordinary, expected state of a default install, never a fault:
	// callers turn retention off and carry on, they do not report a
	// problem to the operator.
	ErrNoKey = errors.New("retention: no key file configured")
	// ErrKeyTooShort reports a key file below MinKeyBytes. Unlike
	// ErrNoKey this is a misconfiguration and is reported: the operator
	// asked for retention and did not get it.
	ErrKeyTooShort = fmt.Errorf("retention: key file holds fewer than %d bytes of key material", MinKeyBytes)
)

// Key is the master secret every retained file is derived from. It is
// never written anywhere: it exists only in this process's memory, read
// once at startup from a file the operator mounts outside the data
// directory.
//
// Outside the data directory is the point of the whole scheme. A key
// kept beside the files it protects is decoration -- whoever copies the
// directory copies both -- so the key file's path is a separate setting
// and the documentation says so in the same breath every time it
// mentions it.
type Key struct {
	material []byte
	// GroupOrWorldReadable records that the key file's mode let someone
	// other than its owner read it. Not a refusal: a secret mounted by
	// an orchestrator commonly arrives 0644 and refusing would break
	// deployments that are otherwise doing the right thing. The caller
	// logs it once at startup so it is visible rather than silent.
	GroupOrWorldReadable bool
}

// LoadKey reads the key file at path.
//
// An empty path is ErrNoKey -- the default install, not an error. A
// path that is set but unreadable, or too short, is a real error and
// the caller must treat it as fail-closed: retention stays off and the
// operator is told, rather than the process quietly falling back to
// writing plaintext or to no retention without saying so. Between "the
// operator asked for encrypted retention" and "mikroview wrote
// something anyway", there is no third option worth building.
func LoadKey(path string) (*Key, error) {
	if path == "" {
		return nil, ErrNoKey
	}
	material, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("retention: reading key file: %w", err)
	}
	// Trailing newlines are stripped because almost every way an
	// operator produces this file adds one, and a key that changes
	// depending on whether the editor added a newline would make every
	// previously written file unreadable for a reason nobody could see.
	for len(material) > 0 && (material[len(material)-1] == '\n' || material[len(material)-1] == '\r') {
		material = material[:len(material)-1]
	}
	if len(material) < MinKeyBytes {
		return nil, ErrKeyTooShort
	}
	k := &Key{material: material}
	if info, err := os.Stat(path); err == nil {
		k.GroupOrWorldReadable = info.Mode().Perm()&(fs.FileMode(0o077)) != 0
	}
	return k, nil
}

// NewKeyFromMaterial builds a Key from bytes already in hand. Tests use
// it; so does any future caller that obtains key material somewhere
// other than a file. It applies the same length floor as LoadKey, so no
// path into this package can produce a Key weaker than the documented
// one.
func NewKeyFromMaterial(material []byte) (*Key, error) {
	if len(material) < MinKeyBytes {
		return nil, ErrKeyTooShort
	}
	dup := make([]byte, len(material))
	copy(dup, material)
	return &Key{material: dup}, nil
}

// fileKey derives the AES-256 key for one day's file.
//
// Per-file rather than one key everywhere, for two reasons. The salt is
// random per file, so two files written from the same master key share
// no key material and a weakness found in one cannot be carried to the
// next. And day is mixed into the info string, so a file renamed to
// another day's name stops decrypting -- an attacker cannot present
// yesterday's events as today's by moving a file, which matters because
// the whole point of a replay is that its window is trustworthy.
func (k *Key) fileKey(salt []byte, day string) ([]byte, error) {
	return hkdf.Key(sha256.New, k.material, salt, keyInfoPrefix+day, 32)
}

// keyInfoPrefix namespaces this package's derived keys. If the same key
// file is ever reused for another purpose -- #853 puts the state store
// and the warm-restart snapshots under this same key -- each derives
// through its own info string, so no two of them ever seal different
// kinds of data with the same bytes.
const keyInfoPrefix = "mikroview/event-retention/v1/"

// The on-disk shape SealDocument produces for one whole document:
//
//	magic     5 bytes   "MVSEL"
//	version   1 byte    sealFormatVersion
//	salt     16 bytes   random, per document
//	nonce    12 bytes   random, per document (AES-256-GCM's size)
//	sealed  variable    the AEAD ciphertext + 16-byte tag
//
// This is the whole-document counterpart to the per-day event-file
// framing above: one seal, not a sequence of appended frames, for a
// caller that already has the entire document in memory and writes it
// once (#394's router-backup vault is the first of these). Reusing this
// package's key derivation and AEAD choice rather than each caller
// rolling its own, per keyInfoPrefix's own doc comment.
const (
	sealMagic         = "MVSEL"
	sealFormatVersion = 1
	sealSaltBytes     = 16
)

// ErrSealedDocumentInvalid reports that OpenDocument was given something
// that is not one of this package's sealed documents at all -- too
// short, wrong magic, or an unsupported format version. Distinct from an
// AEAD authentication failure (wrong key, or genuine tampering), which
// OpenDocument reports separately so a caller can tell "this was never
// one of ours" from "this was ours and something is wrong with it".
var ErrSealedDocumentInvalid = errors.New("retention: not a sealed document")

// SealDocument encrypts plaintext as a single self-contained document,
// binding it to info the way frameAAD binds an event frame to its file
// and position -- a document sealed under one info string fails to open
// under another, so moving a sealed file to a different logical slot
// (a different router's backup, say) is detected rather than silently
// accepted as ciphertext that happens to decrypt.
func (k *Key) SealDocument(info string, plaintext []byte) ([]byte, error) {
	salt := make([]byte, sealSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("retention: generating salt: %w", err)
	}
	derived, err := hkdf.Key(sha256.New, k.material, salt, keyInfoPrefix+"seal/"+info, 32)
	if err != nil {
		return nil, fmt.Errorf("retention: deriving document key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("retention: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("retention: gcm: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("retention: generating nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, []byte(info))

	out := make([]byte, 0, len(sealMagic)+1+len(salt)+len(nonce)+len(sealed))
	out = append(out, sealMagic...)
	out = append(out, sealFormatVersion)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// OpenDocument reverses SealDocument. info must match what SealDocument
// was called with -- see its doc comment.
func (k *Key) OpenDocument(info string, sealed []byte) ([]byte, error) {
	headerLen := len(sealMagic) + 1 + sealSaltBytes
	if len(sealed) < headerLen {
		return nil, ErrSealedDocumentInvalid
	}
	if string(sealed[:len(sealMagic)]) != sealMagic {
		return nil, ErrSealedDocumentInvalid
	}
	if sealed[len(sealMagic)] != sealFormatVersion {
		return nil, fmt.Errorf("%w: unsupported format version %d", ErrSealedDocumentInvalid, sealed[len(sealMagic)])
	}
	salt := sealed[len(sealMagic)+1 : headerLen]

	derived, err := hkdf.Key(sha256.New, k.material, salt, keyInfoPrefix+"seal/"+info, 32)
	if err != nil {
		return nil, fmt.Errorf("retention: deriving document key: %w", err)
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("retention: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("retention: gcm: %w", err)
	}
	if len(sealed) < headerLen+aead.NonceSize() {
		return nil, ErrSealedDocumentInvalid
	}
	nonce := sealed[headerLen : headerLen+aead.NonceSize()]
	ciphertext := sealed[headerLen+aead.NonceSize():]

	plain, err := aead.Open(nil, nonce, ciphertext, []byte(info))
	if err != nil {
		return nil, fmt.Errorf("retention: document did not open -- wrong key, or the file has been altered: %w", err)
	}
	return plain, nil
}
