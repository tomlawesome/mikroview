package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a random 32-character hex string, used for both user
// IDs and session IDs -- unguessable, and unlike a counter or timestamp,
// carries no information about how many users/sessions exist.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing means the OS's CSPRNG is unavailable --
		// not a condition worth degrading gracefully from for an ID that
		// grants access to an account or session.
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
