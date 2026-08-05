package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for a small self-hosted service -- RFC 9106's
// lower-resource recommendation (64 MiB memory, 1 pass, 4 threads).
// Encoded into every hash string, so changing these later doesn't
// invalidate existing hashes -- only new ones use the new parameters.
const (
	argon2Memory  uint32 = 64 * 1024 // KiB
	argon2Time    uint32 = 1
	argon2Threads uint8  = 4
	argon2KeyLen  uint32 = 32
	argon2SaltLen        = 16
)

// dummyHash is verified against when a username doesn't exist, so a
// failed login takes the same amount of time either way -- otherwise
// "valid username, wrong password" (does the Argon2id work) and "no
// such username" (returns immediately) would be distinguishable by
// response time, leaking which usernames exist.
var dummyHash = mustHashPassword("not-a-real-password-used-only-for-timing")

// HashPassword returns a self-describing Argon2id hash string
// ("argon2id$v=<version>$m=<memory>,t=<time>,p=<threads>$<salt>$<hash>",
// all base64) for password, with a fresh random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func mustHashPassword(password string) string {
	h, err := HashPassword(password)
	if err != nil {
		panic("auth: failed to compute dummy hash: " + err.Error())
	}
	return h
}

// VerifyPassword reports whether password matches encodedHash (as
// produced by HashPassword), comparing in constant time. A malformed
// encodedHash is treated as a non-match, never an error -- there's
// nothing a caller can usefully do differently.
func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
