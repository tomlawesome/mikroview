// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP is the authenticator-app second factor from #1249: RFC 6238's
// Time-based One-Time Password, built on RFC 4226's HOTP. Everything
// here is standard-library only -- crypto/hmac, crypto/sha1,
// encoding/base32 -- by explicit requirement on the issue; adding a
// third-party TOTP library is not on the table even where one would be
// a perfectly good choice.
//
// This file is the algorithm only: generating a secret, deriving the
// enrolment URI an authenticator app scans, computing a code, and
// verifying one. It knows nothing about *User or the account store --
// that wiring, and the replay-guard field the store persists, belongs
// to whichever change adds TOTP enrolment to Store.

const (
	// totpSecretLen is 20 bytes (160 bits) -- RFC 4226 §4's recommended
	// shared-secret length for HMAC-SHA1, and conveniently also the
	// exact size of a SHA-1 output.
	totpSecretLen = 20
	// totpStep is RFC 6238's default time step. Every authenticator app
	// in practice assumes 30 seconds; a mikroview-specific value would
	// just make this account impossible to enrol in a normal app.
	totpStep = 30 * time.Second
	// totpDigits is the code length prescribed on #1249. RFC 6238's own
	// worked examples use 8; 6 is the number every consumer
	// authenticator app displays.
	totpDigits = 6
	// totpWindow is how many steps on either side of the current one a
	// submitted code is checked against, per #1249: accept the current
	// step and ±1, reject ±2. The allowance exists for clock drift and
	// for the second or two between a code being read off a phone and
	// typed in; it is deliberately small so a stolen code has a short
	// shelf life.
	totpWindow = 1
)

// GenerateTOTPSecret returns a fresh 20-byte shared secret from
// crypto/rand, suitable for both computing codes (GenerateTOTPCode,
// VerifyTOTP) and building an enrolment URI (TOTPEnrollmentURI, via
// EncodeTOTPSecret). The caller is responsible for persisting it --
// this package holds no state.
func GenerateTOTPSecret() ([]byte, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("auth: generate TOTP secret: %w", err)
	}
	return b, nil
}

// EncodeTOTPSecret base32-encodes secret with RFC 4648's standard
// alphabet and no padding -- the form both an otpauth:// URI and
// VerifyTOTP expect, and the form authenticator apps show when a
// secret is entered by hand instead of scanned.
func EncodeTOTPSecret(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// DecodeTOTPSecret reverses EncodeTOTPSecret. It also tolerates a
// lower-case, padded, or space-separated secret -- forms a person might
// paste in by hand, or that another implementation's encoder produces
// -- so a secret round-trips regardless of how it is typed. Anything
// that still fails to decode, or decodes to zero bytes, is an error:
// an empty secret is not a usable one.
func DecodeTOTPSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	s = strings.TrimRight(s, "=")
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("auth: malformed TOTP secret: %w", err)
	}
	if len(b) == 0 {
		return nil, errors.New("auth: empty TOTP secret")
	}
	return b, nil
}

