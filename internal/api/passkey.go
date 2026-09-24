// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/tomlawesome/mikroview/internal/auth"
)

// #1250, wave 2 slice D: the passkey (WebAuthn) HTTP routes, built against
// docs/plans/passkeys-second-factor.md -- read that first for the request/
// response shapes, the refusals, and the reasoning behind each. This file
// owns the ceremony boundary: converting between internal/auth's storage
// shape (Passkey, deliberately go-webauthn-free -- see its own doc comment)
// and go-webauthn's own types, driving BeginRegistration/CreateCredential/
// BeginLogin/ValidateLogin, and everything else that has to happen either
// side of those calls (sessions, recovery codes, audit, the two ceremony
// cookies webauthn.go's codecs seal).
//
// handleAuthLogin and handleAuthLoginFactor themselves stay in auth.go
// (they are login-flow endpoints first, exactly like #1249's TOTP code
// path lives there rather than in a hypothetical totp.go) -- this file
// supplies verifyPasskeyAssertion, which handleAuthLoginFactor calls into
// when the request body carries an assertion rather than a TOTP code.

// ---- cookie constants ----

const (
	// passkeyRegisterCookieName/Path carry the sealed webauthn.SessionData
	// between POST .../register/begin and .../register/finish -- see the
	// design's "Routes" section. Path is the whole /api/auth/passkeys
	// prefix, not just the two ceremony routes, for the same reason
	// pendingLoginCookiePath is a prefix rather than two exact paths: one
	// Path value covers every route that could plausibly need it without
	// widening to the whole API.
	passkeyRegisterCookieName = "mikroview_passkey_register"
	passkeyRegisterCookiePath = "/api/auth/passkeys"

	// passkeyAssertCookieName/Path are the login-ceremony counterpart,
	// carrying SessionData from POST /api/auth/login/factor/begin to
	// POST /api/auth/login/factor's assertion branch. Path is
	// "/api/auth/login" -- the same prefix pendingLoginCookiePath already
	// uses, and for the identical reason: it covers /api/auth/login,
	// /api/auth/login/factor and /api/auth/login/factor/begin in one
	// value.
	passkeyAssertCookieName = "mikroview_passkey_assert"
	passkeyAssertCookiePath = "/api/auth/login"

	// passkeyCeremonyCookieMaxAge is five minutes for both ceremony
	// cookies, matching pendingLoginCookieMaxAge's own reasoning and the
	// design's own number: long enough to unlock an authenticator app or
	// touch a security key, short enough that an abandoned ceremony
	// doesn't leave a live one-shot ticket sitting in a browser.
	//
	// The cookie's own Max-Age is a client-side instruction, not a
	// server-side guarantee -- an old cookie value presented past that
	// window would otherwise still decode successfully, since go-webauthn
	// only enforces SessionData.Expires when Config.Timeouts.*.Enforce is
	// set, and webauthn.go (wave 1) leaves that at its default (false), so
	// the library's own check is a no-op with this configuration. Both
	// begin handlers below set Expires themselves on the SessionData
	// before sealing it, which is enough to make CreateCredential/
	// ValidateLogin's own `session.Expires.Before(time.Now())` check bite
	// without needing to touch webauthn.go (out of this slice's files) or
	// re-implement expiry a third time the way webauthnSessionCodec's own
	// doc comment explains it deliberately does not.
	passkeyCeremonyCookieMaxAge = 5 * time.Minute
)

func (s *Server) clearPasskeyRegisterCookie(w http.ResponseWriter) {
	s.writeCookie(w, passkeyRegisterCookieName, "", passkeyRegisterCookiePath, -1)
}

func (s *Server) clearPasskeyAssertCookie(w http.ResponseWriter) {
	s.writeCookie(w, passkeyAssertCookieName, "", passkeyAssertCookiePath, -1)
}

// ---- go-webauthn <-> internal/auth boundary ----

// webauthnUser adapts an account (an ID/username pair and a chosen subset
// of its stored passkeys) to the go-webauthn User interface every
// ceremony function needs. It carries no policy of its own about which
// passkeys belong in a given ceremony -- the caller decides that at each
// call site (current-RPID-only for a registration's excludeCredentials,
// non-stale-only for a login ceremony's allowed/owned credentials, see
// nonStalePasskeys below) and builds the credentials slice accordingly.
type webauthnUser struct {
	id          []byte
	username    string
	credentials []webauthn.Credential
}

