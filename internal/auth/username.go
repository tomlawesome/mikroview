// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A username is not just a label. It is written into the audit trail,
// printed to an operator's terminal by `mikroview -list-users`, and
// shown in the admin's account list -- three places where the wrong
// characters stop being text and start being instructions or
// disguises. So it is validated at creation rather than escaped at each
// of those sites, which is the arrangement that stays correct when a
// fourth site is added.
const (
	// maxUsernameLength bounds what any of those surfaces has to render.
	// Long enough for an email address, which is what most identity
	// providers send as preferred_username.
	maxUsernameLength = 64
	minUsernameLength = 1
)

var (
	// ErrUsernameInvalid is returned for a username containing characters
	// that would be unsafe or misleading downstream.
	ErrUsernameInvalid = errors.New("auth: username contains characters that are not allowed")
	// ErrUsernameLength is returned for an empty or over-long username.
	ErrUsernameLength = fmt.Errorf("auth: username must be between %d and %d characters", minUsernameLength, maxUsernameLength)
)

// ValidateUsername rejects a username that would be unsafe downstream.
//
// Three things are refused, each for a specific reason:
//
//   - Control characters (C0, C1, DEL). An ANSI escape in a username is
//     executed by the operator's terminal when they run -list-users, and
//     a newline forges a whole extra line in the audit trail. This is
//     the defect behind CVE-2025-55754 (Tomcat, ANSI escapes reaching
//     console output) and CVE-2025-48432 (Django, crafted input reaching
//     logs unescaped).
//
//   - Unicode format characters (category Cf). These include the bidi
//     overrides -- U+202E RIGHT-TO-LEFT OVERRIDE and friends -- which
//     let a username render as something other than what it is. In an
//     account list whose whole purpose is telling the admin who holds
//     access, an account that displays as another account's name is a
//     real problem.
//
//   - Leading or trailing whitespace. " admin" and "admin" look
//     identical in a list, and the store's uniqueness check is on the
//     lowercased string, so both can exist at once.
//
// Deliberately not an allowlist of ASCII. Plenty of legitimate people
// have non-ASCII names, and refusing them to save a validation function
// is not a security decision.
func ValidateUsername(username string) error {
	if !utf8.ValidString(username) {
		return ErrUsernameInvalid
	}
	if username != strings.TrimSpace(username) {
		return ErrUsernameInvalid
	}
	n := utf8.RuneCountInString(username)
	if n < minUsernameLength || n > maxUsernameLength {
		return ErrUsernameLength
	}
	for _, r := range username {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrUsernameInvalid
		}
	}
	return nil
}

// sanitiseUsernameHint applies ValidateUsername to an identity
// provider's preferred_username/email claim, returning "" if it is not
// usable as a username.
//
// Returning "" rather than an error is the point. The claim is
// controlled by the identity provider, not by mikroview, and the person
// signing in has already authenticated successfully -- refusing the
// login because their IdP sent an awkward display name would lock out
// someone who did nothing wrong and cannot fix it. An empty hint makes
// uniqueUsernameLocked fall back to its deterministic `oidc-<hash>`
// name, so they still get a stable account.
func sanitiseUsernameHint(hint string) string {
	hint = strings.TrimSpace(hint)
	if ValidateUsername(hint) != nil {
		return ""
	}
	return hint
}
