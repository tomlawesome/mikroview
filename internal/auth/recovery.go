// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Recovery keys are the second factor on the CLI commands that change
// authentication state -- admin transfer, admin account recovery, and
// re-arming the first-run setup screen (issue #134).
//
// What they actually buy is worth stating plainly, because it is easy to
// mistake them for something stronger: an attacker who fully owns the
// host can already read and rewrite the accounts file directly, and no
// key stops that. What these do stop is a *lower-privileged* local
// account or a container exec that can run the binary but not write the
// data volume, and someone holding a stolen backup rather than live host
// access. They also make every use of those commands an auditable event.
const (
	// recoveryKeyCount: three keys, generated and rotated as a set.
	//
	// Under full-set rotation this is not "three recoveries" -- using any
	// key invalidates all of them, so functionally it is one key with two
	// spares. The spares exist to survive a smudged printout, a bad
	// paste, or a typo, which is the whole job. Comparisons with
	// implementations that issue 8-16 codes do not apply: those consume
	// codes individually, so N codes really are N recoveries.
	recoveryKeyCount = 3

	// recoveryKeyBits: 160 bits of entropy per key.
	//
	// NIST SP 800-63B requires a look-up secret at or above 112 bits to
	// be hashed with an approved one-way function, and only *below* that
	// threshold demands a salted KDF. 160 clears it with margin, which is
	// why a fast HMAC is the specified treatment here rather than a
	// compromise -- brute-force resistance comes from the search space,
	// not from the cost of the hash. Argon2id here would slow every
	// verification and make the key no harder to guess.
	recoveryKeyBits = 160

	// recoveryPepperBits: the server-side secret mixed into every digest.
	recoveryPepperBits = 256

	// algoHMAC tags each stored digest so the scheme can be changed later
	// without a flag day: a record written by an older build stays
	// verifiable, and is upgraded in place on the next rotation.
	algoHMAC = "hmac-sha256-v1"
)

var (
	// ErrNoRecoveryKeys is returned when a gated command runs on a
	// deployment that has none. Deliberately not "allow through" -- an
	// absent key file must never read as "no gate configured".
	ErrNoRecoveryKeys = errors.New("auth: no recovery keys exist -- run -generate-recovery-keys first")
	// ErrRecoveryKeysExist is returned by Generate on a store that
	// already has keys. Regeneration would be a complete bypass of the
	// gate: anyone with host access could mint a fresh set and then
	// satisfy the check they were supposed to be stopped by. Rotation
	// happens automatically after a successful use instead.
	ErrRecoveryKeysExist = errors.New("auth: recovery keys already exist -- they rotate automatically after each use")
	// ErrInvalidRecoveryKey is returned for a key that doesn't verify,
	// and for a corrupt key file. Callers must not distinguish the two to
	// the user.
	ErrInvalidRecoveryKey = errors.New("auth: invalid recovery key")
)

// keyEncoding is unpadded base32 -- case-insensitive on input and free
// of the characters people mistranscribe from a printout.
var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type recoveryFile struct {
	Keys []recoveryRecord `json:"keys"`
}

type recoveryRecord struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}

// RecoveryStore holds the hashed recovery keys.
//
// Deliberately a separate file from the accounts store: if the digests
// lived inside accounts.json, a corrupted or lost accounts file would
// also destroy the only thing able to validate a recovery key -- exactly
// the situation the gated commands exist to recover from.
type RecoveryStore struct {
	mu         sync.Mutex
	path       string
	pepperPath string
	pepper     []byte
	keys       []recoveryRecord
	// pending holds a generated-but-uncommitted set. Rotation is not
	// persisted until the operator confirms they have saved the new
	// keys -- otherwise a successful recovery whose output was lost
	// (piped to a file nobody reads, a rotated container log, scrollback
	// gone) leaves the deployment with zero valid keys and the next
	// lockout unrecoverable.
	pending []recoveryRecord
}

// OpenRecovery loads the recovery-key store, generating the pepper on
// first use if it does not exist.
//
// The pepper lives in its own file, outside the backup set (issue #97),
// so someone holding a stolen backup has the digests but nothing to
// verify them against. It is written with O_EXCL and never rewritten.
func OpenRecovery(path, pepperPath string) (*RecoveryStore, error) {
	s := &RecoveryStore{path: path, pepperPath: pepperPath}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	pepper, err := loadOrCreatePepper(pepperPath)
	if err != nil {
		return nil, err
	}
	s.pepper = pepper

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f recoveryFile
	if err := json.Unmarshal(data, &f); err != nil {
		// Fail closed. A corrupt key file must not read as an absent
		// one: absent has a legitimate path forward
		// (-generate-recovery-keys), and corrupt must not inherit it,
		// or truncating the file becomes the bypass.
		return nil, fmt.Errorf("%w: recovery key file is unreadable", ErrInvalidRecoveryKey)
	}
	s.keys = f.Keys
	return s, nil
}