func (u webauthnUser) WebAuthnID() []byte                         { return u.id }
func (u webauthnUser) WebAuthnName() string                       { return u.username }
func (u webauthnUser) WebAuthnDisplayName() string                { return u.username }
func (u webauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// passkeyToCredential converts a stored auth.Passkey into the go-webauthn
// Credential shape the library's ceremony functions operate on. A
// straight field-by-field copy, not a translation -- see auth.Passkey's
// own doc comment for why Transports is []string and Flags is a
// package-local struct with matching field names: internal/auth
// deliberately does not depend on go-webauthn, so the conversion lives
// here at the one boundary that does.
func passkeyToCredential(pk auth.Passkey) webauthn.Credential {
	var transports []protocol.AuthenticatorTransport
	if len(pk.Transports) > 0 {
		transports = make([]protocol.AuthenticatorTransport, len(pk.Transports))
		for i, t := range pk.Transports {
			transports[i] = protocol.AuthenticatorTransport(t)
		}
	}
	return webauthn.Credential{
		ID:        pk.ID,
		PublicKey: pk.PublicKey,
		Transport: transports,
		Flags: webauthn.CredentialFlags{
			UserPresent:    pk.Flags.UserPresent,
			UserVerified:   pk.Flags.UserVerified,
			BackupEligible: pk.Flags.BackupEligible,
			BackupState:    pk.Flags.BackupState,
		},
		Authenticator: webauthn.Authenticator{SignCount: pk.SignCount},
	}
}

// credentialToPasskey is passkeyToCredential's inverse, building the
// record AddPasskey stores from what a just-completed registration
// ceremony returned. rpID is always the relying party's *current* RPID --
// a passkey is only ever created against the server it's being created
// on -- and name is the caller-supplied label, normalised by AddPasskey
// itself.
func credentialToPasskey(cred webauthn.Credential, rpID, name string, now time.Time) auth.Passkey {
	transports := make([]string, len(cred.Transport))
	for i, t := range cred.Transport {
		transports[i] = string(t)
	}
	return auth.Passkey{
		ID:         cred.ID,
		PublicKey:  cred.PublicKey,
		SignCount:  cred.Authenticator.SignCount,
		Transports: transports,
		Flags: auth.PasskeyFlags{
			UserPresent:    cred.Flags.UserPresent,
			UserVerified:   cred.Flags.UserVerified,
			BackupEligible: cred.Flags.BackupEligible,
			BackupState:    cred.Flags.BackupState,
		},
		RPID:      rpID,
		Name:      name,
		CreatedAt: now,
	}
}

// nonStalePasskeys returns the subset of passkeys registered under the
// relying party's *current* RPID -- the design's "when publicUrl is set
// but later changes" story: excluded from every login ceremony (both
// BeginLogin's allowed-credentials list and the webauthnUser
// FinishLogin/ValidateLogin checks it against), but still returned by
// GET /api/auth/passkeys and still removable. Every caller of this
// already knows the RP is ready (that's checked separately, with its own
// user-facing refusal); this only ever narrows an already-usable set.
func (s *Server) nonStalePasskeys(passkeys []auth.Passkey) []auth.Passkey {
	if s.RelyingParty == nil || s.RelyingParty.Status != PasskeyStatusReady {
		return nil
	}
	out := make([]auth.Passkey, 0, len(passkeys))
	for _, pk := range passkeys {
		if pk.RPID == s.RelyingParty.RPID {
			out = append(out, pk)
		}
	}
	return out
}

// webauthnCredentialsFor converts a slice of stored passkeys into the
// go-webauthn Credential slice a webauthnUser carries -- the last step
// nonStalePasskeys' (or any other filtered) result needs before it can be
// handed to BeginLogin/ValidateLogin.
func webauthnCredentialsFor(passkeys []auth.Passkey) []webauthn.Credential {
	out := make([]webauthn.Credential, len(passkeys))
	for i, pk := range passkeys {
		out[i] = passkeyToCredential(pk)
	}
	return out
}

// writePasskeysUnavailable answers the refusal every ceremony-starting
// route gives when the relying party isn't ready -- see PasskeyStatus.
// The reason travels as the status word itself; the specific, friendly
// copy per status lives in the frontend (PasskeysOverlay's `unavailable`
// state, per the design), which already has GET /api/auth/session's
// passkeys.status to read it from, so this message only has to be
// diagnosable, not polished.
func writePasskeysUnavailable(w http.ResponseWriter, rp *RelyingParty) {
	status := PasskeyStatusUnset
	if rp != nil {
		status = rp.Status
	}
	http.Error(w, fmt.Sprintf("passkeys are not available on this deployment (%s)", status), http.StatusServiceUnavailable)
}

func (s *Server) passkeysReady() bool {
	return s.RelyingParty != nil && s.RelyingParty.Status == PasskeyStatusReady
}

// ---- GET /api/auth/passkeys ----

// passkeySummary is one row of the passkey list -- feeds PasskeysOverlay
// directly (frontend slice E).
type passkeySummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
	Transports []string  `json:"transports,omitempty"`
	// Stale is true only when the RP is ready *and* this credential's
	// RPID differs from the current one -- the design's own "stale
	// passkey" story (publicUrl changed after this was registered). It is
	// deliberately false whenever the RP isn't ready at all: that is a
	// different, deployment-wide problem (reported on GET /api/auth/
	// session's passkeys.status, and drawn as PasskeysOverlay's whole-
	// overlay `unavailable` state, not a per-row tag), not "every stored
	// passkey just became stale".
	Stale bool   `json:"stale"`
	RPID  string `json:"rpId"`
}

