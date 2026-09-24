// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Passkeys are #1250's second kind of second factor: WebAuthn
// credentials, stored on User.Passkeys alongside the authenticator-app
// fields #1249 added to store.go. This file is the store-layer half
// only -- it holds what a registration or login ceremony produced, and
// never performs the ceremony itself. That work (talking to
// go-webauthn, building the RP config, sealing session data into
// cookies) lives in internal/api/webauthn.go, a parallel #1250 slice
// that owns the go-webauthn dependency; this package does not import
// it and must not gain a reason to. Passkey.Transports and
// Passkey.Flags below reproduce the shapes of two go-webauthn types for
// exactly that reason -- see their doc comments.
//
// Recovery codes are shared between this factor and the authenticator
// app (#1250): minted once, on whichever factor activates first, and
// cleared only when the account's last second factor of either kind
// goes. The minting rule ("mint if absent, never re-mint") is decided
// by the caller (internal/api's registration-finish and TOTP-confirm
// handlers, since only they know which factor is activating and
// whether this is the first); GenerateRecoveryCodes in recoverycodes.go
// is unchanged from #1249 and always mints unconditionally when called.
// What this file owns is the other half -- the clearing rule -- because
// clearing only ever happens alongside a factor actually being removed,
// which is a write this file already makes: DeletePasskey and
// ClearPasskeys below clear RecoveryCodes exactly when HasSecondFactor
// (store.go) goes false as a result; ClearTOTP in store.go does the
// same from the authenticator-app side.

const (
	// maxPasskeysPerAccount bounds AddPasskey -- see
	// ErrPasskeyLimitReached. Ten is generous for "a phone, a security
	// key, a couple of spares" while still keeping the list an account
	// owner has to review, to know what can sign in as them, from
	// growing without bound.
	maxPasskeysPerAccount = 10
	// maxPasskeyNameLength bounds a passkey's display name in runes --
	// generous for "YubiKey 5C NFC (backup)" while keeping the list
	// readable and the stored document small.
	maxPasskeyNameLength = 64
)

var (
	// ErrPasskeyNotFound is returned by RenamePasskey, DeletePasskey and
	// RecordPasskeyAssertion when credID matches none of userID's stored
	// passkeys -- covers both "never existed" and "already removed"; a
	// caller has no legitimate reason to tell those apart.
	ErrPasskeyNotFound = errors.New("auth: no such passkey on this account")
	// ErrPasskeyLimitReached is returned by AddPasskey once an account
	// already holds maxPasskeysPerAccount credentials.
	ErrPasskeyLimitReached = fmt.Errorf("auth: an account may hold at most %d passkeys -- remove one before adding another", maxPasskeysPerAccount)
	// ErrPasskeyDuplicate is returned by AddPasskey when the credential
	// ID being added already exists on the account -- the same
	// authenticator (or a replayed registration ceremony) offered
	// twice. Checked before ErrPasskeyLimitReached, so a re-presented
	// credential is never reported as "limit reached" merely because
	// the account happens to be full.
	ErrPasskeyDuplicate = errors.New("auth: this passkey is already registered to this account")
)

