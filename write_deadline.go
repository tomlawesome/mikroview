// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// slowAuthPaths are the gauntlet v0.4.3 routes that mint or check
// recovery codes with Argon2id (64 MiB, t=3, p=4 each, at most four at
// once), so on a loaded host they can outlast the server-wide 15 s
// WriteTimeout and the client gets EOF with no reply (issue #1552).
// A segment written {name} matches any one path segment.
//
//	/api/auth/totp/confirm             HoldFirstTOTP mints ten codes
//	/api/auth/passkeys/register/finish HoldFirstPasskey mints ten codes
//	/api/auth/recovery-codes           RegenerateRecoveryCodes mints ten
//	/api/auth/login/factor             BurnRecoveryCode checks up to ten
//
// The admin step-up routes (recheckStepUp -> recheckSecondFactor) check
// the caller's password and then, when the code is not a current TOTP
// code, BurnRecoveryCode against every unused recovery code:
//
//	/api/auth/users                    handleCreateUser (creating an admin)
//	/api/auth/users/{id}/role          handleSetRole (granting admin)
//	/api/auth/users/{id}/unlock        handleUnlockUser (own account)
//	/api/auth/users/{id}/allow-sign-in handleAllowSignIn
var slowAuthPaths = []string{
	"/api/auth/totp/confirm",
	"/api/auth/passkeys/register/finish",
	"/api/auth/recovery-codes",
	"/api/auth/login/factor",
	"/api/auth/users",
	"/api/auth/users/{id}/role",
	"/api/auth/users/{id}/unlock",
	"/api/auth/users/{id}/allow-sign-in",
}

// slowAuthWriteDeadline gives requests to slowAuthPaths d to write their
// reply, set before next runs; every other request keeps the server's
// WriteTimeout. Method-agnostic: a method the route does not serve is
// answered at once by the mux, so the longer deadline costs nothing.
func slowAuthWriteDeadline(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSlowAuthPath(r.URL.Path) {
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d)); err != nil {
				// Only a ResponseWriter that does not support deadlines
				// fails here; the request then keeps the server default.
				slog.Debug("extending the write deadline", "path", r.URL.Path, "err", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isSlowAuthPath(path string) bool {
	for _, p := range slowAuthPaths {
		if pathMatches(p, path) {
			return true
		}
	}
	return false
}

// pathMatches reports whether path matches pattern segment for segment,
// a {name} segment in pattern matching any one non-empty segment.
func pathMatches(pattern, path string) bool {
	if !strings.Contains(pattern, "{") {
		return pattern == path
	}
	ps, xs := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(ps) != len(xs) {
		return false
	}
	for i, s := range ps {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			if xs[i] == "" {
				return false
			}
			continue
		}
		if s != xs[i] {
			return false
		}
	}
	return true
}