func (s *Server) currentRPID() string {
	if s.RelyingParty != nil && s.RelyingParty.Status == PasskeyStatusReady {
		return s.RelyingParty.RPID
	}
	return ""
}

func toPasskeySummary(pk auth.Passkey, currentRPID string) passkeySummary {
	return passkeySummary{
		ID:         base64.RawURLEncoding.EncodeToString(pk.ID),
		Name:       pk.Name,
		CreatedAt:  pk.CreatedAt,
		LastUsedAt: pk.LastUsedAt,
		Transports: pk.Transports,
		Stale:      currentRPID != "" && pk.RPID != currentRPID,
		RPID:       pk.RPID,
	}
}

// handleAuthPasskeysList answers the signed-in caller's own registered
// passkeys, stale ones included (still shown, still removable -- only
// excluded from a *login* ceremony's credential set). Same "acts only on
// the session's own account" shape as GET /api/auth/session's HasTOTP:
// userFromContext's copy is fresh enough here, since nothing earlier in
// this request wrote to it.
func (s *Server) handleAuthPasskeysList(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	currentRPID := s.currentRPID()
	out := make([]passkeySummary, 0, len(user.Passkeys))
	for _, pk := range user.Passkeys {
		out = append(out, toPasskeySummary(pk, currentRPID))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- POST /api/auth/passkeys/register/begin ----

func (s *Server) handleAuthPasskeysRegisterBegin(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	if !s.passkeysReady() {
		writePasskeysUnavailable(w, s.RelyingParty)
		return
	}

	// Re-read rather than trust userFromContext's copy, the same
	// discipline #1249's TOTP handlers use (see auth.go's "#1249" section
	// header comment) -- excludeCredentials has to reflect this account's
	// passkeys as of right now, not as of whenever requireAuth resolved
	// the session a moment earlier.
	current, ok := s.Auth.Get(user.ID)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	// excludeCredentials: current-RPID passkeys only, per the design --
	// an account whose only passkeys are stale (registered under a
	// previous publicUrl) is not stopped from registering a fresh one for
	// the address it's actually being reached on now, since those old
	// credentials could never complete a ceremony here anyway.
	var exclude []protocol.CredentialDescriptor
	for _, pk := range current.Passkeys {
		if pk.RPID == s.RelyingParty.RPID {
			cred := passkeyToCredential(pk)
			exclude = append(exclude, cred.Descriptor())
		}
	}

	wu := webauthnUser{id: []byte(current.ID), username: current.Username}
	creation, session, err := s.RelyingParty.WebAuthn.BeginRegistration(wu, webauthn.WithExclusions(exclude))
	if err != nil {
		authLog.Error(fmt.Sprintf("beginning passkey registration for %s: %v", current.Username, err))
		http.Error(w, "unable to start passkey registration", http.StatusInternalServerError)
		return
	}
	// See passkeyCeremonyCookieMaxAge's doc comment: this is what actually
	// bounds the ceremony's lifetime server-side, since go-webauthn's own
	// Expires enforcement is off by default and webauthn.go (out of this
	// slice) doesn't turn it on.
	session.Expires = time.Now().Add(passkeyCeremonyCookieMaxAge)

	encoded, err := passkeyRegisterSessionCodec.encode(*session)
	if err != nil {
		authLog.Error(fmt.Sprintf("sealing passkey registration session for %s: %v", current.Username, err))
		http.Error(w, "unable to start passkey registration", http.StatusInternalServerError)
		return
	}
	s.writeCookie(w, passkeyRegisterCookieName, encoded, passkeyRegisterCookiePath, int(passkeyCeremonyCookieMaxAge.Seconds()))

	writeJSON(w, http.StatusOK, creation)
}

// ---- POST /api/auth/passkeys/register/finish ----

type passkeyRegisterFinishRequest struct {
	// Credential is the raw JSON PublicKeyCredential.toJSON() (or the fake
	// authenticator's RegisterResponse in tests) produced -- handed to
	// go-webauthn's own parser unchanged, not re-shaped here.
	Credential json.RawMessage `json:"credential"`
	Name       string          `json:"name"`
}

type passkeyRegisterFinishResponse struct {
	Passkey passkeySummary `json:"passkey"`
	// RecoveryCodes is nil (JSON null) except the moment this is the
	// account's first second factor of either kind -- see the design's
	// "Recovery codes are shared" section. No omitempty: the frontend
	// contract is exactly `recoveryCodes: [...]|null`, not an absent key.
	RecoveryCodes []string `json:"recoveryCodes"`
}

// handleAuthPasskeysRegisterFinish completes a registration ceremony
// POST .../register/begin started: parse -> CreateCredential -> AddPasskey
// -> (first factor only) revoke-and-reissue sessions, mint-if-absent
// recovery codes -> clear the cookie.
//
// The cookie is read but deliberately *not* cleared until the ceremony
// and the store write both succeed -- the same "a wrong attempt doesn't
// burn the ticket" shape handleAuthLoginFactor already gives the pending-
// login cookie for a wrong TOTP code. A malformed body, a ceremony the
// library refuses (wrong origin, wrong RPID, a tampered response), or a
// store-layer refusal (duplicate, limit reached) all leave the sealed
// session in place so a client that sends a corrected request can still
// finish inside the same five-minute window, rather than being forced back
// to register/begin over a problem that had nothing to do with the
// challenge itself. Only a genuinely unreadable cookie (missing or fails
// to decode) has nothing left to preserve.
func (s *Server) handleAuthPasskeysRegisterFinish(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	if !s.passkeysReady() {
		writePasskeysUnavailable(w, s.RelyingParty)
		return
	}

	cookie, err := r.Cookie(passkeyRegisterCookieName)
	if err != nil {
		writeUnauthorized(w, "start registration again")
		return
	}
	session, err := passkeyRegisterSessionCodec.decode(cookie.Value)
	if err != nil {
		s.clearPasskeyRegisterCookie(w)
		writeUnauthorized(w, "start registration again")
		return
	}

	var req passkeyRegisterFinishRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	current, ok := s.Auth.Get(user.ID)
	if !ok {
		writeUnauthorized(w, "sign in first")
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		http.Error(w, "that passkey couldn't be registered -- try again", http.StatusBadRequest)
		return
	}

	wu := webauthnUser{id: []byte(current.ID), username: current.Username}
	cred, err := s.RelyingParty.WebAuthn.CreateCredential(wu, session, parsed)
	if err != nil {
		http.Error(w, "that passkey couldn't be registered -- try again", http.StatusBadRequest)
		return
	}

	now := time.Now()
	pk := credentialToPasskey(*cred, s.RelyingParty.RPID, req.Name, now)
	wasFirstFactor := !current.HasSecondFactor()

	stored, err := s.Auth.AddPasskey(current.ID, pk)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, auth.ErrPasskeyDuplicate), errors.Is(err, auth.ErrPasskeyLimitReached):
			status = http.StatusConflict
		}
		writeAuthError(w, r, err, status)
		return
	}

	// Mint-if-absent, never re-mint -- see the design's "Recovery codes
	// are shared" section, and handleTOTPConfirm's identical rule just
	// above it in auth.go.
	var recoveryCodes []string
	if len(current.RecoveryCodes) == 0 {
		codes, err := s.Auth.GenerateRecoveryCodes(current.ID, now)
		if err != nil {
			authLog.Error(fmt.Sprintf("generating recovery codes for %s after registering a passkey: %v", current.Username, err))
		} else {
			recoveryCodes = codes
		}
	}

	if wasFirstFactor {
		// Parity with handleTOTPConfirm: turning on the account's first
		// second factor is exactly the moment a stale or forgotten
		// session elsewhere should not get to ride along unchallenged.
		s.Sessions.RevokeAllForUser(current.ID)
		sess := s.Sessions.Create(current.ID, now)
		s.setSessionCookie(w, sess.ID)
	}

	s.clearPasskeyRegisterCookie(w)
	s.Audit.Record(current.Username, "account.passkey_added", current.Username, "name="+stored.Name)

	writeJSON(w, http.StatusOK, passkeyRegisterFinishResponse{
		Passkey:       toPasskeySummary(stored, s.RelyingParty.RPID),
		RecoveryCodes: recoveryCodes,
	})
}

