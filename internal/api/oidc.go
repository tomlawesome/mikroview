// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

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

func (s *Server) setOIDCFlowCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcFlowCookieName,
		Value:    value,
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		Secure:   s.SecureCookie,
		// Lax, not Strict: the provider's redirect back to /callback is
		// a top-level cross-site GET navigation, which Strict would drop
		// this cookie on entirely, breaking the flow.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcFlowCookieMaxAge.Seconds()),
	})
}

func (s *Server) clearOIDCFlowCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcFlowCookieName,
		Value:    "",
		Path:     "/api/auth/oidc",
		HttpOnly: true,
		Secure:   s.SecureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func redirectWithSSOError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?ssoError="+code, http.StatusFound)
}
