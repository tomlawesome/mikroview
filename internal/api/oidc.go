// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/oidc"
)

var oidcLog = logging.New("oidc-api")

const oidcFlowCookieName = "mikroview_oidc_flow"

// oidcFlowCookieMaxAge bounds both the cookie's own Max-Age and the
// tolerance passed to StateCodec.Decode -- kept as one constant so the
// two can never drift apart. Generous enough for a real login (provider
// discovery page, maybe an MFA prompt) without leaving a usable flow
// lying around for long if abandoned.
const oidcFlowCookieMaxAge = 10 * time.Minute

// lastLocalAdminRefusal is what an admin gets back when they ask to
// link without acknowledging what it costs the deployment. It names the
// consequence and the way to avoid it, because this text is what the
// person reads when the overlay's own warning has been bypassed.
const lastLocalAdminRefusal = "linking this account removes its password, and it is the only admin that can sign in without your identity provider. " +
	"If the provider ever goes down, nobody can sign in to MikroView until somebody runs `mikroview -transfer-admin` at the command line. " +
	"Confirm that you mean to do it."

// handleOIDCLogin starts a login: generates fresh PKCE/state/nonce
// values (internal/oidc.FlowState), seals them into a short-lived
// cookie (see setOIDCFlowCookie), and redirects the browser to the
// provider. 404s if OIDC isn't configured -- see Server.OIDC's doc
// comment.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}

	fs, err := oidc.NewFlowState(time.Now())
	if err != nil {
		http.Error(w, "failed to start SSO login", http.StatusInternalServerError)
		return
	}
	encoded, err := s.OIDCState.Encode(fs)
	if err != nil {
		http.Error(w, "failed to start SSO login", http.StatusInternalServerError)
		return
	}
	s.setOIDCFlowCookie(w, encoded)

	http.Redirect(w, r, s.OIDC.AuthCodeURL(fs.State, fs.Nonce, fs.CodeVerifier), http.StatusFound)
}

// handleOIDCLinkStart begins linking the signed-in account to an SSO
// identity. It returns the provider URL as JSON for the frontend to
// navigate to, rather than issuing a redirect itself.
//
// POST, not GET, and that is the security-relevant part. Linking is
// destructive -- it removes the account's local password permanently
// (see auth.Store.LinkOIDCIdentity). A GET that starts the flow could
// be triggered cross-site: an attacker embeds it, the victim's browser
// follows it, their identity provider silently re-authenticates them,
// and the callback completes a link the victim never asked for,
// destroying their password. POST puts the flow behind the CSRF header
// check (see csrfHeaderName), which a cross-site request cannot set.
//
// The target account is taken from the session and sealed into the flow
// state, never from the request body -- the browser does not get to say
// which account a link applies to. The body carries one thing only, and
// it is an acknowledgement, not a selection: see lastLocalAdminRefusal.
func (s *Server) handleOIDCLinkStart(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	caller := userFromContext(r)
	if caller == nil {
		writeUnauthorized(w, "sign in first")
		return
	}
	// Already SSO-only: there is no local password left to convert, and
	// re-linking would only let an account move between identities,
	// which is not what this endpoint is for.
	if !caller.LocalPassword() {
		http.Error(w, "this account already signs in through your identity provider", http.StatusConflict)
		return
	}
	// "SSO is additive; keep a local admin" (#1252). Linking removes
	// this account's local password, so an admin linking theirs is the
	// one request in the API that can leave a deployment with no way in
	// that does not depend on the identity provider. It is still
	// allowed -- the operator may genuinely want an SSO-only
	// deployment -- but only as a deliberate act, acknowledged in the
	// request rather than assumed from a button press. The check is
	// here and not only in SSOLinkOverlay.svelte so the rule holds for
	// curl and for a future client too.
	if s.Auth.HasLocalAdmin() && caller.Role == auth.RoleAdmin {
		var req struct {
			AcknowledgeLastLocalAdmin bool `json:"acknowledgeLastLocalAdmin"`
		}
		// A malformed or absent body is simply not an acknowledgement.
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
		if !req.AcknowledgeLastLocalAdmin {
			http.Error(w, lastLocalAdminRefusal, http.StatusConflict)
			return
		}
	}

	fs, err := oidc.NewFlowState(time.Now())
	if err != nil {
		http.Error(w, "failed to start SSO linking", http.StatusInternalServerError)
		return
	}
	fs.LinkUserID = caller.ID

	encoded, err := s.OIDCState.Encode(fs)
	if err != nil {
		http.Error(w, "failed to start SSO linking", http.StatusInternalServerError)
		return
	}
	s.setOIDCFlowCookie(w, encoded)
	writeJSON(w, http.StatusOK, map[string]string{
		"url": s.OIDC.AuthCodeURL(fs.State, fs.Nonce, fs.CodeVerifier),
	})
}