// ---- PATCH /api/auth/passkeys/{id} ----

func decodePasskeyID(raw string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(raw)
}

type passkeyRenameRequest struct {
	Name string `json:"name"`
}

// handleAuthPasskeyRename renames one of the caller's own passkeys.
// Cosmetic and reversible -- no password check, same as RenamePasskey's
// own doc comment explains, unlike DELETE below.
func (s *Server) handleAuthPasskeyRename(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	var req passkeyRenameRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	credID, err := decodePasskeyID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid passkey id", http.StatusBadRequest)
		return
	}

	pk, err := s.Auth.RenamePasskey(user.ID, credID, req.Name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrPasskeyNotFound) {
			status = http.StatusNotFound
		}
		writeAuthError(w, r, err, status)
		return
	}
	writeJSON(w, http.StatusOK, toPasskeySummary(pk, s.currentRPID()))
}

// ---- DELETE /api/auth/passkeys/{id} ----

type passkeyDeleteRequest struct {
	Password string `json:"password"`
}

// handleAuthPasskeyDelete removes one of the caller's own passkeys,
// gated by their password -- same passwordRecheckLimiterKey budget
// handleTOTPDelete uses, and the same reasoning: this is a guess at a
// live credential made by a caller who already holds a session, exactly
// the position a stolen-cookie attacker would be in.
func (s *Server) handleAuthPasskeyDelete(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	var req passkeyDeleteRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	credID, err := decodePasskeyID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid passkey id", http.StatusBadRequest)
		return
	}

	now := time.Now()
	userKey := passwordRecheckLimiterKey(user.Username)
	if !s.LoginLimiter.Reserve(userKey, now) {
		http.Error(w, "too many attempts, try again later", http.StatusTooManyRequests)
		return
	}
	if _, err := s.Auth.Authenticate(user.Username, req.Password, now); err != nil {
		writeUnauthorized(w, "incorrect password")
		return
	}
	s.LoginLimiter.Release(userKey, now)

	removed, err := s.Auth.DeletePasskey(user.ID, credID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrPasskeyNotFound) {
			status = http.StatusNotFound
		}
		writeAuthError(w, r, err, status)
		return
	}

	s.Audit.Record(user.Username, "account.passkey_removed", user.Username, "name="+removed.Name)
	writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

