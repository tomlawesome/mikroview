// SPDX-License-Identifier: AGPL-3.0-only

package api

// The vault passphrase's HTTP surface (#956). internal/backupvault holds
// the cryptography and knows nothing about who is asking; this file is
// the "who", and it answers three questions the package cannot: is the
// caller an admin, is this the session that unlocked, and has that
// unlock gone stale.
//
// Why the unlock is bound to one session rather than to the process.
// Unlocking hands mikroview a key it is not otherwise trusted with, so
// the unlock is scoped as narrowly as the request carries: the admin who
// typed the passphrase, in the tab they typed it in, until they lock it,
// log out, or leave it idle. Their own other sign-ins -- a second
// browser, the phone in their pocket -- see a locked vault, because the
// unlock belongs to the session that made it rather than to the account
// that owns it.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
	"github.com/tomlawesome/mikroview/internal/logging"
)

// vaultUnlockIdle is how long an unlock survives without being used.
//
// Fifteen minutes, matched to the reason for the feature rather than to
// convenience: the window in which the private key sits in this
// process's memory is the whole cost of unlocking, so it closes on its
// own even when an admin walks away from the tab without locking it.
// Using the unlock -- downloading a backup -- renews it, so a run of
// restores is not interrupted half way. Asking whether it is still open
// does not: a settings tab polling the lock status is not an admin at
// the keyboard, and treating it as one is how an unlock survived a
// weekend (#1120). RunVaultUnlockExpiry closes the window on time with
// no requests at all.
const vaultUnlockIdle = 15 * time.Minute

// vaultUnlockState records which session opened the vault and when it
// last used the unlock.
type vaultUnlockState struct {
	mu        sync.Mutex
	sessionID string
	// userID is the account that session belongs to, recorded here
	// rather than looked up later because the sessions are often
	// already gone by the time it is needed: a password change and a
	// "sign out everywhere" both revoke before they decide what to do
	// about the vault (#1124).
	userID   string
	lastUsed time.Time
}

// claim records sessionID, belonging to userID, as the holder of the
// unlock.
func (u *vaultUnlockState) claim(sessionID, userID string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sessionID = sessionID
	u.userID = userID
	u.lastUsed = now
}

// release forgets the current unlock.
func (u *vaultUnlockState) release() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sessionID = ""
	u.userID = ""
	u.lastUsed = time.Time{}
}

// heldBy reports whether sessionID still holds a live unlock, renewing
// it when it does. For using the unlock, and nothing else.
func (u *vaultUnlockState) heldBy(sessionID string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.liveLocked(sessionID, now) {
		return false
	}
	u.lastUsed = now
	return true
}

// isLive asks the same question without answering it in a way that
// changes it.
//
// The renewal used to be part of the only test there was, so everything
// that merely wanted to know -- the expiry sweep, the status the
// frontend polls -- renewed the very thing it was measuring (#1120). An
// idle timeout that a status poll resets is not a timeout.
func (u *vaultUnlockState) isLive(sessionID string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.liveLocked(sessionID, now)
}

// liveLocked is the shared test. Call with u.mu held.
func (u *vaultUnlockState) liveLocked(sessionID string, now time.Time) bool {
	if u.sessionID == "" || u.sessionID != sessionID {
		return false
	}
	return now.Sub(u.lastUsed) <= vaultUnlockIdle
}

// holder reports which session holds the unlock, or "" for none.
func (u *vaultUnlockState) holder() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sessionID
}

// holding reports the session holding the unlock and the account that
// session belongs to, both "" when nothing holds it.
func (u *vaultUnlockState) holding() (sessionID, userID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sessionID, u.userID
}

// requestSessionID is the caller's session cookie value, or "" if there
// is none. Only ever compared against a session this server issued.
func requestSessionID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// vaultUnlockedFor reports whether r may read vault contents right now,
// and locks the vault when it may not.
//
// Fail-closed, and it actively drops the key rather than only refusing
// the read: an unlock whose session has gone -- signed out, expired,
// revoked by an admin -- or one left idle is not merely unusable, it
// must stop existing in memory, which is the property the feature is
// bought for.
func (s *Server) vaultUnlockedFor(r *http.Request, now time.Time) bool {
	if !s.Vault.PassphraseSet() {
		return true
	}
	sessionID := requestSessionID(r)
	if sessionID == "" || !s.vaultUnlock.heldBy(sessionID, now) {
		// Another session's unlock may still be live and legitimate --
		// only clear the key if the holder itself is gone or stale.
		s.expireVaultUnlock(now)
		return false
	}
	if _, ok := s.Sessions.Validate(sessionID, now); !ok {
		s.lockVault()
		return false
	}
	return true
}

