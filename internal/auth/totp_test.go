// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestGenerateTOTPCodeRFC6238Vectors is the reason any of this can be
// trusted: RFC 6238 Appendix B's SHA-1 column, computed against the
// shared secret the RFC itself specifies ("12345678901234567890", 20
// ASCII bytes -- exactly totpSecretLen).
//
// The RFC's published vectors are 8-digit codes; #1249 calls for 6.
// RFC 4226 §5.3's truncation is "DT mod 10^Digit" against the same
// 31-bit DT regardless of digit count, so the 6-digit code is exactly
// the low 6 digits of the RFC's 8-digit OTP -- not a different value
// that happens to be close. Each case below carries the RFC's original
// 8-digit OTP in a comment so it can be checked against the RFC text
// directly.
func TestGenerateTOTPCodeRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")

	tests := []struct {
		unixSeconds int64
		want        string // low 6 digits of the RFC's 8-digit OTP
	}{
		{59, "287082"},          // RFC OTP 94287082, T = 0000000000000001
		{1111111109, "081804"},  // RFC OTP 07081804, T = 00000000023523EC
		{1111111111, "050471"},  // RFC OTP 14050471, T = 00000000023523ED
		{1234567890, "005924"},  // RFC OTP 89005924, T = 000000000273EF07
		{2000000000, "279037"},  // RFC OTP 69279037, T = 0000000003F940AA
		{20000000000, "353130"}, // RFC OTP 65353130, T = 0000000027BC86AA
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("t=%d", tc.unixSeconds), func(t *testing.T) {
			counter := totpCounter(time.Unix(tc.unixSeconds, 0).UTC(), totpStep)
			if got := GenerateTOTPCode(secret, counter); got != tc.want {
				t.Errorf("GenerateTOTPCode at unix time %d (counter %d) = %q, want %q",
					tc.unixSeconds, counter, got, tc.want)
			}
		})
	}
}

func TestGenerateTOTPSecretShape(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if len(secret) != totpSecretLen {
		t.Fatalf("len(secret) = %d, want %d", len(secret), totpSecretLen)
	}

	other, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if bytes.Equal(secret, other) {
		t.Fatal("two draws from GenerateTOTPSecret produced the same secret")
	}
}

func TestEncodeDecodeTOTPSecretRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	encoded := EncodeTOTPSecret(secret)

	tests := []struct {
		name  string
		input string
	}{
		{"as encoded", encoded},
		{"lower case, as a person might type it", strings.ToLower(encoded)},
		{"with padding added back", encoded + strings.Repeat("=", (8-len(encoded)%8)%8)},
		{"with a stray space", encoded[:4] + " " + encoded[4:]},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodeTOTPSecret(tc.input)
			if err != nil {
				t.Fatalf("DecodeTOTPSecret(%q): %v", tc.input, err)
			}
			if !bytes.Equal(decoded, secret) {
				t.Errorf("DecodeTOTPSecret(%q) = %x, want %x", tc.input, decoded, secret)
			}
		})
	}
}

func TestDecodeTOTPSecretRefusesMalformedOrEmpty(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"not base32 at all", "not-valid-base32!!!"},
		{"padding only", "===="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeTOTPSecret(tc.input); err == nil {
				t.Errorf("DecodeTOTPSecret(%q): want error, got none", tc.input)
			}
		})
	}
}