// ---- POST /api/auth/login/factor/begin ----

// loginFactorBeginRequest is `{}` -- nothing in the body is used, but it
// is still decoded (rather than skipped) so an empty/malformed request
// fails at that check with 400, the same "reject the body before
// touching anything else" shape every other accessPublic route in this
// package already follows (handleAuthLogin, handleAuthLoginFactor,
// handleAuthRegister all decode first). Without this, an authorization-
// matrix probe carrying no body at all would fall through to the pending-
// cookie check below and come back 401 -- indistinguishable, to that
// test, from the route actually being gated on a session it doesn't
// have.
type loginFactorBeginRequest struct{}

// handleAuthLoginFactorBegin starts the passkey half of #1249's second
// login step. Reached with the same pending-login cookie
// POST /api/auth/login/factor itself reads -- that cookie is what proves
// the password already checked out, so this has to work before (and
// without) a session, exactly like /api/auth/login and
// /api/auth/login/factor.
func (s *Server) handleAuthLoginFactorBegin(w http.ResponseWriter, r *http.Request) {
	var req loginFactorBeginRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cookie, err := r.Cookie(pendingLoginCookieName)
	if err != nil {
		writeUnauthorized(w, "sign in again")
		return
	}
	now := time.Now()
	st, err := pendingLoginCodec.decode(cookie.Value, now)
	if err != nil {
		s.clearPendingLoginCookie(w)
		writeUnauthorized(w, "sign in again")
		return
	}
	user, ok := s.Auth.Get(st.UserID)
	if !ok {
		s.clearPendingLoginCookie(w)
		writeUnauthorized(w, "sign in again")
		return
	}

	if !s.passkeysReady() {
		writePasskeysUnavailable(w, s.RelyingParty)
		return
	}

	nonStale := s.nonStalePasskeys(user.Passkeys)
	if len(nonStale) == 0 {
		http.Error(w, "this account has no passkey usable at this address", http.StatusConflict)
		return
	}

	wu := webauthnUser{id: []byte(user.ID), username: user.Username, credentials: webauthnCredentialsFor(nonStale)}
	assertion, session, err := s.RelyingParty.WebAuthn.BeginLogin(wu)
	if err != nil {
		authLog.Error(fmt.Sprintf("beginning passkey login for %s: %v", user.Username, err))
		http.Error(w, "unable to start passkey sign-in", http.StatusInternalServerError)
		return
	}
	session.Expires = now.Add(passkeyCeremonyCookieMaxAge) // see passkeyCeremonyCookieMaxAge's doc comment.

	encoded, err := passkeyAssertSessionCodec.encode(*session)
	if err != nil {
		authLog.Error(fmt.Sprintf("sealing passkey login session for %s: %v", user.Username, err))
		http.Error(w, "unable to start passkey sign-in", http.StatusInternalServerError)
		return
	}
	s.writeCookie(w, passkeyAssertCookieName, encoded, passkeyAssertCookiePath, int(passkeyCeremonyCookieMaxAge.Seconds()))

	writeJSON(w, http.StatusOK, assertion)
}

