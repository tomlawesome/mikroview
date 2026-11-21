// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// slowAuthPrefix covers every gauntlet route. Many of them run Argon2id
// (64 MiB, t=3, p=4, at most four at once): sign-in and password checks,
// the admin step-up recheck (password, then every unused recovery code),
// and minting or burning recovery codes. On a loaded host those outlast
// the server-wide 15 s WriteTimeout and the client gets EOF with no reply
// (#1552 listed the recovery-code routes; #1554 found password reset
// missing from that list, so the whole prefix is covered instead).
const slowAuthPrefix = "/api/auth/"

// slowAuthWriteDeadline gives requests under slowAuthPrefix d to write their
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
	return strings.HasPrefix(path, slowAuthPrefix)
}