// TestTOTPEnrollmentURIEscapesUsername covers exactly the failure mode
// #1249 calls out: a username containing a space or a colon must not
// produce a URI that an authenticator app parses differently from what
// we intend.
func TestTOTPEnrollmentURIEscapesUsername(t *testing.T) {
	secret := []byte("12345678901234567890")

	tests := []struct {
		name           string
		username       string
		wantLabel      string   // the escaped "MikroView:<username>" segment
		mustNotContain []string // raw forms that would signal broken escaping
	}{
		{
			name:      "plain username",
			username:  "alice",
			wantLabel: "MikroView:alice",
		},
		{
			name:      "username with a space",
			username:  "bob smith",
			wantLabel: "MikroView:bob%20smith",
		},
		{
			name:           "username with a colon",
			username:       "eve:evil",
			wantLabel:      "MikroView:eve%3Aevil",
			mustNotContain: []string{"MikroView:eve:evil"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uri := TOTPEnrollmentURI(tc.username, secret)

			if !strings.HasPrefix(uri, "otpauth://totp/"+tc.wantLabel+"?") {
				t.Errorf("TOTPEnrollmentURI(%q) = %q, want it to start with otpauth://totp/%s?",
					tc.username, uri, tc.wantLabel)
			}
			for _, bad := range tc.mustNotContain {
				if strings.Contains(uri, bad) {
					t.Errorf("TOTPEnrollmentURI(%q) = %q, contains unescaped %q", tc.username, uri, bad)
				}
			}
			parsed, err := url.Parse(uri)
			if err != nil {
				t.Fatalf("TOTPEnrollmentURI(%q) = %q, does not parse as a URI: %v", tc.username, uri, err)
			}
			if got := parsed.Query().Get("issuer"); got != "MikroView" {
				t.Errorf("issuer query param = %q, want MikroView", got)
			}
			if got := parsed.Query().Get("secret"); got != EncodeTOTPSecret(secret) {
				t.Errorf("secret query param = %q, want %q", got, EncodeTOTPSecret(secret))
			}
		})
	}
}

func TestVerifyTOTPAcceptsCurrentAndAdjacentSteps(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	encoded := EncodeTOTPSecret(secret)
	now := time.Unix(1_700_000_000, 0).UTC()
	current := totpCounter(now, totpStep)

	tests := []struct {
		name    string
		counter uint64
	}{
		{"current step", current},
		{"one step ahead", current + 1},
		{"one step behind", current - 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := GenerateTOTPCode(secret, tc.counter)
			matched, ok := VerifyTOTP(encoded, code, now, 0)
			if !ok {
				t.Fatalf("VerifyTOTP(code for counter %d) at current counter %d: want accepted, got refused",
					tc.counter, current)
			}
			if matched != tc.counter {
				t.Errorf("matched counter = %d, want %d", matched, tc.counter)
			}
		})
	}
}

func TestVerifyTOTPRejectsTwoStepsAway(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	encoded := EncodeTOTPSecret(secret)
	now := time.Unix(1_700_000_000, 0).UTC()
	current := totpCounter(now, totpStep)

	tests := []struct {
		name    string
		counter uint64
	}{
		{"two steps ahead", current + 2},
		{"two steps behind", current - 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := GenerateTOTPCode(secret, tc.counter)
			if _, ok := VerifyTOTP(encoded, code, now, 0); ok {
				t.Fatalf("VerifyTOTP(code for counter %d) at current counter %d: want refused, got accepted",
					tc.counter, current)
			}
		})
	}
}

// TestVerifyTOTPRefusesReplay is the guard the store depends on: the
// same code, matched to the same counter, must work exactly once.
func TestVerifyTOTPRefusesReplay(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	encoded := EncodeTOTPSecret(secret)
	now := time.Unix(1_700_000_000, 0).UTC()
	current := totpCounter(now, totpStep)
	code := GenerateTOTPCode(secret, current)

	matched, ok := VerifyTOTP(encoded, code, now, 0)
	if !ok {
		t.Fatal("first use of the code: want accepted, got refused")
	}
	if matched != current {
		t.Fatalf("matched counter = %d, want %d", matched, current)
	}

	if _, ok := VerifyTOTP(encoded, code, now, matched); ok {
		t.Fatal("replay of the same code at the same counter: want refused, got accepted")
	}
}

func TestVerifyTOTPRefusesMalformedOrEmptySecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name   string
		secret string
	}{
		{"empty secret", ""},
		{"not valid base32", "not-valid-base32!!!"},
		{"padding only, decodes to nothing", "===="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := VerifyTOTP(tc.secret, "123456", now, 0); ok {
				t.Errorf("VerifyTOTP with %s: want refused, got accepted", tc.name)
			}
		})
	}
}

func TestVerifyTOTPRefusesMalformedCodeWithoutPanicking(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	encoded := EncodeTOTPSecret(secret)
	now := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name string
		code string
	}{
		{"empty", ""},
		{"letters, not digits", "abcdef"},
		{"too short", "12345"},
		{"too long", "1234567"},
		{"contains a space", "12 456"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := VerifyTOTP(encoded, tc.code, now, 0); ok {
				t.Errorf("VerifyTOTP with code %q: want refused, got accepted", tc.code)
			}
		})
	}
}
