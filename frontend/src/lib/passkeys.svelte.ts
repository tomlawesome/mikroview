// SPDX-License-Identifier: AGPL-3.0-only
//
// #1250: the browser-facing half of passkeys as a second factor. Two
// jobs live here and nowhere else in the frontend --
//
// 1. The capability check (the design's own words: "in one place"),
//    read by AccountMenu/PasskeysOverlay (is this browser, on this
//    address, able to use *this account's* passkeys at all) and by
//    AuthScreen (same question, for the pending login's own origin).
// 2. The two ceremonies themselves -- navigator.credentials.create() for
//    registering, .get() for signing in -- wrapped around the matching
//    lib/api.ts round trips. Neither ceremony call can run in jsdom, so
//    this file's own tests stub `navigator.credentials` and assert on
//    what this code does with its result/rejection, never on the
//    browser's own behaviour.
//
// Deliberately uses only the native PublicKeyCredential JSON helpers
// (parseCreationOptionsFromJSON / parseRequestOptionsFromJSON /
// credential.toJSON()) -- no hand-rolled base64url walking. A browser
// too old to have them is exactly the "incapable" case below, answered
// the same as one with no WebAuthn support at all.

import {
  beginPasskeyLogin,
  beginPasskeyRegistration,
  finishPasskeyRegistration,
  submitPasskeyLoginAssertion,
  type PasskeyRegistrationFinish,
} from './api'

// passkeysSupported answers "can this browser do passkeys at all", with
// no reference to any particular account -- the three checks the design
// lists, each guarding the next: a insecure context has no
// PublicKeyCredential to ask; an old one that has it may still lack the
// JSON helpers this file relies on instead of hand-decoding.
export function passkeysSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext === true &&
    typeof PublicKeyCredential !== 'undefined' &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function'
  )
}

// passkeysUsableAt answers the design's other half of the same question:
// a capable browser sitting at the wrong address still can't use this
// account's passkeys -- WebAuthn ties a credential to the origin it was
// created under. `origin` is the server's own answer (session.passkeys
// .origin for the account overlay, the pending login's passkeyOrigin for
// the door) -- undefined for an account/deployment with none to compare
// against, which is never usable.
export function passkeysUsableAt(origin: string | undefined): boolean {
  return !!origin && passkeysSupported() && location.origin === origin
}

// The browser's own cancel/timeout/mismatch surfaces as a DOMException
// with no server round trip at all -- worded the same regardless of
// which one, since none of them are actionable beyond "try again": a
// user who dismissed the platform prompt does not need "NotAllowedError"
// explained to them.
function describeCeremonyError(err: unknown): string {
  if (err instanceof DOMException) {
    return "That didn't complete -- try again, or use another way in."
  }
  return err instanceof Error ? err.message : String(err)
}

// registerPasskey runs the enrol half end to end: begin -> browser
// prompt -> finish. Returns the finish response on success, or error
// text on any failure -- a refusal from the server, from the browser, or
// this account already having declined the ceremony -- so
// PasskeysOverlay's "adding" step has exactly one shape to render either
// way, matching enrolTOTP/confirmTOTP's own string-or-result convention.
export async function registerPasskey(name: string): Promise<PasskeyRegistrationFinish | string> {
  if (!passkeysSupported()) {
    return "This browser can't create a passkey -- try updating it, or use your authenticator app instead."
  }
  const begin = await beginPasskeyRegistration()
  if (typeof begin === 'string') return begin

  let credential: Credential | null
  try {
    const options = PublicKeyCredential.parseCreationOptionsFromJSON(
      begin.publicKey as Parameters<typeof PublicKeyCredential.parseCreationOptionsFromJSON>[0],
    )
    credential = await navigator.credentials.create({ publicKey: options })
  } catch (err) {
    return describeCeremonyError(err)
  }
  if (!credential) return "That didn't complete -- try again, or use another way in."

  const json = (credential as unknown as { toJSON(): unknown }).toJSON()
  return finishPasskeyRegistration(json, name)
}

// loginWithPasskey mirrors registerPasskey for the door's "Use your
// passkey" button: begin -> browser prompt -> finish. Returns null on
// success (authState.check() is the caller's job, same as submitFactor),
// or error text otherwise.
export async function loginWithPasskey(): Promise<string | null> {
  if (!passkeysSupported()) {
    return "This browser can't use a passkey -- use another way in below."
  }
  const begin = await beginPasskeyLogin()
  if (typeof begin === 'string') return begin

  let credential: Credential | null
  try {
    const options = PublicKeyCredential.parseRequestOptionsFromJSON(
      begin.publicKey as Parameters<typeof PublicKeyCredential.parseRequestOptionsFromJSON>[0],
    )
    credential = await navigator.credentials.get({ publicKey: options })
  } catch (err) {
    return describeCeremonyError(err)
  }
  if (!credential) return "That didn't complete -- try again, or use another way in."

  const json = (credential as unknown as { toJSON(): unknown }).toJSON()
  return submitPasskeyLoginAssertion(json)
}