// completeOIDCLink finishes a flow that handleOIDCLinkStart began.
//
// The session is re-checked against the account sealed into the flow,
// rather than trusted from either side alone. The sealed value can't be
// forged, but it also can't notice that the browser signed out and
// signed in as somebody else while the provider round-trip was in
// flight -- without this check, that would attach the identity that
// just authenticated to whichever account started the flow.
func (s *Server) completeOIDCLink(w http.ResponseWriter, r *http.Request, fs oidc.FlowState, identity *oidc.Identity, now time.Time) {
	caller, ok := s.sessionUser(r, now)
	if !ok || caller.ID != fs.LinkUserID {
		redirectWithSSOError(w, r, "link_session_changed")
		return
	}

	if err := s.Auth.LinkOIDCIdentity(caller.ID, identity.Issuer, identity.Subject, now); err != nil {
		if err == auth.ErrOIDCIdentityTaken {
			redirectWithSSOError(w, r, "link_identity_taken")
			return
		}
		oidcLog.Warn(fmt.Sprintf("linking SSO identity to account %s failed: %v", caller.ID, err))
		redirectWithSSOError(w, r, "link_failed")
		return
	}
	detail := "issuer=" + identity.Issuer
	// #1252: the deployment just lost its break-glass account. Recorded
	// on the event itself rather than as a second entry, so the audit log
	// says what happened rather than what was intended -- an
	// acknowledged link that never came back from the provider changes
	// nothing and should leave no trace.
	if !s.Auth.HasLocalAdmin() {
		detail += "; no admin can sign in without SSO now"
	}
	s.Audit.Record(caller.Username, "account.link_sso", caller.Username, detail)

	// LinkOIDCIdentity sets PasswordChangedAt, which invalidates every
	// session issued before it -- including the one that just made this
	// request. A fresh session is issued so the person stays signed in
	// on this browser, while any other session they had is now dead,
	// which is the intended effect of the credential change.
	sess := s.Sessions.Create(caller.ID, now)
	s.setSessionCookie(w, sess.ID)
	http.Redirect(w, r, "/?ssoLinked=1", http.StatusFound)
}

// handleOIDCCallback completes a login: verifies the state/nonce/PKCE
// flow this browser started (see handleOIDCLogin), exchanges the
// authorization code, cryptographically verifies the resulting ID
// token, and either resolves or just-in-time provisions the local
// account for that (issuer, subject) identity (see
// auth.Store.FindOrCreateOIDCUser) before creating a normal session --
// from that point on this is indistinguishable from a local-password
// login to everything else in the app.
//
// Every failure path redirects to "/?ssoError=<opaque-code>" rather
// than rendering an error itself or reflecting any provider-supplied
// text -- an IdP/attacker-controlled error string has no business being
// echoed into the page. The flow cookie is cleared unconditionally,
// before any redirect, since it's single-use regardless of outcome.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}

	cookie, cookieErr := r.Cookie(oidcFlowCookieName)
	// Cleared here, before any of the Redirect/Error calls below can
	// call WriteHeader -- a header added after that point would be
	// silently dropped, and this cookie must never survive past one use
	// regardless of which path below is taken.
	s.clearOIDCFlowCookie(w)
	if cookieErr != nil {
		redirectWithSSOError(w, r, "state_mismatch")
		return
	}

	now := time.Now()
	fs, err := s.OIDCState.Decode(cookie.Value, oidcFlowCookieMaxAge, now)
	if err != nil {
		redirectWithSSOError(w, r, "state_mismatch")
		return
	}

	q := r.URL.Query()
	if q.Get("error") != "" {
		// The provider itself reported a failure (access_denied, etc) --
		// never surfaced verbatim, see this handler's doc comment.
		redirectWithSSOError(w, r, "provider_error")
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(fs.State)) != 1 {
		redirectWithSSOError(w, r, "state_mismatch")
		return
	}
	code := q.Get("code")
	if code == "" {
		redirectWithSSOError(w, r, "provider_error")
		return
	}

	tok, err := s.OIDC.Exchange(r.Context(), code, fs.CodeVerifier)
	if err != nil {
		redirectWithSSOError(w, r, "provider_error")
		return
	}
	identity, err := s.OIDC.VerifyIDToken(r.Context(), tok)
	if err != nil {
		redirectWithSSOError(w, r, "verification_failed")
		return
	}
	if !oidc.VerifyNonce(identity.Nonce, fs.Nonce) {
		redirectWithSSOError(w, r, "state_mismatch")
		return
	}

	// Authentic is not the same as authorised. This runs before
	// FindOrCreateOIDCUser on purpose: a refused identity must not be
	// provisioned an account as a side effect of being refused, and it
	// re-runs on every login, so revoking someone's group at the IdP
	// locks them out at their next sign-in rather than only whenever
	// their session happens to lapse.
	if err := s.OIDCPolicy.Permit(identity); err != nil {
		// The specific unmet condition goes to the operator's log, not to
		// the browser -- telling an outsider "not a member of any
		// permitted group" maps out the allowlist for them.
		oidcLog.Warn(fmt.Sprintf("refused SSO login for subject %q at %s: %v",
			identity.Subject, identity.Issuer, err))
		redirectWithSSOError(w, r, "not_permitted")
		return
	}

	// Branch after the policy check above, deliberately: an identity
	// this deployment refuses must not be attachable to an existing
	// account either, and linking is the more dangerous of the two
	// outcomes -- it hands that identity a permanent way in.
	if fs.IsLink() {
		s.completeOIDCLink(w, r, fs, identity, now)
		return
	}

	usernameHint := identity.PreferredUsername
	if usernameHint == "" {
		usernameHint = identity.Email
	}
	user, _, err := s.Auth.FindOrCreateOIDCUser(identity.Issuer, identity.Subject, usernameHint, now)
	if err != nil {
		redirectWithSSOError(w, r, "login_failed")
		return
	}

	sess := s.Sessions.Create(user.ID, now)
	s.setSessionCookie(w, sess.ID)
	http.Redirect(w, r, "/", http.StatusFound)
}

// The Lax-not-Strict reasoning this cookie depends on lives with
// writeCookie in auth.go, which is where SameSite is now set.
func (s *Server) setOIDCFlowCookie(w http.ResponseWriter, value string) {
	s.writeCookie(w, oidcFlowCookieName, value, "/api/auth/oidc", int(oidcFlowCookieMaxAge.Seconds()))
}

func (s *Server) clearOIDCFlowCookie(w http.ResponseWriter) {
	s.writeCookie(w, oidcFlowCookieName, "", "/api/auth/oidc", -1)
}

func redirectWithSSOError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?ssoError="+code, http.StatusFound)
}