// totpLabelEscape percent-encodes every byte of s outside RFC 3986's
// "unreserved" set (letters, digits, '-', '_', '.', '~').
//
// url.PathEscape is deliberately not used here: it treats ':' and '@'
// as safe in a path segment (RFC 3986's pchar allows them), which is
// exactly wrong for this one field. The otpauth:// label is
// "Issuer:AccountName" with a single literal colon as the separator
// (Google's "Key URI Format", the de facto standard every
// authenticator app follows -- RFC 6238 itself defines no URI). If a
// username contained its own colon, PathEscape would pass it through
// unescaped and an app parsing the label would no longer agree with us
// on where the issuer ends and the account name begins. Escaping to
// the stricter unreserved set removes that ambiguity for any
// character, not just ':' -- including a space, which some scanners
// fail to turn back into a space if it arrives as PathEscape's '%20'
// mixed with an unescaped ':' nearby, and multi-byte UTF-8, which this
// walks and escapes one byte at a time.
func totpLabelEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// TOTPEnrollmentURI builds the otpauth:// URI an authenticator app
// scans (as a QR code) or accepts pasted, for a fresh enrolment of
// username against secret: otpauth://totp/MikroView:<username>?secret=…&issuer=MikroView.
//
// username is escaped by totpLabelEscape rather than left to a generic
// URL escaper, specifically so a username holding a space or a colon
// still produces a URI an app parses the way we intend -- see that
// function's comment. secret and the issuer name go through
// url.Values, which escapes the query string correctly on its own.
func TOTPEnrollmentURI(username string, secret []byte) string {
	label := "MikroView:" + totpLabelEscape(username)
	v := url.Values{}
	v.Set("secret", EncodeTOTPSecret(secret))
	v.Set("issuer", "MikroView")
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// totpCounter returns the RFC 6238 time-step counter for t: the number
// of step-sized intervals since the Unix epoch. Both sides of the
// protocol hash this counter instead of t itself, which is what makes
// a code valid for a whole step rather than one instant, and lets the
// two clocks disagree by anything under a step and still agree on the
// counter.
//
// Clamped at zero for any t before the epoch, so a caller passing a
// zero time.Time (Go's time.Time zero value, year 1) cannot underflow
// the uint64 that carries it everywhere else in this file.
func totpCounter(t time.Time, step time.Duration) uint64 {
	secs := t.Unix()
	if secs < 0 {
		secs = 0
	}
	return uint64(secs) / uint64(step/time.Second)
}

// GenerateTOTPCode returns the totpDigits-digit decimal code for secret
// at counter -- RFC 4226 §5.3's HOTP algorithm (HMAC-SHA1 of the
// big-endian counter, then dynamic truncation), which RFC 6238 layers
// TOTP on top of by feeding it totpCounter's output instead of an
// incrementing counter.
//
// Takes the already-decoded secret, not the base32 string a person or
// a store would hold -- DecodeTOTPSecret sits between the two, and is
// where a malformed secret is caught. This function has no error case
// of its own: crypto/hmac never fails, including for a zero-length
// key, so returning an error here would be dead code that every caller
// still has to handle.
func GenerateTOTPCode(secret []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation, RFC 4226 §5.3: the low nibble of the last byte
	// picks a 4-byte window (0..15, and sum is 20 bytes long, so
	// offset+4 never runs past the end); the top bit of that window is
	// cleared to keep the result a positive 31-bit int.
	offset := sum[len(sum)-1] & 0x0f
	code := uint32(sum[offset]&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, code%mod)
}

// VerifyTOTP reports whether code is currently valid for encodedSecret
// (as produced by EncodeTOTPSecret) at now, within totpWindow steps
// either side, and has not already been consumed.
//
// lastUsedCounter is the counter of the most recently accepted code for
// this secret, or 0 if none has ever been accepted -- 0 itself can
// never be a real match for any date after 1970-01-01T00:00:30Z, so it
// is a safe "never used" sentinel, the same way Token.LastUsedAt's zero
// time.Time means "never used". Any candidate counter <= lastUsedCounter
// is skipped even when its code is correct: that is the replay guard,
// enforced here rather than at each call site, so a caller cannot
// forget it. On acceptance, matchedCounter is the counter that matched;
// the caller is expected to persist it as the new lastUsedCounter for
// the next call, which is what makes the guard advance instead of
// permanently wedging the account once a code is used.
//
// A malformed or empty encodedSecret, or a code that is not exactly
// totpDigits decimal digits, is refused (ok == false) rather than
// causing an error or a panic -- there is nothing a caller can usefully
// do differently, the same stance VerifyPassword takes for a malformed
// hash.
func VerifyTOTP(encodedSecret, code string, now time.Time, lastUsedCounter uint64) (matchedCounter uint64, ok bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}

	secret, err := DecodeTOTPSecret(encodedSecret)
	if err != nil {
		return 0, false
	}

	current := totpCounter(now, totpStep)
	for d := -totpWindow; d <= totpWindow; d++ {
		var c uint64
		if d < 0 {
			if uint64(-d) > current {
				continue // would underflow: before the epoch's first step
			}
			c = current - uint64(-d)
		} else {
			c = current + uint64(d)
		}
		if c <= lastUsedCounter {
			continue // already spent (or older than what was spent)
		}
		want := GenerateTOTPCode(secret, c)
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return c, true
		}
	}
	return 0, false
}
