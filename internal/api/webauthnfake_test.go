// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// FakeAuthenticator hand-builds WebAuthn ceremony responses the real go-webauthn library
// accepts, standing in for the software authenticator #1250's issue text assumed the library
// shipped. It does not: go-webauthn's own testing/ package only mocks the FIDO metadata
// provider, and the authenticator helpers its own end-to-end tests build ceremonies with are
// unexported files inside the library's module and cannot be imported from here. This file
// follows webauthn/es256k_e2e_test.go's pattern (a stdlib key, hand-assembled authenticator
// data, a CBOR attestation object, a signed assertion, using only the library's own protocol
// and webauthncbor/webauthncose packages) almost exactly, substituting an ECDSA P-256 key and
// the library's ES256 COSE algorithm for its secp256k1 example. See
// docs/plans/passkeys-second-factor.md, "The fake authenticator (slice C)".
//
// It signs everything with a single ECDSA P-256 key generated at construction, and reports an
// RP ID, origin, and sign count a caller can change between calls -- deliberately, because
// every route in wave 2 that refuses a bad ceremony (wrong origin, wrong RP ID, a sign count
// that goes backwards) needs a fake able to produce that bad ceremony on demand. A fake that
// can only ever produce valid input would let every refusal test pass while proving nothing --
// the same trap the design doc's "tests that would pass while proving nothing" section names
// for the real routes.
type FakeAuthenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte

	// RPID and Origin are what this authenticator signs into its authenticator data and
	// client data -- not necessarily what the server it's being tested against is
	// configured with. Set them to the server's actual values to produce a ceremony that
	// should succeed, or to anything else to produce one a specific guard should refuse.
	RPID   string
	Origin string

	// SignCount is the counter value AssertionResponse embeds in the next assertion's
	// authenticator data. It is never advanced automatically: a real roaming authenticator
	// increments on every use, but the common passkey case -- a platform authenticator
	// that always reports zero -- has to be reachable too. go-webauthn's
	// Authenticator.UpdateCounter deliberately never raises CloneWarning for a 0 -> 0
	// assertion, and a fake that auto-incremented could never produce that case to prove
	// it isn't falsely refused. Set SignCount explicitly before each call to control
	// exactly what the next assertion reports, including a value that regresses what the
	// caller already recorded (to prove the clone-warning refusal instead).
	SignCount uint32
}

// NewFakeAuthenticator generates a fresh ECDSA P-256 key and a random credential ID, and
// starts out reporting the given rpID and origin -- the values a real registration against
// that Relying Party would produce.
func NewFakeAuthenticator(rpID, origin string) *FakeAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		// crypto/rand failing is not a condition any caller of this test helper can
		// usefully handle -- every test using it needs a key to exist.
		panic("api: generating fake authenticator key: " + err.Error())
	}

	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		panic("api: generating fake credential id: " + err.Error())
	}

	return &FakeAuthenticator{key: key, credentialID: credentialID, RPID: rpID, Origin: origin}
}

// CredentialID is the credential ID this authenticator's registration response carries, and
// the id/rawId an assertion response must reuse to be recognised as the same credential.
func (f *FakeAuthenticator) CredentialID() []byte {
	return f.credentialID
}

// RegisterResponse builds the raw JSON body a browser would hand back from
// navigator.credentials.create() for the given creation challenge: a self-attested ("packed"
// attestation format with no x5c -- go-webauthn reports this as AttestationType
// "basic_surrogate", the exact shape webauthn/es256k_e2e_test.go's
// TestES256KCeremoniesEndToEnd exercises against the real library) ES256 credential, signed
// over authenticator data built from f.RPID and client data built from f.Origin -- whichever
// the caller last set, not necessarily what the creation options asked for.
func (f *FakeAuthenticator) RegisterResponse(creation *protocol.CredentialCreation) ([]byte, error) {
	clientDataJSON, err := f.clientDataJSON("webauthn.create", creation.Response.Challenge)
	if err != nil {
		return nil, err
	}

	publicKey, err := webauthncbor.Marshal(map[int64]any{
		1:  int64(webauthncose.EllipticKey),
		3:  int64(webauthncose.AlgES256),
		-1: int64(webauthncose.P256),
		-2: f.key.PublicKey.X.FillBytes(make([]byte, 32)),
		-3: f.key.PublicKey.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		return nil, fmt.Errorf("fake authenticator: encoding credential public key: %w", err)
	}

	attestedCredentialData := make([]byte, 16, 16+2+len(f.credentialID)+len(publicKey))
	attestedCredentialData = appendUint16(attestedCredentialData, len(f.credentialID))
	attestedCredentialData = append(attestedCredentialData, f.credentialID...)
	attestedCredentialData = append(attestedCredentialData, publicKey...)

	const flags = protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagBackupEligible |
		protocol.FlagBackupState | protocol.FlagAttestedCredentialData

	authData := f.authenticatorData(flags, 0, attestedCredentialData)

	sig, err := f.sign(authData, clientDataJSON)
	if err != nil {
		return nil, err
	}

	attestationObject, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "packed",
		"attStmt":  map[string]any{"alg": int64(webauthncose.AlgES256), "sig": sig},
		"authData": authData,
	})
	if err != nil {
		return nil, fmt.Errorf("fake authenticator: encoding attestation object: %w", err)
	}

	id := base64.RawURLEncoding.EncodeToString(f.credentialID)

	return json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestationObject),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
		},
	})
}