// Passkey is one registered WebAuthn credential, held on User.Passkeys.
// Unlike TOTPSecret's single shared secret, an account may hold several
// -- a phone and a security key, say -- so this is a slice rather than
// a fixed set of fields.
type Passkey struct {
	// ID is the credential ID the registration ceremony returned -- the
	// value every later assertion presents to say "this is the same
	// credential", matched by exact bytes (see the bytes.Equal lookups
	// below). JSON as base64, the standard encoding for a []byte field.
	ID []byte `json:"id"`
	// PublicKey is the COSE-encoded public key the authenticator proved
	// it holds the matching private key for at registration -- needed
	// to verify every later assertion's signature. Not secret in the
	// sense a private key would be (WebAuthn never gives the relying
	// party the private key), but still credential material, not
	// something an admin-facing account list should serialize -- see
	// List's doc comment in store.go.
	PublicKey []byte `json:"publicKey"`
	// SignCount is the authenticator's signature counter as of the most
	// recently accepted assertion (registration supplies the first
	// value). Forward-only, advanced only through
	// RecordPasskeyAssertion below -- see that method's doc comment,
	// and TOTPLastCounter in store.go for why an update that arrives
	// any other way is exactly where a replay guard stops guarding.
	SignCount uint32 `json:"signCount"`
	// Transports is what the authenticator reported it can be reached
	// over (usb, nfc, ble, internal, hybrid, ...) at registration --
	// shown in the UI and offered back as a hint on future
	// registration/assertion requests.
	//
	// A []string, not go-webauthn's own []protocol.AuthenticatorTransport:
	// this package deliberately does not depend on go-webauthn (see the
	// package comment above), and protocol.AuthenticatorTransport's
	// underlying type is exactly string (checked 2026-09-23 against
	// go-webauthn v0.18.2's protocol/authenticator.go), so the JSON on
	// the wire is identical either way and internal/api's conversion at
	// the boundary is one cast per element, not a translation.
	Transports []string `json:"transports,omitempty"`
	// Flags carries the four authenticator flags go-webauthn's own
	// webauthn.CredentialFlags exposes, needed to rebuild a faithful
	// webauthn.Credential for FinishLogin. Reproduced here as
	// PasskeyFlags -- a package-local struct with matching field names
	// and JSON tags -- for the same reason Transports is []string
	// above, not this package's own translation of the concept.
	Flags PasskeyFlags `json:"flags"`
	// RPID is the relying-party ID (essentially the registered domain)
	// this credential was created against -- the "stale passkey" story:
	// publicUrl can change after a passkey is registered, and a
	// credential whose RPID no longer matches the server's current one
	// cannot complete a login (excluded from assertion options) but is
	// still shown and still removable. Nothing in this file reads RPID
	// to decide that -- staleness is an internal/api concern (it knows
	// the server's current RPID; this package does not) -- it is
	// carried here only because it has to live on the credential to be
	// available for that comparison at all.
	RPID string `json:"rpId"`
	// Name is the operator-chosen label shown in the passkey list --
	// "YubiKey", "MacBook Touch ID" -- normalised by AddPasskey and
	// RenamePasskey (see normalisePasskeyName); never empty once
	// stored.
	Name string `json:"name"`
	// CreatedAt is when this credential was registered. Set by the
	// caller (internal/api's registration-finish handler, which already
	// has a "now" from driving the ceremony) rather than by AddPasskey
	// itself -- unlike RecordPasskeyAssertion below, AddPasskey takes
	// no separate now parameter, because pk arrives with everything it
	// needs already on it.
	CreatedAt time.Time `json:"createdAt"`
	// LastUsedAt is when this credential last completed a login --
	// zero until the first one, then advanced by RecordPasskeyAssertion
	// on every accepted assertion.
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
}

// PasskeyFlags mirrors go-webauthn's webauthn.CredentialFlags -- see
// Passkey.Flags' doc comment for why this package reproduces the shape
// instead of importing that type. Field names and JSON tags match
// exactly (checked 2026-09-23 against go-webauthn v0.18.2's
// webauthn/credential.go), so internal/api's conversion to and from the
// library type is a straight field-by-field copy, not a translation.
type PasskeyFlags struct {
	UserPresent    bool `json:"userPresent"`
	UserVerified   bool `json:"userVerified"`
	BackupEligible bool `json:"backupEligible"`
	BackupState    bool `json:"backupState"`
}

// normalisePasskeyName trims name, bounds it to maxPasskeyNameLength
// runes, and falls back to a numbered default ("Passkey <n>") if what's
// left is empty. Shared by AddPasskey and RenamePasskey so a stored
// Name is never blank and the two rules can't drift apart. n is the
// 1-based number to use in the fallback.
func normalisePasskeyName(name string, n int) string {
	trimmed := strings.TrimSpace(name)
	// Bounded on runes, not bytes: a byte-index slice of a UTF-8 string
	// can cut a multi-byte character in half and leave invalid UTF-8
	// stored right in the accounts document.
	if r := []rune(trimmed); len(r) > maxPasskeyNameLength {
		trimmed = string(r[:maxPasskeyNameLength])
	}
	if trimmed == "" {
		return fmt.Sprintf("Passkey %d", n)
	}
	return trimmed
}

// findPasskeyIndex returns the index of the passkey on u matching
// credID by exact bytes, or -1. Shared by every method below that acts
// on one specific credential.
func findPasskeyIndex(u *User, credID []byte) int {
	for i, pk := range u.Passkeys {
		if bytes.Equal(pk.ID, credID) {
			return i
		}
	}
	return -1
}

