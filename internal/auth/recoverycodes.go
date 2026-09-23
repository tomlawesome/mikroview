// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// Recovery codes are the fallback for signing in when the authenticator
// app itself is unavailable -- phone lost, app uninstalled, TOTP secret
// unreachable (#1249). Ten are minted together when the second factor is
// confirmed, each usable exactly once, and never stored anywhere but
// hashed: this store never again has enough information to show one
// back to its owner, only to check a guess against it.

const (
	// recoveryCodeCount is how many codes GenerateRecoveryCodes mints,
	// and how many User.RecoveryCodes holds afterward -- fixed at ten by
	// the ratified design.
	recoveryCodeCount = 10

	// recoveryCodeAlphabet reuses the reset code's alphabet: no 0/O, no
	// 1/I/l, so a code copied off a screen (or read aloud, the same
	// reasoning resetCodeAlphabet documents) has no ambiguous character.
	recoveryCodeAlphabet = resetCodeAlphabet

	// recoveryCodeLength is characters per code, excluding the grouping
	// dash. Ten characters over the 32-character alphabet above is 50
	// bits -- ample for a hashed, single-use, backup-only credential a
	// person copies out of a list once and keeps offline; the reset
	// code is longer only because it stands in for a whole password
	// indefinitely; a recovery code is checked once and then dead.
	recoveryCodeLength = 10

	// recoveryCodeGroup is how many characters sit between dashes in the
	// form shown to a person: xxxxx-xxxxx.
	recoveryCodeGroup = 5
)

// RecoveryCode is one hashed recovery code, held on User.RecoveryCodes.
// Hash is Argon2id via HashPassword -- the same treatment a password
// gets, per the ratified design -- never the plain code, which exists in
// clear only for the instant GenerateRecoveryCodes returns it to its
// caller. UsedAt is zero until BurnRecoveryCode spends it, and is never
// cleared afterward: a spent code stays spent.
type RecoveryCode struct {
	Hash   string    `json:"hash"`
	UsedAt time.Time `json:"usedAt,omitzero"`
}

// newRecoveryCode returns a fresh code in its canonical (dashless) form
// -- the form that gets hashed, mirroring newResetCode.
func newRecoveryCode() string {
	b := make([]byte, recoveryCodeLength)
	if _, err := rand.Read(b); err != nil {
		// Same stance as newID/newResetCode: a CSPRNG that cannot
		// produce bytes is not a condition to degrade from gracefully
		// when the output is about to stand in for a login credential.
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	out := make([]byte, recoveryCodeLength)
	for i, v := range b {
		out[i] = recoveryCodeAlphabet[v&31]
	}
	return string(out)
}

// FormatRecoveryCode groups a canonical code for display: xxxxx-xxxxx.
// The dash is presentation only -- NormaliseRecoveryCode strips it again
// on the way back in.
func FormatRecoveryCode(code string) string {
	var b strings.Builder
	for i, r := range code {
		if i > 0 && i%recoveryCodeGroup == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormaliseRecoveryCode turns whatever a person typed into the canonical
// form a stored hash was computed over: upper case, with dashes, spaces
// and tabs they may have copied (or added themselves) removed. Mirrors
// NormaliseResetCode exactly, for the same reasons.
func NormaliseRecoveryCode(typed string) string {
	var b strings.Builder
	b.Grow(len(typed))
	for _, r := range typed {
		switch r {
		case '-', ' ', '\t':
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// GenerateRecoveryCodes mints a fresh set of ten single-use codes for
// userID, replacing any set already on the account, and returns them in
// clear -- grouped for display -- exactly once. The caller must show
// them to the user immediately and must never itself persist the
// returned strings; only the hashes this writes to the store survive.
func (s *Store) GenerateRecoveryCodes(userID string, now time.Time) ([]string, error) {
	if !s.Persisted() {
		return nil, ErrNotPersisted
	}

	// Hashing happens before the lock, same reasoning as createLocked and
	// IssueResetCode: HashPassword is ~100ms of Argon2id by design, and
	// ten of them held under the store's write lock would serialize
	// every reader for the better part of a second.
	clear := make([]string, recoveryCodeCount)
	hashed := make([]RecoveryCode, recoveryCodeCount)
	for i := range hashed {
		code := newRecoveryCode()
		hash, err := HashPassword(code)
		if err != nil {
			return nil, err
		}
		clear[i] = FormatRecoveryCode(code)
		hashed[i] = RecoveryCode{Hash: hash}
	}

	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	prevCodes := u.RecoveryCodes
	u.RecoveryCodes = hashed
	if err := s.tryPersistLocked(); err != nil {
		// A set that only exists in memory must not be reported as
		// issued: the caller is about to show these to the user as their
		// only way back in, and a restart before the next good write
		// would revert to whatever set (if any) existed before, leaving
		// the shown codes unable to verify against anything.
		u.RecoveryCodes = prevCodes
		return nil, fmt.Errorf("saving accounts: %w", err)
	}
	return clear, nil
}

// BurnRecoveryCode verifies code against userID's unused recovery codes
// and, on a match, marks that one used so it cannot be redeemed again.
//
// Returns false, not an error, for a wrong code or one already used;
// callers must not distinguish those to whoever is attempting login, the
// same reasoning ErrInvalidCredentials documents for a bad password.
// ErrUserNotFound is returned only for an unknown userID -- a caller
// error, since a login flow only reaches this after already resolving
// the account.
//
// Every unused code is checked even after a match is found, rather than
// stopping at the first: the same reasoning RecoveryStore.Redeem's doc
// comment gives for the separate host-recovery-key store -- the time a
// check takes should not tell an observer which of the ten codes (by
// position) just matched.
func (s *Store) BurnRecoveryCode(userID, code string, now time.Time) (bool, error) {
	normalised := NormaliseRecoveryCode(code)

	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return false, ErrUserNotFound
	}

	matchIdx := -1
	for i := range u.RecoveryCodes {
		rc := &u.RecoveryCodes[i]
		if !rc.UsedAt.IsZero() {
			continue
		}
		if VerifyPassword(normalised, rc.Hash) {
			matchIdx = i
		}
	}
	if matchIdx == -1 {
		return false, nil
	}

	prevUsedAt := u.RecoveryCodes[matchIdx].UsedAt
	u.RecoveryCodes[matchIdx].UsedAt = now
	if err := s.tryPersistLocked(); err != nil {
		// A spend that only lands in memory is undone by a restart, and
		// the code is live again for whoever presented it -- refuse the
		// login rather than honour a spend nothing recorded, the same
		// stance Authenticate's reset-code path takes.
		u.RecoveryCodes[matchIdx].UsedAt = prevUsedAt
		return false, fmt.Errorf("saving accounts: %w", err)
	}
	return true, nil
}
