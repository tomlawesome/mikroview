// SPDX-License-Identifier: AGPL-3.0-only

package retention

import (
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
)

// Key.Seal and Key.Open are this package's cipher and key derivation
// (AES-256-GCM, keys derived from the master Key with HKDF-SHA256 -- see
// aeadFromInfo and Derive) made available to other packages that hold the
// same master key for a different purpose (#853): internal/persist's
// file-backed stores and internal/snapshot's warm-restart documents. Both
// need to seal one whole document per write rather than an append-only
// stream of framed events, so they get their own envelope shape here
// rather than reusing file.go's day-file frame format, which is specific
// to that append/replay design. The cipher and the key derivation are the
// same either way -- there is one AEAD scheme in this codebase, not two.
//
// info must be a caller-owned prefix (see the *KeyInfo constants below)
// so two callers sharing one master key never derive the same bytes for
// different data. aad binds the envelope to context the caller supplies
// -- typically the path or name the envelope is stored under -- so a
// ciphertext copied somewhere else fails to open there rather than
// silently decrypting as something it is not.

// sealMagic identifies this envelope shape, distinct from file.go's
// "MVEVT" day-file magic.
const sealMagic = "MVS1"

const sealVersion = 1

// sealHeaderBytes is the fixed prefix before the nonce and ciphertext:
// magic, a version byte, and the random per-call salt.
const sealHeaderBytes = len(sealMagic) + 1 + saltBytes

// gcmNonceBytes and gcmTagBytes are AES-GCM's standard nonce and tag
// sizes, which aeadFromInfo's cipher.NewGCM produces. Named here only so
// SealOverheadBytes can be a constant; TestSealOverheadMatchesTheCipher
// checks them against what the cipher actually emits, so they cannot
// quietly drift.
const (
	gcmNonceBytes = 12
	gcmTagBytes   = 16
)

// SealOverheadBytes is how much longer a sealed envelope is than the
// plaintext inside it: the header, the nonce and the authentication tag.
//
// Exported for a caller assembling an envelope inside a larger buffer --
// internal/backupvault puts a 37-byte header of its own in front of one
// -- so that buffer can be allocated once, at the size it will end up,
// instead of being grown or copied afterwards.
const SealOverheadBytes = sealHeaderBytes + gcmNonceBytes + gcmTagBytes

// StateStoreKeyInfo namespaces keys derived for internal/persist's
// encrypted file backend (#853). See Derive's doc comment: every user of
// the master key needs its own info string.
//
// internal/snapshot's warm-restart documents (#853) are sealed under this
// same Key but do not get their own constant here: internal/snapshot
// cannot import this package without an import cycle (this package
// imports internal/store, which imports internal/snapshot for the Part
// interface), so it takes Key through a small local interface instead and
// carries its own info-string constant -- see internal/snapshot/write.go.
const StateStoreKeyInfo = "mikroview/state-store/v1/"

// Seal encrypts plaintext into a self-contained envelope: a fresh random
// salt and nonce are generated on every call, so nothing needs to be kept
// between calls and two seals of the same bytes never produce the same
// ciphertext.
//
// Envelope shape: magic (4 bytes) + version (1 byte) + salt (16 bytes) +
// nonce (aead.NonceSize() bytes) + ciphertext-with-GCM-tag.
func (k *Key) Seal(info string, aad, plaintext []byte) ([]byte, error) {
	return k.SealAppend(nil, info, aad, plaintext)
}

// SealAppend seals plaintext onto the end of dst and returns the
// extended slice, leaving what dst already held untouched -- the same
// dst-first convention cipher.AEAD.Seal itself uses, and dst may be nil.
//
// Seal is this with no dst, and the reason both exist is the size of
// what is being sealed. A router backup is up to 16MiB, and a caller
// that has to put its own header in front of the envelope was copying
// the whole ciphertext a second time to do it (#1121). Given a dst with
// room -- SealOverheadBytes says how much -- the ciphertext is written
// straight into the caller's buffer and never exists twice.
func (k *Key) SealAppend(dst []byte, info string, aad, plaintext []byte) ([]byte, error) {
	if k == nil {
		return nil, errors.New("retention: Seal called with no key")
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("retention: generating salt: %w", err)
	}
	aead, err := aeadFromInfo(k, salt, info)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("retention: generating nonce: %w", err)
	}

	// One growth for the whole envelope, so neither the append below nor
	// the AEAD has to reallocate part-way and copy what is already
	// there. A dst the caller sized itself is not grown at all.
	dst = slices.Grow(dst, sealHeaderBytes+len(nonce)+len(plaintext)+aead.Overhead())
	dst = append(dst, sealMagic...)
	dst = append(dst, sealVersion)
	dst = append(dst, salt...)
	dst = append(dst, nonce...)
	return aead.Seal(dst, nonce, plaintext, aad), nil
}

// Open reverses Seal. A failure to open -- wrong key, wrong info, wrong
// aad, or a document that has been altered (or was never sealed at all,
// e.g. a legacy plaintext file -- #853 has no migration path for those)
// -- is reported as a single class of error: none of those are worth
// distinguishing to an operator, who can only respond to all of them the
// same way.
func (k *Key) Open(info string, aad, envelope []byte) ([]byte, error) {
	if k == nil {
		return nil, errors.New("retention: Open called with no key")
	}
	if len(envelope) < sealHeaderBytes {
		return nil, errors.New("retention: not a sealed document (too short)")
	}
	if string(envelope[:len(sealMagic)]) != sealMagic {
		return nil, errors.New("retention: not a sealed document")
	}
	if envelope[len(sealMagic)] != sealVersion {
		return nil, fmt.Errorf("retention: unsupported sealed-document version %d", envelope[len(sealMagic)])
	}
	salt := envelope[len(sealMagic)+1 : sealHeaderBytes]
	rest := envelope[sealHeaderBytes:]

	aead, err := aeadFromInfo(k, salt, info)
	if err != nil {
		return nil, err
	}
	if len(rest) < aead.NonceSize() {
		return nil, errors.New("retention: not a sealed document (truncated)")
	}
	nonce := rest[:aead.NonceSize()]
	ciphertext := rest[aead.NonceSize():]

	plain, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("retention: did not decrypt -- wrong key, or the document has been altered: %w", err)
	}
	return plain, nil
}