// AddPasskey registers a new WebAuthn credential on userID's account --
// the store-layer half of the registration ceremony internal/api's
// FinishRegistration drives. pk arrives fully populated by the caller.
//
// The credential ID is checked against every passkey already on the
// account before the account's capacity is: ErrPasskeyDuplicate takes
// priority over ErrPasskeyLimitReached, so an authenticator presented
// twice against a full account is told it's already registered rather
// than that the account is full. Name is normalised (see
// normalisePasskeyName) before it's stored. Returns the stored Passkey,
// with its normalised name, so the caller's response doesn't have to
// re-derive it.
func (s *Store) AddPasskey(userID string, pk Passkey) (Passkey, error) {
	if !s.Persisted() {
		return Passkey{}, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return Passkey{}, ErrUserNotFound
	}

	if findPasskeyIndex(u, pk.ID) != -1 {
		return Passkey{}, ErrPasskeyDuplicate
	}
	if len(u.Passkeys) >= maxPasskeysPerAccount {
		return Passkey{}, ErrPasskeyLimitReached
	}

	pk.Name = normalisePasskeyName(pk.Name, len(u.Passkeys)+1)

	prevPasskeys := u.Passkeys
	u.Passkeys = append(u.Passkeys, pk)
	if err := s.tryPersistLocked(); err != nil {
		// A registration that only exists in memory must not be
		// reported as done: the caller is about to tell its user the
		// passkey was added -- and, on a first factor, mint recovery
		// codes and revoke other sessions around that claim -- and a
		// restart before the next good write would drop the credential
		// while nothing else remembers it ever existed.
		u.Passkeys = prevPasskeys
		return Passkey{}, fmt.Errorf("saving accounts: %w", err)
	}
	return pk, nil
}

// RenamePasskey changes the display name of one of userID's passkeys,
// found by credential ID. No password check here -- a rename is
// cosmetic and reversible, unlike DeletePasskey below, which
// internal/api's route does gate on one (passwordRecheckLimiterKey,
// same budget TOTP delete uses). Runs the same normalisation AddPasskey
// does, so a rename to blank or to something absurdly long behaves the
// same way giving that name at registration would have.
func (s *Store) RenamePasskey(userID string, credID []byte, name string) (Passkey, error) {
	if !s.Persisted() {
		return Passkey{}, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return Passkey{}, ErrUserNotFound
	}

	idx := findPasskeyIndex(u, credID)
	if idx == -1 {
		return Passkey{}, ErrPasskeyNotFound
	}

	// A fresh backing array, not an in-place field write on
	// u.Passkeys[idx]: Get hands out a shallow *User copy that shares
	// this slice's backing array without holding the lock while the
	// caller reads it, so mutating an element in place races that read.
	// See DeletePasskey's own comment for the same reasoning; here the
	// element count doesn't change, only its contents, so every element
	// is copied across including the one being renamed.
	prevPasskeys := u.Passkeys
	kept := make([]Passkey, len(u.Passkeys))
	copy(kept, u.Passkeys)
	kept[idx].Name = normalisePasskeyName(name, idx+1)
	u.Passkeys = kept
	if err := s.tryPersistLocked(); err != nil {
		u.Passkeys = prevPasskeys
		return Passkey{}, fmt.Errorf("saving accounts: %w", err)
	}
	return u.Passkeys[idx], nil
}

