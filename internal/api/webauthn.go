// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ---- Relying Party construction (#1250) ----
//
// See docs/plans/passkeys-second-factor.md, "The public URL setting" and "RP construction",
// which this file implements. Passkeys need MikroView to have a web address: a passkey is
// bound to a Relying Party ID (a domain name) and an origin (scheme + that domain + port),
// neither of which MikroView otherwise has -- the rest of the app is happily reached by IP.
// Wave 1 slice B (internal/config) owns the `publicUrl` setting and validates it (warn, never
// fatal -- the config problem codes CFG-0100..CFG-0103 in docs/configuration.md describe what
// it warns about). This file owns turning that setting into a definite answer -- ready or not,
// and why not -- and, when it's ready, the actual go-webauthn Relying Party.

// PasskeyStatus reports whether this server can offer passkeys at all, and why not when it
// can't. Computed once at boot by NewRelyingParty from the validated `publicUrl` setting, and
// held for the process's lifetime alongside the RelyingParty it came from -- restarting after
// fixing the setting is what picks up a status change, the same "computed once at boot"
// lifetime pendingLoginCodec and SessionStore already have (see auth.go).
type PasskeyStatus string

const (
	// PasskeyStatusReady: publicUrl is a usable https (or http+localhost) address with a
	// non-IP host. Passkeys work.
	PasskeyStatusReady PasskeyStatus = "ready"

	// PasskeyStatusUnset: publicUrl was never configured, or did not parse as an absolute
	// URL. CFG-0100 covers the latter case as "ignored" -- server-side that is the same
	// state as never having set it, so both collapse to this one status.
	PasskeyStatusUnset PasskeyStatus = "unset"

	// PasskeyStatusIP: publicUrl's host is an IP literal (CFG-0101). A passkey is bound to
	// a domain name; browsers refuse to create one for a bare IP address.
	PasskeyStatusIP PasskeyStatus = "ip"

	// PasskeyStatusInsecure: publicUrl's scheme isn't one browsers will create a passkey
	// over. CFG-0102 names the specific case of http on a non-localhost host; any other
	// non-https scheme is refused here for the identical reason -- browsers only ever run
	// a passkey ceremony from a secure context, and https (or http on localhost, which
	// browsers also treat as secure) is the only way to get one.
	PasskeyStatusInsecure PasskeyStatus = "insecure"
)

// RelyingParty is the server's WebAuthn configuration, computed once at boot by
// NewRelyingParty. WebAuthn, RPID and Origin are set only when Status is
// PasskeyStatusReady -- every caller must check Status before touching them.
type RelyingParty struct {
	// WebAuthn is the constructed go-webauthn client, ready to begin and finish
	// registration/login ceremonies. Nil unless Status is PasskeyStatusReady.
	WebAuthn *webauthn.WebAuthn

	// Status is why passkeys are or are not available here; see the PasskeyStatus
	// constants. GET /api/auth/session reports it so the frontend can explain an
	// unavailable state instead of just hiding the feature (the design forbids silently
	// hiding it).
	Status PasskeyStatus

	// RPID is the domain name passkeys are registered under -- webauthn.Config.RPID, and
	// the value recorded on every stored Passkey for the stale-passkey check described in
	// the design doc's "When publicUrl is set but later changes". Set only when Status is
	// PasskeyStatusReady.
	RPID string

	// Origin is the exact origin (scheme://host[:port]) ceremonies run at --
	// webauthn.Config.RPOrigins' one entry, and the `passkeys.origin` value GET
	// /api/auth/session reports. Set only when Status is PasskeyStatusReady.
	Origin string
}

