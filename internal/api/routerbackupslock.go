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
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// vaultUnlockIdle is how long an unlock survives without being used.
//
// Fifteen minutes, matched to the reason for the feature rather than to
// convenience: the window in which the private key sits in this
// process's memory is the whole cost of unlocking, so it closes on its
// own even when an admin walks away from the tab without locking it.
// Downloading a backup renews it, so a run of restores is not
// interrupted half way.
const vaultUnlockIdle = 15 * time.Minute

// vaultUnlockState records which session opened the vault and when it
// last used the unlock.
type vaultUnlockState struct {
	mu        sync.Mutex
	sessionID string
	lastUsed  time.Time
}

// claim records sessionID as the holder of the unlock.
func (u *vaultUnlockState) claim(sessionID string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sessionID = sessionID
	u.lastUsed = now
}

// release forgets the current unlock.
func (u *vaultUnlockState) release() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sessionID = ""
	u.lastUsed = time.Time{}
}

// heldBy reports whether sessionID still holds a live unlock, renewing
// it when it does.
func (u *vaultUnlockState) heldBy(sessionID string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sessionID == "" || u.sessionID != sessionID {
		return false
	}
	if now.Sub(u.lastUsed) > vaultUnlockIdle {
		return false
	}
	u.lastUsed = now
	return true
}

// holder reports which session holds the unlock, or "" for none.
func (u *vaultUnlockState) holder() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sessionID
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
		return
	}
	if s.vaultUnlock.heldBy(holder, now) {
		if _, ok := s.Sessions.Validate(holder, now); ok {
			return
		}
	}
	s.lockVault()
}

// lockVault drops the private key and the unlock together. Safe to call
// when nothing is unlocked.
func (s *Server) lockVault() {
	s.Vault.Lock()
	s.vaultUnlock.release()
}

// lockVaultForSession locks the vault if sessionID is the session
// holding it open -- called when that session signs out.
func (s *Server) lockVaultForSession(sessionID string) {
	if sessionID != "" && s.vaultUnlock.holder() == sessionID {
		s.lockVault()
	}
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
		// A refused attempt is audited as well as a successful one:
		// somebody guessing at the vault passphrase is exactly what an
		// operator reading this log later wants to see.
		s.Audit.Record(auditActor(r), "router_backup.unlock_failed", "vault", "")
		writeVaultLockError(w, err)
		return
	}
	s.LoginLimiter.Release(key, now)
	s.vaultUnlock.claim(sessionID, now)
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
	s.lockVault()
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
		writeVaultLockError(w, err)
		return
	}
	// SetPassphrase leaves the vault open; the session that set it holds
	// that unlock, on the same terms as any other.
	now := time.Now()
	s.vaultUnlock.claim(sessionID, now)
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
		if errors.Is(err, backupvault.ErrWrongPassphrase) {
			s.Audit.Record(auditActor(r), "router_backup.unlock_failed", "vault", "while removing the passphrase")
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
		UnlockedForYou:      set && s.vaultUnlock.heldBy(requestSessionID(r), now),
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
	case errors.Is(err, backupvault.ErrDisabled):
		http.Error(w, "the router-backup vault is not enabled", http.StatusConflict)
	default:
		http.Error(w, "the vault refused that", http.StatusInternalServerError)
	}
}