// DeletePasskey removes one of userID's passkeys, found by credential
// ID, and -- in the same locked write -- clears RecoveryCodes too if
// that removal leaves the account with no second factor of either kind
// (HasSecondFactor, store.go). That conditional clear is the
// load-bearing part of #1250's shared-recovery-codes design: codes
// minted for a passkey must survive removing a *different* passkey, or
// an authenticator app that isn't the last factor standing, and must
// not survive the account actually going back to password-only.
// Getting this wrong in either direction either orphans a still-active
// factor's fallback, or leaves stale codes able to sign in to an
// account that looks, from the outside, like it has no second factor
// at all. ClearTOTP in store.go makes the same call from the other
// direction.
//
// Returns the removed Passkey, so a caller building an audit entry
// (account.passkey_removed) doesn't have to look it up separately
// beforehand.
func (s *Store) DeletePasskey(userID string, credID []byte) (Passkey, error) {
	if !s.Persisted() {
		return Passkey{}, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return Passkey{}, ErrUserNotFound
	}

	idx := findPasskeyIndex(u, credID)
	if idx == -1 {
		return Passkey{}, ErrPasskeyNotFound
	}

	removed := u.Passkeys[idx]
	prevPasskeys := u.Passkeys
	prevCodes := u.RecoveryCodes

	// A fresh backing array rather than the usual in-place
	// append(s[:i], s[i+1:]...) splice: Get/List hand out a shallow
	// *User copy that shares this slice's backing array, and splicing
	// in place would shift elements underneath a copy taken a moment
	// earlier by a concurrent reader.
	kept := make([]Passkey, 0, len(u.Passkeys)-1)
	kept = append(kept, u.Passkeys[:idx]...)
	kept = append(kept, u.Passkeys[idx+1:]...)
	u.Passkeys = kept

	if !u.HasSecondFactor() {
		u.RecoveryCodes = nil
	}
	if err := s.tryPersistLocked(); err != nil {
		// A removal that only exists in memory must not be reported as
		// done: the caller is about to tell its user this credential no
		// longer works (and, if it was the last factor, that recovery
		// codes are gone too), and a restart before the next good write
		// would silently bring both back.
		u.Passkeys = prevPasskeys
		u.RecoveryCodes = prevCodes
		return Passkey{}, fmt.Errorf("saving accounts: %w", err)
	}
	return removed, nil
}