// ---- the passkey branch of POST /api/auth/login/factor ----

// verifyPasskeyAssertion is handleAuthLoginFactor's (auth.go) passkey
// branch, called when the request body carries an `assertion` rather
// than a `code`. Writes its own response and returns false on every
// refusal; the caller (handleAuthLoginFactor) is responsible for nothing
// beyond calling completeLoginFactor when this returns true -- the
// LoginLimiter reservations it shares with the TOTP/recovery-code
// branches are reserved and released by the caller, exactly as the
// design's "Shares the same LoginLimiter reservations as the code path"
// says.
//
// Sign count: per the design, go-webauthn's own CloneWarning (set by
// Authenticator.UpdateCounter, which never fires on a 0 -> 0 assertion --
// see FakeAuthenticator.SignCount's doc comment) is what decides a
// refusal, not a bare "did the count go backwards" comparison of our own
// -- that distinction is exactly what stops a platform authenticator that
// always reports zero from being falsely treated as a clone.
func (s *Server) verifyPasskeyAssertion(w http.ResponseWriter, r *http.Request, user *auth.User, assertion json.RawMessage, now time.Time) bool {
	if !s.passkeysReady() {
		writePasskeysUnavailable(w, s.RelyingParty)
		return false
	}

	cookie, err := r.Cookie(passkeyAssertCookieName)
	if err != nil {
		writeUnauthorized(w, "start passkey sign-in again")
		return false
	}
	session, err := passkeyAssertSessionCodec.decode(cookie.Value)
	if err != nil {
		s.clearPasskeyAssertCookie(w)
		writeUnauthorized(w, "start passkey sign-in again")
		return false
	}

	nonStale := s.nonStalePasskeys(user.Passkeys)
	wu := webauthnUser{id: []byte(user.ID), username: user.Username, credentials: webauthnCredentialsFor(nonStale)}

	parsed, err := protocol.ParseCredentialRequestResponseBytes(assertion)
	if err != nil {
		writeUnauthorized(w, "that passkey couldn't be verified -- use another way in")
		return false
	}

	cred, err := s.RelyingParty.WebAuthn.ValidateLogin(wu, session, parsed)
	if err != nil {
		writeUnauthorized(w, "that passkey couldn't be verified -- use another way in")
		return false
	}

	if cred.Authenticator.CloneWarning {
		// UpdateCounter leaves Authenticator.SignCount at the previously
		// *stored* value when it sets CloneWarning (see its own doc
		// comment in go-webauthn), so cred.Authenticator.SignCount here is
		// exactly the count RecordPasskeyAssertion would otherwise have
		// left untouched -- reading it back out for the audit line rather
		// than re-deriving it from user.Passkeys.
		presented := parsed.Response.AuthenticatorData.Counter
		s.Audit.Record(user.Username, "account.passkey_clone_suspected", user.Username,
			fmt.Sprintf("credential=%s presentedCount=%d storedCount=%d",
				base64.RawURLEncoding.EncodeToString(cred.ID), presented, cred.Authenticator.SignCount))
		writeUnauthorized(w, "that passkey couldn't be verified -- use another way in")
		return false
	}

	// Accepted and recorded in one call, under the store's lock, so two
	// concurrent submissions of the same assertion can't both clear
	// CloneWarning against the same not-yet-advanced counter -- see
	// RecordPasskeyAssertionIfFresh's doc comment (passkeys.go).
	accepted, err := s.Auth.RecordPasskeyAssertionIfFresh(user.ID, cred.ID, cred.Authenticator.SignCount, now)
	if err != nil {
		// Same stance handleAuthLoginFactor's own TOTP branch takes: a
		// persistence failure on an otherwise-accepted assertion is
		// logged, not turned into a refusal of a login that already
		// earned one (accepted stays true in that case -- see the
		// method's doc comment).
		authLog.Warn(fmt.Sprintf("recording passkey assertion for %s: %v", user.Username, err))
	}
	if !accepted {
		writeUnauthorized(w, "that passkey couldn't be verified -- use another way in")
		return false
	}

	s.clearPasskeyAssertCookie(w)
	return true
}