// NewRelyingParty computes passkey availability from publicURL -- internal/config's
// `publicUrl` setting, exactly as configured -- and, when it's usable, constructs the
// go-webauthn Relying Party.
//
// Never returns an error for a bad publicURL: that is a PasskeyStatus, not a startup failure,
// per the design's "MikroView always boots; passkeys fail into an explained unavailable
// state" stance -- config validation (internal/config) already warned about the setting, and
// this function's job is only to turn it into a definite yes/no. An error return means
// webauthn.New itself rejected a configuration this function believed was well-formed, which
// would be a bug here rather than a bad setting.
func NewRelyingParty(publicURL string) (*RelyingParty, error) {
	if publicURL == "" {
		return &RelyingParty{Status: PasskeyStatusUnset}, nil
	}

	u, err := url.Parse(publicURL)
	if err != nil || !u.IsAbs() || u.Host == "" {
		// CFG-0100: doesn't parse as an absolute URL -- ignored, same as unset.
		return &RelyingParty{Status: PasskeyStatusUnset}, nil
	}

	hostname := u.Hostname()

	if net.ParseIP(hostname) != nil {
		// CFG-0101.
		return &RelyingParty{Status: PasskeyStatusIP}, nil
	}

	// CFG-0102 names the http+non-localhost case explicitly; see PasskeyStatusInsecure's
	// doc comment for why every other non-https scheme collapses into the same status.
	secure := u.Scheme == "https" || (u.Scheme == "http" && hostname == "localhost")
	if !secure {
		return &RelyingParty{Status: PasskeyStatusInsecure}, nil
	}

	// CFG-0103: a path, query or fragment on publicUrl is a config mistake worth warning
	// about (internal/config's job), but not one that stops passkeys from working -- an
	// origin is only ever scheme://host[:port], so anything past that is silently dropped
	// here rather than carried into RPOrigins.
	origin := u.Scheme + "://" + u.Host

	wa, err := webauthn.New(&webauthn.Config{
		RPID:          hostname,
		RPDisplayName: "MikroView",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, fmt.Errorf("api: constructing webauthn relying party: %w", err)
	}

	return &RelyingParty{WebAuthn: wa, Status: PasskeyStatusReady, RPID: hostname, Origin: origin}, nil
}

// ---- WebAuthn session cookie codecs (#1250) ----
//
// webauthn.SessionData is the ceremony state go-webauthn hands back from every Begin* call
// and needs returned unchanged to the matching Finish* call: the challenge, the RPID and
// origin the ceremony was bound to, which credentials it may complete against. It must not be
// readable or modifiable by whoever holds the cookie carrying it -- RFC 6265 gives no such
// guarantee on its own -- so it is sealed exactly the way #1249's pendingLoginStateCodec
// (internal/api/auth.go) already seals pendingLoginState: AES-256-GCM, stdlib only,
// authenticated so a tampered cookie fails the tag check rather than decoding into a
// different ceremony, with a key generated once via crypto/rand and held only in memory for
// the process's lifetime.
//
// webauthnSessionCodec is a second implementation rather than a reuse of
// pendingLoginStateCodec, for the same reason auth.go gives for not reusing oidc.StateCodec
// there: widening a codec built for one cookie's payload to also carry an unrelated one would
// leave neither cookie's shape visible from its own file. The three codecs (oidc's,
// pending-login's, this one) share a construction, not a type.
type webauthnSessionCodec struct {
	aead cipher.AEAD
}

// errWebAuthnSessionInvalid covers every way a sealed SessionData can fail to decode:
// tampered, corrupt, or sealed under a different codec's key. One error rather than several,
// the same stance errPendingLoginInvalid takes and for the same reason -- which specific
// failure occurred isn't something a caller, or an attacker probing a passkey route, needs to
// be able to tell apart.
//
// Expiry is deliberately not this codec's concern, unlike pendingLoginStateCodec's: that type
// seals a payload with no expiry of its own (pendingLoginState), so the codec had to check
// IssuedAt itself. webauthn.SessionData already carries its own Expires field, set by
// BeginRegistration/BeginLogin and checked again by FinishRegistration/FinishLogin, so
// re-checking it here would only duplicate logic the library already owns -- and risk that
// duplicate drifting from it.
var errWebAuthnSessionInvalid = errors.New("api: webauthn session expired or was tampered with")

func newWebAuthnSessionCodec() *webauthnSessionCodec {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Same stance mustNewPendingLoginCodec takes: a CSPRNG that cannot produce bytes
		// isn't a condition either passkey ceremony can degrade from gracefully -- every
		// registration or login second factor depends on a codec existing.
		panic("api: crypto/rand unavailable: " + err.Error())
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		panic("api: constructing webauthn session cipher: " + err.Error())
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic("api: constructing webauthn session AEAD: " + err.Error())
	}

	return &webauthnSessionCodec{aead: aead}
}

// passkeyRegisterSessionCodec and passkeyAssertSessionCodec each seal one ceremony's cookie:
// register/begin's mikroview_passkey_register and login/factor-begin's
// mikroview_passkey_assert (routes, wave 2 slice D). They are built with two independently
// generated keys rather than sharing one codec instance: a value sealed for one ceremony must
// be structurally incapable of decoding as the other, so a registration's SessionData can
// never be replayed to finish a login (or vice versa) even if a cookie somehow ended up on
// the wrong path. That is a property of the ciphertext itself this way, not a rule a route
// handler has to remember to enforce.
var (
	passkeyRegisterSessionCodec = newWebAuthnSessionCodec()
	passkeyAssertSessionCodec   = newWebAuthnSessionCodec()
)

// encode seals sd for the cookie value. The caller writes the result behind Max-Age (5
// minutes per the design), which bounds how long a browser holds onto it; encode/decode
// themselves place no separate limit on it (see errWebAuthnSessionInvalid's doc comment).
func (c *webauthnSessionCodec) encode(sd webauthn.SessionData) (string, error) {
	plaintext, err := json.Marshal(sd)
	if err != nil {
		return "", fmt.Errorf("api: encoding webauthn session: %w", err)
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("api: generating webauthn session seal nonce: %w", err)
	}

	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)

	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// decode reverses encode, refusing (errWebAuthnSessionInvalid) anything malformed, tampered,
// or sealed under a different codec's key.
func (c *webauthnSessionCodec) decode(cookieValue string) (webauthn.SessionData, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(cookieValue)
	if err != nil {
		return webauthn.SessionData{}, errWebAuthnSessionInvalid
	}

	ns := c.aead.NonceSize()
	if len(sealed) < ns {
		return webauthn.SessionData{}, errWebAuthnSessionInvalid
	}

	nonce, ciphertext := sealed[:ns], sealed[ns:]

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return webauthn.SessionData{}, errWebAuthnSessionInvalid
	}

	var sd webauthn.SessionData
	if err := json.Unmarshal(plaintext, &sd); err != nil {
		return webauthn.SessionData{}, errWebAuthnSessionInvalid
	}

	return sd, nil
}