func loadOrCreatePepper(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("auth: recovery pepper path is not configured")
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		p, decErr := hex.DecodeString(string(raw))
		if decErr != nil || len(p) != recoveryPepperBits/8 {
			return nil, fmt.Errorf("%w: recovery pepper is unreadable", ErrInvalidRecoveryKey)
		}
		return p, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	p := make([]byte, recoveryPepperBits/8)
	if _, err := rand.Read(p); err != nil {
		return nil, err
	}
	// O_EXCL: never overwrite an existing pepper, even on a race. Losing
	// it makes every stored digest unverifiable.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return loadOrCreatePepper(path) // lost the race; read theirs
		}
		return nil, err
	}
	defer f.Close()
	if _, err := f.WriteString(hex.EncodeToString(p)); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *RecoveryStore) digest(key string) recoveryRecord {
	mac := hmac.New(sha256.New, s.pepper)
	mac.Write([]byte(normaliseKey(key)))
	return recoveryRecord{Algorithm: algoHMAC, Digest: hex.EncodeToString(mac.Sum(nil))}
}

// normaliseKey makes transcription forgiving without weakening the key:
// case and the separators people insert when copying from a printout are
// not part of the secret.
func normaliseKey(k string) string {
	out := make([]byte, 0, len(k))
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c-32)
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			out = append(out, c)
		}
	}
	return string(out)
}

// Exists reports whether this deployment has recovery keys.
func (s *RecoveryStore) Exists() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.keys) > 0
}

// Generate creates a fresh set and returns the cleartext keys, which are
// shown to the operator exactly once and never persisted in the clear.
//
// The new set is NOT yet active: it is held pending until Commit. If the
// process dies before then, the previous keys remain valid. Callers must
// not treat Generate as having rotated anything.
func (s *RecoveryStore) Generate() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil, ErrNotPersisted
	}
	if len(s.keys) > 0 {
		return nil, ErrRecoveryKeysExist
	}
	return s.generateLocked()
}

func (s *RecoveryStore) generateLocked() ([]string, error) {
	clear := make([]string, 0, recoveryKeyCount)
	records := make([]recoveryRecord, 0, recoveryKeyCount)
	for i := 0; i < recoveryKeyCount; i++ {
		buf := make([]byte, recoveryKeyBits/8)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		k := keyEncoding.EncodeToString(buf)
		clear = append(clear, k)
		records = append(records, s.digest(k))
	}
	s.pending = records
	return clear, nil
}

// Commit persists a set produced by Generate or Redeem. This is the
// acknowledgement gate: nothing rotates until the operator confirms they
// have saved the new keys.
func (s *RecoveryStore) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		return errors.New("auth: nothing to commit")
	}
	prev := s.keys
	s.keys = s.pending
	if err := s.persistLocked(); err != nil {
		s.keys = prev // leave the old set valid rather than neither
		return err
	}
	s.pending = nil
	return nil
}

// Redeem verifies key and, on success, generates a replacement set,
// returning the new cleartext keys. The rotation is pending until
// Commit -- see Generate.
//
// Verification is constant-time over the MAC. A wrong key and a corrupt
// store both yield ErrInvalidRecoveryKey; callers must not tell the user
// which, since the distinction is only useful to someone probing.
func (s *RecoveryStore) Redeem(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.keys) == 0 {
		return nil, ErrNoRecoveryKeys
	}

	want := s.digest(key)
	matched := false
	for _, rec := range s.keys {
		// Every candidate is compared even after a match, so the time
		// taken does not reveal which key was used.
		if rec.Algorithm == want.Algorithm &&
			subtle.ConstantTimeCompare([]byte(rec.Digest), []byte(want.Digest)) == 1 {
			matched = true
		}
	}
	if !matched {
		return nil, ErrInvalidRecoveryKey
	}
	return s.generateLocked()
}

func (s *RecoveryStore) persistLocked() error {
	if s.path == "" {
		return ErrNotPersisted
	}
	data, err := json.MarshalIndent(recoveryFile{Keys: s.keys}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