// expireVaultUnlock drops the unlock if whoever holds it has gone idle
// or lost their session.
func (s *Server) expireVaultUnlock(now time.Time) {
	holder := s.vaultUnlock.holder()
	if holder == "" {
		// A key in memory that no session holds: the residue a
		// passphrase change that failed part way leaves behind (#1120).
		// Nothing is ever going to come and claim it, and it used to sit
		// there until the process restarted.
		if s.Vault.PassphraseSet() && !s.Vault.Locked() {
			s.lockVault()
		}
		return
	}
	if s.vaultUnlock.isLive(holder, now) {
		if _, ok := s.Sessions.Validate(holder, now); ok {
			return
		}
	}
	s.lockVault()
}

// vaultUnlockSweep is how often the expiry above runs on its own.
//
// A minute, which is the resolution the fifteen-minute idle window needs
// and no finer: the check is a comparison against one timestamp, but an
// admin who walked away should not have to wait for somebody else's
// request before the key leaves memory (#1120).
const vaultUnlockSweep = time.Minute

// RunVaultUnlockExpiry drops an idle unlock with no help from a request.
//
// Until this existed the expiry was only ever evaluated from the
// download handler, so a vault unlocked on a quiet evening stayed
// unlocked until somebody asked for a file -- which on an instance
// nobody touches overnight means until morning. Runs until ctx ends;
// started by main alongside the other periodic work.
func (s *Server) RunVaultUnlockExpiry(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = vaultUnlockSweep
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireUnlockOnceRecovered()
		}
	}
}

// expireUnlockOnceRecovered isolates panic recovery to a single pass
// rather than the whole loop's lifetime -- a defer in the loop itself
// would end the sweep for good on the first bad pass, and an unrecovered
// one takes the process with it. The shape is
// backupslice.Receiver.sweepOnceRecovered's, for the reason its doc
// comment gives.
func (s *Server) expireUnlockOnceRecovered() {
	defer logging.Recover(apiLog)
	s.expireVaultUnlock(s.now())
}

// lockVault drops the private key and the unlock together. Safe to call
// when nothing is unlocked.
//
// It reports whether the key is gone. The one case where it is not is a
// passphrase change converting the vault right now: that operation is
// working with the key, so Vault.Lock refuses and the unlock stays with
// whoever holds it rather than being released against a vault that is
// still open (#1124).
func (s *Server) lockVault() bool {
	if !s.Vault.Lock() {
		return false
	}
	s.vaultUnlock.release()
	return true
}

// lockVaultForSession locks the vault if sessionID is the session
// holding it open -- called when that session signs out.
func (s *Server) lockVaultForSession(sessionID string) {
	if sessionID != "" && s.vaultUnlock.holder() == sessionID {
		s.lockVault()
	}
}

// lockVaultForUser locks the vault when the unlock belongs to userID --
// the account changing its password, ending all its sessions, or being
// deleted.
//
// Narrower than locking outright, and the difference is who can reach
// the routes. A password change and "sign out everywhere" are open to
// user-role accounts (#653's viewer floor), so locking unconditionally
// let any account end an admin's unlock by changing its own password
// (#1124). It still locks the case those buttons are pressed for: an
// operator acting on a suspected theft ends their *own* other session,
// which is the one holding the key.
//
// A key nobody holds is a different question, and the expiry sweep's:
// it is the residue a passphrase change that failed part way leaves
// behind, and it goes whoever asked.
func (s *Server) lockVaultForUser(userID string) {
	holder, holderUser := s.vaultUnlock.holding()
	if holder == "" {
		s.expireVaultUnlock(s.now())
		return
	}
	if holderUser != "" && holderUser != userID {
		return
	}
	s.lockVault()
}

// callerUserID is the authenticated account's ID, or "" when auth is
// inactive. Only ever compared against an account this server knows.
func callerUserID(r *http.Request) string {
	if u := userFromContext(r); u != nil {
		return u.ID
	}
	return ""
}