// AssertionResponse builds the raw JSON body navigator.credentials.get() would hand back for
// the given assertion challenge, signed with f.SignCount as the authenticator data's counter
// -- set it before calling to control exactly what count the caller observes.
func (f *FakeAuthenticator) AssertionResponse(assertion *protocol.CredentialAssertion) ([]byte, error) {
	clientDataJSON, err := f.clientDataJSON("webauthn.get", assertion.Response.Challenge)
	if err != nil {
		return nil, err
	}

	// BackupEligible/BackupState must match what RegisterResponse reported: go-webauthn
	// stores BackupEligible from the registration ceremony on the credential and then
	// checks every later assertion still reports the same value ("Backup Eligible flag
	// inconsistency detected during login validation" otherwise) -- a real synced platform
	// passkey reports both consistently on every ceremony, and this fake does too.
	const flags = protocol.FlagUserPresent | protocol.FlagUserVerified | protocol.FlagBackupEligible | protocol.FlagBackupState

	authData := f.authenticatorData(flags, f.SignCount, nil)

	sig, err := f.sign(authData, clientDataJSON)
	if err != nil {
		return nil, err
	}

	id := base64.RawURLEncoding.EncodeToString(f.credentialID)

	return json.Marshal(map[string]any{
		"id":    id,
		"rawId": id,
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(sig),
		},
	})
}

// clientDataJSON builds the CollectedClientData JSON a browser would produce for the named
// ceremony ("webauthn.create" or "webauthn.get"), declaring f.Origin as the origin the
// ceremony ran at.
func (f *FakeAuthenticator) clientDataJSON(ceremony string, challenge protocol.URLEncodedBase64) ([]byte, error) {
	data, err := json.Marshal(map[string]any{
		"type":        ceremony,
		"challenge":   challenge.String(),
		"origin":      f.Origin,
		"crossOrigin": false,
	})
	if err != nil {
		return nil, fmt.Errorf("fake authenticator: encoding client data: %w", err)
	}

	return data, nil
}

// authenticatorData builds the authenticator data structure -- RP ID hash, flags, sign
// count, and optionally attested credential data. It hashes f.RPID rather than whatever RP ID
// the ceremony options named, for the same "the caller controls what actually gets signed"
// reason the whole type exists.
func (f *FakeAuthenticator) authenticatorData(flags protocol.AuthenticatorFlags, signCount uint32, attestedCredentialData []byte) []byte {
	rpIDHash := sha256.Sum256([]byte(f.RPID))

	data := make([]byte, 0, 37+len(attestedCredentialData))
	data = append(data, rpIDHash[:]...)
	data = append(data, byte(flags))
	data = appendUint32(data, signCount)

	return append(data, attestedCredentialData...)
}

// sign computes the ECDSA P-256 / SHA-256 signature over authData || SHA-256(clientDataJSON)
// -- the exact bytes the WebAuthn spec requires an authenticator to sign over for both
// registration and assertion -- in ASN.1 DER form, the encoding every ES256 signature in the
// protocol uses.
func (f *FakeAuthenticator) sign(authData, clientDataJSON []byte) ([]byte, error) {
	clientDataHash := sha256.Sum256(clientDataJSON)

	signed := make([]byte, 0, len(authData)+len(clientDataHash))
	signed = append(signed, authData...)
	signed = append(signed, clientDataHash[:]...)

	digest := sha256.Sum256(signed)

	sig, err := ecdsa.SignASN1(rand.Reader, f.key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("fake authenticator: signing: %w", err)
	}

	return sig, nil
}

func appendUint16(b []byte, n int) []byte {
	return append(b, byte(n>>8), byte(n)) //nolint:gosec // n is len(f.credentialID), always small.
}

func appendUint32(b []byte, n uint32) []byte {
	return append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}