// RecordPasskeyAssertion advances userID's credID passkey after a
// successful login assertion. SignCount is forward-only, the same
// stance RecordTOTPCounter (store.go) takes for its counter: a value at
// or below what's stored is a no-op on the count, not an error, since
// many platform authenticators always report 0 and that must never be
// treated as a regression. LastUsedAt is set to now unconditionally,
// even on a 0-to-0 call, so the passkey list can show "last used" for
// an authenticator that never advances its counter at all.
//
// internal/api calls this only after already deciding the assertion is
// genuine: verifying the signature and checking go-webauthn's
// CloneWarning are both its job, not this method's (see the design's
// "Sign count" section) -- by the time this runs, a clone-suspected
// assertion has already been refused 401 and this is never reached for
// one, so the stored count stays exactly where it was.
func (s *Store) RecordPasskeyAssertion(userID string, credID []byte, signCount uint32, now time.Time) error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}

	idx := findPasskeyIndex(u, credID)
	if idx == -1 {
		return ErrPasskeyNotFound
	}

	// A fresh backing array, not in-place field writes on
	// u.Passkeys[idx]: Get hands out a shallow *User copy that shares
	// this slice's backing array without holding the lock while the
	// caller reads it, so mutating an element in place races that read
	// (see RenamePasskey's identical comment, and DeletePasskey's for
	// the element-count-changes case this mirrors).
	prevPasskeys := u.Passkeys
	kept := make([]Passkey, len(u.Passkeys))
	copy(kept, u.Passkeys)
	if signCount > kept[idx].SignCount {
		kept[idx].SignCount = signCount
	}
	kept[idx].LastUsedAt = now
	u.Passkeys = kept
	if err := s.tryPersistLocked(); err != nil {
		// A guard/timestamp that only advanced in memory must not be
		// reported as advanced -- same reasoning RecordTOTPCounter's
		// restore-on-failure comment gives: a restart before the next
		// good write would make an already-used counter value live
		// again for whoever else presented it.
		u.Passkeys = prevPasskeys
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// RecordPasskeyAssertionIfFresh is RecordPasskeyAssertion's login-path
// sibling: the same forward-only SignCount and LastUsedAt update, but
// under the same lock acquisition it also decides whether the login is
// accepted, closing a race the two-step version left open.
// handleAuthLoginFactor's passkey branch (passkey.go) used to call
// go-webauthn's ValidateLogin (whose CloneWarning is what would normally
// catch a replayed assertion) and then RecordPasskeyAssertion as two
// separate steps; two concurrent submissions of the same assertion both
// cleared CloneWarning against the same not-yet-advanced stored count
// and both won a session -- the passkey shape of #1249's TOTP race (see
// VerifyAndRecordTOTP, store.go).
//
// accepted is false when signCount is not fresh: nonzero and at or
// below what's already stored. Zero is exempt, the same exemption
// RecordPasskeyAssertion's own doc comment explains -- an authenticator
// that always reports 0 must not be locked out after its first login,
// so its logins carry no counter-based replay protection here, same as
// before this method existed.
func (s *Store) RecordPasskeyAssertionIfFresh(userID string, credID []byte, signCount uint32, now time.Time) (accepted bool, err error) {
	if !s.Persisted() {
		return false, ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return false, ErrUserNotFound
	}

	idx := findPasskeyIndex(u, credID)
	if idx == -1 {
		return false, ErrPasskeyNotFound
	}

	stored := u.Passkeys[idx].SignCount
	if signCount != 0 && signCount <= stored {
		return false, nil
	}

	prevPasskeys := u.Passkeys
	kept := make([]Passkey, len(u.Passkeys))
	copy(kept, u.Passkeys)
	if signCount > stored {
		kept[idx].SignCount = signCount
	}
	kept[idx].LastUsedAt = now
	u.Passkeys = kept
	if err := s.tryPersistLocked(); err != nil {
		u.Passkeys = prevPasskeys
		return true, fmt.Errorf("saving accounts: %w", err)
	}
	return true, nil
}

// ClearPasskeys removes every passkey on userID's account in one write
// -- the admin-clear counterpart to DeletePasskey, mirroring
// handleTOTPAdminClear's shape (internal/api, wave 2). Same conditional
// recovery-code clear as DeletePasskey: codes survive if the account
// still has an active authenticator-app factor, and are cleared only if
// this was the account's last second factor.
func (s *Store) ClearPasskeys(userID string) error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}

	prevPasskeys := u.Passkeys
	prevCodes := u.RecoveryCodes

	u.Passkeys = nil
	if !u.HasSecondFactor() {
		u.RecoveryCodes = nil
	}
	if err := s.tryPersistLocked(); err != nil {
		u.Passkeys = prevPasskeys
		u.RecoveryCodes = prevCodes
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// ClearAllSecondFactors removes every second factor on userID's account
// -- the authenticator app and every passkey -- and the recovery codes
// that backed them, all in the one write. The CLI's
// `-clear-second-factor` uses this unconditionally (main.go, wave 2
// slice F): "I've lost everything" has no partial form, so unlike
// DeletePasskey, ClearPasskeys and ClearTOTP there is no
// factor-remaining check to make here -- there is nothing left standing
// after this call, by construction.
func (s *Store) ClearAllSecondFactors(userID string) error {
	if !s.Persisted() {
		return ErrNotPersisted
	}
	s.reloadIfStale()

	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}

	prevSecret := u.TOTPSecret
	prevConfirmedAt := u.TOTPConfirmedAt
	prevCounter := u.TOTPLastCounter
	prevPasskeys := u.Passkeys
	prevCodes := u.RecoveryCodes

	u.TOTPSecret = ""
	u.TOTPConfirmedAt = time.Time{}
	u.TOTPLastCounter = 0
	u.Passkeys = nil
	u.RecoveryCodes = nil
	if err := s.tryPersistLocked(); err != nil {
		// A clear that only exists in memory must not be reported as
		// done: the caller tells its operator every second factor is
		// off, and a restart before the next good write would silently
		// bring all of it back underneath that claim.
		u.TOTPSecret = prevSecret
		u.TOTPConfirmedAt = prevConfirmedAt
		u.TOTPLastCounter = prevCounter
		u.Passkeys = prevPasskeys
		u.RecoveryCodes = prevCodes
		return fmt.Errorf("saving accounts: %w", err)
	}
	return nil
}

// PasskeyCount reports how many passkeys userID's account holds. It
// exists for the same reason HasActiveTOTP (store.go) does: List()
// blanks Passkeys entirely on every copy it returns (see List's doc
// comment), so len(copy.Passkeys) on a List() result always reads zero.
// #1249 shipped exactly that mistake once already, reading
// HasActiveTOTP off a blanked TOTPSecret; this is the passkey-shaped
// guard against repeating it for the admin users list's passkeyCount
// (internal/api, wave 2). An unknown user answers 0 rather than
// erroring, the same yes/no-gate stance HasActiveTOTP takes.
func (s *Store) PasskeyCount(userID string) int {
	s.reloadIfStale()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.byID[userID]
	if !ok {
		return 0
	}
	return len(u.Passkeys)
}