type vaultPassphraseRequest struct {
	Passphrase string `json:"passphrase"`
}

// vaultUnlockLimiterKey rate-limits passphrase attempts per account,
// through the same limiter a login uses. Argon2id already makes each
// attempt expensive, but expensive is not the same as bounded, and an
// admin session left open on a shared screen is a plausible way for
// someone to sit and guess.
func vaultUnlockLimiterKey(r *http.Request) string {
	return "router-backup-unlock:" + auditActor(r)
}

// handleRouterBackupUnlock opens the vault for the calling session.
func (s *Server) handleRouterBackupUnlock(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req vaultPassphraseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sessionID := requestSessionID(r)
	if sessionID == "" {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}

	now := time.Now()
	key := vaultUnlockLimiterKey(r)
	if !s.LoginLimiter.Reserve(key, now) {
		http.Error(w, "too many attempts -- wait a little and try again", http.StatusTooManyRequests)
		return
	}

	if err := s.Vault.Unlock(req.Passphrase); err != nil {
		// A wrong passphrase is audited as well as a successful unlock:
		// somebody guessing at the vault passphrase is exactly what an
		// operator reading this log later wants to see. Only a wrong
		// one, though -- recording "no passphrase is set" under the same
		// action diluted the one entry that means someone is guessing
		// (#1120), which is the same restriction the remove handler
		// already applies.
		if errors.Is(err, backupvault.ErrWrongPassphrase) {
			s.Audit.Record(auditActor(r), "router_backup.unlock_failed", "vault", "")
		}
		writeVaultLockError(w, err)
		return
	}
	s.LoginLimiter.Release(key, now)
	s.vaultUnlock.claim(sessionID, callerUserID(r), now)
	s.Audit.Record(auditActor(r), "router_backup.unlocked", "vault",
		fmt.Sprintf("idle timeout=%s", vaultUnlockIdle))
	writeJSON(w, http.StatusOK, s.vaultLockStatus(r, now))
}

// handleRouterBackupLock closes the vault again, from any admin: a lock
// is never something to be refused on the grounds that somebody else
// opened it.
func (s *Server) handleRouterBackupLock(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.Vault.PassphraseSet() {
		writeVaultLockError(w, backupvault.ErrNoPassphrase)
		return
	}
	if !s.lockVault() {
		// A passphrase change is converting the vault with the very key
		// this would drop. Saying so is the honest answer: the button
		// works again the moment that finishes.
		writeVaultLockError(w, backupvault.ErrPassphraseBusy)
		return
	}
	s.Audit.Record(auditActor(r), "router_backup.locked", "vault", "")
	writeJSON(w, http.StatusOK, s.vaultLockStatus(r, time.Now()))
}

// handleRouterBackupSetPassphrase turns the lock on.
func (s *Server) handleRouterBackupSetPassphrase(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req vaultPassphraseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sessionID := requestSessionID(r)
	if sessionID == "" {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}

	if err := s.Vault.SetPassphrase(req.Passphrase); err != nil {
		if errors.Is(err, backupvault.ErrResealIncomplete) {
			// The passphrase is set and the vault is open. Claim and
			// audit both, or the key sits in this process's memory with
			// no session holding it and no record of how it got there
			// (#1120) -- which is exactly the state the feature promises
			// never to be in.
			now := time.Now()
			s.vaultUnlock.claim(sessionID, callerUserID(r), now)
			s.Audit.Record(auditActor(r), "router_backup.passphrase_set", "vault",
				"not every stored backup was re-sealed -- the vault holds a mix of both schemes")
			http.Error(w, "the vault passphrase is set and every stored backup is still readable, but not all of them were re-sealed -- the server log names the file that stopped it", http.StatusInternalServerError)
			return
		}
		writeVaultLockError(w, err)
		return
	}
	// SetPassphrase leaves the vault open; the session that set it holds
	// that unlock, on the same terms as any other.
	now := time.Now()
	s.vaultUnlock.claim(sessionID, callerUserID(r), now)
	s.Audit.Record(auditActor(r), "router_backup.passphrase_set", "vault",
		"stored backups re-sealed; mikroview can no longer read them unaided")
	writeJSON(w, http.StatusOK, s.vaultLockStatus(r, now))
}