// ---- DELETE /api/auth/users/{id}/passkeys ----

// handleAuthPasskeysAdminClear lets an admin remove another user's
// passkeys from the Users group -- mirrors handleTOTPAdminClear
// (auth.go) exactly, including refusing the caller's own account for the
// identical reason (mikroview holds exactly one admin, so that account's
// own lost passkeys are `mikroview -clear-second-factor` at the console,
// not this route).
func (s *Server) handleAuthPasskeysAdminClear(w http.ResponseWriter, r *http.Request) {
	if !callerIsAdmin(r) {
		http.Error(w, "admin role required", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "user id is required", http.StatusBadRequest)
		return
	}
	if caller := userFromContext(r); caller != nil && caller.ID == id {
		http.Error(w, "an administrator cannot clear their own passkeys here -- "+
			"use `mikroview -clear-second-factor` at the console", http.StatusConflict)
		return
	}

	target, ok := s.Auth.Get(id)
	if !ok {
		http.Error(w, "no such user", http.StatusNotFound)
		return
	}
	if err := s.Auth.ClearPasskeys(id); err != nil {
		writeAuthError(w, r, err, http.StatusInternalServerError)
		return
	}

	s.Audit.Record(auditActor(r), "user.passkeys_cleared", target.Username, "passkeys removed by admin")
	writeJSON(w, http.StatusOK, map[string]any{"username": target.Username, "cleared": true})
}