// handleRouterBackupRemovePassphrase turns the lock off, which needs the
// passphrase currently on it.
func (s *Server) handleRouterBackupRemovePassphrase(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req vaultPassphraseRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	now := time.Now()
	key := vaultUnlockLimiterKey(r)
	if !s.LoginLimiter.Reserve(key, now) {
		http.Error(w, "too many attempts -- wait a little and try again", http.StatusTooManyRequests)
		return
	}
	if err := s.Vault.RemovePassphrase(req.Passphrase); err != nil {
		switch {
		case errors.Is(err, backupvault.ErrWrongPassphrase):
			s.Audit.Record(auditActor(r), "router_backup.unlock_failed", "vault", "while removing the passphrase")
		case errors.Is(err, backupvault.ErrResealIncomplete):
			// Removing unlocks the vault to do its work. The vault has
			// already put itself back the way it was found; this drops
			// the key as well if nobody is holding it, and says which
			// state the vault ended up in rather than leaving the
			// operator to find out by trying a download (#1120).
			s.expireVaultUnlock(now)
			s.Audit.Record(auditActor(r), "router_backup.passphrase_remove_failed", "vault",
				"not every stored backup could be re-sealed; the passphrase is still set and the vault is "+vaultStateWord(s.Vault.Locked()))
			http.Error(w, "the vault passphrase was not removed -- not every stored backup could be re-sealed, so it is still needed to read them", http.StatusInternalServerError)
			return
		}
		writeVaultLockError(w, err)
		return
	}
	s.LoginLimiter.Release(key, now)
	s.vaultUnlock.release()
	s.Audit.Record(auditActor(r), "router_backup.passphrase_removed", "vault",
		"stored backups are readable with the retention key again")
	writeJSON(w, http.StatusOK, s.vaultLockStatus(r, now))
}

// vaultLockStatusResponse is what every control above returns, so the
// frontend never has to infer the new state from which call it made.
type vaultLockStatusResponse struct {
	PassphraseSet bool `json:"passphraseSet"`
	// Locked is the vault's own state: no private key held.
	Locked bool `json:"locked"`
	// UnlockedForYou is what the calling tab can actually do, which is
	// the narrower question: another admin's unlock leaves Locked false
	// and this false.
	UnlockedForYou bool `json:"unlockedForYou"`
	// MinPassphraseLength lets the form refuse early rather than making
	// the server say no to something it could have said first.
	MinPassphraseLength int `json:"minPassphraseLength"`
	IdleTimeoutSeconds  int `json:"idleTimeoutSeconds"`
}

func (s *Server) vaultLockStatus(r *http.Request, now time.Time) vaultLockStatusResponse {
	set := s.Vault.PassphraseSet()
	return vaultLockStatusResponse{
		PassphraseSet:       set,
		Locked:              s.Vault.Locked(),
		UnlockedForYou:      set && s.vaultUnlock.isLive(requestSessionID(r), now),
		MinPassphraseLength: backupvault.MinPassphraseRunes,
		IdleTimeoutSeconds:  int(vaultUnlockIdle.Seconds()),
	}
}

// writeVaultLockError maps the vault's refusals onto status codes. A
// wrong passphrase is 403 rather than 401: the caller is authenticated,
// they simply cannot open this.
func writeVaultLockError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backupvault.ErrWrongPassphrase):
		http.Error(w, "that passphrase does not open the vault", http.StatusForbidden)
	case errors.Is(err, backupvault.ErrPassphraseTooShort):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, backupvault.ErrPassphraseSet):
		http.Error(w, "a vault passphrase is already set", http.StatusConflict)
	case errors.Is(err, backupvault.ErrNoPassphrase):
		http.Error(w, "no vault passphrase is set", http.StatusConflict)
	case errors.Is(err, backupvault.ErrPassphraseBusy):
		http.Error(w, "another vault passphrase change is in progress -- try again when it has finished", http.StatusConflict)
	case errors.Is(err, backupvault.ErrDisabled):
		http.Error(w, "the router-backup vault is not enabled", http.StatusConflict)
	default:
		http.Error(w, "the vault refused that", http.StatusInternalServerError)
	}
}

// vaultStateWord is how an audit entry names the vault's state, so an
// operator reading back a failed passphrase change can see whether the
// key was left in memory without going and asking the running process.
func vaultStateWord(locked bool) string {
	if locked {
		return "locked"
	}
	return "unlocked"
}
